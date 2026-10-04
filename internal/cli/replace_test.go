package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"privsan/internal/model"
)

func TestReplacePipeAndReports(t *testing.T) {
	code, out, stderr := execute([]string{"replace", "--find", "OLD", "--with", "$1", "--ignore-case", "--stdin", "--stdout"}, "old OLD 13800138000\r\n")
	if code != 0 || out != "$1 $1 13800138000\r\n" || stderr != "" {
		t.Fatalf("%d %q %s", code, out, stderr)
	}
	code, out, _ = execute([]string{"replace", "--find", "secret", "--with", "", "--dry-run", "--stdin", "--json", "--content"}, "secret title")
	var report model.Report
	if code != 0 || json.Unmarshal([]byte(out), &report) != nil || report.PolicyID != "replace-v1" || *report.Files[0].Content != " title" || strings.Contains(out, "secret") {
		t.Fatal(code, out)
	}
	for _, args := range [][]string{
		{"replace"}, {"replace", "--find", "x", "--dry-run"}, {"replace", "--find", "x", "--with", "y"},
		{"scan", "--find", "x", "--with", "y"}, {"redact", "--regex", "--dry-run"},
		{"replace", "--find", "secret(", "--with", "y", "--regex", "--dry-run"},
	} {
		if c, o, e := execute(args, ""); c != 2 || strings.Contains(o+e, "secret(") {
			t.Fatal(args, c, o, e)
		}
	}
	code, out, _ = execute([]string{"replace", "--find", "x", "--with", "long", "--stdin", "--stdout", "--max-file", "3"}, "x")
	if code != 1 || out != "" {
		t.Fatal("partial stdout", code, out)
	}
}

func TestReplaceFilesystemLifecycle(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.txt")
	original := "old old\r\nphone=13800138000"
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "copy")
	args := []string{"replace", "--find", "old", "--with", "new", "--output", output, "--dry-run", root}
	if code, _, e := execute(args, ""); code != 0 {
		t.Fatal(code, e)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("dry run created output")
	}
	args = []string{"replace", "--find", "old", "--with", "new", "--output", output, root}
	if code, _, e := execute(args, ""); code != 0 {
		t.Fatal(code, e)
	}
	data, err := os.ReadFile(filepath.Join(output, "a.txt"))
	if err != nil || string(data) != strings.ReplaceAll(original, "old", "new") {
		t.Fatal(string(data), err)
	}
	data, _ = os.ReadFile(file)
	if string(data) != original {
		t.Fatal("export changed source")
	}
	backup := t.TempDir()
	code, out, e := execute([]string{"replace", "--find", "old", "--with", "new", "--in-place", "--backup-dir", backup, "--json", root}, "")
	var report model.Report
	if code != 0 || json.Unmarshal([]byte(out), &report) != nil || !report.Complete {
		t.Fatal(code, out, e)
	}
	data, _ = os.ReadFile(file)
	if string(data) != strings.ReplaceAll(original, "old", "new") {
		t.Fatal(string(data))
	}
	if code, _, e := execute([]string{"restore", "--from", report.Operation.RunDir, "--root", root}, ""); code != 0 {
		t.Fatal(code, e)
	}
	data, _ = os.ReadFile(file)
	if string(data) != original {
		t.Fatal("restore lost original")
	}
}

func TestReplaceCSVAndBatchLimits(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.csv"), []byte("name,value\nold,1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"old", "new,field"}, {"name,value", "header"}, {"old", "new\nrow"}} {
		code, out, _ := execute([]string{"replace", "--find", pair[0], "--with", pair[1], "--dry-run", "--json", "--content", root}, "")
		if code != 1 || !strings.Contains(out, "E_CSV") || strings.Contains(out, `"content"`) {
			t.Fatal(code, out)
		}
	}
	root = t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	code, out, _ := execute([]string{"replace", "--find", "x", "--with", "long", "--dry-run", "--json", "--content", "--max-file", "4", "--max-total", "5", root}, "")
	if code != 1 || !strings.Contains(out, "E_LIMIT") || strings.Contains(out, `"content"`) {
		t.Fatal(code, out)
	}
}
