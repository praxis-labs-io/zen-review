package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/zen-review/zen-review/internal/store"
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
	writeComments(&b, v.Comments)

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
func writeComments(b *strings.Builder, comments []store.Comment) {
	rows := make([][]string, 0, len(comments))
	for _, c := range comments {
		rows = append(rows, []string{c.ID, c.Path, string(c.Side), lines(c), string(c.State)})
	}

	widths := columnWidths(rows)
	for i, c := range comments {
		if i > 0 {
			b.WriteString("\n")
		}
		writeRow(b, "", widths, rows[i])
		writeBody(b, c.Body)
	}
}

// writeBody indents a comment under the row naming it. A blank line in the body
// stays blank rather than carrying an indent, so no line ends in whitespace.
func writeBody(b *strings.Builder, body string) {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			b.WriteString("\n")
			continue
		}
		fmt.Fprintf(b, "    %s\n", line)
	}
}

// lines is where a comment points, spelled the way the comment command takes it.
func lines(c store.Comment) string {
	switch {
	case c.Scope == store.ScopeFile:
		return "file"
	case c.Start == c.End:
		return fmt.Sprint(c.Start)
	default:
		return fmt.Sprintf("%d-%d", c.Start, c.End)
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
