# `just test 1` 测试说明

对应代码：`runtime/e2e/graphql_test.go` 的 `t.Run("just test 1", ...)`。

查询：

```graphql
{ book(id: $tb1) { borrowers { name } } }
```

`$tb1` 是种子 `book_tb1`（三体·卷1）。`Book.borrowers` 在 ODL 上声明为 `@link(type: "Borrows", direction: INBOUND)`。子选择只有标量 `name`，没有再嵌套 `@link`。

种子（`library-pack.md` 简化版 / `seeds/simple.yaml`）：

```
xiao_ming(小明) ──Borrows──► book_tb1
lao_wang(老王)  ──Borrows──► book_tb1
```

期望树（AE2 已锁形状；本子测试只 dump JSON）：`borrowers = [小明, 老王]`。

---

## 1. 这条查询走哪条 Expand 路径

GraphQL 编译器按「子选择里还有没有 `@link`」分流（`runtime/api/compile.go` `compileExpand`）：

| 子选择 | Expand 模式 | SPI |
| ------ | ----------- | --- |
| 只有标量（本例 `name`） | `ExpandGetLinks` | `GetLinks` + `hydrateByIDs` |
| 再嵌套 `@link` | `ExpandTraverse` | `Traverse` + `hydrateEdges` |

本例是 **1 跳叶子**，**不进 Traverse**。`Visited`、`Nodes+Edges` 列桶投影、link-to-link JOIN，都不会出现。

执行链：

```
Get(Book, tb1)
        │
        ↓
expandGetLinks
        │
        ├─ GetLinks(tb1, Borrows, inbound, Limit=HopCap)
        │     → 两条 junction 边（from=读者, to=书）
        │
        └─ hydrateByIDs(Reader, [小明, 老王])
              → QueryObjects(or-of-eq on _id)
```

这和 `docs/solutions/architecture-patterns/traverse-nodes-edges-batch-hydration.md` 的 **Example 1（一跳叶子去 N+1）** 是同一条路：拓扑一次 `GetLinks`，payload 按邻接类型一次 `QueryObjects`。它**不是**同文档里的多跳 `expandTraverse`。

---

## 2. 是否符合设计

对照两份设计：

- `docs/draft/q2-visited-draft.md`：Traverse 默认只回 `Nodes + Edges`，中间对象 payload 降到 Expand 层按需批量补查。
- `docs/solutions/architecture-patterns/traverse-nodes-edges-batch-hydration.md`：一跳叶子和多跳共用 `hydrateByIDs`（or-of-eq）；禁止 per-neighbor `GetObject`。

| 设计要点 | 本测试实际 | 判定 |
| -------- | ---------- | ---- |
| 1 跳叶子走 GetLinks，不走 Traverse | 5 条 SQL 里没有 chained-JOIN Traverse | 符合 |
| 拓扑与 payload 分层 | GetLinks 只扫 `borrows` 列；Reader 另一次 QueryObjects | 符合 |
| 批量补查，禁止 N+1 GetObject | 2 个读者 1 次 `id = ? OR id = ?`，不是 2 次 GetObject | 符合 |
| GetLinks JOIN peer 只为可见性 | `INNER JOIN reader` 且 `p.deleted_at IS NULL`；SELECT 没有 reader 业务列 | 符合 |
| 租户 + 软删走现有读路径 | 每条 SQL 都带 `tenant_id = gold` 和 `deleted_at IS NULL` | 符合 |
| or-of-eq 只含 eq 叶子 | `(id = ?) OR (id = ?)` | 符合 |
| Traverse 去掉 Visited，引擎从 Edges hydrate | **本查询不调用 Traverse** | **未覆盖** |
| 多跳 `link → link` JOIN，不 JOIN 中间 object | 未出现 | **未覆盖** |

**结论：查询形状和发出的 SQL 符合计划的 F2（一跳叶子），不是 F1（Traverse 链式 JOIN）。** 下面那条 Patient → Trust 的 link-to-link SQL，这次测试**不可能**打出来。

---

## 2.1 计划目标其实是两条流，不是一条 SQL

`docs/plans/2026-09-11-002-refactor-traverse-nodes-edges-plan.md` 的 Goal 是 `Nodes + Edges` + 引擎批量水合。落地时 **按 GraphQL 选择形状分流**（Product Contract F1 / F2）：

