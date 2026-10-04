package fsutil

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CanonicalRoot permits OS path aliases in ancestors (for example macOS /var),
// but rejects an explicitly supplied root that is itself a symbolic link.
func CanonicalRoot(name string) (string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	st, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("symbolic root")
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	if err = NoLinks(canonical); err != nil {
		return "", err
	}
	return canonical, nil
}

// Destination resolves the existing ancestor of a not-yet-created path.
func Destination(name string) (string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	ancestor := abs
	for {
		_, err = os.Lstat(ancestor)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", err
		}
		ancestor = parent
	}
	canonical, err := CanonicalRoot(ancestor)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(ancestor, abs)
	if err != nil {
		return "", err
	}
	return filepath.Join(canonical, rel), nil
}

// NoLinks rejects links/reparse points in all existing path components.
// It is a precondition check, not a defense against hostile concurrent changes.
func NoLinks(name string) error {
	abs, err := filepath.Abs(name)
	if err != nil {
		return err
	}
	for p := abs; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return errors.New("symbolic link path")
		}
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
	}
	return nil
}
func ValidRelative(name string) bool {
	return name != "" && name != "." && filepath.IsLocal(name) && !strings.Contains(name, "\\") && filepath.ToSlash(filepath.Clean(name)) == name
}
func Within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}
func Read(root *os.Root, name string, limit int64) ([]byte, os.FileInfo, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() || before.Size() > limit {
		return nil, nil, errors.New("not regular or too large")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, nil, errors.New("file identity changed")
	}
	b, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	current, err := root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if len(b) > int(limit) || int64(len(b)) != before.Size() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || !os.SameFile(before, current) {
		return nil, nil, errors.New("file changed during read")
	}
	return b, after, nil
}
func Nonce() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("system random source unavailable")
	}
	return hex.EncodeToString(b[:])
}
func WriteNew(root *os.Root, name string, data []byte, mode os.FileMode) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			_ = root.Remove(name)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = syncParent(root, name); err != nil {
		return err
	}
	ok = true
	return nil
}
func Replace(root *os.Root, name string, data []byte, mode os.FileMode) error {
	tmp := filepath.Join(filepath.Dir(name), ".privsan-"+Nonce())
	if err := WriteNew(root, tmp, data, 0600); err != nil {
		return err
	}
	defer root.Remove(tmp)
	// Chmod the handle so no path-based chmod can follow a substituted link.
	file, err := root.OpenFile(tmp, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = file.Chmod(mode.Perm())
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replace(root, tmp, name)
}
