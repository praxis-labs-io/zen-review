package review_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/zen-review/zen-review/internal/git"
	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
)

// A write committed while a refresh is mid-flight used to be accepted and then
// lost. The refresh read the generation's state, did its git work, and wrote;
// anything landing between the read and the write went onto a generation the
// carry had already read past, where nothing would read it again. The write
// returned nil, so the caller reported success and the lines came back unread.
//
// The reads now happen inside the transaction that writes the generation, and
// every write asserts from inside its own transaction that the generation it
// names is still the latest. So a write in the window either moves forward with
// the refresh or is refused, and these drive that window on purpose.

// during runs one write inside the window, through a second session on the same
// repository, which is the shape two instances have and the shape the TUI has
// when a reload and a mark are in flight together.
func (f *fixture) during(s *review.Session, write func(*review.Session, review.Generation)) {
	f.t.Helper()

	var once sync.Once
	s.DuringRefresh(func() {
		once.Do(func() {
			other := f.mustOpen("")
			g, found := f.latest(other.ID())
			if !found {
				f.t.Error("the session has no generation to write against")
				return
			}
			write(other, handle(g))
		})
	})
	f.t.Cleanup(func() { s.DuringRefresh(nil) })
}

// handle is the generation a caller holds, built from the row. Callers get one
// off a status or a refresh, and a test reaching into the database for one is
// standing in for a caller that read it a moment before the window opened.
func handle(g store.Generation) review.Generation {
	return review.Generation{
		ID:        g.ID,
		Seq:       g.Seq,
		CommitSha: g.CommitSha,
		BaseSha:   g.BaseSha,
		HeadSha:   g.HeadSha,
		CreatedAt: g.CreatedAt,
	}
}

func TestAMarkCommittedDuringARefreshLandsInTheNewGeneration(t *testing.T) {
	f, s, first := marked(t)

	f.during(s, func(other *review.Session, g review.Generation) {
		f.mark(other, g, "code.txt", store.SideHead, review.Range{Start: 15, End: 16})
	})

	f.Write("code.txt", "inserted\n"+numbered(1, 20))
	next := f.refresh(s)

	if next.Seq == first.Seq {
		t.Fatal("the refresh built no new generation, so there was no window to land in")
	}
	// Both ranges shifted by the inserted line: the one read before the refresh
	// started, and the one written while it was running.
	assertRanges(t, f.storedRanges(next), []string{"code.txt head 6:10", "code.txt head 16:17"})
}

// A comment in the window is worse than a lost mark. It stays open, so the
// anchor it shows is a live one, and left behind it points at code nobody wrote
// it about.
func TestACommentWrittenDuringARefreshMovesWithTheCode(t *testing.T) {
	f, s, _ := marked(t)

	var written store.Comment
	f.during(s, func(other *review.Session, g review.Generation) {
		written = f.note(other, g,
			review.NoteOnLines("code.txt", store.SideHead, review.Range{Start: 15, End: 16}, "this one"))
	})

	f.Write("code.txt", "inserted\n"+numbered(1, 20))
	next := f.refresh(s)

	got := f.storedComment(written.ID)
	if got.GenerationID != next.ID {
		t.Errorf("generationID = %d, want the generation the refresh wrote, %d", got.GenerationID, next.ID)
	}
	if got.Start != 16 || got.End != 17 {
		t.Errorf("anchor = %d:%d, want it moved to 16:17 with the code", got.Start, got.End)
	}
	if got.State != store.CommentOpen {
		t.Errorf("state = %s, want it still open", got.State)
	}
}

// The mirror of the two above: a comment settled in the window has stopped
// moving, and a carry that read it as open a moment earlier would walk it onto a
// generation it never lived at.
func TestAResolveDuringARefreshStopsTheCommentMoving(t *testing.T) {
	f, s, g := marked(t)
	c := f.note(s, g, review.NoteOnLines("code.txt", store.SideHead, review.Range{Start: 15, End: 16}, "answered"))

	f.during(s, func(other *review.Session, _ review.Generation) {
		if _, err := other.ResolveComment(f.t.Context(), c.ID); err != nil {
			f.t.Errorf("resolving the comment: %v", err)
		}
	})

	f.Write("code.txt", "inserted\n"+numbered(1, 20))
	next := f.refresh(s)

	got := f.storedComment(c.ID)
	if got.State != store.CommentResolved {
		t.Errorf("state = %s, want resolved", got.State)
	}
	if got.GenerationID == next.ID {
		t.Errorf("generationID = %d, want it left at the generation it stopped moving on", got.GenerationID)
	}
	if got.Start != 15 || got.End != 16 {
		t.Errorf("anchor = %d:%d, want it left where it stopped", got.Start, got.End)
	}
}

