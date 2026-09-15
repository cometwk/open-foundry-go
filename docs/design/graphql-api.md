# GraphQL API（Go Runtime）

Go Runtime 的 GraphQL 服务是 Ontology IR 的**只读投影**：SDL 由 IR + `StorageCapabilities` 确定性生成，resolver 只构造 Query IR，执行走 Engine。语义源不是 GraphQL AST，也不是 `*.obda.yaml`。

产品对照：`docs/open-foundry-spec-v2.md` §8.1。本阶段范围：`docs/brainstorms/2026-08-20-go-phase6-graphql-rest-requirements.md`（只读金路径）。Query IR 形状：`docs/design/query-ir.md`。请求如何落到存储：`docs/solutions/architecture-patterns/go-runtime-query-ir-pipeline.md`。

引用 spec v2 时忽略 Sync Engine 与 Security/Governance（`CLAUDE.md`）。`_redactedFields` / `_consentRestricted` / ReBAC / consent 不进入本文的成功路径。

---

## 在 Runtime 中的位置

```text
ODL ──► Ontology IR ──► GraphQL SDL          （projection/graphql）
                │              │
                │              ▼
                │       统一对象 Go 类型 + Query 根     （api/resolver_type, schema）
                │              │
                ▼              ▼
         GraphQL 请求 ──► Query IR ──► Engine ──► SPI
```

| 层 | 包 | 职责 |
|---|---|---|
| SDL 投影 | `runtime/projection/graphql` | IR + capabilities → SDL 字符串；不 import GraphQL AST |
| 可执行 schema | `runtime/api` | 动态 resolver 类型、`graphql.ParseSchema`、HTTP |
| 编译目标 | `runtime/query` | tagged Op；唯一 `Execute` |
| 存储 | Engine → SPI | GraphQL 包不 import mysqlobda |

进程入口：`runtime/cmd/run.go` 在 `bootstrap.Open` → `ApplySchema` → `engine.NewWithCompiled` 之后 `api.New(e)`，对外 `srv.Handler()`。

---

## 与 spec §8.1 的关系

§8.1 要求：给定 ODL schema 与 capability profile，编译结果确定。这一点落地了：`projgql.Generate(ont, caps)` 对对象名排序后写 SDL，同一 IR + 同一 `SupportsFullTextSearch` 得到同一字符串。

§8.1.1–8.1.5 的表面只有一部分在本阶段生成。金门测试锁「有什么 / 禁止什么」：`runtime/projection/graphql/sdl_test.go` `TestGenerate_SupplyChain_SearchAndNoWriteSurface`。

| spec §8.1 | 落地 |
|---|---|
| `foo(id: ID!): Foo` | 有。`LowerFirst(TypeName)` |
| `foos(filter, orderBy, first, after, last, before): FooConnection!` | 有。Relay 形 Connection / Edge / PageInfo |
| `fooAggregate(...)` | **有**（spec 本节未写；Phase 6 R5 / TS 已有，一并生成） |
| `searchFoos(query, first): FooConnection!` | **有，但形状不同**：`searchFoos(query, fields, filter, first, after): SearchResult_Foo!`，hits 带 score，不是 Connection |
| `searchAll` / `typeahead` | **不生成**（即使 `supportsFullTextSearch`；spec §8.1.5 MUST 被 Phase 6 显式砍掉） |
| `supportsFullTextSearch = false` 省略 search | 有 |
| ObjectType 上 `_redactedFields` / `_consentRestricted` | **不生成**；可空性跟 ODL，不跟 redaction envelope |
| §8.1.2 Mutation（按 ActionType） | **无** `type Mutation` |
| §8.1.3 FHIR → Action | **无** |
| §8.1.4 Subscription | **无** `type Subscription` |
| 根级 `traverse` / `follow` | **无**。走图靠嵌套 `@link`；通用图查询在 REST follow |
| 嵌套 `@link` 字段 | 有，按 IR `RoleLinkNav` 投影 |

