package workflow

import (
	"context"
	"os"
	"path/filepath"
	"privsan/internal/model"
	"privsan/internal/policy"
	"privsan/internal/scan"
	"testing"
)

func TestSharedWritePolicy(t *testing.T) {
	p, e := policy.Compile(policy.Default())
	if e != nil {
		t.Fatal(e)
	}
	svc := Service{Policy: p}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("13800138000"), 0600)
	snapshot := svc.Scan(context.Background(), scan.Options{Root: root}, nil)
	if !snapshot.Complete {
		t.Fatal(snapshot.Issues)
	}
	if op := svc.Write(context.Background(), &snapshot, WriteOptions{DryRun: true}); op.Kind != "" {
		t.Fatal(op)
	}
	if op := svc.Write(context.Background(), &snapshot, WriteOptions{}); op.Complete || len(op.Issues) == 0 {
		t.Fatal(op)
	}
	if op := svc.Write(context.Background(), &model.Snapshot{}, WriteOptions{Output: "unused"}); op.Complete {
		t.Fatal(op)
	}
	dest := filepath.Join(t.TempDir(), "out")
	if op := svc.Write(context.Background(), &snapshot, WriteOptions{Output: dest}); !op.Complete {
		t.Fatal(op)
	}
	if op := svc.Write(context.Background(), &snapshot, WriteOptions{InPlace: true, BackupDir: t.TempDir()}); !op.Complete {
		t.Fatal(op)
	}
}
