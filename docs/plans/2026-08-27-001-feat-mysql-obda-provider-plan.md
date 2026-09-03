---
title: MySQL OBDA StorageProvider - Plan
type: feat
date: 2026-08-27
topic: mysql-obda-provider
artifact_contract: ce-unified-plan/v1
artifact_readiness: requirements-only
product_contract_source: ce-brainstorm
execution: code
---

# MySQL OBDA StorageProvider - Plan

## Goal Capsule

- **Objective:** A MySQL 8.0+ OBDA `StorageProvider` that mirrors sqliteobda's full SPI surface, with MySQL SQL as the sole divergence.
- **Product authority:** This requirements-only plan. It supersedes `docs/brainstorms/2026-08-21-obda-mysql-storage-provider-requirements.md`'s sidecar + full-SPI specifics for this round; that brainstorm remains the architecture/SPI vision and roadmap.
- **Open blockers:** None blocking planning. Outstanding Questions are all deferred-to-planning.

---

## Product Contract

### Summary

Add a MySQL 8.0+ OBDA provider that fully mirrors sqliteobda — objects CRUD, `GetLinks`, chained-JOIN `Traverse` (terminal nodes only), query, schema verify/bootstrap, transactions — pairing a MySQL dialect adapter (backtick quoting, FULLTEXT, `information_schema` introspection, MySQL error mapping) with a MySQL provider that reuses the shared OBDA planner. Capabilities mirror sqliteobda (temporal/bulk off). Tests run sqliteobda's matrix against a real MySQL via `TEST_DB_URL`.

### Problem Frame

The two-layer OBDA architecture — a SQL-neutral core plus pluggable dialect adapters — is already realized by sqliteobda, which proved the `dialect.Dialect` interface and the shared `runtime/obda` planner. The original 2026-08-21 MySQL vision framed a broader provider (sidecar-backed system columns, full temporal/bulk), but the shipped sqliteobda reference narrowed the actual surface: direct system columns on business tables, temporal and bulk off. There is no second dialect yet, so the architecture's "add a dialect without changing SPI or mapping semantics" promise is unproven. This round delivers the MySQL dialect + provider the way sqliteobda was built, taking sqliteobda's actual behavior as authoritative.

### Key Decisions

- **Provider-per-dialect, not a shared core.** MySQL ships its own provider package mirroring sqliteobda, rather than refactoring sqliteobda into a shared base. This honors "按照 sqlite obda 的方式," leaves working sqlite tests untouched, and avoids a speculative abstraction. The cost is duplicated provider plumbing when shared logic changes; a shared-core refactor is deferred until a third dialect appears or the duplication bites.
- **Direct system columns, no sidecars.** Source tables carry `version`/`created_at`/`updated_at`/`deleted_at` as real columns themselves. This adopts sqliteobda's approach and supersedes the 2026-08-21 sidecar design for this round; the sidecar path stays on the roadmap.
- **MySQL 8.0+ only.** Recursive CTE, JSON, FULLTEXT, window functions, and online index are assumed present. The single forced SQL divergence is MySQL's lack of `RETURNING`.
- **Mirror sqliteobda's capability subset.** Temporal history and bulk mutations stay off (`ErrUnsupportedCapability`); the 2026-08-21 full-SPI vision is roadmap, not v1.
- **Real-MySQL integration tests gated on `TEST_DB_URL`.** The test matrix mirrors sqliteobda and runs against a live MySQL 8.0; it skips cleanly when no instance is configured.

### Actors

- A1. Ontology Engine — the sole business caller; reads and writes objects and links through SPI. Unchanged by this round.
- A2. MySQL OBDA provider — parses/verifies mapping, binds the MySQL dialect, owns session setup, assembles query and traversal results.
- A3. MySQL dialect adapter — renders SQL, quotes identifiers, introspects schema, classifies driver errors, generates DDL.
- A4. Operator — provisions business tables with system columns and activates the mapping version.

### Requirements

**Architecture and structure**

- R1. Add a MySQL SQL dialect adapter implementing the existing `dialect.Dialect` interface, mirroring the sqlite adapter's responsibilities: identifier quoting, placeholder, statement render, value normalization, and capability declaration.
- R2. Add a MySQL OBDA `StorageProvider` that mirrors sqliteobda's full SPI surface and reuses the shared `runtime/obda` planner and compiler. MySQL SQL is the only divergence; no SPI shape or mapping semantics change.

**Dialect divergences (the only fork points)**

- R3. Identifier quoting uses MySQL backticks; the placeholder stays `?`, compatible with `github.com/go-sql-driver/mysql`.
- R4. Full-text search renders via `MATCH ... AGAINST` instead of sqlite's FTS5 virtual-table join, keeping `SupportsFullTextSearch` on.
- R5. MySQL has no `RETURNING`; the insert and update paths must still return the full created/updated object, preserving sqliteobda's "Put returns the object" behavior without a return clause.
- R6. DDL targets InnoDB with `utf8mb4` and MySQL type mapping; schema introspection (`InspectTable`/`TableExists`/`InspectIndexes`/`HasUniqueIndex`) and the activation fingerprint read `information_schema` instead of PRAGMAs.
- R7. MySQL driver errors classify to the same SPI errors as sqlite's `Classify`: duplicate key → cardinality or version conflict; missing table → schema drift, by MySQL error number.