```
GraphQL 嵌套 @link          →  F1 Traverse（链式 JOIN + 逐跳列桶）
GraphQL 叶子 @link（本测试） →  F2 GetLinks once + hydrateByIDs
REST follow                 →  F1 Traverse
```

draft §6 / 你贴的那段：

```sql
FROM patient st
JOIN admission e0      -- hop 1 link
JOIN ward_trust e1     -- hop 2 link（e1.from_id = e0.to_id，不 JOIN ward）
JOIN trust n           -- 只 JOIN 终点对象
```

是 **F1、且 hop ≥ 2、且两跳都是 junction** 时的理想形状。它要解决的问题是：不要为了 `Visited` 去 JOIN 中间对象表。

本测试是：

```graphql
{ book(id: $tb1) { borrowers { name } } }
```

只有 1 跳、子选择没有 `@link`。编译器按计划 R8 / F2 走 `expandGetLinks`。1 跳没有「中间实体表」可跨，`PlanTraverse` 根本不会被调用。所以 SQL 是 `GetLinks(borrows)` + `QueryObjects(reader or-of-eq)`，这是预期，不是实现偏了。

要对齐那段描述，查询必须长成 F1 的触发器，例如同文件的 `two hop branches readers`：

```graphql
{ book(id: $tb2) { branches { name readers { name id } } } }
```

`branches` 里还嵌了 `readers` → `ExpandTraverse`。

还要注意两点，即使换 2 跳，**落库 SQL 也不会字面等于 draft §6**：

1. **金路径第二跳是 inline。** `RegisteredAt` 没有 link 表（FK 在 `reader.branch_id`）。计划 Compatibility 写明：draft §6 的直链 JOIN **只对 junction 跳成立**；inline 必须 JOIN 宿主/对端来合成 Edges。library 的 2 跳是 `AvailableAt`（junction）+ `RegisteredAt`（inline），不是 Patient / Admission / Ward / Trust 那种双 junction。
2. **本轮实现没改 JOIN 形状。** U3 / KTD2：`JOIN/WHERE/Order/args 全部不变`，只在 SELECT 后面追加 hop 列桶。`PlanTraverse` 注释仍是「每跳 = link 表 + 目标对象表」。所以全 junction 的 2 跳实际是：

```text
FROM start s0
JOIN link0 l0 → target0 s1      -- 中间对象表仍在（deleted_at / 下一跳挂点）
JOIN link1 l1 → terminal s2
SELECT s2.* , l0.* , l1.*       -- Nodes + 两跳 Edges；不投 s1 业务列
```

中间对象 **payload** 不进 Traverse（这是去掉 `Visited` 的核心）；中间对象 **表** 仍 JOIN，用来滤软删、接下一条边。draft 里「跨过 Ward 表、JOIN 数最少」是方案分析里的理想 SQL，计划明确当成「只加投影、不重写 JOIN」的本轮范围。`Branch.name` 仍由 Expand 对 Edges 做一次 `QueryObjects` 补上。

本子测试本身的缺口（相对 AE2）：

- 只断言 `errors` 为空并 `t.Log` JSON，不锁 `小明` / `老王`。
- 查询与 AE2 完全重复；e2e 层也不计数 SPI 调用（计数在 `runtime/api/resolvers_test.go`）。

---

## 3. SQL 与调用一一对应

MySQL 分页惯例：每个会分页的 SPI 读都是 **COUNT 派生表 + 数据页**。`HasNextPage` 用 `LIMIT limit+1`。`HopCap=1000`，但 `pageLimitOffset` 当前硬顶 `MaxPageLimit=10`（`runtime/storage/mysqlobda/query.go`），所以 GetLinks 的 `LIMIT` 参数是 `10+1=11`（日志里的 `'\v'`）。

### SQL 1 — 根 `Get(Book)`

```sql
SELECT id, tenant_id, available_copies, ..., deleted_at
FROM book
WHERE (tenant_id = ?) AND (id = ?)
-- ["gold", tb1]
```

`book(id:)` 根字段。还不是 Expand。起点 payload 已在这里拿到，后面不会再 Get 这本书。

