package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProviderFileRules(t *testing.T) {
	dir := t.TempDir()

	write := func(stem, content string) string {
		path := filepath.Join(dir, stem+".yaml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("valid file entry with keyring marker", func(t *testing.T) {
		path := write("ollama", "name: ollama\ntype: openai-compatible\nbase_url: http://localhost:11434/v1\napi_key: keyring\n")
		p, err := loadProviderFile(path, "ollama")
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if !p.UseKeyring || p.APIKeyEnv != "" || p.Origin != OriginFile {
			t.Fatalf("unexpected entry: %+v", p)
		}
	})

	t.Run("type defaults to the supported family", func(t *testing.T) {
		path := write("plain", "name: plain\nbase_url: http://localhost:1/v1\n")
		p, err := loadProviderFile(path, "plain")
		if err != nil || p.Type != configProviderType {
			t.Fatalf("want defaulted type, got %+v err %v", p, err)
		}
	})

	t.Run("api_key carrying a value is rejected", func(t *testing.T) {
		path := write("leaky", "name: leaky\nbase_url: http://localhost:1/v1\napi_key: super-secret\n")
		_, err := loadProviderFile(path, "leaky")
		if err == nil || !strings.Contains(err.Error(), "marker") {
			t.Fatalf("want marker-only error, got: %v", err)
		}
	})

	t.Run("stem must equal declared name", func(t *testing.T) {
		path := write("alpha", "name: beta\nbase_url: http://localhost:1/v1\n")
		_, err := loadProviderFile(path, "alpha")
		if err == nil || !strings.Contains(err.Error(), "stem") {
			t.Fatalf("want stem mismatch error, got: %v", err)
		}
	})

	t.Run("unknown keys rejected", func(t *testing.T) {
		path := write("extra", "name: extra\nbase_url: http://localhost:1/v1\nsurprise: yes\n")
		_, err := loadProviderFile(path, "extra")
		if err == nil {
			t.Fatal("unknown key must be rejected")
		}
	})

	t.Run("invalid base url rejected", func(t *testing.T) {
		path := write("badurl", "name: badurl\nbase_url: localhost:1\n")
		_, err := loadProviderFile(path, "badurl")
		if err == nil || !strings.Contains(err.Error(), "base_url") {
			t.Fatalf("want base_url error, got: %v", err)
		}
	})
}

func TestWriteProviderFileAtomic(t *testing.T) {
	dir := t.TempDir()
	p := Provider{Name: "zai", Type: configProviderType, BaseURL: "https://api.z.ai/v1", UseKeyring: true, Origin: OriginFile}
	if err := writeProviderFile(dir, p); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "zai.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	for _, want := range []string{"name: zai", "base_url: https://api.z.ai/v1", "api_key: keyring"} {
		if !strings.Contains(content, want) {
			t.Fatalf("file missing %q:\n%s", want, content)
		}
	}
	// No temp files remain after the rename.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("want exactly one file, got %d", len(entries))
	}
	// Round-trips through the loader.
	back, err := loadProviderFile(filepath.Join(dir, "zai.yaml"), "zai")
	if err != nil || back.Name != "zai" || !back.UseKeyring {
		t.Fatalf("round-trip: %+v err %v", back, err)
	}
}
