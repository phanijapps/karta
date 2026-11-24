package ladybug

import (
	"context"
	"strings"
	"time"
	"zbot/backend/internal/textutil"

	"zbot/backend/internal/domain"
)

// --- memory records ---------------------------------------------------------

// createRecordCypher builds the CREATE for a record; list properties
// are SET afterward when non-empty (the engine cannot bind empty LISTs).
func createRecordCypher() string {
	return `
		CREATE (m:Memory {
			id: $id, kind: $kind, content: $content, normalized_content: $normalized_content,
			confidence: $confidence, subject: $subject, status: $status,
			supersedes: $supersedes, superseded_by: $superseded_by,
			mention_count: $mention_count, last_confirmed_at: $last_confirmed_at,
			source: $source, session_id: $session_id, execution_id: $execution_id,
			agent_id: $agent_id, message_seq: $message_seq,
			created_at: $created_at, updated_at: $updated_at
		})`
}

func rowToRecord(row map[string]any) domain.MemoryRecord {
	rec := domain.MemoryRecord{
		ID: asString(row["id"]), Kind: domain.MemoryKind(asString(row["kind"])),
		Content: asString(row["content"]), Tags: asStrings(row["tags"]),
		Confidence: asFloat(row["confidence"]), Subject: asString(row["subject"]),
		Status:     domain.MemoryStatus(asString(row["status"])),
		Supersedes: asString(row["supersedes"]), SupersededBy: asString(row["superseded_by"]),
		Entities: asStrings(row["entities"]), MentionCount: asInt64(row["mention_count"]),
		Provenance: domain.MemoryProvenance{
			Source:      domain.MemorySource(asString(row["source"])),
			SessionID:   asString(row["session_id"]),
			ExecutionID: asString(row["execution_id"]),
			AgentID:     asString(row["agent_id"]),
		},
	}
	if seq, ok := row["message_seq"].(int64); ok {
		rec.Provenance.MessageSeq = &seq
	}
	if t, ok := asTime(row["last_confirmed_at"]); ok {
		rec.LastConfirmedAt = &t
	}
	rec.CreatedAt, _ = asTime(row["created_at"])
	rec.UpdatedAt, _ = asTime(row["updated_at"])
	return rec
}

var memoryColumns = `m.id AS id, m.kind AS kind, m.content AS content, m.tags AS tags,
	m.confidence AS confidence, m.subject AS subject, m.status AS status,
	m.supersedes AS supersedes, m.superseded_by AS superseded_by,
	m.entities AS entities, m.mention_count AS mention_count,
	m.last_confirmed_at AS last_confirmed_at, m.source AS source,
	m.session_id AS session_id, m.execution_id AS execution_id,
	m.agent_id AS agent_id, m.message_seq AS message_seq,
	m.created_at AS created_at, m.updated_at AS updated_at`

func recordArgs(rec *domain.MemoryRecord) map[string]any {
	var lastConfirmed any
	if rec.LastConfirmedAt != nil {
		lastConfirmed = (*rec.LastConfirmedAt).Truncate(time.Microsecond)
	}
	var msgSeq any
	if rec.Provenance.MessageSeq != nil {
		msgSeq = *rec.Provenance.MessageSeq
	}
	return map[string]any{
		"id": rec.ID, "kind": string(rec.Kind), "content": rec.Content,
		"normalized_content": textutil.NormalizeText(rec.Content),
		"confidence":         rec.Confidence, "subject": rec.Subject,
		"status": string(rec.Status), "supersedes": rec.Supersedes,
		"superseded_by": rec.SupersededBy,
		"mention_count": rec.MentionCount, "last_confirmed_at": lastConfirmed,
		"source":       string(rec.Provenance.Source),
		"session_id":   rec.Provenance.SessionID,
		"execution_id": rec.Provenance.ExecutionID,
		"agent_id":     rec.Provenance.AgentID,
		"message_seq":  msgSeq,
		"created_at":   rec.CreatedAt.Truncate(time.Microsecond), "updated_at": rec.UpdatedAt.Truncate(time.Microsecond),
	}
}

