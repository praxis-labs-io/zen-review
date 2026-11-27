package app

import (
	"errors"
	"fmt"

	"github.com/praxis-labs-io/zen-review/internal/config"
	"github.com/praxis-labs-io/zen-review/internal/tui/diffpane"
	"github.com/praxis-labs-io/zen-review/internal/tui/syntax"
	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// Launch is what the reader is handed at open beside the review.
type Launch struct {
	// Check nil skips the release check.
	Check ReleaseCheck
	// Warning shows on the status bar until the first key.
	Warning error
	// Config is the reader's settings as loaded. Warning carries whatever in the file did not read.
	Config config.Config
}

func launchTheme(cfg config.Config, s theme.Surface) (theme.Theme, error) {
	var problem error

	colors := theme.NewOverrides(cfg.Theme, theme.Inputs{
		Background: cfg.Background,
		Foreground: cfg.Foreground,
		AddedHue:   cfg.AddedHue,
		RemovedHue: cfg.RemovedHue,
	})
	if err := colors.Validate(); err != nil {
		problem = fmt.Errorf("%w, so no color from the config is applied", err)
		colors = theme.Overrides{}
	} else if derivesFromNothing(cfg, s) {
		problem = errors.New("the terminal reported no background, so foreground, addedHue and removedHue " +
			"have nothing to derive against until the config names a background")
	}

	t := colors.Resolve(s, cfg.PaintBackground)
	if cfg.SyntaxTheme == "" {
		return t, problem
	}
	if _, ok := syntax.New(cfg.SyntaxTheme); !ok {
		return t, alsoWarn(problem, fmt.Errorf("syntaxTheme: Chroma has no style %q, so code keeps %s", cfg.SyntaxTheme, t.Syntax))
	}
	t.Syntax = cfg.SyntaxTheme
	return t, problem
}

func derivesFromNothing(cfg config.Config, s theme.Surface) bool {
	named := cfg.Foreground != "" || cfg.AddedHue != "" || cfg.RemovedHue != ""
	return named && cfg.Background == "" && s.Background == nil
}

func panePrefs(cfg config.Config) diffpane.Prefs {
	return diffpane.Prefs{
		Split:       cfg.Split,
		TabWidth:    cfg.TabWidth,
		OpenSettled: !cfg.FoldsSettled(),
	}
}

// alsoWarn joins on one line because the status bar has one.
func alsoWarn(first, next error) error {
	switch {
	case first == nil:
		return next
	case next == nil:
		return first
	}
	return fmt.Errorf("%w; %w", first, next)
}
