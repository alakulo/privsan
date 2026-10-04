// Package storage implements checked export, journaled in-place writes and restore.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"privsan/internal/fsutil"
	"privsan/internal/model"
	"privsan/internal/policy"
)

type Entry struct {
	Path      string `json:"path"`
	Backup    string `json:"backup"`
	Before    string `json:"before_sha256"`
	After     string `json:"after_sha256"`
	Mode      uint32 `json:"mode"`
	Committed bool   `json:"committed"`
}
type Manifest struct {
	Version  int     `json:"version"`
	Created  string  `json:"created_at"`
	PolicyID string  `json:"policy_id"`
	State    string  `json:"state"`
	Entries  []Entry `json:"entries"`
}
type store struct {
	replace func(*os.Root, string, []byte, os.FileMode) error
}

func production() store { return store{replace: fsutil.Replace} }
func Export(ctx context.Context, s *model.Snapshot, destination string) model.Operation {
	return production().export(ctx, s, destination)
}
func Apply(ctx context.Context, s *model.Snapshot, backupRoot string) model.Operation {
	return production().apply(ctx, s, backupRoot)
}
func Restore(ctx context.Context, runDir, allowedRoot string) model.Operation {
	return production().restore(ctx, runDir, allowedRoot)
}
func operation(kind string) model.Operation {
	return model.Operation{Kind: kind, Issues: []model.Issue{}}
}
func issue(op *model.Operation, code, path, msg string) {
	op.Complete = false
	op.Issues = append(op.Issues, model.Error(code, path, msg))
}
func cancelled(ctx context.Context, op *model.Operation) bool {
	if ctx.Err() != nil {
		issue(op, "E_CANCELLED", "", "operation cancelled; inspect operation state before retry")
		return true
	}
	return false
}
func verify(root *os.Root, f model.File) error {
	if f.Info == nil || !fsutil.ValidRelative(f.Path) {
		return errors.New("invalid file snapshot")
	}
	if err := fsutil.NoLinks(filepath.Join(root.Name(), f.Path)); err != nil {
		return err
	}
	b, st, err := fsutil.Read(root, f.Path, int64(len(f.Data)))
	if err != nil {
		return err
	}
	if !os.SameFile(f.Info, st) || model.Hash(b) != f.Digest {
		return errors.New("source changed")
	}
	return nil
}
func preflight(ctx context.Context, s *model.Snapshot, op *model.Operation) (*os.Root, bool) {
	if !s.Complete || s.Root == "" {
		issue(op, "E_INPUT", "", "complete filesystem scan required")
		return nil, false
	}
	if err := fsutil.NoLinks(s.Root); err != nil {
		issue(op, "E_INPUT", "", "source root is unavailable or unsafe")
		return nil, false
	}
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		issue(op, "E_READ", "", "cannot open source root")
		return nil, false
	}
	for _, f := range s.Files {
		if cancelled(ctx, op) {
			root.Close()
			return nil, false
		}
		if err = verify(root, f); err != nil {
			issue(op, "E_CHANGED", f.Path, "source changed since scan; rescan required")
			root.Close()
			return nil, false
		}
		if _, err = model.Render(f.Data, f.Findings, false); err != nil {
			issue(op, "E_RULE", f.Path, "invalid finding spans")
			root.Close()
			return nil, false
		}
	}
	return root, true
}
func newDirectory(name string) (*os.Root, error) {
	parent := filepath.Dir(name)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	canonical, err := fsutil.CanonicalRoot(parent)
	if err != nil {
		return nil, err
	}
	name = filepath.Join(canonical, filepath.Base(name))
	if err := os.Mkdir(name, 0700); err != nil {
		return nil, err
	}
	return os.OpenRoot(name)
}
func (st store) export(ctx context.Context, s *model.Snapshot, dest string) model.Operation {
	op := operation("export")
	root, ok := preflight(ctx, s, &op)
	if !ok {
		return op
	}
	defer root.Close()
	abs, err := fsutil.Destination(dest)
	if err != nil || dest == "" || fsutil.Within(s.Root, abs) {
		issue(&op, "E_INPUT", "", "output must be a new directory outside the source root")
		return op
	}
	out, err := newDirectory(abs)
	if err != nil {
		issue(&op, "E_WRITE", "", "output must not exist and its parent must be writable without links")
		return op
	}
	defer out.Close()
	op.RunDir = out.Name()
	for _, f := range s.Files {
		if cancelled(ctx, &op) {
			return op
		}
		if err = verify(root, f); err != nil {
			issue(&op, "E_CHANGED", f.Path, "source changed during export")
			return op
		}
		b, _ := model.Render(f.Data, f.Findings, false)
		if err = out.MkdirAll(filepath.Dir(f.Path), 0700); err == nil {
			err = fsutil.WriteNew(out, f.Path, b, 0600)
		}
		if err != nil {
			issue(&op, "E_WRITE", f.Path, "export failed; output has no completion marker")
			return op
		}
		op.Changed++
	}
	if cancelled(ctx, &op) {
		return op
	}
	report := model.BuildReport(*s, model.Operation{}, model.ReportOptions{Mode: "export"})
	b, err := json.MarshalIndent(report, "", "  ")
	if err == nil {
		err = fsutil.WriteNew(out, ".privsan-export.json", b, 0600)
	}
	if err != nil {
		issue(&op, "E_WRITE", "", "could not persist export completion marker")
		return op
	}
	op.Complete = true
	return op
}
func saveManifest(root *os.Root, m Manifest, initial bool) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if initial {
		return fsutil.WriteNew(root, "manifest.json", b, 0600)
	}
	return fsutil.Replace(root, "manifest.json", b, 0600)
}
func (st store) apply(ctx context.Context, s *model.Snapshot, backupRoot string) model.Operation {
	op := operation("in-place")
	root, ok := preflight(ctx, s, &op)
	if !ok {
		return op
	}
	defer root.Close()
	// Build a deterministic write plan before creating any backup.
	indexes := []int{}
	for i, f := range s.Files {
		b, _ := model.Render(f.Data, f.Findings, false)
		if model.Hash(b) == f.Digest {
			continue
		}
		links, err := fsutil.Hardlinked(root, f.Path)
		if err != nil || links {
			issue(&op, "E_INPUT", f.Path, "in-place replacement requires a regular file with one hard link")
			return op
		}
		if f.Info.Mode().Perm()&0200 == 0 {
			issue(&op, "E_INPUT", f.Path, "source is read-only")
			return op
		}
		indexes = append(indexes, i)
	}
	if len(indexes) == 0 {
		op.Complete = true
		return op
	}
	backupAbs, err := fsutil.Destination(backupRoot)
	if backupRoot == "" || err != nil || fsutil.Within(s.Root, backupAbs) {
		issue(&op, "E_INPUT", "", "backup directory must be outside the source root")
		return op
	}
	runDir := filepath.Join(backupAbs, "run-"+time.Now().UTC().Format("20060102T150405Z")+"-"+fsutil.Nonce())
	backup, err := newDirectory(runDir)
	if err != nil {
		issue(&op, "E_BACKUP", "", "cannot create private backup run directory")
		return op
	}
	defer backup.Close()
	op.RunDir = backup.Name()
	manifest := Manifest{Version: 1, Created: time.Now().UTC().Format(time.RFC3339), PolicyID: s.PolicyID, State: "prepared", Entries: []Entry{}}
	for _, i := range indexes {
		if cancelled(ctx, &op) {
			return op
		}
		f := s.Files[i]
		b, _ := model.Render(f.Data, f.Findings, false)
		entry := Entry{Path: f.Path, Backup: fsutil.Nonce() + ".bin", Before: f.Digest, After: model.Hash(b), Mode: uint32(f.Info.Mode().Perm())}
		if err = fsutil.WriteNew(backup, entry.Backup, f.Data, 0600); err != nil {
			issue(&op, "E_BACKUP", f.Path, "could not persist backup; no source files changed")
			return op
		}
		saved, _, e := fsutil.Read(backup, entry.Backup, int64(len(f.Data)))
		if e != nil || model.Hash(saved) != entry.Before {
			issue(&op, "E_BACKUP", f.Path, "backup verification failed; no source files changed")
			return op
		}
		manifest.Entries = append(manifest.Entries, entry)
	}
	if err = saveManifest(backup, manifest, true); err != nil {
		issue(&op, "E_BACKUP", "", "could not persist prepared manifest; no source files changed")
		return op
	}
	for j, i := range indexes {
		if cancelled(ctx, &op) {
			return op
		}
		f := s.Files[i]
		if err = verify(root, f); err != nil {
			issue(&op, "E_CHANGED", f.Path, "source changed before commit; use restore for earlier commits")
			return op
		}
		b, _ := model.Render(f.Data, f.Findings, false)
		if err = st.replace(root, f.Path, b, f.Info.Mode()); err != nil {
			issue(&op, "E_WRITE", f.Path, "replacement failed; inspect backup manifest and restore")
			return op
		}
		op.Changed++
		manifest.Entries[j].Committed = true
		if err = saveManifest(backup, manifest, false); err != nil {
			issue(&op, "E_BACKUP", f.Path, "source replaced but journal update failed; restore can infer state from hashes")
			return op
		}
	}
	manifest.State = "complete"
	if err = saveManifest(backup, manifest, false); err != nil {
		issue(&op, "E_BACKUP", "", "could not finalize manifest; backups remain recoverable")
		return op
	}
	op.Complete = true
	return op
}
func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (st store) restore(ctx context.Context, runDir, allowedRoot string) model.Operation {
	op := operation("restore")
	op.RunDir = runDir
	if runDir == "" || allowedRoot == "" {
		issue(&op, "E_INPUT", "", "restore requires backup run and explicit allowed root")
		return op
	}
	var pathErr error
	runDir, pathErr = fsutil.CanonicalRoot(runDir)
	if pathErr != nil {
		issue(&op, "E_INPUT", "", "backup root is unavailable or a link")
		return op
	}
	allowedRoot, pathErr = fsutil.CanonicalRoot(allowedRoot)
	if pathErr != nil {
		issue(&op, "E_INPUT", "", "restore paths must exist without symbolic links")
		return op
	}
	op.RunDir = runDir
	backup, err := os.OpenRoot(runDir)
	if err != nil {
		issue(&op, "E_BACKUP", "", "cannot open backup")
		return op
	}
	defer backup.Close()
	b, _, err := fsutil.Read(backup, "manifest.json", 16<<20)
	if err != nil {
		issue(&op, "E_BACKUP", "", "cannot read bounded manifest")
		return op
	}
	var m Manifest
	if err = policy.StrictJSON(b, &m); err != nil || m.Version != 1 || len(m.Entries) > 100000 || (m.State != "prepared" && m.State != "complete" && m.State != "restored") {
		issue(&op, "E_BACKUP", "", "invalid manifest")
		return op
	}
	root, err := os.OpenRoot(allowedRoot)
	if err != nil {
		issue(&op, "E_INPUT", "", "cannot open restore root")
		return op
	}
	defer root.Close()
	seen := map[string]bool{}
	// Verify every entry and backup before the first restoration.
	for _, e := range m.Entries {
		if cancelled(ctx, &op) {
			return op
		}
		if !fsutil.ValidRelative(e.Path) || !fsutil.ValidRelative(e.Backup) || strings.Contains(e.Backup, "/") || !strings.HasSuffix(e.Backup, ".bin") || seen[e.Path] || !validDigest(e.Before) || !validDigest(e.After) || e.Mode > 0777 {
			issue(&op, "E_BACKUP", "", "unsafe or invalid manifest entry")
			return op
		}
		seen[e.Path] = true
		if fsutil.NoLinks(filepath.Join(root.Name(), e.Path)) != nil {
			issue(&op, "E_RESTORE", e.Path, "restore destination unavailable or traverses a link")
			return op
		}
		links, err := fsutil.Hardlinked(root, e.Path)
		if err != nil || links {
			issue(&op, "E_RESTORE", e.Path, "restore requires one hard link")
			return op
		}
		original, _, err := fsutil.Read(backup, e.Backup, 256<<20)
		if err != nil || model.Hash(original) != e.Before {
			issue(&op, "E_BACKUP", e.Path, "backup digest mismatch")
			return op
		}
		current, _, err := fsutil.Read(root, e.Path, 512<<20)
		if err != nil {
			issue(&op, "E_RESTORE", e.Path, "cannot read restore destination")
			return op
		}
		hash := model.Hash(current)
		if hash != e.Before && hash != e.After {
			issue(&op, "E_CONFLICT", e.Path, "destination modified after sanitization; refusing overwrite")
			return op
		}
	}
	for _, e := range m.Entries {
		if cancelled(ctx, &op) {
			return op
		}
		if fsutil.NoLinks(filepath.Join(root.Name(), e.Path)) != nil {
			issue(&op, "E_RESTORE", e.Path, "restore path changed")
			return op
		}
		current, _, err := fsutil.Read(root, e.Path, 512<<20)
		if err != nil {
			issue(&op, "E_RESTORE", e.Path, "restore destination changed")
			return op
		}
		if model.Hash(current) == e.Before {
			continue
		}
		if model.Hash(current) != e.After {
			issue(&op, "E_CONFLICT", e.Path, "destination changed during restore")
			return op
		}
		original, _, err := fsutil.Read(backup, e.Backup, 256<<20)
		if err != nil || model.Hash(original) != e.Before {
			issue(&op, "E_BACKUP", e.Path, "backup changed during restore")
			return op
		}
		if err = st.replace(root, e.Path, original, os.FileMode(e.Mode)); err != nil {
			issue(&op, "E_RESTORE", e.Path, "restore replacement failed")
			return op
		}
		op.Changed++
	}
	m.State = "restored"
	if err = saveManifest(backup, m, false); err != nil {
		issue(&op, "E_BACKUP", "", "files restored but manifest update failed")
		return op
	}
	op.Complete = true
	return op
}
