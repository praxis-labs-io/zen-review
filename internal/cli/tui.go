package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	//
	// shut is the other end of the same gap. A command queued but not yet
	// scheduled runs after close has taken the mutex and let it go, and a refresh
	// there would run `git add -A` over the work tree for an answer nobody is
	// left to read.
	mu   sync.Mutex
	shut bool
}

func (r *reloader) Reload() (app.Reload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.shut {
		return app.Reload{}, errors.New("the reader closed the session before this reload started")
	}

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

// close releases the session, waiting on a reload still running and refusing
// any that had not started.
//
// The wait happens with the reader already off the screen, so it says what it
// is doing. `git add -A` over a large work tree takes long enough that a shell
// sitting silent reads as a hang, and the context the reload holds is the
// program's rather than the keypress's: quitting does not cancel it, because a
// refresh cancelled between the ref swap and the row that records it is the one
// outcome this waits to avoid.
func (r *reloader) close(out io.Writer) error {
	if !r.mu.TryLock() {
		// Discarded: the session still has to close, and a stderr that cannot
		// take a line has nowhere to report that it could not.
		_, _ = fmt.Fprintln(out, "waiting for a refresh to finish")
		r.mu.Lock()
	}
	defer r.mu.Unlock()

	r.shut = true
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
	defer func() { err = errors.Join(err, src.close(cmd.ErrOrStderr())) }()

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
