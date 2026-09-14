# Concepts

**注意: 本文采用 中/英 双语编写**

Shared domain vocabulary for this project — entities, named processes, and status concepts with project-specific meaning. Seeded with core domain vocabulary, then accretes as ce-compound and ce-compound-refresh process learnings; direct edits are fine. Glossary only, not a spec or catch-all.

> 项目共享领域词汇表 —— 收录在本项目有特定含义的实体、命名流程与状态概念。初始由核心领域词汇播种，随 ce-compound 与 ce-compound-refresh 处理学习沉淀而增长；直接编辑亦可。只是词汇表，不是规范，也不是大杂烩。词条名保留英文，作为跨代码 / 文档 / 对话引用的规范键。

## OBDA / Storage layer

### OBDA
Ontology-Based Data Access — this runtime's data-access style, in which an ontology of object types and link types is mapped onto physical tables, and reads/writes are planned against the ontology first and then compiled to SQL (or served in-process) against that mapping.

> Ontology-Based Data Access（基于本体的数据访问）—— 本运行时的数据访问风格：把对象类型与链接类型组成的本体映射到物理表，读写先按本体规划，再依据映射编译成 SQL（或在进程内直接执行）。

### Traverse
The storage-level primitive that walks a typed link path from a start object and returns two things: the terminal **Nodes** (objects at the last step only) and every **Edge** walked (deduplicated by link identity, so parallel paths and fan-out don't repeat an edge). Intermediate object payloads are not part of the default result — the layer above batch-hydrates them from the Edges (but see Projection-aware Traverse for the opt-in extension). By contract, terminal Nodes keep row semantics on SQL providers: a terminal reachable via two different links appears twice and paginates as two rows (the in-memory provider dedups them instead).
*Avoid:* Visited — the retired Go-only field that used to carry intermediate payloads inside the traversal result; it was deleted so topology (Edges) and payload hydration stay separate concerns. The retirement killed the *uniform* payload field, not the idea of the SQL projecting intermediate columns at all.

> 存储层原语：从起点对象沿类型化链接路径行走，返回两样东西 —— 终点 **Nodes**（仅最后一步的对象）与走过的每一条 **Edge**（按边身份去重，并行路径与扇出不会重复同一条边）。中间对象负载默认不在结果中 —— 由上层从 Edges 批量水合（可选扩展见 Projection-aware Traverse）。按契约，SQL provider 上终点 Nodes 保持行语义：经两条不同链接可达的终点会出现两次、按两行参与分页（内存 provider 则做去重）。
> *避免:* Visited —— 已退役的 Go 专用字段，曾在遍历结果中携带中间负载；删除它是为了让拓扑（Edges）与负载水合保持关注点分离。退役消灭的是*统一*负载字段，而不是「SQL 不得读取它已经在 JOIN 的中间列」。

### Junction link
A link stored as rows in its own dedicated link table, carrying identity, tenant, endpoint foreign keys, and optional properties. The default link shape.
*Avoid:* property link (when meaning a junction-table link as opposed to an inline one).

> junction 链接 —— 存储在自身专属链接表中的链接，携带身份、租户、端点外键与可选属性。默认的链接形态。
> *避免:* property link（当意指「junction 表链接」而非 inline 链接时）。

### Inline link
A link with no junction table: the foreign key lives as a plain column on one endpoint's own table (the host). It has no properties and no row of its own — its identity is derived from the host row's primary key, so the same host row backs at most one such link per link type and direction.

> inline 链接 —— 没有 junction 表的链接：外键是某一端点自身表（宿主表）上的普通列。它没有属性、没有独立的数据行 —— 身份由宿主行主键派生，因此同一宿主行对每个链接类型与方向至多支撑一条这样的链接。

### Or-of-eq filter
The narrow batch-by-ids filter channel: an `Or` predicate accepted only over `eq` leaves (typically on the identity field). The eq-only restriction is a correctness lock, not a style choice — bound arguments follow placeholder appearance order in the rendered SQL, which only stays trivially correct when children are flat equality leaves.

> 窄化的按 id 批量过滤通道：只接受 `eq` 叶子构成的 `Or` 谓词（通常作用在身份字段上）。eq-only 限制是正确性锁，不是风格选择 —— 绑定参数遵循占位符在渲染 SQL 中出现的顺序，只有当子节点全部是扁平等值叶子时，这个顺序才平凡地保持正确。

### Projection-aware Traverse
The agreed extension direction (docs/plans/2026-09-14-001-perf-traverse-expand-sql-optimization-plan.md): Traverse accepts optional, per-selection projection hints — terminal fields, intermediate-type fields, edge system columns — and the compiled SQL projects those columns on the tables it already JOINs, returning them scoped in the result. Distinguishes the public default contract (Nodes + Edges, no intermediate payload) from what the internal SQL may carry: "no uniform Visited" never meant "the SQL must not read intermediate columns it is already joining". Callers that pass no hints keep the hydration-based behavior unchanged.

> 投影感知 Traverse —— 已商定的扩展方向（docs/plans/2026-09-14-001-perf-traverse-expand-sql-optimization-plan.md）：Traverse 接受可选的、按 GraphQL selection 的投影提示（终点字段、中间类型字段、边系统列），编译出的 SQL 在本来就要 JOIN 的表上顺带投影这些列，并在结果中按范围带回。它区分两件事：公共默认契约（Nodes + Edges，无中间负载）与内部 SQL 可以携带的内容 ——「没有统一 Visited」从不等于「SQL 不得读取它正在 JOIN 的中间列」。不传提示的调用方保持现有的基于水合的行为不变。

### Overflow probe
The truncation guard adopted for traversal paging: the data page queries `LIMIT N+1` instead of `N`; if `N+1` rows come back, the underlying result set exceeded the hard cap and the caller gets an explicit hard-limit error rather than a silently truncated tree. GetLinks already uses the same +1 trick to derive `HasNextPage`; for Traverse the probe replaces silent pruning.

> 探顶检测 —— 遍历分页采用的截断防护：数据页用 `LIMIT N+1` 而非 `N` 查询；若返回了 `N+1` 行，说明底层结果集超出硬上限，调用方收到显式的硬上限错误，而不是一棵静默截断的树。GetLinks 已经用同样的 +1 技巧推导 `HasNextPage`；对 Traverse 而言，探针取代静默剪枝。