package review

import "github.com/praxis-labs-io/zen-review/internal/store"

// carry moved a generation's reviewed ranges forward by hand, before Translate took the job.
func carry(rows []store.ReviewedRange, moved map[string]Translation) []store.ReviewedRange {
	out := make([]store.ReviewedRange, 0, len(rows))
	for _, r := range rows {
		t, held := moved[r.Path]
		if !held {
			out = append(out, r)
			continue
		}

		for _, next := range t.Ranges([]Range{{Start: r.Start, End: r.End}}) {
			r.Start, r.End = next.Start, next.End
			out = append(out, r)
		}
	}
	return out
}
