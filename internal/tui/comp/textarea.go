package comp

import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"

	"github.com/zen-kit/zen-kit/theme"
)

// Textarea is the box prose is typed into, stripped of the chrome an editor
// wants and a note does not. Both places prose is typed take this one.
func Textarea(t theme.Theme) textarea.Model {
	area := textarea.New()
	area.Prompt = ""
	area.ShowLineNumbers = false
	area.SetStyles(textareaStyles(t))
	return area
}

// textareaStyles paints from the theme, and every state the same: bubbles picks
// its own colours, and a blurred box is one neither caller draws.
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
		Cursor:  textarea.CursorStyle{Color: t.Accent},
	}
}
