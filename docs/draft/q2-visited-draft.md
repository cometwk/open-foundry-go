## q2r-方案分析

我先给结论：

> **我不建议在 `Traverse` 层强制返回 `Visited`。**
>
> 更合适的折中是：**Traverse 默认只返回 `Nodes + Edges`，`Visited` 从 TraverseResult 中拿掉；GraphQL Expand 在确实需要中间节点 payload 时，再根据 `Edges` 批量补查中间对象。**
>
> 也就是把 `Visited` 从“Traversal 的基础结果”降级成“Expand 的派生数据”。

这比当前三桶模型更简单，同时又不会牺牲多跳 GraphQL。

---

# 1. 先把文档的核心矛盾理一下

当前实际上存在三个不同需求：

```text
                    Traverse
                       │
              ┌────────┴────────┐
              ↓                 ↓
       找到可达对象          GraphQL Expand
       Reachability          构造对象树
              │                 │
         只需要 Nodes       需要 Nodes
                           + Edges
                           + 中间对象 payload
```

而现在 `TraversalResult` 把三种东西全部放进去了：

```go
type TraversalResult struct {
    Nodes   []OntologyObject
    Edges   []OntologyLink
    Visited []OntologyObject
}
```

其中：

* `Nodes`：终点对象
* `Edges`：路径拓扑
* `Visited`：中间对象 payload

真正有争议的其实不是 `Nodes` 和 `Edges`。

**真正有争议的是：**

> 中间对象 payload 是否应该成为 Traverse 的标准输出？

从文档分析来看，答案其实比较明确：

**不一定。**

因为 `Visited` 的唯一核心消费者是 `assemblePath`。

---

# 2. 三个方案实际上是这样的

可以把文档里的方案压缩成：

| 方案 | Traverse 返回             | SQL | GraphQL Expand | 复杂度     |
| -- | ----------------------- | --- | -------------- | ------- |
| A  | Nodes + Edges + Visited | 最复杂 | 最简单            | 高       |
| B  | Nodes + Edges           | 简单  | **需要补查**       | **中低**  |
| C  | Nodes                   | 最简单 | 多跳无法构树         | 最低但能力损失 |

其中：

### A：全三桶

```text
Traverse
   │
   ├── Nodes
   ├── Edges
   └── Visited
```

优点：

```text
一次 SQL
   ↓
Nodes + Edges + Visited
   ↓
GraphQL Expand 直接 assemble
```

缺点是 Traverse 为了服务 GraphQL Expand，承担了很多额外职责。

文档估算需要：

* `PlanTraverse`
* `TraverseLayout`
* 三组 row mapper
* 三组去重

约 155 行左右。

---

### C：只 Nodes

这个方案虽然最简单，但其实不是一个真正合理的通用 Traverse。

因为：

```text
A → B → C
```

只返回：

```text
C
```

你根本不知道：

```text
A → 哪些 B → 哪些 C
```

所以多跳 GraphQL 树无法重建。

文档已经明确指出，`Edges` 是恢复拓扑的必要信息。

所以：

> **只 Nodes 可以作为某种“Reachability Query”的结果，但不适合作为通用 TraverseResult。**

---

# 3. 我最推荐的折中：Nodes + Edges

也就是：

```go
type TraversalResult struct {
    Nodes      []OntologyObject
    Edges      []OntologyLink
    TotalCount int
}
```

直接把：

```go
Visited
```

从 SPI 的基础契约中去掉。

这其实和 TS 侧现有定义也是一致的：

```ts
interface TraversalResult {
    nodes: OntologyObject[];
    edges: OntologyLink[];
    totalCount: number;
}
```

文档也特别指出了这一点。

---

# 4. 关键是：不要因为去掉 Visited 就回到 N+1

这是这个折中方案最重要的地方。

不要设计成：

```text
Traverse
  ↓
Nodes + Edges
  ↓
assemblePath
  ↓
一个一个 GetObject(B)
  ↓
N 次 SQL
```

这样确实会退化。

而应该：

