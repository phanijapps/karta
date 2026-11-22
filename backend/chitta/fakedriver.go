package chitta

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/textutil"
)

// fakeDriver is an in-memory driver for tests: same semantics contract
// as the LadybugDB driver, no persistence. History/search orderings
// implement the per-tool total orders.
type fakeDriver struct {
	mu            sync.Mutex
	records       map[string]*domain.MemoryRecord
	contributions map[string]*domain.MemoryContribution
	entities      map[string]*domain.Entity
	byNormalName  map[string]string // normalized name/alias -> entity id
	relationships map[string]*domain.Relationship
	embeddings    map[string][]float64
	failNextWrite bool // test injection: next composite write fails
	markers       map[string]markerTuple
}

type markerTuple struct {
	attempts int
	dead     bool
	at       time.Time
}

func newFakeDriver() *fakeDriver {
	return &fakeDriver{
		markers:       map[string]markerTuple{},
		records:       map[string]*domain.MemoryRecord{},
		contributions: map[string]*domain.MemoryContribution{},
		entities:      map[string]*domain.Entity{},
		byNormalName:  map[string]string{},
		relationships: map[string]*domain.Relationship{},
		embeddings:    map[string][]float64{},
	}
}

func (f *fakeDriver) Close() error { return nil }

func (f *fakeDriver) GetRecord(ctx context.Context, id string) (domain.MemoryRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[id]
	if !ok {
		return domain.MemoryRecord{}, domain.NewChittaError(domain.ErrNotFound, "record %s not found", id)
	}
	return *rec, nil
}

func (f *fakeDriver) ApplyObservations(ctx context.Context, ops []domain.ObservationOp) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNextWrite {
		f.failNextWrite = false
		return domain.NewChittaError(domain.ErrUnavailable, "injected write failure")
	}
	for _, op := range ops {
		if op.Create != nil {
			cp := *op.Create
			f.records[op.Create.ID] = &cp
		} else {
			rec, ok := f.records[op.MergeID]
			if !ok {
				return domain.NewChittaError(domain.ErrNotFound, "record %s not found", op.MergeID)
			}
			rec.Confidence = op.NewConfidence
			rec.MentionCount++
			rec.LastConfirmedAt = &op.LastConfirmed
			rec.UpdatedAt = op.Updated
			for _, e := range op.AddEntities {
				rec.Entities = appendUnique(rec.Entities, e)
			}
		}
		f.putContributionLocked(op.Contribution)
	}
	return nil
}

func (f *fakeDriver) Supersede(ctx context.Context, oldID string, newRec *domain.MemoryRecord, c *domain.MemoryContribution) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failNextWrite {
		f.failNextWrite = false
		return domain.NewChittaError(domain.ErrUnavailable, "injected write failure")
	}
	old, ok := f.records[oldID]
	if !ok {
		return domain.NewChittaError(domain.ErrNotFound, "record %s not found", oldID)
	}
	now := time.Now().UTC()
	old.Status = domain.MemoryStatusSuperseded
	old.SupersededBy = newRec.ID
	old.UpdatedAt = now
	cp := *newRec
	f.records[newRec.ID] = &cp
	f.putContributionLocked(c)
	return nil
}

func (f *fakeDriver) putContributionLocked(c *domain.MemoryContribution) {
	cp := *c
	f.contributions[c.ContentHash] = &cp
}

func (f *fakeDriver) FindActiveByContent(ctx context.Context, subject string, kind domain.MemoryKind, norm string) (domain.MemoryRecord, domain.MemoryStatus, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best *domain.MemoryRecord
	for _, rec := range f.records {
		if rec.Subject == subject && rec.Kind == kind && textutil.NormalizeText(rec.Content) == norm {
			if best == nil || rec.CreatedAt.After(best.CreatedAt) {
				best = rec
			}
		}
	}
	if best == nil {
		return domain.MemoryRecord{}, "", false, nil
	}
	return *best, best.Status, true, nil
}

