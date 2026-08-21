# Open Foundry OBDA Mapping Design v1

**Status:** Proposed  
**Date:** 2026-08-21  
**Supersedes for implementation:** `docs/design/open-foundry-obda-mapping-spec-v1.md`  
**References:** `docs/open-foundry-spec-v2.md`, `runtime/spi/provider.go`

## 1. Decision Summary

OBDA v1 adopts a two-layer definition:

1. **OBDA Core** is a database-neutral mapping and planning abstraction. It converts ODL storage schema, OBDA mapping metadata, and `StorageProvider` requests into a dialect-neutral SQL plan.
2. **SQL Dialect Adapter** converts the neutral plan into executable SQL for one database family. MySQL is the only v1 adapter.

The first runtime implementation is an **OBDA-backed MySQL `StorageProvider`**. OBDA is not read-only and is not merely a Sync Engine overlay adapter. The provider implements the complete API in `runtime/spi/provider.go`, including schema, object, link, query, aggregate, search, bulk mutation, transaction, temporal, index, health, and capability methods.

v1 explicitly excludes ReBAC integration. Tenant isolation remains mandatory.

## 2. Goals

- Define a stable SQL-neutral OBDA abstraction.
- Keep SQL syntax, type behavior, introspection, and DDL inside dialect adapters.
- Deliver MySQL as the first and only v1 SQL dialect.
- Implement every method of `spi.StorageProvider` with documented behavior.
- Support reads and writes against mapped relational tables.
- Support existing schemas that do not contain Open Foundry system columns.
- Preserve ODL as the semantic schema source of truth.
- Make later PostgreSQL, SQL Server, SQLite, or other adapters possible without changing `StorageProvider` or the OBDA mapping model.

## 3. Non-Goals

- ReBAC planning, OpenFGA tuple projection, or authorization predicate injection.
- Cross-database joins or distributed transactions.
- CDC, polling, batch synchronization, or conflict resolution.
- Federation and cost-based source selection.
- Arbitrary SQL queries or unrestricted SQL expressions in mappings.
- Supporting more than one SQL dialect in v1.
- Treating the old `datasources/*.yaml` format and OBDA as two permanent authoring formats.

## 4. Normative Language

The terms MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are normative.

- A provider is **complete** when every `StorageProvider` method has defined runtime behavior. Embedding `spi.UnimplementedStorageProvider` only satisfies Go's forward-compatibility requirement; returning `spi.ErrUnimplemented` is not a valid v1 implementation.
- A mapping is **writable** when the compiler can deterministically produce `INSERT`, `UPDATE`, and `DELETE` plans for it.
- A mapping is **read-only** when reads are valid but mutations return a stable read-only mapping error.
- A **transaction domain** is one MySQL database in which business tables and OBDA metadata tables can participate in the same local transaction.

## 5. Architecture

```text
Ontology Engine
      |
      | spi.StorageProvider
      v
MySQL OBDA StorageProvider
      |
      +-- Mapping Registry
      +-- OBDA Compiler
      +-- Neutral Query / Mutation Planner
      +-- Row and Identity Mapper
      +-- Transaction Coordinator
      |
      v
SQL Dialect Interface
      |
      v
MySQL Dialect Adapter
      |
      v
database/sql -> MySQL 8
```

The dependency direction is one-way:

```text
StorageProvider -> OBDA Core -> SQL Dialect Interface <- MySQL Adapter
```

OBDA Core MUST NOT import a MySQL driver or emit MySQL syntax. The MySQL adapter MUST NOT redefine ODL or `StorageProvider` semantics.

## 6. OBDA Core Interfaces

The interfaces below are design contracts. Exact Go package boundaries may change during implementation, but responsibilities MUST remain separated.

```go
type Compiler interface {
	Compile(
		schema spi.OntologySchema,
		mapping MappingDocument,
		source SourceSnapshot,
		dialect Dialect,
	) (*CompiledMapping, Diagnostics)
}

type Planner interface {
	PlanObject(Operation, ObjectBinding, Request) (StatementPlan, error)
	PlanLink(Operation, LinkBinding, Request) (StatementPlan, error)
	PlanQuery(ObjectBinding, spi.FilterExpression, *spi.QueryOptions) (QueryPlan, error)
	PlanAggregate(ObjectBinding, spi.AggregateQuery) (QueryPlan, error)
	PlanSearch(ObjectBinding, spi.SearchQuery) (QueryPlan, error)
	PlanTraverse(string, spi.TraversalPath, *spi.TraversalOptions) (QueryPlan, error)
}

type MappingRegistry interface {
	Prepare(ctx context.Context, candidate MappingBundle) (ActivationPlan, error)
	Activate(ctx context.Context, plan ActivationPlan) error
	Current(ctx context.Context) (*CompiledMapping, error)
	Get(ctx context.Context, version int) (*CompiledMapping, error)
}
```