**Provider wiring**

- R8. `Open` injects an already-open `*sql.DB` (the caller owns `sql.Open` with the MySQL DSN), relaxing the mapping dialect gate to accept `mysql` sources; session setup replaces sqlite PRAGMAs.
- R9. The MySQL driver is blank-imported (`_ "github.com/go-sql-driver/mysql"`) so `database/sql` registers it; provider code makes no direct driver-API calls.
- R10. `ApplySchema` verifies and fingerprints without auto-creating tables; `InitMappedSchema` executes MySQL DDL on opt-in. Business tables carry `version`/`created_at`/`updated_at`/`deleted_at` as real columns; no `of_*` sidecars are emitted.

**Capability parity**

- R11. `Capabilities()` mirrors sqliteobda: transactions on, temporal off, bulk off, graph traversal on with `MaxTraversalDepth` 8, full-text on, replication none.
- R12. `GetObjectAtVersion`, `GetObjectAtTime`, and `BulkMutate` return `ErrUnsupportedCapability`, mirroring sqliteobda and narrower than the 2026-08-21 full-SPI vision.

**Traverse and GetLinks**

- R13. `GetLinks` and `Traverse` delegate to the shared `obda.PlanGetLinksJoin`/`PlanTraverse`; the chained JOIN is one SQL; only terminal `Nodes` are returned; `Edges` and `Visited` are empty, matching the 2026-08-26 chained-JOIN requirements.
- R14. `Limit`/`Offset` slice terminal nodes; the default excludes soft-deleted links and peers; `IncludeDeleted` admits them — identical page and soft-delete semantics to sqliteobda.

**Tenancy, security, and lifecycle**

- R15. Every SPI call requires a non-empty `tenantId`; tenant predicates are runtime-injected and cannot be overridden by caller filters; cross-tenant reads are indistinguishable from not-found.
- R16. All runtime SQL values are parameterized; identifiers come only from compiled mapping and are never caller-supplied.
- R17. `UpdateObject`/`UpdateLink` with `expectedVersion` perform an atomic compare-and-swap; mismatch returns `ErrVersionConflict`. Cardinality is enforced on write. Soft delete is the default delete mode.

**Testing**

- R18. A test matrix mirrors sqliteobda's tests (objects CRUD, `GetLinks`, chained-JOIN `Traverse`, query, schema, transactions) and runs against a real MySQL 8.0 instance whose DSN comes from the `TEST_DB_URL` environment variable.
- R19. When `TEST_DB_URL` is unset, the MySQL integration tests skip rather than fail; CI without a MySQL instance does not exercise the provider.

### Key Flows

- F1. Open and activate
  - **Trigger:** Operator supplies an open `*sql.DB` and an OBDA mapping.
  - **Actors:** A2, A3, A4
  - **Steps:** Parse and validate mapping; bind the MySQL dialect; apply session setup; verify business tables and compute the fingerprint; store the activation.
  - **Outcome:** Subsequent SPI requests see one compiled mapping; failure leaves no half-active state.
  - **Covered by:** R2, R6, R8, R10

- F2. Read object or query
  - **Trigger:** Engine `GetObject` / `QueryObjects`.
  - **Actors:** A1, A2, A3
  - **Steps:** Decode identity; inject tenant and soft-delete predicates; render via the dialect; scan rows; map system fields.
  - **Outcome:** A valid `OntologyObject`; cross-tenant access reads as not-found.
  - **Covered by:** R3, R15, R16

- F3. Write with OCC and a RETURNING-free insert
  - **Trigger:** Engine `CreateObject` / `UpdateObject` with `expectedVersion`.
  - **Actors:** A1, A2, A3
  - **Steps:** Validate the writable binding; insert without a return clause, then fetch the full row to return the object; update via a version predicate.
  - **Outcome:** The returned object equals a follow-up `GetObject`; a version mismatch returns `ErrVersionConflict` with no partial write.
  - **Covered by:** R5, R17

- F4. GetLinks and Traverse
  - **Trigger:** Engine `GetLinks` / `Traverse` on a fixed path.
  - **Actors:** A1, A2
  - **Steps:** Delegate to the shared planner; render one chained JOIN; scan terminal rows only.
  - **Outcome:** `Nodes` are terminal objects; `Edges` and `Visited` are empty; pagination and soft-delete act on the terminal.
  - **Covered by:** R13, R14

- F5. Real-MySQL test
  - **Trigger:** `go test` with `TEST_DB_URL` set.
  - **Actors:** A2, A3
  - **Steps:** Connect to the MySQL 8.0 DSN; run the mirrored matrix; tear down per-test state.
  - **Outcome:** Full matrix green when the DSN is set; skipped when unset.
  - **Covered by:** R18, R19

