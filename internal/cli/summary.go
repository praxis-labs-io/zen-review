package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

func newSummary(opts *options) *cobra.Command {
	var set string

	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Read or write the note about the whole review",
		Long: "Read or write the note about the whole review.\n\n" +
			"One note per session, replaced each time it is written and cleared by\n" +
			"writing an empty one. It is what the export opens with, and the place a\n" +
			"conclusion that is not about any one file goes.",
		SilenceUsage: true,
		Args:         cobra.NoArgs,

		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSummary(cmd, opts, set)
		},
	}

	cmd.Flags().StringVar(&set, "set", "",
		"write this as the note, or - to read it from stdin. An empty one clears it")

	return cmd
}

func runSummary(cmd *cobra.Command, opts *options, set string) (err error) {
	ctx := cmd.Context()

	// A read takes --base the way every other read does. A write does not, for the
	// reason refuseBase gives: the move sticks, and this call was not about it.
	writing := cmd.Flags().Changed("set")
	if writing {
		if err := refuseBase(cmd); err != nil {
			return err
		}
	}

	// Read before the database is opened. A note arriving on stdin is somebody
	// still typing, and holding the session open across that is holding it open
	// for as long as they take.
	text, err := body(cmd, set, "summary")
	if err != nil {
		return err
	}

	s, err := open(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { err = closing(err, s) }()

	if writing {
		if err := s.SetSummary(ctx, text); err != nil {
			return err
		}
	}

	st, err := s.Status(ctx)
	if err != nil {
		return err
	}

	v := summaryView{
		header:  statusHeader(s, st),
		Summary: s.Summary(),
		Width:   screen(cmd.OutOrStdout()),
	}
	return emit(cmd.OutOrStdout(), v, opts.asJSON)
}

// summaryView is the session and the note written against it, which is what a
// read and a write both answer with. A write answering with what it wrote is the
// shape the comment commands take.
type summaryView struct {
	header

	Summary string

	// Width is what the note wraps into, measured by the caller so this stays a
	// pure function of the view.
	Width int
}

// render lays the note out under the session the way a comment body is laid out
// under the row naming it.
func (v summaryView) render() string {
	var b strings.Builder
	v.write(&b)

	if v.Summary == "" {
		b.WriteString("\nno summary yet, so write one with zen-review summary --set <text>\n")
		return b.String()
	}

	b.WriteString("\n")
	writeBody(&b, v.Summary, v.Width)
	return b.String()
}

// summaryPayload is the wire shape. The note is a string and not a structure,
// and the session keys are the ones every other command promises.
type summaryPayload struct {
	headerJSON

	Summary string `json:"summary"`
}

func (v summaryView) encode(out io.Writer) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	p := summaryPayload{headerJSON: headerOf(v.header), Summary: v.Summary}
	if err := enc.Encode(p); err != nil {
		return fmt.Errorf("writing the summary as JSON: %w", err)
	}
	return nil
}
