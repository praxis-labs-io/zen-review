package review

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"time"

	"github.com/praxis-labs-io/zen-review/internal/diff"
	"github.com/praxis-labs-io/zen-review/internal/git"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

// refPrefix is outside refs/heads/, so no branch listing, push or checkout shows a session.
const refPrefix = "refs/zen-review/sessions/"

const maxFiles = 5000

// Generation is one snapshot of the changeset, committed under the session ref so its blobs can
// be diffed later, which git's in-memory shas for unstaged edits cannot.
type Generation struct {
	ID  int64
	Seq int

	CommitSha string
	BaseSha   string
	HeadSha   string
	CreatedAt time.Time

	// Skipped is the paths git could not read into the snapshot. It is not stored, so a generation
	// read back from the database has none.
	Skipped []string
}

type Status struct {
	SessionID string
	Kind      store.Kind
	Branch    string
	Base      Base

	// Generation is zero and Exists false on a session that has never refreshed.
	Generation Generation
	Exists     bool

	// Stale means the work tree or the base moved since Generation was built.
	Stale bool

	// Skipped describes the work tree now, not Generation, and is set even when none exists.
	Skipped []string

	Files []diff.File
}

type TooLargeError struct {
	Count int
	Limit int

	// Dir is the directory holding the most of them, InDir of them.
	Dir   string
	InDir int
}

func (e *TooLargeError) Error() string {
	if e.Dir == "" {
		return fmt.Sprintf("%d files is past the %d a review can be", e.Count, e.Limit)
	}
	return fmt.Sprintf("%d files is past the %d a review can be: %d of them are under %s, so gitignore it or measure from a nearer base",
		e.Count, e.Limit, e.InDir, e.Dir)
}

func tooLarge(files []string) *TooLargeError {
	dir, n := crowded(files)
	return &TooLargeError{Count: len(files), Limit: maxFiles, Dir: dir, InDir: n}
}

// Ref chains this session's generations. It can hold one commit the database has no row for,
// left by a crash or a lost race between the ref moving and the row landing.
func (s *Session) Ref() string { return refPrefix + s.row.ID }

// Refresh builds a generation when the changeset moved and returns the current one when not.
// It returns *TooLargeError having written nothing, and git.ErrRefMoved when another instance
// advanced the session first.
func (s *Session) Refresh(ctx context.Context) (Generation, error) {
	head, err := s.repo.Head(ctx)
	if err != nil {
		return Generation{}, err
	}
	if err := s.rebase(ctx, head); err != nil {
		return Generation{}, err
	}

	snap, err := s.snapshot(ctx)
	if err != nil {
		return Generation{}, err
	}

	latest, found, err := s.db.LatestGeneration(ctx, s.row.ID)
	if err != nil {
		return Generation{}, err
	}
	if found {
		holds, err := s.holds(ctx, latest, snap.Tree)
		if err != nil {
			return Generation{}, err
		}
		if holds {
			return generationOf(latest, snap.Skipped), nil
		}
	}

	if s.duringRefresh != nil {
		s.duringRefresh()
	}

	patch, err := s.repo.DiffTrees(ctx, s.base.SHA, snap.Tree)
	if err != nil {
		return Generation{}, err
	}
	files := diff.Parse(patch)
	if len(files) > maxFiles {
		return Generation{}, tooLarge(paths(files))
	}

	advance, err := s.carry(ctx, latest, found, snap.Tree, files)
	if err != nil {
		return Generation{}, err
	}

	old, hadRef, err := s.repo.RefSha(ctx, s.Ref())
	if err != nil {
		return Generation{}, err
	}

	now := time.Now().UTC().Truncate(time.Second)
	commit, err := s.repo.CommitTree(ctx, snap.Tree,
		s.parents(old, hadRef, latest, found),
		message(s.row.ID, s.base.SHA, head.SHA),
		git.Signature{Name: "zen-review", Email: "zen-review@invalid", When: now},
	)
	if err != nil {
		return Generation{}, err
	}

	if err := s.repo.UpdateRef(ctx, s.Ref(), commit, old); err != nil {
		return Generation{}, err
	}

	if s.afterSwap != nil {
		s.afterSwap()
	}

	row, err := s.db.AddGeneration(ctx, store.Generation{
		SessionID: s.row.ID,
		BaseSha:   s.base.SHA,
		HeadSha:   head.SHA,
		CommitSha: commit,
		CreatedAt: now,
	}, genFiles(files), advance)
	if err != nil {
		return Generation{}, errors.Join(lost(err), s.unwind(ctx, commit, old))
	}
	return generationOf(row, snap.Skipped), nil
}

