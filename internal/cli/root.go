// Package cli is the command surface. It is a thin shell over the engine below
// it: every question the TUI can answer has to be answerable here too, without a
// terminal attached.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/zen-review/zen-review/internal/diff"
	"github.com/zen-review/zen-review/internal/git"
	"github.com/zen-review/zen-review/internal/version"
)

// NewRoot builds the command. A bare invocation prints the changeset, and the TUI
// will open on the same thing.
func NewRoot() *cobra.Command {
	var baseRef string

	cmd := &cobra.Command{
		Use:   "zen-review",
		Short: "Review the changes on a branch, and remember what you reviewed",
		Long: "Review the changes on a branch, and remember what you reviewed.\n\n" +
			"One changeset: the merge base with the base branch, through the working\n" +
			"tree, untracked files included.",
		Version:      version.Version,
		SilenceUsage: true,

		// An explicit range gets its own session, so refusing it beats accepting and
		// ignoring it until sessions exist.
		Args: cobra.NoArgs,

		RunE: func(cmd *cobra.Command, _ []string) error {
			return overview(cmd.Context(), cmd.OutOrStdout(), baseRef)
		},
	}

	cmd.Flags().StringVar(&baseRef, "base", "", "ref to measure the changeset from (default origin/HEAD)")
	return cmd
}

// base is the ref that named the starting point, and the merge base the diff
// actually runs against.
type base struct {
	ref string
	sha string
}

func overview(ctx context.Context, out io.Writer, baseRef string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("finding the working directory: %w", err)
	}

	repo, err := git.Open(ctx, cwd)
	if err != nil {
		return err
	}

	from, err := resolveBase(ctx, repo, baseRef)
	if err != nil {
		return err
	}

	files, err := changeset(ctx, repo, from.sha)
	if err != nil {
		return err
	}
	return write(out, from, files)
}

// resolveBase settles what the changeset is measured from: the flag, or the
// branch the remote considers default.
//
// A base that sticks to a branch for days, and a picker for a stacked branch,
// move into review/ with the session that can remember the answer.
func resolveBase(ctx context.Context, repo *git.Repo, ref string) (base, error) {
	if ref == "" {
		found, err := repo.DefaultRemoteBranch(ctx)
		if err != nil {
			if errors.Is(err, git.ErrNoDefaultBranch) {
				return base{}, errors.New("no origin/HEAD to measure from, so pass --base <ref>")
			}
			return base{}, err
		}
		ref = found
	}

	sha, err := repo.MergeBase(ctx, ref, "HEAD")
	if err != nil {
		return base{}, err
	}
	return base{ref: ref, sha: sha}, nil
}

// changeset is the one thing zen-review opens: the merge base through the working
// tree, untracked files included.
//
// The tracked half is a single diff. Each untracked file needs its own, because
// git diff cannot see a file it has never been told about and
// `add --intent-to-add` would write to the index the agent is also using. A
// generation replaces both halves with one diff against its tree.
func changeset(ctx context.Context, repo *git.Repo, from string) ([]diff.File, error) {
	patch, err := repo.Diff(ctx, from)
	if err != nil {
		return nil, err
	}
	files := diff.Parse(patch)

	untracked, err := repo.Untracked(ctx)
	if err != nil {
		return nil, err
	}
	for _, path := range untracked {
		one, err := repo.DiffNoIndex(ctx, os.DevNull, path)
		if err != nil {
			// A path git will not diff is still a change the reader has to know
			// about, so it is listed with the reason rather than dropped.
			files = append(files, diff.File{Path: path, Status: diff.FileAdded, Omitted: "could not be diffed"})
			continue
		}
		files = append(files, diff.Parse(one)...)
	}

	// git orders a diff by path and the untracked half arrives after it, so the
	// whole list is sorted to keep one order rather than two.
	slices.SortStableFunc(files, func(a, b diff.File) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

// write prints the changeset. This is a smoke test for the plumbing rather than
// the product: the TUI takes the bare invocation, and `status` and `files` answer
// this in prose and in JSON.
func write(out io.Writer, from base, files []diff.File) error {
	if _, err := fmt.Fprintf(out, "base  %s (%s)\n", from.ref, short(from.sha)); err != nil {
		return err
	}
	if len(files) == 0 {
		_, err := fmt.Fprintf(out, "\nno changes since %s\n", from.ref)
		return err
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	hunks := 0
	for _, f := range files {
		hunks += len(f.Hunks)

		// The churn cell is left off rather than left empty, because tabwriter pads
		// every cell but the last and a padded empty one is a trailing space.
		cells := []string{letter(f.Status), name(f), extent(f)}
		if c := churn(f); c != "" {
			cells = append(cells, c)
		}
		if _, err := fmt.Fprintln(w, strings.Join(cells, "\t")); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}

	_, err := fmt.Fprintf(out, "\n%s, %s\n", plural(len(files), "file"), plural(hunks, "hunk"))
	return err
}

func letter(s diff.Status) string {
	switch s {
	case diff.FileAdded:
		return "A"
	case diff.FileDeleted:
		return "D"
	case diff.FileRenamed:
		return "R"
	case diff.FileCopied:
		return "C"
	default:
		return "M"
	}
}

// name shows a rename as both of its names, because the old one is how a reader
// knows which file this used to be.
func name(f diff.File) string {
	if f.OldPath == "" {
		return f.Path
	}
	return f.OldPath + " -> " + f.Path
}

func extent(f diff.File) string {
	if f.Omitted != "" {
		return f.Omitted
	}
	return plural(len(f.Hunks), "hunk")
}

func churn(f diff.File) string {
	if f.Omitted != "" {
		return ""
	}
	return fmt.Sprintf("+%d -%d", f.Additions, f.Deletions)
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func short(sha string) string {
	if len(sha) < 7 {
		return sha
	}
	return sha[:7]
}
