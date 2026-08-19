package comp

import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

// Textarea is the box prose is typed into, stripped of the chrome an editor
// wants and a note does not. Both places prose is typed take this one.
func Textarea(t theme.Theme) textarea.Model {
	area := textarea.New()
	area.Prompt = ""
	area.ShowLineNumbers = false
	area.SetStyles(textareaStyles(t))

	// The terminal's own cursor, placed by the root, rather than a block drawn
	// into the text in a colour of ours. The reader set that cursor up already.
	area.SetVirtualCursor(false)

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
		// No colour and no shape: what is left is what the terminal draws for
		// every other program.
		Cursor: textarea.CursorStyle{Blink: true},
	}
}
