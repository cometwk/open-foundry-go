// Package engine orchestrates object and link lifecycle over a
// StorageProvider. It owns the binding-intent mirrors of the TypeScript
// ObjectManager / LinkManager: validate against the IR TBox, call SPI
// storage, and (in later phases) emit CloudEvents on state changes.
//
// Phase 2 surfaces the six atomic verbs Create/Get/Update/Delete Object
// and Create/Delete Link. Versioning, soft-delete, history, events,
// query, and transactions are deferred to Phase 3 and beyond.
//
// The Engine depends on the spi.StorageProvider interface only; the
// concrete storage backend is injected at construction. This protects
// the layer boundary between engine and storage (mirrors the TS
// dependency direction in packages/engine — storage-memory is a
// devDependency only).
package engine

import (
	"fmt"

	"github.com/openfoundry/runtime/internal/uuidv7"
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
)

// Engine orchestrates object/link lifecycle verbs over a StorageProvider.
// The TBox is validated once at construction; per-verb runtime checks
// then run against the held *ir.Ontology.
type Engine struct {
	storage  spi.StorageProvider
	ontology *ir.Ontology
}

// New constructs an Engine bound to the given storage provider and TBox.
// ir.Validate is called once up front; construction fails if the TBox
// is semantically invalid — the Engine never runs verbs against an
// IR it cannot trust.
func New(storage spi.StorageProvider, ontology *ir.Ontology) (*Engine, error) {
	if storage == nil {
		return nil, fmt.Errorf("engine: storage provider must be non-nil")
	}
	if ontology == nil {
		return nil, fmt.Errorf("engine: ontology must be non-nil")
	}
	if err := ir.Validate(ontology); err != nil {
		return nil, fmt.Errorf("engine: ontology validation failed: %w", err)
	}
	return &Engine{storage: storage, ontology: ontology}, nil
}

// CreateObject validates the payload against the held TBox and writes a
// new object through the SPI. Validation is binding-intent, not a port of
// TS validateObjectProperties: the Engine checks the type exists in the
// IR, no role-restricted field (Primary/LinkNav/Computed) appears in the
// payload, scalar properties carry a runtime value matching ir.TypeRef,
// and required (NonNull) fields without a default are present. TS-only
// concerns (enum membership, uniqueness probes, @constraint evaluation,
// immutable-on-patch) stay deferred to Phase 3.
func (e *Engine) CreateObject(ctx spi.RequestContext, typ string, properties map[string]any) (spi.OntologyObject, error) {
	if err := e.validateObjectPayload(typ, properties, false); err != nil {
		return nil, err
	}
	obj, err := e.storage.CreateObject(ctx, typ, properties)
	if err != nil {
		return nil, err
	}
	// TODO(Phase 4): emitObjectCreated via event bus.
	return obj, nil
}

// GetObject reads through to the SPI. The memory provider returns
// spi.ErrObjectNotFound for both missing and cross-tenant (type, id) —
// the Engine surfaces that as a typed not-found, never synthesizing a
// value, mirroring TS ObjectManager.get's null-on-absent contract.
func (e *Engine) GetObject(ctx spi.RequestContext, typ, id string) (spi.OntologyObject, error) {
	obj, err := e.storage.GetObject(ctx, typ, id)
	if err != nil {
		return nil, err
	}
	// TODO(Phase 4): evaluate LAZY computed fields and merge.
	return obj, nil
}

// UpdateObject reads the existing object first (returning a typed
// not-found before any write), merges the patch excluding system
// fields, validates the merged state, then delegates to SPI with the
// caller's expectedVersion. Phase 3 transmits expectedVersion (Phase 2
// passed nil and deferred versioning); the memory provider is now
// authoritative for the conflict check and _version increment (R1, R8).
// A nil expectedVersion from the caller means "accept any version".
// The returned object carries the storage-advanced _updatedAt and _version.
func (e *Engine) UpdateObject(ctx spi.RequestContext, typ, id string, patch map[string]any, expectedVersion *int) (spi.OntologyObject, error) {
	existing, err := e.storage.GetObject(ctx, typ, id)
	if err != nil {
		// Cross-tenant and not-found both surface as ErrObjectNotFound
		// from the SPI; bubble unchanged so callers see a typed error
		// before any write is attempted (covers AE4).
		return nil, err
	}
	merged := mergePatch(existing, patch)
	if err := e.validateObjectPayload(typ, merged, true); err != nil {
		return nil, err
	}
	updated, err := e.storage.UpdateObject(ctx, typ, id, patch, expectedVersion)
	if err != nil {
		return nil, err
	}
	// TODO(Phase 4): emitObjectUpdated via event bus.
	return updated, nil
}

