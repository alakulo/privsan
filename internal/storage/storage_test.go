package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"privsan/internal/fsutil"
	"privsan/internal/model"
	"privsan/internal/policy"
	"privsan/internal/scan"
	"testing"
)

func setup(t *testing.T) (string, model.Snapshot) {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("phone 13800138000\r\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, e := policy.Compile(policy.Default())
	if e != nil {
		t.Fatal(e)
	}
	s := scan.Run(context.Background(), scan.Options{Root: root}, p, nil)
	if !s.Complete {
		t.Fatalf("%+v", s.Issues)
	}
	return root, s
}
func read(t *testing.T, p string) string {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestExportApplyRestore(t *testing.T) {
	root, s := setup(t)
	base := t.TempDir()
	out := filepath.Join(base, "out")
	op := Export(context.Background(), &s, out)
	if !op.Complete || op.Changed != 2 {
		t.Fatalf("%+v", op)
	}
	if read(t, filepath.Join(out, "a.txt")) != "phone [PHONE]\r\n" {
		t.Fatal("wrong export")
	}
	if read(t, filepath.Join(root, "a.txt")) != "phone 13800138000\r\n" {
		t.Fatal("export changed source")
	}
	if _, e := os.Stat(filepath.Join(out, ".privsan-export.json")); e != nil {
		t.Fatal(e)
	}
	if Export(context.Background(), &s, out).Complete {
		t.Fatal("existing output overwritten")
	}
	if Export(context.Background(), &s, filepath.Join(root, "out")).Complete {
		t.Fatal("nested export accepted")
	}
	op = Apply(context.Background(), &s, filepath.Join(base, "backups"))
	if !op.Complete || op.Changed != 2 {
		t.Fatalf("%+v", op)
	}
	if read(t, filepath.Join(root, "a.txt")) != "phone [PHONE]\r\n" {
		t.Fatal("apply failed")
	}
	restored := Restore(context.Background(), op.RunDir, root)
	if !restored.Complete || restored.Changed != 2 {
		t.Fatalf("%+v", restored)
	}
	if read(t, filepath.Join(root, "a.txt")) != "phone 13800138000\r\n" {
		t.Fatal("restore failed")
	}
	again := Restore(context.Background(), op.RunDir, root)
	if !again.Complete || again.Changed != 0 {
		t.Fatalf("not idempotent: %+v", again)
	}
}
func TestPreflightRejectsChangedAndIncomplete(t *testing.T) {
	root, s := setup(t)
	base := t.TempDir()
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("new version"), 0600)
	op := Apply(context.Background(), &s, base)
	if op.Complete || op.Changed != 0 {
		t.Fatalf("%+v", op)
	}
	if read(t, filepath.Join(root, "a.txt")) != "phone 13800138000\r\n" {
		t.Fatal("partial write during preflight")
	}
	entries, _ := os.ReadDir(base)
	if len(entries) != 0 {
		t.Fatal("backup created before preflight")
	}
	s.Complete = false
	if Export(context.Background(), &s, filepath.Join(base, "out")).Complete {
		t.Fatal("incomplete export")
	}
}
func TestInterruptedCommitRecoverable(t *testing.T) {
	root, s := setup(t)
	count := 0
	faulty := store{replace: func(r *os.Root, name string, b []byte, mode os.FileMode) error {
		count++
		if count == 2 {
			return errors.New("injected disk failure")
		}
		return fsutil.Replace(r, name, b, mode)
	}}
	op := faulty.apply(context.Background(), &s, t.TempDir())
	if op.Complete || op.Changed != 1 || op.RunDir == "" {
		t.Fatalf("%+v", op)
	}
	restored := Restore(context.Background(), op.RunDir, root)
	if !restored.Complete || restored.Changed != 1 {
		t.Fatalf("%+v", restored)
	}
	for _, f := range s.Files {
		if read(t, filepath.Join(root, f.Path)) != string(f.Data) {
			t.Fatal("bad recovery")
		}
	}
}
func TestCrashBetweenReplaceAndJournal(t *testing.T) {
	root, s := setup(t)
	faulty := store{replace: func(r *os.Root, name string, b []byte, mode os.FileMode) error {
		if e := fsutil.Replace(r, name, b, mode); e != nil {
			return e
		}
		return errors.New("injected crash after replacement")
	}}
	op := faulty.apply(context.Background(), &s, t.TempDir())
	if op.Complete {
		t.Fatal("unexpected success")
	}
	restored := Restore(context.Background(), op.RunDir, root)
	if !restored.Complete || restored.Changed != 1 {
		t.Fatalf("%+v", restored)
	}
}
func TestRestoreConflictAndTamper(t *testing.T) {
	for _, kind := range []string{"conflict", "backup", "traversal", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			root, s := setup(t)
			op := Apply(context.Background(), &s, t.TempDir())
			if !op.Complete {
				t.Fatal(op)
			}
			manifestPath := filepath.Join(op.RunDir, "manifest.json")
			var m Manifest
			if e := json.Unmarshal([]byte(read(t, manifestPath)), &m); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "conflict":
				os.WriteFile(filepath.Join(root, "b.txt"), []byte("user edited"), 0600)
			case "backup":
				os.WriteFile(filepath.Join(op.RunDir, m.Entries[1].Backup), []byte("corrupt"), 0600)
			case "traversal":
				m.Entries[0].Path = "../outside.txt"
			case "duplicate":
				m.Entries[1].Path = m.Entries[0].Path
			}
			if kind == "traversal" || kind == "duplicate" {
				b, _ := json.Marshal(m)
				os.WriteFile(manifestPath, b, 0600)
			}
			restored := Restore(context.Background(), op.RunDir, root)
			if restored.Complete || restored.Changed != 0 {
				t.Fatalf("%+v", restored)
			}
			if read(t, filepath.Join(root, "a.txt")) != "phone [PHONE]\r\n" {
				t.Fatal("restored before full validation")
			}
		})
	}
}
func TestSelectionCancelledAndHardlink(t *testing.T) {
	root, s := setup(t)
	for i := range s.Files {
		for j := range s.Files[i].Findings {
			s.Files[i].Findings[j].Selected = false
		}
	}
	op := Apply(context.Background(), &s, t.TempDir())
	if !op.Complete || op.Changed != 0 || op.RunDir != "" {
		t.Fatal(op)
	}
	_, s = setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	op = Apply(ctx, &s, t.TempDir())
	if op.Complete || op.Changed != 0 {
		t.Fatal(op)
	}
	if e := os.Link(filepath.Join(root, "a.txt"), filepath.Join(root, "linked.txt")); e == nil {
		p, _ := policy.Compile(policy.Default())
		s = scan.Run(context.Background(), scan.Options{Root: root}, p, nil)
		op = Apply(context.Background(), &s, t.TempDir())
		if op.Complete {
			t.Fatal("hard links replaced")
		}
	}
}