`CompiledMapping` is immutable. A request captures one active compiled version at its start and MUST NOT observe a mapping switch halfway through execution.

### 6.1 Neutral SQL AST

OBDA Core plans against a closed SQL AST rather than SQL strings. The minimum AST includes:

- `Select`
- `Insert`
- `Update`
- `Delete`
- `Join`
- `Predicate`
- `Order`
- `Group`
- `Aggregate`
- `LimitOffset`
- `CommonTableExpression`
- `CreateIndex`
- `DropIndex`

Runtime values are represented as parameters. Physical identifiers are represented as validated identifier nodes. Neither may be injected as raw SQL text.

### 6.2 SQL Dialect Interface

```go
type Dialect interface {
	Name() string
	VersionRange() VersionRange
	Capabilities() DialectCapabilities

	QuoteIdentifier(Identifier) (string, error)
	Placeholder(position int) string
	Render(StatementPlan) (SQLStatement, error)
	MapType(ODLType) (PhysicalType, error)
	NormalizeValue(ODLType, any) (any, error)

	Inspect(context.Context, DBTX, SourceRef) (SourceSnapshot, error)
	PlanSchemaChange(SchemaChange, *CompiledMapping) (DDLPlan, error)
	PlanIndex(IndexRequest, *CompiledMapping) (DDLPlan, error)
	ClassifyError(error) error
}
```

`SQLStatement` contains SQL text and a separate ordered argument list. A dialect MUST NOT concatenate runtime values into SQL.

### 6.3 Dialect Capabilities

Dialect capability declarations describe SQL-engine features, not per-model mapping availability.

```go
type DialectCapabilities struct {
	Transactions       bool
	Savepoints         bool
	RecursiveCTE       bool
	FullTextSearch     bool
	GeneratedColumns   bool
	JSON               bool
	Spatial            bool
	OnlineIndex        bool
}
```

Per-model capabilities are derived during compilation and stored in `CompiledMapping`. For example, MySQL supports full-text search, but a model without searchable fields does not.

## 7. MySQL Dialect Profile v1

The v1 adapter targets MySQL 8.0 or later.

It MUST define:

- `?` value placeholders.
- Backtick identifier quoting with embedded backticks rejected before rendering.
- MySQL scalar and JSON type mappings.
- `LIMIT ? OFFSET ?` pagination.
- `MATCH (...) AGAINST (...)` full-text search.
- Recursive CTE rendering for bounded variable-depth traversal.
- InnoDB transaction behavior.
- MySQL error mapping for duplicate keys, deadlocks, lock timeouts, missing tables, and connection failures.
- MySQL DDL for BTREE, HASH where supported by the selected engine, UNIQUE, and FULLTEXT indexes.

The adapter MUST reject `GIN` and `GIST` index requests. It MUST NOT emulate them silently.

The adapter package contains MySQL-specific implementations of:

- `Dialect`
- source introspection
- SQL AST rendering
- type mapping
- DDL and index planning
- database error classification
- capability detection

Adding a future dialect MUST require a new adapter, not conditionals distributed through OBDA Core.

## 8. Mapping Document

The canonical authoring format is `*.obda.yaml`.

```yaml
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig

metadata:
  name: hospital
  namespace: nhs.acute
  version: 1

schema:
  namespace: nhs.acute
  version: 1

sources:
  primary:
    kind: sql
    dialect: mysql
    connection:
      dsnRef: secret://hospital/mysql-dsn

models:
  Patient:
    sourceRef: primary
    relation:
      kind: table
      catalog: hospital
      name: patient
    access: readWrite
    identity:
      strategy: direct
      columns: [patient_id]
    tenant:
      strategy: column
      column: tenant_id
    system:
      strategy: sidecar
    fields:
      name:
        column: patient_name
      status:
        column: status

links:
  AdmittedTo:
    sourceRef: primary
    relation:
      kind: table
      catalog: hospital
      name: admission
    access: readWrite
    identity:
      strategy: direct
      columns: [admission_id]
    from:
      object: Patient
      columns: [patient_id]
    to:
      object: Ward
      columns: [ward_id]
    tenant:
      strategy: column
      column: tenant_id
    system:
      strategy: sidecar
```

