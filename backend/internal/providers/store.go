package providers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"

	"zbot/backend/internal/config"
)

// Store owns the merged provider set: config.yaml entries (read-only
// origin) plus daemon-managed files under config/providers/. Mutations
// and watcher reloads serialize on one mutex; readers get an immutable
// snapshot via List/Lookup. A failed reload keeps the last good set
// serving (errors are logged) — the registry's swap-despite-errors
// behavior is deliberately NOT copied.
type Store struct {
	dir     string
	cfg     config.Config // config.yaml providers (origin config, read-only)
	secrets SecretStore

	mu       sync.Mutex // serializes mutations and reloads
	snap     atomic.Pointer[Set]
	lastErr  atomic.Pointer[error]
	fw       *fsnotify.Watcher
	stopOnce sync.Once
	quit     chan struct{}
}

// Typed mutation failures, mapped by the server to contract statuses.
var (
	ErrNameExists      = errors.New("providers: name already exists")
	ErrOriginProtected = errors.New("providers: provider is declared in config.yaml (edit the file instead)")
	ErrCapacity        = fmt.Errorf("providers: provider list is full (%d entries)", config.MaxProviders)
)

// ProviderPatch is an edit request: absent fields keep current values.
type ProviderPatch struct {
	Name       *string
	BaseURL    *string
	Credential *CredentialInput
}

// CredentialInput is the explicit credential intent from the API.
type CredentialInput struct {
	Kind    CredentialKind // none | env | keyring
	EnvName string
	Key     string // one-shot value; never persisted outside the keyring
}

// NewStore loads the merged set (config.yaml entries first, then provider
// files) and starts the directory watcher. The initial load fails hard —
// a broken startup state must not serve a partial set silently.
func NewStore(paths config.Paths, cfg config.Config, secrets SecretStore) (*Store, error) {
	s := &Store{dir: paths.ProvidersDir, cfg: cfg, secrets: secrets, quit: make(chan struct{})}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fw.Add(paths.ProvidersDir); err != nil {
		_ = fw.Close()
		return nil, fmt.Errorf("providers: watch %s: %w", paths.ProvidersDir, err)
	}
	s.fw = fw
	go s.watchLoop()
	return s, nil
}

// Close stops the watcher. Idempotent.
func (s *Store) Close() {
	s.stopOnce.Do(func() {
		close(s.quit)
		if s.fw != nil {
			_ = s.fw.Close()
		}
	})
}

// Reload rebuilds the merged snapshot under the store mutex; a load
// error keeps the last good set (recorded via LastError and logged).
func (s *Store) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reloadLocked()
}

// reloadLocked rebuilds and swaps the snapshot; caller holds s.mu.
func (s *Store) reloadLocked() error {
	merged := New(s.cfg.Providers) // config.yaml entries first, origin config
	byName := make(map[string]Provider, len(merged.names))
	for _, name := range merged.names {
		byName[name] = merged.byName[name]
	}
	names := append([]string(nil), merged.names...)

	files, fileErrs := loadProviderFiles(s.dir)
	for _, err := range fileErrs {
		log.Print(err)
	}
	if len(fileErrs) > 0 {
		err := fmt.Errorf("providers: %d invalid provider file(s) kept out of the set (last good set serving); first: %w", len(fileErrs), fileErrs[0])
		s.setLastErr(err)
		return err
	}
	for _, p := range files {
		if err := s.addLocked(&byName, &names, p); err != nil {
			log.Print(err)
			s.setLastErr(err)
			return err
		}
	}
	s.publish(byName, names)
	return nil
}

// addLocked merges one file entry, rejecting duplicates and overflow.
func (s *Store) addLocked(byName *map[string]Provider, names *[]string, p Provider) error {
	if _, dup := (*byName)[p.Name]; dup {
		return fmt.Errorf("providers: duplicate provider name %q (declared in config.yaml and %s)", p.Name, providerFilePath(s.dir, p.Name))
	}
	if len(*names) >= maxProviders {
		return fmt.Errorf("providers: combined provider sources exceed the %d-entry list bound", maxProviders)
	}
	(*byName)[p.Name] = p
	*names = append(*names, p.Name)
	return nil
}

