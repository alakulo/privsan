package tui

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

func (m Model) busy() bool { return m.state == "scanning" || m.state == "writing" }

func (m Model) stop() (tea.Model, tea.Cmd) {
	if m.cancel != nil {
		m.cancel()
	}
	if m.busy() {
		m.quitting, m.status = true, "正在安全停止，等待当前文件完成…"
		return m, nil
	}
	return m, tea.Quit
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case externalCancel:
		return m.stop()
	case startMsg:
		return m.start()
	case tea.WindowSizeMsg:
		m.width, m.height = max(0, msg.Width), max(0, msg.Height)
		m.resize()
	case tea.BackgroundColorMsg:
		m.setTheme(msg.IsDark())
	case spinner.TickMsg:
		if m.busy() {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
	case progressMsg:
		if m.state == "scanning" && msg.source == m.events {
			m.progress = msg.progress
			return m, listen(m.events)
		}
	case scanDone:
		if m.cancel != nil {
			m.cancel()
		}
		m.snapshot, m.state = msg.snapshot, "review"
		m.rebuild()
		m.buildDetails()
		if m.snapshot.Complete {
			m.status = "扫描完成 · 默认选中所有命中，按空格调整"
		} else {
			m.status = "扫描不完整，写入已禁用 · 按 e 查看问题"
		}
		if m.quitting {
			return m, tea.Quit
		}
	case writeDone:
		if m.cancel != nil {
			m.cancel()
		}
		m.operation, m.state = msg.operation, "done"
		m.buildDetails()
		if msg.operation.Complete {
			m.status = fmt.Sprintf("✓ 完成：%d 个文件 · e 查看结果目录，r 开始新扫描", msg.operation.Changed)
		} else {
			m.status = "操作未完整成功 · 按 e 查看错误与恢复目录"
		}
		if m.quitting {
			return m, tea.Quit
		}
	case tea.KeyPressMsg:
		k := msg.String()
		if k == "ctrl+c" {
			return m.stop()
		}
		if m.busy() {
			if k == "q" || k == "esc" {
				return m.stop()
			}
			return m, nil
		}
		if m.inputMode != "" {
			return m.updateInput(msg)
		}
		if m.confirm {
			return m.updateConfirm(msg)
		}
		if m.showHelp {
			switch k {
			case "?", "esc", "q":
				m.showHelp = false
			}
			return m, nil
		}
		if m.showErrors {
			switch k {
			case "e", "esc":
				m.showErrors = false
			case "q":
				return m, tea.Quit
			default:
				var cmd tea.Cmd
				m.details, cmd = scrollViewport(m.details, msg)
				return m, cmd
			}
			return m, nil
		}
		switch k {
		case "q":
			return m, tea.Quit
		case "?":
			m.showHelp = true
		case "e":
			m.showErrors = true
		case "tab", "shift+tab":
			delta := 1
			if k == "shift+tab" {
				delta = 2
			}
			m.focus = focus((int(m.focus) + delta) % 3)
		case "f":
			m.focus = focusFiles
		case "enter":
			if m.focus == focusFiles {
				m.focus = focusFindings
			}
		case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
			if m.focus == focusPreview {
				var cmd tea.Cmd
				m.preview, cmd = scrollViewport(m.preview, msg)
				return m, cmd
			}
			m.navigate(k)
		case "left", "h":
			if m.focus == focusPreview {
				m.preview.ScrollLeft(8)
			}
		case "right", "l":
			if m.focus == focusPreview {
				m.preview.ScrollRight(8)
			}
		case "/":
			return m.beginInput("filter", m.filter)
		case "o":
			return m.beginInput("root", m.options.Root)
		case "d":
			if m.state != "done" {
				return m.beginInput("output", m.write.Output)
			}
		case "r":
			return m.start()
		case "[", "]":
			m.cycleRule(k)
		case "esc":
			m.filter, m.ruleFilter, m.fileScope, m.fileCursor = "", "", -1, 0
			m.cursor = 0
			m.rebuild()
		case "space":
			if m.state == "review" {
				if m.focus == focusFiles {
					m.toggleFile()
				} else if m.focus == focusFindings && len(m.rows) > 0 {
					r := m.rows[m.cursor]
					h := &m.snapshot.Files[r.file].Findings[r.hit]
					h.Selected = !h.Selected
				}
				m.summarize()
			}
		case "a":
			if m.state == "review" {
				m.toggleVisible()
				m.summarize()
			}
		case "w":
			switch {
			case m.state == "done":
				m.status = "本次操作已结束 · 按 r 重新扫描后再操作"
			case m.write.DryRun:
				m.status = "只读 dry-run 模式 · 不允许写入"
			case !m.snapshot.Complete:
				m.status = "扫描不完整 · 请先解决扫描错误"
			case m.stats.selected == 0:
				m.status = "尚未选中命中 · 空格选择，a 全选当前范围"
			case !m.write.InPlace && m.write.Output == "":
				m.status = "设置新的导出目录后，按 w 审阅写入摘要"
				return m.beginInput("output", "")
			default:
				m.confirm, m.confirmWrite = true, false
				m.prepareConfirmation()
			}
		}
	}
	if m.inputMode != "" {
		return m.updateInput(msg)
	}
	return m, nil
}

