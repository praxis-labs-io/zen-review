package mockup

import (
	"embed"
	"io/fs"
	"strings"
	"time"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

//go:embed all:testdata
var fixture embed.FS

// Repo is the name the reader draws in its title.
const Repo = "zen-review"

const sessionID = "mockup"

func files() []diff.File {
	patch, err := fixture.ReadFile("testdata/changeset.patch")
	if err != nil {
		panic("the mockup fixture patch is missing: " + err.Error())
	}
	return diff.Parse(patch)
}

// bodies is the whole text of each file on the side it has bytes on, by the path the changeset
// names it: the head for every file but the deleted one.
func bodies() map[string][]string {
	out := make(map[string][]string)

	err := fs.WalkDir(fixture, "testdata/bodies", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		text, err := fixture.ReadFile(p)
		if err != nil {
			return err
		}
		out[strings.TrimPrefix(p, "testdata/bodies/")] = split(string(text))
		return nil
	})
	if err != nil {
		panic("the mockup fixture bodies are missing: " + err.Error())
	}
	return out
}

func split(text string) []string {
	out := strings.Split(text, "\n")
	if n := len(out); n > 0 && out[n-1] == "" {
		out = out[:n-1]
	}
	return out
}

func base() review.Base {
	return review.Base{Ref: "origin/main", SHA: "9f4c1d0a7b28e3516ad9c0f2e84b7351cc06de92"}
}

func generation() review.Generation {
	return review.Generation{
		ID:        2,
		Seq:       2,
		CommitSha: "3b7e2a915c46d80fa1e5b3c7920d4f86ab15e703",
		BaseSha:   base().SHA,
		HeadSha:   "c81d5e9a02f743b6e1c98d40725a3fbb6e0917d4",
		CreatedAt: created,
	}
}

var created = time.Date(2026, 9, 18, 9, 14, 0, 0, time.UTC)

// reviewed reads the top of the tree and stops part-way down internal/review, so the tree draws
// all three states and the reader opens on a half-read hunk of Go rather than on a binary file.
func reviewed() []store.ReviewedRange {
	return []store.ReviewedRange{
		whole("assets/preview.png", store.SideHead),
		lines("docs/keys.md", store.SideHead, 24, 25),
		lines("internal/review/carry.go", store.SideBase, 1, 21),
		lines("internal/review/reviewed.go", store.SideHead, 12, 13),
		lines("internal/review/reviewed.go", store.SideBase, 12, 12),
		lines("README.md", store.SideHead, 20, 20),
	}
}

func lines(path string, side store.Side, start, end int) store.ReviewedRange {
	return store.ReviewedRange{
		Path:      path,
		Side:      side,
		LineRange: store.LineRange{Start: start, End: end},
		CreatedAt: created,
	}
}

// whole is the row a file with no hunks takes, which is the only mark a binary file can hold.
func whole(path string, side store.Side) store.ReviewedRange {
	return store.ReviewedRange{Path: path, Side: side, CreatedAt: created}
}

// cut names the file a refresh took reviewed lines off, which is the state a diff viewer cannot show.
func cut() map[string]bool {
	return map[string]bool{"internal/tui/paint/rows.go": true}
}

const summary = "Preview reads the file behind the hunks. The tab expansion in paint is the part " +
	"to look at twice: every column after a tab moves with it."

func comments() []store.Comment {
	const (
		keys     = "docs/keys.md"
		reviewed = "internal/review/reviewed.go"
		preview  = "internal/tui/diffpane/preview.go"
		rows     = "internal/tui/paint/rows.go"
	)

	return []store.Comment{
		comment("c1", keys, 24, 25, store.ScopeRange,
			"These two rows say the same thing twice. Keep p and cut v, which is in the moving table already."),
		comment("c2", reviewed, 16, 21, store.ScopeHunk,
			"A generation that is gone and a generation that is behind are different failures. "+
				"Good that they read differently now."),
		answered("c3", preview, 22, 34, store.ScopeRange,
			"A line removed on the base side has no New, so this keys every one of them to zero and the last wins.",
			"Keyed on New only for added and context lines now, and removed lines fall through to the hunk rows."),
		resolved("c4", rows, 22, 22, store.ScopeLine,
			"Four looks small for a Go file. Eight is what gofmt assumes."),
		orphaned("c5", rows, 71, 71, store.ScopeLine,
			"This clip runs on every row of the preview, which is the whole file."),
		comment("c6", "assets/preview.png", 0, 0, store.ScopeFile,
			"Worth checking the size of this before it lands in the README."),
	}
}

func comment(id, path string, start, end int, scope store.Scope, body string) store.Comment {
	return store.Comment{
		ID:                  id,
		SessionID:           sessionID,
		GenerationID:        generation().ID,
		CreatedGenerationID: generation().ID,
		Path:                path,
		Side:                store.SideHead,
		LineRange:           store.LineRange{Start: start, End: end},
		CreatedRange:        store.LineRange{Start: start, End: end},
		Scope:               scope,
		Body:                body,
		State:               store.CommentOpen,
		CreatedAt:           created,
		UpdatedAt:           created,
	}
}

func answered(id, path string, start, end int, scope store.Scope, body, response string) store.Comment {
	c := comment(id, path, start, end, scope, body)
	c.State, c.Response = store.CommentAddressed, response
	return c
}

func resolved(id, path string, start, end int, scope store.Scope, body string) store.Comment {
	c := comment(id, path, start, end, scope, body)
	c.State = store.CommentResolved
	return c
}

// orphaned keeps where the anchor was, which is what the card's "was" label reads off.
func orphaned(id, path string, start, end int, scope store.Scope, body string) store.Comment {
	c := comment(id, path, start, end, scope, body)
	c.State = store.CommentOrphaned
	c.LastPath, c.LastLine = path, start
	return c
}

// replaced is the code the answered comment was written against, which the card draws above the response.
func replaced() map[string][]string {
	return map[string][]string{
		"c3": {
			"\tchanged := make(map[int]diff.Line)",
			"\tfor _, h := range f.Hunks {",
			"\t\tfor _, l := range h.Lines {",
			"\t\t\tchanged[l.New] = l",
			"\t\t}",
			"\t}",
		},
	}
}

func candidates() review.BaseCandidates {
	return review.BaseCandidates{
		Local: []review.Candidate{
			{Branch: "main", SHA: "9f4c1d0a7b28e3516ad9c0f2e84b7351cc06de92", Ahead: 4},
			{Branch: "release/0.4", SHA: "2ad6f80b19c7e534da0b6f1937ce482a5d3b0c11", Ahead: 11},
			{Branch: "feature/base-picker", SHA: "7c05be3419d8a26f0b5e71c4d3902a8fe61b4d75", Ahead: 26},
		},
		Remote: []review.Candidate{
			{Branch: "origin/main", SHA: "9f4c1d0a7b28e3516ad9c0f2e84b7351cc06de92", Ahead: 4},
			{Branch: "origin/HEAD", SHA: "9f4c1d0a7b28e3516ad9c0f2e84b7351cc06de92", Ahead: 4},
		},
	}
}

// sideOf is the side a file has bytes on: the base for a deleted file, the head otherwise.
func sideOf(f diff.File) store.Side {
	if f.Status == diff.FileDeleted {
		return store.SideBase
	}
	return store.SideHead
}

// shaOf is the candidate's sha, or fallback for a ref typed into the box rather than picked.
func shaOf(ref, fallback string) string {
	all := candidates()
	for _, c := range append(all.Local, all.Remote...) {
		if c.Branch == ref {
			return c.SHA
		}
	}
	return fallback
}
