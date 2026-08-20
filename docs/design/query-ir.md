# Query IR

Query IR 是 Go Runtime **全部对外读** 的编译目标：GraphQL 的 get / list / aggregate / search / 嵌套 `@link` / 对象型外键、REST GET by id，以及 REST follow。GraphQL SDL 是 Ontology IR 的投影，不是语义核心。Query IR 位于这些投影与 Engine 之间。

本文描述该 IR 的形状。实现顺序见 `docs/plans/2026-08-20-002-feat-go-phase6-query-ir-traverse-plan.md`。产品规则见 `docs/brainstorms/2026-08-20-go-phase6-query-ir-traverse-requirements.md`。

---

## 在 Runtime 中的位置

`docs/design/draft.md` 列出三种核心中间表示：

| IR | 回答什么 | 现状 |
|---|---|---|
| Ontology IR | 有哪些类型、链接、计算字段 | `runtime/ir`（ODL 下降） |
| Query IR | 针对这些类型执行哪一次读 | 本文 |
| Action IR | 校验并施加哪一次写 | 本阶段不做 |

```text
ODL ──► Ontology IR ──► GraphQL SDL / REST 路由
                │
                ▼
         GraphQL / REST 请求
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

投影层只构造 Query IR，不调用 `StorageProvider`。持久化边界只有 Engine。

Query IR **不是**：

- GraphQL AST（schema 仍来自 Ontology IR）
- 暴露给客户端的 SPI `TraversalPath`（路径词表是 `@link` 字段名）
- OBDA `SemanticQueryPlan`（`docs/design/open-foundry-obda-mapping-spec-v1.md` §71 编译 overlay SQL，是另一套产物）
- 整份 GraphQL 操作上的共享前缀优化器

v1 为每个 resolver（或每个 REST 请求）构造一个 tagged op，再跑 `Execute`。`a { b { c, d } }` 的共享前缀规划延后。

---

## 代数

一个 Query IR 值恰好是下列 op 之一。Go 用「互斥指针的 struct」还是 interface，由实现选择；判别式不变。

```text
Op =
    Get      { type, id, computed? }
  | List     { type, filter, orderBy, page }
  | Aggregate{ type, filter, groupBy, fields }
  | Search   { type, query, fields?, filter, page }
  | Expand   { startType, startId, hops }
```

- **Get** — 单个对象。`computed` 是 LAZY 字段集合（GraphQL：选中的计算字段；REST GET：全部 LAZY，与 Phase 6 一致）。
- **List** — `QueryObjects` + Relay 分页。进入 op 时 filter/orderBy 已是 SPI 类型；GraphQL `FooFilter` 的转换留在投影层。
- **Aggregate** / **Search** — 现有 Engine 动词透传。
- **Expand** — 从单个起点对象出发，沿 Ontology IR 的 `RoleLinkNav` 字段走图。

对象型属性（隐式外键：`RoleProperty` 且类型为 ObjectType）是 **Get**，不是 Expand。`PurchaseOrder.supplier`、`InventoryRecord.product` 仍按存着的 id 做 `GetObject`。

计算字段也不是 Expand。即使和嵌套 `@link` 写在一起，`Facility.currentUtilization` 仍走 `ComputeField` / 带 computed 选项的 Get。

---

## Expand

只有 Expand 会在 `GetLinks` 与 `Traverse` 之间做选择。

### 跳数分类（GraphQL）

按每个 `@link` 字段、用该字段的子选择集分类（graph-gophers 的 `SelectedFieldNames` / `HasSelectedField`）。

```text
对象 A 上的字段 f（@link）：
  若 f 的子选择集不含 RoleLinkNav
      → hop 模式 = GetLinks          # 1 跳叶子（允许兄弟字段）
  否则
      → hop 模式 = Traverse
        每条线性 @link 链一条 TraversalPath
        根在 A，从 f 起步
