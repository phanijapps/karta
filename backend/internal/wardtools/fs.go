package wardtools

import "os"

// readFileIfExists is the small stdlib shim (no error for absence).
func readFileIfExists(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(raw), nil
}
