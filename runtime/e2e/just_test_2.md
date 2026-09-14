# 2 跳 `branches { readers }`：最优 SQL vs 实际 6 条

查询：

```graphql
{ book(id: $tb2) { branches { name readers { name id } } } }
```

图：`三体·卷2 → 主馆 → 小明、老王`。只要分馆名、读者名和 id。

---

## 1. 抛开设计，最优只要 1 条

这条需求的数据全在一张 JOIN 里：书在哪几个馆、馆叫什么、馆里有哪些读者。

```sql
SELECT
    s1.id, s1.name,          -- 分馆
    s2.id, s2.name           -- 读者
FROM book AS s0
JOIN available_at AS l0
  ON l0.from_id = s0.id
 AND l0.tenant_id = s0.tenant_id
 AND l0.deleted_at IS NULL
JOIN branch AS s1
  ON s1.id = l0.to_id
 AND s1.tenant_id = l0.tenant_id
 AND s1.deleted_at IS NULL
JOIN reader AS s2
  ON s2.branch_id = s1.id
 AND s2.tenant_id = s1.tenant_id
 AND s2.deleted_at IS NULL
WHERE s0.tenant_id = ?
  AND s0.id = ?
```

一行就是「一个馆 + 一个读者」。应用按 `s1.id` 收成树即可。根字段没选书的标量，书只要用来定位，不必再查一次。

**1 条。** 不是 6 条。

再宽松一点，根解析器先把书拿出来，也只要 **2 条**：`Get(book)` + 上面这条 JOIN。

---

## 2. 实际为什么是 6 条，以及为何仍算「符合预期」

6 条不是这条查询算出来的，是 **三层通用协议叠出来的**：

```
根 Get          读起点
Traverse        只交「终点 + 边」，不交中间对象字段
QueryObjects    再按 id 批量补中间对象
```

每一层读又固定是 **COUNT + 数据页**（分页协议）。所以：

| # | SQL | 谁发的 | 干什么 |
| - | --- | ------ | ------ |
| 1 | `FROM book WHERE id = tb2` | 根 `book(id:)` | 解析起点 |
| 2 | 同一条 `FROM book` | Traverse 开头的存在性检查 | 起点没了就别跑 JOIN |
| 3 | `COUNT(*) FROM (s0→l0→s1→s2 只选 s2.id)` | Traverse 分页 | 总行数 |
| 4 | 同一 JOIN，选出 `读者整行 + available_at 整行 + 读者整行` | Traverse 数据页 | Nodes + 两跳 Edges |
| 5 | `COUNT(*) FROM branch WHERE id = 主馆` | 引擎补查 | 分页协议带出来的 |
| 6 | `FROM branch WHERE id = 主馆` | 引擎补查 | 只要 `name` |

没有 GetLinks，也没有按读者拆开的 GetObject。6 条里真正「找图」的只有 **3+4**；**5+6** 是因为第 4 条故意不选 `s1.name`；**1+2** 是起点读了两遍。

### 执行顺序（人话）

1. GraphQL 先按 id 把书读出来（SQL 1）。
2. 看到嵌套 `@link`，调用 Traverse。Traverse 先再确认书还在（SQL 2），然后一条链式 JOIN 走出「书 → 上架边 → 馆 → 读者」。
3. JOIN 形状已经包含 `branch`，但 SELECT **不取馆的业务列**，只取：
   - 读者整行 → 终点 Nodes（小明、老王的 name/id 这里就有了）
   - `available_at` 行 → 第 1 跳边
   - 读者行再投一次 → 第 2 跳边（注册关系是 inline，没有独立边表）
4. 组树还缺馆的 `name`。引擎从边上抽出主馆 id，一次 `QueryObjects` 补上（SQL 5–6）。一个馆，所以 `WHERE (id = ?)`，不是两条 Get。

```
SQL 1–2   书
SQL 3–4   图：谁连谁 + 读者 payload
SQL 5–6   馆 payload
        →  { book: { branches: [{ name: 主馆, readers: [小明, 老王] }] } }
```

### 为啥这叫「符合预期」（相对当前架构，不是相对最优）

当前约定是：**Traverse 负责拓扑和终点，中间对象字段留给 Expand 批量补。**

所以：

- 该走 Traverse（嵌套 `@link`），走了，没有退化成多次 GetLinks。
- 该一次 JOIN 走完 2 跳，走了（SQL 4 就是 `book → available_at → branch → reader`）。
- 该不把 `Visited` / 馆字段塞进 Traverse，没塞（SQL 4 没有 `s1.name`）。
- 该按类型补一次，补了（SQL 5–6 只有 Branch，读者不再查）。

多出来的 5 条，各自有层内理由，但都不是这条 GraphQL「必须」的：

| 多出来的 | 原因 | 这条查询里多余吗 |
| -------- | ---- | ---------------- |
| SQL 2 再 Get 一次书 | provider 怕起点已删 | 是，根已经读过 |
| SQL 3 / 5 的 COUNT | 所有列表读都先数总数 | 是，这里只要一页树 |
| SQL 5–6 再查馆 | 分层：JOIN 过馆却不取 `name` | 是，SQL 4 加 `s1.id, s1.name` 就能省掉 |

**符合预期 = 符合「Nodes + Edges + 按类型补查」这条实现约定。**  
**不等于** 这条需求的 SQL 已经最优。最优是 1 条（最多 2 条）；6 条是通用分页和分层换来的。
