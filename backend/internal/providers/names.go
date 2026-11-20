package providers

import "regexp"

// managedNamePattern constrains daemon-created provider names (and every
// file-declared name) to a portable filename-safe namespace: lowercase
// kebab, 1-64 chars. Case-insensitive filesystems cannot collide, colons
// and other platform-hostile characters are excluded, and the length fits
// every mainstream filesystem's stem budget.
var managedNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// reservedWindowsNames are device names that cannot be file stems on
// Windows regardless of extension (case-insensitive).
var reservedWindowsNames = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {}, "com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {}, "lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
}

// ValidManagedName reports whether name is a portable managed-provider
// name (file stem). Config.yaml-declared names keep the wider Id pattern
// and never pass through here.
func ValidManagedName(name string) bool {
	if !managedNamePattern.MatchString(name) {
		return false
	}
	_, reserved := reservedWindowsNames[name]
	return !reserved
}
