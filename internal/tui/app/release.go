package app

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/comp"
)

// ReleaseCheck returns the tag of a release newer than the running build, or "" when there is none.
type ReleaseCheck func(ctx context.Context) (string, error)

// Launch is what the reader is handed at open beside the review.
type Launch struct {
	// Check nil skips the release check.
	Check ReleaseCheck
	// Warning shows on the status bar until the first key.
	Warning error
}

type newerReleaseMsg struct {
	tag string
}

func checkRelease(check ReleaseCheck) tea.Cmd {
	if check == nil {
		return nil
	}
	return func() tea.Msg {
		tag, _ := check(context.Background())
		if tag == "" {
			return nil
		}
		return newerReleaseMsg{tag: tag}
	}
}

func (m Model) releaseNotice() string {
	if m.newer == "" {
		return ""
	}
	return lipgloss.NewStyle().Foreground(m.theme.Subtle).
		Render(fmt.Sprintf("%s is available. Run zen-review update.", comp.Safe(m.newer)))
}
