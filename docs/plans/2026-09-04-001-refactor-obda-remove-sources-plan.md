---
title: OBDA Remove Sources - Plan
type: refactor
date: 2026-09-04
topic: obda-remove-sources
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
product_contract_source: ce-brainstorm
execution: code
---

# OBDA Remove Sources - Plan

## Goal Capsule

- **Objective:** 永久移除 OBDA v3 映射中的 `sources` / `sourceRef` / `dsnRef` 概念——YAML 只描述逻辑映射,唯一数据库连接由组装层从外部配置注入,`dsnRef`/`DSNRefs` 的每一行管道代码消失,零残留。
- **Product authority:** 用户于 2026-09-04 brainstorm 裁定:删除是永久性的,不留兼容层;规划综合确认旧 YAML 键在 parse 阶段显式拒绝。
- **Execution profile:** `runtime/` Go 模块(`go test`)与 `docs/design/obda-spec-v3.md`;单分支交付,代码先行、spec 收尾。
- **Open blockers:** 无——origin 的两个 Deferred to Planning 问题已由 KTD-1 / KTD-2 解决。
- **Stop conditions:** 若删除暴露验证清单之外的 `Sources` 消费者(未见过的调用方),停下上报而非顺手扩散改动。

---

## Product Contract

Product Contract unchanged.

### Summary

移除 OBDA v3 YAML 中的 `sources` 块与 model/link 的 `sourceRef`,DSN 完全退出映射文档:组装层从环境配置(如 `DB_URL`)打开 `*sql.DB`,经 `provider.Open(db, mapping, opts)` 注入,方言由所选 provider 包决定。规格文档、Go 代码、全部 fixture 与测试同步清理。

### Problem Frame

当前 `sources`/`dsnRef` 机制是三层失效的死管道。其一,真实连接早已在组装层打开(`runtime/bootstrap/conf.go` 的 `Open()` 用 `sql.Open(c.DBDriver, c.DBURL)`),`provider.Open` 收到的是现成的 `*sql.DB`,YAML 里的 `dsnRef` 间接层没有任何消费者——`Compile()` 从不读 `SourceRef`,`CompiledModel`/`CompiledLink` 中不存在 source 概念。其二,组装路径传入的 `DSNRefs` 是空 map,而 provider 对每个 `dsnRef` 做 map 成员检查,结果是任何真实映射都保证解析失败:截至本计划,`runtime/storage/sqliteobda` 的 3 个事务测试正以 `unresolved dsnRef "primary"` 失败。其三,v3 规则本就要求所有可写绑定落在同一连接(单一事务域),多源声明在语义上不可用。保留这套机制只产生维护成本,并误导读者以为 YAML 能选择数据源。

### Key Decisions

- **永久删除,无兼容层。** 不为旧形状 YAML 提供迁移工具、兼容承诺或告警通道;`apiVersion` 保持 `openfoundry.io/obda/v1` 不变,新契约即唯一契约。
- **单一事务域 = 单一外部连接。** 连接由组装层注入,provider 不自行开库;跨域写入返回 `ErrTransactionDomain`(哨兵保留,当前单连接路径不触发)。
- **方言由 provider 包选择决定。** 选 `sqliteobda` 即 SQLite,选 `mysqlobda` 即 MySQL;YAML 与 Source 均不再承载 dialect 字段。
- **保留明文凭证键拒绝。** `dsn`/`password`/`uri`/`url`/`token`/`secret`/`user`(大小写不敏感)在 parse 阶段拒绝的安全闸与 sources 机制无关,继续生效;公开错误与 `HealthCheck.Details` 不得泄露 DSN 的既有约束不变。
- **两个 provider 同步同改。** sqliteobda 与 mysqlobda 的契约保持一致,不允许一方残留 dsnRef 解析。

### Requirements

**规格文档(`docs/design/obda-spec-v3.md`)**

- R1. 全文移除 `sources` / `sourceRef` / `dsnRef` 概念;§4.3 改述为全局 DSN 注入契约(环境配置 → `sql.Open(dialect, dsn)` → `provider.Open(db, mapping, opts)`)。
- R2. spec 明确陈述:DSN 不出现在 `*.obda.yaml` 中,YAML 只描述逻辑映射、不持有任何连接信息。
- R3. spec 明确陈述:方言由所选 provider 包决定,不设 YAML dialect 字段。