### 8.1 Source References

Every model and link MUST declare `sourceRef`. A provider instance MAY read multiple connections, but v1 writable bindings MUST all resolve to one transaction domain.

Plaintext credentials MUST NOT appear in mapping files, compiled artifacts, logs, diagnostics, or explain output.

### 8.2 Relation Kinds

v1 supports:

- `table`: readable and potentially writable.
- `view`: read-only.

`sqlQuery` is deferred. Arbitrary SQL text is not accepted by the v1 compiler.

### 8.3 Writable Binding Rules

A writable model or link MUST satisfy all of the following:

- It maps to one physical table.
- Its identity maps to a primary or unique key.
- Every writable property maps directly to one column or to an approved reversible transform.
- Tenant and system-field strategies are complete.
- Inserts have a deterministic source-key strategy.
- Updates can use an atomic version predicate.
- Deletes have a defined soft- and hard-delete plan.
- The table belongs to the provider's writable transaction domain.

The compiler MUST reject `access: readWrite` when any rule is not satisfied.

### 8.4 Transform Scope

v1 supports a closed transform set:

- `prefix`
- `suffix`
- `trim`
- `toUpper`
- `toLower`
- `map`
- `parseDate`
- `parseDateTime`
- `coalesce` for reads only

Writable fields MUST use transforms with a defined inverse. `hash`, `lookup`, custom functions, free-form expressions, and computed SQL are deferred.

## 9. Identity

### 9.1 Object Identity

`GetLinks` and `Traverse` receive an object ID without an object type. Object IDs therefore MUST be globally unambiguous within a provider.

v1 supports:

1. `direct`: encode namespace, object type, and canonical physical key into a reversible ID.
2. `sidecar`: persist an engine UUIDv7 to physical-key mapping.

The provider MUST NOT search multiple tables to guess an ID's type.

### 9.2 Link Identity

Every link has an independent ID. Endpoint hashing is not a valid general identity because more than one link of the same type may connect the same endpoints.

- Association tables with stable unique keys MAY use `direct`.
- Other link shapes MUST use `sidecar`.
- Foreign-key-derived links are read-only in v1 unless a sidecar supplies an independent link identity and mutation plan.

### 9.3 Canonical Physical Keys

Composite physical keys are encoded as typed, ordered components. String concatenation with a delimiter is forbidden because it is ambiguous.

The canonical encoding is used by identity lookup, metadata sidecars, history, idempotency, and cache keys.

## 10. Provider-Owned Metadata

MySQL v1 creates metadata tables in a configurable namespace. Their logical responsibilities are:

| Logical table | Responsibility |
|---|---|
| `of_schema_versions` | Applied `OntologySchema` snapshots |
| `of_mapping_versions` | Source mapping and compiled artifact versions |
| `of_mapping_activation` | Atomically selected active schema and mapping pair |
| `of_object_meta` | Engine ID, version, timestamps, deletion state, physical key |
| `of_link_meta` | Link ID, endpoints, version, timestamps, deletion state |
| `of_object_history` | Object snapshots required by temporal reads |
| `of_link_history` | Link snapshots required by temporal traversal |
| `of_idempotency` | Bulk request key, request hash, status, and result |
| `of_index_registry` | Logical-to-physical index state |

Implementations MAY consolidate tables, but MUST preserve these semantics.

Business-table writes and corresponding metadata/history writes MUST use the same MySQL transaction.

### 10.1 System Fields

Every returned object includes:

- `_tenantId`
- `_type`
- `_id`
- `_version`
- `_createdAt`
- `_updatedAt`
- `_deletedAt` when deleted

Every returned link additionally includes:

- `_fromType`
- `_fromId`
- `_toType`
- `_toId`

Mappings MAY map system fields to native columns. Missing fields MUST be supplied by sidecar metadata. Values MUST NOT be synthesized differently on each read.

## 11. Tenant Isolation

ReBAC is excluded, but tenant isolation is not.

Every operation with an empty `RequestContext.TenantID` MUST fail. Cross-tenant absence is reported as the same not-found error as ordinary absence.

