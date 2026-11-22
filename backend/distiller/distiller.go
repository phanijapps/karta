// Package distiller closes the evolution loop's write path: finished
// sessions from the conversation store are distilled (LLM extraction
// through the provider plane) into chitta through the trusted seam —
// source: distiller, transcripts never persisted, re-runs idempotent.
//
// Boundaries (zone rule extension, backend/AGENTS.md): a plugin-zone
// composition component — imports kernel seams (store reads, eventbus,
// providers, config paths) AND the chitta public surface; nothing
// imports it but cmd/zbotd. chitta never imports the store; the store
// never imports chitta; the loop closes here.
package distiller

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"zbot/backend/internal/domain"
)

// Session is one finished conversation offered to the distiller.
type Session struct {
	ID          string
	ExecutionID string
	Messages    []SessionMessage // newest-last
	// Finished reports whether the session is idle long enough to
	// distill (activity-based; no explicit ended notion).
	Finished bool
}

// SessionMessage is one turn of the transcript.
type SessionMessage struct {
	Role    string // "user" | "assistant" | "tool"
	Content string
}

// LLMCaller performs one completion (provider plane adapter).
type LLMCaller interface {
	Complete(ctx context.Context, provider, model, systemPrompt, userPrompt string) (string, error)
}

// Chitta is the chitta surface the distiller needs (subset of the
// public System — keeps the dependency explicit).
type Chitta interface {
	Remember(ctx context.Context, observations []domain.Observation, host domain.ChittaHostContext) ([]domain.RememberResult, error)
	Connect(ctx context.Context, req domain.ConnectRequest, host domain.ChittaHostContext) (domain.Relationship, error)
	UpsertEntity(ctx context.Context, req domain.EntityUpsert) (domain.Entity, error)
	MarkDistilled(ctx context.Context, sessionID string, attempts int, deadLetter bool) error
	DistillationMarkers(ctx context.Context) (map[string]domain.DistillationMarker, error)
}

// SessionSource lists finished sessions for distillation (kernel store
// adapter in cmd).
type SessionSource interface {
	FinishedSessions(ctx context.Context, limit int) ([]Session, error)
}

// Settings are the live distillation settings (Settings → Advanced).
type Settings struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Prompt   string `json:"prompt,omitempty"`
}

// SettingsReader returns the current settings each pass.
type SettingsReader interface {
	Read(ctx context.Context) (Settings, error)
}

// FileSettings reads the daemon-managed distillation.json each pass;
// prompt resolution: json prompt > distillation-prompt.md > embedded.
type FileSettings struct {
	Dir string // config dir
}

func (f *FileSettings) Read(ctx context.Context) (Settings, error) {
	s := Settings{}
	raw, err := os.ReadFile(filepath.Join(f.Dir, "distillation.json"))
	if err == nil {
		_ = json.Unmarshal(raw, &s)
	}
	if strings.TrimSpace(s.Prompt) == "" {
		if b, err := os.ReadFile(filepath.Join(f.Dir, "distillation-prompt.md")); err == nil {
			s.Prompt = string(b)
		}
	}
	if strings.TrimSpace(s.Prompt) == "" {
		s.Prompt = defaultPrompt
	}
	return s, nil
}

// Distiller is the serial background worker.
type Distiller struct {
	chitta   Chitta
	source   SessionSource
	llm      LLMCaller
	settings SettingsReader
	log      func(format string, args ...any)
	interval time.Duration
	attempts int
}

// New builds the worker (wired in cmd/zbotd; Run starts the loop).
func New(c Chitta, src SessionSource, llm LLMCaller, settings SettingsReader, log func(string, ...any)) *Distiller {
	if log == nil {
		log = func(string, ...any) {}
	}
	return &Distiller{chitta: c, source: src, llm: llm, settings: settings, log: log,
		interval: 5 * time.Minute, attempts: 3}
}

// SetInterval overrides the pass interval (tests).
func (d *Distiller) SetInterval(v time.Duration) { d.interval = v }

// Run loops until ctx is cancelled.
func (d *Distiller) Run(ctx context.Context) {
	t := time.NewTicker(d.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.Pass(ctx)
		}
	}
}

// idleThreshold is how long a session must be quiet before it counts
// as "finished" (no explicit ended notion exists on the sessions
// table; activity-based filtering is the honest v1).
var idleThreshold = 10 * time.Minute

// IdleThreshold reports the activity-based "finished" cutoff.
func IdleThreshold() time.Duration { return idleThreshold }

// Pass runs one selection→extract→write→mark cycle.
func (d *Distiller) Pass(ctx context.Context) {
	settings, err := d.settings.Read(ctx)
	if err != nil {
		d.log("distiller: settings: %v", err)
		return
	}
	if settings.Provider == "" || settings.Model == "" {
		d.log("distiller: no provider/model configured; skipping pass")
		return
	}
	sessions, err := d.source.FinishedSessions(ctx, 50)
	if err != nil {
		d.log("distiller: sessions: %v", err)
		return
	}
	markers, err := d.chitta.DistillationMarkers(ctx)
	if err != nil {
		d.log("distiller: markers: %v", err)
		return
	}
	distilled, failed, dead := 0, 0, 0
	for _, sess := range sessions {
		if !sess.Finished {
			continue // still active (not idle long enough)
		}
		if m, ok := markers[sess.ID]; ok {
			if m.DeadLetter {
				dead++
				continue // dead-lettered
			}
			if m.Attempts == 0 {
				continue // successfully distilled
			}
			// attempts > 0 but not dead: retry (distillOne carries the count)
		}
		if err := d.distillOne(ctx, sess, settings, markers); err != nil {
			failed++
			d.log("distiller: session %s: %v", sess.ID, err)
			continue
		}
		distilled++
	}
	d.log("distiller: pass complete: %d distilled, %d failed, %d dead-lettered", distilled, failed, dead)
}