// publish swaps the snapshot and clears the error state.
func (s *Store) publish(byName map[string]Provider, names []string) {
	s.snap.Store(&Set{byName: byName, names: names})
	s.lastErr.Store(nil)
}

func (s *Store) setLastErr(err error) {
	e := err
	s.lastErr.Store(&e)
}

// LastError reports the most recent reload failure (nil when healthy).
func (s *Store) LastError() error {
	if e := s.lastErr.Load(); e != nil {
		return *e
	}
	return nil
}

// List returns the merged provider set in declaration order.
func (s *Store) List() []Provider { return s.snap.Load().List() }

// Lookup resolves one provider by name.
func (s *Store) Lookup(name string) (Provider, error) { return s.snap.Load().Lookup(name) }

// ResolvedLookup resolves a provider AND its request credential as one
// operation serialized against mutations: a delete or rename that starts
// after this call cannot strip the credential from an in-flight request
// (P1 review finding). The outbound HTTP call happens outside the lock.
func (s *Store) ResolvedLookup(ctx context.Context, name string) (Provider, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.snap.Load().Lookup(name)
	if err != nil {
		return Provider{}, "", err
	}
	token, err := ResolveCredential(ctx, p, s.secrets)
	if err != nil {
		return p, "", err
	}
	return p, token, nil
}

// Secrets exposes the store's secret store for credential resolution.
func (s *Store) Secrets() SecretStore { return s.secrets }

// Create adds a daemon-managed provider: validate → prepare secret →
// commit file → publish. A file-commit failure compensates by deleting
// the just-written secret; a failed mutation leaves prior state intact.
func (s *Store) Create(ctx context.Context, entry Provider, cred *CredentialInput) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !ValidManagedName(entry.Name) {
		return Provider{}, fmt.Errorf("providers: name %q is not a managed name (lowercase kebab, 1-64 chars, not a reserved device name)", entry.Name)
	}
	if entry.Type != configProviderType {
		return Provider{}, fmt.Errorf("providers: unsupported provider type %q (supported: %q)", entry.Type, configProviderType)
	}
	if !validBaseURL(entry.BaseURL) {
		return Provider{}, errors.New("providers: base_url must be an absolute http(s) URL (the provider's API root, e.g. http://localhost:11434/v1)")
	}
	if _, err := s.snap.Load().Lookup(entry.Name); err == nil {
		return Provider{}, fmt.Errorf("%w: %q", ErrNameExists, entry.Name)
	}
	if len(s.snap.Load().names) >= maxProviders {
		return Provider{}, ErrCapacity
	}
	if err := validateCredentialInput(cred, true); err != nil {
		return Provider{}, err
	}
	entry.Origin = OriginFile
	applyCredential(&entry, cred)

	// Prepare the secret first: an unavailable keyring writes nothing.
	if entry.UseKeyring {
		if err := s.secrets.Set(ctx, entry.Name, cred.Key); err != nil {
			return Provider{}, err
		}
	}
	if err := writeProviderFileNew(s.dir, entry); err != nil {
		if entry.UseKeyring {
			s.compensate(ctx, entry.Name, "create")
		}
		if errors.Is(err, errFileExists) {
			return Provider{}, fmt.Errorf("%w: %q (file already exists on disk)", ErrNameExists, entry.Name)
		}
		return Provider{}, err
	}
	s.mergeAndPublish(func(byName *map[string]Provider, names *[]string) error {
		return s.addLocked(byName, names, entry)
	}, entry)
	return entry, nil
}

