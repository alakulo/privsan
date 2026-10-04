package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type layout struct {
	width, bodyHeight, leftWidth, rightWidth, listHeight, previewHeight int
	sidebar, split, compact                                             bool
}

func (m Model) layout() layout {
	l := layout{width: max(1, m.width-2), sidebar: m.width >= 100, compact: m.height < 25}
	top := 7 // header 2, cards 3, toolbar 1, spacer 1
	if l.compact {
		top = 5
	} // single-line counters
	l.bodyHeight = max(1, m.height-top-3)
	l.rightWidth = l.width
	if l.sidebar {
		l.leftWidth = l.width / 4
		l.rightWidth = l.width - l.leftWidth - 1
	}
	l.split = l.bodyHeight >= 10
	l.listHeight = l.bodyHeight
	if l.split {
		l.listHeight = max(5, l.bodyHeight*11/20)
		l.previewHeight = l.bodyHeight - l.listHeight
	}
	return l
}

func (m *Model) resize() {
	l := m.layout()
	// Measure actual frame rather than assuming width minus two. The panels
	// have a one-cell border and one-cell horizontal padding on both sides.
	x, y := m.panelStyle(false).GetFrameSize()
	previewHeight := l.previewHeight
	if !l.split {
		previewHeight = l.bodyHeight
	}
	m.preview.SetWidth(max(1, l.rightWidth-x))
	m.preview.SetHeight(max(1, previewHeight-y-2))
	m.details.SetWidth(max(1, l.width-x))
	m.details.SetHeight(max(1, l.bodyHeight-y-2))
	// Both input width and cursor must fit inside the dialog's actual frame.
	m.input.SetWidth(max(1, min(64, min(72, m.width-6)-x-3)))
	for i := range m.replaceInputs {
		// The input width excludes its two-cell prompt and one-cell cursor.
		m.replaceInputs[i].SetWidth(max(1, min(72, m.width-6)-7))
	}
	dialogWidth := max(1, min(72, m.width-6))
	dialogHeight := min(18, max(3, m.height-2))
	m.confirmation.SetWidth(max(1, dialogWidth-x))
	m.confirmation.SetHeight(max(1, dialogHeight-y-3))
	m.help.SetWidth(l.width)
	m.bar.SetWidth(max(1, min(52, l.width-12)))
}

func (m Model) panelStyle(active bool) lipgloss.Style {
	border := m.theme.border
	if active {
		border = m.theme.mint
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Foreground(m.theme.ink).Background(m.theme.surface).Padding(0, 1)
}

func clipped(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

func fitted(s string, width int) string {
	s = clipped(s, width)
	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s)))
}

// panel fixes the content rectangle before styling. No automatic line wrapping
// or implicit Height on the bordered style can change the frame size.
func (m Model) panel(width, height int, active bool, lines []string) string {
	style := m.panelStyle(active)
	x, y := style.GetFrameSize()
	w, h := max(1, width-x), max(1, height-y)
	if len(lines) > h {
		lines = lines[:h]
	}
	content := make([]string, h)
	base := ansi.Style{}.ForegroundColor(m.theme.ink).BackgroundColor(m.theme.surface).String()
	for i := range content {
		if i < len(lines) {
			// Nested colored labels reset SGR attributes. Restore the panel's
			// base colors so those labels do not punch holes in its background.
			content[i] = strings.ReplaceAll(fitted(lines[i], w), ansi.ResetStyle, ansi.ResetStyle+base)
		} else {
			content[i] = strings.Repeat(" ", w)
		}
	}
	return style.Render(strings.Join(content, "\n"))
}

func windowStart(cursor, count, size int) int {
	return min(max(0, cursor-size/2), max(0, count-size))
}