func (s *Session) unwind(ctx context.Context, commit, old string) error {
	ctx = context.WithoutCancel(ctx)

	if err := s.repo.UpdateRef(ctx, s.Ref(), old, commit); err != nil &&
		!errors.Is(err, git.ErrRefMoved) {
		return err
	}
	return nil
}

func lost(err error) error {
	if !errors.Is(err, store.ErrStaleGeneration) {
		return err
	}
	return fmt.Errorf("the session advanced while this generation was being built: %w", git.ErrRefMoved)
}

// Status reports the session without building a generation, snapshotting the work tree to answer Stale.
func (s *Session) Status(ctx context.Context) (Status, error) {
	head, err := s.repo.Head(ctx)
	if err != nil {
		return Status{}, err
	}
	if err := s.rebase(ctx, head); err != nil {
		return Status{}, err
	}

	st := Status{
		SessionID: s.row.ID,
		Kind:      s.row.Kind,
		Branch:    s.row.Branch,
		Base:      s.base,
	}

	snap, err := s.snapshot(ctx)
	if err != nil {
		return Status{}, err
	}
	st.Skipped = snap.Skipped

	latest, found, err := s.db.LatestGeneration(ctx, s.row.ID)
	if err != nil {
		return Status{}, err
	}
	if !found {
		st.Stale = true
		return st, nil
	}

	holds, err := s.holds(ctx, latest, snap.Tree)
	if err != nil {
		return Status{}, err
	}

	st.Exists, st.Stale = true, !holds
	st.Generation = generationOf(latest, snap.Skipped)

	if st.Files, err = s.Files(ctx, st.Generation); err != nil {
		return Status{}, err
	}
	return st, nil
}

// Files is the diff g's tree makes against its base, in file-tree order.
func (s *Session) Files(ctx context.Context, g Generation) ([]diff.File, error) {
	patch, err := s.repo.DiffTrees(ctx, g.BaseSha, g.CommitSha)
	if err != nil {
		return nil, err
	}

	files := diff.Parse(patch)
	slices.SortFunc(files, func(a, b diff.File) int { return byTree(a.Path, b.Path) })
	return files, nil
}

// snapshot refuses before hashing, because `git add -A` writes every untracked file into the object store.
func (s *Session) snapshot(ctx context.Context) (git.Snapshot, error) {
	untracked, err := s.repo.Untracked(ctx)
	if err != nil {
		return git.Snapshot{}, err
	}
	if len(untracked) > maxFiles {
		return git.Snapshot{}, tooLarge(untracked)
	}
	return s.repo.SnapshotTree(ctx)
}

// holds ignores HEAD, since committing the work tree leaves the same bytes to review.
func (s *Session) holds(ctx context.Context, g store.Generation, tree string) (bool, error) {
	if g.BaseSha != s.base.SHA {
		return false, nil
	}
	had, err := s.repo.Tree(ctx, g.CommitSha)
	if err != nil {
		return false, err
	}
	return had == tree, nil
}

// parents pins the base unless the ref and the row agree it is already reachable, since a crash can part them.
func (s *Session) parents(old string, hadRef bool, latest store.Generation, found bool) []string {
	var parents []string
	if hadRef {
		parents = append(parents, old)
	}

	if s.base.EmptyTree() {
		return parents
	}
	if !hadRef || !found || latest.BaseSha != s.base.SHA {
		parents = append(parents, s.base.SHA)
	}
	return parents
}

func message(sessionID, base, head string) string {
	return fmt.Sprintf("zen-review generation\n\nsession %s\nbase    %s\nhead    %s\n", sessionID, base, head)
}

func genFiles(files []diff.File) []store.GenFile {
	rows := make([]store.GenFile, 0, len(files))
	for _, f := range files {
		rows = append(rows, store.GenFile{
			Path:     f.Path,
			OldPath:  f.OldPath,
			Status:   f.Status,
			BaseBlob: f.OldBlob,
			HeadBlob: f.NewBlob,
		})
	}
	return rows
}

func generationOf(row store.Generation, skipped []string) Generation {
	return Generation{
		ID:        row.ID,
		Seq:       row.Seq,
		CommitSha: row.CommitSha,
		BaseSha:   row.BaseSha,
		HeadSha:   row.HeadSha,
		CreatedAt: row.CreatedAt,
		Skipped:   skipped,
	}
}

func paths(files []diff.File) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

// crowded counts every ancestor and prefers the shortest, since node_modules, not a leaf under it, is the line to ignore.
func crowded(files []string) (string, int) {
	counts := make(map[string]int)
	for _, file := range files {
		for dir := path.Dir(file); dir != "." && dir != "/"; dir = path.Dir(dir) {
			counts[dir]++
		}
	}

	best, most := "", 0
	for dir, n := range counts {
		if n > most || (n == most && shorter(dir, best)) {
			best, most = dir, n
		}
	}
	return best, most
}

func shorter(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}