func (d *Distiller) distillOne(ctx context.Context, sess Session, settings Settings, markers map[string]domain.DistillationMarker) error {
	attempts := 0
	if m, ok := markers[sess.ID]; ok {
		attempts = m.Attempts
	}
	extraction, err := d.extract(ctx, sess, settings)
	if err != nil {
		attempts++
		if attempts >= d.attempts {
			if err := d.chitta.MarkDistilled(ctx, sess.ID, attempts, true); err != nil {
				return fmt.Errorf("dead-letter marker: %w", err)
			}
			return fmt.Errorf("dead-lettered after %d attempts: %w", attempts, err)
		}
		_ = d.chitta.MarkDistilled(ctx, sess.ID, attempts, false)
		return fmt.Errorf("attempt %d: %w", attempts, err)
	}
	host := domain.ChittaHostContext{
		Source:      domain.SourceDistiller,
		SessionID:   sess.ID,
		ExecutionID: sess.ExecutionID,
		AgentID:     "distiller",
	}
	// Observations first (the atomic batch); entities + relationships
	// follow — a failure there leaves the (already converging) evidence
	// but the session unmarked for retry (deliberate: entity upserts are
	// idempotent and re-run cleanly).
	if len(extraction.Observations) > 0 {
		if _, err := d.chitta.Remember(ctx, extraction.Observations, host); err != nil {
			return fmt.Errorf("observations: %w", err)
		}
	}
	for _, e := range extraction.Entities {
		up := domain.EntityUpsert{Name: e.Name, Aliases: e.Aliases}
		if t, ok := mapEntityType(e.Type); ok {
			up.Type = t
		}
		if _, err := d.chitta.UpsertEntity(ctx, up); err != nil {
			return fmt.Errorf("entity %q: %w", e.Name, err)
		}
	}
	for _, r := range extraction.Relationships {
		if _, err := d.chitta.Connect(ctx, domain.ConnectRequest{From: r.From, To: r.To, Type: r.Type}, host); err != nil {
			return fmt.Errorf("relationship %s: %w", r.Type, err)
		}
	}
	// Success resets the attempt count (a session that failed once then
	// succeeded must be skipped on later passes, not retried forever).
	return d.chitta.MarkDistilled(ctx, sess.ID, 0, false)
}

// --- extraction ---

type extraction struct {
	Observations []domain.Observation `json:"observations"`
	Entities     []struct {
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		Aliases []string `json:"aliases"`
	} `json:"entities"`
	Relationships []struct {
		From string `json:"from"`
		To   string `json:"to"`
		Type string `json:"type"`
	} `json:"relationships"`
}

func (d *Distiller) extract(ctx context.Context, sess Session, settings Settings) (*extraction, error) {
	transcript := renderTranscript(sess.Messages, 200)
	raw, err := d.llm.Complete(ctx, settings.Provider, settings.Model, settings.Prompt, transcript)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}
	jsonStr := strings.TrimSpace(raw)
	if idx := strings.Index(jsonStr, "{"); idx > 0 {
		jsonStr = jsonStr[idx:]
	}
	if idx := strings.LastIndex(jsonStr, "}"); idx >= 0 {
		jsonStr = jsonStr[:idx+1]
	}
	var out extraction
	if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	// Bounds + validation (caps, closed enums, content trimming).
	if len(out.Observations) > 20 {
		out.Observations = out.Observations[:20]
	}
	valid := out.Observations[:0]
	dropped := 0
	for _, o := range out.Observations {
		if !o.Kind.Valid() || strings.TrimSpace(o.Content) == "" {
			dropped++
			continue
		}
		if len(o.Tags) > 8 {
			o.Tags = o.Tags[:8]
		}
		valid = append(valid, o)
	}
	// Schema-invalid output fails the attempt (spec: "Malformed or
	// unparseable output SHALL fail that session's attempt") — dropping
	// more than half the observations means the extraction is garbage.
	if len(out.Observations) > 0 && dropped*2 > len(out.Observations) {
		return nil, fmt.Errorf("extraction invalid: %d/%d observations failed schema validation", dropped, len(out.Observations))
	}
	out.Observations = valid
	if len(out.Entities) > 20 {
		out.Entities = out.Entities[:20]
	}
	if len(out.Relationships) > 20 {
		out.Relationships = out.Relationships[:20]
	}
	return &out, nil
}

// renderTranscript renders the newest N messages as role: content lines.
func renderTranscript(msgs []SessionMessage, newest int) string {
	if len(msgs) > newest {
		msgs = msgs[len(msgs)-newest:]
	}
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %s\n", strings.ToUpper(m.Role), m.Content)
	}
	if b.Len() == 0 {
		b.WriteString("(empty session)")
	}
	return b.String()
}

// mapEntityType collapses the prompt's vocabulary onto chitta's closed
// enum; ok=false leaves EntityOther to the upsert default.
func mapEntityType(t string) (domain.EntityType, bool) {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "person":
		return domain.EntityPerson, true
	case "project":
		return domain.EntityProject, true
	case "tool":
		return domain.EntityTool, true
	case "organization", "org":
		return domain.EntityOrganization, true
	case "place", "location":
		return domain.EntityPlace, true
	case "concept":
		return domain.EntityConcept, true
	default:
		return domain.EntityOther, true // file/event/artifact/role/... → other
	}
}
