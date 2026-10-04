//go:build !windows

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func replace(root *os.Root, from, to string) error {
	if err := root.Rename(from, to); err != nil {
		return err
	}
	return syncParent(root, to)
}
func syncParent(root *os.Root, name string) error {
	dir, err := root.Open(filepath.Dir(name))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return err
	}
	return nil
}
func Hardlinked(root *os.Root, name string) (bool, error) {
	info, err := root.Stat(name)
	if err != nil {
		return false, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, errors.New("unsupported file identity")
	}
	return stat.Nlink > 1, nil
}