v1 supports:

- `column`: the runtime injects a tenant predicate and tenant value for every read and write.
- `constant`: the binding is valid for one configured tenant; any other tenant is rejected.
- `connection`: a validated tenant-to-connection map selects an isolated pool.

The tenant value MUST come only from `RequestContext`. Callers cannot override it in filters or properties.

Links MUST connect objects in the same tenant. Transactions capture an immutable tenant at begin time.

## 12. Complete StorageProvider Contract

The MySQL provider embeds `spi.UnimplementedStorageProvider` for source compatibility and overrides every method.

### 12.1 Schema

| Method | v1 behavior |
|---|---|
| `ApplySchema` | Validate against the prepared mapping, store a versioned schema snapshot, apply provider-owned DDL/index changes, and atomically activate the schema/mapping pair. External business-table DDL is changed only when the mapping declares provider ownership. |
| `GetSchema` | Return the active or requested snapshot from `of_schema_versions`. |

Mapping activation uses prepare, validate, then activate. Requests already in flight retain the old immutable compiled mapping.

### 12.2 Objects

| Method | v1 behavior |
|---|---|
| `CreateObject` | Validate a writable binding, inject tenant/system values, insert the business row, create metadata/history, and return the complete object. |
| `GetObject` | Decode identity, apply tenant scope, load the row and metadata, and return soft-deleted objects with `_deletedAt`. |
| `UpdateObject` | Perform atomic compare-and-swap when `expectedVersion` is set; update business data, metadata, and history in one transaction. |
| `DeleteObject` | Soft delete by default; hard delete removes the object and links under an explicit hard mode and records history before removal. |
| `QueryObjects` | Compile filters, ordering, soft-delete behavior, and offset pagination into parameterized SQL. |
| `AggregateObjects` | Support `count`, `sum`, `avg`, `min`, and `max`; tenant and deletion predicates are applied before aggregation. |
| `SearchObjects` | Use MySQL FULLTEXT on declared searchable fields; filters and tenant predicates are part of the same query. |
| `BulkMutate` | Execute tenant-scoped operations with a persistent idempotency record. Same key and same payload returns the prior result; same key and different payload is rejected. |

`UpdateObject` and `DeleteObject` MUST constrain tenant, identity, and expected version in the write predicate. A zero-row compare-and-swap returns `spi.ErrVersionConflict` when the object exists at another version.

### 12.3 Links

| Method | v1 behavior |
|---|---|
| `CreateLink` | Validate endpoints, tenant, type, and cardinality; insert link and metadata atomically. |
| `GetLink` | Resolve independent link identity and return complete endpoint/system fields. |
| `UpdateLink` | Update mapped properties with atomic version checking. |
| `DeleteLink` | Soft-delete the link when supported; otherwise remove it and retain history. |
| `GetLinks` | Resolve the globally typed object ID, apply direction/type/tenant filters, and return a page. |
| `Traverse` | Compile typed path steps to joins or bounded recursive CTEs and return terminal nodes, walked edges, and strict intermediate nodes. |

Cardinality MUST be enforced during writes using a unique active-key strategy or transactionally locked sidecar constraint. Selecting one row with `LIMIT 1` is not cardinality enforcement.

### 12.4 Transactions

`BeginTransaction` returns a MySQL transaction wrapper implementing every method in `spi.Transaction`.

- All writes in the transaction use one captured tenant and one compiled mapping version.
- Business rows, sidecar metadata, history, and idempotency state commit or roll back together.
- Cross-transaction-domain writes are rejected before the first mutation.
- Deadlocks and lock timeouts are returned as retryable classified errors.

### 12.5 Temporal Reads

| Method | v1 behavior |
|---|---|
| `GetObjectAtVersion` | Read the requested version from native history mapping or `of_object_history`. |
| `GetObjectAtTime` | Select the latest version visible at the requested time. |

`QueryOptions.AsOfVersion` and `AsOfTime` use the same history source. A binding cannot advertise temporal support unless both version and time queries pass conformance tests.

### 12.6 Indexes

| Method | v1 behavior |
|---|---|
| `EnsureIndex` | Resolve a logical field to physical columns, validate the MySQL index type, create the index idempotently, and record it. |
| `DropIndex` | Drop only indexes owned by the provider registry. |
| `ListIndexes` | Return logical index definitions and reconcile registry state with MySQL metadata. |

