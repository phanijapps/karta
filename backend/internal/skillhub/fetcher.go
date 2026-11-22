package skillhub

// RepoFetcher: preview + install-by-capability over GitHub-hosted skill
// repositories, under the shared SafeBundle envelope. Byte-binding: the
// preview resolves an immutable commit; installs re-fetch THAT commit
// and verify per-file digests (design D2/D3).

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"zbot/backend/internal/registry"
)

// Typed fetcher failures (mapped by handlers to the contract matrix).
var (
	ErrRepoNotFound     = fmt.Errorf("repository not found")
	ErrRepoUnreachable  = fmt.Errorf("github unreachable")
	ErrRateLimited      = fmt.Errorf("github rate-limited")
	ErrNoSkills         = fmt.Errorf("no valid skills found in repository")
	ErrChangedUpstream  = fmt.Errorf("content changed upstream; re-preview")
	ErrArchiveViolation = fmt.Errorf("archive violation")
)

// FileDigest is one file's size+hash inside a previewed skill.
type FileDigest struct {
	Path string
	Size int64
	Sum  string
	Exec bool
}

// PreviewSkill is one detected skill in a repository (preview payload).
type PreviewSkill struct {
	Name        string
	Description string
	Body        string
	Files       []FileDigest // sorted, relative to the skill root
	License     string
	Root        string // repository-relative skill root
}

// RepoPreview is the byte-binding preview of a whole repository.
type RepoPreview struct {
	Source     string // owner/repo
	Commit     string // immutable SHA the preview pinned
	Skills     []PreviewSkill
	License    string
	TotalFiles int
	skillRoots map[string]string
}

// RepoFetcher previews repositories and re-extracts pinned commits.
type RepoFetcher interface {
	// Preview resolves the default branch to an immutable commit and
	// returns the repository's skills bound to that commit.
	Preview(ctx context.Context, source string) (*RepoPreview, error)
	// ExtractCommit re-fetches the PINNED commit and returns each skill's
	// bundle (paths relative to the skill root) for digest verification.
	ExtractCommit(ctx context.Context, source, commit string) (map[string][]registry.SkillFile, map[string]map[string]FileDigest, error)
}

// GitHubRepoFetcher is the unauthenticated GitHub implementation.
type GitHubRepoFetcher struct {
	APIBase  string // default https://api.github.com
	CodeBase string // default https://codeload.github.com
	Client   *http.Client
}

// NewGitHubRepoFetcher — NewGitHubRepoFetcher builds the default GitHub fetcher.
func NewGitHubRepoFetcher() *GitHubRepoFetcher {
	return &GitHubRepoFetcher{
		APIBase:  "https://api.github.com",
		CodeBase: "https://codeload.github.com",
		Client:   &http.Client{Timeout: FetchDeadline},
	}
}

var ownerRepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)

// ParseSource canonicalizes a pasted source into owner/repo under a
// STRICT policy: bare owner/repo, github.com/owner/repo, or
// https://github.com/owner/repo (optionally /tree/<ref>, which is
// dropped; .git suffix dropped). http, other hosts, credentials, and
// arbitrary deeper paths reject — never an arbitrary fetch destination.
func ParseSource(source string) (string, error) {
	s := strings.TrimSpace(source)
	s = strings.TrimSuffix(s, ".git")
	if strings.HasPrefix(strings.ToLower(s), "github.com/") {
		s = "https://" + s
	}
	if u, err := url.Parse(s); err == nil && u.Scheme != "" {
		if u.Scheme != "https" || strings.ToLower(u.Host) != "github.com" || u.User != nil {
			return "", fmt.Errorf("%w: %q is not an https github.com repository", ErrArchiveViolation, source)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		// Optional, explicitly-defined suffix: /tree/<ref> (ref dropped).
		if len(parts) == 4 && parts[2] == "tree" {
			parts = parts[:2]
		}
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", fmt.Errorf("%w: %q is not a github.com owner/repo path", ErrArchiveViolation, source)
		}
		s = parts[0] + "/" + parts[1]
	}
	if !ownerRepoPattern.MatchString(s) || strings.Contains(s, "..") {
		return "", fmt.Errorf("%w: %q is not a GitHub owner/repo", ErrArchiveViolation, source)
	}
	return s, nil
}

