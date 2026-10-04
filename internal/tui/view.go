package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"privsan/internal/model"
)

func (m Model) ink(s string) string { return lipgloss.NewStyle().Foreground(m.theme.ink).Render(s) }
func (m Model) dim(s string) string { return lipgloss.NewStyle().Foreground(m.theme.muted).Render(s) }
func (m Model) accent(s string) string {
	return lipgloss.NewStyle().Foreground(m.theme.mint).Bold(true).Render(s)
}
func (m Model) pill(s string) string {
	return lipgloss.NewStyle().Foreground(m.theme.mint).Background(m.theme.selected).Padding(0, 1).Render(s)
}

func aligned(left, right string, width int) string {
	rw := lipgloss.Width(right)
	if rw+2 >= width {
		return clipped(left, width)
	}
	left = clipped(left, width-rw-2)
	return left + strings.Repeat(" ", max(2, width-lipgloss.Width(left)-rw)) + right
}

func (m Model) mode() string {
	if m.write.DryRun {
		return "DRY RUN · 只读"
	}
	if m.options.Replacement != nil {
		if m.write.InPlace {
			return "自定义替换 + 备份"
		}
		return "导出替换副本"
	}
	if m.write.InPlace {
		return "原地脱敏 + 备份"
	}
	return "导出脱敏副本"
}

func (m Model) phase() string {
	switch m.state {
	case "idle", "scanning":
		return "扫描中"
	case "writing":
		return "写入中"
	case "done":
		if m.operation.Complete {
			return "已完成"
		}
		return "需处理"
	default:
		if !m.snapshot.Complete {
			return "扫描不完整"
		}
		return "待审阅"
	}
}

func (m Model) header(w int) string {
	brand := m.pill("P") + " " + m.accent("PRIVSAN") + " " + m.dim(model.Version)
	right := m.accent("● OFFLINE") + " " + m.dim("本地隐私工作台")
	line1 := aligned(brand, right, w)
	line2 := aligned(m.dim("ROOT  ")+m.ink(safeText(m.options.Root)), m.pill(m.mode()), w)
	return line1 + "\n" + line2
}

func (m Model) counters(l layout) string {
	files, findings := len(m.snapshot.Files), m.stats.findings
	if m.busy() && m.state == "scanning" {
		files, findings = m.progress.Completed, m.progress.Findings
	}
	issues := len(m.snapshot.Issues) + len(m.operation.Issues)
	if l.compact {
		return clipped(fmt.Sprintf("  %s 文件  ·  %s 命中  ·  %s 已选  ·  %s 问题",
			m.accent(fmt.Sprint(files)), m.accent(fmt.Sprint(findings)), m.accent(fmt.Sprint(m.stats.selected)), m.dim(fmt.Sprint(issues))), l.width)
	}
	labels := []string{"已扫描文件", "识别命中", "已选择脱敏", "问题 / 跳过"}
	if m.options.Replacement != nil {
		labels[1], labels[2] = "查找命中", "已选择替换"
	}
	values := []string{fmt.Sprint(files), fmt.Sprint(findings), fmt.Sprintf("%d / %d", m.stats.selected, findings), fmt.Sprintf("%d / %d", issues, m.stats.skipped)}
	width := (l.width - 3) / 4
	var cards []string
	for i, label := range labels {
		cw := width
		if i == 3 {
			cw = l.width - 3 - width*3
		}
		value := m.accent(values[i])
		if i == 1 {
			value = lipgloss.NewStyle().Foreground(m.theme.violet).Bold(true).Render(values[i])
		}
		if i == 3 && issues > 0 {
			value = lipgloss.NewStyle().Foreground(m.theme.red).Bold(true).Render(values[i])
		}
		cards = append(cards, m.panel(cw, 3, false, []string{aligned(value, m.dim(label), max(1, cw-4))}))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cards[0], " ", cards[1], " ", cards[2], " ", cards[3])
}

func (m Model) toolbar(w int) string {
	if m.options.Replacement != nil {
		return aligned(m.pill("自定义替换")+" "+m.dim(m.replacementSummary()), m.dim("c 编辑 · Ctrl+D 脱敏"), w)
	}
	rule := "全部规则"
	if m.ruleFilter != "" {
		rule = safeText(m.ruleFilter)
	}
	search := " / 搜索路径或规则"
	if m.filter != "" {
		search = " / " + safeText(m.filter)
	}
	return aligned(m.pill("[ "+rule+" ]")+" "+m.dim(search), m.dim("c 自定义替换 · ? 帮助"), w)
}

