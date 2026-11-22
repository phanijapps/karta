package server

// Skill marketplace handlers (design D2/D5/D7 of
// skill-marketplace-and-import): daemon-proxied search, byte-binding
// repo previews issuing capabilities, capability-consumed installs
// with pinned-commit digest verification, and the two-step archive
// import (multipart preview staging + one-shot commit).

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"zbot/backend/internal/registry"
	apigen "zbot/backend/internal/server/apigen"
	"zbot/backend/internal/skillhub"
)

// skillHub bundles the marketplace seams injected into the Server.
type skillHub struct {
	index  skillhub.IndexClient
	fetch  skillhub.RepoFetcher
	stores *skillhub.Store
}

// SetSkillHub wires the marketplace plane (daemon start); tests inject
// fakes. Without it the marketplace endpoints answer typed 503s.
func (s *Server) SetSkillHub(index skillhub.IndexClient, fetch skillhub.RepoFetcher, store *skillhub.Store) {
	s.hub = &skillHub{index: index, fetch: fetch, stores: store}
}

func (s *Server) hubReady() bool {
	return s.hub != nil && s.hub.index != nil && s.hub.fetch != nil && s.hub.stores != nil
}

func (s *Server) hubUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "marketplace_unavailable", "the skill marketplace is not configured on this daemon")
}

// SearchSkillMarketplace proxies the pinned skills.sh index.
func (s *Server) SearchSkillMarketplace(w http.ResponseWriter, r *http.Request, params apigen.SearchSkillMarketplaceParams) {
	if !s.hubReady() {
		s.hubUnavailable(w)
		return
	}
	q := strings.TrimSpace(params.Q)
	if q == "" || len(q) > 128 {
		writeError(w, http.StatusBadRequest, "invalid_request", "q is required (1-128 chars)")
		return
	}
	results, err := s.hub.index.Search(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusBadGateway, "index_unreachable", err.Error())
		return
	}
	out := make([]apigen.SkillMarketSkill, 0, len(results))
	for _, res := range results {
		installs := res.Installs
		out = append(out, apigen.SkillMarketSkill{Id: res.ID, Source: res.Source, Name: res.Name, Installs: &installs})
	}
	writeJSON(w, http.StatusOK, apigen.SkillMarketSearchResult{Skills: out})
}

// PreviewSkillSource resolves a repo to an immutable commit and returns
// per-skill previews plus the install capability.
// Skill-install outcome statuses (contract vocabulary; mirrors the MCP
// import statuses in internal/mcpimport).
const (
	statusCreated = "created"
	statusSkipped = "skipped"
	statusFailed  = "failed"
)

// PreviewSkillSource previews a skill repo paste (contract POST).
func (s *Server) PreviewSkillSource(w http.ResponseWriter, r *http.Request) {
	if !s.hubReady() {
		s.hubUnavailable(w)
		return
	}
	var req apigen.SkillSourceInput
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	preview, err := s.hub.fetch.Preview(r.Context(), req.Source)
	if err != nil {
		writeSkillFetchError(w, err)
		return
	}
	digests := map[string]map[string]skillhub.FileDigest{}
	skills := make([]apigen.SkillPreviewPayload, 0, len(preview.Skills))
	for _, sk := range preview.Skills {
		digests[sk.Name] = map[string]skillhub.FileDigest{}
		files := make([]apigen.SkillFileEntry, 0, len(sk.Files))
		for _, f := range sk.Files {
			exec := f.Exec
			size := f.Size
			sum := f.Sum
			files = append(files, apigen.SkillFileEntry{Path: f.Path, Size: size, Sha256: &sum, Executable: &exec})
			digests[sk.Name][f.Path] = f
		}
		license := sk.License
		skills = append(skills, apigen.SkillPreviewPayload{
			Name: sk.Name, Description: sk.Description, Body: sk.Body, Files: files, License: &license,
		})
	}
	capability, err := s.hub.stores.RegisterRepoPreview(preview.Source, preview.Commit, digests)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "io_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apigen.SkillRepoPreview{
		Source: preview.Source, Commit: preview.Commit, Capability: capability, Skills: skills,
	})
}

