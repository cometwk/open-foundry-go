---
title: "Traverse converged on Nodes+Edges — per-hop edge projection and engine-layer batch hydration"
date: 2026-09-13
category: docs/solutions/architecture-patterns
module: runtime
problem_type: architecture_pattern
component: service_object
severity: high
applies_when:
  - Aligning a Go SPI with its TypeScript counterpart and deciding whether to delete or keep a vestigial field
  - Returning terminal objects and per-hop link rows from one chained-JOIN query via column-bucket projection (TraverseLayout offsets)
  - Batch-hydrating intermediate objects from Edge endpoints with one or-of-eq QueryObjects per endpoint type to eliminate per-object GetObject N+1
  - Synthesizing Edges from host inline-FK columns when a hop has no link table row, with identity matching the GetLinks derivation
  - Removing a storage backend and converging bootstrap on fewer providers (mysql|memory)
symptoms:
  - SQL backends returned empty Edges so multi-hop GraphQL expansion and REST follow were broken on mysql
  - GraphQL one-hop leaf expansion issued per-object GetObject queries (N+1 loop in runtime/query/execute.go)
  - Go TraversalResult carried a Visited field absent from the TS SPI, splitting topology from payload across provider and engine
related_components:
  - database
  - testing_framework
tags:
  - traverse
  - nodes-edges
  - spi-contract
  - chained-join
  - column-bucket-projection
  - batch-hydration
  - n-plus-1
  - mysql-obda
---

# Traverse converged on Nodes+Edges — per-hop edge projection and engine-layer batch hydration

## Context

`Traverse` walks an ontology path (`A → B → C`) as a single chained-JOIN SELECT, but before PR #8 its MySQL implementation only projected the **terminal** object's columns. `Edges` came back as an empty slice and the Go-only `TraversalResult.Visited` field (an artifact of the TS-to-Go port — `packages/spi` never had `visited`) was the sole carrier of intermediate payloads. The consequences, per the plan (`docs/plans/2026-09-11-002-refactor-traverse-nodes-edges-plan.md`):

- Multi-hop GraphQL expansion and REST follow on the SQL backend were broken (the e2e gold path had to skip mysql).
- The one-hop leaf expansion path hydrated neighbors with a per-object `GetObject` loop — roughly one query per neighbor.
- Two parallel hydration protocols (provider-side `Visited`, engine-side expansion) drifted between Go and TS SPI shapes.

(session history) The failure was first isolated in a 2026-09-11 e2e session: MySQL returned an empty two-hop `branches → readers` tree while 1-hop `@link`, list/aggregate, and REST get all passed; debugging through `Traverse(book → AvailableAt → RegisteredAt)` converged on the root cause — mysqlobda `Traverse` populated neither `Edges` nor `Visited`, while `query.assemblePath` needs per-hop edges to stitch both the REST follow response and the GraphQL two-hop tree. At the time the gap was contained, not fixed: the two-hop subtest was skipped on MySQL with the divergence documented (PR #6). PR #8 then removed that divergence class entirely. A same-day analysis also pinned down Traverse's external role — it is a storage-SPI primitive consumed via ODL nested `@link` → Query IR compilation, never a public GraphQL root field — which constrained the refactor target: the SPI contract only needs to serve what the IR compiler consumes.

PR #8 (merged to main, merge commit `7298f21`) converged the contract on **Nodes + Edges**: `TraversalResult` dropped `Visited` entirely and now carries only `Nodes / Edges / TotalCount` (runtime/spi/ontology.go:248-257, doc comment explicitly notes shape parity with the TS SPI). Intermediate payloads became a **derived, engine-layer concern**: `Traverse` returns topology (`Edges`), and the engine batch-hydrates whatever objects the tree still needs. The sqlite OBDA provider was deleted wholesale; bootstrap's `DB_DRIVER` now accepts only `mysql` or `memory` (runtime/bootstrap/conf.go). One caveat to the "sqlite is gone" story: `runtime/storage/sqliteobda` and `runtime/obda/dialect/sqlite` no longer exist (only `memory` and `mysqlobda` remain under runtime/storage/), but `modernc.org/sqlite` still appears as an *indirect* dependency in runtime/go.mod.

## Guidance

The refactor is a reusable playbook for "make the query return what the caller actually needs, then derive the rest in one batch" — applied across four layers. Each practice below is grounded in the merged tree.

### 1. Delete the contract field instead of keeping a ghost

`Visited` was removed from the struct, so every stale reference failed to compile and had to be consciously rewritten rather than silently reading a forever-empty field. This is the aggressive option, but it is the safe one when a field's only producer is being eliminated anyway: `grep -rn "\.Visited" runtime/` is a stronger invariant than "nobody populates it anymore."

### 2. Make topology a first-class output: per-hop column buckets, scanned positionally

`PlanTraverse` now appends one extra column **bucket** per hop after the terminal columns, and returns a `TraverseLayout` describing each bucket's role, alias, and column offset (runtime/obda/planner.go, `PlanTraverse` and the `TraverseLayout`/`TraverseBucket` types). The hop descriptor carries the bucket source:

- junction hops: `TraverseHop.LinkSelect` = the hop's link-binding `SelectColumns`, projected at alias `l<i>`;
- inline-FK hops: `TraverseHop.HostSelect` = the **host** binding's columns (host alias is the previous hop alias when `FKOnPrev`, else this hop's target alias).

