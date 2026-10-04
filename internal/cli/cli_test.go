package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"privsan/internal/model"
	"strings"
	"testing"
	"time"
)

func TestStalledStdinCancellation(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var out, stderr bytes.Buffer
	code := Run(ctx, []string{"scan", "--stdin", "--json", "--content"}, reader, &out, &stderr)
	if code != 130 || strings.Contains(out.String(), `"content"`) {
		t.Fatalf("%d %s", code, out.String())
	}
}

func execute(args []string, input string) (int, string, string) {
	var out, err bytes.Buffer
	code := Run(context.Background(), args, strings.NewReader(input), &out, &err)
	return code, out.String(), err.String()
}
func TestAgentInterface(t *testing.T) {
	input := "中文 phone=13800138000 email=alice@example.com\r\n"
	code, out, stderr := execute([]string{"scan", "--stdin", "--json", "--content", "--hide-paths"}, input)
	if code != 0 || stderr != "" || strings.Contains(out, "13800138000") || strings.Contains(out, "alice@example.com") {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
	var report model.Report
	if e := json.Unmarshal([]byte(out), &report); e != nil {
		t.Fatal(e)
	}
	if report.SchemaVersion != 2 || !report.Complete || report.Files[0].Path != "" || report.Files[0].Content == nil {
		t.Fatal(report)
	}
	code, out, _ = execute([]string{"redact", "--stdin", "--stdout"}, input)
	if code != 0 || out != "中文 phone=[PHONE] email=[EMAIL]\r\n" {
		t.Fatalf("%d %q", code, out)
	}
	code, out, _ = execute([]string{"scan", "--stdin", "--json"}, input)
	if code != 0 || strings.Contains(out, `"content"`) {
		t.Fatal("metadata mode contained body")
	}
	code, out, _ = execute([]string{"scan", "--stdin", "--jsonl", "--content"}, input)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 2 || !strings.Contains(lines[1], `"type":"summary"`) {
		t.Fatal(out)
	}
}
func TestExitCodesAndFlags(t *testing.T) {
	for _, args := range [][]string{
		{"scan", "--write"}, {"scan", "--output", "x"}, {"redact"}, {"redact", "--in-place"}, {"scan", "--json", "--jsonl"},
		{"scan", "--content"}, {"scan", "--workers", "0"}, {"scan", "--stdin", "x"}, {"redact", "--stdin", "--stdout", "--dry-run"},
		{"tui", "--json"}, {"scan", "--unknown"}, {"scan", "a", "b"}, {"scan", "--include", "../x"},
	} {
		if code, _, _ := execute(args, ""); code != 2 {
			t.Fatalf("%v got %d", args, code)
		}
	}
	if code, _, _ := execute([]string{"scan", "--stdin", "--fail-on-findings"}, "13800138000"); code != 3 {
		t.Fatal(code)
	}
	code, out, _ := execute([]string{"scan", "--stdin", "--json", "--content", "--max-file", "2"}, "13800138000")
	if code != 1 || strings.Contains(out, `"content"`) {
		t.Fatalf("%d %s", code, out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var outBuf, errBuf bytes.Buffer
	if code = Run(ctx, []string{"scan", "--stdin"}, strings.NewReader("hello"), &outBuf, &errBuf); code != 130 {
		t.Fatal(code)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }
func TestBrokenPipeAndDryRun(t *testing.T) {
	code := Run(context.Background(), []string{"scan", "--stdin", "--json"}, strings.NewReader("hello"), failWriter{}, &bytes.Buffer{})
	if code != 1 {
		t.Fatal(code)
	}
	root := t.TempDir()
	p := filepath.Join(root, "a.txt")
	os.WriteFile(p, []byte("13800138000"), 0600)
	dest := filepath.Join(t.TempDir(), "out")
	code, _, _ = execute([]string{"redact", "--dry-run", "--output", dest, root}, "")
	if code != 0 {
		t.Fatal(code)
	}
	if _, e := os.Stat(dest); !os.IsNotExist(e) {
		t.Fatal("dry-run created target")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "13800138000" {
		t.Fatal("dry-run wrote source")
	}
}
func TestConfigurationCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	if code, _, e := execute([]string{"config", "init", "--output", path}, ""); code != 0 {
		t.Fatal(code, e)
	}
	if code, _, _ := execute([]string{"config", "init", "--output", path}, ""); code != 1 {
		t.Fatal("overwrote policy")
	}
	if code, _, e := execute([]string{"config", "validate", "--config", path}, ""); code != 0 {
		t.Fatal(code, e)
	}
	if code, out, _ := execute([]string{"rules", "--json"}, ""); code != 0 || !strings.Contains(out, "phone") {
		t.Fatal(code, out)
	}
	if code, out, _ := execute([]string{"version", "--json"}, ""); code != 0 || !strings.Contains(out, "1.0.0") {
		t.Fatal(code, out)
	}
}
