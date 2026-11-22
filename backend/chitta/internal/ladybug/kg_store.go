package ladybug

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"zbot/backend/internal/textutil"

	"zbot/backend/internal/domain"
)

// --- knowledge graph ----------------------------------------------------------

var entityColumns = entityCols("e")

// entityColsPrefixed aliases every column with an ent_ prefix so entity
// and relationship columns never collide in one row map.
func entityColsPrefixed(a string) string {
	return a + `.id AS ent_id, ` + a + `.type AS ent_type, ` + a + `.name AS ent_name,
	` + a + `.normalized_name AS ent_normalized_name, ` + a + `.aliases AS ent_aliases,
	` + a + `.confidence AS ent_confidence, ` + a + `.mention_count AS ent_mention_count,
	` + a + `.last_mentioned_at AS ent_last_mentioned_at`
}

func rowToEntityPrefixed(row map[string]any) domain.Entity {
	prefixed := map[string]any{}
	for k, v := range row {
		if strings.HasPrefix(k, "ent_") {
			prefixed[k[4:]] = v
		}
	}
	return rowToEntity(prefixed)
}

// entityCols builds the entity column list for a given node alias.
func entityCols(a string) string {
	return a + `.id AS id, ` + a + `.type AS type, ` + a + `.name AS name,
	` + a + `.normalized_name AS normalized_name, ` + a + `.aliases AS aliases,
	` + a + `.confidence AS confidence, ` + a + `.mention_count AS mention_count,
	` + a + `.last_mentioned_at AS last_mentioned_at`
}

func rowToEntity(row map[string]any) domain.Entity {
	e := domain.Entity{
		ID: asString(row["id"]), Type: domain.EntityType(asString(row["type"])),
		Name: asString(row["name"]), NormalizedName: asString(row["normalized_name"]),
		Aliases: asStrings(row["aliases"]), Confidence: asFloat(row["confidence"]),
		MentionCount: asInt64(row["mention_count"]),
	}
	if t, ok := asTime(row["last_mentioned_at"]); ok {
		e.LastMentionedAt = &t
	}
	return e
}

// UpsertEntityByName — UpsertEntityByName creates or updates an entity keyed by its canonical name.
func (d *Driver) UpsertEntityByName(ctx context.Context, name string, entityType domain.EntityType, aliases []string, confidence *float64, now time.Time) (domain.Entity, error) {
	norm := textutil.NormalizeText(name)
	existing, ok, err := d.ResolveEntity(ctx, norm)
	if err != nil {
		return domain.Entity{}, err
	}
	if ok {
		for _, a := range aliases {
			an := textutil.NormalizeText(a)
			if an != "" && an != norm {
				existing.Aliases = appendUniqueStr(existing.Aliases, an)
			}
		}
		existing.MentionCount++
		existing.LastMentionedAt = &now
		// Only SET aliases when non-empty (the engine cannot bind empty
		// LIST values — review round 2: aliasless repeats failed here).
		if len(existing.Aliases) > 0 {
			if err := d.exec(ctx, `
				MATCH (e:Entity {id: $id})
				SET e.aliases = $aliases, e.mention_count = $mc, e.last_mentioned_at = $lm`,
				map[string]any{"id": existing.ID, "aliases": existing.Aliases,
					"mc": existing.MentionCount, "lm": now}); err != nil {
				return domain.Entity{}, err
			}
		} else {
			if err := d.exec(ctx, `
				MATCH (e:Entity {id: $id})
				SET e.mention_count = $mc, e.last_mentioned_at = $lm`,
				map[string]any{"id": existing.ID, "mc": existing.MentionCount, "lm": now}); err != nil {
				return domain.Entity{}, err
			}
		}
		return existing, nil
	}
	if entityType == "" {
		entityType = domain.EntityOther
	}
	conf := 0.5
	if confidence != nil {
		conf = domain.Clamp01(*confidence)
	}
	// Ladybug INTERNAL_ID strings are not stable across restarts; the
	// driver derives a deterministic, collision-safe id from a hash of
	// the normalized name (any name maps to exactly one id).
	id := "ent_" + hashHex(norm, 20)
	var al []string
	for _, a := range aliases {
		an := textutil.NormalizeText(a)
		if an != "" && an != norm {
			al = appendUniqueStr(al, an)
		}
	}
	if len(al) == 0 {
		al = nil // Ladybug cannot bind empty LIST values
	}
	if err := d.exec(ctx, `
		CREATE (e:Entity {
			id: $id, type: $type, name: $name, normalized_name: $norm,
			confidence: $confidence,
			mention_count: 1, last_mentioned_at: $lm
		})`, map[string]any{
		"id": id, "type": string(entityType), "name": name, "norm": norm,
		"confidence": conf, "lm": now,
	}); err != nil {
		return domain.Entity{}, err
	}
	// Ladybug cannot bind empty LIST values: SET aliases only when non-empty.
	if len(al) > 0 {
		if err := d.exec(ctx, "MATCH (e:Entity {id: $id}) SET e.aliases = $aliases",
			map[string]any{"id": id, "aliases": al}); err != nil {
			return domain.Entity{}, err
		}
	}
	return domain.Entity{
		ID: id, Type: entityType, Name: name, NormalizedName: norm,
		Aliases: al, Confidence: conf, MentionCount: 1, LastMentionedAt: &now,
	}, nil
}

