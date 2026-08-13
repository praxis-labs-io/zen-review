package cli

import (
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/tui/app"
)

// runRoot is a bare zen-review: the reader on a terminal, the printed
// changeset anywhere else.
func runRoot(cmd *cobra.Command, opts *options) error {
	if !interactive(opts.asJSON, term.IsTerminal(os.Stdout.Fd())) {
		return runRefresh(cmd, opts)
	}
	return runTUI(cmd, opts)
}

// interactive is whether a bare invocation opens the reader.
//
// A pipe and a CI runner get the printed changeset, because a program waiting
// for keys on the other end of a pipe is a program that hangs. --json is a
// request for the wire shape and answers before the terminal does.
//
// It takes the answer rather than asking, so the decision is testable without
// a terminal to attach.
func interactive(asJSON, isTTY bool) bool {
	return isTTY && !asJSON
}

// runTUI refreshes and opens the reader on what came back.
func runTUI(cmd *cobra.Command, opts *options) (err error) {
	s, err := open(cmd.Context(), opts)
	if err != nil {
		return err
	}
	defer func() { err = closing(err, s) }()

	g, err := build(cmd.Context(), s)
	if err != nil {
		return err
	}

	c, err := s.Changeset(cmd.Context(), g)
	if err != nil {
		return err
	}

	t, ok := theme.Get(theme.Default)
	if !ok {
		return fmt.Errorf("zen-kit has no theme named %s", theme.Default)
	}
	return app.Run(cmd.Context(), t, s.Base(), g, c)
}
