package review

import (
	"context"
	"fmt"
	"strings"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

// Replaced is the code each answered comment was written against, by comment id,
// holding only the ones whose lines the changeset has since taken.
func (s *Session) Replaced(ctx context.Context, g Generation, comments []store.Comment) (map[string][]string, error) {
	want := make([]store.Comment, 0, len(comments))
	for _, c := range comments {
		if replaceable(c) {
			want = append(want, c)
		}
	}
	if len(want) == 0 {
		return nil, nil
	}

	files, err := s.db.GenFiles(ctx, g.ID)
	if err != nil {
		return nil, fmt.Errorf("reading generation %d to see what the responses replaced: %w", g.Seq, err)
	}
	live := liveBlobs(files)

	shas := make([]string, 0, len(want))
	for _, c := range want {
		shas = append(shas, c.AnchorBlob)
	}

	blobs, err := s.repo.Blobs(ctx, shas)
	if err != nil {
		return nil, fmt.Errorf("reading what the responses replaced: %w", err)
	}

	// A comment whose anchor blob has gone is dropped before any diff runs. The
	// pair would name an object git cannot read, and fail a call Blobs tolerated.
	held := make([]store.Comment, 0, len(want))
	for _, c := range want {
		if _, ok := blobs[c.AnchorBlob]; ok {
			held = append(held, c)
		}
	}

	moved, err := s.moves(ctx, held, live)
	if err != nil {
		return nil, err
	}

	out := make(map[string][]string, len(held))
	for _, c := range held {
		old := slice(text(blobs[c.AnchorBlob]), c.CreatedRange)
		if len(old) == 0 {
			continue
		}

		if took(moved, c.AnchorBlob, live[anchoredAt(c.Side, c.Path)], c.CreatedRange) {
			out[c.ID] = old
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// moves is each pair of blobs as the remap reads the move between them. Every
// comment on a file asks the same question of the same two, so the diff runs once.
func (s *Session) moves(ctx context.Context, comments []store.Comment, live map[string]string) (map[string]Translation, error) {
	moved := make(map[string]Translation, len(comments))
	for _, c := range comments {
		from, to := c.AnchorBlob, live[anchoredAt(c.Side, c.Path)]
		if to == "" || to == from {
			continue
		}
		if _, held := moved[from+to]; held {
			continue
		}

		patch, err := s.repo.RemapDiff(ctx, from, to)
		if err != nil {
			return nil, fmt.Errorf("diffing what a response replaced: %w", err)
		}
		if files := diff.Parse(patch); len(files) > 0 {
			moved[from+to] = Translate(files[0])
		}
	}
	return moved, nil
}

// took reports whether the changeset took any of the lines r covers. A file the
// live side no longer holds took every one; a pair with no move between took none.
func took(moved map[string]Translation, from, to string, r store.LineRange) bool {
	if to == "" {
		return true
	}

	t, held := moved[from+to]
	if !held {
		return false
	}

	was := []Range{{Start: r.Start, End: r.End}}
	return shrank(was, t.Ranges(was))
}

// replaceable is a comment a block can be drawn on: one the agent has answered,
// naming lines, with a creation range to slice its anchor blob by.
func replaceable(c store.Comment) bool {
	if c.Scope == store.ScopeFile || c.AnchorBlob == "" || c.CreatedRange.Start == 0 {
		return false
	}
	return c.State == store.CommentAddressed || c.Response != ""
}

// liveBlobs is each file's blob at a generation, keyed by the name a comment on
// that side is recorded under: on a rename the base keeps its own.
func liveBlobs(files []store.GenFile) map[string]string {
	out := make(map[string]string, len(files)*2)
	for _, f := range files {
		out[anchoredAt(store.SideHead, f.Path)] = f.HeadBlob

		base := f.Path
		if f.OldPath != "" {
			base = f.OldPath
		}
		out[anchoredAt(store.SideBase, base)] = f.BaseBlob
	}
	return out
}

func anchoredAt(side store.Side, path string) string { return string(side) + "\x00" + path }

// text is a blob's lines, without the empty one a trailing newline leaves.
func text(b []byte) []string {
	lines := strings.Split(string(b), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// slice is the closed line range of a file, clamped to what the file has. Lines
// are numbered from one.
func slice(lines []string, r store.LineRange) []string {
	if r.Start < 1 || r.Start > len(lines) {
		return nil
	}
	return lines[r.Start-1 : min(r.End, len(lines))]
}
