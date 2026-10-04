package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"privsan/internal/detect"
	"privsan/internal/model"
	"privsan/internal/policy"
	"privsan/internal/replace"
)

func (m Model) beginReplace() (tea.Model, tea.Cmd) {
	m.replaceForm, m.replaceFocus, m.replaceError = true, 0, ""
	m.replacePath = ""
	if m.fileScope >= 0 && m.fileScope < len(m.snapshot.Files) {
		m.replacePath = m.snapshot.Files[m.fileScope].Path
	} else if m.previewFile >= 0 && m.previewFile < len(m.snapshot.Files) {
		m.replacePath = m.snapshot.Files[m.previewFile].Path
	}
	m.replaceAll = m.fileScope < 0 || m.replacePath == ""
	if m.options.Replacement != nil {
		opts := m.options.Replacement.Options()
		if cleanInput(opts.Find) != opts.Find || cleanInput(opts.With) != opts.With {
			m.replaceInputs[0].SetValue("")
			m.replaceInputs[1].SetValue("")
			m.replaceError = "当前规则含多行/控制字符；请用 CLI 编辑，或新建单行规则"
		} else {
			m.replaceInputs[0].SetValue(opts.Find)
			m.replaceInputs[1].SetValue(opts.With)
		}
		m.replaceRegex, m.replaceIgnoreCase = opts.Regex, opts.IgnoreCase
		if m.fileScope < 0 && m.options.ReplacePath != "" {
			m.replacePath, m.replaceAll = m.options.ReplacePath, false
		}
	}
	m.replaceInputs[1].Blur()
	m.replaceInputs[0].CursorEnd()
	return m, m.replaceInputs[0].Focus()
}

func (m Model) updateReplace(msg tea.Msg) (tea.Model, tea.Cmd) {
	if paste, ok := msg.(tea.PasteMsg); ok && cleanInput(paste.Content) != paste.Content {
		m.replaceError = "TUI 仅支持无控制字符的单行输入；多行替换请使用 CLI"
		return m, nil
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			m.replaceForm = false
			for i := range m.replaceInputs {
				m.replaceInputs[i].Blur()
			}
			return m, nil
		case "tab", "shift+tab":
			m.replaceInputs[m.replaceFocus].Blur()
			m.replaceFocus = 1 - m.replaceFocus
			return m, m.replaceInputs[m.replaceFocus].Focus()
		case "ctrl+r":
			m.replaceRegex = !m.replaceRegex
			return m, nil
		case "ctrl+g":
			m.replaceIgnoreCase = !m.replaceIgnoreCase
			return m, nil
		case "ctrl+s":
			if m.replacePath != "" {
				m.replaceAll = !m.replaceAll
			}
			return m, nil
		case "enter":
			plan, err := replace.Compile(replace.Options{Find: m.replaceInputs[0].Value(), With: m.replaceInputs[1].Value(), Regex: m.replaceRegex, IgnoreCase: m.replaceIgnoreCase})
			if err != nil {
				m.replaceError = err.Error()
				return m, nil
			}
			m.options.Replacement, m.options.ReplacePath = plan, ""
			if !m.replaceAll {
				m.options.ReplacePath = m.replacePath
			}
			m.filter, m.ruleFilter = "", ""
			for i := range m.replaceInputs {
				m.replaceInputs[i].Blur()
			}
			return m.start()
		}
	}
	var cmd tea.Cmd
	m.replaceInputs[m.replaceFocus], cmd = m.replaceInputs[m.replaceFocus].Update(msg)
	input := &m.replaceInputs[m.replaceFocus]
	if value := cleanInput(input.Value()); value != input.Value() {
		input.SetValue(value)
	}
	m.replaceError = ""
	return m, cmd
}

func (m Model) replaceDialog(w int) string {
	mode, caseMode, scope := "原文", "区分大小写", "全部文件"
	if m.replaceRegex {
		mode = "Go 正则"
	}
	if m.replaceIgnoreCase {
		caseMode = "忽略大小写"
	}
	if !m.replaceAll {
		scope = safeText(m.replacePath)
	}
	message := "空替换值 = 删除；$1 与反斜杠按字面量处理"
	if m.replaceError != "" {
		message = safeText(m.replaceError)
	}
	findLabel, withLabel := m.dim("查找"), m.dim("替换为")
	if m.replaceFocus == 0 {
		findLabel = m.accent("查找 · 编辑中")
	} else {
		withLabel = m.accent("替换为 · 编辑中")
	}
	lines := []string{m.accent("自定义查找替换"), m.dim("预览 → 逐项选择 → 确认写入"),
		findLabel, m.replaceInputs[0].View(),
		withLabel, m.replaceInputs[1].View(),
		m.pill(mode) + " " + m.dim(caseMode), m.dim("范围: ") + m.ink(scope),
		m.dim(message), m.dim("Tab 切换输入 · Enter 预览 · Esc 取消"),
		m.dim("Ctrl+R 正则 · Ctrl+G 大小写 · Ctrl+S 范围")}
	return m.panel(w, min(len(lines)+2, m.height-1), true, lines)
}

// Both source and proposed-result privacy spans are hidden. Mapping source
// spans prevents a partial replacement from exposing the rest of a phone/email.
func prepareReplacementPreview(ctx context.Context, f *model.File, p *policy.Policy) {
	data, err := model.Render(f.Data, f.Findings, true)
	if err != nil {
		f.PreviewBlocked = true
		return
	}
	sourceMasks, err := detect.Find(ctx, f.Data, p, p.Config.Limits.MaxFindings)
	if err != nil {
		f.PreviewBlocked = true
		return
	}
	resultMasks, err := detect.Find(ctx, data, p, p.Config.Limits.MaxFindings)
	if err != nil {
		f.PreviewBlocked = true
		return
	}
	index, delta := 0, 0
	mapOffset := func(offset int, end bool) int {
		for index < len(f.Findings) && f.Findings[index].End <= offset {
			h := f.Findings[index]
			delta += len(h.Replacement) - (h.End - h.Start)
			index++
		}
		if index < len(f.Findings) {
			h := f.Findings[index]
			if h.Start < offset && offset < h.End {
				if end {
					return h.Start + delta + len(h.Replacement)
				}
				return h.Start + delta
			}
		}
		return offset + delta
	}
	for _, h := range sourceMasks {
		a, b := mapOffset(h.Start, false), mapOffset(h.End, true)
		if a < b {
			resultMasks = append(resultMasks, model.Finding{Start: a, End: b, Replacement: "[REDACTED]"})
		}
	}
	sort.Slice(resultMasks, func(i, j int) bool { return resultMasks[i].Start < resultMasks[j].Start })
	var merged []model.Finding
	for _, h := range resultMasks {
		if len(merged) > 0 && h.Start < merged[len(merged)-1].End {
			merged[len(merged)-1].End = max(h.End, merged[len(merged)-1].End)
		} else {
			h.Replacement = "[REDACTED]"
			merged = append(merged, h)
		}
	}
	f.PreviewMasks = merged
}

func (m Model) replacementSummary() string {
	if m.options.Replacement == nil {
		return ""
	}
	opts := m.options.Replacement.Options()
	mode := "原文"
	if opts.Regex {
		mode = "正则"
	}
	if opts.IgnoreCase {
		mode += " / 忽略大小写"
	}
	scope := "全部文件"
	if m.options.ReplacePath != "" {
		scope = safeText(m.options.ReplacePath)
	}
	// Search text can itself be a secret; the background toolbar shows settings.
	return fmt.Sprintf("%s · %s", mode, strings.TrimSpace(scope))
}
