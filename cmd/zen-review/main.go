// Command zen-review tracks which changes you have inspected, and whether they have changed since.
package main

import (
	"context"
	"io"
	"os"

	"github.com/charmbracelet/fang"

	"github.com/praxis-labs-io/zen-review/internal/cli"
	"github.com/praxis-labs-io/zen-review/internal/version"
)

func main() {
	err := fang.Execute(context.Background(), cli.NewRoot(),
		fang.WithColorSchemeFunc(fang.AnsiColorScheme),
		fang.WithVersion(version.Version),
		fang.WithErrorHandler(report))
	os.Exit(cli.ExitCode(err))
}

func report(w io.Writer, styles fang.Styles, err error) {
	if cli.Quiet(err) {
		return
	}
	fang.DefaultErrorHandler(w, styles, err)
}
