---
title: "跨方言移植 OBDA Provider：预设的 SQL 分叉可能是幻觉，真正的分叉在 DB 强制不变式里"
date: 2026-08-28
category: design-patterns
module: "runtime/obda/dialect/mysql + runtime/storage/mysqlobda (OBDA MySQL storage provider)"
problem_type: design_pattern
component: database
severity: medium
applies_when:
  - "Porting an OBDA StorageProvider or SQL dialect adapter to a new database dialect"
  - "Translating SQLite partial unique indexes (CREATE UNIQUE INDEX ... WHERE deleted_at IS NULL) to MySQL 8, which has no partial indexes"
  - "Enforcing soft-delete cardinality uniqueness in MySQL, where NULLs are distinct in unique indexes and would never collide"
  - "Assessing cross-dialect write-path divergence from syntax feature checklists (e.g. INSERT ... RETURNING) before porting"
tags: [mysql, sqlite, obda, dialect-porting, soft-delete, unique-index, partial-index, generated-column]
---

# 跨方言移植 OBDA Provider：预设的 SQL 分叉可能是幻觉，真正的分叉在 DB 强制不变式里

## Context（背景）

本次工作把 SQLite OBDA provider 移植到 MySQL：新增 `runtime/obda/dialect/mysql/`（方言）与 `runtime/storage/mysqlobda/`（存储 provider），驱动为 `github.com/go-sql-driver/mysql v1.10.0`（`runtime/go.mod:13`），通过 blank import 注册到 `database/sql`（`runtime/storage/mysqlobda/provider.go:12`，注释明确说明 provider 只说 `database/sql` 和驱动错误文本）。

移植前的需求/计划阶段做过一次"方言分叉预判"。计划文档写道："The single forced SQL divergence is MySQL's lack of `RETURNING`"（`docs/plans/2026-08-27-001-feat-mysql-obda-provider-plan.md:36`），对应需求 R5："MySQL has no `RETURNING`; the insert and update paths must still return the full created/updated object ... without a return clause"（`docs/plans/2026-08-27-001-feat-mysql-obda-provider-plan.md:58`）。

动手读代码后发现了两件事：

1. **预判的分叉是幻觉。** 参考实现（sqliteobda）根本不依赖 `RETURNING`，所以"MySQL 没有 RETURNING"不构成任何移植工作量（见 Guidance 第 1 节的证据链）。
2. **真正被强制出现的分叉藏在 DDL 里**：软删除基数唯一索引。sqlite 用部分索引（partial index）只让"活着的行"参与唯一性约束，MySQL 8 没有部分索引；直接照搬列会因 MySQL 唯一索引的 NULL 语义导致基数约束**静默失效**（见 Guidance 第 2 节）。

这条分叉有其历史前因（session history）：sqliteobda 的早期形态把身份与系统列放在 `of_*` sidecar 表里，2026-08-21 的 direct-native 重构删掉了全部 sidecar，系统列（含软删标记 `deleted_at`）变成业务表原生列。正是这一决策让"软删留行"与"基数 UNIQUE"落进同一张物理表，冲突才开始爆发——sqlite 可用部分索引规避，MySQL 无此机制，被迫寻找等价表达。同期设计还确认了"基数由物理 UNIQUE 约束兜底"（ApplySchema introspect UNIQUE、DDL helper 生成 UNIQUE），这为 `of_active` 生成列方案提供了直接前提。

## Guidance（核心做法与机制）

### 1. 方法论先行：预判特性缺口前，先 grep 参考实现是否真的用了那个特性

"SQLite 支持 `INSERT ... RETURNING` 而 MySQL 不支持"作为语法特性清单是成立的，但作为移植分叉是错的——参考实现从未走上那条路。证据链：

- `runtime/obda/planner.go:42-59` 的 `PlanCreateObject` 只构造 `&sqlast.Insert{Table: ..., Columns: ids, Values: params}`，从不设置 `sqlast.Insert.Returning` 字段（该字段定义在 `runtime/obda/sqlast/ast.go:47`）；整个 `planner.go` 中没有出现 `Returning`。
- `runtime/storage/sqliteobda/objects.go:65-86` 的 `createObjectTx` 是"先插后读"：第 79 行调 `insertBusiness`（纯 INSERT），第 85 行 `return p.loadObject(...)` 重读整行返回。MySQL 侧完全同构：`runtime/storage/mysqlobda/objects.go:61-81` 同样是 `insertBusiness`（75 行）→ `loadObject`（81 行）。
- sqlite 方言的 `renderInsert` 确实有 `Returning` 分支（`runtime/obda/dialect/sqlite/dialect.go:193-203`，拼接 `" RETURNING ..."`），但因为 provider 路径永远不会设置该字段，这个分支在 provider 路径上是**死代码**。
- mysql 方言的 `renderInsert` 对 `Returning` 显式返回 `spi.ErrUnsupportedCapability`（`runtime/obda/dialect/mysql/dialect.go:192-193`，上一行注释写明 "MySQL has no RETURNING; the provider inserts then re-reads the row"）。这不是为了绕过缺口，而是**防止将来有人设置 Returning 时被静默丢弃**的守卫。