// InstallSkillCapability consumes a preview capability for the chosen
// subset: re-fetch the PINNED commit, verify digests, install per skill.
func (s *Server) InstallSkillCapability(w http.ResponseWriter, r *http.Request) {
	if !s.hubReady() {
		s.hubUnavailable(w)
		return
	}
	var req apigen.SkillInstallRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	source, commit, recorded, ok := s.hub.stores.ConsumeRepo(req.Capability)
	if !ok {
		writeError(w, http.StatusNotFound, "capability_expired", "preview expired, unknown, or already used — re-preview the source")
		return
	}
	// Subset AUTHORIZATION before any fetch: every requested name must be
	// recorded in the preview binding — an unrecorded name never fetches
	// or writes, and the subset must be nonempty and bounded.
	if len(req.Skills) == 0 || len(req.Skills) > 64 {
		writeError(w, http.StatusBadRequest, "invalid_request", "skills must name 1-64 previewed entries")
		return
	}
	for _, name := range req.Skills {
		if _, present := recorded[name]; !present {
			writeError(w, http.StatusBadRequest, "invalid_skill", fmt.Sprintf("skill %q was not in the preview", name))
			return
		}
	}
	bundles, extracted, err := s.hub.fetch.ExtractCommit(r.Context(), source, commit)
	if err != nil {
		writeSkillFetchError(w, err)
		return
	}
	// Byte binding: the re-fetch must match the previewed digests (content
	// AND executable mode) for exactly the authorized subset.
	expected := map[string]map[string]skillhub.FileDigest{}
	for _, name := range req.Skills {
		expected[name] = recorded[name]
	}
	if err := skillhub.CompareDigests(expected, extracted); err != nil {
		writeError(w, http.StatusBadGateway, "changed_upstream", err.Error())
		return
	}
	results := make([]apigen.SkillInstallOutcome, 0, len(req.Skills))
	for _, name := range req.Skills {
		results = append(results, s.installSkillBundle(name, bundles[name], &registry.ProvenanceInput{
			Origin: registry.OriginMarketplace, Source: source, Commit: commit,
		}))
	}
	writeJSON(w, http.StatusOK, apigen.SkillInstallResult{Results: results})
}

// installSkillBundle installs one extracted bundle with a per-skill outcome.
func (s *Server) installSkillBundle(name string, files []registry.SkillFile, prov *registry.ProvenanceInput) apigen.SkillInstallOutcome {
	originEnum := apigen.SkillInstallOutcomeOrigin(prov.Origin)
	out := apigen.SkillInstallOutcome{Name: name, Status: statusCreated, Origin: &originEnum}
	if len(files) == 0 {
		msg := "skill not in the preview"
		out.Status, out.Reason, out.Message = statusFailed, ptrSkillReason("invalid_skill"), &msg
		return out
	}
	skillName, description := name, ""
	for _, f := range files {
		if f.Path == "SKILL.md" {
			if n, d, _, perr := registry.ParseSkillMD(string(f.Data)); perr == nil {
				skillName, description = n, d
			}
			break
		}
	}
	spec, err := s.reg.InstallSkill(registry.SkillBundle{
		Name: skillName, Description: description, Files: files, Provenance: prov,
	})
	if err == nil {
		slug := spec.Path[strings.LastIndex(spec.Path, string(os.PathSeparator))+1:]
		out.Slug = &slug
		return out
	}
	var me *registry.MutationError
	ok := errors.As(err, &me)
	if !ok {
		msg := err.Error()
		out.Status, out.Reason, out.Message = statusFailed, ptrSkillReason("io_error"), &msg
		return out
	}
	msg := me.Message
	switch me.Code {
	case registry.ErrDuplicateName:
		out.Status, out.Reason = statusSkipped, ptrSkillReason("duplicate")
	case registry.ErrInvalidName, registry.ErrInvalidManifest:
		out.Status, out.Reason, out.Message = statusFailed, ptrSkillReason("invalid_skill"), &msg
	case registry.ErrCapacity:
		out.Status, out.Reason, out.Message = statusFailed, ptrSkillReason("capacity"), &msg
	default:
		out.Status, out.Reason, out.Message = statusFailed, ptrSkillReason("io_error"), &msg
	}
	return out
}

