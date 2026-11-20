package providers

import "testing"

func TestValidManagedName(t *testing.T) {
	valid := []string{"ollama", "zai", "ollama-2", "a", "gpt-oss-120b", strings64()}
	for _, name := range valid {
		if !ValidManagedName(name) {
			t.Errorf("%q should be a valid managed name", name)
		}
	}
	invalid := map[string]string{
		"Ollama":       "uppercase",
		"ollama_cloud": "underscore",
		"ollama:11434": "colon",
		"-ollama":      "leading dash",
		"2ollama":      "", // digits-first is fine; keep as valid control
		"con":          "Windows reserved",
		"CON":          "Windows reserved (case)",
		"lpt1":         "Windows reserved",
		"ollama cloud": "space",
		"ollama/cloud": "slash",
		"ollama.cloud": "dot",
		strings65():    "too long",
		"":             "empty",
	}
	for name, why := range invalid {
		if why == "" {
			continue
		}
		if ValidManagedName(name) {
			t.Errorf("%q should be invalid (%s)", name, why)
		}
	}
}

func strings64() string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

func strings65() string {
	b := make([]byte, 65)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
