# (1)
> 有个问题：Visited 是啥，为啥要定义这个，起因是啥？

让我先看看实际代码中 Traverse 的实现和 Visited 的定义，再回答你的问题。
现在我已经了解了全貌。让我阅读一下 `expand.go`，看看 `Visited` 是如何被消耗的。
好，现在两个问题都能回答了。

---

## Visited 的起因

SPI 在 `runtime/spi/ontology.go:234` 定义了 `TraversalResult` 的三个桶：

```go
// Nodes are objects at the last TraversalPath step only.
// Visited are strict intermediates: not the start object and not Nodes.
// A one-step traverse has empty Visited. Edges are every link walked.
type TraversalResult struct {
    Nodes      []OntologyObject
    Edges      []OntologyLink
    Visited    []OntologyObject
    TotalCount int
}
```

**起因是 GraphQL 字段展开（field expansion）需要中间层对象被 hydrate。**

看 `runtime/query/expand.go:157-165` 的 `assemblePath`：

```go
func assemblePath(...) *ExpandResult {
    objs := map[string]spi.OntologyObject{}
    putObj(objs, startObj)
    for _, o := range tr.Visited {   // ← 中间层对象灌进 lookup map
        putObj(objs, o)
    }
    for _, o := range tr.Nodes {     // ← 终点对象灌进 lookup map
        putObj(objs, o)
    }
    adj := map[string]map[string][]spi.OntologyObject{}
    frontier := []string{startID}
    for i, field := range fields {
        // 用 tr.Edges 做邻居查找，用 objs 做对象填充
        nbs := neighbors(pid, steps[i], tr.Edges, objs, HopCap)
        appendAdj(adj, pid, field, nbs)
    }
}
```

场景：GraphQL 查询 `a(id:"...") { b { c { name } } }`，引擎拆成 2-hop Traverse。Traverse 返回后，`assemblePath` 要重建 `A → B → C` 的邻接结构，B 的字段（如 `name`）必须已经被加载好——否则调用方要么再发 N 次 `GetObject(B)`，要么只能拿到一堆 bare ID。

所以 **Edges 给的是拓扑关系（谁连谁），Visited 给的是中间层对象的完整 payload**。两者不能互替：

- Edges 里有 `fromId`/`toId`，但中间层对象 B 的业务字段（`name`、`sku` 等）不在 link 里
- Nodes 只有终点 C，B 不可见
- Visited 专门填这个缺口

单跳 Traverse 的 Visited 为空——没有中间层。测试 `provider_link_extra_test.go:126` 明确验证了这个约定：

```go
// 2-hop: Supplier → Part → Assembly
// Nodes = [Assembly], Visited = [Part], Edges = 2
if len(res.Visited) != 1 {
    t.Fatalf("visited = %d, want 1 (Part only)", len(res.Visited))
}
```

---

## 链式 JOIN 落地时这一条 SQL 要交什么

问题在于：spec §8.5 的示例 SQL 是 `SELECT w.* FROM patient JOIN admission JOIN ward`——只返回终点表的列。但 `TraversalResult` 要三样东西。

当前 sqliteobda 的实现（`links.go:347-426`）**不是链式 JOIN**，而是逐步 BFS：

```go
for i, step := range path.Steps {
    for _, cur := range frontier {
        page, err := p.GetLinks(ctx, cur.id, step.LinkType, dir, ...)
        for _, e := range page.Items {
            edges = append(edges, e)                    // ← 每跳的 link 行直接进 Edges
            obj, err := p.loadObject(...)
            if i == len(path.Steps)-1 {
                nodes = append(nodes, obj)             // ← 最后一跳进 Nodes
            } else {
                visited = append(visited, obj)         // ← 中间跳进 Visited
            }
        }
    }
    frontier = next
}
```

每跳 `GetLinks` 自然产出 link 行（Edges），再 `loadObject` 产出对象（Visited / Nodes）。三桶天然分开。

**如果真正落地链式 JOIN**，一条 SQL 要同时返回三个表的行，SQL 形状大致是：

```sql
SELECT
    w.*                            AS node_*,        -- → Nodes
    a.*                            AS edge_adm_*,   -- → Edges (AdmittedTo link rows)
    mid.*                          AS visited_*      -- → Visited (intermediate objects, if 3+ hops)
FROM patient p
JOIN admission a  ON a.from_id = p.id AND a.tenant_id = ? AND a.deleted_at IS NULL
JOIN ward w       ON w.id = a.to_id AND w.tenant_id = ?
WHERE p.id = ? AND p.tenant_id = ?
```

