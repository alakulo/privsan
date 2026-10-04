package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"privsan/internal/detect"
	"privsan/internal/model"
	"privsan/internal/scan"
)

func press(m Model, code rune) Model {
	next, _ := m.Update(tea.KeyPressMsg{Code: code})
	return next.(Model)
}

func workspace(t *testing.T) Model {
	t.Helper()
	m := fixture(t)
	m.options.Root = `D:\Documents\privacy-review`
	m.write.Output = `D:\Documents\sanitized-copy`
	sources := []struct{ path, content string }{
		{"customers.csv", "name,phone,email\nAlice,13800138000,alice@example.com\nBob,13900139000,bob@example.org"},
		{"notes/项目记录.md", "# 项目记录\n\n联系人：13800138000\n邮箱：team@example.com\n\n请在共享之前检查这份文档。"},
		{"server.log", "2026-10-04 INFO startup\nclient=192.168.1.24 user=dev@example.org\nstatus=ready"},
		{"readme.txt", "This file contains no detected values."},
	}
	m.snapshot.Files = nil
	for _, s := range sources {
		hits, err := detect.Find(context.Background(), []byte(s.content), m.service.Policy, 100)
		if err != nil {
			t.Fatal(err)
		}
		m.snapshot.Files = append(m.snapshot.Files, model.File{Path: s.path, Data: []byte(s.content), Findings: hits})
	}
	m.previewFile, m.previewHit = -1, -1
	m.status = "扫描完成 · 默认选中所有命中，按空格调整"
	m.rebuild()
	m.buildDetails()
	return m
}

func TestWorkspaceFocusFileScopeAndSelection(t *testing.T) {
	m := workspace(t)
	m = key(m, 'f')
	m = press(m, tea.KeyDown)
	if m.focus != focusFiles || m.fileScope != 0 || len(m.rows) != 4 {
		t.Fatalf("file scope: %+v", m.rows)
	}
	m = key(m, ' ')
	if m.stats.selected != 4 || m.stats.chosenPerFile[0] != 0 {
		t.Fatal("file selection changed other files")
	}
	m = press(m, tea.KeyEnter)
	if m.focus != focusFindings {
		t.Fatal("enter did not focus findings")
	}
	m = key(m, ' ')
	if m.stats.chosenPerFile[0] != 1 {
		t.Fatal("individual selection failed")
	}
	m = press(m, tea.KeyTab)
	if m.focus != focusPreview {
		t.Fatal("tab did not focus preview")
	}
	if strings.Contains(m.preview.GetContent(), "13800138000") || !strings.Contains(m.preview.GetContent(), "[PHONE]") {
		t.Fatal("deselected original appeared in preview")
	}
	// Viewing a clean file is supported even though it has no finding rows.
	m = key(m, 'f')
	m = press(m, tea.KeyEnd)
	if m.fileScope != 3 || len(m.rows) != 0 || !strings.Contains(m.preview.GetContent(), "no detected values") {
		t.Fatal("clean file preview unavailable")
	}
}

func TestLiveSearchRuleCombinationAndCancel(t *testing.T) {
	m := workspace(t)
	m = key(m, '/')
	m = key(m, 'e')
	m = key(m, 'm')
	if m.filter != "em" || len(m.rows) != 4 {
		t.Fatalf("live search: %q, %d", m.filter, len(m.rows))
	}
	m = press(m, tea.KeyEscape)
	if m.filter != "" || len(m.rows) != 8 {
		t.Fatal("escape did not restore search")
	}
	m = key(m, ']') // alphabetical first rule: email
	if m.ruleFilter != "email" || len(m.rows) != 4 {
		t.Fatal("rule filter failed")
	}
	m = key(m, '/')
	for _, r := range "server" {
		m = key(m, r)
	}
	m = press(m, tea.KeyEnter)
	if len(m.rows) != 1 || m.snapshot.Files[m.rows[0].file].Path != "server.log" {
		t.Fatal("combined filter failed")
	}
	m = key(m, 'a')
	if m.stats.selected != 7 {
		t.Fatal("selection escaped current scope")
	}
	m = press(m, tea.KeyEscape)
	if len(m.rows) != 8 || m.ruleFilter != "" {
		t.Fatal("clear did not restore all rows")
	}
	// A search that removes the currently scoped file resets that scope.
	m = key(m, 'f')
	m = press(m, tea.KeyDown)
	m.filter = "readme"
	m.rebuild()
	if m.fileScope != -1 || m.fileCursor != 0 || len(m.files) != 1 {
		t.Fatal("stale file scope after filter")
	}
}

