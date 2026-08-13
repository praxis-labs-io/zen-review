package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/review"
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

// reloader is what the reader's reload key reaches: one refresh, and the
// changeset that came out of it.
//
// It holds the context because it is the handle the key is bound to and its
// whole life is that one program, so a git call started by a key dies when the
// program does rather than outliving it.
type reloader struct {
	ctx context.Context
	s   *review.Session

	// mu holds the session while one call is using it. The reader guarantees it
	// asks for one reload at a time, but not that it has stopped asking before
	// the program ends: Bubble Tea does not wait for a command it started, so
	// Run returns with one still in git. Closing the database between the ref
	// swap and the row that records it would leave the ref a generation ahead of
	// the review.
	mu sync.Mutex
}

func (r *reloader) Reload() (app.Reload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	g, err := build(r.ctx, r.s)
	if err != nil {
		return app.Reload{}, err
	}

	c, err := r.s.Changeset(r.ctx, g)
	if err != nil {
		return app.Reload{}, err
	}
	return app.Reload{Base: r.s.Base(), Generation: g, Changeset: c}, nil
}

// close releases the session, waiting on a reload still running.
func (r *reloader) close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.s.Close()
}

// runTUI refreshes and opens the reader on what came back.
//
// The opening changeset is the first reload, so the reader and the key it
// answers to take the same path.
func runTUI(cmd *cobra.Command, opts *options) (err error) {
	s, err := open(cmd.Context(), opts)
	if err != nil {
		return err
	}

	src := &reloader{ctx: cmd.Context(), s: s}
	defer func() { err = errors.Join(err, src.close()) }()

	r, err := src.Reload()
	if err != nil {
		return err
	}

	t, ok := theme.Get(theme.Default)
	if !ok {
		return fmt.Errorf("zen-kit has no theme named %s", theme.Default)
	}
	return app.Run(cmd.Context(), t, src, s.Repo(), r)
}