Crucially, **only the SELECT list changed** — JOIN shape, WHERE, ORDER, and args are untouched, so args stay `[tenant, startID]`. Row scanning stays purely positional: one `scan(rows, totalCols)` per row, then each bucket is sliced by `dest[b.Offset : b.Offset+len(b.Cols)]` and zipped into a `bizMap` (runtime/storage/mysqlobda/links.go, Traverse row loop). Positional scanning is what makes duplicate column names across buckets (every bucket has its own `id`, `tenant_id`, ...) a non-issue — column *names* are per-bucket metadata, never looked up globally.

### 3. Reuse the canonical assemblers; dedup only the dedupable

Each bucket row is fed to the **same assembler** the `GetLinks` path uses — `assembleLink` for junction buckets, `assembleInlineLink` for inline buckets — so an edge's `_id`, version, and system fields are byte-identical to what `GetLinks` would return for the same link. Do not write a parallel edge assembler "just for traverse" — the two exits will drift.

Cartesian fan-out repeats hop rows across the result set, so `Edges` dedups by `linkType:linkID`. But `Nodes` and `TotalCount` deliberately keep raw **row semantics**: a terminal reachable via two parallel links appears twice and paginates consistently (plan R5/R12, guarded by the existing duplicate-terminal test). Dedup and row semantics coexist by design — "optimizing" one into the other is a contract break.

### 4. Hydrate at the engine layer, through the existing query path

The engine, not the provider, turns `Edges` back into object payloads. `expandTraverse` collects terminal ids from `tr.Nodes`, then calls `hydrateEdges` over `tr.Edges` (runtime/query/execute.go, `expandTraverse`). `hydrateEdges` buckets the union of edge **endpoints by their own type** (so a link type repeating across hops in self-referential chains stays correct — runtime/query/hydrate.go, `collectEndpoint`) and issues **one `QueryObjects` per endpoint type** via `hydrateByIDs`, an or-of-eq filter on `_id` (runtime/query/hydrate.go:12-28). The one-hop leaf path (`expandGetLinks`) funnels into the same `hydrateByIDs`, replacing its old per-neighbor `GetObject` loop.

Three properties fall out of going through ctx-bound `QueryObjects` rather than a new SPI method:

