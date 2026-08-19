package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
)

// commentsView is the session with comments on it, which is what all four
// comment commands answer with. The three that write build one holding the row
// they wrote, so a script parsing this surface parses one shape.
type commentsView struct {
	header

	Comments []store.Comment

	// filter is what the listing was asked for, and nil on the three commands
	// that write one comment. A listing counts what it found and says so when it
	// found nothing; a write has its one row and neither question applies.
	filter *filter

	// Width is what a body wraps into, measured by the caller so this stays a
	// pure function of the view and every formatting test is a literal.
	Width int
}

// indent is what a body hangs under the row naming it.
const indent = "    "

// elbow opens a response under the body it answers, the way the card hangs its
// box off the comment. Three single-cell glyphs, counted rather than measured.
const (
	elbow      = "╰─ "
	elbowWidth = 3
)

// screen is the terminal's width capped at comp.BodyWidth, and the cap itself
// when there is nothing to measure. A pipe and a test both take the fallback.
func screen(out io.Writer) int {
	f, ok := out.(*os.File)
	if !ok {
		return comp.BodyWidth
	}

	w, _, err := term.GetSize(f.Fd())
	if err != nil || w <= 0 {
		return comp.BodyWidth
	}
	return min(w, comp.BodyWidth)
}

// render writes a row per comment with its body indented under it.
func (v commentsView) render() string {
	var b strings.Builder
	v.write(&b)

	if v.filter != nil && len(v.Comments) == 0 {
		fmt.Fprintf(&b, "\n%s\n", v.filter.nothing())
		return b.String()
	}

	b.WriteString("\n")
	writeComments(&b, v.Comments, v.Width)

	if v.filter != nil {
		fmt.Fprintf(&b, "\n%s, %d unresolved\n",
			plural(len(v.Comments), "comment"), unresolved(v.Comments))
	}
	return b.String()
}

// writeComments lays the rows out in columns with each body under its own row.
//
// The rows are written one at a time rather than through writeColumns, because
// the bodies go between them. A blank line separates each comment from the next,
// which is what makes a body of several lines read as one.
func writeComments(b *strings.Builder, comments []store.Comment, width int) {
	rows := make([][]string, 0, len(comments))
	for _, c := range comments {
		rows = append(rows, []string{c.ID, at(c), string(c.Side), string(c.State)})
	}

	widths := columnWidths(rows)
	for i, c := range comments {
		if i > 0 {
			b.WriteString("\n")
		}
		writeRow(b, "", widths, rows[i])
		writeBody(b, c.Body, width)
		writeResponse(b, c.Response, width)
	}
}

// writeBody indents a comment under the row naming it, so a long line does not
// come back at column zero reading as the start of something else.
func writeBody(b *strings.Builder, body string, width int) {
	// A blank line carries no indent, so no line ends in whitespace.
	for _, line := range comp.Wrap(body, max(width-len(indent), 1)) {
		if line == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(indent + line + "\n")
	}
}

// writeResponse runs the response under the body it answers, opened by the elbow
// the card hangs its box off. Without it the two run together as one person.
func writeResponse(b *strings.Builder, response string, width int) {
	if response == "" {
		return
	}

	room := max(width-len(indent)-elbowWidth, 1)

	// The elbow goes on the first line with words on it. A response opening on a
	// blank line would otherwise spend it on nothing and print with no elbow.
	opened := false
	for _, line := range comp.Wrap(response, room) {
		if line == "" {
			b.WriteString("\n")
			continue
		}

		lead := indent + strings.Repeat(" ", elbowWidth)
		if !opened {
			lead, opened = indent+elbow, true
		}
		b.WriteString(lead + line + "\n")
	}
}

// at is where a comment points, in the one form every editor and terminal
// already knows: path:line, or path:A-B over a run of them.
//
// The path and the line are one cell rather than two. Split, they are a place
// nobody can click and nobody can paste, and a reader looking at a comment wants
// to be at the code it is about. A comment on the file itself is the path alone,
// because a path with no line is exactly what that means.
func at(c store.Comment) string {
	switch {
	case c.Scope == store.ScopeFile:
		return c.Path
	case c.Start == c.End:
		return fmt.Sprintf("%s:%d", c.Path, c.Start)
	default:
		return fmt.Sprintf("%s:%d-%d", c.Path, c.Start, c.End)
	}
}

// unresolved is the count that matters: everything somebody still has to answer.
// An orphaned comment is one of them, because the code moving under a comment is
// not an answer to it.
func unresolved(comments []store.Comment) int {
	n := 0
	for _, c := range comments {
		if c.State != store.CommentResolved {
			n++
		}
	}
	return n
}

// commentsPayload is the wire shape of the comment surface, and a contract with
// whatever is parsing it rather than a mirror of store.Comment.
//
// generation_id is a database row id and does not go on the wire. A reader
// wanting to know how old a comment is has both timestamps.
type commentsPayload struct {
	headerJSON

	Comments []commentJSON     `json:"comments"`
	Totals   commentTotalsJSON `json:"totals"`
}

type commentJSON struct {
	ID string `json:"id"`

	// Path is the name the comment is recorded under, which on the base side of a
	// rename is the name the file has on the base.
	Path  string      `json:"path"`
	Side  store.Side  `json:"side"`
	Scope store.Scope `json:"scope"`

	// Start and End are 0 on a file comment, which names the file rather than any
	// line in it.
	Start int `json:"start"`
	End   int `json:"end"`

	State store.CommentState `json:"state"`
	Body  string             `json:"body"`

	// Response is what an address left behind, and empty when it left none.
	Response string `json:"response"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// commentTotalsJSON counts each state, and the queue. Unresolved is spelled here
// rather than left to a consumer adding three of the others, because it is the
// number a hook is deciding on.
type commentTotalsJSON struct {
	Comments   int `json:"comments"`
	Open       int `json:"open"`
	Addressed  int `json:"addressed"`
	Resolved   int `json:"resolved"`
	Orphaned   int `json:"orphaned"`
	Unresolved int `json:"unresolved"`
}

// commentsPayloadOf projects the view onto the wire. Comments is made rather
// than declared, so an empty listing is [] and not null.
func commentsPayloadOf(v commentsView) commentsPayload {
	p := commentsPayload{
		headerJSON: headerOf(v.header),
		Comments:   make([]commentJSON, 0, len(v.Comments)),
	}

	for _, c := range v.Comments {
		p.Comments = append(p.Comments, commentJSON{
			ID:        c.ID,
			Path:      c.Path,
			Side:      c.Side,
			Scope:     c.Scope,
			Start:     c.Start,
			End:       c.End,
			State:     c.State,
			Body:      c.Body,
			Response:  c.Response,
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
		})

		switch c.State {
		case store.CommentOpen:
			p.Totals.Open++
		case store.CommentAddressed:
			p.Totals.Addressed++
		case store.CommentResolved:
			p.Totals.Resolved++
		case store.CommentOrphaned:
			p.Totals.Orphaned++
		}
	}

	p.Totals.Comments = len(v.Comments)
	p.Totals.Unresolved = unresolved(v.Comments)
	return p
}

func (v commentsView) encode(out io.Writer) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(commentsPayloadOf(v)); err != nil {
		return fmt.Errorf("writing the comments as JSON: %w", err)
	}
	return nil
}