### Acceptance Examples

- AE1. **Covers R2, R11, R12.** Given mysqlobda injected into the Engine. When each `StorageProvider` method is called. Then every method has defined behavior; temporal and bulk return `ErrUnsupportedCapability`.
- AE2. **Covers R3, R5.** Given a writable object binding. When `CreateObject` then `GetObject`. Then the created object equals the fetched object, with no `RETURNING` in the executed SQL.
- AE3. **Covers R13, R14.** Given a two-hop live path with a soft-deleted peer on one branch. When `Traverse` the fixed path. Then `Nodes` are terminal objects, `Edges`/`Visited` are empty, and the execution is one SQL; the soft-deleted peer is excluded by default and admitted with `IncludeDeleted`.
- AE4. **Covers R6, R7.** Given a duplicate key on insert and a business table missing at activate. Then the insert returns `ErrCardinalityViolation` and activation returns `ErrSourceSchemaDrift`, both classified from MySQL errors.
- AE5. **Covers R18, R19.** Given the matrix with `TEST_DB_URL` set and then unset. Then it runs green against MySQL 8.0, and skips when unset.

### Success Criteria

- The Engine can depend only on SPI and inject mysqlobda for schema apply, object/link lifecycle, query, traverse, and transactions.
- Adding the MySQL dialect changed no SPI shape and no mapping semantics; sqliteobda's tests are unchanged.
- The mirrored matrix passes against a real MySQL 8.0 when `TEST_DB_URL` is set, and skips cleanly otherwise.

### Scope Boundaries

**Deferred for later**

- A shared-core refactor of sqliteobda/mysqlobda — until a third dialect or the duplication bites.
- Temporal history (`GetObjectAtVersion`/`GetObjectAtTime`) and bulk mutations — roadmap from 2026-08-21.
- Recursive CTE / variable-depth `Traverse`; assembling `Edges`/`Visited` from the chained JOIN.
- Sidecar-backed system columns — superseded by the direct-column approach this round.

**Outside this round**

- PostgreSQL and other dialects; MariaDB or MySQL below 8.0.
- ReBAC, consent, or authorization predicate injection.
- Cross-database joins and distributed transactions.

### Dependencies / Assumptions

- The shared `runtime/obda` planner, compiler, sqlast, and the `dialect.Dialect` interface already exist (realized via sqliteobda). mysqlobda reuses them; only the dialect adapter, provider, and MySQL-specific helpers are new.
- `github.com/go-sql-driver/mysql` is not yet in `runtime/go.mod` (verified); it must be added as a blank import.
- sqliteobda is the authoritative reference; its helper surface (`Classify`, `MappedTableStatements`, `InspectTable`/`TableExists`/`InspectIndexes`/`HasUniqueIndex`/`ProbeFTS5`) is what the MySQL dialect mirrors.
- MySQL 8.0+ features (recursive CTE, JSON, FULLTEXT, online index) are available; `RETURNING` is not.
- `TEST_DB_URL` points at a MySQL 8.0 instance with `parseTime` and `multiStatements` enabled (e.g. `root:...@tcp(host:3306)/test?charset=utf8mb4&parseTime=true&loc=UTC&multiStatements=true`).

### Outstanding Questions

**Deferred to Planning**

- Whether MySQL session variables (`sql_mode`, `time_zone`, `foreign_key_checks`) are set in `Open` or relied on via DSN params.
- The exact MySQL type mapping for ODL scalar types (boolean, int, long, string, decimal, datetime) and for the system columns.
- Whether an FTS probe analogous to `ProbeFTS5` is needed, or FULLTEXT is assumed present on 8.0.
- How Traverse tests lock "one SQL" without binding exact SQL text — mirror sqliteobda's `links_test.md` approach.

### Sources / Research

- `docs/brainstorms/2026-08-26-obda-traverse-chained-join-requirements.md` — Traverse chained-JOIN terminal behavior, the reference for R13/R14.
- `docs/brainstorms/2026-08-21-obda-mysql-storage-provider-requirements.md` — original MySQL/SPI vision; sidecar + full-SPI specifics superseded this round.
- `docs/plans/2026-08-21-001-feat-obda-sqlite-storage-provider-plan.md` — the sqlite provider plan, structural reference.
- `runtime/storage/sqliteobda/` — authoritative reference provider (`provider.go`, `schema.go`, `objects.go`, `links.go`, `query.go`, `transaction.go`).
- `runtime/obda/dialect/dialect.go` and `runtime/obda/dialect/sqlite/` — the `Dialect` interface and the sqlite adapter to mirror.
- `runtime/obda/planner.go` — shared planner (`PlanTraverse`, `PlanGetLinksJoin`), reused unchanged.
- `runtime/spi/provider.go`, `runtime/spi/transaction.go`, `runtime/spi/unimplemented.go` — the SPI to implement.
