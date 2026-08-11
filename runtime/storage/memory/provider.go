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
	mu       sync.Mutex
	schemas  map[int]spi.OntologySchema
	current  int
	objects  map[string]spi.OntologyObject // type:id -> OntologyObject
}

// New creates an empty memory provider.
func New() *Provider {
	return &Provider{
		schemas: map[int]spi.OntologySchema{},
		objects: map[string]spi.OntologyObject{},
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

// Capabilities returns Phase 1 capabilities (schema only).
func (p *Provider) Capabilities() spi.StorageCapabilities {
	return spi.StorageCapabilities{
		SupportsTransactions:   false,
		SupportsTemporalQueries: false,
		SupportsFullTextSearch: false,
		SupportsGeoQueries:     false,
		SupportsGraphTraversal: false,
		SupportsBulkMutations:  false,
		MaxTraversalDepth:      0,
		ReplicationSupport:     spi.ReplicationNone,
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
	}
	for k, v := range properties {
		if isSystemField(k) {
			continue
		}
		obj[k] = v
	}
	p.objects[objectKey(typ, obj["_id"].(string))] = obj
	out, err := cloneObject(obj)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetObject reads by (type, id). Cross-tenant reads hide as
// ErrObjectNotFound so the caller cannot probe another tenant's ids.
func (p *Provider) GetObject(ctx spi.RequestContext, typ, id string) (spi.OntologyObject, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	obj, ok := p.objects[objectKey(typ, id)]
	if !ok || obj["_tenantId"] != ctx.TenantID {
		return nil, fmt.Errorf("%w: %s/%s", spi.ErrObjectNotFound, typ, id)
	}
	return cloneObject(obj)
}

// UpdateObject merges the patch (system fields ignored from the patch,
// and re-stamped at the end) onto the existing object. expectedVersion is
// intentionally ignored in Phase 2 (versioning deferred to Phase 3).
func (p *Provider) UpdateObject(ctx spi.RequestContext, typ, id string, properties map[string]any, _ *int) (spi.OntologyObject, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := objectKey(typ, id)
	existing, ok := p.objects[key]
	if !ok || existing["_tenantId"] != ctx.TenantID {
		return nil, fmt.Errorf("%w: %s/%s", spi.ErrObjectNotFound, typ, id)
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
	// Re-stamp system fields with authoritative values.
	merged["_id"] = existing["_id"]
	merged["_type"] = existing["_type"]
	merged["_tenantId"] = existing["_tenantId"]
	merged["_createdAt"] = existing["_createdAt"]
	merged["_updatedAt"] = systemTimestamps()
	p.objects[key] = merged
	return cloneObject(merged)
}

// DeleteObject supports hard delete only in Phase 2. `mode="soft"`
// returns ErrUnimplemented so the soft-delete path stays explicit. Hard
// delete is idempotent and a no-op across tenants (the original tenant
// retains its object).
func (p *Provider) DeleteObject(ctx spi.RequestContext, typ, id, mode string) error {
	if mode != "hard" {
		return fmt.Errorf("%w: DeleteObject mode %q not supported in Phase 2 (hard only)", spi.ErrUnimplemented, mode)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	key := objectKey(typ, id)
	existing, ok := p.objects[key]
	if !ok || existing["_tenantId"] != ctx.TenantID {
		// Idempotent: a missing or cross-tenant id is a no-op.
		return nil
	}
	delete(p.objects, key)
	return nil
}

// objectKey is the canonical map key for stored objects.
func objectKey(typ, id string) string { return typ + ":" + id }

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
