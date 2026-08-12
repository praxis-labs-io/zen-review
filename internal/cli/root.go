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

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/version"
)

// options are the flags every command shares. They are persistent on the root,
// so a subcommand reads the same values a bare invocation does.
type options struct {
	baseRef string
	asJSON  bool
}

// NewRoot builds the command. A bare invocation refreshes and prints the
// changeset, and the TUI takes that slot when it lands.
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

		// An explicit range gets its own session, so refusing it beats accepting and
		// ignoring it until those exist. Cobra does not inherit this, so every
		// command below sets it too or the range walks in through a subcommand.
		Args: cobra.NoArgs,

		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRefresh(cmd, &opts)
		},
	}

	// The base sticks to the session, so the help says so rather than reading as
	// a flag that applies to this invocation alone.
	cmd.PersistentFlags().StringVar(&opts.baseRef, "base", "",
		"ref to measure the changeset from, kept until another is passed (default origin/HEAD)")
	cmd.PersistentFlags().BoolVar(&opts.asJSON, "json", false, "write the changeset as JSON")

	cmd.AddCommand(newStatus(&opts), newRefresh(&opts))
	return cmd
}

// open resolves the session for the working directory. The caller closes it.
func open(ctx context.Context, opts *options) (*review.Session, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("finding the working directory: %w", err)
	}
	return review.Open(ctx, cwd, review.Options{BaseRef: opts.baseRef})
}

// emit writes the view the way the flags asked for.
func emit(out io.Writer, v view, asJSON bool) error {
	if asJSON {
		return encode(out, v)
	}
	if _, err := io.WriteString(out, render(v)); err != nil {
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
