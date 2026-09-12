package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/praxis-labs-io/zen-review/internal/store"
)

const unresolvedState = "unresolved"

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

func runComments(cmd *cobra.Command, opts *options, f *filter) error {
	if err := f.check(); err != nil {
		return err
	}

	matched, err := listComments(cmd, opts, f)
	if err != nil {
		return err
	}

	if f.exitCode && matched {
		return errMatched
	}
	return nil
}

func listComments(cmd *cobra.Command, opts *options, f *filter) (_ bool, err error) {
	ctx := cmd.Context()

	s, err := open(ctx, opts)
	if err != nil {
		return false, err
	}
	defer func() { err = closing(err, s) }()

	st, err := s.Status(ctx)
	if err != nil {
		return false, err
	}

	all, err := s.Comments(ctx)
	if err != nil {
		return false, err
	}

	shown := f.apply(all)

	replaced, err := replacedFor(ctx, opts, s, st, shown)
	if err != nil {
		return false, err
	}

	v := commentsView{
		header:   statusHeader(s, st),
		Comments: shown,
		Replaced: replaced,
		filter:   f,
		Width:    screen(cmd.OutOrStdout()),
	}
	if err := emit(cmd.OutOrStdout(), v, opts.asJSON); err != nil {
		return false, err
	}
	return len(v.Comments) > 0, nil
}

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