**YAML 契约**

- R4. `*.obda.yaml` 顶层与 model/link 绑定不再含 `sources` / `sourceRef` / `dsnRef` 键;`apiVersion` 保持 `openfoundry.io/obda/v1` 不变。
- R5. 明文凭证键在 parse 阶段拒绝的行为原样保留。

**Go 实现**

- R6. `runtime/obda`:删除 `Document.Sources` 字段、`Source` 与 `Connection` 类型、`Model.SourceRef` / `Link.SourceRef` 字段。
- R7. `runtime/obda`:Validate 移除 sources 必填检查、sourceRef 解析检查及 `dsnRefName` 正则。
- R8. `runtime/storage/sqliteobda` 与 `runtime/storage/mysqlobda`:删除 `Options.DSNRefs` 字段与 `doc.Sources` 解析循环,provider 不再解析任何连接引用。
- R9. `runtime/bootstrap`:删除 `Bootstrap.DSNRefs` 与 `Config.DSNRefs` 字段、空 map 传递及 `mergeSources` / Sources 合并逻辑。

**测试与 fixture**

- R10. 全部 9 个 `*.obda.yaml` fixture(sqliteobda 4、mysqlobda 4、supply-chain pack 1)移除 `sources` 块与 `sourceRef` 字段。
- R11. obda / bootstrap / 两个 provider 包中 sources 相关的负例与辅助测试随契约删除或改写。
- R12. 当前失败的 3 个 sqliteobda 事务测试(`TestTransactionRollbackHidesWrites` / `TestTransactionCommitVisible` / `TestConcurrentGetDuringTx`)转绿。

### Scope Boundaries

- 多数据源 / 跨库写入——永久排除,与"所有可写绑定共享单一事务域"的既有规则冲突。
- 不新增 YAML 迁移工具、兼容层或告警通道。
- 不改 `apiVersion`,不重命名 provider 包。

### Success Criteria

- `grep -rin "sourceref\|dsnref" runtime/ domain-packs/` 除 U4 拒绝点外零命中。允许存在：`runtime/obda/parse.go`（键比较与错误文本）及其负例 `runtime/obda/parse_test.go`。该处是这些 token 唯一被允许的存在。`Sources` / `mergeSources` 由 R6/R9 与编译器强制，不必纳入此 grep。
- `cd runtime && go test ./...` 全绿。

### Sources / Research

| 位置 | 已验证事实 |
|---|---|
| `runtime/bootstrap/conf.go:69-110` | `Open()` 以 `sql.Open(c.DBDriver, c.DBURL)` 开库,传 `refs := map[string]string{}` 作 `Options.DSNRefs` |
| `runtime/bootstrap/bootstrap.go:47-51,76-158` | `OpenSQLite` 转发 `cfg.DSNRefs`(nil → 空 map);`mergeDocuments` / `mergeSources` 合并 Sources |
| `runtime/obda/compiler.go:17,44` | `CompiledModel` / `CompiledLink` 无 source 字段;`Compile` 不读 `doc.Sources` 与 `SourceRef` |
| `runtime/obda/validate.go:46-59,83-88` | sources 必填("sources required")、每源须有合法 `dsnRef`、绑定须解析 sourceRef |
| `runtime/obda/parse.go:11-61` | `rejectSecretKeys` 遍历 YAML 节点树拒绝凭证键;`Parse` 用普通 `yaml.Unmarshal`,未开 `KnownFields` |
| `runtime/storage/sqliteobda/provider.go:59-68` | `doc.Sources` 循环:`opts.DSNRefs` 成员检查,缺失即 `unresolved dsnRef` |
| `runtime/storage/mysqlobda/provider.go:64-73` | 等同的 DSNRefs 解析循环 |
| `runtime/storage/sqliteobda/transaction_test.go:11,50,79` | 3 个事务测试现失败:`unresolved dsnRef "primary"` |
| `docs/design/obda-spec-v3.md` | 受影响章节:§1.2(87)、§3.1(187,194)、§4.2(378-411)、§4.3(413)、§4.4(426-464)、§9.2(1026)、§9.3(1032,1062)、§15.8(1585)、§16.3(1672)、§16.4-16.5(1692-1727,introspect/generate 的 `--source` 旗标)、§17.4(1891,缓存 `sources` 字段)、§18(1943)、§21(2007-2033)、§23.1(设计原则图 Source Mapping 框) |

