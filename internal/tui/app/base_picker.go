package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/zen-kit/zen-kit/theme"

	"github.com/zen-review/zen-review/internal/review"
	"github.com/zen-review/zen-review/internal/tui/comp"
)

const pickerRows = 5

type baseOption struct {
	group     string
	candidate review.Candidate
}

type basePicker struct {
	theme    theme.Theme
	input    textinput.Model
	all      []baseOption
	shown    []baseOption
	current  string
	selected int
	offset   int
	err      string
	opened   bool
}

func newBasePicker(t theme.Theme) basePicker {
	input := textinput.New()
	input.Prompt = "/ "
	input.Placeholder = "search branches"
	styles := input.Styles()
	styles.Focused.Text = lipgloss.NewStyle().Foreground(t.Text)
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(t.Muted)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(t.Accent)
	styles.Cursor.Color = t.Accent
	styles.Cursor.Blink = false
	input.SetStyles(styles)
	return basePicker{theme: t, input: input}
}

func (p *basePicker) open(candidates review.BaseCandidates, current string) tea.Cmd {
	p.all = p.all[:0]
	for _, candidate := range candidates.Local {
		p.all = append(p.all, baseOption{group: "Local", candidate: candidate})
	}
	for _, candidate := range candidates.Remote {
		p.all = append(p.all, baseOption{group: "Remote", candidate: candidate})
	}
	p.current, p.selected, p.offset, p.err, p.opened = current, 0, 0, "", true
	p.input.SetValue("")
	p.filter()
	_ = p.input.Focus()
	return nil
}

func (p *basePicker) close() {
	p.input.Blur()
	p.all, p.shown, p.err, p.opened = nil, nil, "", false
}

func (p basePicker) active() bool { return p.opened }

func (p basePicker) choice() (string, bool) {
	if p.selected < 0 || p.selected >= len(p.shown) {
		return "", false
	}
	return p.shown[p.selected].candidate.Branch, true
}

func (p *basePicker) update(msg tea.Msg) tea.Cmd {
	if press, ok := msg.(tea.KeyPressMsg); ok {
		switch press.String() {
		case "up":
			p.move(-1)
			return nil
		case "down":
			p.move(1)
			return nil
		}
	}

	before := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != before {
		p.filter()
	}
	return cmd
}

func (p *basePicker) filter() {
	query := strings.ToLower(p.input.Value())
	p.shown = p.shown[:0]
	for _, option := range p.all {
		if strings.Contains(strings.ToLower(option.candidate.Branch), query) {
			p.shown = append(p.shown, option)
		}
	}
	p.selected, p.offset = 0, 0
}

func (p *basePicker) move(by int) {
	if len(p.shown) == 0 {
		return
	}
	p.selected = min(max(p.selected+by, 0), len(p.shown)-1)
	if p.selected < p.offset {
		p.offset = p.selected
	}
	if p.selected >= p.offset+pickerRows {
		p.offset = p.selected - pickerRows + 1
	}
}

func (p basePicker) view(width, height int) string {
	inner := min(max(width-8, 20), 64)
	p.input.SetWidth(max(inner-2, 0))
	rows := []string{p.input.View(), ""}

	if len(p.shown) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.theme.Muted).Render("No matching branches"))
	} else {
		end := min(p.offset+pickerRows, len(p.shown))
		group := ""
		for i := p.offset; i < end; i++ {
			option := p.shown[i]
			if option.group != group {
				group = option.group
				rows = append(rows, lipgloss.NewStyle().Foreground(p.theme.Muted).Render(group))
			}
			rows = append(rows, p.row(option, i == p.selected, inner))
		}
	}

	if p.err != "" {
		rows = append(rows, "", lipgloss.NewStyle().Foreground(p.theme.Error).Render(comp.Safe(p.err)))
	}
	rows = append(rows, "", lipgloss.NewStyle().Foreground(p.theme.Subtle).
		Render("↑/↓ move  enter select  esc cancel"))
	return comp.Modal(p.theme, "Base", strings.Join(rows, "\n"), width, height)
}

func (p basePicker) row(option baseOption, selected bool, width int) string {
	prefix := "  "
	style := lipgloss.NewStyle().Foreground(p.theme.Text)
	if selected {
		prefix = "> "
		style = style.Foreground(p.theme.Text).Background(p.theme.SelectedBackground).Bold(true)
	}

	name := option.candidate.Branch
	if name == p.current {
		name += " (current)"
	}
	distance := fmt.Sprintf("%d back", option.candidate.Ahead)
	room := max(width-lipgloss.Width(prefix)-lipgloss.Width(distance)-1, 0)
	line := prefix + comp.Clip(name, room, lipgloss.NewStyle().Foreground(p.theme.Muted))
	line += strings.Repeat(" ", max(width-lipgloss.Width(line)-lipgloss.Width(distance), 0)) + distance
	return style.Width(width).Render(line)
}
