//go:build !linux

package noclobber

import "os"

// move is the portable fallback: Go's os.Rename refuses to replace an
// existing directory; files are prechecked here. Residual: an external
// creator winning the microsecond window on an EMPTY DIRECTORY target
// could replace it; the daemon serves loopback only.
func move(oldPath, newPath string) error {
	return moveGuarded(oldPath, newPath)
}

func moveGuarded(oldPath, newPath string) error {
	if _, err := os.Lstat(newPath); err == nil {
		return os.ErrExist
	}
	return os.Rename(oldPath, newPath)
}