2-hop 时中间层是 start（patient），不需要额外 SELECT。但 3-hop（`A → B → C`）时，B 是 Visited，SQL 需要：

```sql
SELECT
    c.*           AS node_*,
    ab.*          AS edge_hop1_*,
    bc.*          AS edge_hop2_*,
    b.*           AS visited_*
FROM a
JOIN ab ON ...
JOIN b  ON ...
JOIN bc ON ...
JOIN c  ON ...
```

每跳的 link 行要带 `_id`/`_fromType`/`_fromId`/`_toType`/`_toId`/`version`/`createdAt`/`updatedAt`/`deletedAt` 才能组装成 `OntologyLink`。

**这就是为什么当前实现选择了逐步 GetLinks BFS 而非链式 JOIN**——BFS 的每跳 GetLinks 天然产出完整 link 行和对象，三桶分离不需要复杂的 projection 别名映射。链式 JOIN 在单条 SQL 里做三桶 projection 需要额外的行映射器（row mapper）来拆分 `node_*` / `edge_*` / `visited_*` 列前缀，复杂度显著上升，收益（减少 round-trip）在 SQLite 单文件场景下有限。

spec §8.4 的 "planner 产出 `sqlast.Join`" 是目标架构描述，当前实现取的是 spec 同一段后半句的 "固定 `path.Steps` 的链式 JOIN 即可。禁止再对影子表 BFS" 中的**逐步 GetLinks 路径**，而非真正的单条 chain JOIN SQL。

# (2) 

> 针对spec §8.5 的 Traverse 情况，不返回 Visited 如何， 会有啥影响


让我追踪所有消费 `tr.Visited` 的代码路径，看看不返回它会有什么具体影响。
让我再追踪一下是谁调用了 `assemblePath`，以了解其完整的传播过程。
现在全链路清楚了。回答如下。

---

## 不返回 Visited 的影响

**唯一生产消费方是 `runtime/query/expand.go:160` 的 `assemblePath`。** 它在多跳 GraphQL 字段展开（如 `a(id:"...") { b { c { name } } }`）中被调用。

### 故障路径：2-hop `A → B → C`

`assemblePath` 的工作流程：

1. 构建 `objs` lookup map：`startObj`(A) + `tr.Visited`(B) + `tr.Nodes`(C)
2. 逐跳用 `neighbors()` 在 `tr.Edges` 里找邻居，再从 `objs` 拿对象 payload

如果 `Visited` 为空：

```go
// expand.go:144-146  neighbors() 内部
obj, ok := objs[nid]
if !ok {
    continue    // ← B_id 在 objs 里找不到，跳过
}
```

- Hop 1 (`A→B`)：Edges 里有 `A→B` 的 link，`neighborID` 得到 `B_id`，但 `objs[B_id]` 不存在 → `continue` → 邻居列表为空
- Frontier 变空
- Hop 2 (`B→C`)：frontier 为空 → 循环不执行
- **最终**：`FirstHop = []`、`Terminals = []`、`Adjacency = {}`

**GraphQL 返回 `b: []`——静默空结果，数据实际存在但不被组装进响应。** 不报错，不告警。

### 1-hop 无影响

单跳 traverse（`A → B`）按定义 `Visited` 为空，`assemblePath` 只需 `startObj` + `Nodes`，不依赖中间层。

### TS 侧根本没这个字段

```ts
// packages/spi/src/ontology.ts:156
export interface TraversalResult {
  nodes: OntologyObject[];
  edges: OntologyLink[];
  totalCount: number;
  // 没有 visited
}
```

TS 的 `TraversalResult` 压根没有 `visited`。Go SPI 在移植时加了这个字段。如果 Go 的 Traverse 不返回 Visited，行为就和 TS 侧一致——但 Go 的 `expand.go` 是依赖它的。

### 总结

| 场景 | 不返回 Visited 的影响 |
|---|---|
| 1-hop Traverse | 无影响（Visited 本来就空） |
| 2+ hop GraphQL 字段展开 | **静默返回空数据**——`assemblePath` 无法组装中间层邻接，整条路径断裂 |
| 非 Expand 的直接 Traverse 调用 | 无影响——调用方只看 Nodes/Edges，不依赖 Visited |