```

示例：

| 选择集 | 生成的 op |
|---|---|
| `product { suppliers { name } }` | 对 `suppliers` 做 Expand GetLinks |
| `a { b { name } c { name } }` | 两次 GetLinks（`b`、`c`） |
| `facility { inventoryRecords { trackedProduct { name } } }` | 一次 Traverse：`inventoryRecords → trackedProduct` |
| `a { b { c {…} d {…} } }` | 两次 Traverse：`a→b→c` 与 `a→b→d`（不共享前缀） |
| `a { b { name } c { d { name } } }` | `b` 走 GetLinks；`a→c→d` 走 Traverse |

REST follow **不用**上表。follow 一律 Traverse，包括单步路径。

### 路径词表

对外路径是带 `RoleLinkNav` 的 **Ontology IR 字段名**。编译器在当前跳的类型上解析每个名字：

```text
字段名 ──► Field.Link.Type + Field.Link.Direction ──► spi.TraversalStep
```

编译期非法（失败关闭，不调用 SPI）：

- 空 path
- 名字是标量、计算字段或外键属性
- 名字是 LinkType（`InventoryAt`）而不是导航字段（`inventoryRecords`）
- 名字在别的类型上存在，但当前跳的类型上没有

follow 的线上契约不出现 direction 和 link type。这样 Query IR 仍是语义图 API，而不是图数据库 API。

### 基数与上限

多端跳的 frontier 上限是 **每个起点、每跳 1000**（与 Phase 6 的 `linkPageLimit` 相同）。SPI `TraversalOptions.Limit` 只切终点 `nodes`；Expand 必须在每一跳截断 frontier，使中间 MANY 列表与 GraphQL 字段上限一致。

ONE / MANY_TO_ONE 导航字段装配成单个对象或 null，不是列表。

### 起点必须存在

SPI 层的 `Traverse` 不要求起点对象存在。Query IR 要求：

1. 先 Get 起点（与 GraphQL get / REST GET 相同的 not-found：缺失、软删、错租户）。
2. 然后再 Expand。

否则 follow 可能走通 GraphQL 已经返回 `null` 的对象上的链接。

---

## 编译

### GraphQL

每个面向 Engine 的 resolver 构造一个 op 并调用 `Execute`：

| Resolver | Op |
|---|---|
| `foo(id)` | Get |
| `foos(filter, …)` | List |
| `fooAggregate` | Aggregate |
| `searchFoos` | Search |
| `@link` 字段 | Expand（按子选择集分类） |
| 对象型外键字段 | 对存着的 id 做 Get |

Expand 结果按 `(startId, fieldName)` 缓存在本次请求上。子 `@link` resolver **只读缓存**。同一 key 再打一次 `GetLinks` / `Traverse` 视为缺陷（双执行）。测试用 Engine 调用次数锁住。

这仍是 Query IR：执行器只有一个。它不是整次操作的计划。列表里的每个根对象各自 Expand；本阶段不合并多个 startId。

公开 SDL 不增加 `Query.traverse` 或 `Type.follow`。GraphQL 走图的方式仍是嵌套 `@link`。

### REST

| 请求 | Op |
|---|---|
| `GET /api/v1/{lowerFirst(type)}/{id}` | Get（全部 LAZY computed） |
| `GET /api/v1/{lowerFirst(type)}/{id}/follow?path=f1,f2` | 先 Get 起点，再 Expand 为单条 Traverse 路径 |

follow 响应是 **终点** 的对外对象（`id`，不是 `_id`）。与 GraphQL 2 跳比较时比 **终点 id 集合**，不比树是否同构：多条库存指向同一产品时，REST `nodes` 去重，GraphQL 仍保留多个父层。

错误：

| 条件 | HTTP |
|---|---|
| 起点缺失 / 空 / 错租户 | 404 `OBJECT_NOT_FOUND` |
| 非法 path（见「路径词表」） | 400 `INVALID_FOLLOW_PATH`，不调用 SPI |
| URL 中类型未知 | 404（与现有 GET 一致） |

---

## 执行

```text
Execute(ctx, Op) → Result
  Get       → Engine.GetObject / GetObjectOpts
  List      → Engine.QueryObjects
  Aggregate → Engine.AggregateObjects
  Search    → Engine.SearchObjects
  Expand    → Engine.GetLinks 和/或 Engine.Traverse
              然后装配