// DeleteObject reads the existing object first (returning a typed
// not-found before any write), then issues a hard delete through the
// SPI. Soft delete is a Phase 3 concern; passing mode="soft" here
// surfaces the SPI's ErrUnimplemented so callers cannot silently get
// free soft-delete behaviour. Subsequent GetObject returns not-found
// (verified by the caller, not re-asserted here).
func (e *Engine) DeleteObject(ctx spi.RequestContext, typ, id, mode string) error {
	if _, err := e.storage.GetObject(ctx, typ, id); err != nil {
		return err
	}
	if err := e.storage.DeleteObject(ctx, typ, id, mode); err != nil {
		return err
	}
	// TODO(Phase 4): emitObjectDeleted via event bus.
	return nil
}

// validateObjectPayload is the per-verb Engine-side validator (R7).
// It performs the lightweight runtime checks the held TBox is trusted
// for after ir.Validate: type presence, role-restricted fields do not
// appear in the payload, scalar type matches the ir.TypeRef, and —
// only on Create — required NonNull fields without a default are
// present. isUpdate relaxes the required check since merged state is
// validated (the patch only needs to match field types).
func (e *Engine) validateObjectPayload(typ string, properties map[string]any, isUpdate bool) error {
	objType := e.ontology.ObjectByName(typ)
	if objType == nil {
		return fmt.Errorf("%w: %s", spi.ErrInvalidObjectType, typ)
	}
	for name, val := range properties {
		if isSystemField(name) {
			// System fields are SPI/Engine-managed; ignore them in the
			// user-payload check (Create/Update strips them anyway).
			continue
		}
		field := findField(objType, name)
		if field == nil {
			// Unknown property. TS would emit a schema-step failure; we
			// accept it as the SPI's problem to store or ignore. Strict
			// unknown-property rejection is a Phase 3 tightening.
			continue
		}
		if !roleWritable(field.Role) {
			return fmt.Errorf("openfoundry: field %q on type %q has role %s and is not writable via payload", name, typ, field.Role)
		}
		if val == nil {
			continue
		}
		if err := checkScalarType(field.Type, val); err != nil {
			return fmt.Errorf("openfoundry: type %q field %q: %w", typ, name, err)
		}
	}
	if !isUpdate {
		for i := range objType.Fields {
			f := &objType.Fields[i]
			if isSystemField(f.Name) || !roleWritable(f.Role) {
				continue
			}
			if !f.Type.NonNull {
				continue
			}
			if f.Flags.Default != nil {
				continue
			}
			if _, present := properties[f.Name]; !present {
				return fmt.Errorf("openfoundry: type %q missing required field %q", typ, f.Name)
			}
		}
	}
	return nil
}

// mergePatch returns a merged property map for validation: existing
// non-system fields plus every key in patch (including overrides).
// System fields stay sourced from existing so the validator never
// treats an injected _id/_type as a user payload.
func mergePatch(existing spi.OntologyObject, patch map[string]any) map[string]any {
	merged := make(map[string]any, len(existing)+len(patch))
	for k, v := range existing {
		if !isSystemField(k) {
			merged[k] = v
		}
	}
	for k, v := range patch {
		if !isSystemField(k) {
			merged[k] = v
		}
	}
	return merged
}

// findField returns a pointer to the field with the given name on
// objType, or nil if none. Allocation-free over small field lists is
// fine; the IR carries tens of fields per type, not thousands.
func findField(objType *ir.ObjectType, name string) *ir.Field {
	for i := range objType.Fields {
		if objType.Fields[i].Name == name {
			return &objType.Fields[i]
		}
	}
	return nil
}

// roleWritable reports whether a field's semantic role accepts a value
// from a Create/Update payload. Primary, LinkNav, and Computed are
// Engine/computed-owned and cannot be set by callers — mirroring TS
// validation.ts skipping isPrimaryField / isLinkField / isComputedField.
// RoleParam is writable (action params); RoleProperty is the common
// writable case.
func roleWritable(role ir.FieldRole) bool {
	switch role {
	case ir.RolePrimary, ir.RoleLinkNav, ir.RoleComputed:
		return false
	default:
		// RoleProperty and RoleParam are writable.
		return true
	}
}

