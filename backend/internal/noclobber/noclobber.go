// Package noclobber provides a move that never replaces an existing
// destination — the atomic primitive for one-time relocations and seed
// installs. Linux uses renameat2(RENAME_NOREPLACE); other platforms use
// Go's guarded os.Rename (occupied targets fail with IsExist).
//
// Shared by config (legacy-tree migrations) and registry (managed
// installs); sits in the lowest tier beside domain (stdlib + x/sys
// only).
package noclobber

import "os"

// Move moves oldPath to newPath, refusing an existing destination
// (os.ErrExist). Directories and files alike.
func Move(oldPath, newPath string) error {
	return move(oldPath, newPath)
}

// IsExist reports whether err is the occupied-destination signal.
func IsExist(err error) bool { return os.IsExist(err) }
