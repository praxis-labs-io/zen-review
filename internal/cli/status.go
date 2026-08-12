package cli

import (
	"github.com/spf13/cobra"
)

func newStatus(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report the session without building a generation",
		Long: "Report the session without building a generation.\n\n" +
			"It writes no generation and does not move the session ref, but it does\n" +
			"read the working tree to answer whether anything has moved since the\n" +
			"last one was built. Passing --base changes the base this session keeps.",
		SilenceUsage: true,
		Args:         cobra.NoArgs,

		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd, opts)
		},
	}
}

func runStatus(cmd *cobra.Command, opts *options) (err error) {
	s, err := open(cmd.Context(), opts)
	if err != nil {
		return err
	}
	defer func() { err = closing(err, s) }()

	st, err := s.Status(cmd.Context())
	if err != nil {
		return err
	}
	return emit(cmd.OutOrStdout(), statusView(s, st), opts.asJSON)
}
