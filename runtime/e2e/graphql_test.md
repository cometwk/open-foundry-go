# `graphql_test.go` 测试说明

对应文件：`runtime/e2e/graphql_test.go`

本文件是 **图书馆简化版金路径 GraphQL e2e**：真实 library pack → Ontology IR → `api.Server.Exec` → Query IR → storage。不走 HTTP，不 mock SPI，也不数 GetLinks/Traverse——计数在 `runtime/api` 的进程内 Exec 测试里锁死；这里锁的是 **真实 pack 上的响应形状** 和 **001 行为不回退**。

HTTP 头、状态码、REST 形状由 `rest_test.go` 和 `runtime/api/http_test.go` 锁。

案例固定为 `domain-packs/library-pack/library-pack.md` 的**简化版**：Reader / Book / Branch，关系只有 `Borrows` / `RegisteredAt` / `AvailableAt`。种子走 `seeds/simple.yaml`（正常版同一批实例的投影），不另造一套对象。

覆盖计划 U7，以及 001 留下的 AE1–AE2 等 GraphQL 金门。

唯一测试函数：`TestGoldPath_GraphQL`。子测试用 `t.Run` 共用同一份 `setupGoldAPI` 数据。

存储后端由 `helper_test.go` 按环境选择：

- `TEST_DB_URL` **有值** → MySQL OBDA（隔离库 + `InitMappedSchema` + pack OBDA）
- **未设置** → memory

`.env` 由 helper 尽力加载（`go test` 直跑也能看到根目录 `TEST_DB_URL`）。

---

## 启动过程

```
setupGoldAPI (helper_test.go)
        │
        ├─ pack.LibraryPackDir / LoadDir
        ├─ TEST_DB_URL? mysqlobda : memory
        ├─ ApplySchema (tenant gold)
        ├─ seedLibrary → seeds/simple.yaml
        └─ api.New
```

租户由 `Server.Exec` 的 `RequestContext.TenantID` 注入，不再读 HTTP 头。

跑法：

```bash
cd runtime && go test ./e2e/ -count=1 -run GraphQL
```

---

## 种子 `seedLibrary`

租户 `gold`。对象 + 链接来自简化版 `seeds/simple.yaml`：14 个对象、21 条边。测试只使用其中这些实例：

```
xiao_hong(小红, BASIC, 西区馆) ──RegisteredAt──► branch_west（西区馆）
xiao_ming(小明) ──Borrows──► book_tb1（三体·卷1）──AvailableAt──► 主馆 / 西区馆
lao_wang(老王)  ──Borrows──► book_tb1
xiao_ming / lao_wang ──RegisteredAt──► branch_central（主馆）
book_tb2（三体·卷2）──AvailableAt──► 主馆
book_sapiens（人类简史）──AvailableAt──► 主馆 / 西区馆
book_quantum（量子纠缠导论）无 Borrows 入边
```

| `libraryIDs` | 对象 | 图边 |
| ------------ | ---- | ---- |
| `xiaoHong` | 小红 | RegisteredAt → 西区馆；无 Borrows |
| `sapiens` | 人类简史 | AvailableAt → 主馆、西区馆 |
| `tb1` | 三体·卷1 | 被 小明、老王 Borrows |
| `tb2` | 三体·卷2 | AvailableAt → 主馆（主馆 readers = 小明、老王） |
| `quantum` | 量子纠缠导论 | **无 Borrows** |
| `west` | 西区馆 | 被 小红 RegisteredAt |

`book_quantum` 故意没有 `Borrows`，这样 `borrowers` 必须是空列表；`book_tb1` 有两条入边，用来证明 `@link` 不是靠书名字段猜出来的。

---

## 子测试

### AE1 get three types

对简化版三个 ObjectType 各 Exec 一次 get，只要不报错：

`reader` / `book` / `branch`。

证明 IR 投影的 SDL 能 parse、get 根字段能绑上、种子对象能被读到。走 Query IR `Get`。

