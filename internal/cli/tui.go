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

	"github.com/praxis-labs-io/zen-review/internal/config"
	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/tui/app"
	"github.com/praxis-labs-io/zen-review/internal/update"
	"github.com/praxis-labs-io/zen-review/internal/version"
)

func runRoot(cmd *cobra.Command, opts *options) error {
	if !interactive(opts.asJSON, term.IsTerminal(os.Stdout.Fd())) {
		return runRefresh(cmd, opts)
	}
	return runTUI(cmd, opts)
}

func interactive(asJSON, isTTY bool) bool {
	return isTTY && !asJSON
}

type reloader struct {
	ctx context.Context
	s   *review.Session

	// Bubble Tea does not wait for a command it started, so a reload can still be in git after Run returns.
	mu sync.Mutex
	// A reload scheduled after close must not run git add -A for an answer nobody is left to read.
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
		return app.Reload{}, overtaken(err)
	}

	rel, err := r.at(g)
	return rel, overtaken(err)
}

func overtaken(err error) error {
	var stale *review.StaleGenerationError
	if errors.Is(err, errOvertaken) || errors.As(err, &stale) {
		return app.ErrOvertaken
	}
	return err
}

func (r *reloader) Candidates() (review.BaseCandidates, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.shut {
		return review.BaseCandidates{}, errors.New("the reader closed the session before the bases loaded")
	}
	return r.s.Candidates(r.ctx)
}

func (r *reloader) SetBase(ref string) (app.Reload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.shut {
		return app.Reload{}, errors.New("the reader closed the session before the base changed")
	}
	if err := r.s.SetBase(r.ctx, ref); err != nil {
		return app.Reload{}, err
	}
	g, err := build(r.ctx, r.s)
	if err != nil {
		return app.Reload{}, err
	}
	return r.at(g)
}

func (r *reloader) MarkHunk(g review.Generation, path string, h review.Hunk) (app.Reload, error) {
	return r.wrote(g, func() error { return r.s.MarkHunk(r.ctx, g, path, h) })
}

func (r *reloader) UnmarkHunk(g review.Generation, path string, h review.Hunk) (app.Reload, error) {
	return r.wrote(g, func() error { return r.s.UnmarkHunk(r.ctx, g, path, h) })
}

func (r *reloader) MarkFile(g review.Generation, f review.File) (app.Reload, error) {
	return r.wrote(g, func() error { return r.s.MarkFile(r.ctx, g, f) })
}

func (r *reloader) UnmarkFile(g review.Generation, f review.File) (app.Reload, error) {
	return r.wrote(g, func() error { return r.s.UnmarkFile(r.ctx, g, f) })
}

func (r *reloader) AddComment(g review.Generation, n review.Note) (app.Reload, error) {
	return r.wrote(g, func() error {
		_, err := r.s.AddComment(r.ctx, g, n)
		return err
	})
}

func (r *reloader) Reanchor(n review.Note, from, to review.Generation) (review.Note, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.shut {
		return review.Note{}, false, errors.New("the reader closed the session before the comment could move")
	}
	return r.s.Reanchor(r.ctx, n, from, to)
}

func (r *reloader) ResolveComment(g review.Generation, id string) (app.Reload, error) {
	return r.wrote(g, func() error {
		_, err := r.s.ResolveComment(r.ctx, id)
		return err
	})
}

func (r *reloader) EditComment(g review.Generation, id, body string) (app.Reload, error) {
	return r.wrote(g, func() error {
		_, err := r.s.EditComment(r.ctx, id, body)
		return err
	})
}

func (r *reloader) DeleteComment(g review.Generation, id string) (app.Reload, error) {
	return r.wrote(g, func() error {
		_, err := r.s.DeleteComment(r.ctx, id)
		return err
	})
}

func (r *reloader) Body(g review.Generation, path string) (review.Body, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.shut {
		return review.Body{}, errors.New("the reader closed the session before this read started")
	}
	return r.s.Body(r.ctx, g, path)
}

func (r *reloader) SetSummary(text string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.shut {
		return "", errors.New("the reader closed the session before this note started")
	}
	if err := r.s.SetSummary(r.ctx, text); err != nil {
		return "", err
	}
	return r.s.Summary(r.ctx)
}

func (r *reloader) wrote(g review.Generation, do func() error) (app.Reload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.shut {
		return app.Reload{}, errors.New("the reader closed the session before this write started")
	}
	if err := do(); err != nil {
		return app.Reload{}, err
	}

	rel, err := r.at(g)
	if err != nil {
		return app.Reload{}, fmt.Errorf("%w: %w", app.ErrSaved, err)
	}
	return rel, nil
}

func (r *reloader) at(g review.Generation) (app.Reload, error) {
	c, err := r.s.Changeset(r.ctx, g)
	if err != nil {
		return app.Reload{}, err
	}

	comments, err := r.s.CommentsAt(r.ctx, g)
	if err != nil {
		return app.Reload{}, err
	}

	replaced, err := r.s.Replaced(r.ctx, g, comments)
	if err != nil {
		return app.Reload{}, err
	}

	summary, err := r.s.Summary(r.ctx)
	if err != nil {
		return app.Reload{}, err
	}

	return app.Reload{
		Base:       r.s.Base(),
		Generation: g,
		Changeset:  c,
		Comments:   comments,
		Replaced:   replaced,
		Summary:    summary,
	}, nil
}

// Waits rather than cancels: a refresh cut between the ref swap and its row leaves the ref ahead of the review.
func (r *reloader) close(out io.Writer) error {
	if !r.mu.TryLock() {
		_, _ = fmt.Fprintln(out, "waiting for a refresh to finish")
		r.mu.Lock()
	}
	defer r.mu.Unlock()

	r.shut = true
	return r.s.Close()
}

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

	return app.Run(cmd.Context(), src, s.Repo(), r, launch())
}

func launch() app.Launch {
	cfg, err := config.Load()
	if err != nil {
		return app.Launch{Warning: fmt.Errorf("%w, so the release check is off", err)}
	}
	if !cfg.ChecksForUpdates() {
		return app.Launch{}
	}
	return app.Launch{Check: newerRelease}
}

func newerRelease(ctx context.Context) (string, error) {
	path, err := update.Path()
	if err != nil {
		return "", err
	}

	result, err := update.Check(ctx, update.Options{Current: version.Version, CachePath: path})
	if !result.Available {
		return "", err
	}
	return result.Latest, err
}
