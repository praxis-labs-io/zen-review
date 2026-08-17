package review

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"time"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/git"
	"github.com/zen-review/zen-review/internal/store"
)

// refPrefix is where a session's generation chain lives. Under refs/ rather
// than refs/heads/, so no branch listing, no push and no checkout ever shows it.
const refPrefix = "refs/zen-review/sessions/"

// maxFiles is where a changeset stops being something a person reviews and
// starts being a directory somebody forgot to ignore.
const maxFiles = 5000

// Generation is one snapshot of the changeset, written into git as a real
// commit under the session's ref. Its blob shas are objects that commit wrote,
// which is the whole reason it exists: the head-side shas git prints for an
// unstaged edit are computed in memory and cannot be diffed through later.
type Generation struct {
	// ID is what reviewed ranges and comments anchor to.
	ID  int64
	Seq int

	CommitSha string
	BaseSha   string
	HeadSha   string
	CreatedAt time.Time

	// Skipped names the paths git could not read into the snapshot. It comes
	// from the snapshot just taken and never from the database, which does not
	// store it. A caller that drops it presents an incomplete review as a whole
	// one.
	Skipped []string
}

// Status is the session as it stands. It writes no generation, no commit and
// does not move the ref.
//
// Open may already have written the session row before this runs, when the
// caller passed a base that differs from the stored one. That is the base
// changing, not the status reading, and it is the whole of what a --base flag
// on a read command does.
type Status struct {
	SessionID string
	Kind      store.Kind
	Branch    string
	Base      Base

	// Generation is the zero value and Exists is false on a session that has
	// never refreshed.
	Generation Generation
	Exists     bool

	// Stale says the work tree or the base has moved since the generation was
	// built, so Files below is what was reviewed rather than what is there now.
	Stale bool

	// Skipped names the paths git could not read into the snapshot taken to
	// answer Stale. It describes the work tree as it is now, not the generation
	// being reported, and it is filled in whether or not one exists: a session
	// with nothing built yet is exactly where a reader has no other way to find
	// out that a file is missing from what they are about to review.
	Skipped []string

	// Files is the changeset at Generation, and is empty when there is none.
	Files []diff.File
}

