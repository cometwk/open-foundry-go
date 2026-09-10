---
title: "MySQL OBDA 的 FULLTEXT 契约按列清单 fail-closed，搜索绑定顺序跟 `?` 出现位走"
date: 2026-09-10
category: design-patterns
module: "runtime/obda + runtime/obda/dialect/mysql + runtime/storage/mysqlobda (AggregateObjects / SearchObjects)"
problem_type: design_pattern
component: database
severity: medium
applies_when:
  - "Adding FULLTEXT (or any dialect-named index) to an OBDA mapping whose compile layer must stay dialect-neutral"
  - "Verifying declared indexes on ApplySchema without coupling introspection to generated index names"
  - "Rendering MySQL statements that repeat the same bind value (MATCH in SELECT and WHERE) while the driver only has positional `?`"
  - "Unifying filter compilation across Query/Aggregate/Search so non-eq operators cannot silently become equality"
tags: [mysql, obda, fulltext, match-against, filter, dialect-neutral, bind-order, fail-closed]
---

# MySQL OBDA 的 FULLTEXT 契约按列清单 fail-closed，搜索绑定顺序跟 `?` 出现位走

## Context（背景）

`mysqlobda` 补齐 `AggregateObjects` 与 `SearchObjects` 时，搜索不是单点 SQL，而是一条必须接通的链路：映射 `search.fields` → `CompiledModel.SearchableFields` → `CREATE FULLTEXT INDEX` → ApplySchema verify → `MATCH (...) AGAINST (?)` 查询。

有三条容易走错的分叉：

1. **把 FULLTEXT 索引名烧进方言中立层。** 编译产物曾经预留 `SearchIndex` 字符串。MySQL 派生名是 `ft_<table>`，sqlite FTS5 是另一套虚拟表名。若 compile 填充索引名，verify 再按名核对，DDL helper 与 introspection 会在两层各维护一份命名规则。
2. **MySQL `?` 按出现顺序绑定，AST 的 `Param.Position` 不能当复用槽。** Search 的 SQL 在 SELECT 里先写 `MATCH ... AGAINST (?) AS of_score`，WHERE 里才是 `tenant = ? AND MATCH ... AGAINST (?)`。若 planner 按「位次 1 = 租户」返回 `[tenant, query, query]`，驱动会把租户绑进 SELECT 的 MATCH，查询静默错位。
3. **filter 编译曾忽略 `Operator`。** QueryObjects 把 `ne`/`gt` 静默当成 `eq`。新接口若照抄会继承这个缺陷；三接口必须在同一条规则上显式拒绝。

InnoDB 默认 `innodb_ft_min_token_size = 3`，短于 3 字符的词不命中——这是引擎行为，测试 fixture 用 ≥3 字符的词规避，不在 provider 里补救。

## Guidance（核心做法与机制）

### 1. 索引名留在 MySQL 方言；verify 只认列清单 + FULLTEXT 类型

Compile 只填充 `SearchableFields`（物理列）。`PlanSearch` 的空映射守卫是 `len(SearchableFields) == 0`，不再看 `SearchIndex`。

DDL 发射在方言内派生 `ft_<table>`（`runtime/obda/dialect/mysql/ddl.go` `fulltextIndexName`），与 `uniqueIndexName` / `ActiveKeyColumn` 同层。`MappedTableStatements` 在 unique 语句后追加独立的 `CREATE FULLTEXT INDEX`，不把 FULLTEXT 塞进 `CREATE TABLE`。

Introspection 不读索引名：`HasFulltextIndex(indexes, columns)` 按 **排序后的列清单** + `INDEX_TYPE = FULLTEXT` 匹配。BTREE 同列不命中。因此改派生名不会让 verify 假绿或假红——名称的唯一消费方是 DDL 文本。

ApplySchema 对声明了 search 的表强制核对，缺失报 `spi.ErrSourceSchemaDrift`（fail-closed，镜像 unique verify）。未声明 search 的模型不发射、不核对。

### 2. Search 绑定顺序跟 SQL 文本里 `?` 的出现位走

MySQL 方言的 placeholder 恒为 `?`，`database/sql` 按出现顺序绑定，忽略 AST 里的 `Param.Position`。

渲染形状（`renderSelect` 在 `Search != nil` 时追加 score 列并落入共享 ORDER/LIMIT 路径，不再早退）：

```sql
SELECT `cols...`, MATCH (`name`, `city`) AGAINST (?) AS `of_score`
FROM `t`
WHERE `tenant_id` = ? AND MATCH (`name`, `city`) AGAINST (?)
ORDER BY `of_score` DESC, `id` ASC
LIMIT ? OFFSET ?
```

