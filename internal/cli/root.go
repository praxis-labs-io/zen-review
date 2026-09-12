package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/version"
)

type options struct {
	baseRef string
	asJSON  bool
}

func NewRoot() *cobra.Command {
	var opts options

	cmd := &cobra.Command{
		Use:   "zen-review",
		Short: "Review the changes on a branch, and remember what you reviewed",
		Long: "Review the changes on a branch, and remember what you reviewed.\n\n" +
			"One changeset: the merge base with the base branch, through the working\n" +
			"tree, untracked files included.",
		Version:      version.Version,
		SilenceUsage: true,

		SilenceErrors: true,

		Args: cobra.NoArgs,

		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRoot(cmd, &opts)
		},
	}

	cmd.PersistentFlags().StringVar(&opts.baseRef, "base", "",
		"ref to measure the changeset from, kept until another is passed (detected when unset)")
	cmd.PersistentFlags().BoolVar(&opts.asJSON, "json", false, "write the changeset as JSON")

	cmd.AddCommand(
		newStatus(&opts), newRefresh(&opts), newFiles(&opts),
		newReview(&opts), newUnreview(&opts),
		newComment(&opts), newComments(&opts), newAddress(&opts), newResolve(&opts),
		newEdit(&opts), newDelete(&opts),
		newSummary(&opts), newExport(&opts),
	)
	return cmd
}

func refuseBase(cmd *cobra.Command) error {
	if !cmd.Flags().Changed("base") {
		return nil
	}
	return fmt.Errorf("the base is the session's, and %s does not take --base: "+
		"passing it here moves the base and keeps it moved, which is not what this call was about. "+
		"Change it with zen-review status --base <ref>", cmd.Name())
}

func refuseJSON(cmd *cobra.Command) error {
	if !cmd.Flags().Changed("json") {
		return nil
	}
	return fmt.Errorf("%s writes markdown and does not take --json: "+
		"the same comments in JSON are zen-review comments --json", cmd.Name())
}

func open(ctx context.Context, opts *options) (*review.Session, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("finding the working directory: %w", err)
	}
	return review.Open(ctx, cwd, review.Options{BaseRef: opts.baseRef})
}

type output interface {
	render() string
	encode(io.Writer) error
}

func emit(out io.Writer, v output, asJSON bool) error {
	if asJSON {
		return v.encode(out)
	}
	if _, err := io.WriteString(out, v.render()); err != nil {
		return err
	}
	return nil
}

func closing(err error, s *review.Session) error {
	return errors.Join(err, s.Close())
}
