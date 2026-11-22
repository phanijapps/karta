package server

import (
	"errors"
	"net/http"
	"strings"

	"zbot/backend/internal/mcpimport"
	"zbot/backend/internal/registry"
	apigen "zbot/backend/internal/server/apigen"
)

// ImportMcps implements POST /api/mcps/import: raw-text standard-JSON
// import with per-entry outcomes in paste order, plus the replace mode
// (edit-as-JSON). Document-level rejections are typed 400s that create
// nothing; entry failures are outcome rows, never request failures; a
// write failure (io_error) on one entry never aborts the rest.
func (s *Server) ImportMcps(w http.ResponseWriter, r *http.Request) {
	var req apigen.McpImportRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return // decodeJSON already wrote the 400.
	}
	singleName := ""
	if req.Name != nil {
		singleName = *req.Name
	}
	// Replace mode is selected by PRESENCE: an explicitly empty selector
	// is a client bug, never a silent fallthrough to create mode.
	replaceName := ""
	if req.ReplaceName != nil {
		if strings.TrimSpace(*req.ReplaceName) == "" {
			writeError(w, http.StatusBadRequest, "invalid_import", "replaceName must not be empty")
			return
		}
		replaceName = *req.ReplaceName
	}
	// Wiring preflight (read-only): a nil or unconfigured registry is a
	// document-level 500 before any entry — never per-entry io errors.
	if s.reg == nil || !s.reg.Ready(registry.KindMCP) {
		writeError(w, http.StatusInternalServerError, "unconfigured", "the registry is not configured for MCP management (LoadAll must run first)")
		return
	}
	kvs, err := mcpimport.ParseDocument(req.Document)
	if err != nil {
		writeImportDocError(w, err)
		return
	}
	// Replace mode classifies with the target as the single-entry name
	// (a single-object paste carries no key of its own).
	classifyName := singleName
	if replaceName != "" && classifyName == "" {
		classifyName = replaceName
	}
	entries, wasSingle, err := mcpimport.Classify(kvs, req.Document, classifyName)
	if err != nil {
		writeImportDocError(w, err)
		return
	}
	_ = wasSingle

	// Replace mode (edit-as-JSON): exactly one entry, mapped with full
	// fidelity, replacing the target's transports wholesale. Any skip,
	// failure, or a second entry rejects with the target untouched.
	if replaceName != "" {
		target := replaceName
		if len(entries) != 1 {
			writeError(w, http.StatusBadRequest, "invalid_import", "an edit-as-JSON document must hold exactly one server")
			return
		}
		spec, act := mcpimport.MapEntry(entries[0])
		if act.Skip != "" {
			writeError(w, http.StatusBadRequest, "invalid_import", "the entry would be skipped ("+act.Skip+"); nothing was replaced")
			return
		}
		if act.Fail != "" {
			writeError(w, http.StatusBadRequest, "invalid_import", act.FailMsg)
			return
		}
		if _, err := s.reg.UpdateMCP(target, "", spec); err != nil {
			writeRegistryError(w, err, registry.KindMCP)
			return
		}
		writeJSON(w, http.StatusOK, apigen.McpImportResult{Results: []apigen.McpImportOutcome{{
			SourceKey: entries[0].Key, Name: &target, Status: mcpimport.StatusCreated, Warnings: warningsPtr(act.Warnings),
		}}})
		return
	}
	// Import mode: per-entry independence. Duplicates (via the registry)
	// outrank capacity; io failures keep the report honest.
	outcomes := make([]apigen.McpImportOutcome, 0, len(entries))
	for _, kv := range entries {
		outcomes = append(outcomes, s.importOne(kv))
	}
	writeJSON(w, http.StatusOK, apigen.McpImportResult{Results: outcomes})
}

// importOne maps and creates one entry, mapping registry results to
// outcome rows. An io_error or unconfigured registry never aborts the
// loop's report: io_error is a per-entry failure, and Unconfigured (a
// daemon wiring bug) fails that entry rather than the request.
func (s *Server) importOne(kv mcpimport.KV) apigen.McpImportOutcome {
	out := apigen.McpImportOutcome{SourceKey: truncate(kv.Key, 256), Status: mcpimport.StatusCreated, Warnings: nil}
	spec, act := mcpimport.MapEntry(kv)
	out.Warnings = warningsPtr(act.Warnings)
	switch {
	case act.Skip != "":
		out.Status, out.Reason = mcpimport.StatusSkipped, ptrReason(act.Skip)
		return out
	case act.Fail != "":
		msg := act.FailMsg
		out.Status, out.Reason, out.Message = mcpimport.StatusFailed, ptrReason(act.Fail), &msg
		return out
	}
	name, _, err := s.reg.CreateMCPFromKey(kv.Key, spec)
	if err == nil {
		out.Name = &name
		return out
	}
	var me *registry.MutationError
	if !errors.As(err, &me) {
		msg := err.Error()
		out.Status, out.Reason, out.Message = mcpimport.StatusFailed, ptrReason(mcpimport.ReasonIO), &msg
		return out
	}
	msg := me.Message
	switch me.Code {
	case registry.ErrDuplicateName:
		out.Status, out.Reason = mcpimport.StatusSkipped, ptrReason(mcpimport.ReasonDuplicate)
	case registry.ErrInvalidName:
		out.Status, out.Reason, out.Message = mcpimport.StatusFailed, ptrReason(mcpimport.ReasonName), &msg
	case registry.ErrCapacity:
		out.Status, out.Reason, out.Message = mcpimport.StatusFailed, ptrReason(mcpimport.ReasonCapacity), &msg
	case registry.ErrInvalidManifest:
		out.Status, out.Reason, out.Message = mcpimport.StatusFailed, ptrReason(mcpimport.ReasonField), &msg
	default: // ErrIO, ErrUnconfigured: operational, reported per entry
		out.Status, out.Reason, out.Message = mcpimport.StatusFailed, ptrReason(mcpimport.ReasonIO), &msg
	}
	return out
}

func writeImportDocError(w http.ResponseWriter, err error) {
	var ide *mcpimport.DocError
	if errors.As(err, &ide) {
		writeError(w, http.StatusBadRequest, "invalid_import", err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_import", err.Error())
}

func ptrReason(reason string) *apigen.McpImportOutcomeReason {
	r := apigen.McpImportOutcomeReason(reason)
	return &r
}

func warningsPtr(warnings []string) *[]string {
	if len(warnings) == 0 {
		return nil
	}
	capped := warnings
	if len(capped) > 32 {
		capped = capped[:32]
	}
	return &capped
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