// PreviewSkillArchive handles the single multipart-exempt route: one
// `archive` file part only, ≤10 MiB, staged under the envelope.
func (s *Server) PreviewSkillArchive(w http.ResponseWriter, r *http.Request) {
	if !s.hubReady() {
		s.hubUnavailable(w)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "archive_too_large", "the upload exceeds 10 MiB")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_import", "malformed multipart body")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["archive"]) != 1 || len(r.MultipartForm.Value) != 0 {
		writeError(w, http.StatusBadRequest, "invalid_import", "exactly one file part named archive is required (no other parts)")
		return
	}
	fh := r.MultipartForm.File["archive"][0]
	if fh.Size > 10<<20 {
		writeError(w, http.StatusRequestEntityTooLarge, "archive_too_large", "the upload exceeds 10 MiB")
		return
	}
	f, err := fh.Open()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_import", "cannot read the archive part")
		return
	}
	data := make([]byte, fh.Size)
	if _, err := readFull(f, data); err != nil {
		_ = f.Close()
		writeError(w, http.StatusBadRequest, "invalid_import", "cannot read the archive part")
		return
	}
	_ = f.Close()

	preview, id, err := s.hub.stores.StageArchive(data)
	if err != nil {
		if errors.Is(err, skillhub.ErrStageBusy) {
			writeError(w, http.StatusBadRequest, "staging_busy", "too many staged imports; retry shortly")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_import", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, apigen.SkillArchivePreview{StagingId: id, Skill: previewPayload(preview.Skill)})
}

// CommitSkillArchive installs a staged archive (one-shot capability).
func (s *Server) CommitSkillArchive(w http.ResponseWriter, r *http.Request) {
	if !s.hubReady() {
		s.hubUnavailable(w)
		return
	}
	var req apigen.SkillImportCommitRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	staged, err := s.hub.stores.CommitArchive(req.StagingId)
	if err != nil {
		writeError(w, http.StatusNotFound, "capability_expired", "staging expired, unknown, or already used — preview the archive again")
		return
	}
	files := staged.StagedFiles()
	if files == nil {
		writeError(w, http.StatusInternalServerError, "io_error", "staged archive unreadable — preview it again")
		return
	}
	outcome := s.installSkillBundle(staged.Skill.Name, files, &registry.ProvenanceInput{Origin: registry.OriginImport})
	writeJSON(w, http.StatusOK, outcome)
}

func previewPayload(sk skillhub.PreviewSkill) apigen.SkillPreviewPayload {
	files := make([]apigen.SkillFileEntry, 0, len(sk.Files))
	for _, f := range sk.Files {
		exec := f.Exec
		size := f.Size
		sum := f.Sum
		files = append(files, apigen.SkillFileEntry{Path: f.Path, Size: size, Sha256: &sum, Executable: &exec})
	}
	license := sk.License
	return apigen.SkillPreviewPayload{
		Name: sk.Name, Description: sk.Description, Body: sk.Body, Files: files, License: &license,
	}
}

func writeSkillFetchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, skillhub.ErrRepoNotFound):
		writeError(w, http.StatusNotFound, "repo_not_found", err.Error())
	case errors.Is(err, skillhub.ErrRateLimited):
		writeError(w, http.StatusBadGateway, "upstream_rate_limited", "GitHub rate limit reached (unauthenticated) — retry in a minute or paste the repository later")
	case errors.Is(err, skillhub.ErrRepoUnreachable):
		writeError(w, http.StatusBadGateway, "upstream_unreachable", err.Error())
	case errors.Is(err, skillhub.ErrNoSkills):
		writeError(w, http.StatusBadGateway, "no_skills_found", "no valid skills found in the repository (layouts: skills/*/, .agents/skills/*/, root SKILL.md)")
	case errors.Is(err, skillhub.ErrArchiveViolation):
		writeError(w, http.StatusBadRequest, "invalid_import", err.Error())
	default:
		writeError(w, http.StatusBadGateway, "upstream_unreachable", err.Error())
	}
}

func ptrSkillReason(reason string) *apigen.SkillInstallOutcomeReason {
	r := apigen.SkillInstallOutcomeReason(reason)
	return &r
}

// readFull reads exactly len(buf) bytes.
func readFull(r io.Reader, buf []byte) (int, error) {
	return io.ReadFull(r, buf)
}
