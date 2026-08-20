# Query IR

Query IR is the compile target for **all external reads** in the Go Runtime: GraphQL get / list / aggregate / search / nested `@link` / object-typed FK, REST GET by id, and REST follow. GraphQL SDL is a projection of Ontology IR, not the semantic core. Query IR sits between those projections and Engine.

This document is the shape of that IR. Implementation sequencing lives in `docs/plans/2026-08-20-002-feat-go-phase6-query-ir-traverse-plan.md`. Product rules live in `docs/brainstorms/2026-08-20-go-phase6-query-ir-traverse-requirements.md`.

---

## Place in the Runtime

`docs/design/draft.md` names three core intermediate representations:

| IR | Answers | Status |
|---|---|---|
| Ontology IR | What types, links, and computed fields exist | `runtime/ir` (ODL lowered) |
| Query IR | What read to run against those types | this document |
| Action IR | What mutation to validate and apply | not this phase |

```text
ODL ──► Ontology IR ──► GraphQL SDL / REST routes
                │
                ▼
         GraphQL / REST request
                │
                ▼
            Query IR
                │
                ▼
         query.Execute
                │
                ▼
              Engine ──► SPI
```

Projections construct Query IR. They do not call `StorageProvider`. Engine is the only persistence boundary.

Query IR is **not**:

- a GraphQL AST (schema still comes from Ontology IR)
- SPI `TraversalPath` exposed to clients (path vocabulary is `@link` field names)
- OBDA `SemanticQueryPlan` (`docs/design/open-foundry-obda-mapping-spec-v1.md` §71) — that compiles overlay SQL, a different artifact
- a whole-document optimizer that shares prefixes across a GraphQL operation

v1 constructs one tagged op per resolver (or per REST request) and runs `Execute`. Sharing a prefix across `a { b { c, d } }` is deferred.

---

## Algebra

A Query IR value is exactly one of the ops below. Go representation (struct with exclusive pointers vs interface) is an implementation choice; the discriminant is not.

```text
Op =
    Get      { type, id, computed? }
  | List     { type, filter, orderBy, page }
  | Aggregate{ type, filter, groupBy, fields }
  | Search   { type, query, fields?, filter, page }
  | Expand   { startType, startId, hops }
```

- **Get** — one object. `computed` is the LAZY field set (GraphQL: selected computed fields; REST GET: all LAZY, matching Phase 6).
- **List** — `QueryObjects` + Relay page. Filter/orderBy are already SPI types when they enter the op; GraphQL `FooFilter` conversion stays in the projection.
- **Aggregate** / **Search** — pass-through of existing Engine verbs.
- **Expand** — graph navigation from one start object along Ontology IR `RoleLinkNav` fields.

Object-typed properties (implicit FK, `RoleProperty` whose type is an ObjectType) are **Get**, not Expand. `PurchaseOrder.supplier` and `InventoryRecord.product` stay `GetObject` on the stored id.

Computed fields are not Expand. `Facility.currentUtilization` stays `ComputeField` / Get with computed options, even when selected next to nested `@link`.

---

## Expand

Expand is the only op that chooses between `GetLinks` and `Traverse`.

### Hop classification (GraphQL)

Classification is per `@link` field, using that field's child selection (graph-gophers `SelectedFieldNames` / `HasSelectedField`).

```text
on object A, field f (@link):
  if child selection of f contains no RoleLinkNav
      → hop mode = GetLinks          # 1-hop leaf (siblings allowed)
  else
      → hop mode = Traverse
        one TraversalPath per linear @link chain
        rooted at A, starting with f
```

Examples:

| Selection | Ops |
|---|---|
| `product { suppliers { name } }` | Expand GetLinks on `suppliers` |
| `a { b { name } c { name } }` | two GetLinks (`b`, `c`) |
| `facility { inventoryRecords { trackedProduct { name } } }` | one Traverse `inventoryRecords → trackedProduct` |
| `a { b { c {…} d {…} } }` | two Traverses `a→b→c` and `a→b→d` (no shared prefix) |
| `a { b { name } c { d { name } } }` | GetLinks on `b`; Traverse `a→c→d` |

REST follow does **not** use this table. Follow is always Traverse, including a one-step path.

### Path vocabulary

Client-facing paths are **Ontology IR field names** with `RoleLinkNav`. Compiler resolves each name on the type at that hop:

```text
field name ──► Field.Link.Type + Field.Link.Direction ──► spi.TraversalStep
```

Illegal at compile time (fail closed, do not call SPI):

- empty path
- name is a scalar, computed field, or FK property
- name is a LinkType (`InventoryAt`) rather than a nav field (`inventoryRecords`)
- name exists on another type but not on the type at this hop

Direction and link type never appear on the wire for follow. That keeps Query IR a semantic graph API, not a Graph Database API.

### Cardinality and caps

Many-side hops cap the frontier at **1000 per hop per start object** (same as Phase 6 `linkPageLimit`). SPI `TraversalOptions.Limit` only slices terminal `nodes`; Expand must truncate the frontier at each hop so intermediate MANY lists match GraphQL field caps.

ONE / MANY_TO_ONE nav fields assemble as a single object or null, not a list.

### Start existence

`Traverse` at the SPI layer does not require the start object to exist. Query IR does:

1. Get the start (same not-found as GraphQL get / REST GET: missing, soft-deleted, wrong tenant).
2. Only then Expand.

Otherwise follow could walk links of an object GraphQL already returns as `null`.