```

`query` 依赖 Engine 与 Ontology IR，不 import 存储提供者。

Engine `Traverse` 与 `GetLinks` 一样透传到 SPI。分类、路径解析、上限、起点存在性、树装配属于 Query IR 的 Execute（以及 GraphQL memo），不属于 Engine。

---

## 装配

加法契约之后的 SPI `TraversalResult`：

| 字段 | 含义 |
|---|---|
| `nodes` | **仅最后一步** 的对象（语义不变） |
| `edges` | 走过的每一条链接 |
| `visited` | **严格中间对象**：不含起点、不含终点。1 跳 traverse 时为空 |
| `totalCount` | 不变（终点计数） |

今日 `nodes` = 最后一步只写在 memory 实现注释里，SPI 类型本身没有。类型注释必须写清这四个字段，GraphQL 装配不能依赖某个后端的注释。

装配步骤：

1. 按 `_id` 索引 `visited` 与 `nodes`。
2. 沿 `edges` 按路径中的每一跳把子对象挂到父对象上。
3. 两次 Traverse 共享同一中间 id 时只保留一个对象。
4. **不要**因为某 id 在更早一跳出现过就折叠后续跳（`product { suppliers { products { sku } } }` 仍须展示第二层 `products`）。

GetLinks 装配仍是「链接 → 对目标 GetObject」，上限 1000、目标缺失则跳过，与 `runtime/api/node.go` 现行为一致。

---

## 工作示例

**GraphQL 1 跳（必须仍走 GetLinks）**

```graphql
{ product(id: "p1") { suppliers { name } } }
```

```text
Expand{ start: Product/p1, hops: [GetLinks suppliers] }
```

**GraphQL 2 跳（必须走 Traverse）**

```graphql
{ facility(id: "f1") {
    inventoryRecords { quantity trackedProduct { name } }
  }
}
```

`inventoryRecords` 与 `trackedProduct` 是 `@link` 导航字段（外键 `product` / `facility` 仍走 Get）。路径：

```text
Facility --InventoryAt inbound--> InventoryRecord --InventoryOf outbound--> Product
Expand{ Traverse [由字段名解析出的 TraversalStep] }
```

若种子只有外键、没有 `InventoryOf` 链接，则 `trackedProduct` 为 null，`product { name }` 仍可通过 Get 解析。

**REST follow（一律 Traverse）**

```http
GET /api/v1/facility/f1/follow?path=inventoryRecords,trackedProduct
GET /api/v1/product/p1/follow?path=suppliers
```

第二条是单步 Traverse，不是 GetLinks。

---

## 非目标

- GraphQL 根字段 `traverse` / ObjectType 上的 `follow`
- 整份文档级 Query IR、共享前缀规划、对列表多个根做 DataLoader
- 在 Execute 内做鉴权 / consent / 字段红线
- SPARQL / Cypher 前端（以后应编译到本 IR，而不是绕过它）
- TS GraphQL planner（SPI 仍更新 `visited`，避免契约分叉）
- `supportsGraphTraversal` 为 false 时从 schema 省略 follow

---

## 来源

- `docs/design/draft.md` — Ontology IR + Query IR + Action IR
- `docs/brainstorms/2026-08-20-go-phase6-query-ir-traverse-requirements.md`
- `docs/plans/2026-08-20-002-feat-go-phase6-query-ir-traverse-plan.md`
- `docs/open-foundry-spec-v2.md` §2.1.3 `@link`、§3.1 `traverse`、§8.1.1 生成 Query
- `runtime/ir/ontology.go` — `RoleLinkNav` / `RoleProperty` / `RoleComputed`
- `runtime/spi/ontology.go` — `TraversalPath` / `TraversalResult`
- `runtime/api/node.go` — 当前逐字段 `GetLinks` + `GetObject`
