package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zen-review/zen-review/internal/store"
)

// unresolvedState is the filter word for everything somebody still has to
// answer. It is not a state a comment is ever in, which is why it is spelled
// here and not in the store's vocabulary.
const unresolvedState = "unresolved"

// filter is what a listing was asked for. Its zero value matches everything.
type filter struct {
	state string
	path  string

	exitCode bool
}

func newComments(opts *options) *cobra.Command {
	var f filter

	cmd := &cobra.Command{
		Use:   "comments",
		Short: "List the comments written against this session",
		Long: "List the comments written against this session.\n\n" +
			"Every comment, live and frozen, by file and then down the file. With\n" +
			"--exit-code it leaves a status of 1 when the filter matched anything and\n" +
			"2 when it failed, so a hook can tell an open comment from a broken run.",
		SilenceUsage: true,
		Args:         cobra.NoArgs,

		RunE: func(cmd *cobra.Command, _ []string) error {
			return runComments(cmd, opts, &f)
		},
	}

	cmd.Flags().StringVar(&f.state, "state", "",
		"list only comments in this state: open, addressed, resolved, orphaned, "+
			"or unresolved for every one of those but resolved")
	cmd.Flags().StringVar(&f.path, "path", "",
		"list only comments recorded under this path, which on the base side of a rename is the old name")
	cmd.Flags().BoolVar(&f.exitCode, "exit-code", false,
		"leave a status of 1 when the filter matched anything")

	return cmd
}

func runComments(cmd *cobra.Command, opts *options, f *filter) (err error) {
	if err := f.check(); err != nil {
		return err
	}

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

	all, err := s.Comments(ctx)
	if err != nil {
		return err
	}

	v := commentsView{header: statusHeader(s, st), Comments: f.apply(all), filter: f}
	if err := emit(cmd.OutOrStdout(), v, opts.asJSON); err != nil {
		return err
	}

	// After the listing is written, never instead of it. The status is what a
	// hook acts on and the comments are what a person reads, and a command that
	// withheld one to report the other would be answering half the question.
	if f.exitCode && len(v.Comments) > 0 {
		return errMatched
	}
	return nil
}

// check reads the state a listing was asked for against the vocabulary, so a
// typo is a sentence rather than an empty list that looks like an answer.
func (f *filter) check() error {
	switch f.state {
	case "", unresolvedState,
		string(store.CommentOpen), string(store.CommentAddressed),
		string(store.CommentResolved), string(store.CommentOrphaned):
		return nil
	default:
		return fmt.Errorf("a comment is open, addressed, resolved or orphaned, and %q is none of them: "+
			"unresolved is every one of those but resolved", f.state)
	}
}

// apply narrows a listing to what was asked for.
func (f *filter) apply(comments []store.Comment) []store.Comment {
	out := make([]store.Comment, 0, len(comments))
	for _, c := range comments {
		if f.matches(c) {
			out = append(out, c)
		}
	}
	return out
}

func (f *filter) matches(c store.Comment) bool {
	if f.path != "" && c.Path != f.path {
		return false
	}

	switch f.state {
	case "":
		return true
	case unresolvedState:
		return c.State != store.CommentResolved
	default:
		return string(c.State) == f.state
	}
}

// nothing is what a listing that matched none says.
//
// It repeats the filter back rather than saying there is nothing at all, because
// those are different answers and a reader who mistyped a path would believe the
// wrong one.
func (f *filter) nothing() string {
	if f.state == "" && f.path == "" {
		return "no comments yet"
	}

	var b strings.Builder
	b.WriteString("no ")
	if f.state != "" {
		b.WriteString(f.state + " ")
	}
	b.WriteString("comment")
	if f.path != "" {
		b.WriteString(" on " + f.path)
	}
	return b.String()
}