// A whole refresh in the window is the case the ref swap cannot catch. This one
// read what it was carrying from generation one, another instance built two and
// took every open comment with it, and the swap still succeeds because the ref
// was read after it moved.
//
// Writing anyway would carry out of a generation two behind: everything written
// against the one in between is dropped, and the comments that moved onto it are
// left pinned to a generation nothing reads again. So it refuses, as the same
// lost race the swap reports.
func TestARefreshThatLosesTheSessionUnderItWritesNothing(t *testing.T) {
	f, s, first := marked(t)

	var once sync.Once
	s.DuringRefresh(func() {
		once.Do(func() { f.refresh(f.mustOpen("")) })
	})
	t.Cleanup(func() { s.DuringRefresh(nil) })

	f.Write("code.txt", "inserted\n"+numbered(1, 20))
	_, err := s.Refresh(t.Context())

	if !errors.Is(err, git.ErrRefMoved) {
		t.Fatalf("err = %v, want it to read as the lost race it is", err)
	}

	// Two: the one the fixture built, and the one the other instance built inside
	// the window. Never a third.
	latest, found := f.latest(s.ID())
	if !found || latest.Seq != first.Seq+1 {
		t.Fatalf("latest = %+v, want the generation the other instance wrote", latest)
	}
	// The other instance's own translation, untouched. This one refusing is what
	// leaves it standing.
	assertRanges(t, f.storedRanges(handle(latest)), []string{"code.txt head 6:10"})
}

// The same lost session on a fresh one, where there is no generation to name and
// so nothing that looked like a claim to check. Two instances build the first
// generation, the second takes the swap, and this one would write a second first
// generation carrying nothing out of the one it never saw.
func TestAFirstRefreshThatLosesTheSessionWritesNothing(t *testing.T) {
	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")
	s := f.mustOpen("")

	var once sync.Once
	s.DuringRefresh(func() {
		once.Do(func() {
			other := f.mustOpen("")
			g := f.refresh(other)
			f.mark(other, g, "code.txt", store.SideHead, review.Range{Start: 5, End: 9})
		})
	})
	t.Cleanup(func() { s.DuringRefresh(nil) })

	_, err := s.Refresh(t.Context())

	if !errors.Is(err, git.ErrRefMoved) {
		t.Fatalf("err = %v, want it to read as the lost race it is", err)
	}

	latest, found := f.latest(s.ID())
	if !found || latest.Seq != 1 {
		t.Fatalf("latest = %+v, want the one generation the other instance wrote", latest)
	}
	assertRanges(t, f.storedRanges(handle(latest)), []string{"code.txt head 5:9"})
}

// A carry moving an open anchor leaves the comment open, so a resolve reading it
// a moment earlier still wins the swap. Where it stopped is where the carry left
// it, not where the read found it.
func TestAResolveRecordsWhereARefreshLeftTheComment(t *testing.T) {
	f, s, _, c := commented(t)

	var once sync.Once
	s.BeforeFreeze(func() {
		once.Do(func() {
			f.Write("code.txt", "inserted\n"+numbered(1, 20))
			f.refresh(f.mustOpen(""))
		})
	})
	t.Cleanup(func() { s.BeforeFreeze(nil) })

	got, err := s.ResolveComment(t.Context(), c.ID)

	if err != nil {
		t.Fatalf("resolving the comment: %v", err)
	}
	// The comment was written on line 10 and the inserted line moved it to 11.
	if got.LastLine != 11 {
		t.Errorf("answered with last line %d, want 11, where the refresh left it", got.LastLine)
	}
	stored := f.storedComment(c.ID)
	if stored.State != store.CommentResolved {
		t.Errorf("state = %s, want resolved", stored.State)
	}
	if stored.LastPath != "code.txt" || stored.LastLine != 11 {
		t.Errorf("last known = %s:%d, want code.txt:11", stored.LastPath, stored.LastLine)
	}
}