// Update edits a daemon-managed provider (rename, base URL, credential).
// Absent patch fields retain current values; rename re-keys the secret.
// A failed update leaves the old provider fully operational.
func (s *Store) Update(ctx context.Context, name string, patch ProviderPatch) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, err := s.snap.Load().Lookup(name)
	if err != nil {
		return Provider{}, err
	}
	if old.Origin != OriginFile {
		return Provider{}, fmt.Errorf("%w: %q", ErrOriginProtected, name)
	}
	if err := validateCredentialInput(patch.Credential, false); err != nil {
		return Provider{}, err
	}

	next := old
	if patch.BaseURL != nil {
		if !validBaseURL(*patch.BaseURL) {
			return Provider{}, errors.New("providers: base_url must be an absolute http(s) URL")
		}
		next.BaseURL = *patch.BaseURL
	}
	renamed := false
	if patch.Name != nil && *patch.Name != name {
		if !ValidManagedName(*patch.Name) {
			return Provider{}, fmt.Errorf("providers: name %q is not a managed name (lowercase kebab, 1-64 chars, not a reserved device name)", *patch.Name)
		}
		if _, err := s.snap.Load().Lookup(*patch.Name); err == nil {
			return Provider{}, fmt.Errorf("%w: %q", ErrNameExists, *patch.Name)
		}
		next.Name = *patch.Name
		renamed = true
	}
	applyCredential(&next, patch.Credential)

	// Secret work only when something actually changes:
	//   - retained keyring credential, same address, no replacement key →
	//     nothing at all (an unavailable keyring cannot block a URL edit);
	//   - same-name key replacement → back up the prior value so a
	//     file-commit failure can restore it (never lose K1 to K2);
	//   - rename → move the retained/replacement key to the new address.
	replacingKey := patch.Credential != nil && patch.Credential.Kind == CredentialKeyring && patch.Credential.Key != ""
	secretBackup := ""   // prior value to restore on same-name failure
	hadSecret := false   // provider had a readable secret before this edit
	secretPrepared := "" // new value written at the (possibly new) address
	if next.UseKeyring {
		switch {
		case !replacingKey && !renamed:
			// Retained keyring credential, same address (no patch object or
			// keyless retain): no secret ops at all.
		case replacingKey && !renamed:
			secretPrepared = patch.Credential.Key
			if oldVal, err := s.secrets.Get(ctx, old.Name); err == nil {
				hadSecret, secretBackup = true, oldVal
			} else if !errors.Is(err, ErrSecretNotFound) {
				return Provider{}, err
			}
		default:
			// Rename moving the key (retained or replaced), or a first
			// keyring reference introduced by a file edit.
			if replacingKey {
				secretPrepared = patch.Credential.Key
			} else if old.UseKeyring {
				key, err := s.secrets.Get(ctx, old.Name)
				if errors.Is(err, ErrSecretNotFound) {
					key = "" // old secret missing: keep the marker, status stays missing
				} else if err != nil {
					return Provider{}, err
				}
				secretPrepared = key
			}
		}
		if secretPrepared != "" {
			if err := s.secrets.Set(ctx, next.Name, secretPrepared); err != nil {
				return Provider{}, err
			}
		}
	}

	// Commit files. Rename installs the new file without clobbering an
	// unexpected on-disk target, then removes the old file; a remove
	// failure rolls the new file back so the old provider stays intact.
	if renamed {
		if err := writeProviderFileNew(s.dir, next); err != nil {
			if next.UseKeyring && next.Name != old.Name {
				s.compensate(ctx, next.Name, "rename")
			}
			if errors.Is(err, errFileExists) {
				return Provider{}, fmt.Errorf("%w: %q (file already exists on disk)", ErrNameExists, next.Name)
			}
			return Provider{}, err
		}
		if err := removeProviderFile(s.dir, old.Name); err != nil {
			_ = removeProviderFile(s.dir, next.Name)
			return Provider{}, err
		}
	} else if err := writeProviderFile(s.dir, next); err != nil {
		// Same-name replace failed after a secret write: restore the prior
		// secret (or remove the item if none existed) — never lose K1 by
		// leaving K2 in place (P0 review finding).
		if next.UseKeyring && secretPrepared != "" {
			if hadSecret {
				_ = s.secrets.Set(ctx, next.Name, secretBackup)
			} else {
				s.compensate(ctx, next.Name, "update")
			}
		}
		return Provider{}, err
	}

	// Post-commit cleanup: the old address is obsolete after rename or
	// credential removal. Best-effort — an orphan is overwritten on reuse.
	if old.UseKeyring && (renamed || next.CredentialKind() != CredentialKeyring) {
		s.compensate(ctx, old.Name, "update")
	}
	s.mergeAndPublish(func(byName *map[string]Provider, names *[]string) error {
		delete(*byName, old.Name)
		*names = removeName(*names, old.Name)
		return s.addLocked(byName, names, next)
	}, next)
	return next, nil
}