// ApplyObservations atomically persists a whole remember batch inside
// ONE engine transaction (review round 3: per-item commits leaked
// partial batches on failure).
func (d *Driver) ApplyObservations(ctx context.Context, ops []domain.ObservationOp) error {
	return d.withTx(ctx, func() error {
		for _, op := range ops {
			if op.Create != nil {
				if err := d.execLocked(ctx, createRecordCypher(), recordArgs(op.Create)); err != nil {
					return err
				}
				if err := d.setRecordListsLocked(ctx, op.Create); err != nil {
					return err
				}
			} else {
				if err := d.execLocked(ctx, `
					MATCH (m:Memory {id: $id})
					SET m.confidence = $confidence, m.mention_count = m.mention_count + 1,
					    m.last_confirmed_at = $last_confirmed_at, m.updated_at = $updated_at`,
					map[string]any{"id": op.MergeID, "confidence": op.NewConfidence,
						"last_confirmed_at": op.LastConfirmed, "updated_at": op.Updated}); err != nil {
					return err
				}
				if len(op.AddEntities) > 0 {
					// Deduplicated union: read current links, merge in Go,
					// SET once (review round 3: blind list_append duplicated).
					rows, err := d.queryRowLocked(ctx,
						"MATCH (m:Memory {id: $id}) RETURN m.entities AS entities",
						map[string]any{"id": op.MergeID})
					if err != nil {
						return err
					}
					union := asStrings(rows["entities"])
					for _, e := range op.AddEntities {
						union = appendUniqueStr(union, e)
					}
					if len(union) > 0 {
						if err := d.execLocked(ctx,
							"MATCH (m:Memory {id: $id}) SET m.entities = $entities",
							map[string]any{"id": op.MergeID, "entities": union}); err != nil {
							return err
						}
					}
				}
			}
			if err := d.putContributionLocked(ctx, op.Contribution); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetRecord — exported library symbol.
func (d *Driver) GetRecord(ctx context.Context, id string) (domain.MemoryRecord, error) {
	rows, err := d.queryRows(ctx,
		"MATCH (m:Memory {id: $id}) RETURN "+memoryColumns, map[string]any{"id": id})
	if err != nil {
		return domain.MemoryRecord{}, err
	}
	if len(rows) == 0 {
		return domain.MemoryRecord{}, domain.NewChittaError(domain.ErrNotFound, "record %s not found", id)
	}
	return rowToRecord(rows[0]), nil
}

// WriteObservation atomically persists a new record (with list
// properties and entity links) plus its contribution.
func (d *Driver) WriteObservation(ctx context.Context, rec *domain.MemoryRecord, c *domain.MemoryContribution) error {
	return d.withTx(ctx, func() error {
		if err := d.execLocked(ctx, createRecordCypher(), recordArgs(rec)); err != nil {
			return err
		}
		if err := d.setRecordListsLocked(ctx, rec); err != nil {
			return err
		}
		return d.putContributionLocked(ctx, c)
	})
}

// MergeObservation atomically applies the merge bump, adds entity
// links, and persists the new contribution.
func (d *Driver) MergeObservation(ctx context.Context, id string, newConfidence float64, lastConfirmedAt, updatedAt time.Time, addEntities []string, c *domain.MemoryContribution) error {
	return d.withTx(ctx, func() error {
		if err := d.execLocked(ctx, `
			MATCH (m:Memory {id: $id})
			SET m.confidence = $confidence, m.mention_count = m.mention_count + 1,
			    m.last_confirmed_at = $last_confirmed_at, m.updated_at = $updated_at`,
			map[string]any{"id": id, "confidence": newConfidence,
				"last_confirmed_at": lastConfirmedAt, "updated_at": updatedAt}); err != nil {
			return err
		}
		if len(addEntities) > 0 {
			// Deduplicated union: read the current links inside the
			// transaction, merge in Go, SET once (review round 3: blind
			// list_append duplicated links on repeated evidence).
			rows, err := d.queryRowLocked(ctx,
				"MATCH (m:Memory {id: $id}) RETURN m.entities AS entities",
				map[string]any{"id": id})
			if err != nil {
				return err
			}
			current := asStrings(rows["entities"])
			union := current
			for _, e := range addEntities {
				union = appendUniqueStr(union, e)
			}
			if len(union) > 0 {
				if err := d.execLocked(ctx,
					"MATCH (m:Memory {id: $id}) SET m.entities = $entities",
					map[string]any{"id": id, "entities": union}); err != nil {
					return err
				}
			}
		}
		return d.putContributionLocked(ctx, c)
	})
}

// Supersede atomically flips old to superseded and persists the
// revision record with its contribution.
func (d *Driver) Supersede(ctx context.Context, oldID string, newRec *domain.MemoryRecord, c *domain.MemoryContribution) error {
	return d.withTx(ctx, func() error {
		if err := d.execLocked(ctx, `
			MATCH (m:Memory {id: $old})
			SET m.status = $status, m.superseded_by = $new, m.updated_at = $now`,
			map[string]any{"old": oldID, "status": string(domain.MemoryStatusSuperseded),
				"new": newRec.ID, "now": newRec.UpdatedAt.Truncate(time.Microsecond)}); err != nil {
			return err
		}
		if err := d.execLocked(ctx, createRecordCypher(), recordArgs(newRec)); err != nil {
			return err
		}
		if err := d.setRecordListsLocked(ctx, newRec); err != nil {
			return err
		}
		return d.putContributionLocked(ctx, c)
	})
}

// setRecordListsLocked sets list properties only when non-empty (the
// engine cannot bind empty LIST values).
func (d *Driver) setRecordListsLocked(ctx context.Context, rec *domain.MemoryRecord) error {
	if len(rec.Tags) > 0 {
		if err := d.execLocked(ctx, "MATCH (m:Memory {id: $id}) SET m.tags = $tags",
			map[string]any{"id": rec.ID, "tags": rec.Tags}); err != nil {
			return err
		}
	}
	if len(rec.Entities) > 0 {
		if err := d.execLocked(ctx, "MATCH (m:Memory {id: $id}) SET m.entities = $entities",
			map[string]any{"id": rec.ID, "entities": rec.Entities}); err != nil {
			return err
		}
	}
	return nil
}

func (d *Driver) putContributionLocked(ctx context.Context, c *domain.MemoryContribution) error {
	var msgSeq any
	if c.MessageSeq != nil {
		msgSeq = *c.MessageSeq
	}
	// MERGE, not CREATE: the contribution's id IS its content identity
	// (ContributionIdentity hash), so re-asserting the same evidence —
	// the resource indexer's constant host context revising the same
	// subject+content across syncs — is IDEMPOTENT evidence, never a
	// primary-key violation (the live-boot failure this fixed).
	return d.execLocked(ctx, `
		MERGE (c:Contribution {id: $id})
		ON CREATE SET c.record_id = $record_id, c.source = $source,
			c.session_id = $session_id, c.execution_id = $execution_id,
			c.message_seq = $message_seq, c.content_hash = $content_hash,
			c.created_at = $created_at`, map[string]any{
		"id": c.ContentHash, "record_id": c.RecordID, "source": string(c.Source),
		"session_id": c.SessionID, "execution_id": c.ExecutionID,
		"message_seq": msgSeq, "content_hash": c.ContentHash,
		"created_at": c.CreatedAt.Truncate(time.Microsecond),
	})
}

// FindActiveByContent — exported library symbol.
func (d *Driver) FindActiveByContent(ctx context.Context, subject string, kind domain.MemoryKind, normalizedContent string) (domain.MemoryRecord, domain.MemoryStatus, bool, error) {
	rows, err := d.queryRows(ctx, `
		MATCH (m:Memory)
		WHERE m.subject = $subject AND m.kind = $kind
		  AND m.normalized_content = $norm
		RETURN `+memoryColumns+`
		ORDER BY m.created_at DESC LIMIT 1`,
		map[string]any{"subject": subject, "kind": string(kind), "norm": normalizedContent})
	if err != nil {
		return domain.MemoryRecord{}, "", false, err
	}
	if len(rows) == 0 {
		return domain.MemoryRecord{}, "", false, nil
	}
	rec := rowToRecord(rows[0])
	return rec, rec.Status, true, nil
}

// ListRecords — exported library symbol.
func (d *Driver) ListRecords(ctx context.Context, req domain.HistoryRequest) (domain.Page[domain.MemoryRecord], error) {
	where := []string{"1 = 1"}
	args := map[string]any{}
	if req.ActiveOnly {
		where = append(where, "m.status = 'active'")
	}
	if req.Subject != "" {
		where = append(where, "m.subject = $subject")
		args["subject"] = req.Subject
	}
	if len(req.Kinds) > 0 {
		where = append(where, "m.kind IN $kinds")
		kinds := make([]string, 0, len(req.Kinds))
		for _, k := range req.Kinds {
			kinds = append(kinds, string(k))
		}
		args["kinds"] = kinds
	}
	if len(req.IDs) > 0 {
		where = append(where, "m.id IN $ids")
		args["ids"] = req.IDs
	}
	// Loop-fetch raw pages until `limit` survivors or exhaustion, so
	// post-fetch tag/time filters cannot strand later matches behind a
	// dead cursor (contracts "Full pagination walk is exact"). The cursor
	// predicate is applied on EVERY iteration from the running scan
	// cursor (review round 1: skipping it on later pages repeated page
	// one forever).
	limit := req.Limit
	if limit <= 0 {
		limit = domain.RecallDefaultLimit
	}
	baseWhere := strings.Join(where, " AND ")
	args["lim"] = 100
	items := []domain.MemoryRecord{}
	scanCursor := req.Cursor
	for len(items) <= limit {
		iterArgs := map[string]any{}
		for k, v := range args {
			iterArgs[k] = v
		}
		pageWhere := baseWhere
		if scanCursor != "" {
			ct, cid, ok := decodeCursor(scanCursor)
			if !ok {
				break
			}
			iterArgs["ct"], iterArgs["cid"] = ct, cid
			if req.NewestFirst {
				pageWhere += " AND (m.created_at < $ct OR (m.created_at = $ct AND m.id > $cid))"
			} else {
				pageWhere += " AND (m.created_at > $ct OR (m.created_at = $ct AND m.id > $cid))"
			}
		}
		order := "m.created_at ASC, m.id ASC"
		if req.NewestFirst {
			order = "m.created_at DESC, m.id ASC"
		}
		rows, err := d.queryRows(ctx, `
			MATCH (m:Memory) WHERE `+pageWhere+`
			RETURN `+memoryColumns+`
			ORDER BY `+order+` LIMIT $lim`, iterArgs)
		if err != nil {
			return domain.Page[domain.MemoryRecord]{}, err
		}
		if len(rows) == 0 {
			break
		}
		items = append(items, filterRecords(rows, req)...)
		if len(rows) < 100 {
			break
		}
		last := rowToRecord(rows[len(rows)-1])
		scanCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	var page domain.Page[domain.MemoryRecord]
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	page.Items = items
	return page, nil
}

func filterRecords(rows []map[string]any, req domain.HistoryRequest) []domain.MemoryRecord {
	out := make([]domain.MemoryRecord, 0, len(rows))
	for _, row := range rows {
		rec := rowToRecord(row)
		if len(req.Tags) > 0 {
			set := map[string]bool{}
			for _, t := range rec.Tags {
				set[t] = true
			}
			ok := true
			for _, want := range req.Tags {
				if !set[want] {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
		}
		if req.Since != nil && rec.CreatedAt.Before(*req.Since) {
			continue
		}
		if req.Until != nil && rec.CreatedAt.After(*req.Until) {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// SearchRecords is the keyword lane: native Okapi BM25 via the FTS
// extension (Porter stemmer), with tag/time filters applied post-fetch
// before truncation.
func (d *Driver) SearchRecords(ctx context.Context, req domain.RecallRequest) (domain.Page[domain.MemoryRecord], error) {
	q := textutil.NormalizeText(req.Query)
	lim := domain.RecallMaxLimit // lanes over-fetch; fusion trims
	if q == "" {
		return domain.Page[domain.MemoryRecord]{Items: []domain.MemoryRecord{}}, nil
	}
	args := map[string]any{"q": q, "lim": lim}
	rows, err := d.queryRows(ctx, `
		CALL QUERY_FTS_INDEX('Memory', 'mem_fts', $q, TOP := $lim)
		RETURN node.id AS id, score`, args)
	if err != nil {
		return domain.Page[domain.MemoryRecord]{}, err
	}
	items := make([]domain.MemoryRecord, 0, len(rows))
	for _, row := range rows {
		rec, err := d.GetRecord(ctx, asString(row["id"]))
		if err != nil {
			continue // raced delete; skip
		}
		if !recordPasses(rec, req) {
			continue
		}
		items = append(items, rec)
		if len(items) >= lim {
			break
		}
	}
	return domain.Page[domain.MemoryRecord]{Items: items}, nil
}

// recordPasses applies status/subject/kind/tag/time/entity filters the
// index query cannot express.
func recordPasses(rec domain.MemoryRecord, req domain.RecallRequest) bool {
	if !req.IncludeSuperseded && rec.Status != domain.MemoryStatusActive {
		return false
	}
	if req.Subject != "" && rec.Subject != req.Subject {
		return false
	}
	if len(req.Kinds) > 0 {
		ok := false
		for _, k := range req.Kinds {
			if rec.Kind == k {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(req.Tags) > 0 {
		set := map[string]bool{}
		for _, t := range rec.Tags {
			set[t] = true
		}
		for _, want := range req.Tags {
			if !set[want] {
				return false
			}
		}
	}
	if len(req.EntityIDs) > 0 {
		hit := false
		for _, want := range req.EntityIDs {
			for _, have := range rec.Entities {
				if have == want {
					hit = true
				}
			}
		}
		if !hit {
			return false
		}
	}
	if req.Since != nil && rec.CreatedAt.Before(*req.Since) {
		return false
	}
	if req.Until != nil && rec.CreatedAt.After(*req.Until) {
		return false
	}
	return true
}

// VectorSearch is the vector lane: the native vector index over the
// fixed-dimension embedding column. Distance converts to similarity
// rank (smaller distance = stronger match).
func (d *Driver) VectorSearch(ctx context.Context, embedding []float64, limit int) ([]domain.VectorHit, error) {
	if len(embedding) == 0 || limit <= 0 {
		return nil, nil
	}
	rows, err := d.queryRows(ctx, `
		CALL QUERY_VECTOR_INDEX('Memory', 'mem_vec', $emb, $k)
		RETURN node.id AS id, distance`, map[string]any{"emb": embedding, "k": limit})
	if err != nil {
		return nil, err
	}
	hits := make([]domain.VectorHit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, domain.VectorHit{
			RecordID: asString(row["id"]),
			Score:    1.0 - asFloat(row["distance"]),
		})
	}
	return hits, nil
}

// RecordsLackingEmbeddings pages records the producer must embed.
func (d *Driver) RecordsLackingEmbeddings(ctx context.Context, limit int) ([]string, []string, error) {
	rows, err := d.queryRows(ctx, `
		MATCH (m:Memory {status: 'active'})
		WHERE m.embedding IS NULL
		RETURN m.id AS id, m.content AS content
		ORDER BY m.created_at ASC LIMIT $lim`, map[string]any{"lim": limit})
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(rows))
	contents := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, asString(row["id"]))
		contents = append(contents, asString(row["content"]))
	}
	return ids, contents, nil
}

// EmbeddingsFor returns stored embeddings for MMR.
func (d *Driver) EmbeddingsFor(ctx context.Context, ids []string) (map[string][]float64, error) {
	out := map[string][]float64{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := d.queryRows(ctx, `
		MATCH (m:Memory)
		WHERE m.id IN $ids AND m.embedding IS NOT NULL
		RETURN m.id AS id, m.embedding AS emb`, map[string]any{"ids": ids})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[asString(row["id"])] = asFloats(row["emb"])
	}
	return out, nil
}

// RecordsMentioning — exported library symbol.
func (d *Driver) RecordsMentioning(ctx context.Context, entityID string, limit int, cursor string) (domain.Page[domain.MemoryRecord], error) {
	// Source of truth is the record's entities array (the Mentions rel
	// table is maintained for future graph-side traversals by the
	// distiller; array membership is the normative link).
	args := map[string]any{"eid": entityID}
	where := "m.status = 'active'"
	if cursorT, cursorID, has := decodeCursor(cursor); has {
		args["ct"], args["cid"] = cursorT, cursorID
		where += " AND (m.created_at < $ct OR (m.created_at = $ct AND m.id > $cid))"
	}
	if limit <= 0 {
		limit = domain.RecallDefaultLimit
	}
	args["lim"] = limit + 1
	rows, err := d.queryRows(ctx, `
		MATCH (m:Memory)
		WHERE $eid IN m.entities AND `+where+`
		RETURN `+memoryColumns+`
		ORDER BY m.created_at DESC, m.id ASC LIMIT $lim`, args)
	if err != nil {
		return domain.Page[domain.MemoryRecord]{}, err
	}
	var page domain.Page[domain.MemoryRecord]
	page.Items = make([]domain.MemoryRecord, 0, len(rows))
	for _, row := range rows {
		page.Items = append(page.Items, rowToRecord(row))
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

// --- evidence ledger ----------------------------------------------------------

// GetContribution — exported library symbol.
func (d *Driver) GetContribution(ctx context.Context, identity string) (domain.MemoryContribution, bool, error) {
	rows, err := d.queryRows(ctx, `
		MATCH (c:Contribution {content_hash: $h})
		RETURN c.id AS id, c.record_id AS record_id, c.source AS source,
		       c.session_id AS session_id, c.execution_id AS execution_id,
		       c.message_seq AS message_seq, c.content_hash AS content_hash,
		       c.created_at AS created_at LIMIT 1`,
		map[string]any{"h": identity})
	if err != nil {
		return domain.MemoryContribution{}, false, err
	}
	if len(rows) == 0 {
		return domain.MemoryContribution{}, false, nil
	}
	row := rows[0]
	c := domain.MemoryContribution{
		RecordID:  asString(row["record_id"]),
		Source:    domain.MemorySource(asString(row["source"])),
		SessionID: asString(row["session_id"]), ExecutionID: asString(row["execution_id"]),
		ContentHash: asString(row["content_hash"]),
	}
	if seq, ok := row["message_seq"].(int64); ok {
		c.MessageSeq = &seq
	}
	c.CreatedAt, _ = asTime(row["created_at"])
	return c, true, nil
}

// ListContributions — exported library symbol.
func (d *Driver) ListContributions(ctx context.Context, recordID string) ([]domain.MemoryContribution, error) {
	rows, err := d.queryRows(ctx, `
		MATCH (c:Contribution {record_id: $rid})
		RETURN c.id AS id, c.record_id AS record_id, c.source AS source,
		       c.session_id AS session_id, c.execution_id AS execution_id,
		       c.message_seq AS message_seq, c.content_hash AS content_hash,
		       c.created_at AS created_at
		ORDER BY c.created_at ASC`,
		map[string]any{"rid": recordID})
	if err != nil {
		return nil, err
	}
	out := make([]domain.MemoryContribution, 0, len(rows))
	for _, row := range rows {
		c := domain.MemoryContribution{
			RecordID:  asString(row["record_id"]),
			Source:    domain.MemorySource(asString(row["source"])),
			SessionID: asString(row["session_id"]), ExecutionID: asString(row["execution_id"]),
			ContentHash: asString(row["content_hash"]),
		}
		if seq, ok := row["message_seq"].(int64); ok {
			c.MessageSeq = &seq
		}
		c.CreatedAt, _ = asTime(row["created_at"])
		out = append(out, c)
	}
	return out, nil
}

// SetEmbeddings — exported library symbol.
func (d *Driver) SetEmbeddings(ctx context.Context, embeddings []domain.RecordEmbedding) error {
	for _, e := range embeddings {
		if err := d.exec(ctx, `
			MATCH (m:Memory {id: $id}) SET m.embedding = $emb`,
			map[string]any{"id": e.RecordID, "emb": e.Embedding}); err != nil {
			return err
		}
		// Embeddings ride on the record node. (The Mentions rel table in
		// schema.cypher is deliberately unwritten in v1: future graph-side
		// traversals only; record.entities membership is the link today.)
	}
	return nil
}

func asFloats(v any) []float64 {
	switch arr := v.(type) {
	case []float64:
		return arr
	case []any:
		out := make([]float64, 0, len(arr))
		for _, x := range arr {
			out = append(out, asFloat(x))
		}
		return out
	}
	return nil
}
