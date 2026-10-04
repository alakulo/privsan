// Package tui presents the offline scan/review/write workflow. Commands own I/O;
// Update owns state; View only renders sanitized, bounded terminal content.
package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"privsan/internal/model"
	"privsan/internal/scan"
	"privsan/internal/workflow"
)

type row struct{ file, hit int }
type scanDone struct{ snapshot model.Snapshot }
type writeDone struct{ operation model.Operation }
type progressMsg struct {
	progress scan.Progress
	source   <-chan scan.Progress
}
type progressClosed struct{}
type externalCancel struct{}
type startMsg struct{}

type focus int

const (
	focusFiles focus = iota
	focusFindings
	focusPreview
)

type totals struct {
	findings, selected, selectedFiles, skipped int
	perFile, chosenPerFile                     []int
	byRule                                     map[string]int
}

type Model struct {
	parent                                                context.Context
	cancel                                                context.CancelFunc
	service                                               workflow.Service
	options                                               scan.Options
	write                                                 workflow.WriteOptions
	snapshot                                              model.Snapshot
	operation                                             model.Operation
	rows                                                  []row
	files                                                 []int
	cursor, fileCursor, fileScope, width, height          int
	focus                                                 focus
	state                                                 string
	progress                                              scan.Progress
	events                                                chan scan.Progress
	filter, ruleFilter, status                            string
	rules                                                 []string
	stats                                                 totals
	inputMode, savedFilter                                string
	input                                                 textinput.Model
	confirm, confirmWrite, showErrors, showHelp, quitting bool
	preview, details                                      viewport.Model
	confirmation                                          viewport.Model
	previewFile, previewHit, previewLine                  int
	previewTargets                                        []int
	spinner                                               spinner.Model
	bar                                                   progress.Model
	help                                                  help.Model
	theme                                                 theme
}

func New(ctx context.Context, svc workflow.Service, opts scan.Options, w workflow.WriteOptions) Model {
	m := Model{parent: ctx, service: svc, options: opts, write: w, state: "idle", width: 100, height: 30,
		fileScope: -1, focus: focusFindings, previewFile: -1, previewHit: -1,
		input: textinput.New(), preview: viewport.New(), details: viewport.New(), confirmation: viewport.New(),
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		bar:     progress.New(progress.WithoutPercentage()), help: help.New()}
	m.input.CharLimit = 1024
	m.input.Prompt = "› "
	m.input.SetVirtualCursor(true)
	m.preview.SoftWrap = false
	m.details.SoftWrap = true
	m.confirmation.SoftWrap = true
	m.setTheme(true)
	m.resize()
	return m
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
	return tea.Batch(func() tea.Msg { return startMsg{} }, tea.RequestBackgroundColor,
		func() tea.Msg { <-m.parent.Done(); return externalCancel{} })
}

func listen(ch <-chan scan.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return progressClosed{}
		}
		return progressMsg{p, ch}
	}
}

func (m Model) start() (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(m.parent)
	m.cancel = cancel
	m.state, m.status = "scanning", "正在本地扫描…"
	m.rows, m.files = nil, nil
	m.snapshot = model.Snapshot{}
	m.stats = totals{}
	m.cursor, m.fileCursor, m.fileScope = 0, 0, -1
	m.previewFile, m.previewHit = -1, -1
	m.preview.SetContent("")
	m.showErrors, m.confirm, m.showHelp = false, false, false
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
	}, listen(events), m.spinner.Tick)
}

func (m *Model) summarize() {
	m.stats = totals{perFile: make([]int, len(m.snapshot.Files)), chosenPerFile: make([]int, len(m.snapshot.Files)), byRule: map[string]int{}}
	for i, f := range m.snapshot.Files {
		m.stats.perFile[i] = len(f.Findings)
		for _, h := range f.Findings {
			m.stats.findings++
			m.stats.byRule[h.Rule]++
			if h.Selected {
				m.stats.selected++
				m.stats.chosenPerFile[i]++
			}
		}
		if m.stats.chosenPerFile[i] > 0 {
			m.stats.selectedFiles++
		}
	}
	for _, n := range m.snapshot.Skipped {
		m.stats.skipped += n
	}
	m.rules = nil
	for id := range m.stats.byRule {
		m.rules = append(m.rules, id)
	}
	sort.Strings(m.rules)
}

