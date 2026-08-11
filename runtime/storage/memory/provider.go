package memory

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/openfoundry/runtime/internal/uuidv7"
	"github.com/openfoundry/runtime/spi"
)

// Provider is an in-memory StorageProvider. Phase 1 implemented only the
// schema surface; Phase 2 adds object lifecycle (Create/Get/Update/Delete
// Object overridden here; link lifecycle overrides follow in
// provider_link.go). All other SPI methods remain inherited from
// UnimplementedStorageProvider and return ErrUnimplemented.
type Provider struct {
	spi.UnimplementedStorageProvider
	mu      sync.Mutex
	schemas map[int]spi.OntologySchema
	current int
	objects map[string]spi.OntologyObject // type:id -> OntologyObject
	links   map[string]spi.OntologyLink   // type:id -> OntologyLink
	// versionHistory holds per-(type:id) snapshots of an object across
	// create/update/soft-delete, oldest first. Read by GetObjectAtVersion
	// and GetObjectAtTime (Phase 3 U2) and maintained by transaction journal
	// rollback (U7). Links never push history (mirrors TS memory).
	versionHistory map[string][]spi.OntologyObject
}

// New creates an empty memory provider.
func New() *Provider {
	return &Provider{
		schemas:        map[int]spi.OntologySchema{},
		objects:        map[string]spi.OntologyObject{},
		links:          map[string]spi.OntologyLink{},
		versionHistory: map[string][]spi.OntologyObject{},
	}
}

// ApplySchema stores a schema version (idempotent for same version).
func (p *Provider) ApplySchema(_ spi.RequestContext, schema spi.OntologySchema) (spi.MigrationResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	from := p.current
	cloned, err := cloneSchema(schema)
	if err != nil {
		return spi.MigrationResult{}, err
	}
	p.schemas[schema.Version] = cloned
	p.current = schema.Version
	return spi.MigrationResult{
		Success:     true,
		FromVersion: from,
		ToVersion:   schema.Version,
		AppliedAt:   time.Now().UTC(),
	}, nil
}

// GetSchema returns a schema version, or the latest when version is nil.
func (p *Provider) GetSchema(_ spi.RequestContext, version *int) (spi.OntologySchema, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.current
	if version != nil {
		v = *version
	}
	schema, ok := p.schemas[v]
	if !ok {
		return spi.OntologySchema{}, fmt.Errorf("schema version %d not found", v)
	}
	return cloneSchema(schema)
}

// HealthCheck reports a healthy memory provider.
func (p *Provider) HealthCheck() (spi.HealthStatus, error) {
	return spi.HealthStatus{Healthy: true, Provider: "memory", LatencyMs: 0}, nil
}

// Capabilities returns the Phase 3 capability surface. Phase 2 flipped
// all flags false; Phase 3 enables them progressively (R9):
//   - U2: SupportsTemporalQueries (GetObjectAtVersion/GetObjectAtTime)
//   - U4: SupportsFullTextSearch (SearchObjects implemented)
//
// Remaining flags stay false until their units (U5 bulk, U6 graph
// traversal, U7 transactions) land; they will flip in those units.
// SupportsGeoQueries remains false for all of Phase 3.
func (p *Provider) Capabilities() spi.StorageCapabilities {
	return spi.StorageCapabilities{
		SupportsTransactions:    false,
		SupportsTemporalQueries: true,
		SupportsFullTextSearch:  true,
		SupportsGeoQueries:      false,
		SupportsGraphTraversal:  false,
		SupportsBulkMutations:   false,
		MaxTraversalDepth:       0,
		ReplicationSupport:      spi.ReplicationNone,
	}
}

func cloneSchema(s spi.OntologySchema) (spi.OntologySchema, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return spi.OntologySchema{}, err
	}
	var out spi.OntologySchema
	if err := json.Unmarshal(b, &out); err != nil {
		return spi.OntologySchema{}, err
	}
	return out, nil
}

