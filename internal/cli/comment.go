package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/store"
)

// aimFile is the third way of naming what a comment is about: the file itself,
// rather than lines in it. The other two are shared with a mark.
const aimFile aim = "file"

// note is what a comment command was asked to write. Exactly one of the three
// ways of naming what it is about is given, and side qualifies the two that name
// lines.
type note struct {
	aim   aim
	hunk  int
	lines string
	file  bool

	side string
	body string

	generation int
}

// mover is a comment moved to the state a verb names.
type mover func(context.Context, string) (store.Comment, error)

func newComment(opts *options) *cobra.Command {
	var n note

	cmd := &cobra.Command{
		Use:   "comment <path>",
		Short: "Write a comment against part of the changeset",
		Long: "Write a comment against part of the changeset.\n\n" +
			"A hunk is named by the line it introduces first, which is what the files\n" +
			"command prints beside it. Lines are taken as given rather than narrowed,\n" +
			"and lines no hunk holds are refused: a comment is what somebody said, and\n" +
			"moving it to lines they did not pick says something else.",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := n.check(cmd); err != nil {
				return err
			}
			return runComment(cmd, opts, &n, args[0])
		},
	}

	cmd.Flags().IntVar(&n.hunk, "hunk", 0, "comment on the hunk named by this line")
	cmd.Flags().StringVar(&n.lines, "lines", "", "comment on these lines, as A-B or a single A")
	cmd.Flags().BoolVar(&n.file, "file", false, "comment on the file itself rather than on lines in it")
	cmd.Flags().StringVar(&n.side, "side", string(store.SideHead),
		"which blob the lines are measured on, head or base")
	cmd.Flags().StringVar(&n.body, "body", "", "what the comment says, or - to read it from stdin")
	cmd.Flags().IntVar(&n.generation, "generation", 0,
		"refuse unless this is the generation the comment lands on")

	return cmd
}

// newAddress is the one state verb that takes words, because the state is a
// claim and the words are what back it.
func newAddress(opts *options) *cobra.Command {
	var text string

	cmd := &cobra.Command{
		Use:   "address <id>",
		Short: "Record that a comment has been handled, and say how",
		Long: "Record that a comment has been handled, and say how.\n\n" +
			"This is the agent's verb. It stops the comment moving and says the work\n" +
			"was done; it does not close it, because the claim and the confirmation\n" +
			"are different facts and a queue letting one stand for the other is worth\n" +
			"nothing.\n\n" +
			"The answer is what the reader confirms against. It is optional, because\n" +
			"half a queue is change requests where the diff is the answer, and the\n" +
			"other half are questions a diff does not answer at all.",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := refuseBase(cmd); err != nil {
				return err
			}

			// Read before the database is opened, for the reason runComment gives.
			written, err := body(cmd, text, "answer")
			if err != nil {
				return err
			}

			return runVerb(cmd, opts, args[0], func(s *review.Session) mover {
				return func(ctx context.Context, id string) (store.Comment, error) {
					return s.AddressComment(ctx, id, written)
				}
			})
		},
	}

	cmd.Flags().StringVar(&text, "body", "", "what the answer says, or - to read it from stdin")

	return cmd
}

func newResolve(opts *options) *cobra.Command {
	return newVerb(opts, "resolve", "Close a comment",
		"Close a comment.\n\n"+
			"This is the reader's verb and it closes anything not already closed, an\n"+
			"orphaned comment included: the code it was about is gone, and saying that\n"+
			"settles it is the reader's call.",
		func(s *review.Session) mover { return s.ResolveComment })
}

func newDelete(opts *options) *cobra.Command {
	return newVerb(opts, "delete", "Delete a comment",
		"Delete a comment.\n\n"+
			"It goes rather than settling into a state. A comment nobody meant to\n"+
			"write is a record of nothing, and a state for it would have to be filtered\n"+
			"out of every count and every ring forever. The row that went is what this\n"+
			"prints, so --json hands back what it removed.",
		func(s *review.Session) mover { return s.DeleteComment })
}

