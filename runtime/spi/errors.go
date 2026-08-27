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

	// ErrPreconditionFailed signals a CEL precondition that compiled and
	// ran but did not evaluate to true. The error chain carries the
	// manifest `error` string; it is distinct from resolve failures
	// (ErrObjectNotFound) and CEL compile/runtime failures (ErrCelEval).
	ErrPreconditionFailed = errors.New("openfoundry: precondition failed")

	// ErrCelEval signals a CEL compile or runtime error (including
	// missing-field access on dyn maps). It is not a false precondition.
	ErrCelEval = errors.New("openfoundry: cel eval")

	// ErrReadOnlyMapping signals a mutation against a compiled binding
	// whose access is read-only. Queries may still succeed.
	ErrReadOnlyMapping = errors.New("openfoundry: read-only mapping")

	// ErrUnsupportedCapability signals a request the dialect or compiled
	// binding cannot execute (search without FTS, GIN index, temporal
	// without history). It is not an empty-result success.
	ErrUnsupportedCapability = errors.New("openfoundry: unsupported capability")

	// ErrInvalidMapping signals a mapping document that fails parse or
	// semantic validation before activation.
	ErrInvalidMapping = errors.New("openfoundry: invalid mapping")

	// ErrMappingNotActive signals SPI methods other than HealthCheck and
	// Capabilities called before a schema/mapping pair is activated.
	ErrMappingNotActive = errors.New("openfoundry: mapping not active")

	// ErrTenantRequired signals a RequestContext with an empty TenantID.
	ErrTenantRequired = errors.New("openfoundry: tenant required")

	// ErrUnsupportedIndexType signals EnsureIndex/DropIndex for a physical
	// index type this provider will not create (HASH, GIN, GIST, or a
	// BTREE on a business table in SQLite v1).
	ErrUnsupportedIndexType = errors.New("openfoundry: unsupported index type")

	// ErrSourceSchemaDrift signals the live source fingerprint no longer
	// matches the activated mapping.
	ErrSourceSchemaDrift = errors.New("openfoundry: source schema drift")

	// ErrIdempotencyConflict signals BulkMutate reused a tenant-scoped
	// idempotency key with a different payload hash.
	ErrIdempotencyConflict = errors.New("openfoundry: idempotency conflict")

	// ErrTransactionDomain signals a write that would cross the provider's
	// single local transaction domain.
	ErrTransactionDomain = errors.New("openfoundry: transaction domain")
)
