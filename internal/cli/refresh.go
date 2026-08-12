package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/zen-review/zen-review/internal/git"
	"github.com/zen-review/zen-review/internal/review"
)

func newRefresh(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Build a generation from the working tree",
		Long: "Build a generation from the working tree.\n\n" +
			"A generation is a snapshot of the whole changeset, written into git as a\n" +
			"real commit, so a comment always knows the exact bytes it was about.\n" +
			"Nothing is built when the changeset has not moved.",
		SilenceUsage: true,
		Args:         cobra.NoArgs,

		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRefresh(cmd, opts)
		},
	}
}

// runRefresh is what a bare zen-review runs too. One function rather than one
// body copied twice, so that when the TUI takes the bare invocation there is a
// caller to delete and nothing left to have drifted.
func runRefresh(cmd *cobra.Command, opts *options) (err error) {
	s, err := open(cmd.Context(), opts)
	if err != nil {
		return err
	}
	defer func() { err = closing(err, s) }()

	g, err := build(cmd.Context(), s)
	if err != nil {
		return err
	}

	files, err := s.Files(cmd.Context(), g)
	if err != nil {
		return err
	}
	return emit(cmd.OutOrStdout(), generationView(s, g, files), opts.asJSON)
}

// build refreshes, retrying once when another instance won the ref.
//
// ErrRefMoved means something else refreshed this session first, which is a
// normal Tuesday with a TUI open in the next pane. The loser of that swap wrote
// no row and no ref, so a second attempt starts from a clean state and either
// finds the work already done or does it. Handing the reader "the ref moved"
// would be plumbing for a condition they did not cause and cannot act on.
func build(ctx context.Context, s *review.Session) (review.Generation, error) {
	g, err := s.Refresh(ctx)
	if errors.Is(err, git.ErrRefMoved) {
		return s.Refresh(ctx)
	}
	return g, err
}