func (f *fakeDriver) ListRecords(ctx context.Context, req domain.HistoryRequest) (domain.Page[domain.MemoryRecord], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var recs []domain.MemoryRecord
	idset := map[string]bool{}
	for _, id := range req.IDs {
		idset[id] = true
	}
	for _, rec := range f.records {
		if req.ActiveOnly && rec.Status != domain.MemoryStatusActive {
			continue
		}
		if len(idset) > 0 && !idset[rec.ID] {
			continue
		}
		if req.Subject != "" && rec.Subject != req.Subject {
			continue
		}
		if !kindMatch(req.Kinds, rec.Kind) || !tagMatch(req.Tags, rec.Tags) {
			continue
		}
		if req.Since != nil && rec.CreatedAt.Before(*req.Since) {
			continue
		}
		if req.Until != nil && rec.CreatedAt.After(*req.Until) {
			continue
		}
		recs = append(recs, *rec)
	}
	sort.Slice(recs, func(i, j int) bool { // per-request total order
		if !recs[i].CreatedAt.Equal(recs[j].CreatedAt) {
			if req.NewestFirst {
				return recs[i].CreatedAt.After(recs[j].CreatedAt)
			}
			return recs[i].CreatedAt.Before(recs[j].CreatedAt)
		}
		return recs[i].ID < recs[j].ID
	})
	limit := req.Limit
	if limit <= 0 {
		limit = domain.RecallDefaultLimit
	}
	start := 0
	if req.Cursor != "" {
		start = cursorIndex(req.Cursor, recs, func(r domain.MemoryRecord) string { return r.ID })
	}
	return pageOf(recs, start, limit, func(r domain.MemoryRecord) string { return r.ID }), nil
}

func (f *fakeDriver) SearchRecords(ctx context.Context, req domain.RecallRequest) (domain.Page[domain.MemoryRecord], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := textutil.NormalizeText(req.Query)
	type scored struct {
		rec   domain.MemoryRecord
		score float64
	}
	var hits []scored
	for _, rec := range f.records {
		if !req.IncludeSuperseded && rec.Status != domain.MemoryStatusActive {
			continue
		}
		if req.Subject != "" && rec.Subject != req.Subject {
			continue
		}
		if !kindMatch(req.Kinds, rec.Kind) || !tagMatch(req.Tags, rec.Tags) {
			continue
		}
		// Keyword-lane approximation: whole-query substring on normalized
		// content. Production runs native BM25 (stems, term weighting);
		// shared-suite cases use exact substrings both drivers satisfy.
		content := textutil.NormalizeText(rec.Content)
		if q != "" && strings.Contains(content, q) {
			hits = append(hits, scored{*rec, 1.0})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		if !hits[i].rec.CreatedAt.Equal(hits[j].rec.CreatedAt) {
			return hits[i].rec.CreatedAt.After(hits[j].rec.CreatedAt)
		}
		return hits[i].rec.ID < hits[j].rec.ID
	})
	out := make([]domain.MemoryRecord, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.rec)
	}
	return domain.Page[domain.MemoryRecord]{Items: out}, nil
}

func (f *fakeDriver) VectorSearch(ctx context.Context, embedding []float64, limit int) ([]domain.VectorHit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	type hit struct {
		id    string
		score float64
	}
	var hits []hit
	for id, emb := range f.embeddings {
		if rec, ok := f.records[id]; ok && rec.Status == domain.MemoryStatusActive && len(emb) == len(embedding) {
			hits = append(hits, hit{id, cosine(emb, embedding)})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].id < hits[j].id
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]domain.VectorHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, domain.VectorHit{RecordID: h.id, Score: h.score})
	}
	return out, nil
}

func (f *fakeDriver) MarkDistilled(ctx context.Context, sessionID string, attempts int, deadLetter bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markers[sessionID] = markerTuple{attempts, deadLetter, time.Now().UTC()}
	return nil
}

func (f *fakeDriver) DistillationMarkers(ctx context.Context) (map[string]domain.DistillationMarker, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]domain.DistillationMarker, len(f.markers))
	for k, v := range f.markers {
		out[k] = domain.DistillationMarker{Attempts: v.attempts, DeadLetter: v.dead, At: v.at}
	}
	return out, nil
}

func (f *fakeDriver) RecordsLackingEmbeddings(ctx context.Context, limit int) ([]string, []string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids, contents []string
	for _, rec := range f.records {
		if rec.Status == domain.MemoryStatusActive {
			if _, ok := f.embeddings[rec.ID]; !ok {
				ids = append(ids, rec.ID)
				contents = append(contents, rec.Content)
				if len(ids) >= limit {
					break
				}
			}
		}
	}
	return ids, contents, nil
}

func (f *fakeDriver) EmbeddingsFor(ctx context.Context, ids []string) (map[string][]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][]float64{}
	for _, id := range ids {
		if emb, ok := f.embeddings[id]; ok {
			out[id] = emb
		}
	}
	return out, nil
}

