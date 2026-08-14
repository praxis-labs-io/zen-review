// Command zen-review answers one question about a branch: which of these changes
// have you inspected, and are they still the ones you inspected.
package main

import (
	"context"
	"io"
	"os"

	"github.com/charmbracelet/fang"

	"github.com/zen-review/zen-review/internal/cli"
	"github.com/zen-review/zen-review/internal/version"
)

func main() {
	err := fang.Execute(context.Background(), cli.NewRoot(),
		fang.WithVersion(version.Version),
		fang.WithErrorHandler(report))
	os.Exit(cli.ExitCode(err))
}

// report prints what fang would, except for an error a command raised to set an
// exit status rather than to say anything.
func report(w io.Writer, styles fang.Styles, err error) {
	if cli.Quiet(err) {
		return
	}
	fang.DefaultErrorHandler(w, styles, err)
}
