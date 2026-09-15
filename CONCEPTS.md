# Concepts

**注意: 本文采用 中/英 双语编写**

Shared domain vocabulary for this project — entities, named processes, and status concepts with project-specific meaning. Seeded with core domain vocabulary, then accretes as ce-compound and ce-compound-refresh process learnings; direct edits are fine. Glossary only, not a spec or catch-all.

> 项目共享领域词汇表 —— 收录在本项目有特定含义的实体、命名流程与状态概念。初始由核心领域词汇播种，随 ce-compound 与 ce-compound-refresh 处理学习沉淀而增长；直接编辑亦可。只是词汇表，不是规范，也不是大杂烩。词条名保留英文，作为跨代码 / 文档 / 对话引用的规范键。

## OBDA / Storage layer

### OBDA
Ontology-Based Data Access — this runtime's data-access style, in which an ontology of object types and link types is mapped onto physical tables, and reads/writes are planned against the ontology first and then compiled to SQL (or served in-process) against that mapping.

> Ontology-Based Data Access（基于本体的数据访问）—— 本运行时的数据访问风格：把对象类型与链接类型组成的本体映射到物理表，读写先按本体规划，再依据映射编译成 SQL（或在进程内直接执行）。

### Direct-native identity
The rule that the SPI object id is the raw value in the mapped identity column — one column, no type envelope. The Engine mints that value when the mapping says generated; storage does not re-encode it, and HTTP passes it through. Link type is recovered from the mapping plus direction, never by decoding the id.

> Direct-native identity —— SPI 对象 id 就是映射身份列里的裸值：恰好一列，没有类型信封。mapping 声明 generated 时由 Engine 铸造该值；存储不再二次编码，HTTP 原样透传。链接类型从 mapping 加方向推导，绝不从 id 解码。

### Physical expectation
The dialect-neutral picture of mapped tables derived from a compiled mapping: required columns, cardinality unique keys, and declared full-text columns. DDL rendering, optional init, and ApplySchema verification all compare against this picture. SQL types, quoting, generated columns, and index names stay in the dialect.

> 物理期望 —— 从已编译映射派生的、与方言无关的表图景：必需要列、基数唯一键、已声明的全文列。DDL 渲染、可选建表、以及 ApplySchema 校验都对照这张图。SQL 类型、引号、生成列与索引名留在方言里。

