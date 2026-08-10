// Command zen-review answers one question about a branch: which of these
// changes have you inspected, and are they still the ones you inspected.
package main

import (
	"context"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/zen-review/zen-review/internal/version"
)

func main() {
	if err := fang.Execute(context.Background(), newRootCmd(), fang.WithVersion(version.Version)); err != nil {
		os.Exit(1)
	}
}

// newRootCmd has no RunE yet, so a bare invocation prints help. Bare
// zen-review opens the current branch's changeset once the engine exists.
func newRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "zen-review",
		Short:        "Review the changes on a branch, and remember what you reviewed",
		Version:      version.Version,
		SilenceUsage: true,
	}
}
