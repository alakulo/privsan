package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReportBoundary(t *testing.T) {
	s := Snapshot{Complete: true, Root: "private-root", Files: []File{{Path: "private.txt", Data: []byte("secret"), Digest: Hash([]byte("secret")), Findings: []Finding{{ID: 1, Rule: "custom", Start: 0, End: 6, Replacement: "[MASK]", Selected: true}}}}}
	if _, e := json.Marshal(s); e == nil {
		t.Fatal("private snapshot serialized")
	}
	if _, e := json.Marshal(s.Files[0]); e == nil {
		t.Fatal("private file serialized")
	}
	r := BuildReport(s, Operation{}, ReportOptions{Content: false, HidePaths: true})
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "private") || strings.Contains(string(b), "sha256") {
		t.Fatal(string(b))
	}
	r = BuildReport(s, Operation{}, ReportOptions{Content: true})
	if *r.Files[0].Content != "[MASK]" {
		t.Fatal("bad content")
	}
	s.Complete = false
	r = BuildReport(s, Operation{}, ReportOptions{Content: true})
	if r.Files[0].Content != nil {
		t.Fatal("failed open")
	}
	if _, e := Render([]byte("x"), []Finding{{Start: 0, End: 2}}, true); e == nil {
		t.Fatal("invalid span accepted")
	}
}