Filter 输入在 SDL 上接近 spec（每属性一个 `*Filter`，外加 `AND` / `OR` / `NOT`）。对象型属性与 `@link` **不**进入 `FooFilter`（避免 `supplier: SupplierFilter`）。mysql-obda 对 filter 仍 fail-closed（eq + or-of-eq）；SDL 更宽不代表 SQL 会执行 `contains`。

---

## 启动：两份产物必须对齐

`runtime/api/schema.go:39-61` 的 `New`：

1. `buildResolverTypes(ont)` — 一份跨类型的 Go 结构（对象、Connection、SearchResult）。
2. `projgql.Generate(ont, caps)` — SDL。
3. `buildQueryRoot()` — Query 根上每个 ObjectType 的 get / list / aggregate /（可选）search 函数。
4. `graphql.ParseSchema(sdl, root, UseFieldResolvers(), DisableIntrospection())`。

SDL 与 Go 类型任一方字段对不上，ParseSchema 失败，进程起不来。Introspection 关闭：客户端不能靠 `__schema` 发现被故意省略的 Mutation / searchAll。

HTTP（`runtime/api/http.go:18-25`）：

```text
POST /graphql                         graph-gophers relay.Handler
GET  /api/v1/{lowerFirst}/{id}        REST Get（同一 Engine）
GET  /api/v1/{lowerFirst}/{id}/follow REST Expand Traverse
```

所有入口要求 `X-OpenFoundry-Tenant`。`withRC` 写入 `spi.RequestContext`，并挂上本次请求的 Expand memo。Actor / Trace 目前是 phase6 哨兵常量，不是认证管道。

---

## SDL 投影

`runtime/projection/graphql/sdl.go:32-86` 的 `Generate` 读 **FieldRole**，不读 ODL 指令袋。

对每个 ObjectType：

- `type Foo { ... }`：IR 字段原样投影。列表 `@link` 保持 IR 可空性（空导航是 `[]` 不是 `null`）。对象型 FK 属性在 SDL 上可空（读时可能缺失）。
- `FooConnection` / `FooEdge` / `FooFilter` / `FooOrderBy`。
- `supportsFullTextSearch` 时：`SearchHit_Foo`、`SearchResult_Foo`。

Query 根（`sdl.go:305-338`）对每个类型生成 `foo` / `foos` / `fooAggregate`，有 FTS 时再加 `searchFoos`。复数是 `LowerFirst + "s"`（`searchFacilitys` 这种不规则复数会原样出现）。

没有 `type Mutation`、`type Subscription`、根 `searchAll` / `typeahead`。

---

## 统一对象类型

graph-gophers 的 `UseFieldResolvers` 要求：返回给 GraphQL 的值上，带 `graphql:"fieldName"` 的导出方法/字段对应 SDL。Go 无法为每个 ObjectType 手写一份 struct，且 `@link` 的返回类型必须是「正在生成的这份类型」——`reflect.StructOf` 表达不了这个环。

`runtime/api/resolver_type.go:46-120` 的做法：

1. `collectObjFields` 把 **全部** ObjectType 的字段按 GraphQL 名做成并集。同名必须签名兼容（同 kind、同标量形状），否则 `New` 失败。
2. 先用占位函数类型 `StructOf` 出 `objType`。
3. `rewriteStructFieldType`（`dynstruct.go:47-65`）把 `@link` / FK 的返回类型改写成 `*objType` / `[]*objType`。这是有意的 ABI 补丁：错配会让 `New` 失败，而不是静默写坏内存。
4. 同时生成 Edge / Connection / SearchHit / SearchResult 结构，Node 指向同一 `*objType`。

运行时每个实例是 `wrap(typ, obj)`（`node.go:22-29`）：一份 `node{typ, obj}` 绑到新的 `objType` 值上。GraphQL 只会调用该 SDL 类型上存在的字段；并集里多出来的方法不会被调度。真正的角色以 **该 node 的 Ontology IR 类型** 为准：`makeObjFunc` 先 `n.irField(name)` 再 `classifyField`（`resolver_type.go:252-280`）。

字段分类：