所以如果要砍掉 Visited，要么 `assemblePath` 改为对中间层对象补发 `GetObject`（N 次 round-trip），要么多跳 Expand 降级为不展开中间层字段（只返回终点 ID 列表，丢失 `b { name }` 的 `name`）。这就是它存在的起因：**避免 N+1 查询，把中间层对象随 Traverse 一次性带回。**


# (3) 

> 一条 JOIN 必须一次装满 Nodes(终点)、Edges(沿途每条 link)、Visited (严格中间对象);Query IR2 跳才能继续拼树
>
> 评估是否能搞定，如何搞定？

让我先看看 `sqlast` 现有的结构，以及 planner 现在能产出什么，再评估。
现在我已经有了完整的了解。让我来追踪一下现有的基础设施。
现在全链路代码都看完了。以下是评估。

---

## 结论：能搞定，现有基础设施 ~70% 已就位

### 已有的

| 能力 | 位置 | 状态 |
|---|---|---|
| `sqlast.Select.Joins []Join` | `sqlast/ast.go:69-75` | ✅ JOIN 链结构已有 |
| `Identifier.Qualifier` | `sqlast/ast.go:4-7` | ✅ 表别名限名列已有 |
| `renderSelect` 渲染 JOIN + 限名列 | `dialect/sqlite/dialect.go:94-175` | ✅ 渲染 `"e0"."id"` 和 `INNER JOIN ... ON` |
| `PlanGetLinksJoin` 两表 JOIN | `planner.go:178-222` | ✅ 已证明 2 表 JOIN + 别名 `l`/`p` 可行 |
| `ObjectBinding.SelectColumns` | `planner.go:11-19` + `compiler.go:74-117` | ✅ 每张表的完整列清单（含 identity/tenant/fields/system，尊重 Omit） |
| `CompiledModel.Binding()` / `CompiledLink.Binding()` | `compiler.go:74,120` | ✅ 都产出 `SelectColumns` |

### 需要新建的（3 件）

#### 1. `PlanTraverse` planner 函数

输入：start model binding、每跳的 link binding + peer model binding、direction。

产出一条 `sqlast.Select`，列按固定顺序排列：

```
[node_cols] [edge0_cols] [visited0_cols] [edge1_cols] [visited1_cols] ...
```

表别名约定：

| 别名 | 角色 |
|---|---|
| `st` | start 表 |
| `e0`, `e1`, ... | 每跳 link 表 |
| `v0`, `v1`, ... | 中间层 object 表 |
| `n` | 终点 object 表 |

2-hop 示例 SQL：

```sql
SELECT
  -- Nodes (terminal = Trust)
  "n"."id", "n"."tenant_id", "n"."name", "n"."version", "n"."created_at", "n"."updated_at", "n"."deleted_at",
  -- Edges[0] (AdmittedTo links)
  "e0"."id", "e0"."tenant_id", "e0"."from_id", "e0"."to_id", "e0"."version", "e0"."created_at", "e0"."updated_at", "e0"."deleted_at",
  -- Visited[0] (Ward)
  "v0"."id", "v0"."tenant_id", "v0"."name", "v0"."version", "v0"."created_at", "v0"."updated_at", "v0"."deleted_at",
  -- Edges[1] (BelongsTo links)
  "e1"."id", "e1"."tenant_id", "e1"."from_id", "e1"."to_id", "e1"."version", "e1"."created_at", "e1"."updated_at", "e1"."deleted_at"
FROM patient "st"
INNER JOIN admission "e0"
  ON "e0"."from_id" = "st"."id"
  AND "e0"."tenant_id" = "st"."tenant_id"
  AND "e0"."deleted_at" IS NULL
INNER JOIN ward "v0"
  ON "v0"."id" = "e0"."to_id"
  AND "v0"."tenant_id" = "e0"."tenant_id"
  AND "v0"."deleted_at" IS NULL
INNER JOIN ward_trust "e1"
  ON "e1"."from_id" = "v0"."id"
  AND "e1"."tenant_id" = "v0"."tenant_id"
  AND "e1"."deleted_at" IS NULL
INNER JOIN trust "n"
  ON "n"."id" = "e1"."to_id"
  AND "n"."tenant_id" = "e1"."tenant_id"
  AND "n"."deleted_at" IS NULL
WHERE "st"."id" = ? AND "st"."tenant_id" = ?
```

不需要改 `sqlast` 任何类型——`Identifier{Qualifier:"e0", Name:"id"}` 已有，renderer 已渲染。

#### 2. 行扫描器（row mapper）

