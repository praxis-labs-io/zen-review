package theme_test

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/tui/theme"
)

func TestOptionalFieldsFallBack(t *testing.T) {
	bare := theme.Theme{
		Text:   lipgloss.Color("#ffffff"),
		Border: lipgloss.Color("#333333"),
	}

	if got := bare.InvertedOrText(); got != bare.Text {
		t.Errorf("InvertedOrText() = %v, want Text when Inverted is unset", got)
	}
	if got := bare.BorderSubtleOrBorder(); got != bare.Border {
		t.Errorf("BorderSubtleOrBorder() = %v, want Border when unset", got)
	}
	if got := bare.BorderMutedOrSubtle(); got != bare.Border {
		t.Errorf("BorderMutedOrSubtle() = %v, want it to fall through to Border", got)
	}
}

func TestSetOptionalFieldsWin(t *testing.T) {
	full := theme.RosePineMoon

	if got := full.InvertedOrText(); got != full.Inverted {
		t.Errorf("InvertedOrText() = %v, want Inverted when it is set", got)
	}
	if got := full.BorderMutedOrSubtle(); got != full.BorderMuted {
		t.Errorf("BorderMutedOrSubtle() = %v, want BorderMuted when it is set", got)
	}
}
