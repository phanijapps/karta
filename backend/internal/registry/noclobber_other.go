//go:build !linux

package registry

import "os"

// renameDirNoClobber moves a directory, refusing an existing target. On
// non-Linux platforms there is no portable rename-with-noreplace, so this
// is Go's guarded os.Rename (Lstat precheck): occupied targets — empty
// directories included — fail with IsExist. Residual: an external creator
// winning the microsecond window between precheck and rename could have
// an EMPTY target directory replaced; the daemon serves loopback only.
func renameDirNoClobber(oldDir, newDir string) error {
	return renameDirGuarded(oldDir, newDir)
}