因此无 filter 时 `PlanSearch` 返回 `args = [query, tenant, query]`：SELECT MATCH、租户、WHERE MATCH。带 eq filter 时为 `[query, tenant, filter, query]`——filter 的 `?` 落在 WHERE 树里、第二次 MATCH 之前。分页再追加 `limit+1` / `offset`。计数子查询去掉 ORDER BY 与 LIMIT，仍消费同一组 MATCH/租户/filter 参数。

sqlite 方言继续读 `FullTextMatch.Source`（FTS5 虚拟表），本次不改（无生产调用方）。AST 同时保留 `Source` 与 `Columns`。

### 3. 三接口 filter 只放行单叶子 eq

`compileFilter`（planner）与 `translateFilter`（mysqlobda）同一规则：`Operator == ""` 或 `"eq"` 视为等值；其余操作符与 And/Or/Not 一律 `fmt.Errorf("%w: unsupported filter ...", spi.ErrInvalidMapping)`。

共享 `compileFilter` 会改变 sqliteobda.QueryObjects 的非 eq 行为（静默 eq → 显式错误）。这是预期一致化；既有 sqlite 测试只传空/eq filter。

聚合入参校验（空 Fields、非法 fn、`sum(*)`）走普通 `fmt.Errorf` 文本，不新造 sentinel——映射错误才用 `ErrInvalidMapping`。

聚合 tiebreak 不能 ORDER BY 裸 identity 列（`ONLY_FULL_GROUP_BY`），改为隐藏 `MIN(identity) AS of_tiebreak`。搜索无 GROUP BY 限制，`of_score DESC` 后接 identity 裸列即可。

## Why This Matters（为什么重要）

FULLTEXT 若按名 verify：DDL 改名或手工建了同列不同名的索引，要么假漂移、要么假通过。按列清单核对的是「声明的列是否可被 MATCH」，这才是 R7 的契约。

参数错位不会报错。租户字符串进 `AGAINST (?)` 的结果是零命中或错命中，集成测试若不断言 score>0 与排序关系就会放行。U4 金样锁完整 SQL 文本，planner 测试锁 `[query, tenant, query]`。

filter 静默 eq 会让 `ne` 过滤器返回「等于该值」的行。QueryObjects 已有回归（`TestQueryFilterNonEqRejected`）；Search/Aggregate 走同一 `translateFilter`。

## When to Apply（何时适用）

- 在方言中立的 compile/planner 里表示「需要某种索引」，但索引命名是方言细节时：compile 只带列/类型期望，名称 helper 放方言包，introspection 按可观察属性匹配。
- 渲染重复表达式（两处 MATCH、两处同一参数）且驱动没有命名参数时：**以 SQL 文本 `?` 出现顺序为 args 合同**，不要用 AST Position 当复用 id。
- 多个查询入口共享 filter 编译时：在所有编译点同时收紧，避免「新接口显式报错、旧接口静默执行」。

## Examples（具体示例）

**DDL（名称只出现在这一句）：**

```sql
CREATE FULLTEXT INDEX `ft_patient`
  ON `patient` (`patient_name`, `city_name`)
```

**Verify 命中规则：** 同列 BTREE 不计数；列序不同但集合相同则命中。缺索引 → `ErrSourceSchemaDrift`（`TestApplySchemaFulltextVerify`）。

**错误的 Search args（会把租户绑进 MATCH）：**

```
args = [tenant, query, query]  // 与 SELECT MATCH 先行的 SQL 错位
```

**正确的 Search args：**

```
args = [query, tenant, query]  // 对应 SELECT MATCH、WHERE tenant、WHERE MATCH
```

**filter：** `Operator: "ne"` → `ErrInvalidMapping`；`Operator: ""` 与 `"eq"` 放行。

## Related（相关文档）

- `docs/plans/2026-09-10-001-feat-mysqlobda-aggregate-search-plan.md` — 本次计划；KTD3/KTD6/KTD8 是上述三条的决策原文。KTD6 初稿写过 `[tenant, query, query]`，执行时按 MySQL `?` 出现位改为 `[query, tenant, query]`。
- `docs/solutions/design-patterns/mysql-port-divergence-of-active-unique-index.md` — 同构先例：DDL 与 introspection 共享约定、精确文本断言 + 真 MySQL 集成锁语义。
- `docs/design/obda-spec-v3.md` — SearchObjects / AggregateObjects 既定闭环。
