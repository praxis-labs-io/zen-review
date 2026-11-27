package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/praxis-labs-io/zen-review/internal/config"
)

func newConfig(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Where the reader's settings live",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the config file the reader reads, whether or not it exists yet",
		Long: "Print the config file the reader reads, whether or not it exists yet.\n\n" +
			"That is $XDG_CONFIG_HOME/zen-review/config.json (or ~/.config/zen-review/config.json)\n" +
			"when that file exists, and ~/.zen-review/config.json otherwise.",
		SilenceUsage: true,
		Args:         cobra.NoArgs,

		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("base") {
				return fmt.Errorf("config path reads no review, so it does not take --base")
			}
			path, err := config.Path()
			if err != nil {
				return err
			}
			return emit(cmd.OutOrStdout(), configPath{Path: path}, opts.asJSON)
		},
	})
	return cmd
}

type configPath struct {
	Path string `json:"path"`
}

func (c configPath) render() string { return c.Path + "\n" }

func (c configPath) encode(w io.Writer) error { return json.NewEncoder(w).Encode(c) }
