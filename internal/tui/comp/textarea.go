package comp

import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// Textarea returns a chrome-free textarea painted from t. It draws no cursor; the caller places it.
func Textarea(t theme.Theme) textarea.Model {
	area := textarea.New()
	area.Prompt = ""
	area.ShowLineNumbers = false
	area.SetStyles(textareaStyles(t))

	area.SetVirtualCursor(false)

	return area
}

func textareaStyles(t theme.Theme) textarea.Styles {
	text := lipgloss.NewStyle().Foreground(t.Text)
	muted := lipgloss.NewStyle().Foreground(t.Muted)

	state := textarea.StyleState{
		Base:        lipgloss.NewStyle(),
		Text:        text,
		CursorLine:  text,
		EndOfBuffer: muted,
		Placeholder: muted,
		Prompt:      muted,
	}
	return textarea.Styles{
		Focused: state,
		Blurred: state,
		Cursor:  textarea.CursorStyle{Blink: true},
	}
}
