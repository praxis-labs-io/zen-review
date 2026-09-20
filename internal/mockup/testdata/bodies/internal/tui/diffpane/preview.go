package diffpane

import (
	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/review"
)

// preview is the whole file behind the hunks, filled in on p and dropped on the next reload.
type preview struct {
	path string
	gen  int64
	body review.Body
}

func (p preview) has(path string, gen int64) bool {
	return p.path == path && p.gen == gen && len(p.body.Lines) > 0
}

// fill returns every line of the file as a row, the changed ones from the hunks and the rest as
// context, so selection and the painter need no second path.
func fill(f review.File, b review.Body) []diff.Line {
	changed := make(map[int]diff.Line)
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			if l.Kind != diff.Context {
				changed[l.New] = l
			}
		}
	}

	out := make([]diff.Line, 0, len(b.Lines))
	for i, text := range b.Lines {
		no := i + 1
		if l, ok := changed[no]; ok {
			out = append(out, l)
			continue
		}
		out = append(out, diff.Line{Kind: diff.Context, Old: no, New: no, Text: text})
	}
	return out
}