// The reverse ordering of the case above: the refresh gets there first and the
// state change arrives with the comment already orphaned under it.
//
// Resolving an orphan is the reader's call either way, so the swap goes again
// against the state that is there rather than answering a legal resolve with a
// refusal about a state it accepts.
func TestAResolveGoesAgainWhenARefreshOrphansTheCommentUnderIt(t *testing.T) {
	f, s, _, c := commented(t)

	var once sync.Once
	s.BeforeFreeze(func() {
		once.Do(func() {
			f.Write("code.txt", numbered(1, 9)+"line 10 rewritten\n"+numbered(11, 20))
			f.refresh(f.mustOpen(""))
		})
	})
	t.Cleanup(func() { s.BeforeFreeze(nil) })

	got, err := s.ResolveComment(t.Context(), c.ID)

	if err != nil {
		t.Fatalf("resolving a comment a refresh orphaned underneath: %v", err)
	}
	if got.State != store.CommentResolved {
		t.Errorf("state = %s, want resolved", got.State)
	}
	assertComments(t, f.storedComments(s), []string{"code.txt head 10:10 resolved"})

	stored := f.storedComment(c.ID)
	if stored.LastPath != "code.txt" || stored.LastLine != 10 {
		t.Errorf("last known = %s:%d, want code.txt:10", stored.LastPath, stored.LastLine)
	}
}

// The same window under goroutines rather than a seam, which is what -race has
// something to say about. Both writes go in, because a mark and a comment are
// different transactions and only one of them is covered above.
//
// It asserts the invariant and not the winner, because both outcomes are
// correct: a write that comes back nil is at whatever generation is latest by
// then, and one that is refused named a generation the refresh moved past. Which
// one happens on a given pass is the scheduler's business, and a test demanding
// one of them would be a test that fails for no reason.
func TestWritesRacingARefreshLoseNothing(t *testing.T) {
	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	refresher, writer := f.mustOpen(""), f.mustOpen("")
	f.refresh(refresher)
	db := f.db()

	landed, refused := 0, 0
	for pass := range 8 {
		// Appended, so lines 1 to 20 keep their numbers through every generation
		// and a write against one of them is where it was left.
		line := pass + 1
		f.Write("code.txt", numbered(1, 20)+numbered(100, 100+pass))
		g := handle(latestOf(t, db, writer.ID()))

		var refresh, mark, write error
		var c store.Comment

		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			_, refresh = refresher.Refresh(t.Context())
		}()
		go func() {
			defer wg.Done()
			mark = writer.Mark(t.Context(), g, "code.txt", store.SideHead, []review.Range{{Start: line, End: line}})
		}()
		go func() {
			defer wg.Done()
			c, write = writer.AddComment(t.Context(), g,
				review.NoteOnLines("code.txt", store.SideHead, review.Range{Start: line, End: line}, "raced"))
		}()
		wg.Wait()

		if refresh != nil {
			t.Fatalf("pass %d: refreshing: %v", pass, refresh)
		}
		now := latestOf(t, db, writer.ID())

		for what, err := range map[string]error{"the mark": mark, "the comment": write} {
			switch {
			case err == nil:
				landed++
			case isStale(err):
				refused++
			default:
				t.Fatalf("pass %d: %s: %v", pass, what, err)
			}
		}

		if mark == nil && !covers(readAt(t, db, now.ID), line) {
			t.Fatalf("pass %d: the mark on line %d came back nil and is not at generation %d",
				pass, line, now.Seq)
		}
		if write == nil {
			if got := f.storedComment(c.ID); got.GenerationID != now.ID {
				t.Fatalf("pass %d: the comment came back nil and sits at generation %d, not the latest %d",
					pass, got.GenerationID, now.ID)
			}
		}
	}
	t.Logf("%d writes landed, %d were refused", landed, refused)
}

func isStale(err error) bool {
	var stale *review.StaleGenerationError
	return errors.As(err, &stale)
}

func latestOf(t *testing.T, db *store.DB, session string) store.Generation {
	t.Helper()

	g, found, err := db.LatestGeneration(t.Context(), session)
	if err != nil {
		t.Fatalf("reading the latest generation: %v", err)
	}
	if !found {
		t.Fatal("the session has no generation")
	}
	return g
}

func readAt(t *testing.T, db *store.DB, generationID int64) []store.ReviewedRange {
	t.Helper()

	rs, err := db.ReviewedRanges(t.Context(), generationID)
	if err != nil {
		t.Fatalf("reading the reviewed ranges of generation %d: %v", generationID, err)
	}
	return rs
}

// covers says the line reads reviewed, whatever the write merged it into. Marks
// on adjacent lines come back as one range, so naming the rows is the wrong
// question to ask across passes.
func covers(rs []store.ReviewedRange, line int) bool {
	for _, r := range rs {
		if r.Side == store.SideHead && r.Start <= line && line <= r.End {
			return true
		}
	}
	return false
}
