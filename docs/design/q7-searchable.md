# Q7: OBDA searchable 映射与索引契约

**决定：`search.fields` 模型级块声明搜索面；`InitMappedSchema` 建 FULLTEXT、`ApplySchema` 按列强制 verify；`SearchQuery.Fields` 必须为空或与声明集合完全一致，不一致返回 `ErrUnsupportedCapability`；`AggregateObjects`/`SearchObjects` 的 `Filter` 复用 `QueryObjects` 现状（单叶子字段），不在本轮扩展 And/Or/多操作符编译。**

延续 `docs/design/obda-spec-v3.md` §14.4「计划中的收口」里 Search 的行为闭环（无映射 + 非空 query → `ErrUnsupportedCapability`；空白 query → 空 hits）——spec 定了行为，没定映射语法、DDL 职责归属、`Fields` 交互与 Filter 范围，本文件把这四点补齐。聚合走 SQL `GROUP BY` 原生实现，不做应用层聚合；这条与本文件的 search 决定同属一轮讨论，一并记录。

---

## 决定 1：`search.fields` 模型级块

```yaml
models:
  Patient:
    ...
    search:
      fields: [name, city]   # 逻辑字段名，声明顺序 = 复合 FULLTEXT 索引列顺序
```

编译为 `CompiledModel.SearchableFields`（物理列）+ 一个派生名字的复合 FULLTEXT 索引（建议 `ft_<table>`）。`Model`（`runtime/obda/mapping.go:30`）现有的 `Identity`/`Tenant`/`System` 都是模型级策略块，`search` 与其风格一致；字段级 `searchable: true` 会打破这个约定，且丢失"这几个字段是一组复合索引"的分组语义。

`verify` 按列清单核对索引存在，不核对索引名——这是抄 `verifyUniques`（`runtime/storage/mysqlobda/schema.go:68-81`）已有的模式，唯一索引 verify 只按 `spec.Columns` 匹配，从不比较索引名字符串。索引名因此不需要在 YAML 里让用户指定，`search: { index: ft_patient, fields: [...] }` 这种显式命名是多余自由度。

**已拒绝的方案**
- 字段级 `searchable: true` 标记：声明散落在 `fields` 里，且需要额外的分组机制才能表达"这些字段共用一个复合索引"。
- 显式索引名 `search: { index: ..., fields: [...] }`：verify 逻辑不按名字匹配，操作员填的名字和实际建的索引名可能对不上，是没有对应校验兜底的自由度。

---

## 决定 2：DDL 建索引与 verify 都对称套用 unique 索引的现有模式

`PhysicalSchema`（`runtime/obda/physical.go:43`）是唯一真源，`PhysicalTable.Uniques` 同时喂给 `MappedTableStatements`（建表建索引）和 `verifyMappedSchema`（fail-closed 核对），两条路径读同一份数据、天然不会跑偏。`search.fields` 复用这条管线：

- `PhysicalTable` 加 `Search *SearchSpec`（列清单），由 `CompiledModel.SearchableFields` 填入；
- `MappedTableStatements`（`runtime/obda/dialect/mysql/ddl.go`）在现有 unique 索引循环旁边加 `CREATE FULLTEXT INDEX`；
- `verifyMappedSchema` 加 `verifySearch`，复用 `InspectIndexes` 已经查询的 `information_schema.STATISTICS`，多取一列 `INDEX_TYPE`，判断该列集合上是否存在 `FULLTEXT` 类型索引——按列匹配，不看名字，与 `HasUniqueIndex` 同构。

已有库表补声明时，操作员需先建好 FULLTEXT 索引才能 `ApplySchema` 成功，这与唯一索引现状的操作员体验一致。

**已拒绝的方案**
- 只 verify 不建：`MappedTableStatements`/`verifyMappedSchema` 在唯一索引上从未有过"只查不建"的先例，为 search 破例会让 `InitMappedSchema` 的语义不完整。
- 索引可选、查询时 introspect 降级：会把 fail-closed 检测从"provider 激活时一次性做完"拆回请求路径，破坏"激活后物理 schema 假设稳定"这个不变量，且与已有 unique 索引的处理方式不一致。

---

## 决定 3：`SearchQuery.Fields` 与声明集合必须精确匹配

`Fields` 为空 → 搜索 `SearchableFields` 声明的全集；非空 → 必须与声明集合完全相等，否则 `ErrUnsupportedCapability`。

原因是物理约束，不是任意选择：MySQL `MATCH(col1, col2)` 要求存在恰好覆盖 `(col1, col2)` 的 FULLTEXT 索引，复合索引不能服务子集 MATCH。声明 `search.fields: [name, city]` 建出的是一个复合索引，`query.Fields: [name]` 这种收窄在物理上跑不通，除非为每个子集组合单独建索引（成本转嫁到映射语法和 DDL，本轮不做）。

报错 sentinel 用 `spi.ErrUnsupportedCapability`，不是新造一个 `ErrInvalidArgument`：现有 sentinel 列表（`runtime/spi/errors.go`）和 spec 的错误分类表（`docs/design/obda-spec-v3.md` §14.3）里都没有 `ErrInvalidArgument`；`ErrUnsupportedCapability` 的定义原文就是"a request the dialect or compiled binding cannot execute"，与"无 searchable 映射却查"是同一族问题（当前物理绑定做不到这个请求形状），spec 已经把后者定为 `ErrUnsupportedCapability`，`Fields` 子集不匹配应并入同一条规则，不新增错误分类。