func TestPasteInputAndControlCharacters(t *testing.T) {
	m := workspace(t)
	m = key(m, '/')
	next, _ := m.Update(tea.PasteMsg{Content: "email\x1b[2J\u202e"})
	m = next.(Model)
	if strings.ContainsAny(m.input.Value(), "\x1b\u202e") {
		t.Fatal("input contains terminal controls")
	}
	m = press(m, tea.KeyEscape)
	m.snapshot.Files[0].Path = "bad\x1b[2J\u202e.csv"
	m.snapshot.Files[0].Data = []byte("header\x1b[2J\u202e\n13800138000")
	hits, err := detect.Find(context.Background(), m.snapshot.Files[0].Data, m.service.Policy, 100)
	if err != nil {
		t.Fatal(err)
	}
	m.snapshot.Files[0].Findings = hits
	m.previewFile = -1
	m.rebuild()
	content := m.preview.GetContent()
	if strings.Contains(content, "\x1b") || strings.Contains(content, "\u202e") || strings.Contains(content, "13800138000") {
		t.Fatal("unsafe preview")
	}
	if !strings.Contains(content, `\x1b`) {
		t.Fatal("controls not visibly escaped")
	}
}

func TestConfirmDefaultsToCancelAndScrollsDestination(t *testing.T) {
	m := workspace(t)
	m.write.Output = "D:/" + strings.Repeat("long-directory/", 20) + "destination"
	next, _ := m.Update(tea.WindowSizeMsg{Width: 48, Height: 15})
	m = next.(Model)
	m = key(m, 'w')
	if !m.confirm || m.confirmWrite {
		t.Fatal("confirmation default must be cancel")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "执行脱敏") || !strings.Contains(view, "取消") {
		t.Fatal("buttons off screen")
	}
	m = press(m, tea.KeyEnd)
	if !strings.Contains(strings.Join(strings.Fields(ansi.Strip(m.confirmation.View())), ""), "destination") {
		t.Fatal("long destination cannot be reviewed")
	}
	m = press(m, tea.KeyEnter)
	if m.confirm || m.state != "review" {
		t.Fatal("default Enter wrote files")
	}
	m = key(m, 'a')
	m = key(m, 'w')
	if m.confirm {
		t.Fatal("empty selection allowed writing")
	}
}

func TestEditExportDirectoryInsideConfirmation(t *testing.T) {
	m := fixture(t)
	m.write.Output = "out-typo"
	m = key(m, 'w')
	m = press(m, tea.KeyTab) // execute had focus before editing
	m = key(m, 'd')
	if !m.confirm || m.confirmWrite || m.inputMode != "output" || !m.input.Focused() || m.input.Value() != "out-typo" {
		t.Fatal("confirmation did not open a focused directory editor")
	}
	m = press(m, tea.KeyEnd)
	for i := 0; i < 4; i++ {
		m = press(m, tea.KeyBackspace)
	}
	for _, r := range "fixed" {
		m = key(m, r)
	}
	if m.input.Value() != "out-fixed" || m.write.Output != "out-typo" {
		t.Fatal("draft modified destination before commit")
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || m.state != "review" || !m.confirm || m.confirmWrite || m.inputMode != "" || m.write.Output != "out-fixed" {
		t.Fatal("editing wrote files or did not return to cancel")
	}
	if !strings.Contains(m.confirmation.GetContent(), "out-fixed") || strings.Contains(m.confirmation.GetContent(), "out-typo") {
		t.Fatal("confirmation shows stale destination")
	}
	m = press(m, tea.KeyEnter)
	if m.state != "review" || m.confirm {
		t.Fatal("default enter after editing performed a write")
	}
}

func TestCancelDirectoryEditAndRejectEmptyPath(t *testing.T) {
	m := fixture(t)
	m.write.Output = "original-copy"
	m = key(m, 'w')
	m = press(m, tea.KeyTab)
	m = key(m, 'd')
	m.input.SetValue("")
	m = press(m, tea.KeyEnter)
	if m.inputMode != "output" || m.write.Output != "original-copy" || m.confirmWrite || !strings.Contains(ansi.Strip(m.modal()), "路径不能为空") {
		t.Fatal("empty directory accepted or invisible error")
	}
	m.input.SetValue("changed-draft")
	m = press(m, tea.KeyEscape)
	if !m.confirm || m.confirmWrite || m.inputMode != "" || m.write.Output != "original-copy" || !strings.Contains(m.confirmation.GetContent(), "original-copy") {
		t.Fatal("cancel did not keep original destination and confirmation")
	}
	m.write.InPlace, m.write.Output, m.write.BackupDir = true, "", "backups"
	m.prepareConfirmation()
	m = key(m, 'd')
	if m.inputMode != "" || !m.write.InPlace || m.write.BackupDir != "backups" {
		t.Fatal("in-place mode changed unexpectedly")
	}
}

func TestDirectoryEditControlsAtMinimumTerminalSize(t *testing.T) {
	m := fixture(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 48, Height: 15})
	m = next.(Model)
	m = key(m, 'w')
	if !strings.Contains(ansi.Strip(m.View().Content), "d 修改目录") {
		t.Fatal("directory edit action hidden")
	}
	m = key(m, 'd')
	m.input.SetValue("short-path")
	m.input.CursorEnd()
	view := m.View().Content
	if !strings.Contains(ansi.Strip(view), "Esc 保留原目录") || !strings.Contains(ansi.Strip(view), "Backspace") {
		t.Fatal("edit controls hidden")
	}
	if lipgloss.Width(m.input.View()) > 38 {
		t.Fatal("input cursor exceeds dialog frame")
	}
	if lipgloss.Height(view) > 15 {
		t.Fatal("editor exceeds height")
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 48 {
			t.Fatal("editor exceeds width")
		}
	}
}