// newEdit is the one verb that is not a state, so it takes a body rather than
// the id alone.
func newEdit(opts *options) *cobra.Command {
	var text, answer string

	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Rewrite what a comment says",
		Long: "Rewrite what a comment says.\n\n" +
			"The reader's words, the agent's answer, or both. The anchor never moves,\n" +
			"so a comment on the wrong lines is a delete and a new one rather than an\n" +
			"edit. An empty --answer takes the answer back; an empty --body is refused,\n" +
			"because wiping a comment is a delete and has its own verb.",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := refuseBase(cmd); err != nil {
				return err
			}
			if err := needsWords(cmd); err != nil {
				return err
			}

			// Read before the database is opened, for the reason runComment gives:
			// words on stdin are the reader still typing.
			written, err := given(cmd, "body", text, "comment")
			if err != nil {
				return err
			}
			said, err := given(cmd, "answer", answer, "answer")
			if err != nil {
				return err
			}

			return runVerb(cmd, opts, args[0], func(s *review.Session) mover {
				return func(ctx context.Context, id string) (store.Comment, error) {
					return s.EditComment(ctx, id, written, said)
				}
			})
		},
	}

	cmd.Flags().StringVar(&text, "body", "", "what the comment says, or - to read it from stdin")
	cmd.Flags().StringVar(&answer, "answer", "", "what the answer says, or - to read it from stdin")

	return cmd
}

func newVerb(opts *options, use, short, long string, verb func(*review.Session) mover) *cobra.Command {
	return &cobra.Command{
		Use:          use + " <id>",
		Short:        short,
		Long:         long,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := refuseBase(cmd); err != nil {
				return err
			}
			return runVerb(cmd, opts, args[0], verb)
		},
	}
}

// check reads the flags against each other, before anything opens a database.
//
// No message here opens on a flag name or a path, for the reason mark.go gives:
// the error is printed with its first letter capitalised, which turns --hunk
// into a flag that does not exist.
func (n *note) check(cmd *cobra.Command) error {
	if err := refuseBase(cmd); err != nil {
		return err
	}

	// --file is read by its value and the other two by whether they were passed,
	// the same way --all is: a bool flag is set either way it is spelled.
	named := 0
	if cmd.Flags().Changed("hunk") {
		named, n.aim = named+1, aimHunk
	}
	if cmd.Flags().Changed("lines") {
		named, n.aim = named+1, aimLines
	}
	if n.file {
		named, n.aim = named+1, aimFile
	}

	switch {
	case named == 0:
		return errors.New("nothing to comment on: pass --hunk, --lines or --file")
	case named > 1:
		return errors.New("pass one of --hunk, --lines and --file: they name three different things to comment on")
	}

	if n.file && cmd.Flags().Changed("side") {
		return errors.New("the side is not a choice under --file, which takes the side the file has bytes on")
	}
	return needsBody(cmd)
}

// needsBody refuses a write with no --body, in the words both of them use.
func needsBody(cmd *cobra.Command) error {
	if !cmd.Flags().Changed("body") {
		return errors.New("a comment needs something in it: pass --body <text>, or --body - to read it from stdin")
	}
	return nil
}

// needsWords is needsBody for a command that can rewrite either half.
func needsWords(cmd *cobra.Command) error {
	if !cmd.Flags().Changed("body") && !cmd.Flags().Changed("answer") {
		return errors.New("an edit needs something to write: pass --body <text> or --answer <text>, either as - to read it from stdin")
	}
	return nil
}

func runComment(cmd *cobra.Command, opts *options, n *note, path string) (err error) {
	ctx := cmd.Context()

	// Read before the database is opened. A body arriving on stdin is the reader
	// still typing, and holding the session open across that is holding it open
	// for as long as they take.
	text, err := body(cmd, n.body, "comment")
	if err != nil {
		return err
	}

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
		return errors.New("this session has no generation, so there is nothing to comment on: run zen-review refresh")
	}
	// Read by whether it was passed, not by whether it is zero. Generation 0 is
	// not a generation, so a sentinel would let the one value that can never be
	// current through as though the flag had been left off.
	if cmd.Flags().Changed("generation") && n.generation != st.Generation.Seq {
		return &review.StaleGenerationError{Seq: n.generation, Current: st.Generation.Seq}
	}

	c, err := s.Changeset(ctx, st.Generation)
	if err != nil {
		return err
	}

	resolved, err := n.resolve(c, path, text)
	if err != nil {
		return err
	}

	written, err := s.AddComment(ctx, st.Generation, resolved)
	if err != nil {
		return err
	}
	return emit(cmd.OutOrStdout(), one(cmd, s, st, written), opts.asJSON)
}