---

## Planning Contract

### Key Technical Decisions

- **KTD-1 旧键显式拒绝(解决 origin 遗留问题"旧形状 YAML 的 parse 行为")。** parse 阶段对旧契约形状做定向检查:顶层 `sources` 映射键、models/links 层的 `sourceRef` 键 → `ErrInvalidMapping`,错误文本指明概念已移除。不开全局 `KnownFields`:fields 之下名为 `sources`/`sourceRef` 的用户属性是合法字段名,全局严格模式会误伤;定向检查把拒绝面精确限定在旧契约所在的层级。
- **KTD-2 Options 保留空结构体(解决 origin 遗留问题"Options 保留还是删除")。** `sqliteobda.Options` / `mysqlobda.Options` 删去 `DSNRefs` 后保留为空 struct,`Open(db, mapping, opts)` 签名不变。删参数需触达全部调用点且未来选项无处安放;保留零残留(字段已删)。
- **KTD-3 测试策略:删概念绑定,保契约保留,新增旧键负例。** 与概念绑定的测试随概念删除(sourceRef 解析、URI 形 dsnRef、dialect 不透明、Sources 合并默认值、缺 DSNRefs 须报 unresolved dsnRef);契约保留测试不动(凭证键拒绝、sentinel distinct、合并去重);唯一新增覆盖是旧键拒绝负例(U4)。删除清单含 `TestValidateMissingSourceRef`、`TestValidateRejectsURIStyleDSNRef`、`TestParseDialectMySQLIsOpaque`、`TestOpenSQLite_CompatibleSourceDefaults`、`TestOpenSQLite_MissingDSNRef`。
- **KTD-4 顺序:代码先行,spec 收尾。** spec 重写描述已被测试验证的落地契约,避免文档先行导致措辞与实现脱节;U5 因此依赖 U1-U4。

### Sequencing

U1 → U2 → U3 → U4 → U5。U1–U3 是编译原子变更:R6 删除核心类型后,bootstrap 与两个 provider 在 U2/U3 落地前不再编译,不得在 U1 检查点跑 `go test ./...`。每单元门是该单元 Verification 行所列包级测试;U2/U3 可在 U1 之后并行;`go test ./...` 仅在 U3 后与最终交付适用。U4 只依赖 U1;U5 收尾。单元内同步改测试,不留红窗。

---

## Implementation Units

### U1. obda 核心:类型与校验移除 sources 概念

- **Goal:** `Document`/`Model`/`Link` 不再承载 sources 概念,Validate 不再执行 sources 规则,obda 包测试同步更新且包级全绿。
- **Requirements:** R6, R7, R11(部分), R5(保持)
- **Dependencies:** 无
- **Files:** `runtime/obda/mapping.go`, `runtime/obda/validate.go`, `runtime/obda/parse_test.go`, `runtime/obda/validate_test.go`
- **Approach:** 删除 `Document.Sources`、`Source`/`Connection` 类型、`Model.SourceRef`/`Link.SourceRef`;validate.go 删 sources 必填块、`validateBinding` 的 sourceRef 解析(签名相应收窄)、`dsnRefName` 正则。删除概念绑定测试:`TestValidateMissingSourceRef`、`TestValidateRejectsURIStyleDSNRef`、`TestParseDialectMySQLIsOpaque`(dialect 字段随 sources 消失)。`parse_test.go`/`validate_test.go` 的 YAML 助手去掉 sources/sourceRef 块。`parse.go` 的 `rejectSecretKeys` 不动(R5);`TestParseRejectsPlaintextDSN`/`TestParseRejectsPassword` 保留,断言不变。
- **Test scenarios:** 无 sources 块的合法映射 parse + Validate 通过;任意层级出现明文 `dsn` 键 → `ErrInvalidMapping`(保留);`password` 键 → `ErrInvalidMapping`(保留);sentinel distinct 测试保持绿色。
- **Verification:** `cd runtime && go test ./obda/` 全绿;包内 grep `Sources`/`SourceRef` 仅余无关命中(应为零)。

### U2. provider 层:删除 DSNRefs 与解析循环

