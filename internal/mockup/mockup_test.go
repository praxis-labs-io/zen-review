package mockup_test

import (
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/mockup"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
	"github.com/praxis-labs-io/zen-review/internal/tui/app"
)

const (
	binary  = "assets/preview.png"
	keys    = "docs/keys.md"
	carry   = "internal/review/carry.go"
	marked  = "internal/review/reviewed.go"
	preview = "internal/tui/diffpane/preview.go"
	rows    = "internal/tui/paint/rows.go"
)

func TestTheFixtureOpensPartlyRead(t *testing.T) {
	r := opened(t)

	want := map[string]review.State{
		binary:      review.Reviewed,
		keys:        review.Reviewed,
		carry:       review.Reviewed,
		marked:      review.Partial,
		preview:     review.Unreviewed,
		rows:        review.Unreviewed,
		"README.md": review.Reviewed,
	}

	if len(r.Changeset.Files) != len(want) {
		t.Fatalf("the fixture holds %d files, want %d", len(r.Changeset.Files), len(want))
	}
	for _, f := range r.Changeset.Files {
		if got := f.State; got != want[f.Diff.Path] {
			t.Errorf("%s reads %s, want %s", f.Diff.Path, got, want[f.Diff.Path])
		}
	}

	if r.Changeset.Reviewed == 0 || r.Changeset.Reviewed == r.Changeset.Items {
		t.Errorf("the burn-down is %d/%d, want it part-way so the tree draws every state",
			r.Changeset.Reviewed, r.Changeset.Items)
	}
	if r.Summary == "" {
		t.Error("the fixture carries no session note")
	}
	if len(r.Replaced) == 0 {
		t.Error("the fixture carries no replaced block, so no card draws what a response answered")
	}
}

// The reader opens on the first unread stop, which is what the first screenshot shows.
func TestTheFixtureOpensOnAHunkOfCode(t *testing.T) {
	r := opened(t)

	for _, f := range r.Changeset.Files {
		if f.State == review.Reviewed {
			continue
		}
		if f.Diff.Path != marked {
			t.Errorf("the reader opens on %s, want %s", f.Diff.Path, marked)
		}
		if len(f.Hunks) == 0 {
			t.Errorf("%s has no hunks, so the reader opens on a file with nothing to draw", f.Diff.Path)
		}
		return
	}
	t.Fatal("every file reads reviewed, so the reader opens on nothing")
}

func TestMarkingAHunkSticks(t *testing.T) {
	m := mockup.New()
	h := hunkOf(t, opened(t), preview, 0)

	r, err := m.MarkHunk(generationOf(t, m), preview, h)
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, r, preview); got != review.Reviewed {
		t.Errorf("%s reads %s after the mark, want reviewed", preview, got)
	}

	r, err = m.UnmarkHunk(generationOf(t, m), preview, h)
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, r, preview); got != review.Unreviewed {
		t.Errorf("%s reads %s after the mark came off, want unreviewed", preview, got)
	}
}

// The partial file is the case exact-row bookkeeping would get wrong: its seeded ranges are part
// of a hunk's anchors, so taking the file's mark back has to cut into them.
func TestMarkingAFileTakesInWhatWasAlreadyRead(t *testing.T) {
	m := mockup.New()
	f := fileOf(t, opened(t), marked)

	r, err := m.MarkFile(generationOf(t, m), f)
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, r, marked); got != review.Reviewed {
		t.Errorf("%s reads %s after the file was marked, want reviewed", marked, got)
	}

	r, err = m.UnmarkFile(generationOf(t, m), f)
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, r, marked); got != review.Unreviewed {
		t.Errorf("%s reads %s after the file's mark came off, want unreviewed", marked, got)
	}
}

func TestMarkingADeletedFileLandsOnTheBaseSide(t *testing.T) {
	m := mockup.New()
	f := fileOf(t, opened(t), carry)

	r, err := m.UnmarkFile(generationOf(t, m), f)
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, r, carry); got != review.Unreviewed {
		t.Errorf("%s reads %s after the mark came off, want unreviewed", carry, got)
	}

	r, err = m.MarkFile(generationOf(t, m), f)
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, r, carry); got != review.Reviewed {
		t.Errorf("%s reads %s after the mark went back on, want reviewed", carry, got)
	}
}

