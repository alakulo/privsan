package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfinementAndReplacement(t *testing.T) {
	dir := t.TempDir()
	root, e := os.OpenRoot(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	if e = WriteNew(root, "a.txt", []byte("original"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = WriteNew(root, "a.txt", []byte("overwrite"), 0600); e == nil {
		t.Fatal("overwrote existing file")
	}
	if _, _, e = Read(root, "a.txt", 2); e == nil {
		t.Fatal("read beyond budget")
	}
	if e = Replace(root, "a.txt", []byte("masked"), 0600); e != nil {
		t.Fatal(e)
	}
	b, _, e := Read(root, "a.txt", 100)
	if e != nil || string(b) != "masked" {
		t.Fatal(string(b), e)
	}
	if _, e = root.Open("../outside.txt"); e == nil {
		t.Fatal("escaped root")
	}
	for _, p := range []string{"../x", "/abs", ".", "a/../b", "a\\b"} {
		if ValidRelative(p) {
			t.Fatal(p)
		}
	}
	if !ValidRelative("sub/a.txt") || !Within(dir, filepath.Join(dir, "sub")) || Within(dir, filepath.Dir(dir)) {
		t.Fatal("bad containment")
	}
}