planner 同时产出一个 `TraverseLayout` 描述列偏移：

```go
type TraverseLayout struct {
    NodeCols    []string   // 列名
    NodeOffset  int        // 起始列索引
    Edges       []EdgeLayout
    Visited     []VisitedLayout
}

type EdgeLayout struct {
    Cols   []string
    Offset int
}

type VisitedLayout struct {
    Cols   []string
    Offset int
}
```

扫描逻辑：

```go
totalCols := layout.totalCols()
values := make([]any, totalCols)

for rows.Next() {
    rows.Scan(values...)

    // 按偏移切片 + 构建 OntologyObject/OntologyLink
    nodeObj := buildObject(values[layout.NodeOffset : layout.NodeOffset+len(layout.NodeCols)], ...)
    edge0   := buildLink(values[layout.Edges[0].Offset : ...], ...)
    vis0    := buildObject(values[layout.Visited[0].Offset : ...], ...)

    // 去重
    if _, ok := nodeSeen[nodeID]; !ok {
        nodeSeen[nodeID] = struct{}{}
        result.Nodes = append(result.Nodes, nodeObj)
    }
    // ...同理 edge/visited
}
```

#### 3. 去重 maps

fan-out 产生的重复行用 `map[string]struct{}` 去重，key 是 identity 列值。

---

### 需要处理的约束

| 约束 | 解法 |
|---|---|
| **fan-out 笛卡尔积** | JOIN 产生 M×N 行，同一 B/C/link 重复出现。`map[string]struct{}` 按 ID 去重，正确 |
| **BFS `seen` 语义差异** | BFS 按"首次见到"跳过已见；JOIN 返回所有匹配路径。对 DAG（有 cardinality 约束的 typed link）结果集等价。有环图 JOIN 可能多出路径——但 OBDA link 是 typed + cardinality-constrained，实际不太出现环 |
| **每表 Omit 不同** | `Binding().SelectColumns` 已尊重 Omit。planner 直接用每张表自己的列清单 |
| **列名冲突** | 多表都有 `id`/`tenant_id`/`version`。SQLite `rows.Scan` 是位置绑定，不看列名。planner 按固定顺序排列列，scanner 按偏移读取，不依赖列名 |
| **1-hop 退化** | 无 Visited、无中间表。JOIN = `start → link → terminal`，退化为 `PlanGetLinksJoin` 的超集 |

---

### 复杂度评估

| 工作项 | 量 |
|---|---|
| `PlanTraverse` 函数 | ~80 行（循环拼 JOIN 链 + 列清单） |
| `TraverseLayout` 类型 | ~15 行 |
| 行扫描器 | ~60 行（flat slice + 偏移切片 + 去重） |
| `sqlast` 改动 | **零** |
| renderer 改动 | **零** |

**不需要改 `sqlast` 任何类型**，`Identifier{Qualifier, Name}` + `Select{Joins}` + `Predicate{col_eq, is_null}` 全部已有。这是关键——AST 层不需要扩展。

唯一的注意点：`renderSelect` 的列渲染（`dialect.go:102-104`）只接受 `sqlast.Identifier`，不接受 `AS alias`。但我们不需要 alias——用 `Qualifier` 区分表，用列位置区分桶，足够。

**能搞定，中等复杂度，核心是 3 个新文件/函数，零 AST 改动。**

# (4)

> 如果 不返回 Visited ，但返回 Nodes / Edges 难度如何，评估

这比返回三桶简单很多。核心原因在 v3 direct-native 的物理模型里。

---

## 关键发现：link 表之间可以直接链式 JOIN，不需要中间 object 表

v3 中 `admission.to_id` 存的是 Ward 的编码 id，`ward_trust.from_id` 存的也是 Ward 的编码 id——**同一个值**。所以：

```sql
-- 不需要 JOIN ward 表！
-- e0.to_id 和 e1.from_id 是同一个值
e1.from_id = e0.to_id AND e1.tenant_id = e0.tenant_id
```

这意味着不带 Visited 时，JOIN 链从 `start + N links + (N-1) intermediate objects + terminal` 变成 `start + N links + terminal`：

| 跳数 | 带 Visited 的 JOIN 表数 | 不带 Visited 的 JOIN 表数 |
|---|---|---|
| 1-hop | 3 (start + link + terminal) | 3 (同) |
| 2-hop | 5 (start + L1 + B + L2 + terminal) | 3 (start + L1 + L2 + terminal → 实际 4) |
| 3-hop | 7 | 5 |

