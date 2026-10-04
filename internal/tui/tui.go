package tui

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"privsan/internal/model"
	"privsan/internal/scan"
	"privsan/internal/workflow"
)

type row struct{ file, hit int }
type scanDone struct{ snapshot model.Snapshot }
type writeDone struct{ operation model.Operation }
type progressMsg scan.Progress
type progressClosed struct{}
type externalCancel struct{}
type Model struct {
	parent                           context.Context
	cancel                           context.CancelFunc
	service                          workflow.Service
	options                          scan.Options
	write                            workflow.WriteOptions
	snapshot                         model.Snapshot
	operation                        model.Operation
	rows                             []row
	cursor, width, height            int
	state                            string
	progress                         scan.Progress
	events                           chan scan.Progress
	inputMode, input, filter, status string
	confirm, showErrors, quitting    bool
}

func New(ctx context.Context, svc workflow.Service, opts scan.Options, w workflow.WriteOptions) Model {
	return Model{parent: ctx, service: svc, options: opts, write: w, state: "idle", width: 100, height: 30}
}
func Run(ctx context.Context, svc workflow.Service, opts scan.Options, w workflow.WriteOptions, in io.Reader, out io.Writer) (model.Snapshot, model.Operation, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m, err := tea.NewProgram(New(runCtx, svc, opts, w), tea.WithInput(in), tea.WithOutput(out)).Run()
	if err != nil {
		return model.Snapshot{}, model.Operation{}, err
	}
	result := m.(Model)
	return result.snapshot, result.operation, nil
}
func (m Model) Init() tea.Cmd {
	return tea.Batch(func() tea.Msg { return startMsg{} }, func() tea.Msg { <-m.parent.Done(); return externalCancel{} })
}

type startMsg struct{}

