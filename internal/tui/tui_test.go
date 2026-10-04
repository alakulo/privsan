package tui

import (
	"bytes"
	tea "charm.land/bubbletea/v2"
	"context"
	"io"
	"os"
	"path/filepath"
	"privsan/internal/detect"
	"privsan/internal/model"
	"privsan/internal/policy"
	"privsan/internal/scan"
	"privsan/internal/workflow"
	"strings"
	"testing"
	"time"
)

type automated struct{ Model }

func (a automated) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := a.Model.Update(msg)
	a.Model = next.(Model)
	switch msg.(type) {
	case scanDone:
		if !a.snapshot.Complete {
			return a, tea.Quit
		}
		next, _ = a.Model.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
		a.Model = next.(Model)
		next, cmd = a.Model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
		a.Model = next.(Model)
	case writeDone:
		return a, tea.Quit
	}
	return a, cmd
}
func TestProgramAsyncExport(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("13800138000"), 0600)
	p, e := policy.Compile(policy.Default())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output := filepath.Join(t.TempDir(), "out")
	initial := New(ctx, workflow.Service{Policy: p}, scan.Options{Root: root}, workflow.WriteOptions{Output: output})
	result, e := tea.NewProgram(automated{initial}, tea.WithInput(bytes.NewReader(nil)), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithContext(ctx)).Run()
	if e != nil {
		t.Fatal(e)
	}
	final := result.(automated)
	if !final.operation.Complete || final.operation.Changed != 1 {
		t.Fatalf("%+v", final.operation)
	}
	b, e := os.ReadFile(filepath.Join(output, "a.txt"))
	if e != nil || string(b) != "[PHONE]" {
		t.Fatal(string(b), e)
	}
}

func fixture(t *testing.T) Model {
	t.Helper()
	p, e := policy.Compile(policy.Default())
	if e != nil {
		t.Fatal(e)
	}
	data := []byte("phone 13800138000 email alice@example.com")
	h, e := detect.Find(context.Background(), data, p, 100)
	if e != nil {
		t.Fatal(e)
	}
	m := New(context.Background(), workflow.Service{Policy: p}, scan.Options{Root: "test"}, workflow.WriteOptions{Output: "output"})
	m.snapshot = model.Snapshot{Complete: true, Files: []model.File{{Path: "test.txt", Data: data, Findings: h}}}
	m.state = "review"
	m.rebuild()
	return m
}
func key(m Model, k rune) Model {
	next, _ := m.Update(tea.KeyPressMsg{Code: k, Text: string(k)})
	return next.(Model)
}
func TestReviewSelectionPreviewAndConfirmation(t *testing.T) {
	m := fixture(t)
	m = key(m, ' ')
	if m.snapshot.Files[0].Findings[0].Selected {
		t.Fatal("space ignored")
	}
	view := m.View().Content
	if strings.Contains(view, "13800138000") || strings.Contains(view, "alice@example.com") {
		t.Fatal("original leaked")
	}
	m = key(m, 'w')
	if !m.confirm {
		t.Fatal("missing confirmation")
	}
	m = key(m, 'n')
	if m.confirm || m.state != "review" {
		t.Fatal("cancel failed")
	}
	m.write.DryRun = true
	m = key(m, 'w')
	if m.confirm {
		t.Fatal("dry-run write")
	}
	m.write.DryRun = false
	m.snapshot.Complete = false
	m = key(m, 'w')
	if m.confirm {
		t.Fatal("incomplete write")
	}
}
func TestFilterAndMultilinePreview(t *testing.T) {
	m := fixture(t)
	m.filter = "email"
	m.rebuild()
	if len(m.rows) != 1 {
		t.Fatal("filter failed")
	}
	m = key(m, 'a')
	if m.snapshot.Files[0].Findings[1].Selected {
		t.Fatal("filtered selection failed")
	}
	c := policy.Default()
	c.Rules = []policy.Rule{{ID: "block", Pattern: "secret\\nblock", Replacement: "[BLOCK]"}}
	p, e := policy.Compile(c)
	if e != nil {
		t.Fatal(e)
	}
	data := []byte("secret\nblock\nphone 13800138000")
	hits, e := detect.Find(context.Background(), data, p, 100)
	if e != nil {
		t.Fatal(e)
	}
	f := model.File{Data: data, Findings: hits}
	if got := PreviewLine(f, 1); !strings.Contains(got, "[PHONE]") || strings.Contains(got, "13800138000") {
		t.Fatal(got)
	}
	m.width = 30
	if !strings.Contains(m.View().Content, "48") {
		t.Fatal("missing small terminal message")
	}
}
func TestWriteIsAsynchronous(t *testing.T) {
	m := fixture(t)
	m = key(m, 'w')
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = next.(Model)
	if m.state != "writing" || cmd == nil {
		t.Fatal("write was not scheduled")
	}
	before := m.snapshot.Files[0].Findings[0].Selected
	m = key(m, ' ')
	if m.snapshot.Files[0].Findings[0].Selected != before {
		t.Fatal("selection changed during write")
	}
	if m.cancel != nil {
		m.cancel()
	}
}