### SQL 2 — `GetLinks` 的 TotalCount

```sql
SELECT COUNT(*) FROM (
    SELECT l.id, l.tenant_id, l.from_id, l.to_id, ...
    FROM borrows AS l
    INNER JOIN reader AS p
      ON l.from_id = p.id AND l.tenant_id = p.tenant_id
    WHERE l.tenant_id = ? AND l.to_id = ?
      AND l.deleted_at IS NULL
      AND p.deleted_at IS NULL
    ORDER BY l.id ASC
) AS q
-- ["gold", tb1]
```

inbound：书在 `to_id`，读者在 `from_id`。JOIN `reader` **不是**把读者 hydrate 进 link 行，而是藏掉对端已软删的边（`GetLinksHidesDeletedPeer`）。投影只有 `l.*`。

派生表包一层是 MySQL `COUNT(*)` 的现成写法；这里没有多 hop 列桶，不会撞 Error 1060（那是 Traverse count 才要缩投影的问题）。

### SQL 3 — `GetLinks` 数据页

与 SQL 2 同一 SELECT，加 `LIMIT ? OFFSET ?`，参数 `["gold", tb1, 11, 0]`。

`expandGetLinks` 从每条边取对端 id（inbound → `from_id`），去重后得到两个 Reader id。**到这里只有拓扑，没有 `name`。**

### SQL 4 — `hydrateByIDs` 的 TotalCount

```sql
SELECT COUNT(*) FROM (
    SELECT id, tenant_id, current_borrow_count, membership_level,
           name, registered_days, ...
    FROM reader
    WHERE (tenant_id = ?)
      AND ((id = ?) OR (id = ?))
      AND (deleted_at IS NULL)
    ORDER BY id ASC
) AS q
-- ["gold", 小明id, 老王id]
```

`hydrateByIDs`：`QueryObjects(Reader, Or{eq _id, eq _id}, Limit=len(ids))`。这就是设计里的 **or-of-eq 批量补查**。两个 id，一次 SQL，不是两次 `GetObject`。

`QueryObjects` 自己也算 TotalCount，hydrate 其实只用 `page.Items`。COUNT 是分页协议带出来的，不是 Expand 多要的。

### SQL 5 — `hydrateByIDs` 数据页

与 SQL 4 同一 SELECT，加 `LIMIT / OFFSET`。`Limit=2` → 实绑 `2+1` 看下一页。读出 `name` 等业务列，拼进 `borrowers { name }`。

---

## 4. 和「不该出现」的 SQL 对比

若仍是 PR #8 之前的 N+1：

```
Get(Book)
GetLinks(Borrows inbound)
GetObject(Reader, 小明)     ← 禁止
GetObject(Reader, 老王)     ← 禁止
```

实际没有按 id 单条 `GetObject(Reader)`。

若误走了 Traverse（本查询不该）：

```
Get(Book)
Traverse COUNT   -- 单列投影的 derived-table count
Traverse page    -- book 起点 JOIN borrows，投影 terminal + edge bucket
hydrateByIDs     -- 一跳 Traverse 的 terminal 已在 Nodes 里，通常不再补 Reader
```

实际是 `GetLinks`（`borrows` 为主表、`l.to_id = book`），不是 `PlanTraverse` 的 chained JOIN。

`GetLinks` 已经 JOIN 了 `reader`，却仍再 `QueryObjects(reader)`，看起来像重复读。这是分层故意留下的：JOIN 只过滤活着的对端；payload 统一走 `QueryObjects`，和多跳 `hydrateEdges` 同一条可见性 / 软删 / 租户通道。第一阶段不把 peer 列并进 GetLinks 投影。

---

## 5. 一句话

`just test 1` 的 SQL 序列是：

```
Get(Book) → GetLinks COUNT/page(Borrows inbound) → QueryObjects COUNT/page(Reader or-of-eq)
```

这锁住了 **1 跳叶子的 Nodes/Edges 分层 + 批量 hydration**，与 q2 折中方案和 PR #8 playbook 的一跳半边一致。它**没有**覆盖 Traverse 去掉 `Visited`、按 hop 投影 Edges、或引擎 `hydrateEdges`。要验那一半，看 `two hop branches readers`。
