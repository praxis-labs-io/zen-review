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

	"github.com/spf13/cobra"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/version"
)

// options are the flags every command shares. They are persistent on the root,
// so a subcommand reads the same values a bare invocation does.
type options struct {
	baseRef string
	asJSON  bool
}

// NewRoot builds the command. A bare invocation refreshes and opens the
// reader, or prints the changeset when there is no terminal to open it on.
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

		// Errors are rendered by the handler cmd/zen-review hands fang, which is
		// also what keeps the --exit-code sentinel from being printed as a failure.
		// Left to cobra, it writes "Error: the filter matched" to stderr of a
		// command that succeeded.
		SilenceErrors: true,

		// An explicit range gets its own session, so refusing it beats accepting and
		// ignoring it until those exist. Cobra does not inherit this, so every
		// command below sets it too or the range walks in through a subcommand.
		Args: cobra.NoArgs,

		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRoot(cmd, &opts)
		},
	}

	// The base sticks to the session, so the help says so rather than reading as
	// a flag that applies to this invocation alone.
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

// refuseBase turns away a write that also asked to move the base.
//
// --base does not apply to one invocation. Open writes it back, so it stays
// moved, and a write is not where that decision belongs. On a mark or a comment
// it is worse than a side effect: the move recomputes the changeset the write
// then anchors into. zen-review status --base is where it changes.
func refuseBase(cmd *cobra.Command) error {
	if !cmd.Flags().Changed("base") {
		return nil
	}
	return fmt.Errorf("the base is the session's, and %s does not take --base: "+
		"passing it here moves the base and keeps it moved, which is not what this call was about. "+
		"Change it with zen-review status --base <ref>", cmd.Name())
}

// refuseJSON turns away --json on a command that writes markdown.
//
// The flag is persistent, so it reaches every command whether or not the command
// has an answer for it. Honouring it here would mean a second wire shape saying
// what comments --json already says, and two answers to one question is how they
// come to disagree.
func refuseJSON(cmd *cobra.Command) error {
	if !cmd.Flags().Changed("json") {
		return nil
	}
	return fmt.Errorf("%s writes markdown and does not take --json: "+
		"the same comments in JSON are zen-review comments --json", cmd.Name())
}

// open resolves the session for the working directory. The caller closes it.
func open(ctx context.Context, opts *options) (*review.Session, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("finding the working directory: %w", err)
	}
	return review.Open(ctx, cwd, review.Options{BaseRef: opts.baseRef})
}

// output is a view that knows both ways of writing itself: the summary a status
// prints, and the changeset with the review derived on it.
type output interface {
	render() string
	encode(io.Writer) error
}

// emit writes the view the way the flags asked for.
func emit(out io.Writer, v output, asJSON bool) error {
	if asJSON {
		return v.encode(out)
	}
	if _, err := io.WriteString(out, v.render()); err != nil {
		return err
	}
	return nil
}

// closing joins the close error onto whatever the command was already
// returning. A session holds the database open, and dropping the error from
// letting it go would hide a failed write behind a clean exit.
func closing(err error, s *review.Session) error {
	return errors.Join(err, s.Close())
}
