package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/praxis-labs-io/zen-review/internal/git"
	"github.com/praxis-labs-io/zen-review/internal/review"
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

// No retry on ErrRefMoved: an immediate second swap can land two rows in an order the ref disagrees with.
func build(ctx context.Context, s *review.Session) (review.Generation, error) {
	g, err := s.Refresh(ctx)
	if errors.Is(err, git.ErrRefMoved) {
		return review.Generation{}, errors.New("another zen-review refreshed this session first, so nothing was built: run it again")
	}
	return g, err
}
