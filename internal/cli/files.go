package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/praxis-labs-io/zen-review/internal/review"
)

func newFiles(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "files",
		Short: "Report the changeset with what has been reviewed on it",
		Long: "Report the changeset with what has been reviewed on it.\n\n" +
			"A row per file and a row per hunk under it, each hunk named by the side\n" +
			"and line the review and unreview commands take. It builds nothing and\n" +
			"reads the generation already recorded, so run refresh first.",
		SilenceUsage: true,
		Args:         cobra.NoArgs,

		RunE: func(cmd *cobra.Command, _ []string) error {
			return runFiles(cmd, opts)
		},
	}
}

func runFiles(cmd *cobra.Command, opts *options) (err error) {
	s, err := open(cmd.Context(), opts)
	if err != nil {
		return err
	}
	defer func() { err = closing(err, s) }()

	st, err := s.Status(cmd.Context())
	if err != nil {
		return err
	}

	v, err := derive(cmd.Context(), s, st)
	if err != nil {
		return err
	}
	return emit(cmd.OutOrStdout(), v, opts.asJSON)
}

// derive reads the review on the generation the status reported.
//
// A session with no generation is reported rather than refused, the way a status
// is: the header says to run refresh and there is no changeset to say anything
// else about. Marking is the call that has to refuse, because there is nothing
// for a mark to anchor to.
func derive(ctx context.Context, s *review.Session, st review.Status) (changesetView, error) {
	v := changesetView{header: statusHeader(s, st)}
	if !st.Exists {
		return v, nil
	}

	c, err := s.Changeset(ctx, st.Generation)
	if err != nil {
		return changesetView{}, err
	}
	v.Changeset = c
	return v, nil
}