| FieldRole / 形状 | kind | 行为 |
|---|---|---|
| `RoleLinkNav` 列表 | `fieldLinkList` | `resolveLink` → Expand |
| `RoleLinkNav` 单数 | `fieldLinkOne` | 同上，取第一跳或 null |
| `RoleProperty` 且类型是 ObjectType | `fieldFK` | `resolveFK` → Get（隐式外键，不是 traverse） |
| `RoleComputed` | `fieldComputed` | `Engine.ComputeField`（不经 Expand） |
| 其余 | `fieldScalar` | 从已物化的 `OntologyObject` 取值 |

`RoleParam` 不出现在对象类型上。

---

## Query 根：每个字段一个 Query IR Op

`buildQueryRoot`（`schema.go:85-124`）按 IR 对象列表反射出根 struct，再填函数。

| GraphQL | 构造 | 找不到 / 空 |
|---|---|---|
| `foo(id)` | `Get{Type, ID}` | `ErrObjectNotFound` → GraphQL `null` |
| `foos(...)` | `List`；filter/order 在投影层转 SPI | Connection；默认 `first=20`，硬顶 `100`（`pagination.go:10-12`） |
| `fooAggregate` | `Aggregate` | `groups: []` |
| `searchFoos` | `Search` | 仅当 caps 打开时根上才有该字段 |

列表分页把 Relay `after`/`before` 解成 offset cursor（`cursor:<n>` 的 base64）。这是 offset 分页，不是 keyset。

嵌套 `@link` 不在根上分类。`resolveLink`（`node.go:45-63`）：

1. `SelectedFieldNames` 做选择指纹，命中 memo 则不再 Execute。
2. `compileExpand` 按子选择集选择 `ExpandGetLinks` 或 `ExpandTraverse`。
3. `query.Execute`；邻接写入 memo，返回第一跳给 graph-gophers 继续下钻。

规则与 REST follow 的差异见 Query IR 设计稿：GraphQL 1 跳叶子走 GetLinks；REST follow 一律 Traverse。公开 SDL 不增加 `Query.traverse`。

---

## 请求期

```text
POST /graphql
  → withTenant（缺头 400 MISSING_TENANT）
  → relay.Handler / schema.Exec
  → 根 resolver：Get / List / Aggregate / Search
  → 对象字段：scalar | ComputeField | Get(FK) | Expand(@link)
  → query.Execute → Engine → SPI
```

进程内测试走 `Server.Exec`（`schema.go:257-259`），与 HTTP 共用 memo 与 Op 构造。

计算字段即使和嵌套 `@link` 写在同一选择集里，也仍走 `ComputeField`，不是 Expand。

---

## 非目标（本阶段）

- Mutation / Action HTTP / FHIR 写映射（spec §8.1.2–8.1.3）
- Subscription / EventBus（spec §8.1.4）
- `searchAll`、`typeahead`、BM25 排名（spec §8.1.5；Phase 6 文档化砍切）
- 鉴权、consent、字段红线、`_redactedFields`
- 整份 GraphQL 文档的共享前缀优化 / DataLoader 多 startId
- 把 SPI `TraversalPath` 暴露给客户端
- 按类型生成独立 Go struct（统一 `objType` 是刻意取舍）

---

## 来源

- `docs/open-foundry-spec-v2.md` §8.1（Query / Mutation / FHIR / Subscription / Search；安全字段按仓库约定忽略）
- `docs/brainstorms/2026-08-20-go-phase6-graphql-rest-requirements.md` — 只读金路径与 spec 砍切
- `docs/brainstorms/2026-08-20-go-phase6-query-ir-traverse-requirements.md` — Query IR 与嵌套 `@link`
- `docs/design/query-ir.md`
- `docs/solutions/architecture-patterns/go-runtime-query-ir-pipeline.md`
- `runtime/projection/graphql/sdl.go`、`sdl_test.go`
- `runtime/api/schema.go`、`resolver_type.go`、`node.go`、`compile.go`、`http.go`、`dynstruct.go`
- `runtime/cmd/run.go`
