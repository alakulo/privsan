package scan

import (
	"context"
	"os"
	"path/filepath"
	"privsan/internal/model"
	"privsan/internal/policy"
	"reflect"
	"testing"
)

func fixture(t *testing.T, root, name, data string) {
	t.Helper()
	p := filepath.Join(root, name)
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(data), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestScopeAndDeterminism(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "a.txt", "13800138000")
	fixture(t, root, "sub/B.LOG", "a@example.com")
	fixture(t, root, "skip.txt", "secret")
	fixture(t, root, "file.go", "a@example.com")
	fixture(t, root, ".privsanignore", "skip.txt\n# comment\n")
	fixture(t, root, ".git/private.txt", "a@example.com")
	c := policy.Default()
	c.Limits.Workers = 1
	p, _ := policy.Compile(c)
	a := Run(context.Background(), Options{Root: root}, p, nil)
	c.Limits.Workers = 4
	p, _ = policy.Compile(c)
	b := Run(context.Background(), Options{Root: root}, p, nil)
	if !a.Complete || len(a.Files) != 2 || !b.Complete || a.Skipped["S_EXCLUDED"] != 2 {
		t.Fatalf("%+v", a)
	}
	for i := range a.Files {
		if a.Files[i].Path != b.Files[i].Path || !reflect.DeepEqual(a.Files[i].Findings, b.Files[i].Findings) {
			t.Fatal("non deterministic")
		}
	}
	b = Run(context.Background(), Options{Root: root, Include: []string{"**/*.LOG"}}, p, nil)
	if len(b.Files) != 1 {
		t.Fatalf("%+v", b)
	}
}
func TestBudgetsAndEncoding(t *testing.T) {
	for _, kind := range []string{"file", "total", "count", "findings", "encoding", "csv"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			fixture(t, root, "a.txt", "13800138000")
			fixture(t, root, "b.txt", "13800138001")
			c := policy.Default()
			switch kind {
			case "file":
				c.Limits.MaxFile = 2
			case "total":
				c.Limits.MaxFile = 12
				c.Limits.MaxTotal = 12
			case "count":
				c.Limits.MaxFiles = 1
			case "findings":
				c.Limits.MaxFindings = 1
			case "encoding":
				fixture(t, root, "bad.txt", string([]byte{255}))
			case "csv":
				c.Rules = []policy.Rule{{ID: "cross", Pattern: "a,b", Replacement: "X"}}
				fixture(t, root, "a.csv", "a,b")
			}
			p, e := policy.Compile(c)
			if e != nil {
				t.Fatal(e)
			}
			s := Run(context.Background(), Options{Root: root}, p, nil)
			if s.Complete || len(s.Issues) == 0 {
				t.Fatal("failed open")
			}
			r := model.BuildReport(s, model.Operation{}, model.ReportOptions{Content: true})
			for _, f := range r.Files {
				if f.Content != nil {
					t.Fatal("incomplete content emitted")
				}
			}
		})
	}
}
func TestSymlinkAndCancellation(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	fixture(t, outside, "private.txt", "a@example.com")
	if e := os.Symlink(filepath.Join(outside, "private.txt"), filepath.Join(root, "link.txt")); e == nil {
		p, _ := policy.Compile(policy.Default())
		s := Run(context.Background(), Options{Root: root}, p, nil)
		if !s.Complete || len(s.Files) != 0 || s.Skipped["S_SYMLINK"] != 1 {
			t.Fatal("followed symlink")
		}
	}
	p, _ := policy.Compile(policy.Default())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := Run(ctx, Options{Root: root}, p, nil)
	if s.Complete {
		t.Fatal("cancel reported success")
	}
}
func TestGlob(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"**/*.txt", "a.txt", true}, {"**/*.txt", "a/b.txt", true}, {"*.txt", "a/b.txt", false},
		{"logs/", "logs/a/b.log", true}, {"a/**/b?.log", "a/b1.log", true}, {"a/**/b?.log", "a/x/b1.log", true},
	}
	for _, c := range cases {
		g, e := CompileGlob(c.pattern)
		if e != nil || g.Match(c.path) != c.want {
			t.Fatalf("%+v %v", c, e)
		}
	}
	for _, p := range []string{"../secret", "/abs", "a\\b", "!x", "["} {
		if _, e := CompileGlob(p); e == nil {
			t.Fatalf("accepted %q", p)
		}
	}
}
