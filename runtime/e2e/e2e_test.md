## 方案：用 `E2E_DB_MODE` 分三条路

只影响 e2e；`testdb.Open` 给 mysqlobda 单测用，行为不变。e2e 按模式选连接方式。

| 模式                        | 何时用           | 数据库动作                                                                   |
| --------------------------- | ---------------- | ---------------------------------------------------------------------------- |
| **unset / `fresh`**（默认） | CI、日常         | 情况 1：删表 → 建表 → seed → 测 → 再删表                                     |
| **`init`**                  | 手动准备一次库   | 删表 → 建表 → seed → **不删表**                                              |
| **`reuse`**                 | 反复只读、看代码 | 只连库 → `ApplySchema`（校验已有表）→ 按字段查 ID → 测 → **不 seed、不删表** |

`reuse` 时不再 `ApplySeeds`（避免重复插入），只 `lookupBy` 拿现有 ID。memory 后端没有持久化，`init`/`reuse` 只在 `TEST_DB_URL` 有值时生效。

## 入口怎么拆

- **情况 1**：`TestGoldPath_GraphQLREST_HTTP` 默认 `fresh`，`go test ./e2e/` 不变
- **情况 2 初始化**：独立 `TestInitGoldDB`，默认 Skip；只有 `E2E_DB_MODE=init` 才跑
- **情况 2 测试**：同一个 `TestGoldPath_...`，`E2E_DB_MODE=reuse` 时走「只连接」

```bash
# 情况 1（默认）
cd runtime && go test ./e2e/ -count=1 -run GraphQLREST

# 情况 2：先准备库
E2E_DB_MODE=init go test ./e2e/ -count=1 -run TestInitGoldDB

# 情况 2：反复只读
E2E_DB_MODE=reuse go test ./e2e/ -count=1 -run GraphQLREST
```

## SQL 打印

`sqlopen.Open("mysql", dsn)` 注册的是 `mysql-hooks`，每条 SQL 和耗时打到 stdout。手动看代码时有用，CI 的 `fresh` 会刷屏，所以按模式绑死，不再加开关：

| 模式 | 连接 | 打印 SQL |
| ---- | ---- | -------- |
| `fresh` / `testdb.Open` | `sql.Open("mysql", …)` | 否 |
| `init`（`OpenKeep`） | `sqlopen.Open("mysql", …)` | 是 |
| `reuse`（`Connect`） | `sqlopen.Open("mysql", …)` | 是 |

改动点只在 `testdb.open`：`OpenKeep` / `Connect` 走 hooks，`Open` 不动。mysqlobda 单测仍安静。