// cloneObject copies an OntologyObject shallow enough that the caller can
// mutate it without affecting the stored copy. Maps are deep-cloned by JSON
// round-trip, matching the cloneSchema convention.
func cloneObject(o spi.OntologyObject) (spi.OntologyObject, error) {
	b, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	var out spi.OntologyObject
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// now stamps the current UTC time as both _createdAt and _updatedAt; called
// when a clone-marshalable time is the cheapest way to remain JSON-safe.
func systemTimestamps() (now any) {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// cloneVersionSnapshot deep-copies an object for versionHistory storage.
// It is the same JSON round-trip as cloneObject, named distinctly so the
// reader sees the path-snapshots and the caller-facing return clones as
// separate responsibilities (history snapshots are never returned unstripped
// to callers; temporal reads re-clone them). Both functions honor the KTD-5
// JSON-clone convention so numbers survive as float64 — callers comparing
// _version must coerce (see objectVersionInt below, added in U2).
func cloneVersionSnapshot(o spi.OntologyObject) (spi.OntologyObject, error) {
	return cloneObject(o)
}

// pushVersionHistoryUnlocked appends a snapshot to the (type:id) history
// slice. Caller MUST hold p.mu. Oldest-first ordering, matching the TS
// memory provider's _versionHistory push order.
func (p *Provider) pushVersionHistoryUnlocked(key string, snap spi.OntologyObject) {
	p.versionHistory[key] = append(p.versionHistory[key], snap)
}

// popVersionHistoryUnlocked removes the most recent snapshot, used by
// transaction rollback (U7) to undo a pushed update. Caller MUST hold p.mu.
// Safe on empty history (rollback of a create also deletes the object, so
// the matching pop is a no-op when history was never pushed for that key).
func (p *Provider) popVersionHistoryUnlocked(key string) {
	s := p.versionHistory[key]
	if len(s) == 0 {
		return
	}
	p.versionHistory[key] = s[:len(s)-1]
}

// CreateObject mints a UUIDv7 _id, stamps system fields, and stores by type:id.
// Engine does per-type validation; the memory provider does not reimplement it.
func (p *Provider) CreateObject(ctx spi.RequestContext, typ string, properties map[string]any) (spi.OntologyObject, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ts := systemTimestamps()
	obj := spi.OntologyObject{
		"_id":        uuidv7.New(),
		"_type":      typ,
		"_tenantId":  ctx.TenantID,
		"_createdAt": ts,
		"_updatedAt": ts,
		"_version":   1,
	}
	for k, v := range properties {
		if isSystemField(k) {
			continue
		}
		obj[k] = v
	}
	key := objectKey(typ, obj["_id"].(string))
	p.objects[key] = obj
	// Phase 3 (U1): stamp authoritative _version:1 on create and push the
	// initial snapshot into versionHistory. Update/soft-delete (U2) advance
	// _version and push further snapshots; links deliberately skip history.
	snap, err := cloneVersionSnapshot(obj)
	if err != nil {
		return nil, err
	}
	p.pushVersionHistoryUnlocked(key, snap)
	out, err := cloneObject(obj)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetObject reads by (type, id). Cross-tenant reads and soft-deleted
// objects both hide as ErrObjectNotFound so the caller cannot probe
// another tenant's ids or distinguish "deleted" from "never was".
// QueryObjects (U4) honors options.IncludeDeleted to read soft-deleted
// entries; this single-point read path always masks. Covers R3, AE2.
func (p *Provider) GetObject(ctx spi.RequestContext, typ, id string) (spi.OntologyObject, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	obj, ok := p.objects[objectKey(typ, id)]
	if !ok || obj["_tenantId"] != ctx.TenantID || obj["_deletedAt"] != nil {
		return nil, fmt.Errorf("%w: %s/%s", spi.ErrObjectNotFound, typ, id)
	}
	return cloneObject(obj)
}

// UpdateObject merges the patch (system fields ignored from the patch,
// and re-stamped at the end) onto the existing object. expectedVersion is
// enforced in Phase 3: a non-nil pointer must equal the stored _version or
// the call rejects with ErrVersionConflict before any write; nil accepts
// any version. On accept, _version increments by 1 and a snapshot is pushed
// into versionHistory (R1, R2). Covers AE1.
func (p *Provider) UpdateObject(ctx spi.RequestContext, typ, id string, properties map[string]any, expectedVersion *int) (spi.OntologyObject, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := objectKey(typ, id)
	existing, ok := p.objects[key]
	if !ok || existing["_tenantId"] != ctx.TenantID {
		return nil, fmt.Errorf("%w: %s/%s", spi.ErrObjectNotFound, typ, id)
	}
	// Conflict check before any write (mirrors TS _doUpdateObject
	// VERSION_CONFLICT throw at memory-storage-provider.ts:372-375). The
	// stored authoritative copy keeps _version as int; objectVersionInt
	// also tolerates float64 in case history snapshots are compared.
	if expectedVersion != nil {
		current := objectVersionInt(existing)
		if current != *expectedVersion {
			return nil, fmt.Errorf("%w: %s/%s expected %d, have %d", spi.ErrVersionConflict, typ, id, *expectedVersion, current)
		}
	}
	merged := spi.OntologyObject{}
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range properties {
		if isSystemField(k) {
			continue
		}
		merged[k] = v
	}
	// Re-stamp system fields with authoritative values. _version advances
	// by 1 on every accepted mutation; the int representation stays the
	// source of truth (JSON clones surface as float64, see Risks table).
	merged["_id"] = existing["_id"]
	merged["_type"] = existing["_type"]
	merged["_tenantId"] = existing["_tenantId"]
	merged["_createdAt"] = existing["_createdAt"]
	merged["_updatedAt"] = systemTimestamps()
	merged["_version"] = objectVersionInt(existing) + 1
	p.objects[key] = merged
	if snap, err := cloneVersionSnapshot(merged); err == nil {
		p.pushVersionHistoryUnlocked(key, snap)
	} else {
		return nil, err
	}
	return cloneObject(merged)
}

// DeleteObject supports hard and soft delete. `mode="soft"` stamps
// _deletedAt, increments _version, re-stamps _updatedAt, and pushes a
// snapshot — the object stays in the map so GetObject (U3) can mask it as
// not-found and QueryObjects (U4) can honor includeDeleted. `mode="hard"`
// removes the entry, idempotent and a no-op across tenants (the original
// tenant retains its object). Any other mode is routed to soft (treated as
// the catch-all non-destructive path), matching the plan's "non-hard is
// soft" leaning. Covers R3 (write side), AE2 (write side).
func (p *Provider) DeleteObject(ctx spi.RequestContext, typ, id, mode string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := objectKey(typ, id)
	existing, ok := p.objects[key]
	if !ok || existing["_tenantId"] != ctx.TenantID {
		// Idempotent: a missing or cross-tenant id is a no-op for both modes.
		return nil
	}
	if mode == "hard" {
		delete(p.objects, key)
		return nil
	}
	// Soft delete: stamp _deletedAt + _version+1, push history, keep the
	// entry. Re-merging from existing preserves all user fields and prior
	// system fields (_createdAt, _tenantId, _id, _type unchanged).
	merged := spi.OntologyObject{}
	for k, v := range existing {
		merged[k] = v
	}
	merged["_updatedAt"] = systemTimestamps()
	merged["_deletedAt"] = systemTimestamps()
	merged["_version"] = objectVersionInt(existing) + 1
	p.objects[key] = merged
	if snap, err := cloneVersionSnapshot(merged); err == nil {
		p.pushVersionHistoryUnlocked(key, snap)
	} else {
		return err
	}
	return nil
}

// GetObjectAtVersion returns the snapshot of (typ, id) at the given version,
// masked to the caller's tenant. Missing key, version, or cross-tenant
// access all surface as ErrObjectNotFound (no leak, matching GetObject).
// Covers R2, AE1.
func (p *Provider) GetObjectAtVersion(ctx spi.RequestContext, typ, id string, version int) (spi.OntologyObject, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := objectKey(typ, id)
	for _, snap := range p.versionHistory[key] {
		if snap["_tenantId"] != ctx.TenantID {
			continue
		}
		if objectVersionInt(snap) == version {
			return cloneObject(snap)
		}
	}
	return nil, fmt.Errorf("%w: %s/%s at version %d", spi.ErrObjectNotFound, typ, id, version)
}

// GetObjectAtTime returns the newest snapshot of (typ, id) whose _updatedAt
// is at or before ts, masked to the caller's tenant. Mirrors TS
// getObjectAtTime (memory-storage-provider.ts:990-1004). Covers R2, AE1.
func (p *Provider) GetObjectAtTime(ctx spi.RequestContext, typ, id string, ts time.Time) (spi.OntologyObject, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := objectKey(typ, id)
	var newest spi.OntologyObject
	var newestTs time.Time
	found := false
	for _, snap := range p.versionHistory[key] {
		if snap["_tenantId"] != ctx.TenantID {
			continue
		}
		s, ok := snap["_updatedAt"].(string)
		if !ok {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			// Unparseable _updatedAt: cannot order — skip per the plan's
			// AsOfTime parse-failure handling (treat as no snapshot).
			continue
		}
		if parsed.After(ts) {
			continue
		}
		if !found || parsed.After(newestTs) {
			newest = snap
			newestTs = parsed
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: %s/%s at or before %s", spi.ErrObjectNotFound, typ, id, ts.Format(time.RFC3339Nano))
	}
	return cloneObject(newest)
}

// objectVersionInt returns the _version of o as an int, tolerating the
// JSON-clone int→float64 widening (Risks table). Returns 0 when _version
// is absent (Phase 1/2 schemas that predate U1, or a malformed map); the
// caller's behavior on 0 is a nil expectedVersion-skip or a conflict on
// any positive expected version, which matches the "no version yet" intent.
func objectVersionInt(o spi.OntologyObject) int {
	switch v := o["_version"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	}
	return 0
}

// objectKey is the canonical map key for stored objects.
func objectKey(typ, id string) string { return typ + ":" + id }

// linkKey is the canonical map key for stored links.
func linkKey(typ, id string) string { return "link:" + typ + ":" + id }

// linkSystemFields is the reserved field set on OntologyLink. Properties
// supplying these are ignored and the authoritative values are stamped.
func linkSystemFields() map[string]bool {
	return map[string]bool{
		"_id":        true,
		"_type":      true,
		"_tenantId":  true,
		"_fromId":    true,
		"_toId":      true,
		"_fromType":  true,
		"_toType":    true,
		"_createdAt": true,
		"_updatedAt": true,
		"_version":   true,
		"_deletedAt": true,
	}
}

// isLinkSystemField reports whether k is a link-reserved field name.
func isLinkSystemField(k string) bool { return linkSystemFields()[k] }

// cloneLink deep-copies an OntologyLink through JSON, matching the
// cloneObject / cloneSchema convention.
func cloneLink(l spi.OntologyLink) (spi.OntologyLink, error) {
	b, err := json.Marshal(l)
	if err != nil {
		return nil, err
	}
	var out spi.OntologyLink
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateLink honors the Engine-supplied _engineLinkId (UUIDv7) when present
// and otherwise mints one internally. _engineLinkId is stripped from the
// stored link's user-facing properties, matching the TS contract
// "Engine generates, SPI stores".
func (p *Provider) CreateLink(ctx spi.RequestContext, typ, fromID, toID string, properties map[string]any) (spi.OntologyLink, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	engineID, _ := properties["_engineLinkId"].(string)
	id := engineID
	if id == "" {
		id = uuidv7.New()
	}
	fromType, toType := p.linkTypeDefinitionUnlocked(typ)
	ts := systemTimestamps()
	link := spi.OntologyLink{
		"_id":        id,
		"_type":      typ,
		"_tenantId":  ctx.TenantID,
		"_fromId":    fromID,
		"_toId":      toID,
		"_fromType":  fromType,
		"_toType":    toType,
		"_createdAt": ts,
		"_updatedAt": ts,
		"_version":   1,
	}
	for k, v := range properties {
		if k == "_engineLinkId" || isLinkSystemField(k) {
			continue
		}
		link[k] = v
	}
	p.links[linkKey(typ, id)] = link
	return cloneLink(link)
}

// linkTypeDefinitionUnlocked is the lock-free inner used by CreateLink,
// which already holds p.mu.
func (p *Provider) linkTypeDefinitionUnlocked(typ string) (fromType, toType string) {
	if p.current == 0 {
		return "unknown", "unknown"
	}
	schema, ok := p.schemas[p.current]
	if !ok {
		return "unknown", "unknown"
	}
	for _, lt := range schema.LinkTypes {
		if lt.Name == typ {
			return lt.FromType, lt.ToType
		}
	}
	return "unknown", "unknown"
}

// GetLink reads a link by (type, id). Cross-tenant reads are masked as
// ErrLinkNotFound so the caller cannot probe another tenant's links.
// Phase 3 does not soft-delete links (TS memory uses hard delete for
// links; R3 explicitly defers link soft delete), but the _deletedAt
// guard is symmetric with GetObject for safety and so that any future
// link soft-delete path becomes a one-line wiring change rather than
// a new mask surface.
func (p *Provider) GetLink(ctx spi.RequestContext, typ, id string) (spi.OntologyLink, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	link, ok := p.links[linkKey(typ, id)]
	if !ok || link["_tenantId"] != ctx.TenantID || link["_deletedAt"] != nil {
		return nil, fmt.Errorf("%w: %s/%s", spi.ErrLinkNotFound, typ, id)
	}
	return cloneLink(link)
}

// DeleteLink removes the link from the map. Idempotent and a no-op across
// tenants, matching the hard-delete semantics on objects.
func (p *Provider) DeleteLink(ctx spi.RequestContext, typ, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := linkKey(typ, id)
	existing, ok := p.links[key]
	if !ok || existing["_tenantId"] != ctx.TenantID {
		return nil
	}
	delete(p.links, key)
	return nil
}

// isSystemField reports whether k is a memory-reserved field name that the
// caller cannot override via properties. Matches the TS memory provider's
// system-field set plus the Phase 2 deferral note for _version/_deletedAt.
func isSystemField(k string) bool {
	switch k {
	case "_id", "_type", "_tenantId", "_createdAt", "_updatedAt", "_version", "_deletedAt":
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Phase 3 U4: filter evaluator + QueryObjects + AggregateObjects + SearchObjects
//
// Mirrors the intent (not the AST) of the TS memory provider at
// packages/storage-memory/src/memory-storage-provider.ts:70-119 (filter
// evaluator), :545-581 (queryObjects), :583-707 (aggregateObjects),
// :709-787 (searchObjects). The FilterExpression is a single self-
// referential struct in Go (spi/ontology.go:121-128) — there are no
// separate FieldPredicate/LogicalPredicate types. The evaluator dispatches
// by filled field: leaf if Field+Operator are non-empty, logical if
// And/Or/Not are populated. KTD-5 (JSON-clone convention) + KTD-8.
// ---------------------------------------------------------------------------

// evaluateFilter returns true if obj matches the single-struct
// FilterExpression under KTD-8 dispatch: a leaf predicate has Field and
// Operator populated; a logical predicate has And/Or/Not populated. An
// empty FilterExpression (no Field, no Operator, no logical children) and
// a nil filter both match everything, mirroring TS `evaluateFilter`
// returning true at its outer default (`return true`).
func evaluateFilter(obj map[string]any, f *spi.FilterExpression) bool {
	if f == nil {
		return true
	}
	// Leaf predicate: Field and Operator both non-empty.
	if f.Field != "" && f.Operator != "" {
		return evaluateFieldPredicate(obj, f)
	}
	// Logical predicate: one of And/Or/Not.
	if len(f.And) > 0 {
		for i := range f.And {
			if !evaluateFilter(obj, &f.And[i]) {
				return false
			}
		}
		return true
	}
	if len(f.Or) > 0 {
		for i := range f.Or {
			if evaluateFilter(obj, &f.Or[i]) {
				return true
			}
		}
		return false
	}
	if f.Not != nil {
		return !evaluateFilter(obj, f.Not)
	}
	// Empty filter matches everything (matches TS default).
	return true
}

// evaluateFieldPredicate evaluates one leaf operator. The TS semantics
// (memory-storage-provider.ts:80-105) are mirrored literally:
//   - eq/neq: value equality (no coercion)
//   - gt/gte/lt/lte: numeric-only, both sides must be numbers
//   - in: pred.Value must be a slice/array containing val
//   - contains/startsWith: string-only, both sides must be strings
//   - exists: pred.Value truthy → must be present-non-null; falsy → must
//     be nil/absent
//   - unknown operator: false
// The TS `===` semantics pair naturally with JSON-clone types —
// float64 vs float64, string vs string, bool vs bool.
func evaluateFieldPredicate(obj map[string]any, f *spi.FilterExpression) bool {
	val, _ := obj[f.Field]
	switch f.Operator {
	case "eq":
		return val == f.Value
	case "neq":
		return val != f.Value
	case "gt", "gte", "lt", "lte":
		a, okA := asFloat(val)
		b, okB := asFloat(f.Value)
		if !okA || !okB {
			return false
		}
		switch f.Operator {
		case "gt":
			return a > b
		case "gte":
			return a >= b
		case "lt":
			return a < b
		case "lte":
			return a <= b
		}
	case "in":
		slice, ok := toAnySlice(f.Value)
		if !ok {
			return false
		}
		for _, item := range slice {
			if item == val {
				return true
			}
		}
		return false
	case "contains":
		s, ok1 := val.(string)
		sub, ok2 := f.Value.(string)
		if !ok1 || !ok2 {
			return false
		}
		return strings.Contains(s, sub)
	case "startsWith":
		s, ok1 := val.(string)
		pre, ok2 := f.Value.(string)
		if !ok1 || !ok2 {
			return false
		}
		return strings.HasPrefix(s, pre)
	case "exists":
		truthy, _ := f.Value.(bool)
		if truthy {
			return val != nil
		}
		return val == nil
	}
	return false
}

// asFloat coerces numeric JSON-clone values to float64. mirroring the TS
// `typeof val === 'number'` gate: int / float64 / int64 all qualify; the
// TS provider stores numbers as float64 via JSON clone, and so do we.
// Non-numbers return ok=false so the caller (a comparison operator)
// evaluates to false, matching TS.
func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// toAnySlice returns the any-element slice for the `in` operator's value.
// JSON-decoded `in` predicates carry []any (Go's encoding/json produces
// []interface{} for arbitrary JSON arrays), which is the wire shape SPI
// consumers send. A nil or non-slice value returns ok=false so the
// evaluator treats it as no-match, mirroring TS Array.isArray gate.
func toAnySlice(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	s, ok := v.([]any)
	return s, ok
}

// QueryObjects reads objects for the caller's tenant and type, applying
// the FilterExpression, OrderBy (multi-key), pagination, and an
// IncludeDeleted default of false. AsOfVersion/AsOfTime are best-effort:
// when populated, the history is the source of snapshots and the live
// map is NOT consulted; otherwise the live objects are scanned. Mirrors
// TS queryObjects (memory-storage-provider.ts:545-581). Covers R5(query),
// AE4.
func (p *Provider) QueryObjects(ctx spi.RequestContext, typ string, filter spi.FilterExpression, options *spi.QueryOptions) (spi.ObjectPage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Snapshot source: AsOf*-populated queries read history only
	limit, offset, orderBy, includeDeleted := 100, 0, []spi.OrderBy{}, false
	if options != nil {
		if options.Limit > 0 {
			limit = options.Limit
		}
		offset = options.Offset
		orderBy = options.OrderBy
		includeDeleted = options.IncludeDeleted
	}

	var candidates []map[string]any
	asOf := options != nil && (options.AsOfVersion != nil || options.AsOfTime != nil)
	if asOf {
		candidates = p.historySnapshotUnlocked(ctx, typ, options)
		// AsOf snapshots carry _deletedAt; for AsOf the includeDeleted
		// semantics are: when false, drop soft-deleted snapshots. Mirror
		// TS note (AsOf variants ignore includeDeleted locally but the
		// plan's best-effort keeps the same default).
	} else {
		for key, obj := range p.objects {
			if obj["_tenantId"] != ctx.TenantID || obj["_type"] != typ {
				continue
			}
			if !includeDeleted && obj["_deletedAt"] != nil {
				continue
			}
			_ = key
			candidates = append(candidates, obj)
		}
	}

	// Filter
	matched := make([]map[string]any, 0, len(candidates))
	for _, obj := range candidates {
		if evaluateFilter(obj, &filter) {
			matched = append(matched, obj)
		}
	}
	totalCount := len(matched)

	// Sort: reverse-iterate OrderBy so multi-key is leftmost-first
	// (mirrors TS `[...orderBy].reverse()`). Comparator: nil sorts last.
	if len(orderBy) > 0 {
		for i := len(orderBy) - 1; i >= 0; i-- {
			s := orderBy[i]
			desc := strings.EqualFold(s.Direction, "desc")
			cmp := s.Field
			_ = cmp
			sortSliceStable(matched, sortKey{field: s.Field, desc: desc})
		}
	}

	// Pagination — enforce MAX_QUERY_LIMIT to prevent unbounded scans.
	const maxQueryLimit = 1000
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}
	if offset > len(matched) {
		offset = len(matched)
	}
	end := offset + limit
	if end > len(matched) {
		end = len(matched)
	}
	sliced := matched[offset:end]

	items := make([]spi.OntologyObject, 0, len(sliced))
	for _, obj := range sliced {
		c, err := cloneObject(obj)
		if err != nil {
			return spi.ObjectPage{}, err
		}
		items = append(items, c)
	}
	return spi.ObjectPage{
		Items:       items,
		TotalCount:  totalCount,
		HasNextPage: offset+limit < totalCount,
	}, nil
}

// sortSliceStable runs sort.SliceStable with the given sortKey. Nil values
// always sort after non-nil regardless of asc/desc, matching the TS
// comparator at memory-storage-provider.ts:562-563.
func sortSliceStable(items []map[string]any, key sortKey) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i][key.field], items[j][key.field]
		if a == b {
			return false
		}
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		cmp := compareLess(a, b)
		if key.desc {
			return !cmp
		}
		return cmp
	})
}

type sortKey struct {
	field string
	desc  bool
}

// compareLess returns true if a < b for the JSON-clone types the memory
// provider stores: numbers (coerced to float64), strings (lexicographic),
// bools (false < true). Mixed-type comparisons return false (the caller's
// equality check has already handled the a == b case). This is the Go
// translation of the TS `aVal < bVal ? -1 : 1` block; Go's `==` on
// mixed any types is type-aware (different types are unequal).
func compareLess(a, b any) bool {
	if af, ok := asFloat(a); ok {
		if bf, ok := asFloat(b); ok {
			return af < bf
		}
	}
	if as, ok1 := a.(string); ok1 {
		if bs, ok2 := b.(string); ok2 {
			return as < bs
		}
	}
	if ab, ok1 := a.(bool); ok1 {
		if bb, ok2 := b.(bool); ok2 {
			return !ab && bb
		}
	}
	return false
}

// historySnapshotUnlocked returns the AsOf-history snapshots for (typ,*)
// in the caller's tenant. AsOfVersion selects the snapshot at exactly that
// version for every object; AsOfTime selects the newest at-or-before the
// given time. The live map is NOT consulted — these reads see the world
// as it was at the supplied marker. Caller MUST hold p.mu.
func (p *Provider) historySnapshotUnlocked(ctx spi.RequestContext, typ string, options *spi.QueryOptions) []map[string]any {
	var out []map[string]any
	for key, hist := range p.versionHistory {
		_ = key
		// nearest-by-version / newest-by-time per object
		var picked map[string]any
		typMatch := false
		if options.AsOfVersion != nil {
			for _, snap := range hist {
				if snap["_tenantId"] != ctx.TenantID {
					continue
				}
				if snap["_type"] != typ {
					continue
				}
				typMatch = true
				if objectVersionInt(snap) == *options.AsOfVersion {
					picked = snap
					break
				}
			}
		} else if options.AsOfTime != nil {
			var newestTs time.Time
			found := false
			for _, snap := range hist {
				if snap["_tenantId"] != ctx.TenantID {
					continue
				}
				if snap["_type"] != typ {
					continue
				}
				typMatch = true
				tsStr, ok := snap["_updatedAt"].(string)
				if !ok {
					continue
				}
				ts, err := time.Parse(time.RFC3339Nano, tsStr)
				if err != nil || ts.After(*options.AsOfTime) {
					continue
				}
				if !found || ts.After(newestTs) {
					picked = snap
					newestTs = ts
					found = true
				}
			}
		}
		if picked != nil {
			out = append(out, picked)
		} else if typMatch {
			// AsOf history may legitimately exclude an object whose
			// snapshot at that marker does not exist — mirror TS note
			// "no snapshot matches" by omitting (intentionally empty).
		}
	}
	return out
}

// AggregateObjects runs groupBy aggregations over non-soft-deleted,
// tenant-scoped, type-matched objects. count/sum/avg/min/max. The TS
// provider always excludes _deletedAt (no includeDeleted opport) —
// mirrored. The function list is validated up-front so an invalid
// function throws even when zero rows match (mirrors TS
// memory-storage-provider.ts:589-594). Covers R5(aggregate), AE5.
func (p *Provider) AggregateObjects(ctx spi.RequestContext, typ string, query spi.AggregateQuery) (spi.AggregateResult, error) {
	if len(query.Fields) == 0 {
		return spi.AggregateResult{}, fmt.Errorf("aggregate query must specify at least one field")
	}
	// Validate aggregate functions up-front (before grouping) so invalid
	// fns throw even with zero rows — mirrors TS ALLOWED_FNS gate.
	allowedFns := map[string]bool{"count": true, "sum": true, "avg": true, "min": true, "max": true}
	for _, aggField := range query.Fields {
		if !allowedFns[strings.ToLower(aggField.Fn)] {
			return spi.AggregateResult{}, fmt.Errorf("invalid aggregate function: %s", aggField.Fn)
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// 1. Collect matching objects (tenant-scoped, type-matched, non-deleted).
	var items []map[string]any
	for _, obj := range p.objects {
		if obj["_tenantId"] != ctx.TenantID || obj["_type"] != typ {
			continue
		}
		if obj["_deletedAt"] != nil {
			continue
		}
		if query.Filter != nil && !evaluateFilter(obj, query.Filter) {
			continue
		}
		items = append(items, obj)
	}

	// 2. Group by groupBy fields. JSON-serialised group key, mirroring TS.
	groupMap := map[string][]map[string]any{}
	groupKeyMap := map[string]map[string]any{}
	groupOrder := []string{}
	for _, obj := range items {
		keys := map[string]any{}
		for _, field := range query.GroupBy {
			v, ok := obj[field]
			if !ok {
				keys[field] = nil
			} else {
				keys[field] = v
			}
		}
		gk := groupKeyJSON(keys)
		if _, exists := groupMap[gk]; !exists {
			groupMap[gk] = []map[string]any{}
			groupKeyMap[gk] = keys
			groupOrder = append(groupOrder, gk)
		}
		groupMap[gk] = append(groupMap[gk], obj)
	}

	// No groupBy with zero matches → single empty group, matching TS.
	if len(groupOrder) == 0 {
		gk := "{}"
		groupMap[gk] = []map[string]any{}
		groupKeyMap[gk] = map[string]any{}
		groupOrder = append(groupOrder, gk)
	}

	// 3. Compute aggregates per group.
	groups := make([]spi.AggregateGroup, 0, len(groupOrder))
	for _, gk := range groupOrder {
		groupItems := groupMap[gk]
		keys := groupKeyMap[gk]
		values := map[string]any{}
		for _, aggField := range query.Fields {
			fnLower := strings.ToLower(aggField.Fn)
			alias := aggField.Alias
			if alias == "" {
				alias = aggField.Fn + "_" + aggField.Field
			}
			if fnLower == "count" {
				if aggField.Field == "*" {
					values[alias] = len(groupItems)
				} else {
					count := 0
					for _, item := range groupItems {
						v, ok := item[aggField.Field]
						if ok && v != nil {
							count++
						}
					}
					values[alias] = count
				}
				continue
			}
			// sum, avg, min, max — only on numeric (non-nil) values.
			var nums []float64
			for _, item := range groupItems {
				v, ok := item[aggField.Field]
				if !ok || v == nil {
					continue
				}
				if fv, ok := asFloat(v); ok {
					nums = append(nums, fv)
				}
			}
			if len(nums) == 0 {
				values[alias] = nil
				continue
			}
			switch fnLower {
			case "sum":
				s := 0.0
				for _, v := range nums {
					s += v
				}
				values[alias] = s
			case "avg":
				s := 0.0
				for _, v := range nums {
					s += v
				}
				values[alias] = s / float64(len(nums))
			case "min":
				m := nums[0]
				for _, v := range nums[1:] {
					if v < m {
						m = v
					}
				}
				values[alias] = m
			case "max":
				m := nums[0]
				for _, v := range nums[1:] {
					if v > m {
						m = v
					}
				}
				values[alias] = m
			}
		}
		groups = append(groups, spi.AggregateGroup{Keys: keys, Values: values})
	}

	// 4. Apply ordering (group keys or aggregate values).
	if len(query.OrderBy) > 0 {
		for i := len(query.OrderBy) - 1; i >= 0; i-- {
			s := query.OrderBy[i]
			desc := strings.EqualFold(s.Direction, "desc")
			sortGroupStable(groups, sortKey{field: s.Field, desc: desc})
		}
	}
	totalGroups := len(groups)

	// 5. Apply limit/offset.
	offset := query.Offset
	if offset > len(groups) {
		offset = len(groups)
	}
	limit := query.Limit
	if limit <= 0 {
		limit = len(groups)
	}
	end := offset + limit
	if end > len(groups) {
		end = len(groups)
	}
	groups = groups[offset:end]

	return spi.AggregateResult{Groups: groups, TotalGroups: totalGroups}, nil
}

// sortGroupStable sorts AggregateGroup entries by either group key or
// aggregate value at the given field, with nil always sorting last.
func sortGroupStable(groups []spi.AggregateGroup, key sortKey) {
	sort.SliceStable(groups, func(i, j int) bool {
		av, aok := groups[i].Keys[key.field]
		if !aok {
			av = groups[i].Values[key.field]
		}
		bv, bok := groups[j].Keys[key.field]
		if !bok {
			bv = groups[j].Values[key.field]
		}
		if av == bv {
			return false
		}
		if av == nil {
			return false
		}
		if bv == nil {
			return true
		}
		cmp := compareLess(av, bv)
		if key.desc {
			return !cmp
		}
		return cmp
	})
}

// groupKeyJSON is a stable JSON encoding of a group-key map. It sorts
// the keys before encoding so two structurally identical maps produce
// the same string regardless of map iteration order.
func groupKeyJSON(m map[string]any) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range ks {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('"')
		sb.WriteString(k)
		sb.WriteString("\":")
		b, _ := json.Marshal(m[k])
		sb.Write(b)
	}
	sb.WriteByte('}')
	return sb.String()
}

// SearchObjects tokenises the query (lowercased whitespace split), scores
// each tenant-scoped type-matched non-soft-deleted candidate by counting
// token occurrences across the default (non-_, string-valued) fields or
// the user-supplied Fields, and ranks by score descending. Highlights
// push the entire field value (mirrors TS). Blank/whitespace-only query
// returns an empty result. Always excludes soft-deleted (no includeDeleted
// opport — TS has none). Covers R5(search), AE5.
func (p *Provider) SearchObjects(ctx spi.RequestContext, typ string, query spi.SearchQuery) (spi.SearchResult, error) {
	// Blank / whitespace-only query → empty (mirrors TS line 711-713).
	if strings.TrimSpace(query.Query) == "" {
		return spi.SearchResult{Hits: []spi.SearchHit{}, TotalCount: 0, HasNextPage: false}, nil
	}
	queryLower := strings.ToLower(query.Query)
	terms := strings.Fields(strings.TrimSpace(queryLower))

	p.mu.Lock()
	defer p.mu.Unlock()

	// Candidate set: tenant + type + non-soft-deleted.
	var candidates []map[string]any
	for _, obj := range p.objects {
		if obj["_tenantId"] != ctx.TenantID || obj["_type"] != typ {
			continue
		}
		if obj["_deletedAt"] != nil {
			continue
		}
		candidates = append(candidates, obj)
	}

	type scored struct {
		hit        spi.SearchHit
		idx        int // preserve insertion order for tie-stable sort
	}
	scoredHits := make([]scored, 0, len(candidates))
	for i, obj := range candidates {
		// Default fields = non-_, string-valued. User-supplied fields
		// override (even if they are _-prefixed — only the user can ask
		// for system fields explicitly).
		var searchFields []string
		if len(query.Fields) > 0 {
			searchFields = query.Fields
		} else {
			for k, v := range obj {
				if strings.HasPrefix(k, "_") {
					continue
				}
				if _, ok := v.(string); ok {
					searchFields = append(searchFields, k)
				}
			}
		}

		score := 0
		highlights := map[string][]string{}
		for _, field := range searchFields {
			val, ok := obj[field]
			if !ok {
				continue
			}
			s, ok := val.(string)
			if !ok {
				continue
			}
			valLower := strings.ToLower(s)
			for _, term := range terms {
				idx := 0
				count := 0
				for {
					pos := strings.Index(valLower[idx:], term)
					if pos < 0 {
						break
					}
					count++
					idx += pos + len(term)
					if idx > len(valLower) {
						break
					}
				}
				if count > 0 {
					score += count
					if _, exists := highlights[field]; !exists {
						highlights[field] = []string{}
					}
					// TS pushes the entire field value, not a snippet.
					highlights[field] = append(highlights[field], s)
				}
			}
		}

		if score == 0 {
			continue
		}
		if query.Filter != nil && !evaluateFilter(obj, query.Filter) {
			continue
		}

		objClone, err := cloneObject(obj)
		if err != nil {
			return spi.SearchResult{}, err
		}
		var hlOut map[string][]string
		if len(highlights) > 0 {
			hlOut = highlights
		}
		scoredHits = append(scoredHits, scored{
			hit: spi.SearchHit{Object: objClone, Score: float64(score), Highlights: hlOut},
			idx: i,
		})
	}

	// Sort by score descending; ties keep pre-scored order (stable).
	sort.SliceStable(scoredHits, func(i, j int) bool {
		if scoredHits[i].hit.Score != scoredHits[j].hit.Score {
			return scoredHits[i].hit.Score > scoredHits[j].hit.Score
		}
		return scoredHits[i].idx < scoredHits[j].idx
	})

	totalCount := len(scoredHits)
	offset := query.Offset
	if offset > totalCount {
		offset = totalCount
	}
	limit := query.Limit
	if limit <= 0 {
		limit = totalCount
	}
	end := offset + limit
	if end > totalCount {
		end = totalCount
	}
	hits := make([]spi.SearchHit, 0, end-offset)
	for i := offset; i < end; i++ {
		hits = append(hits, scoredHits[i].hit)
	}
	return spi.SearchResult{
		Hits:        hits,
		TotalCount:  totalCount,
		HasNextPage: offset+limit < totalCount,
	}, nil
}