2-hop 不带 Visited 的 SQL：

```sql
SELECT
  "n"."id", "n"."tenant_id", "n"."name", "n"."version", ...,
  "e0"."id", "e0"."tenant_id", "e0"."from_id", "e0"."to_id", "e0"."version", ...,
  "e1"."id", "e1"."tenant_id", "e1"."from_id", "e1"."to_id", "e1"."version", ...
FROM patient "st"
INNER JOIN admission "e0"
  ON "e0"."from_id" = "st"."id"
  AND "e0"."tenant_id" = "st"."tenant_id"
  AND "e0"."deleted_at" IS NULL
INNER JOIN ward_trust "e1"
  ON "e1"."from_id" = "e0"."to_id"          -- 直接 link-to-link，跳过 ward 表
  AND "e1"."tenant_id" = "e0"."tenant_id"
  AND "e1"."deleted_at" IS NULL
INNER JOIN trust "n"
  ON "n"."id" = "e1"."to_id"
  AND "n"."tenant_id" = "e1"."tenant_id"
  AND "n"."deleted_at" IS NULL
WHERE "st"."id" = ? AND "st"."tenant_id" = ?
```

没有 `ward` 表。少一个 JOIN，少一整组列。

---

## 难度评估

### SQL + planner + row mapper：简单

| 工作项 | 带 Visited | 不带 Visited |
|---|---|---|
| JOIN 表数 | start + N links + (N-1) objects + terminal = 2N | start + N links + terminal = N+1 |
| SELECT 列分组 | 3 类桶（node / edge / visited） | 2 类桶（node / edge） |
| Row mapper 去重 | 3 组 map（node + edge + visited） | 2 组 map（node + edge） |
| `TraverseLayout` | 需要 Node/Edge/Visited 三种 offset | 只需 Node/Edge 两种 offset |
| `sqlast` 改动 | 零 | 零 |
| renderer 改动 | 零 | 零 |

**planner ~50 行，row mapper ~40 行。** 比三桶方案少约 40% 代码量。

### `assemblePath` 影响：需要处理

`expand.go:160` 的 `tr.Visited` 会空。多跳 GraphQL 展开会静默返回空。

**解法选择：**

| 方案 | 做法 | 额外 round-trip | 复杂度 |
|---|---|---|---|
| A. 批量补查 | 从 Edges 提取中间层 ID，`SELECT ... WHERE id IN (...)` 按中间类型批量补查 | 1 SQL/中间类型 | 低 |
| B. 不补查 | `assemblePath` 降级：只返回终点 ID 列表，中间层字段不展开 | 0 | 极低，但 GraphQL `b { name }` 拿不到 `name` |
| C. 两段式 | 1-hop 用 JOIN（无中间层），2+ hop 退回逐步 BFS（自带 Visited） | BFS 的 N 次 GetLinks | 已有代码 |

**方案 A 最实用**：JOIN 返回后，从 Edges 的 `from_id`/`to_id` 提取出中间层 ID 集合，每类中间对象发一条 `WHERE id IN (...) AND tenant_id = ?`，补满 Visited。以 2-hop 为例只多 1 条 SQL（查 ward），但主链 JOIN 已经把 Nodes + Edges 一次拿全。

### 中间层 deleted_at 的语义一致性

当前 BFS 的 `loadObject`（`PlanGetObject`）**不过滤 `deleted_at`**——soft-deleted 中间层对象仍然进入 Visited。跳过中间表 JOIN 后也无法过滤，但行为一致，不引入回归。

---

## 总结

| 维度 | 不带 Visited（Nodes + Edges） | 带全三桶 |
|---|---|---|
| JOIN 链 | link-to-link 直链，跳过中间 object 表 | 必须穿中间 object 表 |
| SQL 复杂度 | 低 | 中 |
| Row mapper | 2 桶，~40 行 | 3 桶，~60 行 |
| `assemblePath` | 需补查中间层（方案 A）或降级（方案 B） | 直接可用 |
| 总工作量 | planner ~50 + mapper ~40 + 补查 ~20 = **~110 行** | planner ~80 + mapper ~60 + layout ~15 = **~155 行** |

**不带 Visited 难度明显更低**，主要省在 link-to-link 直链跳过了中间 object 表。代价是 `assemblePath` 需要一条补查 SQL 拿中间层对象。如果接受方案 B（多跳只返回终点，不展开中间层字段），则连补查都不用，工作量极低。


