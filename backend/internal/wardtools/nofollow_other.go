//go:build !unix

package wardtools

// Non-unix fallback: Lstat immediately before the open (a narrow
// check-then-open window remains — documented limit of the platform).

import (
	"errors"
	"io"
	"os"
)

var errSymlink = errors.New("refusing to follow a symlink")

func readNoFollow(path string) ([]byte, error) {
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return nil, errSymlink
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

func writeNoFollow(path string, data []byte) error {
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return errSymlink
	}
	return os.WriteFile(path, data, 0o644)
}
