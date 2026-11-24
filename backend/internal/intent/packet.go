package intent

import (
	"sort"
	"strings"

	"zbot/backend/internal/domain"
)

// Packet caps (agentzero's catalog.rs numbers): the packet is bounded
// per kind so the analysis prompt stays small regardless of roster
// size. Wards get the widest cap — ward selection is the packet's
// highest-stakes decision.
const (
	PacketCapSkills = 20
	PacketCapAgents = 20
	PacketCapMCPs   = 20
	PacketCapWards  = 64
	// PacketDescLimit bounds one entry's description contribution.
	PacketDescLimit = 512
	// PacketRequestLimit bounds the embedded request echo (advisory
	// data — control characters stripped, never executed).
	PacketRequestLimit = 32768
)

// CatalogEntry is one host-authoritative roster entry.
type CatalogEntry struct {
	ID          string
	Description string
}

// Packet is the analyzer's context: the full host roster per kind,
// ranked and capped. HOST metadata is the authority; memory recall
// only ORDERS entries (agentzero's rule — the index never authorizes
// a recommendation the roster does not carry).
type Packet struct {
	Skills []CatalogEntry
	Agents []CatalogEntry
	MCPs   []CatalogEntry
	Wards  []CatalogEntry
}

// advisory renders one entry's description line body (bounded,
// control-character-free — untrusted data discipline).
func advisory(s string, limit int) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			r = ' '
		}
		b.WriteRune(r)
		if b.Len() >= limit {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func (e CatalogEntry) line() string {
	d := advisory(e.Description, PacketDescLimit)
	if d == "" {
		return "- " + e.ID
	}
	return "- " + e.ID + " — " + d
}

func renderEntries(entries []CatalogEntry) string {
	if len(entries) == 0 {
		return "(none)"
	}
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = e.line()
	}
	return strings.Join(lines, "\n")
}

// Render returns the packet's four slot bodies (the user-owned intent
// prompt's {{.Skills}}/{{.Agents}}/{{.MCPs}}/{{.Wards}} slots).
func (p Packet) Render() (skills, agents, mcps, wards string) {
	return renderEntries(p.Skills), renderEntries(p.Agents), renderEntries(p.MCPs), renderEntries(p.Wards)
}

// rankEntries orders one kind: recall-ranked head (rank < 0 = not
// recalled; stable by id), capped.
func rankEntries(entries []CatalogEntry, rank func(id string) int, cap int) []CatalogEntry {
	if len(entries) <= cap && rank == nil {
		return entries
	}
	ordered := make([]CatalogEntry, len(entries))
	copy(ordered, entries)
	if rank != nil {
		sort.SliceStable(ordered, func(i, j int) bool {
			ri, rj := rank(ordered[i].ID), rank(ordered[j].ID)
			if ri != rj {
				return ri < rj
			}
			return ordered[i].ID < ordered[j].ID
		})
	}
	if len(ordered) > cap {
		ordered = ordered[:cap]
	}
	return ordered
}

// Ranked builds the packet from a full roster plus a recall ranking
// (the position of an id in the recall output; -1 when unrecalled).
// rank may be nil (alphabetical, capped).
func (roster Packet) Ranked(rank func(kind, id string) int) Packet {
	mk := func(kind string) func(string) int {
		if rank == nil {
			return nil
		}
		return func(id string) int { return rank(kind, id) }
	}
	return Packet{
		Skills: rankEntries(roster.Skills, mk("skill"), PacketCapSkills),
		Agents: rankEntries(roster.Agents, mk("agent"), PacketCapAgents),
		MCPs:   rankEntries(roster.MCPs, mk("mcp"), PacketCapMCPs),
		Wards:  rankEntries(roster.Wards, mk("ward"), PacketCapWards),
	}
}

func entrySet(entries []CatalogEntry) map[string]bool {
	// An EMPTY set is still authoritative (nothing exists → everything
	// unknown); nil is reserved for unset (never returned).
	set := make(map[string]bool, len(entries))
	for _, e := range entries {
		set[e.ID] = true
	}
	return set
}

// Sets returns per-kind membership (validation's authority).
func (p Packet) Sets() (skills, agents, mcps, wards map[string]bool) {
	return entrySet(p.Skills), entrySet(p.Agents), entrySet(p.MCPs), entrySet(p.Wards)
}

// Validate checks a parsed decision against the packet's ID sets:
// every unknown recommendation is one correction code. Empty result =
// valid. (Root is always a valid agent — the delegation default.)
func (p Packet) Validate(a domain.IntentAnalysis) []string {
	sk, ag, mc, wd := p.Sets()
	var codes []string
	for _, id := range a.RecommendedSkills {
		if !sk[id] {
			codes = append(codes, "unknown_skill:"+id)
		}
	}
	for _, id := range a.RecommendedAgents {
		if id == "root" {
			continue
		}
		if !ag[id] {
			codes = append(codes, "unknown_agent:"+id)
		}
	}
	for _, id := range a.RecommendedMCPs {
		if !mc[id] {
			codes = append(codes, "unknown_mcp:"+id)
		}
	}
	if a.WardRecommendation.Action == domain.WardUseExisting && a.WardRecommendation.WardName != "" {
		if !wd[a.WardRecommendation.WardName] {
			codes = append(codes, "unknown_ward:"+a.WardRecommendation.WardName)
		}
	}
	return codes
}

// correctionAdvice maps a failure code to the retry instruction
// (agentzero's correction strings: name the failure, state the fix).
func correctionAdvice(code string) string {
	switch {
	case strings.HasPrefix(code, "unknown_"):
		return "Use only exact catalog IDs from the packet's lists (or empty arrays when nothing fits). Do not recommend resources the packet does not list."
	case code == "invalid_json":
		return "Return one valid JSON object — no markdown fences around other text, no trailing commas, no prose outside the object."
	default:
		return "Follow the output contract exactly and return the corrected decision JSON."
	}
}

// EscapeRequest renders the user request as embedded advisory data:
// control characters stripped, length bounded, no fence markers.
func EscapeRequest(goal string) string {
	return advisory(strings.ReplaceAll(strings.ReplaceAll(goal, "```", "′′′"), "<intent-data>", ""), PacketRequestLimit)
}

// RequestBlock renders the escaped request block for the user message.
func RequestBlock(goal string) string {
	return "<user-request>\n" + EscapeRequest(goal) + "\n</user-request>\n\nReturn the decision JSON now."
}

// CorrectionBlock renders the retry instruction after a failed
// attempt.
func CorrectionBlock(codes []string) string {
	var b strings.Builder
	b.WriteString("Your previous decision failed host validation: ")
	for i, c := range codes {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(c)
	}
	b.WriteString(". ")
	if len(codes) > 0 {
		b.WriteString(correctionAdvice(codes[0]))
	} else {
		b.WriteString(correctionAdvice(""))
	}
	b.WriteString(" Return the corrected decision JSON now.")
	return b.String()
}
