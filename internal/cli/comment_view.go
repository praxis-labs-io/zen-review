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

type commentsView struct {
	header

	Comments []store.Comment

	Replaced map[string][]string

	filter *filter

	Width int
}

const indent = "    "

const (
	elbow      = "╰─ "
	elbowWidth = 3
)

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

func writeBody(b *strings.Builder, body string, width int) {
	for _, line := range comp.Wrap(body, max(width-len(indent), 1)) {
		if line == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(indent + line + "\n")
	}
}

func writeResponse(b *strings.Builder, response string, width int) {
	if response == "" {
		return
	}

	room := max(width-len(indent)-elbowWidth, 1)

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

func unresolved(comments []store.Comment) int {
	n := 0
	for _, c := range comments {
		if c.State != store.CommentResolved {
			n++
		}
	}
	return n
}

type commentsPayload struct {
	headerJSON

	Comments []commentJSON     `json:"comments"`
	Totals   commentTotalsJSON `json:"totals"`
}

type commentJSON struct {
	ID string `json:"id"`

	Path  string      `json:"path"`
	Side  store.Side  `json:"side"`
	Scope store.Scope `json:"scope"`

	Start int `json:"start"`
	End   int `json:"end"`

	State store.CommentState `json:"state"`
	Body  string             `json:"body"`

	Response string `json:"response"`

	Replaced []string `json:"replaced"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type commentTotalsJSON struct {
	Comments   int `json:"comments"`
	Open       int `json:"open"`
	Addressed  int `json:"addressed"`
	Resolved   int `json:"resolved"`
	Orphaned   int `json:"orphaned"`
	Unresolved int `json:"unresolved"`
}

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
			Replaced:  v.Replaced[c.ID],
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
