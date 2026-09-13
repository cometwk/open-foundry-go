# `rest_test.go` 测试说明

对应文件：`runtime/e2e/rest_test.go`

本文件是 **图书馆简化版金路径 REST e2e**：真的起 `httptest` 服务器，经 `GET /api/v1/...` 打完整栈（pack → Ontology IR → Engine → Query IR → storage）。不 mock SPI。锁的是 **对外响应形状**（状态码、`id` 不是 `_id`、follow 终点）。

GraphQL 形状由 `graphql_test.go` 用 `Server.Exec` 锁；HTTP 中间件细节（非法 follow path、1 跳也走 Traverse）由 `runtime/api/http_test.go` 锁。

案例与种子和 `graphql_test.go` 相同：简化版 Reader / Book / Branch，`seeds/simple.yaml`。

唯一测试函数：`TestGoldPath_REST`。子测试用 `t.Run` 共用同一台服务器和同一份 `setupGoldHTTP` 数据。

存储后端选择与 GraphQL e2e 相同（`TEST_DB_URL` → MySQL，否则 memory）。

---

## 启动过程

```
setupGoldHTTP (helper_test.go)
        │
        ├─ setupGoldAPI（pack / storage / seed / api.New）
        └─ httptest.NewServer
```

租户头一律 `X-OpenFoundry-Tenant`。

跑法：

```bash
cd runtime && go test ./e2e/ -count=1 -run REST
```

---

## 子测试

### two hop follow

```http
GET /api/v1/book/{tb2}/follow?path=branches,readers
```

REST follow **一律 Traverse**（含 1 跳也是）。响应只有终点 `{ "nodes": [ { id, name, ... } ] }`，没有中间 Branch。

三体·卷2 只在主馆；主馆 readers = 小明、老王。断言 200、2 个 node、名字为小明和老王、不暴露 `_id`。

路径词汇是 **已声明** `@link` **字段名**，不是 SPI `AvailableAt` / `OUTBOUND`。公开 GraphQL **没有** 根字段 `follow`；REST `.../follow` 才是通用图查询。

```mermaid
flowchart LR
    B[Book 三体·卷2] -->|AvailableAt OUTBOUND| C[Branch 主馆]
    C -->|RegisteredAt INBOUND| R[Reader 小明 / 老王]
    R --> N["nodes = 终点 Reader"]
```

### AE7 REST book

```http
GET /api/v1/book/{id}
GET /api/v1/book/missing
```

200 体含 `title=人类简史` 且 `id`（不是 `_id`）。缺失 → 404 `OBJECT_NOT_FOUND`。REST GET 走 Query IR `Get`。

### cross tenant

同一 URL，头改成 `other` → 404。memory 按 `_tenantId` 隔离。

### missing tenant

不带头或空白头 → 400 `MISSING_TENANT`（`withTenant` 中间件）。

---

## 辅助函数

| 函数 | 方法 | 路径 | 行为 |
| ---- | ---- | ---- | ---- |
| `rest` | GET | 调用方拼 URL | 返回 `(status, body)`；可空 tenant 测缺头 |

---

## 本文件不测什么

| 缺口 | 谁锁 |
| ---- | ---- |
| GraphQL 嵌套树、schema error、list/search/aggregate | `graphql_test.go` |
| 1 跳 follow 也走 Traverse、非法 path 零 SPI | `runtime/api/http_test.go` |
