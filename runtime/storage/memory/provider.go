package memory

import (
	"encoding/json"
	"fmt"
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
//
// Remaining flags stay false until their units (U4 search, U5 bulk, U6
// graph traversal, U7 transactions) land; they will flip in those units.
// SupportsGeoQueries remains false for all of Phase 3.
func (p *Provider) Capabilities() spi.StorageCapabilities {
	return spi.StorageCapabilities{
		SupportsTransactions:    false,
		SupportsTemporalQueries: true,
		SupportsFullTextSearch:  false,
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
