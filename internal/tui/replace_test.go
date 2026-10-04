package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"privsan/internal/model"
	"privsan/internal/replace"
)

func ctrl(m Model, r rune) Model {
	next, _ := m.Update(tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl})
	return next.(Model)
}

func TestReplaceFormIsolationValidationAndScope(t *testing.T) {
	m := workspace(t)
	m = key(m, 'f')
	m = press(m, tea.KeyDown)
	m = key(m, 'c')
	if !m.replaceForm || m.replaceAll || m.replacePath != "customers.csv" {
		t.Fatal("scope not captured")
	}
	for _, r := range "cqword" {
		m = key(m, r)
	}
	if m.replaceInputs[0].Value() != "cqword" || m.confirm || m.quitting || m.focus != focusFiles {
		t.Fatal("form keys escaped to workspace")
	}
	m = press(m, tea.KeyTab)
	for _, r := range "new" {
		m = key(m, r)
	}
	m = ctrl(m, 'g')
	m = ctrl(m, 'r')
	m = ctrl(m, 's')
	if !m.replaceIgnoreCase || !m.replaceRegex || !m.replaceAll || m.replaceInputs[1].Value() != "new" {
		t.Fatal("form options")
	}
	m = press(m, tea.KeyEscape)
	if m.replaceForm || m.options.Replacement != nil {
		t.Fatal("cancel committed draft")
	}
	m = key(m, 'c')
	m.replaceInputs[0].SetValue("secret(")
	m.replaceRegex = true
	m = press(m, tea.KeyEnter)
	if !m.replaceForm || m.replaceError == "" || strings.Contains(m.replaceError, "secret(") {
		t.Fatal("invalid regex")
	}
	m.replaceInputs[0].SetValue("")
	m.replaceRegex = false
	m = press(m, tea.KeyEnter)
	if !m.replaceForm {
		t.Fatal("empty find accepted")
	}
	next, _ := m.Update(tea.PasteMsg{Content: "two\nlines"})
	m = next.(Model)
	if m.replaceInputs[0].Value() != "" || !strings.Contains(m.replaceError, "单行") {
		t.Fatal("multiline paste silently changed")
	}
}