// Delete removes a daemon-managed provider. The file goes first; a failed
// secret cleanup never blocks the delete (orphan overwritten on reuse).
func (s *Store) Delete(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, err := s.snap.Load().Lookup(name)
	if err != nil {
		return err
	}
	if p.Origin != OriginFile {
		return fmt.Errorf("%w: %q", ErrOriginProtected, name)
	}
	if err := removeProviderFile(s.dir, name); err != nil {
		return err
	}
	if p.UseKeyring {
		s.compensate(ctx, name, "delete")
	}
	s.mergeAndPublish(func(byName *map[string]Provider, names *[]string) error {
		delete(*byName, name)
		*names = removeName(*names, name)
		return nil
	}, Provider{})
	return nil
}

// mergeAndPublish applies a local delta to the current snapshot and
// republishes it, so a committed mutation is immediately visible even if
// a concurrent external file breakage would fail a full reload.
func (s *Store) mergeAndPublish(delta func(byName *map[string]Provider, names *[]string) error, entry Provider) {
	cur := s.snap.Load()
	byName := make(map[string]Provider, len(cur.byName))
	for k, v := range cur.byName {
		byName[k] = v
	}
	names := append([]string(nil), cur.names...)
	if err := delta(&byName, &names); err != nil {
		log.Printf("providers: post-commit merge failed: %v", err)
		return
	}
	s.publish(byName, names)
}

func removeName(names []string, name string) []string {
	for i, n := range names {
		if n == name {
			return append(names[:i], names[i+1:]...)
		}
	}
	return names
}

// compensate deletes a secret best-effort and logs the orphan otherwise.
func (s *Store) compensate(ctx context.Context, name, op string) {
	if err := s.secrets.Delete(ctx, name); err != nil && !errors.Is(err, ErrSecretNotFound) {
		log.Printf("providers: %s: keyring cleanup for %q failed (orphan remains; overwritten on name reuse): %v", op, name, err)
	}
}

// validateCredentialInput checks the explicit credential object combos.
// On create, keyring requires a one-shot key; on edit, a keyless keyring
// object means "retain the stored key".
func validateCredentialInput(c *CredentialInput, isCreate bool) error {
	if c == nil {
		return nil
	}
	switch c.Kind {
	case CredentialNone:
		if c.EnvName != "" || c.Key != "" {
			return errors.New("providers: credential kind none takes no fields")
		}
	case CredentialEnv:
		if c.EnvName == "" || c.Key != "" {
			return errors.New("providers: credential kind env requires env_name only")
		}
		if !validEnvName(c.EnvName) {
			return fmt.Errorf("providers: credential env_name %q must be an environment variable name (letters, digits, underscores; at most 128 characters)", c.EnvName)
		}
	case CredentialKeyring:
		if c.Key == "" && isCreate {
			return errors.New("providers: credential kind keyring requires the one-shot key on create (on edit it retains the stored key)")
		}
		if c.EnvName != "" {
			return errors.New("providers: credential kind keyring takes no env_name")
		}
	default:
		return fmt.Errorf("providers: unknown credential kind %q", c.Kind)
	}
	return nil
}

// applyCredential mutates entry per the input; nil input keeps everything.
func applyCredential(entry *Provider, c *CredentialInput) {
	if c == nil {
		return
	}
	entry.APIKeyEnv = ""
	entry.UseKeyring = false
	switch c.Kind {
	case CredentialEnv:
		entry.APIKeyEnv = c.EnvName
	case CredentialKeyring:
		entry.UseKeyring = true
	}
}

// providerWatchDebounce coalesces editor save bursts (registry parity).
const providerWatchDebounce = 150 * time.Millisecond

// watchLoop reloads on provider-directory changes, debounced like the
// registry watcher. Reload runs under the store mutex, so it can never
// publish a stale set over a completed mutation.
func (s *Store) watchLoop() {
	var timer *time.Timer
	for {
		select {
		case <-s.quit:
			if timer != nil {
				timer.Stop()
			}
			return
		case _, ok := <-s.fw.Events:
			if !ok {
				return
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(providerWatchDebounce, func() {
				// Errors are logged inside Reload; last good set keeps serving.
				_ = s.Reload()
			})
		case err, ok := <-s.fw.Errors:
			if !ok {
				return
			}
			log.Printf("providers: watcher error: %v", err)
		}
	}
}