func listen(ch <-chan scan.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return progressClosed{}
		}
		return progressMsg(p)
	}
}
func (m Model) start() (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(m.parent)
	m.cancel = cancel
	m.state = "scanning"
	m.status = "正在本地扫描…"
	m.rows = nil
	m.cursor = 0
	m.progress = scan.Progress{}
	m.operation = model.Operation{}
	events := make(chan scan.Progress, 8)
	m.events = events
	svc, opts := m.service, m.options
	return m, tea.Batch(func() tea.Msg {
		defer close(events)
		result := svc.Scan(ctx, opts, func(p scan.Progress) {
			select {
			case events <- p:
			default:
			}
		})
		return scanDone{result}
	}, listen(events))
}
func (m *Model) rebuild() {
	m.rows = nil
	needle := strings.ToLower(m.filter)
	for i, f := range m.snapshot.Files {
		for j, h := range f.Findings {
			if needle == "" || strings.Contains(strings.ToLower(f.Path), needle) || strings.Contains(h.Rule, needle) {
				m.rows = append(m.rows, row{i, j})
			}
		}
	}
	m.cursor = min(max(m.cursor, 0), max(0, len(m.rows)-1))
}
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case externalCancel:
		if m.cancel != nil {
			m.cancel()
		}
		if m.state == "scanning" || m.state == "writing" {
			m.quitting = true
			m.status = "正在安全停止…"
			return m, nil
		}
		return m, tea.Quit
	case startMsg:
		return m.start()
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case progressMsg:
		m.progress = scan.Progress(msg)
		if m.state == "scanning" {
			return m, listen(m.events)
		}
	case scanDone:
		if m.cancel != nil {
			m.cancel()
		}
		m.snapshot = msg.snapshot
		m.rebuild()
		m.state = "review"
		if m.snapshot.Complete {
			m.status = "扫描完成。默认勾选所有命中；预览隐藏所有识别值。"
		} else {
			m.status = "扫描未完整成功，禁止写入。按 e 查看错误。"
		}
		if m.quitting {
			return m, tea.Quit
		}
	case writeDone:
		if m.cancel != nil {
			m.cancel()
		}
		m.operation = msg.operation
		m.state = "done"
		if msg.operation.Complete {
			m.status = fmt.Sprintf("完成：%d 个文件。按 e 查看操作与备份路径。", msg.operation.Changed)
		} else {
			m.status = "操作未完整成功。按 e 查看错误与恢复目录。"
		}
		if m.quitting {
			return m, tea.Quit
		}
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			if m.state == "scanning" || m.state == "writing" {
				m.quitting = true
				m.status = "正在安全停止，等待当前文件完成…"
				return m, nil
			}
			return m, tea.Quit
		}
		if m.state == "scanning" || m.state == "writing" {
			if key == "q" || key == "esc" {
				if m.cancel != nil {
					m.cancel()
				}
				m.quitting = true
				m.status = "取消中…"
			}
			return m, nil
		}
		if m.inputMode != "" {
			switch key {
			case "esc":
				m.inputMode = ""
				m.input = ""
			case "backspace":
				if len(m.input) > 0 {
					_, n := utf8.DecodeLastRuneInString(m.input)
					m.input = m.input[:len(m.input)-n]
				}
			case "enter":
				mode, value := m.inputMode, m.input
				m.inputMode = ""
				m.input = ""
				switch mode {
				case "filter":
					m.filter = value
					m.cursor = 0
					m.rebuild()
				case "root":
					if strings.TrimSpace(value) != "" {
						m.options.Root = value
						m.filter = ""
						return m.start()
					}
				case "output":
					if strings.TrimSpace(value) != "" {
						m.write.Output = value
						m.write.InPlace = false
						m.write.BackupDir = ""
						m.status = "导出目录已设置；按 w 审阅确认。"
					}
				}
			default:
				if len(m.input) < 1024 {
					for _, r := range msg.Text {
						if r >= 32 && r != 127 {
							m.input += string(r)
						}
					}
				}
			}
			return m, nil
		}
		if m.confirm {
			m.confirm = false
			if key == "y" {
				ctx, cancel := context.WithCancel(m.parent)
				m.cancel = cancel
				m.state = "writing"
				m.status = "正在校验、备份并写入…"
				snapshot, svc, w := m.snapshot, m.service, m.write
				return m, func() tea.Msg { return writeDone{svc.Write(ctx, &snapshot, w)} }
			}
			return m, nil
		}
		switch key {
		case "q":
			return m, tea.Quit
		case "e":
			m.showErrors = !m.showErrors
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(max(0, len(m.rows)-1), m.cursor+1)
		case "pgup":
			m.cursor = max(0, m.cursor-max(1, m.height-17))
		case "pgdown":
			m.cursor = min(max(0, len(m.rows)-1), m.cursor+max(1, m.height-17))
		case "home":
			m.cursor = 0
		case "end":
			m.cursor = max(0, len(m.rows)-1)
		case "/":
			m.inputMode = "filter"
			m.input = m.filter
		case "esc":
			m.filter = ""
			m.rebuild()
			m.showErrors = false
		case "o":
			m.inputMode = "root"
			m.input = m.options.Root
		case "d":
			if m.state != "done" {
				m.inputMode = "output"
				m.input = m.write.Output
			}
		case "r":
			return m.start()
		case "space":
			if m.state == "review" && len(m.rows) > 0 {
				r := m.rows[m.cursor]
				h := &m.snapshot.Files[r.file].Findings[r.hit]
				h.Selected = !h.Selected
			}
		case "a":
			if m.state == "review" {
				all := true
				for _, r := range m.rows {
					if !m.snapshot.Files[r.file].Findings[r.hit].Selected {
						all = false
					}
				}
				for _, r := range m.rows {
					m.snapshot.Files[r.file].Findings[r.hit].Selected = !all
				}
			}
		case "w":
			if m.state == "done" {
				m.status = "本次操作已结束。按 r 重新扫描后再操作。"
			} else if m.write.DryRun {
				m.status = "dry-run 禁止写入。"
			} else if !m.snapshot.Complete {
				m.status = "请先解决扫描错误。"
			} else if !m.write.InPlace && m.write.Output == "" {
				m.inputMode = "output"
				m.input = ""
				m.status = "请输入新的导出目录，然后按 w 确认。"
			} else {
				m.confirm = true
			}
		}
	}
	return m, nil
}

