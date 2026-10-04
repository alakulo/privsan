// Package model separates private scan snapshots from public reports.
package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
)

var Version = "1.0.0-rc.1"
var Commit = "development"
var BuildDate = "unknown"

type Issue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
}

func Error(code, path, message string) Issue { return Issue{code, "error", path, message} }

type Finding struct {
	ID          int    `json:"id"`
	Rule        string `json:"rule"`
	Start       int    `json:"start_byte"`
	End         int    `json:"end_byte"`
	Line        int    `json:"line"`
	Column      int    `json:"column"`
	Replacement string `json:"replacement"`
	Selected    bool   `json:"selected"`
}

// File and Snapshot must never be marshaled: Data is private plaintext.
type File struct {
	Path     string
	Data     []byte
	Digest   string
	Info     os.FileInfo
	Findings []Finding
}
type Snapshot struct {
	Root     string
	Files    []File
	Issues   []Issue
	Skipped  map[string]int
	PolicyID string
	Complete bool
}

func (File) MarshalJSON() ([]byte, error) {
	return nil, errors.New("private file snapshots cannot be serialized")
}
func (Snapshot) MarshalJSON() ([]byte, error) {
	return nil, errors.New("private scan snapshots cannot be serialized")
}

type Operation struct {
	Kind     string  `json:"kind"`
	Complete bool    `json:"complete"`
	Changed  int     `json:"changed_files"`
	RunDir   string  `json:"run_dir,omitempty"`
	Issues   []Issue `json:"issues"`
}
type Summary struct {
	Files    int            `json:"scanned_files"`
	Skipped  map[string]int `json:"skipped_files"`
	Bytes    int64          `json:"bytes"`
	Findings int            `json:"findings"`
	Selected int            `json:"selected"`
}
type ReportFile struct {
	Path     string    `json:"path,omitempty"`
	Size     int       `json:"size"`
	Findings []Finding `json:"findings"`
	Content  *string   `json:"content,omitempty"`
}
type Report struct {
	Version       string       `json:"version"`
	SchemaVersion int          `json:"schema_version"`
	Mode          string       `json:"mode"`
	Complete      bool         `json:"complete"`
	PolicyID      string       `json:"policy_id"`
	Summary       Summary      `json:"summary"`
	Files         []ReportFile `json:"files"`
	Issues        []Issue      `json:"issues"`
	Operation     *Operation   `json:"operation,omitempty"`
}
type ReportOptions struct {
	Mode               string
	Content, HidePaths bool
}

func Hash(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func Render(data []byte, findings []Finding, all bool) ([]byte, error) {
	var b bytes.Buffer
	pos, last := 0, 0
	for _, f := range findings {
		if f.Start < last || f.End <= f.Start || f.End > len(data) {
			return nil, fmt.Errorf("invalid finding span")
		}
		last = f.End
		if !all && !f.Selected {
			continue
		}
		b.Write(data[pos:f.Start])
		b.WriteString(f.Replacement)
		pos = f.End
	}
	b.Write(data[pos:])
	return b.Bytes(), nil
}
func BuildReport(s Snapshot, op Operation, opts ReportOptions) Report {
	r := Report{Version: Version, SchemaVersion: 2, Mode: opts.Mode, Complete: s.Complete, PolicyID: s.PolicyID, Files: []ReportFile{}, Issues: append([]Issue{}, s.Issues...), Summary: Summary{Skipped: s.Skipped}}
	if r.Summary.Skipped == nil {
		r.Summary.Skipped = map[string]int{}
	}
	if op.Kind != "" {
		r.Operation = &op
		r.Complete = r.Complete && op.Complete
	}
	for _, f := range s.Files {
		rf := ReportFile{Path: f.Path, Size: len(f.Data), Findings: append([]Finding{}, f.Findings...)}
		if opts.HidePaths {
			rf.Path = ""
		}
		r.Summary.Files++
		r.Summary.Bytes += int64(len(f.Data))
		r.Summary.Findings += len(f.Findings)
		for _, h := range f.Findings {
			if h.Selected {
				r.Summary.Selected++
			}
		}
		if opts.Content && r.Complete {
			b, err := Render(f.Data, f.Findings, false)
			if err != nil {
				r.Complete = false
				r.Issues = append(r.Issues, Error("E_RULE", f.Path, "invalid finding spans"))
			} else {
				v := string(b)
				rf.Content = &v
			}
		}
		r.Files = append(r.Files, rf)
	}
	if !r.Complete {
		for i := range r.Files {
			r.Files[i].Content = nil
		}
	}
	if opts.HidePaths {
		for i := range r.Issues {
			r.Issues[i].Path = ""
		}
		if r.Operation != nil {
			r.Operation.RunDir = ""
			r.Operation.Issues = append([]Issue{}, op.Issues...)
			for i := range r.Operation.Issues {
				r.Operation.Issues[i].Path = ""
			}
		}
	}
	return r
}
