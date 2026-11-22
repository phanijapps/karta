// Package skillhub is the daemon-side skills marketplace plane: the
// pinned skills.sh index client, the GitHub repository fetcher, the
// shared SafeBundle extraction envelope, the .skill/.zip archive
// importer with staged bearer capabilities, and the preview-capability
// store binding installs to previewed bytes.
//
// Graph placement: imports `registry` (types + ParseSkillMD +
// InstallSkill) and stdlib only; sits below `server`, never touches
// engine/* or clients. The daemon dials out ONLY at explicit user
// action (search/preview/install); unit tests run entirely against
// fakes and fixtures — zero network.
package skillhub

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// The ONE safety envelope (design D3): every remote or archived source
// — GitHub tarballs and uploaded archives alike — passes these caps.
const (
	MaxDownloadBytes    = 50 << 20 // compressed fetch / upload bound
	MaxUncompressedByte = 50 << 20 // total extracted bytes
	MaxEntries          = 512      // entries walked per archive
	MaxFileBytes        = 10 << 20 // per extracted file
	FetchDeadline       = 20 * time.Second
)

// Capability TTLs and bounds (design D8).
const (
	CapabilityTTL      = 15 * time.Minute
	MaxArchiveStages   = 8
	MaxRepoPreviews    = 16
	previewCacheTTL    = 5 * time.Minute
	searchCacheTTL     = 5 * time.Minute
	maxSearchCacheKeys = 32
)

// IndexSkill is one skills.sh search result.
type IndexSkill struct {
	ID       string `json:"id"`     // owner/repo/skill slug
	Source   string `json:"source"` // owner/repo
	SkillID  string `json:"skillId"`
	Name     string `json:"name"`
	Installs int64  `json:"installs"`
}

// IndexClient searches the pinned skills.sh index. Typed failure:
// ErrIndexUnreachable — never fabricated results.
type IndexClient interface {
	Search(ctx context.Context, query string) ([]IndexSkill, error)
}

// ErrIndexUnreachable is the typed index failure.
var ErrIndexUnreachable = fmt.Errorf("skills.sh index unreachable")

// HTTPIndexClient talks to the pinned endpoint (preflight record in the
// change's design.md): GET https://skills.sh/api/search?q=<query>.
type HTTPIndexClient struct {
	BaseURL string // override for tests; default https://skills.sh
	Client  *http.Client

	mu    sync.Mutex
	cache map[string]cachedSearch
}

type cachedSearch struct {
	at      time.Time
	results []IndexSkill
}

// NewHTTPIndexClient — NewHTTPIndexClient builds the index client with default limits.
func NewHTTPIndexClient() *HTTPIndexClient {
	return &HTTPIndexClient{
		BaseURL: "https://skills.sh",
		Client:  &http.Client{Timeout: FetchDeadline},
		cache:   map[string]cachedSearch{},
	}
}

// Search queries the index with a five-minute cache: fresh cache
// serves, expired re-queries synchronously; there is no background
// refresh and no stale success.
func (c *HTTPIndexClient) Search(ctx context.Context, query string) ([]IndexSkill, error) {
	c.mu.Lock()
	if hit, ok := c.cache[query]; ok && time.Since(hit.at) < searchCacheTTL {
		c.mu.Unlock()
		return hit.results, nil
	}
	c.mu.Unlock()

	base := c.BaseURL
	if base == "" {
		base = "https://skills.sh"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/api/search?q="+url.QueryEscape(query), nil)
	if err != nil {
		return nil, ErrIndexUnreachable
	}
	httpClient := c.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: FetchDeadline}
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrIndexUnreachable, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: index answered %d", ErrIndexUnreachable, res.StatusCode)
	}
	var body struct {
		Skills []struct {
			ID       string `json:"id"`
			Source   string `json:"source"`
			SkillID  string `json:"skillId"`
			Name     string `json:"name"`
			Installs int64  `json:"installs"`
		} `json:"skills"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, res.Body, MaxDownloadBytes))
	if err := dec.Decode(&body); err != nil {
		return nil, fmt.Errorf("%w: malformed index response: %w", ErrIndexUnreachable, err)
	}
	out := make([]IndexSkill, 0, len(body.Skills))
	for _, s := range body.Skills {
		out = append(out, IndexSkill{ID: s.ID, Source: s.Source, SkillID: s.SkillID, Name: s.Name, Installs: s.Installs})
	}
	c.mu.Lock()
	c.cache[query] = cachedSearch{at: time.Now(), results: out}
	// Bound the cache; drop the oldest quarter when full.
	if len(c.cache) > maxSearchCacheKeys {
		for q := range c.cache {
			delete(c.cache, q)
			if len(c.cache) <= maxSearchCacheKeys/4*3 {
				break
			}
		}
	}
	c.mu.Unlock()
	return out, nil
}

// randomToken returns a 256-bit opaque bearer token, base64url.
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// shortHex renders a digest for previews/capabilities.
func shortHex(b []byte) string { return hex.EncodeToString(b) }
