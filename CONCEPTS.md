# Concepts

Shared domain vocabulary for this project — entities, named processes, and status concepts with project-specific meaning. Seeded with core domain vocabulary, then accretes as ce-compound and ce-compound-refresh process learnings; direct edits are fine. Glossary only, not a spec or catch-all.

## OBDA / Storage layer

### OBDA
Ontology-Based Data Access — this runtime's data-access style, in which an ontology of object types and link types is mapped onto physical tables, and reads/writes are planned against the ontology first and then compiled to SQL (or served in-process) against that mapping.

### Traverse
The storage-level primitive that walks a typed link path from a start object and returns two things: the terminal **Nodes** (objects at the last step only) and every **Edge** walked (deduplicated by link identity, so parallel paths and fan-out don't repeat an edge). Intermediate object payloads are deliberately not part of the result — the layer above batch-hydrates them from the Edges. By contract, terminal Nodes keep row semantics on SQL providers: a terminal reachable via two different links appears twice and paginates as two rows (the in-memory provider dedups them instead).
*Avoid:* Visited — the retired Go-only field that used to carry intermediate payloads inside the traversal result; it was deleted so topology (Edges) and payload hydration stay separate concerns.

### Junction link
A link stored as rows in its own dedicated link table, carrying identity, tenant, endpoint foreign keys, and optional properties. The default link shape.
*Avoid:* property link (when meaning a junction-table link as opposed to an inline one).

### Inline link
A link with no junction table: the foreign key lives as a plain column on one endpoint's own table (the host). It has no properties and no row of its own — its identity is derived from the host row's primary key, so the same host row backs at most one such link per link type and direction.

### Or-of-eq filter
The narrow batch-by-ids filter channel: an `Or` predicate accepted only over `eq` leaves (typically on the identity field). The eq-only restriction is a correctness lock, not a style choice — bound arguments follow placeholder appearance order in the rendered SQL, which only stays trivially correct when children are flat equality leaves.