func (d *Driver) entityByNorm(ctx context.Context, norm string) (domain.Entity, bool, error) {
	rows, err := d.queryRows(ctx,
		"MATCH (e:Entity {normalized_name: $norm}) RETURN "+entityColumns,
		map[string]any{"norm": norm})
	if err != nil {
		return domain.Entity{}, false, err
	}
	if len(rows) == 0 {
		return domain.Entity{}, false, nil
	}
	return rowToEntity(rows[0]), true, nil
}

// ResolveEntity — ResolveEntity resolves an entity reference to its canonical entity, if known.
func (d *Driver) ResolveEntity(ctx context.Context, ref string) (domain.Entity, bool, error) {
	// 1. Exact chitta id.
	rows, err := d.queryRows(ctx, "MATCH (e:Entity {id: $id}) RETURN "+entityColumns, map[string]any{"id": ref})
	if err != nil {
		return domain.Entity{}, false, err
	}
	if len(rows) > 0 {
		return rowToEntity(rows[0]), true, nil
	}
	// 2. Normalized name.
	norm := textutil.NormalizeText(ref)
	if e, ok, err := d.entityByNorm(ctx, norm); err != nil || ok {
		return e, ok, err
	}
	// 3. Alias (normalized).
	rows, err = d.queryRows(ctx, `
		MATCH (e:Entity) WHERE $ref IN e.aliases
		RETURN `+entityColumns+" LIMIT 1", map[string]any{"ref": norm})
	if err != nil {
		return domain.Entity{}, false, err
	}
	if len(rows) == 0 {
		return domain.Entity{}, false, nil
	}
	return rowToEntity(rows[0]), true, nil
}

// SearchEntities — exported library symbol.
func (d *Driver) SearchEntities(ctx context.Context, req domain.EntitySearchRequest) (domain.Page[domain.Entity], error) {
	prefix := textutil.NormalizeText(req.Prefix)
	limit := req.Limit
	if limit <= 0 {
		limit = domain.RecallDefaultLimit
	}
	typeFilter := ""
	args := map[string]any{"lim": limit + 1}
	if req.Type != "" {
		typeFilter = " AND e.type = $type"
		args["type"] = string(req.Type)
	}
	// Rank 1: name-prefix matches (empty prefix matches all names).
	fetch := func(where string) ([]domain.Entity, error) {
		a := map[string]any{}
		for k, v := range args {
			a[k] = v
		}
		if prefix != "" {
			a["prefix"] = prefix
		}
		rows, err := d.queryRows(ctx, `
			MATCH (e:Entity) WHERE `+where+typeFilter+`
			RETURN `+entityColumns+`
			ORDER BY e.name ASC, e.id ASC LIMIT $lim`, a)
		if err != nil {
			return nil, err
		}
		out := make([]domain.Entity, 0, len(rows))
		for _, row := range rows {
			out = append(out, rowToEntity(row))
		}
		return out, nil
	}
	nameWhere := "1 = 1"
	if prefix != "" {
		nameWhere = "e.normalized_name STARTS WITH $prefix"
	}
	byName, err := fetch(nameWhere)
	if err != nil {
		return domain.Page[domain.Entity]{}, err
	}
	// Rank 2: alias-prefix matches — fetched separately and filtered in
	// Go (the engine has no list-comprehension prefix predicate; v1
	// bounds the scan, personal-scale graphs only).
	var byAlias []domain.Entity
	if prefix != "" {
		aliasRows, err := d.queryRows(ctx, `
			MATCH (e:Entity) WHERE size(e.aliases) > 0`+typeFilter+`
			RETURN `+entityColumns+`
			ORDER BY e.name ASC LIMIT 1000`, args)
		if err != nil {
			return domain.Page[domain.Entity]{}, err
		}
		for _, row := range aliasRows {
			e := rowToEntity(row)
			for _, a := range e.Aliases {
				if len(a) >= len(prefix) && a[:len(prefix)] == prefix {
					byAlias = append(byAlias, e)
					break
				}
			}
		}
	}
	items := dedupeEntities(append(byName, byAlias...))
	if len(items) > limit {
		items = items[:limit]
	}
	return domain.Page[domain.Entity]{Items: items}, nil
}

func dedupeEntities(list []domain.Entity) []domain.Entity {
	seen := map[string]bool{}
	out := list[:0]
	for _, e := range list {
		if !seen[e.ID] {
			seen[e.ID] = true
			out = append(out, e)
		}
	}
	return out
}

const relColumns = `r.id AS rel_id, r.type AS type, r.confidence AS confidence,
	r.source AS source, r.session_id AS session_id,
	r.execution_id AS execution_id, r.agent_id AS agent_id,
	r.created_at AS created_at,
	a.id AS from_id, b.id AS to_id`

