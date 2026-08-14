package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
)

// target is what a mark applies to. Exactly one of the three ways of naming it
// is given, and side qualifies the two that name lines.
type target struct {
	hunk  int
	lines string
	all   bool

	side       string
	generation int
}

// writer is the pair of engine calls one direction needs. review and unreview
// differ by nothing else, so they are one command body with this switched.
type writer struct {
	lines func(context.Context, review.Generation, string, store.Side, []review.Range) error
	hunk  func(context.Context, review.Generation, string, review.Hunk) error
	file  func(context.Context, review.Generation, review.File) error
}

func newReview(opts *options) *cobra.Command {
	return newMark(opts, "review", "Record part of a file as reviewed",
		"Record part of a file as reviewed.\n\n"+
			"A hunk is named by the line it introduces first, which is what the files\n"+
			"command prints beside it. Marking a hunk marks every side it touches,\n"+
			"because the lines it removes are not lines it has.",
		func(s *review.Session) writer {
			return writer{lines: s.Mark, hunk: s.MarkHunk, file: s.MarkFile}
		})
}

func newUnreview(opts *options) *cobra.Command {
	return newMark(opts, "unreview", "Take part of a file back out of what was reviewed",
		"Take part of a file back out of what was reviewed.\n\n"+
			"It cuts a recorded range where the two overlap rather than dropping it\n"+
			"whole, and it settles any change the last refresh recorded against the\n"+
			"file: taking lines back by hand makes the coverage the reader's own.",
		func(s *review.Session) writer {
			return writer{lines: s.Unmark, hunk: s.UnmarkHunk, file: s.UnmarkFile}
		})
}

func newMark(opts *options, use, short, long string, direction func(*review.Session) writer) *cobra.Command {
	var t target

	cmd := &cobra.Command{
		Use:          use + " <path>",
		Short:        short,
		Long:         long,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := t.check(cmd); err != nil {
				return err
			}
			return runMark(cmd, opts, &t, args[0], direction)
		},
	}

	cmd.Flags().IntVar(&t.hunk, "hunk", 0, "mark the hunk named by this line")
	cmd.Flags().StringVar(&t.lines, "lines", "", "mark these lines, as A-B or a single A")
	cmd.Flags().BoolVar(&t.all, "all", false, "mark every hunk of the file, or the file itself when it has none")
	cmd.Flags().StringVar(&t.side, "side", string(store.SideHead),
		"which blob the lines are measured on, head or base")
	cmd.Flags().IntVar(&t.generation, "generation", 0,
		"refuse unless this is the generation the mark lands on")

	return cmd
}

// check reads the flags against each other, before anything opens a database.
//
// No message here opens on a flag name or a path. The error is printed with its
// first letter capitalised, which turns --hunk into --Hunk: a flag that does not
// exist, in the sentence explaining which ones do.
//
// The three ways of naming a target are refused together rather than ranked,
// because a call passing two of them meant one and the tool cannot tell which.
func (t *target) check(cmd *cobra.Command) error {
	// The base is the session's and it outlives this call. Moving it here would
	// recompute the changeset and then record a mark against the one it replaced.
	// Checked first, so a call passing it is told about it rather than about
	// whichever other flag it also got wrong.
	if cmd.Flags().Changed("base") {
		return fmt.Errorf("the base is the session's, and %s does not take --base: "+
			"it would move the base and then record a mark against the changeset that move recomputed. "+
			"Change it with zen-review status --base <ref>", cmd.Name())
	}

	named := 0
	for _, flag := range []string{"hunk", "lines", "all"} {
		if cmd.Flags().Changed(flag) {
			named++
		}
	}

	switch {
	case named == 0:
		return errors.New("nothing to mark: pass --hunk, --lines or --all")
	case named > 1:
		return errors.New("pass one of --hunk, --lines and --all: they name three different things to mark")
	}

	if t.all && cmd.Flags().Changed("side") {
		return errors.New("the side is not a choice under --all, which marks every side the file has")
	}
	return nil
}

func runMark(
	cmd *cobra.Command,
	opts *options,
	t *target,
	path string,
	direction func(*review.Session) writer,
) (err error) {
	ctx := cmd.Context()

	s, err := open(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { err = closing(err, s) }()

	st, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if !st.Exists {
		return errors.New("this session has no generation, so there is nothing to mark against: run zen-review refresh")
	}
	if t.generation != 0 && t.generation != st.Generation.Seq {
		return &review.StaleGenerationError{Seq: t.generation, Current: st.Generation.Seq}
	}

	before, err := s.Changeset(ctx, st.Generation)
	if err != nil {
		return err
	}
	if err := t.apply(ctx, direction(s), st.Generation, before, path); err != nil {
		return err
	}

	// Re-derived rather than adjusted in hand. The engine wrote the row and the
	// engine says what the row means, which is the same rule the TUI follows.
	v, err := derive(ctx, s, st)
	if err != nil {
		return err
	}
	return emit(cmd.OutOrStdout(), v, opts.asJSON)
}

// apply resolves the target against the changeset and writes it.
func (t *target) apply(
	ctx context.Context,
	w writer,
	g review.Generation,
	c review.Changeset,
	path string,
) error {
	f, found := c.File(path)
	if !found {
		return fmt.Errorf("no file of this changeset is called %s: run zen-review files for the ones there are", path)
	}

	if t.all {
		return w.file(ctx, g, f)
	}

	side, err := parseSide(t.side)
	if err != nil {
		return err
	}

	if t.lines != "" {
		r, err := parseLines(t.lines)
		if err != nil {
			return err
		}
		return w.lines(ctx, g, path, side, []review.Range{r})
	}

	h, found := c.Hunk(path, side, t.hunk)
	if !found {
		return fmt.Errorf("no hunk of %s is named %s %d: run zen-review files for the ones there are",
			path, side, t.hunk)
	}
	return w.hunk(ctx, g, path, h)
}

func parseSide(s string) (store.Side, error) {
	switch side := store.Side(s); side {
	case store.SideHead, store.SideBase:
		return side, nil
	default:
		return "", fmt.Errorf("the side is head or base, not %q", s)
	}
}

// parseLines reads A-B, or a bare A meaning A-A. A selection of one line is
// still a selection and nobody types 42-42.
//
// Line 0 is the file as a whole rather than a line in it, so it is refused here.
// --all is how a file with no lines to name gets marked.
func parseLines(s string) (review.Range, error) {
	malformed := fmt.Errorf("the lines to mark are A-B or a single A, not %q", s)

	first, last, split := strings.Cut(s, "-")
	if !split {
		last = first
	}

	start, err := strconv.Atoi(first)
	if err != nil {
		return review.Range{}, malformed
	}
	end, err := strconv.Atoi(last)
	if err != nil {
		return review.Range{}, malformed
	}

	switch {
	case start < 1:
		return review.Range{}, fmt.Errorf("line numbers start at 1, so %q names none: "+
			"--all marks a file that has no lines to name", s)
	case end < start:
		return review.Range{}, fmt.Errorf("the range %q ends before it starts", s)
	}
	return review.Range{Start: start, End: end}, nil
}