```text
             Traverse
                │
                ↓
          Nodes + Edges
                │
                │
       GraphQL Expand 需要
       中间对象 payload？
                │
          ┌─────┴─────┐
          │           │
         No          Yes
          │           │
          ↓           ↓
       直接使用     从 Edges 提取
                    intermediate IDs
                         │
                         ↓
                  Batch GetObjects
                         │
                    WHERE id IN (...)
                         │
                         ↓
                    assemblePath
```

这就是一个非常自然的**按需物化（lazy hydration）**。

---

# 5. 为什么这个折中其实更合理

因为：

> **Topology 和 Object Payload 是两个不同层次的数据。**

`Edges`：

```text
A ──edge──> B ──edge──> C
```

解决的是：

> “谁和谁连接？”

而 Object：

```json
{
  "id": "B",
  "name": "Ward A",
  "type": "Ward"
}
```

解决的是：

> “这个节点有什么业务属性？”

因此 Traverse 最核心的职责应该是：

```text
Traversal
    =
    找路径
    +
    返回拓扑
    +
    返回终点对象
```

而不是：

```text
Traversal
    =
    找路径
    +
    返回拓扑
    +
    返回终点
    +
    把所有中间节点完整 hydrate
```

后者明显更偏向 GraphQL Expand 的需求。

---

# 6. SQL 也会非常漂亮

以：

```text
Patient
   ↓
Admission
   ↓
Ward
   ↓
Trust
```

为例。

不返回 Visited，可以直接：

```sql
SELECT
    n.*,

    e0.*,

    e1.*

FROM patient st

JOIN admission e0
  ON e0.from_id = st.id
 AND e0.tenant_id = st.tenant_id
 AND e0.deleted_at IS NULL

JOIN ward_trust e1
  ON e1.from_id = e0.to_id
 AND e1.tenant_id = e0.tenant_id
 AND e1.deleted_at IS NULL

JOIN trust n
  ON n.id = e1.to_id
 AND n.tenant_id = e1.tenant_id
 AND n.deleted_at IS NULL

WHERE st.id = ?
  AND st.tenant_id = ?
```

这里有一个很漂亮的性质：

```text
start
  ↓
link
  ↓
link
  ↓
node
```

而不需要：

```text
start
  ↓
link
  ↓
中间 object
  ↓
link
  ↓
node
```

文档也验证了这种 link-to-link JOIN 是可行的。

---

# 7. 然后 Expand 再做一次 Batch Hydration

假设返回：

```text
Edges:

A ──e0──> B1
A ──e0──> B2

B1 ──e1──> C1
B1 ──e1──> C2

B2 ──e1──> C3
```

Expand 从 Edges 可以得到：

```text
Intermediate IDs

B1
B2
```

然后：

```sql
SELECT *
FROM ward
WHERE id IN (?, ?)
  AND tenant_id = ?
```

得到：

```text
B1 payload
B2 payload
```

于是：

```text
Edges
+
Nodes
+
Batch Hydration
       ↓
assemblePath
       ↓
A
├── B1
│   ├── C1
│   └── C2
└── B2
    └── C3
```

这样：

* 没有 N+1
* Traverse 不需要知道 GraphQL
* SQL 不需要 JOIN 中间 object
* Expand 仍然可以完整构树

这就是我认为最好的折中。

---

# 8. 更进一步：甚至可以把补查做成 Expand 的统一机制

我会进一步把架构定义成：

```text
                    Traverse
                       │
                       ↓
               TraversalResult
                ┌──────┴──────┐
                ↓             ↓
              Nodes         Edges
                              │
                              ↓
                       Expand Planner
                              │
                 ┌────────────┴────────────┐
                 │                         │
           拓扑已经足够                需要 payload
                 │                         │
                 │                         ↓
                 │                  Batch Hydration
                 │                         │
                 └────────────┬────────────┘
                              ↓
                        assemblePath
```

这样职责非常清晰：

### Traverse

负责：

```text
Path → Nodes + Edges
```

### Expand

负责：

```text
Nodes + Edges
      ↓
必要时 hydration
      ↓
GraphQL Object Tree
```

这比：

```text
Traverse → Nodes + Edges + Visited
```

职责边界更干净。

---

# 9. 还有一个更好的优化：Visited 不必真的消失，而是变成“可选能力”