func (f *GitHubRepoFetcher) bases() (api, code string, client *http.Client) {
	client = f.Client
	if client == nil {
		client = &http.Client{Timeout: FetchDeadline}
	}
	api, code = f.APIBase, f.CodeBase
	if api == "" {
		api = "https://api.github.com"
	}
	if code == "" {
		code = "https://codeload.github.com"
	}
	return api, code, client
}

// Preview resolves default-branch → immutable commit → tarball → skills.
func (f *GitHubRepoFetcher) Preview(ctx context.Context, source string) (*RepoPreview, error) {
	src, err := ParseSource(source)
	if err != nil {
		return nil, err
	}
	commit, err := f.defaultCommit(ctx, src)
	if err != nil {
		return nil, err
	}
	files, err := f.fetchCommitFiles(ctx, src, commit)
	if err != nil {
		return nil, err
	}
	return detectSkills(src, commit, files)
}

// defaultCommit resolves the repo's default branch to its commit SHA
// (previews pin immutable commits; branches move).
func (f *GitHubRepoFetcher) defaultCommit(ctx context.Context, src string) (string, error) {
	api, _, client := f.bases()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api+"/repos/"+src, nil)
	if err != nil {
		return "", ErrRepoUnreachable
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "karta-daemon")
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrRepoUnreachable, err)
	}
	defer func() { _ = res.Body.Close() }()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return "", ErrRepoNotFound
	case res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests:
		return "", ErrRateLimited
	case res.StatusCode != http.StatusOK:
		return "", fmt.Errorf("%w: github answered %d", ErrRepoUnreachable, res.StatusCode)
	}
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := decodeLimited(res.Body, &repo); err != nil {
		return "", fmt.Errorf("%w: malformed repo response", ErrRepoUnreachable)
	}
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, api+"/repos/"+src+"/commits/"+repo.DefaultBranch, nil)
	if err != nil {
		return "", ErrRepoUnreachable
	}
	req2.Header.Set("Accept", "application/vnd.github+json")
	req2.Header.Set("User-Agent", "karta-daemon")
	res2, err := client.Do(req2)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrRepoUnreachable, err)
	}
	defer func() { _ = res2.Body.Close() }()
	if res2.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: commits answered %d", ErrRepoUnreachable, res2.StatusCode)
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := decodeLimited(res2.Body, &commit); err != nil || commit.SHA == "" {
		return "", fmt.Errorf("%w: malformed commit response", ErrRepoUnreachable)
	}
	return commit.SHA, nil
}

// extractedFile is one envelope-validated archive file.