// TooLargeError means the changeset holds more files than a review can be.
type TooLargeError struct {
	Count int
	Limit int

	// Dir holds the most of them, and InDir is how many. Naming it is the
	// point: the fix is one line in .gitignore and this says which.
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

// tooLarge is the refusal, naming where the files came from.
func tooLarge(files []string) *TooLargeError {
	dir, n := crowded(files)
	return &TooLargeError{Count: len(files), Limit: maxFiles, Dir: dir, InDir: n}
}

// Ref is where this session's generations are chained. `git log --first-parent`
// on it walks the review.
//
// It can hold one commit the database has no row for. The ref moves before the
// row, so an instance that loses the session between the two leaves its commit
// behind, and so does a crash there. Nothing reviewed is lost either way: the
// next refresh parents on it, and a generation that was never recorded was never
// reviewed against.
func (s *Session) Ref() string { return refPrefix + s.row.ID }

// Refresh brings the session up to date, building a generation when the
// changeset moved and returning the current one when it did not.
//
// It can refuse: a changeset past the file ceiling returns *TooLargeError and
// writes nothing at all, and a session another instance advanced first returns
// git.ErrRefMoved.
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

	// From here to the write is the window a concurrent write lands in: what it
	// is carrying has been named and nothing has been read yet.
	if s.duringRefresh != nil {
		s.duringRefresh()
	}

	// Against the tree rather than a commit, so the ceiling refuses before a
	// commit, a ref or a row exists. The objects are already written by here,
	// which is what the check in snapshot is for.
	patch, err := s.repo.DiffTrees(ctx, s.base.SHA, snap.Tree)
	if err != nil {
		return Generation{}, err
	}
	files := diff.Parse(patch)
	if len(files) > maxFiles {
		return Generation{}, tooLarge(paths(files))
	}

	// The git half of the carry, before the swap rather than after. Every step
	// below this line has to be cheap, because the ref moves partway through them
	// and a failure after that leaves it ahead of the database. Two wasted tree
	// diffs on a lost race is the better trade.
	//
	// The translation this returns is not cheap in the same sense and does not
	// need to be: it is pure, and the store runs it inside the transaction that
	// writes the row.
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

	// The ref moves before the row, and the swap is against what the ref itself
	// held rather than against the last commit_sha in the database. Two
	// instances refreshing one session both build, one wins the swap, and the
	// loser writes no row at all. The other order lets both write rows and
	// leaves the ref pointing at one of them.
	//
	// A crash between the two leaves the ref one commit ahead of the database.
	// The next refresh parents on it and carries on, and nothing reviewed is
	// lost, because a generation that was never recorded was never reviewed
	// against.
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

// unwind takes this refresh's commit back off the ref, the row it swapped for
// having not landed. An empty old is the ref this refresh created.
func (s *Session) unwind(ctx context.Context, commit, old string) error {
	// Past the cancel, that being one of the ways the row does not land and the
	// one that would otherwise leave the ref ahead on every quit.
	ctx = context.WithoutCancel(ctx)

	// A third instance already past us keeps the ref, leaving the state a crash
	// here leaves, which Ref documents and the next refresh carries on from.
	if err := s.repo.UpdateRef(ctx, s.Ref(), old, commit); err != nil &&
		!errors.Is(err, git.ErrRefMoved) {
		return err
	}
	return nil
}

// lost is the generation write refusing because the session advanced under it.
//
// It reads its own swap the way the ref swap reads its own: this instance is
// carrying out of a generation that is no longer the tip, so what it built
// describes a session two steps back and nothing it says is still true. The
// answer either way is to run it again, so it arrives as the error callers
// already have that sentence for.
func lost(err error) error {
	if !errors.Is(err, store.ErrStaleGeneration) {
		return err
	}
	return fmt.Errorf("the session advanced while this generation was being built: %w", git.ErrRefMoved)
}

// Status reports the session without touching it. It snapshots the work tree to
// answer Stale, which costs what a refresh costs minus the commit, and that is
// the honest price of an answer about what is on disk right now.
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
		// Nothing has been reviewed against, so everything on disk is unseen.
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

// Files is the changeset at a generation: the diff its tree makes against the
// base, parsed, in the order a file tree reads.
//
// Nothing about hunks is stored. This is the same parse a remap runs through,
// and one derivation beats a stored count that can disagree with it.
func (s *Session) Files(ctx context.Context, g Generation) ([]diff.File, error) {
	patch, err := s.repo.DiffTrees(ctx, g.BaseSha, g.CommitSha)
	if err != nil {
		return nil, err
	}

	files := diff.Parse(patch)
	slices.SortFunc(files, func(a, b diff.File) int { return byTree(a.Path, b.Path) })
	return files, nil
}

// snapshot writes the work tree into a tree object, refusing first if it is
// carrying more untracked files than a review can be.
//
// The refusal has to come before the snapshot and not only after the diff.
// SnapshotTree runs `git add -A`, which hashes every untracked file into the
// object store, so a checkout with an unignored node_modules in it pays for the
// whole directory on every invocation and leaves the objects there, reachable
// from nothing, for gc's prune window to hold. Counting the changeset
// afterwards refuses the review and keeps the bill.
//
// The tree is the work tree and not HEAD. `add -A` reconciles the index to what
// is on disk, so what HEAD was seeded from does not decide the contents, and
// head_sha is the branch tip at the time rather than a claim about the tree.
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

// holds says a generation still describes what is on disk: same base, same tree.
//
// Without this every status would write a commit. HEAD moving is deliberately
// not part of it: committing what was already in the work tree leaves the same
// bytes to review, and a generation per commit would be a generation per
// nothing.
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

// parents chain the generation, and keep the base reachable.
//
// The chain comes off the ref and the base comes off the database, and the two
// can disagree: a crash between the swap and the insert leaves a generation the
// ref holds and no row describing it. The base is therefore pinned unless the
// commit being hung off is known to reach it already, which takes both sources
// agreeing. Deciding from the row alone leaves the base unpinned in exactly the
// window the swap ordering above creates.
//
// A parent that is already reachable costs nothing, and `git log --first-parent`
// still walks generations alone.
func (s *Session) parents(old string, hadRef bool, latest store.Generation, found bool) []string {
	var parents []string
	if hadRef {
		parents = append(parents, old)
	}

	// The empty tree is reachable from everywhere and is not a commit, so there
	// is nothing here to pin and no way to pin it.
	if s.base.EmptyTree() {
		return parents
	}
	if !hadRef || !found || latest.BaseSha != s.base.SHA {
		parents = append(parents, s.base.SHA)
	}
	return parents
}

// message is what `git log` on the session ref shows.
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

// paths is the path of each file, which is all the ceiling needs.
func paths(files []diff.File) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

// crowded is the directory holding the most of these paths, and how many.
//
// Every ancestor is counted, not just the immediate parent: a build directory
// spreads its files over hundreds of leaves and no one of them is the line
// worth adding to .gitignore. Ties go to the shortest path for the same reason,
// because node_modules and node_modules/.pnpm hold the same count and only the
// first is worth saying.
//
// Paths come from git and are always slash-separated, so this is path and not
// filepath.
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

// shorter breaks a tie, by length and then by name so the answer does not
// depend on map order.
func shorter(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}
