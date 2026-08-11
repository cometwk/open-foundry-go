package spi

import "errors"

// ErrUnimplemented is returned by Phase 1 stubs for non-schema SPI methods.
var ErrUnimplemented = errors.New("openfoundry: unimplemented")

// Sentinel errors for the lifecycle categories Phase 2 returns. They are
// defined in spi so the Engine and storage providers share a single
// classification surface, mirroring the TS PlatformError enum (`OBJECT_NOT_FOUND`,
// `LINK_NOT_FOUND`, `INVALID_LINK_TYPE`, `INVALID_OBJECT_TYPE`,
// `CARDINALITY_VIOLATION`). Sentinel equality (`errors.Is`) is the stable
// contract; message text may evolve.
var (
	// ErrObjectNotFound signals an object read/update/delete where the
	// (type, id) pair is absent or belongs to a different tenant than the
	// RequestContext. Tenant isolation hides cross-tenant presence behind
	// the same sentinel so callers cannot probe.
	ErrObjectNotFound = errors.New("openfoundry: object not found")

	// ErrLinkNotFound signals a link read/delete absent or cross-tenant.
	ErrLinkNotFound = errors.New("openfoundry: link not found")

	// ErrInvalidObjectType signals an Engine-level type rejection: the
	// type name is not present in the held *ir.Ontology.
	ErrInvalidObjectType = errors.New("openfoundry: invalid object type")

	// ErrInvalidLinkType signals an Engine-level link type rejection.
	ErrInvalidLinkType = errors.New("openfoundry: invalid link type")

	// ErrVersionConflict signals an UpdateObject/UpdateLink call whose
	// expectedVersion does not match the stored object's current _version.
	// Mirrors the TS VERSION_CONFLICT category. Phase 3 introduces version
	// bookkeeping in the memory provider (R1); a nil expectedVersion skips
	// the check and accepts any version.
	ErrVersionConflict = errors.New("openfoundry: version conflict")

	// ErrCardinalityViolation signals a CreateLink that would exceed the
	// LinkTypeDefinition.Cardinality bound (counted over active, same-tenant,
	// same-type links). Mirrors the TS CARDINALITY_VIOLATION category already
	// listed in the package comment above. Phase 3 enforces this atomically
	// under the memory provider's mutex (R4).
	ErrCardinalityViolation = errors.New("openfoundry: cardinality violation")
)
