# `library_test.go` 测试说明

对应文件：`runtime/e2e/library_test.go`

本文件是 **library-pack 的 Gold Path 验收**：用真实领域包走一遍 runtime 主链路，确认「从 pack 到引擎动词」是通的，而不是各层各自能编译。

不把 pack 拷进 `runtime/`。测试通过 `pack.LibraryPackDir()` 从仓库根加载 `domain-packs/library-pack`，pack 仍是唯一真相源。案例固定为 `library-pack.md` 的**简化版**：Reader / Book / Branch，关系只有 `Borrows` / `RegisteredAt` / `AvailableAt`。

存储停在 **memory + Engine + SPI**，不起 HTTP，也不依赖真实 MySQL。往上的 GraphQL 形状由 `graphql_test.go`（`Server.Exec`）锁，REST 形状由 `rest_test.go` 锁。

---

## 测什么

两条验收：

| 函数 | 阶段 | 覆盖 |
| ---- | ---- | ---- |
| `TestGoldPath_Library_F8` | Phase 3 | AE11 / F8 / R11 |
| `TestGoldPath_BorrowBook_F1` | Phase 4 | AE6 / F1 |

数据用简化案例（小红、人类简史），不另造一套对象。

### `TestGoldPath_Library_F8`

加载真实 pack → 投影成存储 schema → 灌进 memory provider → 用简化图书馆模型走一遍对象和边的 CRUD，再加上查询、图遍历、事务回滚、软删。

```
pack.LoadDir
    → IR
    → ProjectStorage
    → memory.ApplySchema
    → Engine 动词 + query / traverse / transaction / soft-delete
```

证明 Phase 1 → Phase 3 整条管道接线正确，已实现的 SPI **不会再撞 `ErrUnimplemented`**。

### `TestGoldPath_BorrowBook_F1`

加载 `BorrowBook` 动作，走 CEL 前置条件求值。**Evaluate 本身不能写库**；通过后再由调用方手写 `UpdateObject` / `CreateLink` 完成借书。

---

## 有什么意义

这是 runtime 的冒烟验收：单测保证零件对，这条测试保证零件能装成一辆能开的车。

- 证明 pack 加载、IR、schema 投影、`ApplySchema`、Engine 动词、SPI（query / traverse / transaction / soft-delete）是串起来的。
- 用最小但真实的领域故事（馆藏、读者、借阅关系）当验收场景，比纯 mock 更能抓住「层之间没接上」的问题。
- `graphql_test.go` / `rest_test.go` 再往上走 GraphQL 与 REST；本文件停在 Engine + memory SPI。