# (5)

> 若只返回 Nodes 呢

只返回 Nodes 的话，SQL 和行扫描最简单，但 `assemblePath` 在多跳场景下完全断裂。逐项拆解。

---

## SQL：最简

JOIN 链不变（仍然需要穿 link 表到达终点），但 SELECT 只投影终点表：

```sql
SELECT
  "n"."id", "n"."tenant_id", "n"."name", "n"."version", "n"."created_at", "n"."updated_at", "n"."deleted_at"
FROM patient "st"
INNER JOIN admission "e0"
  ON "e0"."from_id" = "st"."id" AND "e0"."tenant_id" = "st"."tenant_id" AND "e0"."deleted_at" IS NULL
INNER JOIN ward_trust "e1"
  ON "e1"."from_id" = "e0"."to_id" AND "e1"."tenant_id" = "e0"."tenant_id" AND "e1"."deleted_at" IS NULL
INNER JOIN trust "n"
  ON "n"."id" = "e1"."to_id" AND "n"."tenant_id" = "e1"."tenant_id" AND "n"."deleted_at" IS NULL
WHERE "st"."id" = ? AND "st"."tenant_id" = ?
```

## Row mapper：直接复用现有 `loadObject`

现有 `loadObject` 已经做的就是"扫一行 → 建 `OntologyObject`"。只加一个 `map[string]struct{}` 按终点 id 去重。**~20 行。**

## `assemblePath`：多跳断裂

`expand.go:157` 的 `assemblePath` 靠 `tr.Edges` 在 `neighbors()` 里重建邻接树。没有 Edges：

```go
// expand.go:133-155
func neighbors(parentID string, step ..., edges []spi.OntologyLink, objs ...) {
    for _, e := range edges {        // ← edges 为空，循环不执行
        ...
    }
    return out                       // ← 返回空
}
```

- Hop 1：`neighbors()` 返回空 → `adj` 为空 → `frontier` 变空
- Hop 2：`frontier` 为空 → 不执行
- **结果**：`FirstHop = []`、`Terminals = []`、`Adjacency = {}`——即使 `objs` 里有 Nodes

### 1-hop 可修

1-hop 不需要 Edges 重建路径——终点就是唯一跳的邻居：

```go
// 伪码
if len(tr.Edges) == 0 && len(steps) == 1 {
    first = tr.Nodes      // 直接用 Nodes 当 FirstHop
    terminals = tr.Nodes
    adj[startID][field] = tr.Nodes
    return
}
```

### 多跳不可修（没有拓扑信息）

2-hop `A → B → C`，只有 Nodes(C)。你不知道哪些 C 是从哪个 B 到的——Edges 没了，无法重建 `A→B→C` 的邻接树。GraphQL `a(id) { b { c { name } } }` 返回空。

---

## 总评估

| 维度 | 只返回 Nodes | Nodes + Edges | 全三桶 |
|---|---|---|---|
| SQL SELECT | 终点列 only | 终点 + link 列 | 终点 + link + 中间 object 列 |
| JOIN 表数 | N+1 | N+1 | 2N |
| Row mapper | 复用 `loadObject`，~20 行 | 2 桶，~40 行 | 3 桶，~60 行 |
| 1-hop Expand | 小改可用 | 可用 | 可用 |
| 多-hop "找可达对象" | ✅ | ✅ | ✅ |
| 多-hop "建树给 GraphQL" | ❌ 完全断裂 | ❌ 缺 Visited | ✅ |

### 断裂点在哪

`assemblePath` 的设计前提是：**Edges 提供拓扑，Visited/Nodes 提供对象 payload**。只给 Nodes 等于只给了终点 payload，丢了全部拓扑。多跳 GraphQL 展开需要拓扑来知道"哪个 B 连到哪个 C"。

### 如果只返回 Nodes 能接受什么

- "给我这个 Patient 能到达的所有 Trust"——✅ 可以，一条 JOIN SQL 搞定
- "给我 `a(id) { b { c { name } } }` 的树"——❌ 不行，没有 Edges 无法重建路径
- "给我 `a(id) { b { name } }`"（1-hop）——✅ 可以，小改 `assemblePath` 跳过 edges 路径

**如果多跳只返回 Nodes，`assemblePath` 要么退化到逐跳 GetLinks（回到现有 BFS），要么多跳 GraphQL 展开不支持。** 这不是工作量问题，是信息缺失——没有 Edges 就没有拓扑。
