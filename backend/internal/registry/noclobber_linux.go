//go:build linux

package registry

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// renameDirNoClobber moves a directory atomically, failing if the target
// exists (RENAME_NOREPLACE): the whole check-and-move is one syscall, so
// an externally created empty target directory can never be silently
// replaced the way plain rename(2) would.
func renameDirNoClobber(oldDir, newDir string) error {
	err := unix.Renameat2(unix.AT_FDCWD, oldDir, unix.AT_FDCWD, newDir, unix.RENAME_NOREPLACE)
	if err == nil {
		return nil
	}
	if err == unix.ENOSYS || err == unix.EINVAL {
		// Kernel or filesystem without renameat2: fall back to the
		// guarded os.Rename (Lstat precheck; microsecond TOCTOU on
		// empty targets — documented residual).
		return renameDirGuarded(oldDir, newDir)
	}
	if err == unix.EEXIST || err == unix.ENOTEMPTY {
		err = os.ErrExist // map both occupant signals to IsExist
	}
	return &os.LinkError{Op: "rename", Old: oldDir, New: newDir, Err: err}
}

var _ = fmt.Sprintf
