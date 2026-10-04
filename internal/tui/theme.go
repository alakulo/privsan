package tui

import (
	"image/color"

	"charm.land/bubbles/v2/help"
	bkey "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

type theme struct {
	bg, surface, border, ink, muted, mint, violet, amber, red, selected color.Color
}

func (m *Model) setTheme(dark bool) {
	if dark {
		m.theme = theme{lipgloss.Color("#10141E"), lipgloss.Color("#171D2B"), lipgloss.Color("#35415B"),
			lipgloss.Color("#E7EDF7"), lipgloss.Color("#96A3BC"), lipgloss.Color("#67DEB0"),
			lipgloss.Color("#BAA2FF"), lipgloss.Color("#F3C57A"), lipgloss.Color("#FF8C9C"), lipgloss.Color("#263D40")}
	} else {
		m.theme = theme{lipgloss.Color("#F3F5FA"), lipgloss.Color("#FFFFFF"), lipgloss.Color("#C1CBDE"),
			lipgloss.Color("#202C40"), lipgloss.Color("#576880"), lipgloss.Color("#087B58"),
			lipgloss.Color("#7549B8"), lipgloss.Color("#976009"), lipgloss.Color("#C4324C"), lipgloss.Color("#DAF2E9")}
	}
	t := m.theme
	m.spinner.Style = lipgloss.NewStyle().Foreground(t.mint)
	m.bar.FullColor, m.bar.EmptyColor = t.mint, t.border
	s := textinput.DefaultStyles(dark)
	s.Focused.Text = lipgloss.NewStyle().Foreground(t.ink).Background(t.surface)
	s.Focused.Prompt = lipgloss.NewStyle().Foreground(t.mint)
	s.Focused.Placeholder = lipgloss.NewStyle().Foreground(t.muted)
	s.Cursor.Color, s.Cursor.Blink = t.mint, false
	m.input.SetStyles(s)
	for i := range m.replaceInputs {
		m.replaceInputs[i].SetStyles(s)
	}
	m.help.Styles = help.DefaultStyles(dark)
	m.help.Styles.ShortKey = lipgloss.NewStyle().Foreground(t.mint)
	m.help.Styles.ShortDesc = lipgloss.NewStyle().Foreground(t.muted)
	m.help.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(t.border)
	m.stylePreview()
}

func (m *Model) stylePreview() {
	t, target := m.theme, m.previewLine
	m.preview.StyleLineFunc = func(index int) lipgloss.Style {
		style := lipgloss.NewStyle().Foreground(t.ink)
		if index == target {
			style = style.Foreground(t.mint).Background(t.selected)
		}
		return style
	}
}

func binding(keys []string, label, description string) bkey.Binding {
	return bkey.NewBinding(bkey.WithKeys(keys...), bkey.WithHelp(label, description))
}

var shortKeys = []bkey.Binding{
	binding([]string{"c"}, "c", "替换"),
	binding([]string{"tab"}, "tab", "切换面板"),
	binding([]string{" "}, "space", "选择"),
	binding([]string{"/"}, "/", "搜索"),
	binding([]string{"w"}, "w", "执行"),
	binding([]string{"?"}, "?", "帮助"),
	binding([]string{"q"}, "q", "退出"),
}