func (m *Model) navigate(k string) {
	index, count := &m.cursor, len(m.rows)
	page := max(1, m.layout().listHeight-4)
	if m.focus == focusFiles {
		index, count = &m.fileCursor, len(m.files)+1
		page = max(1, m.layout().bodyHeight-4)
	}
	switch k {
	case "up", "k":
		*index = max(0, *index-1)
	case "down", "j":
		*index = min(max(0, count-1), *index+1)
	case "pgup":
		*index = max(0, *index-page)
	case "pgdown":
		*index = min(max(0, count-1), *index+page)
	case "home":
		*index = 0
	case "end":
		*index = max(0, count-1)
	}
	if m.focus == focusFiles {
		m.scopeFile()
	} else {
		m.syncPreview()
	}
}

func (m *Model) cycleRule(k string) {
	index := 0
	for i, rule := range m.rules {
		if rule == m.ruleFilter {
			index = i + 1
			break
		}
	}
	delta := 1
	if k == "[" {
		delta = -1
	}
	index = (index + delta + len(m.rules) + 1) % (len(m.rules) + 1)
	m.ruleFilter = ""
	if index > 0 {
		m.ruleFilter = m.rules[index-1]
	}
	m.fileScope, m.fileCursor, m.cursor = -1, 0, 0
	m.rebuild()
}

func (m *Model) toggleVisible() {
	all := len(m.rows) > 0
	for _, r := range m.rows {
		if !m.snapshot.Files[r.file].Findings[r.hit].Selected {
			all = false
			break
		}
	}
	for _, r := range m.rows {
		m.snapshot.Files[r.file].Findings[r.hit].Selected = !all
	}
}

func (m *Model) toggleFile() {
	// The file pane selects only findings in the active search/rule scope.
	// \"All files\" selects exactly the same scope as a in the finding pane.
	m.toggleVisible()
}

func (m Model) beginInput(mode, value string) (tea.Model, tea.Cmd) {
	m.inputMode = mode
	m.savedFilter = m.filter
	m.input.SetValue(value)
	m.input.CursorEnd()
	return m, m.input.Focus()
}

func cleanInput(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, s)
}

func (m Model) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			if m.inputMode == "filter" {
				m.filter = m.savedFilter
				m.rebuild()
			}
			m.inputMode = ""
			m.input.Blur()
			return m, nil
		case "enter":
			mode, value := m.inputMode, cleanInput(m.input.Value())
			if mode != "filter" && strings.TrimSpace(value) == "" {
				m.status = "路径不能为空 · 输入路径或 Esc 取消"
				return m, nil
			}
			m.inputMode = ""
			m.input.Blur()
			switch mode {
			case "filter":
				m.filter = value
				m.cursor = 0
				m.rebuild()
			case "root":
				m.options.Root, m.filter, m.ruleFilter = value, "", ""
				return m.start()
			case "output":
				m.write.Output, m.write.InPlace, m.write.BackupDir = value, false, ""
				m.status = "导出目录已设置 · 按 w 审阅确认"
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if value := cleanInput(m.input.Value()); value != m.input.Value() {
		m.input.SetValue(value)
	}
	if m.inputMode == "filter" {
		m.filter = m.input.Value()
		m.cursor = 0
		m.rebuild()
	}
	return m, cmd
}

func (m *Model) prepareConfirmation() {
	lines := []string{
		fmt.Sprintf("%d 项替换 · %d 个含已选项的文件", m.stats.selected, m.stats.selectedFiles),
		fmt.Sprintf("%d 项未选中，将保留原值", m.stats.findings-m.stats.selected),
		"",
		"扫描路径: " + safeText(m.options.Root),
		"",
	}
	if m.write.InPlace {
		lines = append(lines, "强制备份目录:", safeText(m.write.BackupDir), "", "校验源文件后创建备份，再修改原文件。")
	} else {
		lines = append(lines, "导出目录:", safeText(m.write.Output), "", "未命中或未选择的文件也会导出副本。")
	}
	m.confirmation.SetContent(strings.Join(lines, "\n"))
	m.confirmation.GotoTop()
}

func (m Model) updateConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "tab", "shift+tab", "left", "right":
		m.confirmWrite = !m.confirmWrite
	case "esc", "n", "q":
		m.confirm = false
		m.status = "已取消写入 · 可继续审阅"
	case "enter":
		if !m.confirmWrite {
			m.confirm = false
			return m, nil
		}
		return m.beginWrite()
	case "y":
		return m.beginWrite()
	case "up", "down", "pgup", "pgdown", "home", "end", "j", "k":
		var cmd tea.Cmd
		m.confirmation, cmd = scrollViewport(m.confirmation, msg)
		return m, cmd
	}
	return m, nil
}

func scrollViewport(v viewport.Model, msg tea.KeyPressMsg) (viewport.Model, tea.Cmd) {
	switch msg.String() {
	case "home":
		v.GotoTop()
		return v, nil
	case "end":
		v.GotoBottom()
		return v, nil
	default:
		return v.Update(msg)
	}
}

func (m Model) beginWrite() (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(m.parent)
	m.cancel, m.confirm = cancel, false
	m.state, m.status = "writing", "正在校验、备份并写入…"
	snapshot, svc, w := m.snapshot, m.service, m.write
	return m, tea.Batch(func() tea.Msg { return writeDone{svc.Write(ctx, &snapshot, w)} }, m.spinner.Tick)
}
