package providers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// KeyringMarker is the only legal value of a provider file's `api_key`
// field: it references the OS keyring without ever storing a value.
const KeyringMarker = "keyring"

// errFileExists reports a no-clobber install onto an existing target.
var errFileExists = errors.New("providers: target file already exists")

// envNamePattern mirrors the contract's api_key_env schema (shared rule
// with internal/config so managed files validate identically).
var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// validEnvName enforces the shared credential-reference rule.
func validEnvName(name string) bool { return envNamePattern.MatchString(name) }

// providerFile is the on-disk shape of config/providers/<name>.yaml.
// Strict decoding: unknown keys are rejected.
type providerFile struct {
	Name      string `yaml:"name"`
	Type      string `yaml:"type"`
	BaseURL   string `yaml:"base_url"`
	APIKeyEnv string `yaml:"api_key_env,omitempty"`
	APIKey    string `yaml:"api_key,omitempty"` // must be KeyringMarker when set
}

// loadProviderFiles reads every *.yaml under dir (sorted, deterministic).
// Errors are per-file so one bad hand edit cannot blank the whole store.
func loadProviderFiles(dir string) ([]Provider, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// An absent directory is an empty store (bootstrap self-heals it).
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{fmt.Errorf("providers: read %s: %w", dir, err)}
	}
	var out []Provider
	var errs []error
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".yaml")
		p, err := loadProviderFile(filepath.Join(dir, e.Name()), stem)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, p)
	}
	return out, errs
}

// loadProviderFile decodes and validates one provider file. The file stem
// must equal the declared name, and file names live in the managed
// namespace (they are daemon-managed content).
func loadProviderFile(path, stem string) (Provider, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Provider{}, fmt.Errorf("providers: %s: %w", path, err)
	}
	var f providerFile
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return Provider{}, fmt.Errorf("providers: %s: decode: %w", path, err)
	}
	if !ValidManagedName(f.Name) {
		return Provider{}, fmt.Errorf("providers: %s: name %q is not a managed name (lowercase kebab, 1-64 chars, not a reserved device name)", path, f.Name)
	}
	if f.Name != stem {
		return Provider{}, fmt.Errorf("providers: %s: file stem %q must equal declared name %q", path, stem, f.Name)
	}
	if f.Type == "" {
		f.Type = configProviderType // files may rely on the single supported type
	}
	if f.Type != configProviderType {
		return Provider{}, fmt.Errorf("providers: %s: unsupported provider type %q (supported: %q)", path, f.Type, configProviderType)
	}
	if !validBaseURL(f.BaseURL) {
		return Provider{}, fmt.Errorf("providers: %s: base_url must be an absolute http(s) URL (the provider's API root, e.g. http://localhost:11434/v1)", path)
	}
	if f.APIKeyEnv != "" && !validEnvName(f.APIKeyEnv) {
		return Provider{}, fmt.Errorf("providers: %s: api_key_env %q must be an environment variable name", path, f.APIKeyEnv)
	}
	if f.APIKey != "" && f.APIKey != KeyringMarker {
		return Provider{}, fmt.Errorf("providers: %s: api_key may only be the marker %q (never a value); use api_key_env for environment references", path, KeyringMarker)
	}
	if f.APIKey == KeyringMarker && f.APIKeyEnv != "" {
		return Provider{}, fmt.Errorf("providers: %s: api_key and api_key_env are mutually exclusive", path)
	}
	return Provider{
		Name:       f.Name,
		Type:       f.Type,
		BaseURL:    f.BaseURL,
		APIKeyEnv:  f.APIKeyEnv,
		UseKeyring: f.APIKey == KeyringMarker,
		Origin:     OriginFile,
	}, nil
}

// writeProviderFileNew installs a provider file WITHOUT clobbering an
// existing target (create and rename targets): temp file, then link —
// link fails atomically if the destination exists, so an externally
// authored file the snapshot has not seen yet can never be overwritten.
func writeProviderFileNew(dir string, p Provider) error {
	raw, err := marshalProviderFile(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("providers: ensure %s: %w", dir, err)
	}
	target := providerFilePath(dir, p.Name)
	tmp, err := os.CreateTemp(dir, "."+p.Name+".*.tmp")
	if err != nil {
		return fmt.Errorf("providers: temp write for %s: %w", target, err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("providers: write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("providers: close %s: %w", tmp.Name(), err)
	}
	if err := os.Link(tmp.Name(), target); err != nil {
		if os.IsExist(err) {
			return errFileExists
		}
		return fmt.Errorf("providers: install %s: %w", target, err)
	}
	ok = true
	_ = os.Remove(tmp.Name())
	return nil
}

// marshalProviderFile serializes the on-disk shape.
func marshalProviderFile(p Provider) ([]byte, error) {
	f := providerFile{Name: p.Name, Type: p.Type, BaseURL: p.BaseURL, APIKeyEnv: p.APIKeyEnv}
	if p.UseKeyring {
		f.APIKey = KeyringMarker
	}
	raw, err := yaml.Marshal(&f)
	if err != nil {
		return nil, fmt.Errorf("providers: marshal %s: %w", p.Name, err)
	}
	return raw, nil
}

// writeProviderFile persists one provider file atomically: temp file in
// the same directory, then rename over the target (intentional replace
// for in-place updates).
func writeProviderFile(dir string, p Provider) error {
	raw, err := marshalProviderFile(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("providers: ensure %s: %w", dir, err)
	}
	target := providerFilePath(dir, p.Name)
	tmp, err := os.CreateTemp(dir, "."+p.Name+".*.tmp")
	if err != nil {
		return fmt.Errorf("providers: temp write for %s: %w", target, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after successful rename
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("providers: write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("providers: close %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("providers: commit %s: %w", target, err)
	}
	return nil
}

// removeProviderFile deletes one provider file.
func removeProviderFile(dir, name string) error {
	if err := os.Remove(providerFilePath(dir, name)); err != nil {
		return fmt.Errorf("providers: remove %s: %w", providerFilePath(dir, name), err)
	}
	return nil
}

func providerFilePath(dir, name string) string { return filepath.Join(dir, name+".yaml") }