func (m *Model) rebuild() {
	m.summarize()
	m.rows, m.files = nil, nil
	var matching []row
	needle := strings.ToLower(m.filter)
	for i, f := range m.snapshot.Files {
		pathMatch := needle == "" || strings.Contains(strings.ToLower(f.Path), needle)
		fileMatch := pathMatch && m.ruleFilter == ""
		for j, h := range f.Findings {
			if (pathMatch || strings.Contains(strings.ToLower(h.Rule), needle)) && (m.ruleFilter == "" || h.Rule == m.ruleFilter) {
				fileMatch = true
				matching = append(matching, row{i, j})
			}
		}
		if fileMatch {
			m.files = append(m.files, i)
		}
	}
	found := false
	for i, f := range m.files {
		if f == m.fileScope {
			m.fileCursor, found = i+1, true
			break
		}
	}
	if !found {
		m.fileScope, m.fileCursor = -1, 0
	}
	for _, r := range matching {
		if m.fileScope < 0 || m.fileScope == r.file {
			m.rows = append(m.rows, r)
		}
	}
	m.cursor = min(max(m.cursor, 0), max(0, len(m.rows)-1))
	m.syncPreview()
}

func (m *Model) scopeFile() {
	m.fileScope = -1
	if m.fileCursor > 0 && m.fileCursor <= len(m.files) {
		m.fileScope = m.files[m.fileCursor-1]
	}
	m.cursor = 0
	m.rebuild()
}

// safeText escapes control sequences and bidi controls, retaining readable
// Unicode. Never pass untrusted file content or paths directly to Lip Gloss.
func safeText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsGraphic(r) {
			b.WriteRune(r)
		} else {
			q := strconv.QuoteRuneToGraphic(r)
			b.WriteString(q[1 : len(q)-1])
		}
	}
	return b.String()
}

func redactedOffset(f model.File, index int) int {
	h := f.Findings[index]
	offset := h.Start
	for _, prior := range f.Findings {
		if prior.Start >= h.Start {
			break
		}
		offset += len(prior.Replacement) - (prior.End - prior.Start)
	}
	return offset
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
	offset := redactedOffset(f, index)
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

// Caching occurs in Update, never in View. Changing selection cannot reveal
// originals because the cache always renders every detected span.
func (m *Model) syncPreview() {
	file, hit := -1, -1
	if len(m.rows) > 0 {
		r := m.rows[m.cursor]
		file, hit = r.file, r.hit
	} else if m.fileScope >= 0 {
		file = m.fileScope
	}
	if file < 0 || file >= len(m.snapshot.Files) {
		m.previewFile, m.previewHit = -1, -1
		m.preview.SetContent("")
		return
	}
	f := m.snapshot.Files[file]
	if file != m.previewFile {
		data, err := model.Render(f.Data, f.Findings, true)
		if err != nil {
			m.preview.SetContent("[invalid preview]")
			return
		}
		lines := strings.Split(string(data), "\n")
		m.previewTargets = make([]int, len(f.Findings))
		delta, previous, line := 0, 0, 0
		for i, hit := range f.Findings {
			offset := hit.Start + delta
			if offset < previous || offset > len(data) {
				m.preview.SetContent("[invalid preview]")
				return
			}
			line += bytes.Count(data[previous:offset], []byte{'\n'})
			m.previewTargets[i] = line
			previous = offset
			delta += len(hit.Replacement) - (hit.End - hit.Start)
		}
		for i, s := range lines {
			lines[i] = fmt.Sprintf("%4d │ %s", i+1, safeText(strings.ReplaceAll(strings.TrimSuffix(s, "\r"), "\t", "    ")))
		}
		m.preview.SetContentLines(lines)
		m.preview.SetXOffset(0)
		m.preview.GotoTop()
	}
	if file != m.previewFile || hit != m.previewHit {
		m.previewLine = 0
		if hit >= 0 && hit < len(m.previewTargets) {
			m.previewLine = m.previewTargets[hit]
		}
		m.preview.SetYOffset(max(0, m.previewLine-m.preview.Height()/2))
	}
	m.previewFile, m.previewHit = file, hit
	m.stylePreview()
}

func (m *Model) buildDetails() {
	var lines []string
	if m.operation.Kind != "" {
		lines = append(lines, "操作: "+safeText(m.operation.Kind), fmt.Sprintf("已处理文件: %d", m.operation.Changed))
		if m.operation.RunDir != "" {
			lines = append(lines, "操作 / 恢复目录:", safeText(m.operation.RunDir))
		}
		if m.write.Output != "" {
			lines = append(lines, "导出目录:", safeText(m.write.Output))
		}
		lines = append(lines, "")
	}
	issues := append(append([]model.Issue{}, m.snapshot.Issues...), m.operation.Issues...)
	if len(issues) == 0 {
		lines = append(lines, "✓ 无错误。")
	}
	for _, issue := range issues {
		lines = append(lines, safeText(issue.Code)+"  "+safeText(issue.Path), "  "+safeText(issue.Message), "")
	}
	if len(m.snapshot.Skipped) > 0 {
		lines = append(lines, "", "跳过文件（按原因统计）")
		var keys []string
		for k := range m.snapshot.Skipped {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lines = append(lines, fmt.Sprintf("  %s: %d", safeText(k), m.snapshot.Skipped[k]))
		}
	}
	m.details.SetContent(strings.Join(lines, "\n"))
	m.details.GotoTop()
}