Indexes on views, read-only sources, non-pushdown transforms, or unsupported types return stable typed errors.

### 12.7 Health and Capabilities

`HealthCheck` checks:

- database connectivity and latency
- metadata schema version
- active schema/mapping pair
- source fingerprint compatibility
- degraded model/link bindings

The initial MySQL capability declaration is:

```text
SupportsTransactions    = true
SupportsTemporalQueries = true
SupportsFullTextSearch  = true
SupportsGeoQueries      = false
SupportsGraphTraversal  = true
SupportsBulkMutations   = true
MaxTraversalDepth       = 8
ReplicationSupport      = configured from deployment
```

Provider-level capability `true` means the operation exists. The compiled mapping additionally records which object or link types can use it.

## 13. Query Semantics

### 13.1 Filters

The closed operator set is:

```text
eq neq gt gte lt lte in contains startsWith exists
and or not
```

Fields and operators are resolved against the compiled mapping. Runtime values are bound parameters. Unknown fields, mixed logical/field nodes, empty logical arrays, and invalid operator/type combinations are rejected before SQL rendering.

### 13.2 Ordering and Pagination

v1 implements the current `QueryOptions` contract with `LIMIT/OFFSET`. The planner always adds canonical identity as a deterministic tie-breaker.

`ObjectPage.Cursor` and `LinkPage.Cursor` remain empty until the input SPI accepts a cursor. The provider MUST NOT emit a cursor it cannot consume.

Keyset pagination is a follow-up SPI change, not an OBDA-specific extension hidden in SQL.

### 13.3 Aggregate

Group-by fields and aggregate functions come from closed enums. Aliases are validated logical names, not raw SQL. Tenant and soft-delete predicates are applied before grouping.

### 13.4 Search

Search is available only for fields backed by a MySQL FULLTEXT index. The provider returns a stable score. v1 does not promise generated highlights; `Highlights` MAY be empty.

### 13.5 Traversal

- A path with explicit steps is fixed-depth traversal.
- `MaxDepth` of `0` or `1` means one hop for that step.
- `MaxDepth > 1` uses a bounded recursive CTE.
- A request exceeding `MaxTraversalDepth` is rejected.
- Cycles are prevented using visited canonical object IDs.

## 14. Mutation Semantics

### 14.1 Optimistic Concurrency

Updates use one atomic write predicate. With sidecar versions, the transaction locks the metadata row, verifies the expected version, updates the business row, increments metadata, and writes history before commit.

### 14.2 Soft and Hard Delete

- Soft delete updates native or sidecar deletion state and increments version.
- Default queries exclude deleted objects and links.
- `GetObject` and `GetLink` may return deleted records with `_deletedAt`.
- Hard delete requires explicit `mode == "hard"`.
- Object hard delete removes or terminates all links in the same transaction.

Retention policy and archive workflows are outside OBDA v1.

### 14.3 Bulk Idempotency

The idempotency key is scoped by tenant. The provider stores a canonical request hash and final result.

- same key + same hash + complete: return stored result
- same key + same hash + in progress: resume or return retryable status
- same key + different hash: reject

v1 processes operations in one transaction. Chunked/resumable jobs require an SPI extension and are deferred.

## 15. Mapping Lifecycle

Activation validates three versions:

```text
OntologySchema version
OBDA mapping version
MySQL source fingerprint
```

The lifecycle is:

1. Parse and schema-validate `*.obda.yaml`.
2. Introspect the MySQL source.
3. Compile and validate all bindings.
4. Produce diagnostics and required provider-owned DDL.
5. Apply metadata migration.
6. Backfill identity/system metadata when required.
7. Atomically activate the schema/mapping pair.
8. Retain the previous compiled version for in-flight requests and rollback.

Changing a business-table fingerprint without a compatible mapping puts affected bindings into degraded health and fails closed.

## 16. Error Contract

Existing SPI sentinel errors remain authoritative:

- `spi.ErrObjectNotFound`
- `spi.ErrLinkNotFound`
- `spi.ErrInvalidObjectType`
- `spi.ErrInvalidLinkType`
- `spi.ErrVersionConflict`
- `spi.ErrCardinalityViolation`

Implementation planning MUST add stable errors for:

- unsupported capability
- read-only mapping
- invalid mapping
- mapping not active
- tenant required or mismatched
- unsupported index type
- source schema drift
- idempotency conflict
- transaction-domain violation

