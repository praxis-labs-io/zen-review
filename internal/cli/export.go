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

func title(s *review.Session) string {
	if branch := s.Branch(); branch != "" {
		return branch
	}
	return s.Repo()
}

type exportView struct {
	header

	Title   string
	Summary string

	Comments []store.Comment

	Reviewed int
	Items    int
}

// Bodies go through unfolded, because whatever renders the paste owns its layout.
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

		if c.Response != "" {
			fmt.Fprintf(&b, "\n> %s\n", strings.ReplaceAll(c.Response, "\n", "\n> "))
		}
	}
	return finish(&b)
}

func (v exportView) meta(b *strings.Builder) {
	fmt.Fprintf(b, "base `%s` (%s)", v.Base.Ref, short(v.Base.SHA))
	if v.Exists {
		fmt.Fprintf(b, ", generation %d\n%d of %d reviewed, %s\n",
			v.Generation.Seq, v.Reviewed, v.Items, outstanding(len(v.Comments)))
	} else {
		b.WriteString("\nNo generation yet, so nothing has been reviewed. Run `zen-review refresh`.\n")
	}

	switch v.reason() {
	case staleBase:
		fmt.Fprintf(b, "\nThe base has moved to %s since generation %d was measured, so the lines below may have too.\n",
			short(v.Base.SHA), v.Generation.Seq)
	case staleTree:
		fmt.Fprintf(b, "\nThe work tree has moved since generation %d was built, so the lines below may have too.\n",
			v.Generation.Seq)
	case fresh:
	}

	if settled(v.Comments) {
		b.WriteString("\nAn addressed or orphaned comment stopped moving when it was settled, " +
			"so its line is where it was then.\n")
	}

	if len(v.Skipped) > 0 {
		fmt.Fprintf(b, "\ngit could not read %s just now, so they are not in this review: %s\n",
			plural(len(v.Skipped), "path"), strings.Join(v.Skipped, ", "))
	}
}

func settled(comments []store.Comment) bool {
	for _, c := range comments {
		if c.State != store.CommentOpen {
			return true
		}
	}
	return false
}

func outstanding(n int) string {
	if n == 0 {
		return "nothing unresolved"
	}
	return plural(n, "comment") + " unresolved"
}

func finish(b *strings.Builder) string {
	return strings.TrimRight(b.String(), "\n") + "\n"
}
