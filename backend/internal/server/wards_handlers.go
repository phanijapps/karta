package server

import (
	"context"
	"errors"
	"net/http"

	apigen "zbot/backend/internal/server/apigen"
	"zbot/backend/internal/wards"
)

// WardIndex is the seam the ward handlers consume: the concrete
// *wards.Vault in production (zbotd resolves, bootstraps, and opens the
// vault at startup); a temp vault over a fixture tree in handler tests.
type WardIndex interface {
	Root() string
	Index(ctx context.Context) ([]wards.WardInfo, error)
	Detail(ctx context.Context, name string) (wards.WardDetail, error)
}

// ListWards answers GET /api/wards: the roster plus the resolved vault
// root (the UI's empty state points at it).
func (s *Server) ListWards(w http.ResponseWriter, r *http.Request) {
	if s.wards == nil {
		writeError(w, http.StatusServiceUnavailable, "wards_unavailable", "ward vault is not wired")
		return
	}
	infos, err := s.wards.Index(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	roster := make([]apigen.WardSummary, len(infos))
	for i, info := range infos {
		roster[i] = wardSummaryView(info)
	}
	writeJSON(w, http.StatusOK, apigen.WardList{VaultRoot: s.wards.Root(), Wards: roster})
}

// GetWard answers GET /api/wards/{name}: the parsed layout tree (the
// node ids slice 3's Active Ward Template injects) plus full findings.
func (s *Server) GetWard(w http.ResponseWriter, r *http.Request, name string) {
	if s.wards == nil {
		writeError(w, http.StatusServiceUnavailable, "wards_unavailable", "ward vault is not wired")
		return
	}
	detail, err := s.wards.Detail(r.Context(), name)
	if err != nil {
		if errors.Is(err, wards.ErrUnknownWard) {
			writeError(w, http.StatusNotFound, "not_found", "no ward named "+name)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	view := apigen.Ward{
		Name:         detail.Info.Name,
		Archetype:    detail.Info.Archetype,
		LintStatus:   apigen.WardLintStatus(detail.Info.LintStatus()),
		FindingCount: detail.Info.FindingCount,
		Findings:     make([]apigen.WardFinding, len(detail.Findings)),
	}
	for i, f := range detail.Findings {
		view.Findings[i] = wardFindingView(f)
	}
	if detail.Info.CanonicalTitle != "" {
		title := detail.Info.CanonicalTitle
		view.CanonicalTitle = &title
	}
	if detail.Layout != nil {
		layout := wardNodeView(*detail.Layout)
		view.Layout = &layout
	}
	writeJSON(w, http.StatusOK, view)
}

// wardSummaryView maps one indexed ward to the contract shape.
func wardSummaryView(info wards.WardInfo) apigen.WardSummary {
	view := apigen.WardSummary{
		Name:         info.Name,
		Archetype:    info.Archetype,
		LintStatus:   apigen.WardSummaryLintStatus(info.LintStatus()),
		FindingCount: info.FindingCount,
	}
	if info.CanonicalTitle != "" {
		title := info.CanonicalTitle
		view.CanonicalTitle = &title
	}
	return view
}

// wardFindingView maps one lint finding; nodes without a declared id are
// identified by their match pattern (contract: WardFinding.node_id).
func wardFindingView(f wards.Finding) apigen.WardFinding {
	view := apigen.WardFinding{Kind: apigen.WardFindingKind(f.Kind), Message: f.Message}
	if f.NodeID != "" {
		id := f.NodeID
		view.NodeId = &id
	}
	if f.Path != "" {
		path := f.Path
		view.Path = &path
	}
	return view
}

// wardNodeView maps one layout-tree node (recursive; omitted optionals
// stay absent so the tree round-trips the conf's own declarations).
func wardNodeView(n wards.Node) apigen.WardNode {
	view := apigen.WardNode{Kind: apigen.WardNodeKind(n.Kind)}
	if n.ID != "" {
		id := n.ID
		view.Id = &id
	}
	if n.Match != "" {
		match := n.Match
		view.Match = &match
	}
	if n.Required != nil {
		view.Required = n.Required
	}
	if n.Repeat != nil {
		view.Repeat = n.Repeat
	}
	if n.Format != "" {
		format := apigen.WardNodeFormat(n.Format)
		view.Format = &format
	}
	if len(n.Children) > 0 {
		children := make([]apigen.WardNode, len(n.Children))
		for i, c := range n.Children {
			children[i] = wardNodeView(c)
		}
		view.Children = &children
	}
	return view
}