func runVerb(cmd *cobra.Command, opts *options, id string, verb func(*review.Session) mover) (err error) {
	ctx := cmd.Context()

	s, err := open(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { err = closing(err, s) }()

	// The session is read before the write, so a failure to describe it refuses
	// rather than leaving a comment moved and nothing said about it.
	st, err := s.Status(ctx)
	if err != nil {
		return err
	}

	c, err := verb(s)(ctx, id)
	if err != nil {
		return err
	}
	return emit(cmd.OutOrStdout(), one(cmd, s, st, c), opts.asJSON)
}

// one is the answer a write gives: the comment it wrote, in the shape the
// listing prints, so --json hands back the id the next command takes.
func one(cmd *cobra.Command, s *review.Session, st review.Status, c store.Comment) commentsView {
	return commentsView{
		header:   statusHeader(s, st),
		Comments: []store.Comment{c},
		Width:    screen(cmd.OutOrStdout()),
	}
}

// resolve turns the flags into the note the engine takes.
func (n *note) resolve(c review.Changeset, path, text string) (review.Note, error) {
	f, found := c.File(path)
	if !found {
		return review.Note{}, fmt.Errorf("no file of this changeset is called %s: run zen-review files for the ones there are",
			path)
	}

	if n.aim == aimFile {
		return review.NoteOnFile(f, text), nil
	}

	side, err := parseSide(n.side)
	if err != nil {
		return review.Note{}, err
	}

	if n.aim == aimLines {
		r, err := parseLines(n.lines, "--file")
		if err != nil {
			return review.Note{}, err
		}
		if err := anchored(f, side, r); err != nil {
			return review.Note{}, err
		}
		return review.NoteOnLines(path, side, r, text), nil
	}

	h, found := c.Hunk(path, side, n.hunk)
	if !found {
		return review.Note{}, fmt.Errorf("no hunk of %s is named %s %d: run zen-review files for the ones there are",
			path, side, n.hunk)
	}
	return review.NoteOnHunk(path, h, text), nil
}

// anchored refuses lines that no hunk of the file holds any of on that side.
//
// review --lines clips to the hunks instead, and the difference is deliberate.
// Clipping a mark narrows a claim about how much was read; clipping a comment
// moves what somebody said onto lines they did not pick. So a range overlapping
// one anchor is kept exactly as typed, and one spanning two hunks stays one
// comment about both.
//
// Refusing the rest matters for the reason the clip exists. A comment anchored
// outside every hunk is on nothing a reader can be shown, and it carries into
// each new generation drifting as it goes.
func anchored(f review.File, side store.Side, r review.Range) error {
	if len(f.Hunks) == 0 {
		return fmt.Errorf("nothing in %s is named by a line, so --file is how to comment on it", f.Diff.Path)
	}

	var held []string
	for _, h := range f.Hunks {
		for _, a := range h.Anchors {
			if a.Side != side {
				continue
			}
			if max(r.Start, a.Range.Start) <= min(r.End, a.Range.End) {
				return nil
			}
			held = append(held, span(a.Range))
		}
	}

	if len(held) == 0 {
		return fmt.Errorf("no hunk of %s holds lines on the %s side: run zen-review files for the side each one is on",
			f.Diff.Path, side)
	}
	return fmt.Errorf("no hunk of %s holds %s-side lines between %d and %d, and it holds %s",
		f.Diff.Path, side, r.Start, r.End, strings.Join(held, ", "))
}

// span is a run of lines as a reader types it back. A selection of one line is
// still a selection and nobody types 42-42.
func span(r review.Range) string {
	if r.Start == r.End {
		return fmt.Sprint(r.Start)
	}
	return fmt.Sprintf("%d-%d", r.Start, r.End)
}

// body is what was written: the text passed, or stdin when it is -, so prose
// with newlines in it does not have to survive a shell. Only the stdin failure
// uses what, to name the thing it was reading.
//
// Trailing whitespace goes, because a heredoc ends in a newline and a comment
// does not. Leading whitespace stays: the listing reads an indented line as one
// somebody laid out on purpose, and eating it here would make that promise
// false for every body that arrives with one. A comment left with nothing in it
// is refused by the engine rather than stored.
func body(cmd *cobra.Command, flag, what string) (string, error) {
	if flag != "-" {
		return trailing(flag), nil
	}

	raw, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", fmt.Errorf("reading the %s from stdin: %w", what, err)
	}
	return trailing(string(raw)), nil
}

func trailing(s string) string { return strings.TrimRight(s, " \t\r\n") }

// given is body for a flag that may be left out. A flag nobody spelled is nil,
// which is what tells the engine to leave that half of the comment alone.
func given(cmd *cobra.Command, flag, value, what string) (*string, error) {
	if !cmd.Flags().Changed(flag) {
		return nil, nil
	}

	written, err := body(cmd, value, what)
	if err != nil {
		return nil, err
	}
	return &written, nil
}