func detectSkills(src, commit string, files map[string]extractedFile) (*RepoPreview, error) {
	// Group files by directory.
	dirs := map[string][]string{}
	var license string
	for name := range files {
		if name == "SKILL.md" || strings.HasSuffix(name, "/SKILL.md") {
			dir := path.Dir(name)
			if dir == "SKILL.md" {
				dir = "."
			}
			dirs[dir] = append(dirs[dir], name)
		}
		if license == "" && (name == "LICENSE" || name == "LICENSE.md" || name == "LICENSE.txt") {
			license = name
		}
	}
	// Candidate roots: the known layouts.
	var roots []string
	for dir := range dirs {
		switch {
		case dir == ".":
		case strings.HasPrefix(dir, "skills/") && strings.Count(dir, "/") == 1:
		case strings.HasPrefix(dir, ".agents/skills/") && strings.Count(dir, "/") == 2:
		default:
			continue
		}
		roots = append(roots, dir)
	}
	// Single top-level directory layout when nothing matched.
	if len(roots) == 0 && len(dirs) == 1 {
		for dir := range dirs {
			if strings.Count(dir, "/") == 0 && dir != "." {
				roots = append(roots, dir)
			}
		}
	}
	sort.Strings(roots)
	if len(roots) == 0 {
		return nil, ErrNoSkills
	}

	preview := &RepoPreview{Source: src, Commit: commit, License: license, TotalFiles: len(files), skillRoots: map[string]string{}}
	for _, root := range roots {
		ef, ok := files[path.Join(root, "SKILL.md")]
		if !ok {
			continue
		}
		name, desc, body, err := registry.ParseSkillMD(string(ef.data))
		if err != nil {
			continue // loader-exact: preview refuses what the registry would drop
		}
		skill := PreviewSkill{Name: name, Description: desc, Body: body, License: license, Root: root}
		prefix := ""

		if root != "." {
			prefix = root + "/"
		}
		var relPaths []string
		for f := range files {
			if strings.HasPrefix(f, prefix) {
				rel := strings.TrimPrefix(f, prefix)
				if rel == "" {
					continue
				}
				relPaths = append(relPaths, rel)
			}
		}
		sort.Strings(relPaths)
		for _, rel := range relPaths {
			if rel == ".karta/provenance.yaml" || strings.HasPrefix(rel, ".karta/provenance.yaml/") {
				return nil, fmt.Errorf("%w: reserved provenance path inside skill %q", ErrArchiveViolation, name)
			}
			file := files[prefix+rel]
			sum := sha256.Sum256(file.data)
			skill.Files = append(skill.Files, FileDigest{Path: rel, Size: int64(len(file.data)), Sum: shortHex(sum[:]), Exec: file.exec})
		}
		preview.Skills = append(preview.Skills, skill)
		preview.skillRoots[name] = root
	}
	if len(preview.Skills) == 0 {
		return nil, ErrNoSkills
	}
	return preview, nil
}

// ExtractCommit re-fetches a pinned commit into per-skill bundles with
// digest verification against the capability's recorded digests.
func (f *GitHubRepoFetcher) ExtractCommit(ctx context.Context, source, commit string) (map[string][]registry.SkillFile, map[string]map[string]FileDigest, error) {
	src, err := ParseSource(source)
	if err != nil {
		return nil, nil, err
	}
	files, err := f.fetchCommitFiles(ctx, src, commit)
	if err != nil {
		return nil, nil, err
	}
	preview, err := detectSkills(src, commit, files)
	if err != nil {
		return nil, nil, err
	}
	bundles := map[string][]registry.SkillFile{}
	digestsBySkill := map[string]map[string]FileDigest{}
	for _, skill := range preview.Skills {
		prefix := ""
		if skill.Root != "." {
			prefix = skill.Root + "/"
		}
		bundle := make([]registry.SkillFile, 0, len(skill.Files))
		for _, dig := range skill.Files {
			ef, ok := files[prefix+dig.Path]
			if !ok {
				return nil, nil, fmt.Errorf("%w: file %q vanished between preview and install", ErrChangedUpstream, dig.Path)
			}
			sum := sha256.Sum256(ef.data)
			if shortHex(sum[:]) != dig.Sum || int64(len(ef.data)) != dig.Size {
				return nil, nil, fmt.Errorf("%w: %q digest mismatch", ErrChangedUpstream, dig.Path)
			}
			mode := fs.FileMode(0o644)
			if ef.exec {
				mode = 0o755
			}
			bundle = append(bundle, registry.SkillFile{Path: dig.Path, Mode: mode, Data: ef.data})
		}
		bundles[skill.Name] = bundle
		digestsBySkill[skill.Name] = map[string]FileDigest{}
		for _, d := range skill.Files {
			digestsBySkill[skill.Name][d.Path] = d
		}
	}
	return bundles, digestsBySkill, nil
}
