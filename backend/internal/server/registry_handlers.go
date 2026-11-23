package server

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"zbot/backend/internal/mcpclient"
	"zbot/backend/internal/registry"
	apigen "zbot/backend/internal/server/apigen"
)

// Registry-backed contract operations: MCP server management (with the
// connection probe) and skill management. Mutations run through the
// registry's write paths (serialized with its watcher); listings carry
// per-file load errors so hand-edited breakage is visible.

// maxLoadErrors bounds the load_errors arrays on list responses.
const maxLoadErrors = 64

// --- MCP handlers ----------------------------------------------------------

// ListMcps returns every registered MCP server plus per-file load errors.
func (s *Server) ListMcps(w http.ResponseWriter, r *http.Request) {
	snap := s.reg.Snapshot()
	names := make([]string, 0, len(snap.MCPs))
	for name := range snap.MCPs {
		names = append(names, name)
	}
	sort.Strings(names)
	servers := make([]apigen.Mcp, 0, len(names))
	for _, name := range names {
		servers = append(servers, mcpView(name, snap.MCPs[name]))
	}
	writeJSON(w, http.StatusOK, apigen.McpList{Servers: servers, LoadErrors: loadErrorsView(s.reg, registry.KindMCP)})
}

// CreateMcp installs a managed manifest and returns it as now registered.
func (s *Server) CreateMcp(w http.ResponseWriter, r *http.Request) {
	var req apigen.McpCreate
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	input, err := mcpInput(req.Stdio, req.Http, req.Enabled)
	if err != nil {
		writeRegistryError(w, err, registry.KindMCP)
		return
	}
	created, err := s.reg.CreateMCP(string(req.Name), input)
	if err != nil {
		writeRegistryError(w, err, registry.KindMCP)
		return
	}
	writeJSON(w, http.StatusCreated, mcpView(string(req.Name), created))
}

// UpdateMcp replaces a server's manifest wholesale (PUT semantics);
// a present name renames.
func (s *Server) UpdateMcp(w http.ResponseWriter, r *http.Request, name apigen.McpName) {
	var req apigen.McpUpdate
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	newName := ""
	if req.Name != nil {
		newName = string(*req.Name)
	}
	input, err := mcpInput(req.Stdio, req.Http, req.Enabled)
	if err != nil {
		writeRegistryError(w, err, registry.KindMCP)
		return
	}
	updated, err := s.reg.UpdateMCP(string(name), newName, input)
	if err != nil {
		writeRegistryError(w, err, registry.KindMCP)
		return
	}
	writeJSON(w, http.StatusOK, mcpView(firstNonEmpty(newName, string(name)), updated))
}

