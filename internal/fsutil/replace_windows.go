package fsutil

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"unsafe"
)

var replaceFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

// Windows has no equivalent portable directory-fsync contract.
func syncParent(_ *os.Root, _ string) error { return nil }

func replace(root *os.Root, from, to string) error {
	// ReplaceFileW preserves Windows metadata and refuses a missing target.
	source, err := windows.UTF16PtrFromString(filepath.Join(root.Name(), from))
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(filepath.Join(root.Name(), to))
	if err != nil {
		return err
	}
	result, _, callErr := replaceFile.Call(uintptr(unsafe.Pointer(target)), uintptr(unsafe.Pointer(source)), 0, 0, 0, 0)
	if result == 0 {
		return callErr
	}
	return nil
}
func Hardlinked(root *os.Root, name string) (bool, error) {
	f, err := root.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return false, err
	}
	return info.NumberOfLinks > 1, nil
}
