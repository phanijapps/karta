//go:build linux

package noclobber

import (
	"os"

	"golang.org/x/sys/unix"
)

// move uses renameat2(RENAME_NOREPLACE): the whole check-and-move is
// one syscall, so an externally created destination can never be
// silently replaced.
func move(oldPath, newPath string) error {
	err := unix.Renameat2(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, unix.RENAME_NOREPLACE)
	if err == nil {
		return nil
	}
	if err == unix.ENOSYS || err == unix.EINVAL {
		// Kernel/filesystem without renameat2: guarded fallback.
		return moveGuarded(oldPath, newPath)
	}
	if err == unix.EEXIST || err == unix.ENOTEMPTY {
		err = os.ErrExist
	}
	return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: err}
}

func moveGuarded(oldPath, newPath string) error {
	if _, err := os.Lstat(newPath); err == nil {
		return os.ErrExist
	}
	return os.Rename(oldPath, newPath)
}