func (m Model) filePanel(w, h int) string {
	inner := w - 4
	lines := []string{aligned(m.accent("01 文件"), m.dim(fmt.Sprintf("%d", len(m.files))), inner), m.dim("↑↓ 定位 · Enter 审阅")}
	size := max(1, h-5)
	start := windowStart(m.fileCursor, len(m.files)+1, size)
	for i := start; i < min(len(m.files)+1, start+size); i++ {
		name, count, selected := "全部文件", m.stats.findings, m.stats.selected
		if i > 0 {
			f := m.files[i-1]
			name = safeText(m.snapshot.Files[f].Path)
			count, selected = m.stats.perFile[f], m.stats.chosenPerFile[f]
		}
		mark := "○"
		if count > 0 && selected == count {
			mark = "✓"
		} else if selected > 0 {
			mark = "◐"
		}
		prefix := "  "
		if i == m.fileCursor {
			prefix = "› "
		}
		text := aligned(prefix+mark+" "+name, m.dim(fmt.Sprintf("%d", count)), inner)
		if i == m.fileCursor {
			text = lipgloss.NewStyle().Foreground(m.theme.mint).Background(m.theme.selected).Bold(true).Render(fitted(text, inner))
		} else {
			text = m.ink(text)
		}
		lines = append(lines, text)
	}
	if len(m.files) == 0 {
		lines = append(lines, m.dim("没有匹配的文件"))
	}
	return m.panel(w, h, m.focus == focusFiles, lines)
}

func (m Model) findingPanel(w, h int) string {
	inner := w - 4
	title := "02 命中审阅"
	if m.fileScope >= 0 && m.fileScope < len(m.snapshot.Files) {
		title += " · " + safeText(m.snapshot.Files[m.fileScope].Path)
	}
	lines := []string{aligned(m.accent(title), m.dim(fmt.Sprintf("%d 项", len(m.rows))), inner), m.dim("选择  规则         位置 / 文件")}
	size := max(1, h-5)
	start := windowStart(m.cursor, len(m.rows), size)
	for i := start; i < min(len(m.rows), start+size); i++ {
		r := m.rows[i]
		f := m.snapshot.Files[r.file]
		hit := f.Findings[r.hit]
		mark := "[ ]"
		if hit.Selected {
			mark = "[✓]"
		}
		pointer := " "
		if i == m.cursor {
			pointer = "›"
		}
		rule := fitted(safeText(hit.Rule), min(12, max(5, inner/4)))
		where := fmt.Sprintf("L%d:%d", hit.Line, hit.Column)
		text := pointer + " " + mark + " " + rule + " " + where
		if m.fileScope < 0 {
			text += "  " + safeText(f.Path)
		}
		text = fitted(text, inner)
		if i == m.cursor {
			text = lipgloss.NewStyle().Foreground(m.theme.ink).Background(m.theme.selected).Bold(true).Render(text)
		} else if !hit.Selected {
			text = m.dim(text)
		} else {
			text = m.ink(text)
		}
		lines = append(lines, text)
	}
	if len(m.rows) == 0 {
		if m.stats.findings == 0 {
			lines = append(lines, m.accent("✓ 当前范围没有识别命中"), m.dim("可在文件面板查看内容，o 更换路径"))
			if m.options.Replacement != nil {
				lines[len(lines)-2] = m.accent("未找到匹配文本 · c 编辑查找条件")
			}
		} else {
			lines = append(lines, m.dim("没有匹配项 · Esc 清除筛选"))
		}
	}
	for len(lines) < max(1, h-3) {
		lines = append(lines, "")
	}
	position := 0
	if len(m.rows) > 0 {
		position = m.cursor + 1
	}
	lines = append(lines, m.dim(fmt.Sprintf("%d / %d  ·  空格选择  ·  a 当前范围全选", position, len(m.rows))))
	return m.panel(w, h, m.focus == focusFindings, lines)
}

func (m Model) previewPanel(w, h int) string {
	inner := w - 4
	lines := []string{aligned(m.accent("03 安全预览"), m.dim(fmt.Sprintf("%.0f%%", m.preview.ScrollPercent()*100)), inner)}
	hint := "所有已识别值均隐藏 · Tab 聚焦后滚动"
	if m.previewFile >= 0 {
		hint = safeText(m.snapshot.Files[m.previewFile].Path) + " · 所有已识别值均隐藏"
	}
	lines = append(lines, m.dim(hint))
	if m.previewFile < 0 {
		lines = append(lines, m.dim("选择一个命中或文件以查看脱敏内容"))
	} else {
		lines = append(lines, strings.Split(m.preview.View(), "\n")...)
	}
	return m.panel(w, h, m.focus == focusPreview, lines)
}

