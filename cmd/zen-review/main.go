// Command zen-review answers one question about a branch: which of these changes
// have you inspected, and are they still the ones you inspected.
package main

import (
	"context"
	"os"

	"github.com/charmbracelet/fang"

	"github.com/zen-review/zen-review/internal/cli"
	"github.com/zen-review/zen-review/internal/version"
)

func main() {
	if err := fang.Execute(context.Background(), cli.NewRoot(), fang.WithVersion(version.Version)); err != nil {
		os.Exit(1)
	}
}