---

## Compile

### GraphQL

Each Engine-facing resolver builds an op and calls `Execute`:

| Resolver | Op |
|---|---|
| `foo(id)` | Get |
| `foos(filter, …)` | List |
| `fooAggregate` | Aggregate |
| `searchFoos` | Search |
| `@link` field | Expand (classified from child selection) |
| object-typed FK field | Get of the stored id |

Results of Expand are memoized on the request by `(startId, fieldName)`. Child `@link` resolvers **only read the memo**. A second `GetLinks` / `Traverse` for the same key is a bug (double-execute). Tests count Engine calls.

This is still Query IR: there is one executor. It is not a whole-operation plan. List items each Expand independently; this phase does not batch start ids.

Public SDL does not grow `Query.traverse` or `Type.follow`. Nested `@link` remains the GraphQL graph UX.

### REST

| Request | Op |
|---|---|
| `GET /api/v1/{lowerFirst(type)}/{id}` | Get (all LAZY computed) |
| `GET /api/v1/{lowerFirst(type)}/{id}/follow?path=f1,f2` | Get start, then Expand with a single Traverse path |

Follow response is the **terminal** public objects (`id`, not `_id`). Compare GraphQL 2-hop by **terminal id set**, not tree isomorphism: several inventory rows pointing at one product de-dupe in REST `nodes` and stay as multiple parents in GraphQL.

Errors:

| Condition | HTTP |
|---|---|
| missing / empty / wrong-tenant start | 404 `OBJECT_NOT_FOUND` |
| illegal path (see Path vocabulary) | 400 `INVALID_FOLLOW_PATH`, no SPI call |
| unknown type in the URL | 404 (existing GET behavior) |

---

## Execute

```text
Execute(ctx, Op) → Result
  Get       → Engine.GetObject / GetObjectOpts
  List      → Engine.QueryObjects
  Aggregate → Engine.AggregateObjects
  Search    → Engine.SearchObjects
  Expand    → Engine.GetLinks and/or Engine.Traverse
              then assemble
```

`query` depends on Engine + Ontology IR. It does not import a storage provider.

Engine `Traverse` is a pass-through to SPI, same as `GetLinks`. Classification, path resolution, caps, start-existence, and tree assembly belong in Query IR Execute (and the GraphQL memo), not in Engine.

---

## Assemble

SPI `TraversalResult` after the additive contract:

| Field | Meaning |
|---|---|
| `nodes` | objects at the **last** step only (unchanged) |
| `edges` | every link walked |
| `visited` | **strict intermediates**: not start, not terminals. Empty on a 1-hop traverse |
| `totalCount` | unchanged (terminal count) |

Today `nodes` = last step is documented only on memory implementations, not on the SPI type. The type comments must state all four fields so GraphQL assembly does not depend on a backend comment.

Assembly:

1. Index `visited` and `nodes` by `_id`.
2. Walk `edges` to attach children to parents for each hop in the path.
3. If two Traverses share an intermediate id, keep one object.
4. Do **not** collapse a later hop just because an id appeared earlier (`product { suppliers { products { sku } } }` must still show the second-layer `products`).

GetLinks assembly stays “links → target GetObject”, with the 1000 cap and missing-target skip already used in `runtime/api/node.go`.

---

## Worked examples

**1-hop GraphQL (must stay GetLinks)**

```graphql
{ product(id: "p1") { suppliers { name } } }
```

```text
Expand{ start: Product/p1, hops: [GetLinks suppliers] }
```

**2-hop GraphQL (must Traverse)**

```graphql
{ facility(id: "f1") {
    inventoryRecords { quantity trackedProduct { name } }
  }
}
```

`inventoryRecords` and `trackedProduct` are `@link` nav fields (FK `product` / `facility` remain Get). Path:

```text
Facility --InventoryAt inbound--> InventoryRecord --InventoryOf outbound--> Product
Expand{ Traverse [Admitted-style steps resolved from field names] }
```

If seed has the FK but no `InventoryOf` link, `trackedProduct` is null and `product { name }` still resolves via Get.

**REST follow (always Traverse)**

```http
GET /api/v1/facility/f1/follow?path=inventoryRecords,trackedProduct
GET /api/v1/product/p1/follow?path=suppliers
```

The second call is one-step Traverse, not GetLinks.

---

## Non-goals

- GraphQL root `traverse` / ObjectType `follow`
- Whole-document Query IR, shared-prefix planning, DataLoader over many list roots
- AuthZ / consent / field redaction inside Execute
- SPARQL / Cypher front ends (they should compile to this IR later, not bypass it)
- TS GraphQL planner (SPI `visited` still updates so the contract does not fork)
- Capability-gated omission of follow when `supportsGraphTraversal` is false

---

## Sources

- `docs/design/draft.md` — Ontology IR + Query IR + Action IR
- `docs/brainstorms/2026-08-20-go-phase6-query-ir-traverse-requirements.md`
- `docs/plans/2026-08-20-002-feat-go-phase6-query-ir-traverse-plan.md`
- `docs/open-foundry-spec-v2.md` §2.1.3 `@link`, §3.1 `traverse`, §8.1.1 generated Query
- `runtime/ir/ontology.go` — `RoleLinkNav` vs `RoleProperty` vs `RoleComputed`
- `runtime/spi/ontology.go` — `TraversalPath` / `TraversalResult`
- `runtime/api/node.go` — current per-field `GetLinks` + `GetObject`
