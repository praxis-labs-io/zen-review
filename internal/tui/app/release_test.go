package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/praxis-labs-io/zen-review/internal/review"
	"github.com/praxis-labs-io/zen-review/internal/testchangeset"
	"github.com/praxis-labs-io/zen-review/internal/tui/app"
	"github.com/praxis-labs-io/zen-review/internal/tui/testtheme"
)

const newerNotice = "v9.9.9 is available. Run zen-review update."

func launched(t *testing.T, l app.Launch, width, height int) *screen {
	t.Helper()

	r := app.Reload{
		Base:       review.Base{Ref: "origin/main", SHA: "a1b2c3d4e5f67890"},
		Generation: review.Generation{ID: 2, Seq: 2},
		Changeset:  testchangeset.Nested(t),
	}

	src := &source{at: r}
	s := &screen{t: t, m: app.New(testtheme.Dark, src, "zen-review", r, l), src: src}
	s.send(tea.WindowSizeMsg{Width: width, Height: height})
	s.drain(s.m.Init())
	return s
}

func answering(tag string, err error) app.ReleaseCheck {
	return func(context.Context) (string, error) { return tag, err }
}

func TestTheBarNamesANewerRelease(t *testing.T) {
	for _, width := range []int{120, 72} {
		s := launched(t, app.Launch{Check: answering("v9.9.9", nil)}, width, 16)
		bar := s.bar()

		if !strings.Contains(bar, newerNotice) {
			t.Errorf("at %d columns the bar does not name the release whole: %q", width, bar)
		}
		if !strings.Contains(bar, "? help") {
			t.Errorf("at %d columns the notice pushed out the way to help: %q", width, bar)
		}
		if got := lipgloss.Width(bar); got != width {
			t.Errorf("at %d columns the bar is %d wide: %q", width, got, bar)
		}
	}
}

func TestTheReleaseNoticeTakesTheFactsPlace(t *testing.T) {
	s := launched(t, app.Launch{Check: answering("v9.9.9", nil)}, 120, 7)
	bar := s.bar()

	if !strings.Contains(bar, newerNotice) {
		t.Errorf("the bar does not name the release: %q", bar)
	}
	if strings.Contains(bar, "Generation 2") {
		t.Errorf("the facts drew beside the release notice: %q", bar)
	}
}

func TestNoReleaseLeavesTheBarAlone(t *testing.T) {
	tests := []struct {
		name  string
		check app.ReleaseCheck
	}{
		{name: "no check", check: nil},
		{name: "nothing newer", check: answering("", nil)},
		{name: "a failed check", check: answering("", errors.New("rate limited"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bar := launched(t, app.Launch{Check: tt.check}, 100, 7).bar()

			if strings.Contains(bar, "available") || strings.Contains(bar, "rate limited") {
				t.Errorf("the bar said something about a release: %q", bar)
			}
			if !strings.Contains(bar, "Generation 2") {
				t.Errorf("the facts did not come back: %q", bar)
			}
		})
	}
}

func TestAWarningHoldsTheBarUntilAKey(t *testing.T) {
	s := launched(t, app.Launch{
		Check:   answering("v9.9.9", nil),
		Warning: errors.New("parsing config.json"),
	}, 120, 16)

	if bar := s.bar(); !strings.Contains(bar, "parsing config.json") || strings.Contains(bar, newerNotice) {
		t.Errorf("the warning did not win the bar: %q", bar)
	}

	s.press("j")
	if bar := s.bar(); !strings.Contains(bar, newerNotice) {
		t.Errorf("the release notice did not follow the warning: %q", bar)
	}
}
