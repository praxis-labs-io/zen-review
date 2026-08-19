package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/store"
)

func newExport(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Write the review as markdown, for pasting somewhere else",
		Long: "Write the review as markdown, for pasting somewhere else.\n\n" +
			"The note, how much has been read, and every comment still waiting on\n" +
			"somebody: open, addressed and orphaned, grouped by file. Locations and\n" +
			"bodies, never the code they are about, which the reader of a paste has\n" +
			"in front of them anyway.",
		SilenceUsage: true,
		Args:         cobra.NoArgs,

		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExport(cmd, opts)
		},
	}
}

func runExport(cmd *cobra.Command, opts *options) (err error) {
	if err := refuseJSON(cmd); err != nil {
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

	summary, err := s.Summary(ctx)
	if err != nil {
		return err
	}

	v := exportView{
		header:   statusHeader(s, st),
		Title:    title(s),
		Summary:  summary,
		Comments: (&filter{state: unresolvedState}).apply(all),
	}

	// Read only where there is a generation to read it off, and it costs a diff of
	// the two trees. A report saying what is outstanding without saying how much
	// has been read is half an answer.
	if st.Exists {
		c, err := s.Changeset(ctx, st.Generation)
		if err != nil {
			return err
		}
		v.Reviewed, v.Items = c.Reviewed, c.Items
	}

	_, err = io.WriteString(cmd.OutOrStdout(), v.markdown())
	return err
}

// title is what the report is a review of: the branch, or the repository when
// the session is not keyed to one.
func title(s *review.Session) string {
	if branch := s.Branch(); branch != "" {
		return branch
	}
	return s.Repo()
}

// exportView is the report: the session, its note, and everything still
// unanswered.
type exportView struct {
	header

	Title   string
	Summary string

	// Comments are the unresolved ones, in the order Session.Comments hands them
	// back, which is the order a file tree reads.
	Comments []store.Comment

	// Reviewed and Items are the burn-down, and both 0 on a session with no
	// generation to count.
	Reviewed int
	Items    int
}

// markdown is the report as something to paste.
//
// Bodies go through verbatim. Markdown is laid out by whatever renders it, and
// folding one here would put this tool's terminal width into somebody else's
// chat window. A body carrying its own heading mark becomes a heading, which is
// the price of not re-marking what somebody wrote.
func (v exportView) markdown() string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Review of %s\n\n", v.Title)
	v.meta(&b)

	if v.Summary != "" {
		fmt.Fprintf(&b, "\n%s\n", v.Summary)
	}

	file := ""
	for _, c := range v.Comments {
		if c.Path != file {
			file = c.Path
			fmt.Fprintf(&b, "\n## %s\n", file)
		}
		fmt.Fprintf(&b, "\n**`%s`** %s, %s, `%s`\n\n%s\n", at(c), c.Side, c.State, c.ID, c.Body)

		// A state with no words behind it is what the reader has to re-read the
		// code to check, which is the work the state was meant to save.
		if c.Response != "" {
			fmt.Fprintf(&b, "\n> %s\n", strings.ReplaceAll(c.Response, "\n", "\n> "))
		}
	}
	return finish(&b)
}

// meta is the two or three lines under the heading: what the review is measured
// against, how much of it has been read, and anything that makes those two less
// true than they look.
func (v exportView) meta(b *strings.Builder) {
	fmt.Fprintf(b, "base `%s` (%s)", v.Base.Ref, short(v.Base.SHA))
	if v.Exists {
		fmt.Fprintf(b, ", generation %d\n%d of %d reviewed, %s\n",
			v.Generation.Seq, v.Reviewed, v.Items, outstanding(len(v.Comments)))
	} else {
		b.WriteString("\nNo generation yet, so nothing has been reviewed. Run `zen-review refresh`.\n")
	}

	// A paste lands in front of somebody who cannot see this repository, so what
	// the lines below no longer describe has to travel with them. reason is fresh
	// on a session with no generation, so the two cases share the rest of this.
	switch v.reason() {
	case staleBase:
		fmt.Fprintf(b, "\nThe base has moved to %s since generation %d was measured, so the lines below may have too.\n",
			short(v.Base.SHA), v.Generation.Seq)
	case staleTree:
		fmt.Fprintf(b, "\nThe work tree has moved since generation %d was built, so the lines below may have too.\n",
			v.Generation.Seq)
	case fresh:
	}

	// A frozen comment records where its anchor was when it stopped, which is not
	// where the generation named above puts that file. The state word beside each
	// one does not say so, and the reader of a paste cannot check.
	if settled(v.Comments) {
		b.WriteString("\nAn addressed or orphaned comment stopped moving when it was settled, " +
			"so its line is where it was then.\n")
	}

	// Last, and on both paths. A session with nothing built yet is the one case
	// with no earlier warning that a file is missing from what is being reported,
	// and an edit nobody can see is the failure this tool exists to prevent.
	if len(v.Skipped) > 0 {
		fmt.Fprintf(b, "\ngit could not read %s just now, so they are not in this review: %s\n",
			plural(len(v.Skipped), "path"), strings.Join(v.Skipped, ", "))
	}
}

// settled reports a comment that has stopped moving. The report lists open,
// addressed and orphaned, so anything not open is one whose line is frozen.
func settled(comments []store.Comment) bool {
	for _, c := range comments {
		if c.State != store.CommentOpen {
			return true
		}
	}
	return false
}

// outstanding is what is left to answer, said once. A report that counted zero
// here and then wrote a sentence under the headings saying the same thing would
// be answering the question twice.
func outstanding(n int) string {
	if n == 0 {
		return "nothing unresolved"
	}
	return plural(n, "comment") + " unresolved"
}

// finish leaves the report ending in exactly one newline, whichever branch above
// wrote the last line.
func finish(b *strings.Builder) string {
	return strings.TrimRight(b.String(), "\n") + "\n"
}
