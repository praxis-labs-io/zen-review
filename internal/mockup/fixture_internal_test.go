package mockup

import (
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

// The patch and the bodies are two hand-kept fixtures of the same files. Every line the patch
// says is in the head has to be the line the body holds at that number, or p draws a file the
// hunks above it disagree with.
func TestThePatchAndTheBodiesAgree(t *testing.T) {
	text := bodies()

	for _, f := range files() {
		if f.Binary || f.Status == diff.FileDeleted {
			continue
		}

		body, held := text[f.Path]
		if !held {
			t.Errorf("%s has no body, so p shows nothing", f.Path)
			continue
		}

		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				if l.Kind == diff.Removed {
					continue
				}
				if l.New < 1 || l.New > len(body) {
					t.Errorf("%s line %d is in the patch and past the end of the %d-line body",
						f.Path, l.New, len(body))
					continue
				}
				if got := body[l.New-1]; got != l.Text {
					t.Errorf("%s line %d:\n patch %q\n  body %q", f.Path, l.New, l.Text, got)
				}
			}
		}
	}
}

// A deleted file is read on the base side, so its body is the text the patch removes.
func TestTheDeletedFileKeepsItsBaseSideBody(t *testing.T) {
	text := bodies()

	for _, f := range files() {
		if f.Status != diff.FileDeleted {
			continue
		}

		body, held := text[f.Path]
		if !held {
			t.Fatalf("%s has no body, so p shows nothing on the file that was deleted", f.Path)
		}
		if sideOf(f) != store.SideBase {
			t.Errorf("%s reads on the %s side, want the base", f.Path, sideOf(f))
		}

		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				if l.Old < 1 || l.Old > len(body) {
					t.Errorf("%s line %d is in the patch and past the end of the %d-line body",
						f.Path, l.Old, len(body))
					continue
				}
				if got := body[l.Old-1]; got != l.Text {
					t.Errorf("%s line %d:\n patch %q\n  body %q", f.Path, l.Old, l.Text, got)
				}
			}
		}
		return
	}
	t.Fatal("the fixture deletes no file, so a whole-file mark on the base side is never drawn")
}

func TestEveryBodyBelongsToAFileInThePatch(t *testing.T) {
	paths := make(map[string]bool)
	for _, f := range files() {
		paths[f.Path] = true
	}

	for path := range bodies() {
		if !paths[path] {
			t.Errorf("testdata/bodies/%s is in no hunk of the patch", path)
		}
	}
}

// The seeded ranges are what the tree draws before a key is pressed.
func TestTheSeededRangesNameFilesThePatchHolds(t *testing.T) {
	head, base := make(map[string]bool), make(map[string]bool)
	for _, f := range files() {
		head[f.Path] = true
		base[f.BasePath()] = true
	}

	for _, r := range reviewed() {
		held := head[r.Path]
		if r.Side == store.SideBase {
			held = base[r.Path]
		}
		if !held {
			t.Errorf("a reviewed range names %s on the %s side, which the patch does not hold",
				r.Path, r.Side)
		}
	}
}

// Every comment has to reach a file, or its card is drawn nowhere and the ring skips it.
func TestEveryCommentReachesAFile(t *testing.T) {
	files := files()

	for _, c := range comments() {
		found := false
		for _, f := range files {
			at := f.Path
			if c.Side == store.SideBase {
				at = f.BasePath()
			}
			if at == c.Path {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("comment %s is on %s, which the patch does not hold", c.ID, c.Path)
		}
	}
}

// The replaced block is drawn on a card, so it has to name a comment carrying a response.
func TestEveryReplacedBlockNamesAnAnsweredComment(t *testing.T) {
	answered := make(map[string]bool)
	for _, c := range comments() {
		if c.State == store.CommentAddressed || c.Response != "" {
			answered[c.ID] = true
		}
	}

	for id, block := range replaced() {
		if !answered[id] {
			t.Errorf("a replaced block names comment %s, which answers nothing", id)
		}
		if len(block) == 0 {
			t.Errorf("the replaced block for %s is empty", id)
		}
	}
}