### Junction link
The storage-level primitive that walks a typed link path from a start object and returns two things: the terminal **Nodes** (objects at the last step only) and every **Edge** walked (deduplicated by link identity, so parallel paths and fan-out don't repeat an edge). Intermediate object payloads are not part of the default result — the layer above batch-hydrates them from the Edges (but see Projection-aware Traverse for the opt-in extension). By contract, terminal Nodes keep row semantics on SQL providers: a terminal reachable via two different links appears twice and paginates as two rows (the in-memory provider dedups them instead).
*Avoid:* Visited — the retired Go-only field that used to carry intermediate payloads inside the traversal result; it was deleted so topology (Edges) and payload hydration stay separate concerns. The retirement killed the *uniform* payload field, not the idea of the SQL projecting intermediate columns at all.

> 存储层原语：从起点对象沿类型化链接路径行走，返回两样东西 —— 终点 **Nodes**（仅最后一步的对象）与走过的每一条 **Edge**（按边身份去重，并行路径与扇出不会重复同一条边）。中间对象负载默认不在结果中 —— 由上层从 Edges 批量水合（可选扩展见 Projection-aware Traverse）。按契约，SQL provider 上终点 Nodes 保持行语义：经两条不同链接可达的终点会出现两次、按两行参与分页（内存 provider 则做去重）。
> *避免:* Visited —— 已退役的 Go 专用字段，曾在遍历结果中携带中间负载；删除它是为了让拓扑（Edges）与负载水合保持关注点分离。退役消灭的是*统一*负载字段，而不是「SQL 不得读取它已经在 JOIN 的中间列」。

### GetLinks
The storage-level primitive that returns the links of one typed hop from a start object. Distinct from Traverse, which walks a multi-step path in one call. GraphQL one-hop leaf `@link` fields compile to GetLinks; REST follow never does, even for a single hop.

> 存储层原语：从起点对象返回一跳类型化链接。区别于 Traverse（一次走完多步路径）。GraphQL 的一跳叶子 `@link` 字段编译为 GetLinks；REST follow 即使只有一跳也不走 GetLinks。

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
The opt-in Traverse extension: callers pass per-type field lists; the compiled SQL projects those columns on tables it already JOINs and returns them as hop-scoped payloads so Expand can skip hydrating those types. Distinguishes the public default contract (Nodes + Edges, no intermediate payload) from what internal SQL may carry: "no uniform Visited" never meant "the SQL must not read intermediate columns it is already joining". Callers that pass no hints keep the hydration-based behavior unchanged. A present type with an empty field list is a skeleton (identity only) so a hop that was walked but not selected still stitches the tree.

> 投影感知 Traverse —— 可选扩展：调用方传入按类型的字段清单；编译出的 SQL 在本来就要 JOIN 的表上顺带投影这些列，并以跳范围负载带回，使 Expand 不必再水合这些类型。它区分两件事：公共默认契约（Nodes + Edges，无中间负载）与内部 SQL 可以携带的内容 ——「没有统一 Visited」从不等于「SQL 不得读取它正在 JOIN 的中间列」。不传提示的调用方保持现有的基于水合的行为不变。某类型在提示中出现但字段列表为空时，只合成身份骨架，以便走过但未选中字段的一跳仍能拼上树。

### Overflow probe
The truncation guard for Expand paging: the data page queries one row past the hard cap; if that probe row comes back, the caller gets an explicit hard-limit error rather than a silently truncated tree. Traverse applies the probe in the provider. GetLinks still uses the same +1 trick to derive `HasNextPage`; Expand treats hop-cap `HasNextPage` as the same hard-limit error, including when duplicate links fill the window.

> 探顶检测 —— Expand 分页的截断防护：数据页在硬上限之外再取一行；若探针行返回，调用方收到显式硬上限错误，而不是一棵静默截断的树。Traverse 在 provider 内做探顶。GetLinks 仍用同样的 +1 技巧推导 `HasNextPage`；Expand 把跳上限上的 `HasNextPage` 视为同一硬上限错误，即使重复链接填满了窗口。

## Runtime / Ontology IR

### ODL
Open Foundry's schema definition language — a GraphQL-SDL dialect whose custom directives declare ontology structure (object types, link types, actions, computed and link-navigation fields) rather than a GraphQL API surface. Parsed in syntax-only mode (no GraphQL semantic validation) so the custom directives are accepted; their meaning is assigned by the lowerer into Ontology IR, not by the parser.

> Open Foundry 的模式定义语言 —— 一种 GraphQL-SDL 方言，其自定义指令声明的是本体结构（对象类型、链接类型、动作、计算字段与链接导航字段），而非 GraphQL API 表面。以纯语法模式解析（不做 GraphQL 语义校验），从而接受自定义指令；其含义由 lowerer 在降低到 Ontology IR 时赋予，而非由解析器赋予。

### Ontology IR
The runtime's parser-free intermediate representation of the TBox — the types and their relationships (object types, link types, actions, enums, interfaces, scalars) with field roles already resolved. It is the stable semantic core that every downstream projection (storage schema, GraphQL SDL, OpenFGA tuples) binds to without re-reading the SDL or re-shaping storage JSON. The IR package imports no parser types; a parser upgrade or swap touches only the parse/lower layers and cannot ripple into the semantic core.

> 运行时中不依赖解析器的中间表示，承载 TBox —— 类型及其关系（对象类型、链接类型、动作、枚举、接口、标量），且字段角色已解析完毕。它是稳定的语义核心，每个下游投影（存储模式、GraphQL SDL、OpenFGA 元组）都绑定于它，无需重读 SDL 或重塑存储 JSON。IR 包不导入任何解析器类型；解析器升级或替换只影响 parse/lower 层，不会波及语义核心。

### FieldRole
The semantic role assigned to a field at ODL-lower time — one of Property, Primary, Param, LinkNav, or Computed. Once lowered, the role is the sole input to what each projection does with the field; projections never re-read raw directives. This collapses directive ambiguity once: if every projection re-dispatched the primary/link/computed directives independently, they could disagree.

> 在 ODL 降低时赋予字段的语义角色 —— Property、Primary、Param、LinkNav 或 Computed 之一。一旦降低完成，角色就是每个投影处理该字段的唯一输入；投影不再重读原始指令。这把指令歧义一次性消解：若每个投影各自重新分派 primary/link/computed 指令，它们可能给出不一致的解释。

### StorageProvider (SPI)
The runtime's storage contract — an interface spanning schema lifecycle, object/link CRUD, queries, traversal, transactions, and health. A concrete provider embeds an unimplemented stub enforced by an unexported interface method, so the interface cannot be satisfied accidentally; every non-overridden method returns a sentinel error carrying the method name. A backend can ship schema-only and be honest about every other surface.

> 运行时的存储契约 —— 一个涵盖模式生命周期、对象/链接 CRUD、查询、遍历、事务与健康的接口。具体 provider 嵌入一个由未导出接口方法强制的 unimplemented 桩，使接口无法被意外满足；每个未覆写的方法返回一个携带方法名的哨兵错误。后端可以只实现模式部分，对其余表面保持诚实。

## Runtime / Query IR

### Query IR
The runtime's tagged-op intermediate representation of a single read. GraphQL field resolvers and REST handlers compile one Get, List, Aggregate, Search, or Expand per request; Execute is the only path from those ops to the Engine. Projections never call storage directly. Distinct from Ontology IR, which is the TBox — Query IR is the per-read compile target that binds to it.

> 运行时中单次读的带标签操作中间表示。GraphQL 字段 resolver 与 REST 处理器把每次请求编译成一个 Get、List、Aggregate、Search 或 Expand；Execute 是这些操作到达 Engine 的唯一路径。投影层不直接调用存储。区别于承载 TBox 的 Ontology IR —— Query IR 是绑定于它的每次读的编译目标。

### Expand
The Query IR operation that navigates declared `@link` fields from one start object. A one-hop leaf (the child selection contains no nested `@link`) compiles to GetLinks; any deeper linear path, and REST follow of any length, compiles to Traverse. Forks do not share prefixes — each full linear path is its own Traverse. Implicit foreign-key object fields compile to Get, not Expand.

> Query IR 操作：从起点对象沿已声明的 `@link` 字段导航。一跳叶子（子选择集不再嵌套 `@link`）编译为 GetLinks；任何更深的线性路径，以及任意长度的 REST follow，都编译为 Traverse。分叉不共享前缀 —— 每条线性全路径各自一次 Traverse。隐式外键对象字段编译为 Get，不是 Expand。

### REST follow
The REST generic graph query: from an object instance, walk a path of declared `@link` field names (not link-type names) to a terminal object set. Always compiled as Traverse, including a one-hop path. Unknown field names fail closed; a missing start is not found. GraphQL does not expose a root traverse or object-level follow — clients still write nested `@link`.

> REST 通用图查询：从某对象实例出发，按已声明的 `@link` 字段名路径（不是链接类型名）走到终点对象集合。一律编译为 Traverse，包括单步路径。未知字段名失败、不静默跳过；起点缺失则未找到。GraphQL 不暴露根级 traverse 或对象级 follow —— 客户端仍写嵌套 `@link`。

## Flagged ambiguities

- "follow" and "traverse" are not interchangeable: REST follow is the HTTP surface; Traverse is the storage primitive it compiles to; GraphQL nested `@link` is neither a public traverse field nor REST follow.
- "Visited" was the retired uniform intermediate-payload field on the traversal result — not a ban on SQL projecting columns already in the JOIN (see Projection-aware Traverse).
- "sidecar" was the missing-system-column strategy (separate `of_*` tables). Landed storage is Direct-native identity on business tables; the MySQL uniqueness helper is a generated column on the same table, not a sidecar.

> 「follow」与「traverse」不可互换：REST follow 是 HTTP 表面；Traverse 是它编译到的存储原语；GraphQL 嵌套 `@link` 既不是公开的 traverse 字段，也不是 REST follow。
> 「Visited」是遍历结果上已退役的统一中间负载字段 —— 并不禁止 SQL 投影它已经 JOIN 的列（见 Projection-aware Traverse）。
> 「sidecar」曾是缺失系统列时的补齐策略（独立 `of_*` 表）。落地存储是业务表上的 Direct-native identity；MySQL 的基数唯一辅助是同表生成列，不是 sidecar。