**已拒绝的方案**
- 支持收窄子集：允许子集 MATCH 需要映射侧支持声明多组 `search` 块（每组一个索引），映射语法、DDL、verify 都要按组扩展，成本明显上升，本轮不做。
- 忽略 `query.Fields`：SPI 参数变成摆设，调用方容易误以为传参生效，实际被静默丢弃。

---

## 决定 4：`AggregateObjects`/`SearchObjects` 的 `Filter` 复用 `QueryObjects` 现状

`AggregateQuery.Filter`、`SearchQuery.Filter` 与 `QueryObjects` 的 filter 编译能力一致：单叶子字段可用，`And`/`Or`/`Not` 不支持。范围克制——filter 编译能力（And/Or 递归、`gt`/`gte`/`lt`/`lte`/`in` 等操作符本身的语义）是独立任务，扩展后三个查询接口一起受益，不为这两个新接口单独扩大 `obda` planner / dialect `renderPred` 的改动面。

**修正（原判断的前提有误，重新拍板）**：`compileFilter`（`runtime/obda/planner.go:437-449`）和各 provider 的 `translateFilter`（如 `runtime/storage/mysqlobda/query.go:120-133`）现状不是"非 `eq` 操作符报错"，而是**只要 `Field` 非空就无条件编译成 `eq`，从不检查 `Operator` 的值**——传 `Operator: "gt"` 会被静默当成等值比较，不会报错、不会跳过。

处理方式：**在共享的 `compileFilter` 里补一行显式校验（`Field != "" && Operator != "" && Operator != "eq"` → `ErrUnsupportedCapability`），三个接口（`QueryObjects`/`AggregateObjects`/`SearchObjects`）统一变成"仅支持 `eq`，其他操作符报错"，包括修掉 `QueryObjects` 现有的静默 eq。** 不是"新接口报错、`QueryObjects` 留着已知问题"：`compileFilter` 是 `obda` 包里唯一一份共享函数，`PlanQuery` 已经在用，`PlanAggregate`/`PlanSearch` 加 filter 支持时自然也会调它——三个接口迟早共享同一份编译逻辑，要让 `QueryObjects` 继续保留静默 eq 反而需要为它单独开一个"宽松模式"分支，比直接修共享函数更贵。这不是扩大 filter 编译能力（And/Or、其他操作符语义仍不做），只是把一个从未被正确实现过的检查补上，且是唯一能让三个接口对同一 `spi.FilterExpression` 语义保持一致的做法。

**已拒绝的方案**
- 顺带扩展 filter 编译（And/Or 递归 + 常用操作符语义）：任务范围显著变大，动 `obda` planner + `renderPred` + 各自测试，留给独立任务。
- 新接口暂不支持 `Filter`：聚合场景下"先过滤再聚合"是高频需求，等于核心场景不可用，过于保守。
- 只让新接口报错、`QueryObjects` 保留静默 eq：三个接口共享同一份 `compileFilter`，专门为保留一个已知错误行为再开分支或复制函数，比直接修共享函数成本更高，且让同一 `FilterExpression` 语义在三个接口上表现不一致。

---

## 决定 5：打分与高亮取 MySQL 原生口径；两条已知行为记录不修复

`Score` 直接使用 `MATCH(...) AGAINST(...)` 返回的相关性原值，不做归一化、不跨 provider 对齐数值范围。`Highlights` 对 `search.fields` 声明的每个字段返回整字段值（不截片段），且**不做逐字段命中检测**——不判断某个具体列是否真的包含匹配词，命中即对所有声明字段整值返回。

"整字段值而非片段"延续 `runtime/storage/memory/provider.go:1494-1498` 已有行为（注释：`Highlights push the entire field value (mirrors TS)`），跟随参考实现，不是新发明。但两者不完全一致：memory provider 在字段级别仍做了命中判断，只有 `count > 0` 的字段才进 `highlights[field]`（`provider.go:1571-1578`），是精确到字段的；mysqlobda 因为 MySQL 复合 FULLTEXT 索引的 `MATCH()` 只在行级别返回相关性、不暴露是哪一列命中，要拿到逐列信息只能对每个字段单独发一条 `MATCH(单列) AGAINST(...)`——这与决定 3 的单复合索引架构矛盾，且是每条命中额外 N 次 SQL，违反 spec §8.9「最大化 source pushdown」。因此 mysqlobda 的 highlights 精度低于 memory provider，这是 MySQL 物理限制决定的降级，不是随意选择，跨 provider 的这个行为差异需要留痕，不能被当成两个 provider 语义一致。

两条随之而来的后果记为已知行为，不作为缺陷：

- **已有库表补 `search.fields` 必须先手动建好 FULLTEXT 索引才能 `ApplySchema` 成功。** 这是决定 2（Init 建 + ApplySchema 强制 verify、fail-closed）本身的必然推论，不是新的选择。
- **MySQL InnoDB 默认 `innodb_ft_min_token_size = 3`，两字符短词搜不到。** 这是只读的 MySQL 服务器级配置，修改需要改 `my.cnf` 并重建所有 FULLTEXT 索引，不能按查询覆盖，OBDA 运行时无法在单次请求里绕过。真要支持短词搜索是部署时的运维决定（调整该变量并重建索引），不得在应用层用子串匹配悄悄"修复"，否则打分/高亮就不再是纯 MySQL 原生口径。

**已拒绝的方案**
- 逐字段跑 `MATCH(单列)` 精确判断命中列：额外 N 次查询，且与单复合索引架构冲突。
- 应用层对短词做子串兜底搜索：打破"打分/高亮走 MySQL 原生口径"的一致性，且需要额外维护一套与索引结果不一致的过滤逻辑。