结论：插入/更新路径 MySQL 与 sqlite 零分叉，照搬即可。计划里 R5 的工作量实际是"什么都不用做"。

### 2. 真正的强制分叉：软删除基数唯一索引

**sqlite 侧机制 —— 部分唯一索引。** `runtime/obda/dialect/sqlite/ddl.go:215-241` 的 `uniqueIndex` 在未省略 `deletedAt` 时给唯一索引追加 `WHERE deleted_at IS NULL`（`ddl.go:238`）：

```sql
-- runtime/obda/dialect/sqlite/ddl.go:232-238 生成
CREATE UNIQUE INDEX IF NOT EXISTS "admission_from_active"
  ON "admission" ("tenant_id", "from_id") WHERE "deleted_at" IS NULL
```

只有活跃行（`deleted_at IS NULL`）参与唯一性冲突，软删除的行立刻让出"槽位"。

**MySQL 8 没有部分索引。** 天真的逐列照搬 `UNIQUE (tenant_id, from_id, deleted_at)` 是**错的**（详见 Why This Matters）。

**MySQL 侧机制 —— 虚拟生成列 + 唯一索引。** `runtime/obda/dialect/mysql/ddl.go` 的做法：

- `ActiveKeyColumn = "of_active"`（`ddl.go:18`），其文档注释（`ddl.go:13-17`）完整陈述了动机与 NULL 语义："1 while the row is live, NULL once soft-deleted ... NULL values never collide, so at most one live row per endpoint is enforced exactly like sqlite"。
- 生成列 DDL 由 `activeKeyDef()` 生成（`ddl.go:152-162`，实际文本在 161 行）：

```sql
`of_active` TINYINT GENERATED ALWAYS AS (IF(`deleted_at` IS NULL, 1, NULL)) VIRTUAL
```

- 是否需要该列由 `needsActiveKey(l)` 决定（`ddl.go:209-219`）：`l.Omit.DeletedAt` 为真则不需要；基数是 `MANY_TO_ONE` / `ONE_TO_MANY` / `ONE_TO_ONE` 时需要。建表时由 `createTableStmt` 的 `activeKey` 参数追加（`ddl.go:197-203`）。
- 唯一索引在 `activeKey` 为真时把 `of_active` 追加到列尾（`runtime/obda/dialect/mysql/ddl.go:277-283`，索引名沿用 sqlite 的 `_from_active` / `_to_active` 命名，见 `ddl.go:221-249`）：

```sql
-- runtime/obda/dialect/mysql/ddl.go:260-285 生成
CREATE UNIQUE INDEX `admission_from_active`
  ON `admission` (`tenant_id`, `from_id`, `of_active`)
```

语义等价性：活跃行 `of_active = 1`，同一 `(tenant_id, from_id, 1)` 第二次插入必然冲突；软删除行 `of_active = NULL`，MySQL 唯一索引中 NULL 互不相撞，永远不会挡住新链接。**利用了同一个"NULL 不冲突"规则，方向相反**：天真方案被它坑，正确方案靠它实现"软删除让位"。

**Schema 校验同构镜像。** `runtime/obda/dialect/mysql/introspect.go:137-164` 的 `HasUniqueIndex(indexes, columns, requireActiveKey)`：`requireActiveKey` 为真时把 `ActiveKeyColumn` 追加进期望列集（`introspect.go:139-141`），即要求 information_schema 里看到的唯一索引确实带 `of_active` 后缀（注释见 `introspect.go:134-136`）。DDL 生成与 introspection 校验两侧共用同一常量，防止漂移。

## Why This Matters（为什么重要）

**错误分叉预判的代价是双向的**：预算和注意力被花在一个不存在的问题（RETURNING）上，而真正会造成数据损坏的问题（部分索引）差点漏掉。

若按"列语义等价"的天真思路移植为 `UNIQUE (tenant_id, from_id, deleted_at)`：MySQL 唯一索引把 NULL 视为互不相同，两条**活跃**链接（`deleted_at = NULL`）不会冲突——`MANY_TO_ONE` 基数约束**静默失效**，不报错、不建索引失败，只在数据里悄悄出现"一个病人同时住两个病房"这类违规。这是最危险的一类移植 bug：语法全部正确，约束语义整体丢失。

测试已把正确行为锁死：

- `runtime/storage/mysqlobda/links_test.go:35-52` `TestCardinalityManyToOne`：第二条活跃链接必须得到 `spi.ErrCardinalityViolation`。
- `runtime/storage/mysqlobda/links_test.go:74-85` `TestCardinalitySoftDeleteFreesSlot`：`CreateLink` → `DeleteLink`（软删）→ 再次 `CreateLink` 必须成功（断言 "soft-deleted link must free the active slot"）。
- `runtime/obda/dialect/mysql/ddl_test.go:77-92` `TestMappedTableStatementsLinkActiveKey`：断言生成列文本与索引列 `(\`tenant_id\`, \`from_id\`, \`of_active\`)`，并断言 sqlite 的部分索引谓词 `WHERE deleted_at IS NULL` **不得**出现在 MySQL DDL 中——锁死的是"语义等价、语法必须分叉"。
- 同文件 `TestMappedTableStatementsOmitDeletedAtDropsActiveKey`、`TestHasUniqueIndex` 覆盖省略 `deletedAt` 的退化路径与 `of_active` 后缀匹配。

