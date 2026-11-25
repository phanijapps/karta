//go:build unix

package wardtools

// Race-resistant, symlink-refusing file I/O for the ward file tools:
// O_NOFOLLOW makes the kernel reject a final-component symlink at
// open time (no Lstat-then-open window). A symlink anywhere — even
// one pointing inside the ward — is refused for the tools' purposes:
// the Active Ward layout is a tree of real files.

import (
	"errors"
	"io"
	"os"
	"syscall"
)

var errSymlink = errors.New("refusing to follow a symlink")

// readNoFollow reads a file, refusing a final-component symlink.
func readNoFollow(path string) ([]byte, error) {
	fd, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if isSymlinkErr(err) {
			return nil, errSymlink
		}
		return nil, err
	}
	defer func() { _ = fd.Close() }()
	return io.ReadAll(fd)
}

// writeNoFollow writes a file, refusing a final-component symlink
// (O_NOFOLLOW on open; a dangling symlink is refused, not followed).
func writeNoFollow(path string, data []byte) error {
	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		if isSymlinkErr(err) {
			return errSymlink
		}
		return err
	}
	if _, err := fd.Write(data); err != nil {
		_ = fd.Close()
		return err
	}
	return fd.Close()
}

func isSymlinkErr(err error) bool {
	return errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EMLINK)
}