func rowToRelationship(row map[string]any) domain.Relationship {
	rel := domain.Relationship{
		ID: asString(row["rel_id"]), From: asString(row["from_id"]), To: asString(row["to_id"]),
		Type: asString(row["type"]), Confidence: asFloat(row["confidence"]),
		Provenance: domain.MemoryProvenance{
			Source:    domain.MemorySource(asString(row["source"])),
			SessionID: asString(row["session_id"]), ExecutionID: asString(row["execution_id"]),
			AgentID: asString(row["agent_id"]),
		},
	}
	rel.CreatedAt, _ = asTime(row["created_at"])
	return rel
}

// FindRelationship — exported library symbol.
func (d *Driver) FindRelationship(ctx context.Context, from, to, relType string) (domain.Relationship, bool, error) {
	rows, err := d.queryRows(ctx, `
		MATCH (a:Entity {id: $from})-[r:Relates {type: $type}]->(b:Entity {id: $to})
		RETURN `+relColumns+" LIMIT 1",
		map[string]any{"from": from, "to": to, "type": relType})
	if err != nil {
		return domain.Relationship{}, false, err
	}
	if len(rows) == 0 {
		return domain.Relationship{}, false, nil
	}
	return rowToRelationship(rows[0]), true, nil
}

// UpdateRelationshipConfidence persists a merge bump on one edge.
func (d *Driver) UpdateRelationshipConfidence(ctx context.Context, id string, confidence float64) error {
	return d.exec(ctx, "MATCH ()-[r:Relates {id: $id}]->() SET r.confidence = $confidence",
		map[string]any{"id": id, "confidence": confidence})
}

// CreateRelationship — exported library symbol.
func (d *Driver) CreateRelationship(ctx context.Context, rel *domain.Relationship) error {
	// The service layer (Connect) is the idempotency owner: it checks
	// (from, to, type) and bumps confidence. The driver only creates.
	return d.exec(ctx, `
		MATCH (a:Entity {id: $from}), (b:Entity {id: $to})
		CREATE (a)-[:Relates {
			id: $id, type: $type, confidence: $confidence,
			source: $source, session_id: $session_id,
			execution_id: $execution_id, agent_id: $agent_id,
			created_at: $created_at
		}]->(b)`, map[string]any{
		"from": rel.From, "to": rel.To, "id": rel.ID, "type": rel.Type,
		"confidence": rel.Confidence, "source": string(rel.Provenance.Source),
		"session_id": rel.Provenance.SessionID, "execution_id": rel.Provenance.ExecutionID,
		"agent_id": rel.Provenance.AgentID, "created_at": rel.CreatedAt,
	})
}

// Neighborhood — exported library symbol.
func (d *Driver) Neighborhood(ctx context.Context, entityID string, depth int, types []string, limit int) (domain.Subgraph, error) {
	sub := domain.Subgraph{Nodes: []domain.Entity{}, Edges: []domain.Relationship{}}
	seenNodes := map[string]bool{entityID: true}
	seenEdges := map[string]bool{}
	frontier := []string{entityID}
	typeFilter := ""
	argsBase := map[string]any{}
	if len(types) > 0 {
		typeFilter = " AND r.type IN $types"
		argsBase["types"] = types
	}
	for hop := 0; hop < depth && len(frontier) > 0; hop++ {
		var next []string
		for _, node := range frontier {
			args := map[string]any{"id": node}
			for k, v := range argsBase {
				args[k] = v
			}
			// rel columns carry rel_id/from_id/to_id; the OTHER endpoint's
			// entity columns carry ent_* aliases (no key collisions).
			rows, err := d.queryRows(ctx, `
				MATCH (a:Entity {id: $id})-[r:Relates]->(b:Entity)
				WHERE 1 = 1`+typeFilter+`
				RETURN `+relColumns+", "+entityColsPrefixed("b"), args)
			if err != nil {
				return sub, err
			}
			rowsRev, err := d.queryRows(ctx, `
				MATCH (a:Entity)-[r:Relates]->(b:Entity {id: $id})
				WHERE 1 = 1`+typeFilter+`
				RETURN `+relColumns+", "+entityColsPrefixed("a"), args)
			if err != nil {
				return sub, err
			}
			for _, row := range append(rows, rowsRev...) {
				other := asString(row["from_id"])
				if other == node {
					other = asString(row["to_id"])
				}
				rel := rowToRelationship(row)
				if !seenEdges[rel.ID] {
					seenEdges[rel.ID] = true
					sub.Edges = append(sub.Edges, rel)
				}
				if seenNodes[other] {
					continue
				}
				seenNodes[other] = true
				sub.Nodes = append(sub.Nodes, rowToEntityPrefixed(row))
				next = append(next, other)
				if len(sub.Nodes) >= limit {
					return sub, nil
				}
			}
		}
		frontier = next
	}
	return sub, nil
}

func hashHex(s string, n int) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:n]
}

func appendUniqueStr(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