### AE2 borrowers nested

```graphql
{ book(id: $tb1) { borrowers { name } } }
```

`Book.borrowers` 是 `@link(Borrows, INBOUND)`。子选择只有标量 `name`，没有嵌套 `@link` → GraphQL 分类为 **GetLinks**（本文件不计数，形状上要拿到 `小明` 和 `老王`）。

反例：`borrowers { isbn }` 必须 schema error——`isbn` 是 Book 字段，不是 Reader 字段。用来防止「嵌套对象投影偷了父类型字段」。

### two hop branches readers

金 2 跳，只断言 GraphQL 树。REST follow 在 `rest_test.go`。

```graphql
{
  book(id: $tb2) {
    branches { name readers { name id } }
  }
}
```

嵌套 `@link` 走一条 Traverse。断言：`branches` 长度 1，`name == 主馆`，`readers` 为 小明、老王。

路径词汇是 **已声明** `@link` **字段名**，不是 SPI `AvailableAt` / `OUTBOUND`。

### borrowers empty without Borrows

反假绿。查 `book_quantum`：

```graphql
{
  book(id: $quantum) {
    title
    borrowers { name }
  }
}
```

| 字段 | Role | 数据来源 | 期望 |
| ---- | ---- | -------- | ---- |
| `title` | 标量 | 对象字段 | `"量子纠缠导论"` |
| `borrowers` | `@link(Borrows, INBOUND)` | 图边；本条没有 CreateLink | `[]` |

如果有人把书名字段误当成借阅关系，本用例会红。

### list search aggregate

001 读动词不回退：

- `books(first: 1)` Relay 连接，`totalCount >= 1`
- `searchBooks(query: "人类简史")` 命中（仅 memory；MySQL 尚未声明 `search.fields`）
- `searchBooks(query: "  ")` 空白查询 **0 条**（不是 GraphQL 校验错误）
- `bookAggregate` COUNT 成功

都走 Query IR 的 List / Search / Aggregate。

### readers filter by branchId

仅 MySQL。`branchId` 是 `RegisteredAt` 宿主列的只读投影，不进 `fields`。memory 不 assemble 该键，子测试 Skip。

```graphql
{
  readers(filter: { branchId: { eq: $west } }) {
    totalCount
    edges { node { id name branchId } }
  }
}
```

西区馆有小红、小李。`reader(id: $xiaoHong) { branchId }` 等于西区馆 id。写入 `branchId` 由 Engine / mysqlobda 硬拒绝，不在本文件测。

### nested registered_at and borrows

1. `reader { branch { name } borrowedBooks { title } }`
   `branch` 是 `@link(RegisteredAt, OUTBOUND)`。小红注册在西区馆、在借为 0。
2. `book { branches { name } }`
   `branches` 是 `@link(AvailableAt, OUTBOUND)`。人类简史在主馆 + 西区馆。

### cross tenant

同一 query，`TenantID` 改成 `other`：

- get → `book: null`（不是 error）
- list / search → `totalCount == 0`

memory 按 `_tenantId` 隔离；Query IR 没有另做租户逻辑。

---

## 辅助函数

`gql` 调用 `Server.Exec`，把 `json.RawMessage` 解成 `data` / `errors`。业务错误在 `errors` 数组里（例如 `borrowers { isbn }`）。

---

## 本文件不测什么（有意留给别层）

| 缺口 | 谁锁 |
| ---- | ---- |
| HTTP 头、状态码、REST GET / follow | `rest_test.go`、`runtime/api/http_test.go` |
| 1 跳 GetLinks 次数、2 跳 Traverse 次数、禁止双执行 | `runtime/api/resolvers_test.go` |
| Execute 与 Engine 结果相等、分叉两次 Traverse | `runtime/query/execute_test.go` |

e2e GraphQL 只证明：**真实 library pack + Exec** 上，1 跳嵌套仍绿、2 跳树有值、没有 `Borrows` 边时 `borrowers` 为空。