- **Goal:** 两个 provider 不再解析任何连接引用,3 个失败的事务测试转绿,8 个 testdata fixture 清理。
- **Requirements:** R8, R12, R10(8 个 fixture), R11(部分)
- **Dependencies:** U1
- **Files:** `runtime/storage/sqliteobda/provider.go`, `runtime/storage/mysqlobda/provider.go`, `runtime/storage/sqliteobda/apply_schema_test.go`, `runtime/storage/sqliteobda/transaction_test.go`, `runtime/storage/mysqlobda/apply_schema_test.go`, `runtime/storage/{sqliteobda,mysqlobda}/testdata/*.obda.yaml`(各 4 个)
- **Approach:** `Options` 删 `DSNRefs` 字段、保留空 struct(KTD-2);删两处 `doc.Sources` 循环,`Open` 签名不变。测试构造器去掉 `Options{DSNRefs: ...}`;fixture 去掉 sources/sourceRef 块。两 provider 同步同改(产品关键决策),不允许分批。
- **Test scenarios:** 事务回滚写入不可见(`TestTransactionRollbackHidesWrites` 转绿);事务提交写入可见(`TestTransactionCommitVisible` 转绿);Tx 打开期间并发 `GetObject`/`HealthCheck` 不挂死(`TestConcurrentGetDuringTx` 转绿);两包 apply_schema 流程通过;mysqlobda 套件维持当前环境下的绿色(含 skip 行为,不新增 DB 依赖)。
- **Verification:** `cd runtime && go test ./storage/...` 全绿;失败测试清单清零。

### U3. bootstrap 组装:删除 DSNRefs 管道与 Sources 合并

- **Goal:** 组装路径不再传递任何连接引用;Sources 合并逻辑随概念消失,supply-chain pack fixture 清理。
- **Requirements:** R9, R10(supply-chain fixture), R11(部分)
- **Dependencies:** U1
- **Files:** `runtime/bootstrap/conf.go`, `runtime/bootstrap/bootstrap.go`, `runtime/bootstrap/bootstrap_test.go`, `runtime/pack/mappings_test.go`, `domain-packs/supply-chain/obda/supply-chain.obda.yaml`
- **Approach:** conf.go 删 `Bootstrap.DSNRefs` 字段与 `refs := map[string]string{}` 构造;bootstrap.go 删 `Config.DSNRefs`、nil 默认化、`mergeSources` 及 `mergeDocuments` 的 Sources 合并分支(duplicate model/link/table 冲突检查保留)。测试删 `DSNRefs:` 字段行;`TestOpenSQLite_CompatibleSourceDefaults` 与 `TestOpenSQLite_MissingDSNRef` 删除(合并对象与 DSNRefs 解析均消失);`TestOpenSQLite_MergesTwoMappings` 保留并覆盖合并路径。fixture 与 `modelMapping` 助手去 sources/sourceRef;fixture 头部注释中 "the DSN is resolved from the dsnRef at provider construction" 一并删除(否则 grep 残留)。
- **Test scenarios:** `OpenSQLite` round-trip 通过;两映射合并成功且 duplicate model/table/link 各自被拒(保留);pack 加载器对明文 `dsn` 拒绝(保留);supply-chain pack 映射加载成功(`TestLoadSupplyChainMappings`)。
- **Verification:** `cd runtime && go test ./bootstrap/ ./pack/` 全绿。

### U4. parse 阶段显式拒绝旧键

- **Goal:** 旧契约形状(顶层 `sources`、绑定内 `sourceRef`)在 parse 即被拒,同一 `apiVersion` 下唯一合法形状。
- **Requirements:** R4(唯一形状), R11(新增负例)
- **Dependencies:** U1
- **Files:** `runtime/obda/parse.go`, `runtime/obda/parse_test.go`
- **Approach:** 在 `Parse` 的 `rejectSecretKeys` 旁增加定向检查(KTD-1):顶层映射含 `sources` 键、或 models/links 映射层含 `sourceRef` 键 → `ErrInvalidMapping`,错误文本指明概念已移除。
- **Technical design(directional,非实现规格):** 复用既有的 `yaml.Node` 树——先查根映射键集是否含 `sources`,再进入 `models`/`links` 的值映射查键集是否含 `sourceRef`;只查这两层,不递归进 `fields`。
- **Test scenarios:** 顶层 `sources` 块 → `ErrInvalidMapping`;models 内 `sourceRef` → `ErrInvalidMapping`;links 内 `sourceRef` → `ErrInvalidMapping`;`fields` 下名为 `sources` 的字段 → parse 成功(无误伤);`fields` 下名为 `sourceRef` 的字段 → parse 成功(无误伤)。
- **Verification:** `cd runtime && go test ./obda/` 全绿,新增负例全部在列。

