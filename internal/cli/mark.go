package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

// aim is which of the three ways of naming a target was used. It is settled in
// check, so apply dispatches on the flag that was passed rather than on the
// value it carries: --lines= is a flag that was passed, and reading it back as
// an empty string sends the call down the --hunk branch to be refused for a
// flag nobody typed.
type aim string

const (
	aimHunk  aim = "hunk"
	aimLines aim = "lines"
	aimAll   aim = "all"
)

// target is what a mark applies to. Exactly one of the three ways of naming it
// is given, and side qualifies the two that name lines.
type target struct {
	aim   aim
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
	// Checked first, so a call passing --base is told about it rather than about
	// whichever other flag it also got wrong.
	if err := refuseBase(cmd); err != nil {
		return err
	}

	// --all is read by its value and the other two by whether they were passed.
	// A bool flag is set either way it is spelled, so --all=false counts as
	// passed while saying the opposite, and there is nothing else it could mean.
	named := 0
	if cmd.Flags().Changed("hunk") {
		named, t.aim = named+1, aimHunk
	}
	if cmd.Flags().Changed("lines") {
		named, t.aim = named+1, aimLines
	}
	if t.all {
		named, t.aim = named+1, aimAll
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
	// Read by whether it was passed, not by whether it is zero. Generation 0 is
	// not a generation, so a sentinel would let the one value that can never be
	// current through as though the flag had been left off.
	if cmd.Flags().Changed("generation") && t.generation != st.Generation.Seq {
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

	if t.aim == aimAll {
		return w.file(ctx, g, f)
	}

	side, err := parseSide(t.side)
	if err != nil {
		return err
	}

	if t.aim == aimLines {
		r, err := parseLines(t.lines, "--all")
		if err != nil {
			return err
		}
		rs, err := clip(f, side, r)
		if err != nil {
			return err
		}
		return w.lines(ctx, g, path, side, rs)
	}

	h, found := c.Hunk(path, side, t.hunk)
	if !found {
		return fmt.Errorf("no hunk of %s is named %s %d: run zen-review files for the ones there are",
			path, side, t.hunk)
	}
	return w.hunk(ctx, g, path, h)
}

// clip narrows the lines a reader named to the ones the file's hunks hold on
// that side.
//
// Coverage is only ever read against an anchor, so a range reaching past every
// one of them records nothing a reader can see. It does not stay harmless. It
// carries into each new generation and drifts as it goes, and the first hunk to
// land inside it reads as read with nobody having read it, which is the failure
// this tool exists to prevent. An unmark cannot answer it either: unreview --all
// subtracts the anchors, so whatever lay outside them outlives the review.
func clip(f review.File, side store.Side, r review.Range) ([]review.Range, error) {
	if len(f.Hunks) == 0 {
		return nil, fmt.Errorf("nothing in %s is named by a line, so --all is how to mark it", f.Diff.Path)
	}

	var out []review.Range
	held := false
	for _, h := range f.Hunks {
		for _, a := range h.Anchors {
			if a.Side != side {
				continue
			}
			held = true
			if lo, hi := max(r.Start, a.Range.Start), min(r.End, a.Range.End); lo <= hi {
				out = append(out, review.Range{Start: lo, End: hi})
			}
		}
	}

	switch {
	case !held:
		return nil, fmt.Errorf("no hunk of %s holds lines on the %s side: run zen-review files for the side each one is on",
			f.Diff.Path, side)
	case len(out) == 0:
		return nil, fmt.Errorf("no hunk of %s holds %s-side lines between %d and %d: run zen-review files for the ones it does hold",
			f.Diff.Path, side, r.Start, r.End)
	}
	return out, nil
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
// whole is the flag that reaches a file with no lines to name, which is --all
// for a mark and --file for a comment: this is shared by both, and a message
// naming the wrong one points the reader at a flag their command does not have.
func parseLines(s, whole string) (review.Range, error) {
	malformed := fmt.Errorf("the lines are A-B or a single A, not %q", s)

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
			"%s is how to reach a file that has no lines to name", s, whole)
	case end < start:
		return review.Range{}, fmt.Errorf("the range %q ends before it starts", s)
	}
	return review.Range{Start: start, End: end}, nil
}