Provider methods MUST return classified errors, never raw driver text as the public contract.

## 17. Security Boundary for v1

ReBAC is intentionally ignored in this design.

The provider still MUST enforce:

- tenant isolation
- parameterized runtime values
- validated and quoted identifiers
- secret references instead of plaintext credentials
- TLS-capable MySQL connections
- separate runtime DML and migration DDL credentials
- log, trace, explain, and error redaction
- bounds on query limit, traversal depth, and bulk size

Authorization and consent remain responsibilities of layers above `StorageProvider`. This design does not claim that direct provider access is authorized access.

## 18. Package Shape

A recommended implementation layout is:

```text
runtime/
  obda/
    mapping.go
    compiler.go
    planner.go
    identity.go
    registry.go
    sqlast/
    dialect/
      dialect.go
      mysql/
        dialect.go
        renderer.go
        introspect.go
        migrate.go
        errors.go
  storage/
    mysqlobda/
      provider.go
      objects.go
      links.go
      query.go
      aggregate.go
      search.go
      bulk.go
      transaction.go
      temporal.go
      indexes.go
      health.go
```

The exact file split is non-normative. The core/dialect/provider dependency boundaries are normative.

## 19. Delivery Phases

### Phase 1 — Core writable provider

- mapping parser and validator
- neutral AST and MySQL renderer
- mapping registry and activation
- provider-owned metadata
- schema methods
- object CRUD and query
- link CRUD and `GetLinks`
- local transactions
- tenant isolation
- soft delete and optimistic concurrency
- health and capability reporting

### Phase 2 — Complete current SPI

- aggregate
- MySQL FULLTEXT search
- bulk idempotency
- object/link history
- temporal methods
- index lifecycle
- fixed and bounded recursive traversal

Phase 1 and Phase 2 together constitute OBDA v1. No release may claim full `StorageProvider` conformance before both pass.

### Deferred

- additional SQL dialect adapters
- views as rich read models
- arbitrary SQL query sources
- computed SQL and custom transforms
- cross-source writes
- distributed transactions
- keyset pagination SPI
- ReBAC integration
- Sync Engine and external overlay topology

## 20. Conformance and Acceptance

The MySQL implementation is accepted when:

1. It has a compile-time assertion that it implements `spi.StorageProvider`.
2. It overrides every `StorageProvider` method and none returns `spi.ErrUnimplemented`.
3. The existing storage conformance tests pass against MySQL.
4. MySQL-specific tests run against a real MySQL 8 instance.
5. Object and link lifecycle tests cover direct and sidecar identities.
6. Concurrent update tests prove one `expectedVersion` writer wins.
7. Concurrent link tests prove cardinality cannot be exceeded.
8. Tenant tests prove cross-tenant reads and writes are indistinguishable from absence.
9. Transaction tests prove business rows and metadata/history commit and roll back together.
10. Bulk retry tests prove idempotency across provider restart.
11. Temporal tests prove version and time reads.
12. Search, aggregate, traversal, and index tests cover supported and rejected mappings.
13. SQL injection tests cover values, identifiers, sort fields, group fields, and mapping input.
14. Mapping activation tests prove in-flight requests retain one compiled version.
15. No ReBAC behavior is required by the v1 conformance suite.

## 21. Compatibility Decisions

This design intentionally changes the direction of the previous OBDA draft:

- OBDA is a full read/write provider foundation, not a read-only overlay.
- MySQL replaces PostgreSQL as the first implementation target.
- PostgreSQL+AGE is not required by this provider.
- Sync Engine does not own the direct MySQL provider query path.
- OBDA becomes the canonical mapping format; the overlapping v2 datasource mapping requires a separate compatibility/deprecation update.

`docs/open-foundry-spec-v2.md` still contains statements that conflict with these decisions, especially the v1 provider recommendation and Overlay ownership. Those statements MUST be reconciled before this design is marked Accepted, but they do not change the implementation direction defined here.

## 22. Final Position

The v1 product is:

```text
ODL storage schema
        +
OBDA mapping
        |
        v
database-neutral OBDA compiler and planner
        |
        v
SQL dialect interface
        |
        v
MySQL dialect adapter
        |
        v
complete MySQL StorageProvider
```

This boundary keeps `StorageProvider` stable, keeps OBDA independent of one SQL family, and limits the first delivery to one implementation that can be tested end to end.
