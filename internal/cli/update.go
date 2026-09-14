package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/praxis-labs-io/zen-review/internal/update"
	"github.com/praxis-labs-io/zen-review/internal/version"
)

func newUpdate() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Install the latest release over this binary",
		Long: "Install the latest release over this binary.\n\n" +
			"Asks GitHub for the latest release and stops when this is it. Otherwise it\n" +
			"runs the published installer into the directory this binary is in.",
		SilenceUsage: true,
		Args:         cobra.NoArgs,

		RunE: runUpdate,
	}
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	for _, flag := range []string{"base", "json"} {
		if cmd.Flags().Changed(flag) {
			return fmt.Errorf("update installs a release and reads no review, so it does not take --%s", flag)
		}
	}

	dir, err := update.InstallDir()
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	result, err := update.Check(ctx, update.Options{Current: version.Version})
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "Could not check for a newer release: %v\n", err)
	}

	switch {
	case version.Version == update.DevVersion:
		_, _ = fmt.Fprintln(out, "This is a locally built binary. Installing the latest release over it.")
	case result.Available:
		_, _ = fmt.Fprintf(out, "%s is available, running v%s.\n", result.Latest, strings.TrimPrefix(version.Version, "v"))
	case result.Latest != "":
		_, _ = fmt.Fprintf(out, "%s is the latest release. Nothing to install.\n", result.Latest)
		return nil
	}

	if err := update.Install(ctx, update.InstallOptions{Dir: dir, Out: out}); err != nil {
		return fmt.Errorf("updating: %w", err)
	}
	return nil
}