func (m Model) body(l layout) string {
	if m.showErrors {
		lines := []string{m.accent("操作结果与诊断"), m.dim("↑↓ / PgUp / PgDn 滚动 · e / Esc 返回")}
		lines = append(lines, strings.Split(m.details.View(), "\n")...)
		return m.panel(l.width, l.bodyHeight, true, lines)
	}
	var main string
	if l.split {
		top := m.findingPanel(l.rightWidth, l.listHeight)
		if !l.sidebar && m.focus == focusFiles {
			top = m.filePanel(l.rightWidth, l.listHeight)
		}
		main = lipgloss.JoinVertical(lipgloss.Left, top, m.previewPanel(l.rightWidth, l.previewHeight))
	} else {
		switch {
		case !l.sidebar && m.focus == focusFiles:
			main = m.filePanel(l.rightWidth, l.bodyHeight)
		case m.focus == focusPreview:
			main = m.previewPanel(l.rightWidth, l.bodyHeight)
		default:
			main = m.findingPanel(l.rightWidth, l.bodyHeight)
		}
	}
	if l.sidebar {
		return lipgloss.JoinHorizontal(lipgloss.Top, m.filePanel(l.leftWidth, l.bodyHeight), " ", main)
	}
	return main
}

func (m Model) footer(w int) string {
	status := m.status
	if status == "" {
		status = "Tab 切换文件 / 命中 / 预览 · 按 ? 查看所有快捷键"
	}
	color := m.theme.muted
	if m.state == "done" && m.operation.Complete {
		color = m.theme.mint
	} else if m.state == "review" && !m.snapshot.Complete {
		color = m.theme.amber
	}
	line1 := lipgloss.NewStyle().Foreground(color).Render(clipped(status, w))
	line2 := m.help.ShortHelpView(shortKeys)
	line3 := m.dim("预览隐藏全部命中；写入仅替换已选项。")
	if m.options.Replacement != nil {
		line3 = m.dim("预览展示全部替换候选并隐藏隐私；仅已选替换写入。")
	}
	return line1 + "\n" + clipped(line2, w) + "\n" + clipped(line3, w)
}