func TestACommentIsWrittenEditedResolvedAndDeleted(t *testing.T) {
	m := mockup.New()
	g := generationOf(t, m)
	was := len(opened(t).Comments)

	r, err := m.AddComment(g, review.NoteOnLines(rows, store.SideHead, review.Range{Start: 22, End: 22},
		"tabWidth belongs beside the row, not in the package"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Comments) != was+1 {
		t.Fatalf("the fixture holds %d comments after one was written, want %d", len(r.Comments), was+1)
	}

	id := r.Comments[len(r.Comments)-1].ID

	r, err = m.EditComment(g, id, "tabWidth belongs beside the row")
	if err != nil {
		t.Fatal(err)
	}
	if got := commentOf(t, r, id).Body; got != "tabWidth belongs beside the row" {
		t.Errorf("the comment reads %q after the edit", got)
	}

	r, err = m.ResolveComment(g, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := commentOf(t, r, id).State; got != store.CommentResolved {
		t.Errorf("the comment is %s after being resolved, want resolved", got)
	}

	r, err = m.DeleteComment(g, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Comments {
		if c.ID == id {
			t.Fatalf("comment %s survived the delete", id)
		}
	}

	if _, err := m.DeleteComment(g, id); err == nil {
		t.Error("deleting a comment that is gone went ahead")
	}
}

func TestTheSessionNoteIsKept(t *testing.T) {
	m := mockup.New()

	stored, err := m.SetSummary("Read the tab expansion twice.")
	if err != nil {
		t.Fatal(err)
	}
	if stored != "Read the tab expansion twice." {
		t.Errorf("the note came back as %q", stored)
	}

	r, err := m.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary != stored {
		t.Errorf("the reload carries %q, want the note that was just written", r.Summary)
	}
}

// A refresh reports no news, so a demo does not reshuffle between screenshots.
func TestAReloadStaysAtTheGenerationItOpenedAt(t *testing.T) {
	m := mockup.New()

	first, err := m.Reload()
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if again.Generation.ID != first.Generation.ID {
		t.Errorf("a reload moved the fixture to generation %d from %d",
			again.Generation.Seq, first.Generation.Seq)
	}
}

func TestABodyIsTheWholeFileOnTheSideItHasBytesOn(t *testing.T) {
	m := mockup.New()
	g := generationOf(t, m)

	tests := []struct {
		path string
		side store.Side
		head string
	}{
		{preview, store.SideHead, "package diffpane"},
		{carry, store.SideBase, "package review"},
		{"README.md", store.SideHead, "# zen-review"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			b, err := m.Body(g, tt.path)
			if err != nil {
				t.Fatal(err)
			}
			if b.Side != tt.side {
				t.Errorf("the body came back on the %s side, want %s", b.Side, tt.side)
			}
			if len(b.Lines) == 0 {
				t.Fatalf("%s has no body, so p shows nothing", tt.path)
			}
			if b.Lines[0] != tt.head {
				t.Errorf("the body opens %q, want %q", b.Lines[0], tt.head)
			}
		})
	}

	if _, err := m.Body(g, "no/such/file.go"); err == nil {
		t.Error("a body was read for a file the fixture does not hold")
	}
}

func TestSettingTheBaseMovesTheRef(t *testing.T) {
	m := mockup.New()

	cs, err := m.Candidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Local) == 0 || len(cs.Remote) == 0 {
		t.Fatalf("the picker opens on %d local and %d remote branches", len(cs.Local), len(cs.Remote))
	}

	pick := cs.Local[1]
	r, err := m.SetBase(pick.Branch)
	if err != nil {
		t.Fatal(err)
	}
	if r.Base.Ref != pick.Branch || r.Base.SHA != pick.SHA {
		t.Errorf("the base is %+v, want %s at %s", r.Base, pick.Branch, pick.SHA)
	}
}

func opened(t *testing.T) app.Reload {
	t.Helper()

	r, err := mockup.New().Reload()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func generationOf(t *testing.T, m *mockup.Mock) review.Generation {
	t.Helper()

	r, err := m.Reload()
	if err != nil {
		t.Fatal(err)
	}
	return r.Generation
}

func fileOf(t *testing.T, r app.Reload, path string) review.File {
	t.Helper()

	f, found := r.Changeset.File(path)
	if !found {
		t.Fatalf("the fixture holds no %s", path)
	}
	return f
}

func hunkOf(t *testing.T, r app.Reload, path string, nth int) review.Hunk {
	t.Helper()

	f := fileOf(t, r, path)
	if nth >= len(f.Hunks) {
		t.Fatalf("%s holds %d hunks, want at least %d", path, len(f.Hunks), nth+1)
	}
	return f.Hunks[nth]
}

func stateOf(t *testing.T, r app.Reload, path string) review.State {
	t.Helper()
	return fileOf(t, r, path).State
}

func commentOf(t *testing.T, r app.Reload, id string) store.Comment {
	t.Helper()

	for _, c := range r.Comments {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("the fixture holds no comment %s", id)
	return store.Comment{}
}
