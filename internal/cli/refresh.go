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

// build refreshes, and says what a lost ref means rather than passing the
// plumbing up.
//
// ErrRefMoved means something else refreshed this session first, which is a
// normal Tuesday with a TUI open in the next pane. Nothing was written, so the
// answer is to run it again.
//
// Retrying here rather than asking would be wrong, not merely lazy. Refresh
// promises the loser of the swap writes no row at all, and an immediate second
// attempt clears the swap against the winner's own commit while the winner is
// still inserting its row, which is how two rows land in an order the ref does
// not agree with. A reader running it again seconds later is past that window.
func build(ctx context.Context, s *review.Session) (review.Generation, error) {
	g, err := s.Refresh(ctx)
	if errors.Is(err, git.ErrRefMoved) {
		return review.Generation{}, errors.New("another zen-review refreshed this session first, so nothing was built: run it again")
	}
	return g, err
}
