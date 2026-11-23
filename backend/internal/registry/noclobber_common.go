package registry

import "zbot/backend/internal/noclobber"

// renameDirNoClobber moves a directory atomically, refusing an existing
// target: one shared primitive (internal/noclobber), used by the
// managed installs and the config-tree migrations alike.
func renameDirNoClobber(oldDir, newDir string) error {
	return noclobber.Move(oldDir, newDir)
}