如果你担心未来某些调用方确实希望一次拿到中间对象，可以不要把它设计成核心 `TraversalResult`。

例如：

```go
type TraverseOptions struct {
    IncludeIntermediateObjects bool
}
```

默认：

```go
TraverseOptions{
    IncludeIntermediateObjects: false,
}
```

普通 Traverse：

```text
Nodes + Edges
```

GraphQL Expand 如果未来发现批量 hydration 不合适，可以：

```text
IncludeIntermediateObjects = true
```

得到：

```text
Nodes + Edges + Visited
```

于是形成：

```text
                 Traverse
                    │
          ┌─────────┴─────────┐
          ↓                   ↓
       default             optional
          │                   │
     Nodes+Edges      Nodes+Edges+Visited
          │
          ↓
    Expand hydration
```

不过我会把这个放在**第二阶段**，第一阶段甚至不需要实现。

---

# 10. 三种数据返回模型，我会这样定性

### ① Nodes Only

适合：

```text
Reachability / terminal query
```

例如：

```text
Patient → Trust
```

只问：

> “有哪些 Trust？”

可以。

但是不适合通用 Traverse。

---

### ② Nodes + Edges ← **推荐默认**

适合：

```text
通用 Traverse
Graph traversal
GraphQL Expand
Query IR
```

因为：

```text
Nodes = result
Edges = topology
```

信息足够完整，同时保持 SQL 和 SPI 简洁。

---

### ③ Nodes + Edges + Visited

适合：

```text
特殊场景
希望一次 Traverse 完成 hydration
```

但不应该成为默认协议。

---

# 11. 其实还有一个隐藏的好处

这个方案对你前面一直在考虑的：

> **Query IR → Planner → SQL**

也更友好。

Query IR 本身表达的是：

```text
A
 └─ relation B
      └─ relation C
```

Planner 最终主要需要生成：

```text
Traversal topology
```

即：

```text
Edges
```

而：

```text
B 的 name
B 的 sku
B 的其他 properties
```

属于后续 projection / object materialization 的事情。

所以可以形成非常自然的两阶段：

```text
Query IR
   │
   ↓
Traversal Planner
   │
   ↓
SQL JOIN
   │
   ├── Nodes
   └── Edges
          │
          ↓
     Expand / Hydrate
          │
          ↓
      Object Tree
```

这比把 `Visited` 强行塞进 TraversalResult 更符合分层设计。

---

# 12. 最终我会选择这个版本

如果让我直接替这个文档做架构决策，我会定：

```text
TraversalResult
├── Nodes
├── Edges
└── TotalCount
```

**去掉 Visited。**

然后：

```text
Traverse SQL
    ↓
Nodes + Edges
```

采用：

```text
link → link
```

直接 JOIN，避免中间 object JOIN。文档已经证明这条路线实现成本明显更低。

GraphQL Expand：

```text
Nodes + Edges
      ↓
从 Edges 得到 intermediate IDs
      ↓
按 Object Type 批量 GetObjects
      ↓
assemblePath
```

最终形成：

```text
                 ┌─────────────────┐
                 │   Query / IR    │
                 └────────┬────────┘
                          ↓
                 ┌─────────────────┐
                 │    Traverse     │
                 └────────┬────────┘
                          ↓
                  ┌───────┴───────┐
                  ↓               ↓
                Nodes           Edges
                  │               │
                  │         ┌─────┘
                  │         ↓
                  │   Intermediate IDs
                  │         │
                  │         ↓
                  │   Batch Hydration
                  │         │
                  └────┬────┘
                       ↓
                ┌───────────────┐
                │  assemblePath │
                └───────┬───────┘
                        ↓
                  GraphQL Tree
```

### 一句话总结

**不要为了避免一次补查 SQL，而把 `Visited` 提升成 Traverse 的基础协议。**

`Nodes + Edges` 已经保留了 Traverse 最重要的两个信息：

> **结果 + 拓扑。**

`Visited` 本质上只是：

> **为了 GraphQL 展开而进行的中间节点 hydration。**

因此把它放到 Expand 层，通过**按类型批量补查**解决，是我认为在“实现复杂度、性能、职责边界、未来扩展性”之间最均衡的方案。
