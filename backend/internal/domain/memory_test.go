package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Contract tests for the chitta transfer objects (spec capability
// chitta-contracts). Field names and omission behavior are normative.

func TestMemoryRecordJSONShape(t *testing.T) {
	now := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	rec := MemoryRecord{
		ID:         "mem_01J8ZG3W2K",
		Kind:       MemoryKindPreference,
		Subject:    "user",
		Content:    "Prefers concise answers",
		Confidence: 0.9,
		Status:     MemoryStatusActive,
		Provenance: MemoryProvenance{Source: SourceAgent, SessionID: "s1", ExecutionID: "e1"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	// Required keys present.
	for _, key := range []string{"id", "kind", "subject", "content", "confidence", "status", "provenance", "created_at", "updated_at"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing required key %q in %s", key, data)
		}
	}
	// Optional-absent keys omitted, never null.
	for _, key := range []string{"tags", "supersedes", "superseded_by", "entities", "last_confirmed_at"} {
		if _, ok := m[key]; ok {
			t.Errorf("optional-absent key %q must be omitted, got %s", key, data)
		}
	}
	// Snake_case provenance.
	p := m["provenance"].(map[string]any)
	if p["source"] != "agent" || p["session_id"] != "s1" {
		t.Errorf("provenance wire shape wrong: %v", p)
	}
}

func TestDecodeChittaWriteRejectsUnknownFields(t *testing.T) {
	err := DecodeChittaWrite([]byte(`{"kind":"fact","content":"x","id":"mem_hax"}`), &Observation{})
	if err == nil {
		t.Fatal("unknown field id must be rejected on write")
	}
	var ce *ChittaError
	ok := errors.As(err, &ce)
	if !ok || ce.Code != ErrInvalidInput {
		t.Fatalf("want invalid_input, got %v", err)
	}
	if !strings.Contains(ce.Message, "id") && !strings.Contains(ce.Error(), "id") {
		t.Errorf("error should name the field: %v", err)
	}
}

func TestDecodeReadToleratesUnknownFields(t *testing.T) {
	// Reads are lenient: a newer producer's extra field is ignored.
	var rec MemoryRecord
	if err := json.Unmarshal([]byte(`{"id":"mem_1","kind":"fact","subject":"user","content":"c","confidence":0.5,"status":"active","created_at":"2026-11-08T10:00:00Z","updated_at":"2026-11-08T10:00:00Z","future_field":true}`), &rec); err != nil {
		t.Fatalf("read must tolerate unknown fields: %v", err)
	}
	if rec.ID != "mem_1" {
		t.Errorf("decode lost fields: %+v", rec)
	}
}

func TestEnumClosedness(t *testing.T) {
	// Plain decode succeeds for any string; closedness is enforced by the
	// Valid() gates the write boundary calls.
	var k MemoryKind
	if err := json.Unmarshal([]byte(`"fact"`), &k); err != nil || k != MemoryKindFact {
		t.Errorf("fact: err %v", err)
	}
	if err := json.Unmarshal([]byte(`"vibe"`), &k); err != nil || k.Valid() {
		t.Error("kind enum must be closed (Valid gate)")
	}
	var s MemoryStatus
	if err := json.Unmarshal([]byte(`"deleted"`), &s); err != nil || s.Valid() {
		t.Error("status enum must be closed (Valid gate)")
	}
	if EntityType("rocket").Valid() {
		t.Error("entity type must be closed")
	}
	if !EntityConcept.Valid() || !SourceDistiller.Valid() {
		t.Error("valid members must pass")
	}
}

func TestConvergeConfidence(t *testing.T) {
	// c' = c + (1-c)*alpha, monotone, approaches but never reaches 1.
	c := 0.5
	for i := 0; i < 1000; i++ {
		c = ConvergeConfidence(c, DefaultConfidenceAlpha)
		if c >= 1.0 {
			t.Fatalf("confidence saturated at iteration %d", i)
		}
	}
	if c < 0.999 {
		t.Errorf("confidence should converge near 1, got %f", c)
	}
	if got := ConvergeConfidence(0.2, 0.3); got < 0.2 || got > 0.5 {
		t.Errorf("first step out of range: %f", got)
	}
	// Clamping.
	if got := ConvergeConfidence(7, 0.3); got != 1 {
		t.Errorf("clamp high: %f", got)
	}
	if got := Clamp01(-3); got != 0 {
		t.Errorf("clamp low: %f", got)
	}
}

func TestContributionIdentity(t *testing.T) {
	seq := int64(42)
	a := ContributionIdentity(SourceAgent, "s1", "e1", &seq, "user", MemoryKindPreference, "prefers pnpm")
	b := ContributionIdentity(SourceAgent, "s1", "e1", &seq, "user", MemoryKindPreference, "prefers pnpm")
	if a != b {
		t.Error("identical evidence must hash identically (replay idempotency)")
	}
	c := ContributionIdentity(SourceAgent, "s2", "e1", &seq, "user", MemoryKindPreference, "prefers pnpm")
	if a == c {
		t.Error("distinct session is distinct evidence")
	}
	d := ContributionIdentity(SourceDistiller, "s1", "e1", &seq, "user", MemoryKindPreference, "prefers pnpm")
	if a == d {
		t.Error("distinct source is distinct evidence")
	}
	e := ContributionIdentity(SourceAgent, "s1", "e1", &seq, "project-x", MemoryKindPreference, "prefers pnpm")
	if a == e {
		t.Error("same content under a different subject is distinct evidence")
	}
	f := ContributionIdentity(SourceAgent, "s1", "e1", &seq, "user", MemoryKindFact, "prefers pnpm")
	if a == f {
		t.Error("same content under a different kind is distinct evidence")
	}
}

func TestHostContextStampsProvenance(t *testing.T) {
	seq := int64(7)
	host := ChittaHostContext{Source: SourceDistiller, SessionID: "s1", ExecutionID: "e1", AgentID: "root", MessageSeq: &seq}
	p := host.Provenance()
	if p.Source != SourceDistiller || p.SessionID != "s1" || *p.MessageSeq != 7 {
		t.Fatalf("provenance stamp wrong: %+v", p)
	}
}

func TestChittaErrorShape(t *testing.T) {
	e := NewChittaError(ErrNotFound, "record %s not found", "mem_x")
	data, _ := json.Marshal(e)
	if !strings.Contains(string(data), `"code":"not_found"`) || !strings.Contains(string(data), "mem_x") {
		t.Errorf("error envelope wrong: %s", data)
	}
}