// checkScalarType asserts the runtime value matches the ir.TypeRef for
// the scalar types Phase 2 carries (the supply-chain pack uses String,
// Int, Float, Boolean, ID, DateTime). Non-scalar refs (object/enum
// names) are accepted; they are validated upstream by ir.Validate.
// List types accept any slice/array; element-type checks are a Phase 3
// tightening. JSON-decoded payloads carry numbers as float64, so Int
// accepts float64 with integer magnitude.
func checkScalarType(t ir.TypeRef, val any) error {
	switch t.Name {
	case "String", "ID":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("expected string, got %T", val)
		}
	case "Int":
		switch n := val.(type) {
		case int, int32, int64:
			return nil
		case float64:
			if n != float64(int64(n)) {
				return fmt.Errorf("expected int, got non-integer float64 %v", n)
			}
		default:
			return fmt.Errorf("expected int, got %T", val)
		}
	case "Float":
		switch val.(type) {
		case float32, float64, int, int32, int64:
			return nil
		default:
			return fmt.Errorf("expected float, got %T", val)
		}
	case "Boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("expected bool, got %T", val)
		}
	case "DateTime":
		// Stored as RFC3339Nano strings (see memory.systemTimestamps).
		if _, ok := val.(string); !ok {
			return fmt.Errorf("expected datetime string, got %T", val)
		}
	}
	return nil
}

// isSystemField reports whether k is an Engine/SPI-managed field name.
// Mirrors memory.isSystemField so the two layers agree on the reserved
// set; intentionally kept inline here to preserve the engine→storage
// layer boundary (engine does not import the memory package).
func isSystemField(k string) bool {
	switch k {
	case "_id", "_type", "_tenantId", "_createdAt", "_updatedAt", "_version", "_deletedAt", "_engineLinkId":
		return true
	}
	return false
}

// CreateLink validates the link type exists in the held TBox, asserts
// both endpoints are present objects via storage.GetObject (returning
// a typed not-found before any write per AE6), mints a UUIDv7 link id,
// and delegates to storage.CreateLink with _engineLinkId merged in.
// Cardinality enforcement is deliberately skipped (Phase 3, with
// GetLinks); the Engine never calls GetLinks here. Properties are
// copied before mutation so the caller's map is not side-effected.
func (e *Engine) CreateLink(ctx spi.RequestContext, typ, fromID, toID string, properties map[string]any) (spi.OntologyLink, error) {
	linkDef := e.ontology.LinkByName(typ)
	if linkDef == nil {
		return nil, fmt.Errorf("%w: %s", spi.ErrInvalidLinkType, typ)
	}
	if _, err := e.storage.GetObject(ctx, linkDef.From, fromID); err != nil {
		// Missing or cross-tenant endpoint both surface as
		// ErrObjectNotFound from the SPI; bubble unchanged so the
		// caller sees a typed not-found before any link write (AE6
		// from side; same shape applies to the to side below).
		return nil, err
	}
	if _, err := e.storage.GetObject(ctx, linkDef.To, toID); err != nil {
		return nil, err
	}
	linkID := uuidv7.New()
	// Copy so we never mutate the caller's map; same shape as the TS
	// spread ...properties, { _engineLinkId }.
	props := make(map[string]any, len(properties)+1)
	for k, v := range properties {
		props[k] = v
	}
	props["_engineLinkId"] = linkID
	link, err := e.storage.CreateLink(ctx, typ, fromID, toID, props)
	if err != nil {
		return nil, err
	}
	// TODO(Phase 4): emitLinkCreated via event bus.
	return link, nil
}

// GetLink is a pass-through read. The memory provider returns
// spi.ErrLinkNotFound for both missing and cross-tenant links; the
// Engine surfaces that as a typed not-found, matching TS LinkManager
// .getLink's null-on-absent contract.
func (e *Engine) GetLink(ctx spi.RequestContext, typ, linkID string) (spi.OntologyLink, error) {
	link, err := e.storage.GetLink(ctx, typ, linkID)
	if err != nil {
		return nil, err
	}
	return link, nil
}

// DeleteLink reads the existing link first (returning a typed not-found
// before any write per AE7), then issues a hard delete via SPI. Memory
// removes the link from its map; subsequent GetLink returns not-found
// (verified by the caller, not re-asserted here).
func (e *Engine) DeleteLink(ctx spi.RequestContext, typ, linkID string) error {
	if _, err := e.storage.GetLink(ctx, typ, linkID); err != nil {
		return err
	}
	if err := e.storage.DeleteLink(ctx, typ, linkID); err != nil {
		return err
	}
	// TODO(Phase 4): emitLinkDeleted via event bus.
	return nil
}
