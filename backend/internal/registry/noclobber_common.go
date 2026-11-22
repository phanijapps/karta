package registry

import "os"

// renameDirGuarded is the portable fallback: os.Rename on this Go
// toolchain refuses to replace any existing directory (Lstat precheck
// returning EEXIST for empty and non-empty targets alike).
func renameDirGuarded(oldDir, newDir string) error {
	err := os.Rename(oldDir, newDir)
	if err != nil && os.IsExist(err) {
		return err
	}
	return err
}