移植完成时的验证记录：全仓测试套件通过；53 个 MySQL 集成测试对真实 MySQL Community Server 8.0.43 全绿（经 `TEST_DB_URL` 环境变量接入）。

## When to Apply（何时适用）

- 把任何带 **DB 强制不变式**（唯一性、约束、外键行为、错误语义）的存储 provider / DDL 生成器移植到另一个 SQL 方言时——本例是 SQLite → MySQL 8 的 OBDA provider。
- 在计划阶段基于"方言特性清单"得出"某功能不支持，需要分叉方案"的结论时：**先读引用实现确认该特性在 provider 实际路径上被用到**，再决定是否分叉。
- 枚举分叉点时，不要只对比 SQL 语句形状（INSERT/UPDATE/CTE 语法），要逐项枚举**数据库强制的不变式**（唯一性如何排除行、NULL 在索引中如何比较、约束违规如何报错），并为每个不变式找目标方言的对应机制。
- 审查跨方言移植 PR 时，重点看"约束从 A 方言机制翻译到 B 方言机制"的那几行 DDL——那里是静默语义丢失的高发区，语句翻译反而低风险。

## Examples（具体示例）

**反例：天真的列级照搬（会导致基数静默失效）。**

```sql
-- 错误：MySQL 唯一索引中 NULL 互不相撞，
-- 两条 deleted_at = NULL 的活跃链接不会冲突，
-- MANY_TO_ONE 约束静默失效。
CREATE UNIQUE INDEX `admission_from_active`
  ON `admission` (`tenant_id`, `from_id`, `deleted_at`);
```

**正解：生成列 + 唯一索引（与 sqlite 部分索引语义等价）。**

```sql
-- 建表（runtime/obda/dialect/mysql/ddl.go:161,197-203）
`of_active` TINYINT GENERATED ALWAYS AS (IF(`deleted_at` IS NULL, 1, NULL)) VIRTUAL

-- 唯一索引（runtime/obda/dialect/mysql/ddl.go:260-285）
CREATE UNIQUE INDEX `admission_from_active`
  ON `admission` (`tenant_id`, `from_id`, `of_active`);
-- 活跃行 of_active=1 → 冲突被拦；软删除行 of_active=NULL → 永不冲突，槽位释放。
```

**插入路径无分叉的证据链（逐条可复核）。**

1. `runtime/obda/planner.go:43-58`：`PlanCreateObject` 返回的 `sqlast.Insert` 只有 `Table/Columns/Values`，无 `Returning`。
2. `runtime/storage/sqliteobda/objects.go:79,85`：`createObjectTx` = `insertBusiness`（INSERT）+ `loadObject`（重读）。
3. `runtime/obda/dialect/sqlite/dialect.go:193-203`：sqlite `renderInsert` 的 `RETURNING` 分支存在但 provider 路径不可达。
4. `runtime/obda/dialect/mysql/dialect.go:192-193`：mysql `renderInsert` 对 `Returning` 显式报 `spi.ErrUnsupportedCapability`，是防静默丢弃的守卫而非缺口补丁。
5. `runtime/storage/mysqlobda/objects.go:75,81`：MySQL 完整复刻 insert-then-load，无任何方言分叉代码。

**给下一次的方法清单。**

- (a) 计划围绕某特性缺口展开前，先 grep 参考实现里该特性的实际使用点（如本例 `grep -n Returning runtime/obda/planner.go runtime/storage/sqliteobda/`——结果为零）。
- (b) 枚举每一个 DB 强制不变式（唯一性、约束、级联、错误码），而不只是 SQL 语句形状，并为每个不变式找目标方言的机制（本例：部分索引 → 生成列 + NULL 唯一性规则）。
- (c) 让 DDL 生成与 schema introspection 校验共用同一常量（`ActiveKeyColumn` 同时被 `ddl.go:18` 与 `introspect.go:140` 引用），并用集成测试对着真实数据库锁死语义。

## Related（相关文档）

- `docs/plans/2026-08-27-001-feat-mysql-obda-provider-plan.md` — 本次 MySQL provider 移植的需求计划；其 R5「无 RETURNING 分叉」预判被本文第 1 节推翻。
- `docs/plans/2026-08-21-001-feat-obda-sqlite-storage-provider-plan.md` — 被移植的 sqlite 参考实现计划；U6 记录部分唯一索引的基数兜底。
- `docs/design/obda-spec-v3.md` / `docs/design/open-foundry-obda-mapping-spec-v3.md` — 规范层写明 `CREATE UNIQUE INDEX ... WHERE deleted_at IS NULL` 的部分索引 DDL，即 MySQL `of_active` 机制所等价替代的机制。
- `docs/brainstorms/2026-08-21-obda-mysql-storage-provider-requirements.md` — MySQL OBDA 的原始架构愿景（sidecar 具体条款已被 2026-08-27 计划收窄）。
