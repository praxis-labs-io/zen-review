package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/praxis-labs-io/zen-review/internal/config"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// BodyFailed is exported because the render harness lands one read before the next key, so two in flight cannot be staged.
func BodyFailed(path string, gen int64, err error) tea.Msg {
	return bodyFailedMsg{path: path, gen: gen, err: err}
}

// LaunchTheme is exported because Run is the only caller and it queries a real terminal.
func LaunchTheme(cfg config.Config, s theme.Surface) (theme.Theme, error) { return launchTheme(cfg, s) }