func TestPreviewMapsMultilineReplacementAndScrolls(t *testing.T) {
	m := fixture(t)
	data := []byte("intro\nsecret\nblock\nphone 13800138000\n" + strings.Repeat("ordinary text\n", 80))
	m.snapshot.Files = []model.File{{Path: "multi.txt", Data: data, Findings: []model.Finding{
		{Rule: "block", Start: 6, End: 18, Line: 2, Replacement: "[BLOCK]", Selected: false},
		{Rule: "phone", Start: 25, End: 36, Line: 4, Replacement: "[PHONE]", Selected: true},
	}}}
	m.previewFile = -1
	m.rebuild()
	m.cursor = 1
	m.syncPreview()
	if m.previewLine != 2 {
		t.Fatalf("line map: %d", m.previewLine)
	}
	if strings.Contains(m.preview.GetContent(), "secret") || strings.Contains(m.preview.GetContent(), "13800138000") {
		t.Fatal("multiline original leaked")
	}
	m.focus = focusPreview
	m = press(m, tea.KeyEnd)
	if !m.preview.AtBottom() {
		t.Fatal("preview did not scroll")
	}
}

func TestAllViewsFitTerminalAndHideDetectedValues(t *testing.T) {
	sizes := [][2]int{{48, 15}, {60, 20}, {80, 24}, {99, 30}, {100, 25}, {120, 40}, {180, 50}}
	for _, size := range sizes {
		for _, dark := range []bool{true, false} {
			for _, mode := range []string{"review", "confirm", "search", "root", "output", "help", "errors", "scanning", "writing", "done", "empty", "incomplete"} {
				t.Run(fmt.Sprintf("%dx%d/%t/%s", size[0], size[1], dark, mode), func(t *testing.T) {
					m := workspace(t)
					next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					m = next.(Model)
					m.setTheme(dark)
					switch mode {
					case "confirm":
						m = key(m, 'w')
					case "search":
						m = key(m, '/')
					case "root":
						m = key(m, 'o')
					case "output":
						m = key(m, 'd')
					case "help":
						m = key(m, '?')
					case "errors":
						m = key(m, 'e')
					case "empty":
						m.snapshot.Files = nil
						m.previewFile = -1
						m.rebuild()
					case "incomplete":
						m.snapshot.Complete = false
					case "scanning", "writing", "done":
						m.state = mode
					}
					v := m.View().Content
					if mode == "help" && !strings.Contains(ansi.Strip(v), "关闭帮助") {
						t.Fatal("help close control hidden")
					}
					if h := lipgloss.Height(v); h > size[1] {
						t.Fatalf("height %d > %d", h, size[1])
					}
					for i, line := range strings.Split(v, "\n") {
						if w := lipgloss.Width(line); w > size[0] {
							t.Fatalf("line %d width %d > %d", i, w, size[0])
						}
					}
					for _, secret := range []string{"13800138000", "13900139000", "alice@example.com", "bob@example.org", "team@example.com", "192.168.1.24", "dev@example.org"} {
						if strings.Contains(ansi.Strip(v), secret) {
							t.Fatalf("leaked %s", secret)
						}
					}
					// Optional local visual captures are never stored in the repo.
					if dir := os.Getenv("PRIVSAN_TUI_CAPTURE_DIR"); dir != "" && dark && size[0] == 120 {
						if err := os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(dir, mode+".ansi"), []byte(v), 0600); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func TestBusyCancellationWaitsForCompletion(t *testing.T) {
	m := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.state = "writing"
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = next.(Model)
	if !m.quitting || cmd != nil || ctx.Err() == nil {
		t.Fatal("busy exit failed to cancel and wait")
	}
	next, cmd = m.Update(writeDone{model.Operation{Kind: "export"}})
	m = next.(Model)
	if m.state != "done" || cmd == nil {
		t.Fatal("did not exit after safe completion")
	}
}

func TestStaleProgressCannotAffectRescan(t *testing.T) {
	m := fixture(t)
	m.state = "scanning"
	m.events = make(chan scan.Progress, 1)
	stale := make(chan scan.Progress, 1)
	next, cmd := m.Update(progressMsg{scan.Progress{Completed: 99}, stale})
	m = next.(Model)
	if m.progress.Completed != 0 || cmd != nil {
		t.Fatal("stale progress affected rescan")
	}
	next, cmd = m.Update(progressMsg{scan.Progress{Completed: 1, Total: 4}, m.events})
	m = next.(Model)
	if m.progress.Completed != 1 || cmd == nil {
		t.Fatal("current scan progress ignored")
	}
}
