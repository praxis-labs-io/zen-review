package review_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/git"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

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
	assertRanges(t, f.storedRanges(next), []string{"code.txt head 6:10", "code.txt head 16:17"})
}

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

	latest, found := f.latest(s.ID())
	if !found || latest.Seq != first.Seq+1 {
		t.Fatalf("latest = %+v, want the generation the other instance wrote", latest)
	}
	assertRanges(t, f.storedRanges(handle(latest)), []string{"code.txt head 6:10"})
}

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

func TestWritesRacingARefreshLoseNothing(t *testing.T) {
	f := branched(t)
	f.Write("code.txt", numbered(1, 20))
	f.Commit("add code")

	refresher, writer := f.mustOpen(""), f.mustOpen("")
	f.refresh(refresher)
	db := f.db()

	landed, refused := 0, 0
	for pass := range 8 {
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

func covers(rs []store.ReviewedRange, line int) bool {
	for _, r := range rs {
		if r.Side == store.SideHead && r.Start <= line && line <= r.End {
			return true
		}
	}
	return false
}