### U5. spec v3 文档重写

- **Goal:** `docs/design/obda-spec-v3.md` 全文与新契约一致,sources 概念零残留。
- **Requirements:** R1, R2, R3
- **Dependencies:** U1, U2, U3, U4(spec 描述已落地契约,KTD-4)
- **Files:** `docs/design/obda-spec-v3.md`
- **Approach:** 按 Sources / Research 表逐节改写:§1.2 方言改述为 provider 包选择;§3.1 包表与 Open 签名示例(去 DSNRefs 形参说明);§4.2 顶层 YAML 与字段表去 `sources`,凭证说明改为"DSN 不进 YAML,由组装层注入";§4.3 改写为"全局 DSN"节(环境配置 → `sql.Open(dialect, dsn)` → `provider.Open(db, mapping, opts)` 注入链、单一事务域、`ErrTransactionDomain` 预留、方言随 provider);§4.4 删 sourceRef MUST;§9.2/§9.3 安全边界措辞去 dsnRef、保留"DSN 不进 YAML";§15.8 示例去 sourceRef;§16.3 validate 描述同步;§16.4-16.5 introspect/generate 去 `--source` 旗标(introspection 面向注入连接);§17.4 缓存结构删 `sources` 字段;§18 对比措辞更新(relation + direct identity,无 sourceRef/source.kind);§21 canonical example 去 sources 块;§23.1 设计原则图删 Source Mapping 框。
- **Test scenarios:** Test expectation: none -- 纯文档重写,正确性由 Verification Contract 的一致性门度量。
- **Verification:** spec 内 YAML 示例与代码契约一致;全文无悬空 sources/sourceRef/dsnRef 引用(历史对比措辞除外,如 §18 "不是 sourceRef + source.kind" 的否定式表述)。

---

## Verification Contract

| 门 | 命令 / 标准 | 适用 |
|---|---|---|
| 单元包级绿 | 该单元 Verification 行所列包级测试(U1 `./obda/`;U2 `./storage/...`;U3 `./bootstrap/ ./pack/`;U4 `./obda/`) | 每单元落地后 |
| 全量全绿 | `cd runtime && go test ./...` | U3 后与最终交付 |
| 零残留 | `grep -rin "sourceref\|dsnref" runtime/ domain-packs/` 除 U4 拒绝点外零命中。允许：`runtime/obda/parse.go` 与 `runtime/obda/parse_test.go`（这些 token 唯一被允许的存在） | U4 完成后 |
| 事务回归 | `cd runtime && go test ./storage/sqliteobda/ -run 'TestTransaction\|TestConcurrentGetDuringTx'` 全绿 | U2 |
| spec 一致性 | `docs/design/obda-spec-v3.md` 示例与代码契约一致,无悬空引用 | U5 |

注:mysqlobda 套件当前在本环境通过(可能为 skip 路径);改动后必须维持绿色且不新增数据库依赖。

---

## Definition of Done

- R1-R12 全部成立,Verification Contract 各门全部通过。
- 3 个事务测试转绿(R12);零残留 grep 达成(Success Criteria,U4 拒绝点除外)。
- 清理:本次改动产生的注释残迹(如 fixture 头部 dsnRef 注释)、死代码与试验性残留全部移除,不留在 diff 中。
- 每单元交付时其 Verification 所列包级测试绿色;U1–U3 作为编译原子变更;最终 `go test ./...` 一次通过。

---

## Deferred / Open Questions

无未决问题。2026-09-04 评审四条已写入上文并在执行中落地:

- 零残留 grep 排除 U4 拒绝点,改用大小写不敏感 `sourceref\|dsnref`。
- U1–U3 为编译原子变更;单元门为包级测试,`go test ./...` 仅 U3 后与最终交付。
- `TestOpenSQLite_MissingDSNRef` 列入 U3 / KTD-3 删除清单。