func TestReplaceFormSchedulesScanAndSelectedWrite(t *testing.T) {
	m := fixture(t)
	m.options.Root = t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(m.options.Root, name), []byte("old old 13800138000"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.snapshot = m.service.Scan(context.Background(), m.options, nil)
	m.rebuild()
	m = key(m, 'f')
	m = press(m, tea.KeyDown)
	m = key(m, 'c')
	m.replaceInputs[0].SetValue("old")
	m.replaceInputs[1].SetValue("new")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.state != "scanning" || cmd == nil || m.replaceForm || m.options.ReplacePath != "a.txt" {
		t.Fatal("scan not scheduled")
	}
	batch := cmd().(tea.BatchMsg)
	done := batch[0]().(scanDone)
	next, _ = m.Update(done)
	m = next.(Model)
	if !m.snapshot.Complete || m.stats.findings != 2 || len(m.snapshot.Files[1].Findings) != 0 {
		t.Fatal("scope escaped", m.snapshot.Issues)
	}
	if strings.Contains(m.preview.GetContent(), "13800138000") || !strings.Contains(m.preview.GetContent(), "new new") {
		t.Fatal("unsafe replacement preview")
	}
	m.focus = focusFindings
	m = key(m, ' ')
	m.write.Output = filepath.Join(t.TempDir(), "out")
	m = key(m, 'w')
	if !m.confirm || m.confirmWrite || !strings.Contains(m.confirmation.GetContent(), "不自动脱敏") {
		t.Fatal("write semantics unclear")
	}
	next, cmd = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = next.(Model)
	writeBatch := cmd().(tea.BatchMsg)
	result := writeBatch[0]().(writeDone)
	if !result.operation.Complete {
		t.Fatal(result.operation)
	}
	data, _ := os.ReadFile(filepath.Join(m.write.Output, "a.txt"))
	if string(data) != "old new 13800138000" {
		t.Fatal("selection or preview mask affected output", string(data))
	}
	data, _ = os.ReadFile(filepath.Join(m.write.Output, "b.txt"))
	if string(data) != "old old 13800138000" {
		t.Fatal("other file changed")
	}
}

func TestReplacementPreviewProtectsPartialAndNewPII(t *testing.T) {
	for _, tc := range []struct{ input, find, with string }{
		{"phone=13800138000 and mail=a@example.com", "1380", "xxxx"},
		{"phone=13800138000", "0013", ""},
		{"value=PREFIX38000", "PREFIX", "138001"},
		{"a@example.com", "@example", "@other"},
		{"note=old\n13800138000\nold", "old", "two\nlines"},
	} {
		m := fixture(t)
		plan, err := replace.Compile(replace.Options{Find: tc.find, With: tc.with})
		if err != nil {
			t.Fatal(err)
		}
		data := []byte(tc.input)
		hits, err := plan.Find(context.Background(), data, 100, 1000)
		if err != nil {
			t.Fatal(err)
		}
		f := model.File{Path: "a.txt", Data: data, Findings: hits}
		prepareReplacementPreview(context.Background(), &f, m.service.Policy)
		if f.PreviewBlocked || len(f.PreviewMasks) == 0 {
			t.Fatal("privacy preparation", tc)
		}
		m.options.Replacement = plan
		m.snapshot.Files = []model.File{f}
		m.previewFile = -1
		m.rebuild()
		for i := range m.rows {
			m.cursor = i
			m.syncPreview()
		}
		for _, secret := range []string{"13800138000", "xxxx0138000", "13838000", "a@other.com", "a@example.com"} {
			if strings.Contains(m.preview.GetContent(), secret) {
				t.Fatal("leaked", secret)
			}
		}
	}
}

func TestReplacementLayoutsAndControls(t *testing.T) {
	for _, dark := range []bool{true, false} {
		for _, size := range [][2]int{{48, 15}, {60, 20}, {80, 24}, {100, 25}, {120, 30}, {160, 45}} {
			m := workspace(t)
			m.setTheme(dark)
			next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m = next.(Model)
			m = key(m, 'c')
			m.replaceInputs[0].SetValue(strings.Repeat("中文👩‍💻", 100))
			v := m.View().Content
			if lipgloss.Height(v) > size[1] {
				t.Fatal("height", size)
			}
			for _, line := range strings.Split(v, "\n") {
				if lipgloss.Width(line) > size[0] {
					t.Fatal("width", size)
				}
			}
			plain := ansi.Strip(v)
			for _, label := range []string{"查找", "替换为", "Enter", "Ctrl+R", "Ctrl+G", "Ctrl+S"} {
				if !strings.Contains(plain, label) {
					t.Fatal("hidden form control", size, label)
				}
			}
			if dir := os.Getenv("PRIVSAN_TUI_CAPTURE_DIR"); dir != "" && dark && size[0] == 120 {
				m.replaceInputs[0].SetValue("旧项目名称")
				m.replaceInputs[1].SetValue("新项目名称")
				if strings.Contains(ansi.Strip(m.replaceDialog(72)), "…") {
					t.Fatal("short inputs are clipped")
				}
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "replace-form.ansi"), []byte(m.View().Content), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestReplacementPrivacyPreviewFailsClosed(t *testing.T) {
	m := fixture(t)
	f := model.File{Path: "a.txt", Data: []byte("13800138000")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prepareReplacementPreview(ctx, &f, m.service.Policy)
	if !f.PreviewBlocked {
		t.Fatal("cancelled privacy scan not blocked")
	}
	m.snapshot.Files = []model.File{f}
	m.fileScope = 0
	m.previewFile = -1
	m.rebuild()
	if strings.Contains(m.preview.GetContent(), "13800138000") || !strings.Contains(m.preview.GetContent(), "原文已隐藏") {
		t.Fatal("failed preview exposed data")
	}
}