func (f *fakeDriver) RecordsMentioning(ctx context.Context, entityID string, limit int, cursor string) (domain.Page[domain.MemoryRecord], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var recs []domain.MemoryRecord
	for _, rec := range f.records {
		if rec.Status != domain.MemoryStatusActive {
			continue
		}
		for _, e := range rec.Entities {
			if e == entityID {
				recs = append(recs, *rec)
				break
			}
		}
	}
	sort.Slice(recs, func(i, j int) bool { // created_at desc, id
		if !recs[i].CreatedAt.Equal(recs[j].CreatedAt) {
			return recs[i].CreatedAt.After(recs[j].CreatedAt)
		}
		return recs[i].ID < recs[j].ID
	})
	if limit <= 0 {
		limit = domain.RecallDefaultLimit
	}
	start := 0
	if cursor != "" {
		start = cursorIndex(cursor, recs, func(r domain.MemoryRecord) string { return r.ID })
	}
	return pageOf(recs, start, limit, func(r domain.MemoryRecord) string { return r.ID }), nil
}

func (f *fakeDriver) GetContribution(ctx context.Context, identity string) (domain.MemoryContribution, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.contributions[identity]; ok {
		return *c, true, nil
	}
	return domain.MemoryContribution{}, false, nil
}

func (f *fakeDriver) ListContributions(ctx context.Context, recordID string) ([]domain.MemoryContribution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.MemoryContribution
	for _, c := range f.contributions {
		if c.RecordID == recordID {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (f *fakeDriver) SetEmbeddings(ctx context.Context, embeddings []domain.RecordEmbedding) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range embeddings {
		f.embeddings[e.RecordID] = e.Embedding
	}
	return nil
}

func (f *fakeDriver) UpsertEntityByName(ctx context.Context, name string, entityType domain.EntityType, aliases []string, confidence *float64, now time.Time) (domain.Entity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	norm := textutil.NormalizeText(name)
	if id, ok := f.byNormalName[norm]; ok {
		e := f.entities[id]
		for _, a := range aliases {
			an := textutil.NormalizeText(a)
			if an != "" {
				f.byNormalName[an] = id
				e.Aliases = appendUnique(e.Aliases, an)
			}
		}
		e.MentionCount++
		e.LastMentionedAt = &now
		return *e, nil
	}
	id := newID("ent")
	if entityType == "" {
		entityType = domain.EntityOther
	}
	conf := 0.5
	if confidence != nil {
		conf = domain.Clamp01(*confidence)
	}
	e := &domain.Entity{
		ID: id, Type: entityType, Name: strings.TrimSpace(name), NormalizedName: norm,
		Confidence: conf, MentionCount: 1, LastMentionedAt: &now,
	}
	for _, a := range aliases {
		an := textutil.NormalizeText(a)
		if an != "" && an != norm {
			f.byNormalName[an] = id
			e.Aliases = appendUnique(e.Aliases, an)
		}
	}
	f.entities[id] = e
	f.byNormalName[norm] = id
	return *e, nil
}

func (f *fakeDriver) ResolveEntity(ctx context.Context, ref string) (domain.Entity, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Production precedence: id → normalized name → alias.
	if e, ok := f.entities[ref]; ok {
		return *e, true, nil
	}
	norm := textutil.NormalizeText(ref)
	if id, ok := f.byNormalName[norm]; ok {
		return *f.entities[id], true, nil
	}
	for _, e := range f.entities {
		for _, a := range e.Aliases {
			if a == norm {
				return *e, true, nil
			}
		}
	}
	return domain.Entity{}, false, nil
}

func (f *fakeDriver) SearchEntities(ctx context.Context, req domain.EntitySearchRequest) (domain.Page[domain.Entity], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	prefix := textutil.NormalizeText(req.Prefix)
	type ranked struct {
		e    domain.Entity
		rank int
	}
	var out []ranked
	for _, e := range f.entities {
		if req.Type != "" && e.Type != req.Type {
			continue
		}
		rank := 1
		if !strings.HasPrefix(e.NormalizedName, prefix) {
			found := false
			for _, a := range e.Aliases {
				if strings.HasPrefix(a, prefix) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
			rank = 2
		}
		out = append(out, ranked{*e, rank})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		if out[i].e.Name != out[j].e.Name {
			return out[i].e.Name < out[j].e.Name
		}
		return out[i].e.ID < out[j].e.ID
	})
	limit := req.Limit
	if limit <= 0 {
		limit = domain.RecallDefaultLimit
	}
	entities := make([]domain.Entity, 0, len(out))
	for _, r := range out {
		entities = append(entities, r.e)
	}
	// Top-k like the real driver (v1: no cursor on entity search).
	if len(entities) > limit {
		entities = entities[:limit]
	}
	return domain.Page[domain.Entity]{Items: entities}, nil
}

func (f *fakeDriver) UpdateRelationshipConfidence(ctx context.Context, id string, confidence float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, rel := range f.relationships {
		if rel.ID == id {
			rel.Confidence = confidence
			return nil
		}
	}
	return domain.NewChittaError(domain.ErrNotFound, "relationship %s not found", id)
}

func (f *fakeDriver) FindRelationship(ctx context.Context, from, to, relType string) (domain.Relationship, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := from + "\x1f" + to + "\x1f" + relType
	if rel, ok := f.relationships[key]; ok {
		return *rel, true, nil
	}
	return domain.Relationship{}, false, nil
}

func (f *fakeDriver) CreateRelationship(ctx context.Context, rel *domain.Relationship) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := rel.From + "\x1f" + rel.To + "\x1f" + rel.Type
	cp := *rel
	f.relationships[key] = &cp
	return nil
}

func (f *fakeDriver) Neighborhood(ctx context.Context, entityID string, depth int, types []string, limit int) (domain.Subgraph, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sub := domain.Subgraph{Nodes: []domain.Entity{}, Edges: []domain.Relationship{}}
	seen := map[string]bool{entityID: true}
	frontier := []string{entityID}
	for hop := 0; hop < depth && len(frontier) > 0; hop++ {
		var next []string
		for _, rel := range f.relationships {
			if !typeAllowed(rel.Type, types) {
				continue
			}
			var other string
			if containsStr(frontier, rel.From) && !seen[rel.To] {
				other = rel.To
			} else if containsStr(frontier, rel.To) && !seen[rel.From] {
				other = rel.From
			} else {
				continue
			}
			seen[other] = true
			sub.Edges = append(sub.Edges, *rel)
			if e, ok := f.entities[other]; ok {
				sub.Nodes = append(sub.Nodes, *e)
			}
			next = append(next, other)
			if len(sub.Nodes) >= limit {
				return sub, nil
			}
		}
		frontier = next
	}
	return sub, nil
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func typeAllowed(t string, types []string) bool {
	if len(types) == 0 {
		return true
	}
	return containsStr(types, t)
}

func kindMatch(want []domain.MemoryKind, have domain.MemoryKind) bool {
	if len(want) == 0 {
		return true
	}
	for _, k := range want {
		if k == have {
			return true
		}
	}
	return false
}

func tagMatch(want, have []string) bool {
	if len(want) == 0 {
		return true
	}
	set := map[string]bool{}
	for _, t := range have {
		set[t] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

func cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// cursor helpers: the opaque cursor is the id of the last-emitted item;
// resuming continues after it (stable for a fixed dataset).
func cursorIndex[T any](cursor string, items []T, idOf func(T) string) int {
	for i, item := range items {
		if "cur:"+idOf(item) == cursor {
			return i + 1
		}
	}
	return 0
}

func pageOf[T any](items []T, start, limit int, idOf func(T) string) domain.Page[T] {
	if start >= len(items) {
		return domain.Page[T]{Items: []T{}}
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	page := domain.Page[T]{Items: items[start:end]}
	if end < len(items) {
		page.NextCursor = "cur:" + idOf(items[end-1])
	}
	return page
}

// PutRecordForTest seeds one record (test helper).
func (f *fakeDriver) PutRecordForTest(ctx context.Context, i int) error {
	now := time.Now().UTC()
	rec := &domain.MemoryRecord{
		ID: newID("mem"), Kind: domain.MemoryKindObservation,
		Subject: "user", Content: "seed " + string(rune('a'+i)),
		Confidence: 0.5, Status: domain.MemoryStatusActive,
		Provenance:   domain.MemoryProvenance{Source: domain.SourceAgent},
		MentionCount: 1, LastConfirmedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	c := &domain.MemoryContribution{
		RecordID: rec.ID, Source: domain.SourceAgent, ContentHash: "test-" + rec.ID, CreatedAt: now,
	}
	return f.ApplyObservations(ctx, []domain.ObservationOp{{Create: rec, Contribution: c}})
}
