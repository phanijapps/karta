package skillhub

// The archive half of repo fetching: bounded tar.gz extraction with
// entry-name safety (rawEntryName), size/count limits
// (countLimitReader), and digest-bearing file maps. Pure I/O shaping —
// no GitHub, no registry knowledge.

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"zbot/backend/internal/registry"
)

type extractedFile struct {
	data []byte
	exec bool
}

// fetchCommitFiles downloads the tarball at the pinned commit and
// extracts it under the ONE envelope.
func (f *GitHubRepoFetcher) fetchCommitFiles(ctx context.Context, src, commit string) (map[string]extractedFile, error) {
	_, code, client := f.bases()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		code+"/"+src+"/tar.gz/"+commit, nil)
	if err != nil {
		return nil, ErrRepoUnreachable
	}
	req.Header.Set("User-Agent", "karta-daemon")
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRepoUnreachable, err)
	}
	defer func() { _ = res.Body.Close() }()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return nil, ErrRepoNotFound
	case res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests:
		return nil, ErrRateLimited
	case res.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: codeload answered %d", ErrRepoUnreachable, res.StatusCode)
	}
	return extractTarGz(io.LimitReader(res.Body, MaxDownloadBytes))
}

// countLimitReader bounds AND counts every byte pulled from the
// decompressed stream — tar metadata and padding included — so the
// decompression envelope holds even when headers lie.
type countLimitReader struct {
	r    io.Reader
	n    int64
	max  int64
	used *int64
}

func (c *countLimitReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	*c.used += int64(n)
	if c.n > c.max {
		return n, fmt.Errorf("stream over bound")
	}
	return n, err
}

// rawEntryName validates an ARCHIVE-RAW entry name before any wrapper
// stripping: no absolutes, no parent components, no backslashes, no
// NUL — cleaning must never erase a prohibited component.
func rawEntryName(name string) error {
	if name == "" || strings.ContainsRune(name, 0) {
		return fmt.Errorf("empty or NUL-bearing entry name")
	}
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "./") {
		return fmt.Errorf("absolute, dot-relative, or backslash entry %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return fmt.Errorf("parent component in entry %q", name)
		}
	}
	return nil
}

// extractTarGz applies the SafeBundle envelope to a gzipped tarball:
// bounded download, bounded DECOMPRESSED stream (headers and padding
// counted, not just file bodies), bounded entries, regular files and
// directories only, raw-name validation before wrapper stripping, and
// duplicate/collision rejection on the stripped tree.
func extractTarGz(r io.Reader) (map[string]extractedFile, error) {
	gz, err := gzip.NewReader(io.LimitReader(r, MaxDownloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: not a gzip stream: %w", ErrArchiveViolation, err)
	}
	var decompressed int64
	// Content ≤ MaxUncompressedByte; tar metadata/padding allowance on top.
	bound := &countLimitReader{r: gz, max: MaxUncompressedByte + 2<<20, used: &decompressed}
	tr := tar.NewReader(bound)
	files := map[string]extractedFile{}
	dirs := map[string]bool{}
	content := int64(0)
	entries := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: corrupt tar: %w", ErrArchiveViolation, err)
		}
		entries++
		if entries > MaxEntries {
			return nil, fmt.Errorf("%w: over %d entries", ErrArchiveViolation, MaxEntries)
		}
		if err := rawEntryName(hdr.Name); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrArchiveViolation, err)
		}
		if hdr.Typeflag == tar.TypeDir {
			// GitHub tarballs prefix with <repo>-<ref>/; strip it.
			dir := hdr.Name
			if i := strings.IndexByte(dir, '/'); i > 0 {
				dir = dir[i+1:]
			}
			if dir != "" {
				dirs[dir] = true
			}
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("%w: non-regular entry %q", ErrArchiveViolation, hdr.Name)
		}
		if hdr.Size > MaxFileBytes {
			return nil, fmt.Errorf("%w: file %q declares %d bytes (over %d)", ErrArchiveViolation, hdr.Name, hdr.Size, MaxFileBytes)
		}
		if content+hdr.Size > MaxUncompressedByte {
			return nil, fmt.Errorf("%w: declared sizes exceed %d uncompressed bytes", ErrArchiveViolation, MaxUncompressedByte)
		}
		name := hdr.Name
		// Strip the wrapper prefix; a bare root file has no home in a repo
		// tree — reject rather than guess.
		i := strings.IndexByte(name, '/')
		if i <= 0 {
			return nil, fmt.Errorf("%w: entry %q lacks the archive wrapper", ErrArchiveViolation, hdr.Name)
		}
		name = name[i+1:]
		if name == "" {
			continue
		}
		if err := registry.ValidBundlePath(name); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrArchiveViolation, err)
		}
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("%w: duplicate entry %q", ErrArchiveViolation, name)
		}
		if dirs[name] {
			return nil, fmt.Errorf("%w: %q is both a file and a directory", ErrArchiveViolation, name)
		}
		data, err := io.ReadAll(io.LimitReader(tr, MaxFileBytes+1))
		if err != nil {
			return nil, fmt.Errorf("%w: read %q: %w", ErrArchiveViolation, name, err)
		}
		if int64(len(data)) != hdr.Size {
			return nil, fmt.Errorf("%w: file %q exceeded its stated size", ErrArchiveViolation, name)
		}
		content += int64(len(data))
		if content > MaxUncompressedByte {
			return nil, fmt.Errorf("%w: archive over %d uncompressed bytes", ErrArchiveViolation, MaxUncompressedByte)
		}
		files[name] = extractedFile{data: data, exec: hdr.Mode&0o111 != 0}
	}
	// Directory/file collisions in both orders.
	for name := range files {
		for dir := name; ; {
			i := strings.LastIndexByte(dir, '/')
			if i <= 0 {
				break
			}
			dir = dir[:i]
			if _, isFile := files[dir]; isFile {
				return nil, fmt.Errorf("%w: %q is both a file and a directory prefix", ErrArchiveViolation, dir)
			}
		}
	}
	return files, nil
}

// detectSkills finds skill roots in the extracted tree exactly as the
// ecosystem lays them out: skills/*/, .agents/skills/*/, the repo root,
// or a single top-level directory; parses SKILL.mds with the loader's
// exact rules; collects licenses.

func decodeLimited(r io.Reader, dst any) error {
	return jsonDecodeLimited(r, dst)
}