// PreviewLine maps original offsets into fully redacted text, including
// multiline custom matches, and never displays deselected originals.
func PreviewLine(f model.File, index int) string {
	if index < 0 || index >= len(f.Findings) {
		return ""
	}
	data, err := model.Render(f.Data, f.Findings, true)
	if err != nil {
		return "[invalid preview]"
	}
	h := f.Findings[index]
	offset := h.Start
	for _, prior := range f.Findings {
		if prior.Start >= h.Start {
			break
		}
		offset += len(prior.Replacement) - (prior.End - prior.Start)
	}
	if offset < 0 || offset > len(data) {
		return "[invalid preview]"
	}
	start := strings.LastIndex(string(data[:offset]), "\n") + 1
	end := strings.Index(string(data[offset:]), "\n")
	if end < 0 {
		end = len(data)
	} else {
		end += offset
	}
	return strconv.Quote(string(data[start:end]))
}
func (m Model) View() tea.View {
	if m.width < 48 || m.height < 15 {
		v := tea.NewView("Privsan\n终端至少需要 48 列 × 15 行。\n请调整窗口大小；Ctrl+C 退出。")
		v.AltScreen = true
		return v
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  PRIVSAN  %s  ·  本地隐私工作台\n", model.Version)
	fmt.Fprintf(&b, "  路径 %s\n", strconv.Quote(m.options.Root))
	mode := "导出副本"
	if m.write.InPlace {
		mode = "原地修改 + 强制备份"
	}
	if m.write.DryRun {
		mode = "只读 dry-run"
	}
	fmt.Fprintf(&b, "  模式 %s  |  %s\n", mode, m.state)
	if m.state == "scanning" {
		fmt.Fprintf(&b, "\n  已扫描 %d / %d 文件，%d 项命中\n", m.progress.Completed, m.progress.Total, m.progress.Findings)
	} else {
		selected, total := 0, 0
		for _, f := range m.snapshot.Files {
			for _, h := range f.Findings {
				total++
				if h.Selected {
					selected++
				}
			}
		}
		fmt.Fprintf(&b, "  %d 文件 · %d 命中 · %d 已选 · 筛选 %q\n\n", len(m.snapshot.Files), total, selected, m.filter)
		if m.showErrors {
			if m.operation.RunDir != "" {
				fmt.Fprintf(&b, "  操作目录 %q\n", m.operation.RunDir)
			}
			issues := append([]model.Issue{}, m.snapshot.Issues...)
			issues = append(issues, m.operation.Issues...)
			if len(issues) == 0 {
				b.WriteString("  无错误。\n")
			}
			for i, v := range issues {
				if i >= m.height-13 {
					fmt.Fprintf(&b, "  … 另有 %d 条，退出后查看完整报告\n", len(issues)-i)
					break
				}
				fmt.Fprintf(&b, "  %s %q: %s\n", v.Code, v.Path, v.Message)
			}
		} else {
			size := max(1, m.height-16)
			start := max(0, m.cursor-size/2)
			end := min(len(m.rows), start+size)
			if len(m.rows) == 0 {
				b.WriteString("  当前范围没有命中。\n")
			}
			for i := start; i < end; i++ {
				r := m.rows[i]
				f := m.snapshot.Files[r.file]
				h := f.Findings[r.hit]
				pointer, check := " ", " "
				if i == m.cursor {
					pointer = "›"
				}
				if h.Selected {
					check = "x"
				}
				fmt.Fprintf(&b, " %s [%s] %-8s %s:%d:%d\n", pointer, check, h.Rule, strconv.Quote(f.Path), h.Line, h.Column)
			}
			b.WriteString("\n  安全预览（包括未选项在内的所有命中均隐藏）：\n")
			if len(m.rows) > 0 {
				r := m.rows[m.cursor]
				fmt.Fprintf(&b, "  %s\n", PreviewLine(m.snapshot.Files[r.file], r.hit))
			}
		}
	}
	b.WriteString("\n  ↑↓/PgUp/PgDn 移动 · 空格 勾选 · a 当前筛选全选 · / 文件/规则筛选\n  o 输入路径 · d 导出目录 · r 重扫 · e 错误 · w 执行 · q 退出\n")
	if m.inputMode != "" {
		fmt.Fprintf(&b, "  输入 %s: %s▏  [Enter 确定 / Esc 取消]\n", m.inputMode, strconv.Quote(m.input))
	} else if m.confirm {
		target := m.write.Output
		if m.write.InPlace {
			target = m.write.BackupDir
		}
		fmt.Fprintf(&b, "  确认 %s？目录 %q  [y 执行 / 其他键取消]\n", mode, target)
	} else {
		fmt.Fprintf(&b, "  %s\n", m.status)
	}
	lines := strings.Split(b.String(), "\n")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], m.width-1, "…")
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}