// DeleteMcp removes a server's manifest and unregisters it.
func (s *Server) DeleteMcp(w http.ResponseWriter, r *http.Request, name apigen.McpName) {
	if err := s.reg.DeleteMCP(string(name)); err != nil {
		writeRegistryError(w, err, registry.KindMCP)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// TestMcp probes every declared transport (initialize → tools/list →
// shutdown, one shared deadline) and returns one typed result per
// transport. Probe failures are 200 bodies — a broken configuration is
// expected data, not a server error.
func (s *Server) TestMcp(w http.ResponseWriter, r *http.Request, name apigen.McpName) {
	spec, ok := s.reg.Snapshot().MCPs[string(name)]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no MCP server named "+string(name))
		return
	}
	res := s.prober.Test(r.Context(), spec)
	// Successful probes feed the semantic resource index (the only
	// place probed tool lists become durable — phase-5 gate finding).
	if s.indexMCPTools != nil {
		for _, tr := range res.Results {
			if tr.Status == mcpclient.StatusOK && !tr.Truncated {
				tools := make([]mcpclient.Tool, len(tr.Tools))
				copy(tools, tr.Tools)
				s.indexMCPTools(string(name), tools)
			}
		}
	}
	out := make([]apigen.McpTransportTest, 0, len(res.Results))
	for _, tr := range res.Results {
		item := apigen.McpTransportTest{
			Transport: apigen.McpTransportTestTransport(tr.Transport),
			Status:    apigen.McpTransportTestStatusFailed,
		}
		if tr.Status == mcpclient.StatusOK {
			item.Status = apigen.McpTransportTestStatusOk
		}
		if tr.Failure != nil {
			item.Failure = &struct {
				Message string                               `json:"message"`
				Reason  apigen.McpTransportTestFailureReason `json:"reason"`
			}{Message: tr.Failure.Message, Reason: apigen.McpTransportTestFailureReason(tr.Failure.Reason)}
		}
		if len(tr.Tools) > 0 {
			tools := make([]apigen.McpTool, 0, len(tr.Tools))
			for _, tool := range tr.Tools {
				t := tool
				tools = append(tools, apigen.McpTool{Name: t.Name, Description: &t.Description})
			}
			item.Tools = &tools
		}
		if tr.Truncated {
			truncated := true
			item.Truncated = &truncated
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, apigen.McpTestResult{Results: out})
}

// --- Skill handlers ----------------------------------------------------------

// ListSkills returns every registered skill plus per-file load errors.
func (s *Server) ListSkills(w http.ResponseWriter, r *http.Request) {
	snap := s.reg.Snapshot()
	names := make([]string, 0, len(snap.Skills))
	for name := range snap.Skills {
		names = append(names, name)
	}
	sort.Strings(names)
	skills := make([]apigen.Skill, 0, len(names))
	for _, name := range names {
		skills = append(skills, skillView(name, snap.Skills[name]))
	}
	writeJSON(w, http.StatusOK, apigen.SkillList{Skills: skills, LoadErrors: loadErrorsView(s.reg, registry.KindSkill)})
}

// CreateSkill writes a managed skill directory and returns it as loaded.
func (s *Server) CreateSkill(w http.ResponseWriter, r *http.Request) {
	var req apigen.SkillCreate
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	body := ""
	if req.Body != nil {
		body = *req.Body
	}
	created, err := s.reg.CreateSkill(req.Name, req.Description, body)
	if err != nil {
		writeRegistryError(w, err, registry.KindSkill)
		return
	}
	writeJSON(w, http.StatusCreated, skillView(created.Name, created))
}

// GetSkill returns one skill's frontmatter and full body.
func (s *Server) GetSkill(w http.ResponseWriter, r *http.Request, name apigen.SkillName) {
	spec, body, err := s.reg.SkillDetail(string(name))
	if err != nil {
		writeRegistryError(w, err, registry.KindSkill)
		return
	}
	writeJSON(w, http.StatusOK, apigen.SkillDetail{
		Name: spec.Name, Description: spec.Description, Directory: spec.Path, Body: body,
		Origin: apigen.SkillDetailOrigin(spec.Origin), Source: strPtrOrNil(spec.Source),
	})
}

// UpdateSkill replaces a skill's SKILL.md wholesale; a present name
// renames. Absent body keeps the existing body.
func (s *Server) UpdateSkill(w http.ResponseWriter, r *http.Request, name apigen.SkillName) {
	var req apigen.SkillUpdate
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}
	newName := ""
	if req.Name != nil {
		newName = *req.Name
	}
	updated, err := s.reg.UpdateSkill(string(name), newName, req.Description, req.Body)
	if err != nil {
		writeRegistryError(w, err, registry.KindSkill)
		return
	}
	_, body, err := s.reg.SkillDetail(updated.Name)
	if err != nil {
		writeRegistryError(w, err, registry.KindSkill)
		return
	}
	writeJSON(w, http.StatusOK, apigen.SkillDetail{
		Name: updated.Name, Description: updated.Description, Directory: updated.Path, Body: body,
		Origin: apigen.SkillDetailOrigin(updated.Origin), Source: strPtrOrNil(updated.Source),
	})
}

// DeleteSkill removes a skill's whole directory (bundle included).
func (s *Server) DeleteSkill(w http.ResponseWriter, r *http.Request, name apigen.SkillName) {
	if err := s.reg.DeleteSkill(string(name)); err != nil {
		writeRegistryError(w, err, registry.KindSkill)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- mapping helpers ----------------------------------------------------------

// mcpInput converts the contract's transport objects into the registry
// manifest. At-least-one is enforced by the registry's validation; a
// present-but-commandless stdio object is rejected here so it is never
// silently dropped from the manifest.
func mcpInput(stdio *apigen.McpStdio, http_ *apigen.McpHttp, enabled *bool) (registry.MCPSpec, error) {
	var spec registry.MCPSpec
	if stdio != nil && strings.TrimSpace(stdio.Command) == "" {
		return spec, mutationErrInvalidTransport("a stdio transport requires a command")
	}
	if stdio != nil {
		spec.Command = stdio.Command
		if stdio.Args != nil {
			spec.Args = *stdio.Args
		}
		if stdio.Env != nil {
			spec.Env = *stdio.Env
		}
	}
	if http_ != nil {
		spec.URL = http_.Url
	}
	// Absent means enabled (full replace); the probe stays available on
	// disabled servers either way.
	spec.Disabled = enabled != nil && !*enabled
	return spec, nil
}

// mutationErrInvalidTransport is a local 400 carrier for transport-shape
// errors caught before the registry sees the payload.
func mutationErrInvalidTransport(msg string) error {
	return &registry.MutationError{Code: registry.ErrInvalidManifest, Message: msg}
}

// mcpView renders one registered server (stdio transport first).
func mcpView(name string, spec registry.MCPSpec) apigen.Mcp {
	view := apigen.Mcp{Name: name, File: spec.Path, Enabled: !spec.Disabled}
	if spec.Command != "" {
		command := spec.Command
		item := mcpTransportElem{Kind: apigen.McpTransportsKindStdio, Command: &command}
		if len(spec.Args) > 0 {
			args := spec.Args
			item.Args = &args
		}
		if len(spec.Env) > 0 {
			env := spec.Env
			item.Env = &env
		}
		view.Transports = append(view.Transports, item)
	}
	if spec.URL != "" {
		url := spec.URL
		view.Transports = append(view.Transports, mcpTransportElem{Kind: apigen.McpTransportsKindHttp, Url: &url})
	}
	return view
}

// mcpTransportElem aliases the generated anonymous transports element
// (the contract's discriminated transport object) so construction reads.
type mcpTransportElem = struct {
	Args    *[]string                `json:"args,omitempty"`
	Command *string                  `json:"command,omitempty"`
	Env     *map[string]string       `json:"env,omitempty"`
	Kind    apigen.McpTransportsKind `json:"kind"`
	Url     *string                  `json:"url,omitempty"`
}

func skillView(name string, spec registry.SkillSpec) apigen.Skill {
	view := apigen.Skill{Name: name, Description: spec.Description, Directory: spec.Path, Origin: apigen.SkillOrigin(spec.Origin)}
	if spec.Origin == registry.OriginMarketplace && spec.Source != "" {
		view.Source = &spec.Source
		if spec.Ref != "" {
			view.Ref = &spec.Ref
		}
	}
	return view
}

// loadErrorsView filters the registry's load errors to one kind, bounded.
func loadErrorsView(reg *registry.Registry, kind registry.Kind) *[]apigen.RegistryLoadError {
	errs := reg.Errors()
	out := make([]apigen.RegistryLoadError, 0, len(errs))
	for _, e := range errs {
		if e.Kind != kind || len(out) >= maxLoadErrors {
			continue
		}
		kindStr := apigen.RegistryLoadErrorKindMcp
		if kind == registry.KindSkill {
			kindStr = apigen.RegistryLoadErrorKindSkill
		}
		out = append(out, apigen.RegistryLoadError{Path: e.Path, Kind: kindStr, Message: e.Err.Error()})
	}
	if len(out) == 0 {
		return nil
	}
	return &out
}

// writeRegistryError maps typed registry mutation failures to the
// contract's status matrix (identical across both kinds).
func writeRegistryError(w http.ResponseWriter, err error, kind registry.Kind) {
	var me *registry.MutationError
	ok := errors.As(err, &me)
	if !ok {
		writeError(w, http.StatusInternalServerError, "io_error", err.Error())
		return
	}
	switch me.Code {
	case registry.ErrNotFound:
		writeError(w, http.StatusNotFound, "not_found", me.Message)
	case registry.ErrDuplicateName:
		writeError(w, http.StatusConflict, "duplicate_name", me.Message)
	case registry.ErrInvalidName:
		writeError(w, http.StatusBadRequest, "invalid_name", me.Message)
	case registry.ErrInvalidManifest:
		writeError(w, http.StatusBadRequest, "invalid_manifest", me.Message)
	case registry.ErrCapacity:
		if kind == registry.KindSkill {
			writeError(w, http.StatusConflict, "skill_limit", me.Message)
		} else {
			writeError(w, http.StatusConflict, "mcp_limit", me.Message)
		}
	case registry.ErrIO:
		writeError(w, http.StatusInternalServerError, "io_error", me.Message)
	default: // ErrUnconfigured and anything unexpected
		writeError(w, http.StatusInternalServerError, "unconfigured", me.Message)
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ListAgents lists the configured agent roster (read-only; agents are
// file-defined under ~/.zbot/agents).
func (s *Server) ListAgents(w http.ResponseWriter, r *http.Request) {
	snap := s.reg.Snapshot()
	names := make([]string, 0, len(snap.Agents))
	for name := range snap.Agents {
		names = append(names, name)
	}
	sort.Strings(names)
	agents := make([]apigen.Agent, 0, len(names))
	for _, name := range names {
		spec := snap.Agents[name]
		agent := apigen.Agent{
			Name: spec.Name, DisplayName: nilStrPtr(spec.DisplayName),
			Description: nilStrPtr(spec.Description), Model: nilStrPtr(spec.Model),
			MaxDepth: &spec.MaxDepth, Source: nilStrPtr(spec.Path),
		}
		if spec.Tools != nil {
			tools := spec.Tools
			agent.Tools = &tools
		}
		agents = append(agents, agent)
	}
	writeJSON(w, http.StatusOK, apigen.AgentList{Agents: agents})
}