- **Tenant isolation and `deleted_at` semantics come for free** — the call carries the `RequestContext` and `QueryOptions.IncludeDeleted`, so both providers apply the same visibility rules as every other read.
- **No SPI surface growth.** The only provider change needed was teaching the mysql filter path the `or` predicate the SPI already promised (plan KTD1).
- **Bounded SQL.** One query per intermediate type, independent of neighbor count. (Watch the provider's page clamp: `hydrateByIDs` passes `Limit: len(ids)`, which `pageLimitOffset` caps at `MaxPageLimit` — as of this writing that constant is *temporarily* set to `10` for testing, with the intended `1000` in a trailing comment at runtime/storage/mysqlobda/query.go:106-111; a batch larger than the clamp silently prunes branches, since misses are tolerated by design. Keep the clamp comfortably above realistic fan-out.)

### 5. Narrow extensions, locked by the args-order trap

The or-of-eq channel was added as a deliberately minimal slice, in three coordinated places:

- planner `compileFilter` accepts `Or` **only over `eq` leaves**, everything else still errors (runtime/obda/planner.go:551-567);
- mysql `translateFilter` maps each child's logical field (including `_id` → first identity column) the same way (runtime/storage/mysqlobda/query.go, `translateFilter` and `filterColumn`);
- the renderer's `or` case joins parenthesized children with ` OR ` and rejects empty children (runtime/obda/dialect/mysql/dialect.go, `renderPred` "or" case).

The eq-leaves-only restriction is not aesthetic: the mysql renderer emits bare `?` placeholders and binds args in textual appearance order — `sqlast.Param.Position` is decorative there. Or children render in array order, so keeping children as flat eq leaves is what makes the args slice trivially correct. The planner comment states this invariant outright.

### 6. Delete dead providers when the contract moves

When a provider can no longer meet the contract, deleting it beats maintaining a degraded twin. sqlite went from "rejects inline links" (a documented divergence) to "does not exist"; its CLI tests moved to the in-process `memory` backend so they still run without a real database (runtime/bootstrap/conf.go, `openBackend`). The memory provider's BFS `Traverse` needed only to stop populating the deleted `Visited` field — it already returned real `Edges`.

## Why This Matters

- **SPI parity by construction.** Go and TS `TraversalResult` now have the same shape; there is no Go-only hydration protocol baked into the traversal primitive. Topology (Edges) and payload (Nodes) are cleanly separated concerns.
- **N+1 eliminated at the source.** Leaf expansion issues `GetLinks` once plus one batched `QueryObjects` per neighbor type — the per-neighbor `GetObject` calls are gone from both expand modes.
- **One hydration path, two entry points.** Multi-hop (Traverse mode) and one-hop leaf (`GetLinks` mode) expansions share `hydrateByIDs`, so visibility, tenant, and error semantics cannot drift between them.
- **Consistent partial trees under truncation.** `Traverse` runs with `Limit: HopCap` (1000, runtime/query/ir.go:11), and `Edges`, `Nodes`, and the hydration ids all derive from that same row window — a truncated fan-out yields a coherent partial tree rather than nodes with missing parents or parents with missing children (plan R12; comment in runtime/query/execute.go, `expandTraverse`).
- **Half the SQL surface to maintain.** One SQL provider (mysql) plus one reference in-memory provider; the e2e gold paths exercise the same two-hop assertions on whichever backend `TEST_DB_URL` selects (runtime/e2e/graphql_test.go and runtime/e2e/rest_test.go "two hop" subtests, backend selection in runtime/e2e/helper_test.go).

## When to Apply

- A provider query returns "close to" what callers need, and the gap is bridgeable by **projecting more columns** from tables already joined — extend the projection and return a layout; do not add round trips.
- Derived data (payloads for things the query already identifies) is needed at a layer **above** the provider — hydrate in batches through the existing read API instead of widening the provider protocol.
- An eq-only SQL filter path needs batch-by-ids reads — add a guarded or-of-eq slice rather than a general expression evaluator, and keep the guard tied to the args-ordering constraint.
- A cross-language SPI has drifted (a field exists in one port only) and its producers are being removed — delete the field and let the compiler enumerate the fallout.
- Two backends exist but one cannot meet a new contract — consider deleting the laggard instead of forever encoding its divergence as conditionals.
- You are scanning wide rows with repeated column names across logical groups — slice by recorded offsets (`bizMap(dest[o:o+n], cols)`), never by column-name lookup.

## Examples

### Example 1 — N+1 per-neighbor loop → one batched read

Before (pre-PR #8, `expandGetLinks` in runtime/query/execute.go — one `GetObject` per neighbor):

```go
for _, link := range page.Items {
    tid := neighborID(link, startID, steps[0].Direction)
    if tid == "" || seen[tid] {
        continue
    }
    seen[tid] = true
    obj, err := eng.GetObject(ctx, target, tid) // N queries for N neighbors
    if err != nil {
        if errors.Is(err, spi.ErrObjectNotFound) {
            continue
        }
        return nil, err
    }
    kids = append(kids, obj)
}
```

After (runtime/query/execute.go, `expandGetLinks` — collect ids, one batched read; misses prune silently):

```go
seen := map[string]bool{}
ids := make([]string, 0, len(page.Items))
for _, link := range page.Items {
    tid := neighborID(link, startID, steps[0].Direction)
    if tid == "" || seen[tid] {
        continue
    }
    seen[tid] = true
    ids = append(ids, tid)
    if len(ids) >= HopCap {
        break
    }
}
// One batched read replaces the per-neighbor GetObject loop; missing
// objects prune silently, query errors propagate.
found, err := hydrateByIDs(eng, ctx, target, ids, false)
```

`hydrateByIDs` itself is the reusable batch-by-ids primitive (runtime/query/hydrate.go:12-28):

```go
ors := make([]spi.FilterExpression, 0, len(ids))
for _, id := range ids {
    ors = append(ors, spi.FilterExpression{Field: spi.FieldID, Operator: "eq", Value: id})
}
page, err := eng.QueryObjects(ctx, typ, spi.FilterExpression{Or: ors}, &spi.QueryOptions{
    Limit:          len(ids),
    IncludeDeleted: includeDeleted,
})
```

### Example 2 — terminal-*type*-bucket bug → terminal-*ids* fix (post-review fix in PR #8)

First version of `hydrateEdges` removed the terminal type's entire bucket, because `tr.Nodes` already carries terminal payloads. That silently dropped **self-typed intermediates**: in `A → B → C → B` or `User → friend → User → friend → User`, hop-1 `User` objects share the terminal type — the whole bucket vanished and `neighbors()` could not find them in the hydrated map, truncating the assembled tree without any error.

Before (PR #8 review fix, coarse delete):

```go
// The terminal type's bucket is dropped — tr.Nodes already carries terminal payloads
delete(buckets, terminalType)
```

After (runtime/query/hydrate.go, `hydrateEdges` — per-id removal; caller builds `terminalIDs` from `tr.Nodes` in runtime/query/execute.go, `expandTraverse`):

```go
if termBucket := buckets[terminalType]; termBucket != nil {
    for id := range terminalIDs {
        delete(termBucket, id)
    }
    if len(termBucket) == 0 {
        delete(buckets, terminalType)
    }
}
```

The start object's own id is handled the same way — remove the id, never the type bucket. Rule of thumb: **dedupe hydration input by identity, never by type**, whenever a path can revisit a type.

### Example 3 — MySQL derived-table count gotcha (Error 1060)

`Traverse` computes `TotalCount` by wrapping the same SELECT in `SELECT COUNT(*) FROM (...) AS q`. Once per-hop buckets were added, the derived table contained duplicate column names (`s1.id` and `l0.id` both derive to `id`) — MySQL rejects that with Error 1060 (duplicate column name). The count variant therefore projects a single terminal identity column and drops LIMIT/ORDER (runtime/storage/mysqlobda/links.go:466-476):

```go
countSel := *sel
countSel.Limit = nil
countSel.Order = nil
// The count subquery is a MySQL derived table, which rejects duplicate
// column names (s1.id and l0.id both derive to "id"). Projecting a single
// terminal column keeps the derived-table names unique; the count is row
// count either way.
countSel.Columns = []sqlast.Expr{sqlast.Identifier{
    Qualifier: layout.NodeAlias,
    Name:      firstCol(terminal.IdentityColumns),
}}
```

Generalize: any `SELECT ... FROM (subquery) AS q` on MySQL must keep the subquery's projection column-name-unique — if the outer query only counts, shrink the inner projection instead of aliasing every duplicate.

## Related

- [mysql-fulltext-search-and-eq-only-filter](../design-patterns/mysql-fulltext-search-and-eq-only-filter.md) — extends its fail-closed filter rule: `Or` is now accepted over eq leaves as the batch-by-ids shape introduced here (planner `compileFilter` + mysqlobda `translateFilter`); non-eq leaves and `And`/`Not` still fail closed with `ErrInvalidMapping`.
- [mysql-port-divergence-of-active-unique-index](../design-patterns/mysql-port-divergence-of-active-unique-index.md) — lineage context for the same mysqlobda provider whose junction link tables the Edges assembler reads; its sqlite reference-implementation paths were deleted by this refactor's backend removal.