func (m Model) modal() string {
	w := max(1, min(72, m.width-6))
	inner := max(1, w-4)
	var lines []string
	switch {
	case m.replaceForm:
		return m.replaceDialog(w)
	case m.inputMode != "":
		title, description := "搜索", "实时筛选文件路径或规则 ID"
		if m.inputMode == "root" {
			title, description = "打开扫描路径", "输入本地文件或目录；确认后开始新的扫描"
		}
		if m.inputMode == "output" {
			title, description = "设置导出目录", "输入尚不存在的目录，用于保存处理后的副本"
		}
		lines = []string{m.accent(title), m.dim(description), "", m.input.View(), "", m.dim("Enter 确定  ·  Esc 取消  ·  ←→ 编辑")}
		if m.inputMode == "output" {
			lines[4] = m.dim("←→ / Home / End 定位 · Backspace 删除")
			if m.confirm {
				lines[5] = m.dim("Enter 更新并返回确认 · Esc 保留原目录")
			}
		}
		if m.inputError != "" {
			lines[4] = m.accent(m.inputError)
		}
		if m.inputMode == "filter" {
			lines = append(lines, m.dim(fmt.Sprintf("%d 个文件 · %d 项命中匹配", len(m.files), len(m.rows))))
		}
	case m.confirm:
		title := "确认导出脱敏副本"
		if m.write.InPlace {
			title = "确认原地脱敏"
		}
		if m.options.Replacement != nil {
			title = "确认自定义替换"
		}
		lines = []string{m.accent(title)}
		lines = append(lines, strings.Split(m.confirmation.View(), "\n")...)
		cancel, execute := m.dim(" 取消 "), m.dim(" 执行脱敏 ")
		if m.options.Replacement != nil {
			execute = m.dim(" 执行替换 ")
		}
		if m.confirmWrite {
			execute = m.pill("执行脱敏")
			if m.options.Replacement != nil {
				execute = m.pill("执行替换")
			}
		} else {
			cancel = m.pill("取消")
		}
		actions := cancel + "  " + execute
		if !m.write.InPlace {
			actions += "  " + m.dim("d 修改目录")
		}
		lines = append(lines, actions, m.dim("↑↓ 滚动 · Tab 切换 · y 执行 · Esc 返回"))
	case m.showHelp:
		lines = []string{m.accent("键盘操作"), m.dim("文件 → 命中 → 安全预览 → 确认写入"), "",
			m.ink("Tab / Shift+Tab") + "  切换面板",
			m.ink("↑↓ j/k · PgUp/PgDn · Home/End") + "  导航",
			m.ink("Space / a") + "  选择当前项 / 当前范围全部",
			m.ink("/ · [ / ] · Esc") + "  搜索 / 切换规则 / 清筛选",
			m.ink("f / Enter") + "  文件面板 / 返回命中审阅",
			m.ink("←→ h/l") + "  预览水平滚动（Tab 聚焦）",
			m.ink("o / d / r") + "  扫描路径 / 导出目录 / 重扫",
			m.ink("w / e") + "  确认写入 / 结果与诊断",
			m.ink("? / Esc / q") + "  关闭帮助"}
		lines[2] = m.ink("c / Ctrl+D") + "  自定义替换 / 隐私脱敏"
		if m.width < 70 {
			lines[5] = m.ink("Space / a") + "  切换项 / 当前范围"
			lines[6] = m.ink("/ · [ / ] · Esc") + "  搜索 / 规则 / 清除"
			lines[7] = m.ink("f / Enter") + "  文件 / 命中"
			lines[9] = m.ink("o / d / r") + "  路径 / 导出 / 重扫"
		}
	case m.busy():
		title := "正在本地扫描"
		detail := fmt.Sprintf("%d / %d 文件  ·  %d 项命中", m.progress.Completed, m.progress.Total, m.progress.Findings)
		if m.state == "writing" {
			title, detail = "正在安全写入", "校验源文件 → 备份 / 导出 → 提交单个文件"
		}
		if m.quitting {
			title, detail = "正在安全停止", "等待当前文件操作结束，请稍候"
		}
		lines = []string{m.spinner.View() + " " + m.accent(title), "", m.ink(detail)}
		if m.state == "scanning" {
			pct := float64(m.progress.Completed) / float64(max(1, m.progress.Total))
			lines = append(lines, "", m.bar.ViewAs(pct))
		}
		lines = append(lines, "", m.dim("全程离线 · 文件内容仅在本机处理"), m.dim("q / Esc / Ctrl+C 请求取消"))
	default:
		return ""
	}
	// Dialog paths may wrap, rather than truncate away the destination. The
	// final height is constrained to the terminal, including its frame.
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(lipgloss.NewStyle().Width(inner).Render(line), "\n")...)
	}
	height := min(len(wrapped)+2, max(3, m.height-1))
	return m.panel(w, height, true, wrapped)
}

func (m Model) View() tea.View {
	t := m.theme
	if m.width < 48 || m.height < 15 {
		content := "PRIVSAN\n终端至少需要 48 列 × 15 行。\n请调整窗口大小；Ctrl+C 退出。"
		lines := strings.Split(content, "\n")
		for i := range lines {
			lines[i] = clipped(lines[i], max(1, m.width))
		}
		v := tea.NewView(strings.Join(lines[:min(len(lines), max(1, m.height))], "\n"))
		v.AltScreen = true
		return v
	}
	l := m.layout()
	content := m.header(l.width) + "\n" + m.counters(l) + "\n" + m.toolbar(l.width) + "\n\n" + m.body(l) + "\n" + m.footer(l.width)
	// One cell of margin on each side; the view is exactly the terminal's
	// height. ANSI-aware truncation is the last guard, not the layout engine.
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = " " + fitted(lines[i], l.width) + " "
	}
	content = strings.Join(lines, "\n")
	if dialog := m.modal(); dialog != "" {
		content = lipgloss.NewCompositor(lipgloss.NewLayer(content), lipgloss.NewLayer(dialog).
			X((m.width-lipgloss.Width(dialog))/2).Y(max(0, (m.height-lipgloss.Height(dialog))/2)).Z(1)).Render()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.BackgroundColor, v.ForegroundColor = t.bg, t.ink
	v.WindowTitle = "Privsan · 本地隐私工作台"
	return v
}
