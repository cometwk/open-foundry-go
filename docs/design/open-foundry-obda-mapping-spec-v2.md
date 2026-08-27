# Open Foundry — OBDA Mapping Specification v2.0

**Status:** In implementation (Go / SQLite)  
**Date:** 2026-08-21  
**YAML `apiVersion`:** `openfoundry.io/obda/v1`（文档版本与 YAML 版本独立：本文件是 v2 spec，可执行文档仍用 v1 apiVersion）  
**Format:** `*.obda.yaml`  
**Runtime:** `runtime/obda` + `runtime/storage/sqliteobda`  
**SPI:** `runtime/spi/provider.go`

## 0. 与其他文档的关系

| 文档 | 角色 |
|---|---|
| `docs/brainstorms/2026-08-21-obda-mysql-storage-provider-requirements.md` | 行为需求（WHAT）。标题仍写 MySQL；v1 方言按计划改为 SQLite。 |
| `docs/design/obda-mapping-design-v1.md` | 机制草案。两层 Core/Dialect、sidecar、identity 仍有效；文中 MySQL 剖面读成 SQLite。 |
| `docs/design/open-foundry-obda-mapping-spec-v1.md` | **历史 overlay 草案。不要实现。** 只读 overlay、PostgreSQL+AGE、Sync Engine、`sqlQuery`、`sync.mode`、ReBAC 注入 SQL 均不是本 spec。 |
| `docs/plans/2026-08-21-001-feat-obda-sqlite-storage-provider-plan.md` | HOW 与实现进度。 |
| 本文件 | 按 **当前 Go 代码** 锁定的 mapping 语言 + 运行时契约。冲突时：需求约束行为，本文件约束 YAML 与包边界，计划约束交付顺序。 |

不修订 `docs/open-foundry-spec-v2.md`。

---

## 1. 产品定义

OBDA Mapping 是 ODL `ObjectType` / `LinkType` 到物理关系的**可执行对应**。它不重述业务语义（类型、cardinality、`@primary`、constraint 仍以 ODL 为准）。

Go v1 产品是：

```text
ODL storage schema
        +
*.obda.yaml（构造注入，不是 ApplySchema 参数）
        |
        v
SQL-neutral OBDA Core（parse / validate / compile / planner / identity）
        |
        v
Dialect 接口
        |
        v
SQLite 适配器
        |
        v
完整 spi.StorageProvider（sqliteobda）
        |
        v
Ontology Engine（只依赖 SPI）
```

它 **是** 可注入 Engine 的读写存储。它 **不是** Sync Engine overlay、不是 JDBC connector、不是第四个图数据库、也不做 ReBAC。

v1 只交付一个方言：**SQLite**（`modernc.org/sqlite`）。Core 把 `sources.*.dialect` 当不透明字符串；`sqliteobda.Open` 在无 sqlite 适配器时返回 `ErrInvalidMapping`。`dialect: mysql` 可被 Core 解析，不能被本 provider 激活。

---

## 2. 非目标

- ReBAC / OpenFGA / 把权限谓词注入 SQL
- 跨库 JOIN、分布式事务、CDC / polling / batch / federation
- mapping 内任意 SQL、`sqlQuery` source、computed SQL、不可逆 `hash`
- 本轮交付 MySQL 或其他方言
- 改 `memory` provider、改 Engine Get-then-Update 语义、改 `runtime/projection`（Primary 仍被丢掉）
- Pack loader 加载 `*.obda.yaml`（mapping 由调用方注入）
- 把 Go/SQLite 测例接入现有 GitHub CI

---

## 3. 架构与包边界

依赖单向：

```text
sqliteobda  →  runtime/obda  →  dialect.Dialect  ←  dialect/sqlite
```

| 包 | 职责 | MUST NOT |
|---|---|---|
| `runtime/obda` | YAML 模型、Validate、Compile、planner、direct identity 编解码 | import `modernc.org/sqlite`；发射带方言 quoting / `fts5` / `MATCH` 的 SQL 文本 |
| `runtime/obda/sqlast` | 封闭 AST（值只进 Param，标识符只进 Identifier） | 拼接运行时字面量 |
| `runtime/obda/dialect` | `Dialect` 接口与 `SQLStatement{SQL, Args}` | 绑定某一驱动 |
| `runtime/obda/dialect/sqlite` | 双引号 quoting、Render、introspect、sidecar DDL、FTS helper、`Classify`、`NormalizeValue` | 重定义 SPI 语义 |
| `runtime/storage/sqliteobda` | `StorageProvider`：激活、sidecar、CRUD、query、links、事务 | 把 DSN 写进 YAML / 公开 error / HealthCheck.Details |

`Compiled` mapping 不可变。一次 SPI 调用通过 `pin()` 钉住当前激活版本；`ApplySchema` 成功后进行中的 pinned 副本不变。

构造：

```go
sqliteobda.Open(db *sql.DB, mapping []byte, opts sqliteobda.Options{DSNRefs: map[string]string{...}})
```

未 `ApplySchema` 成功激活前，除 `HealthCheck` / `Capabilities` 外返回 `ErrMappingNotActive`。

---

## 4. Mapping 文档

文件名推荐 `<name>.obda.yaml`。Go 解析器只认本节字段。v1 overlay 的 `runtime`、`sync`、`connector`、`urlRef`、`source.expression`、`sqlQuery` **不是**可执行文档的一部分。

### 4.1 顶层

```yaml
apiVersion: openfoundry.io/obda/v1   # MUST
kind: OBDAConfig                     # MUST
metadata:
  name: hospital                     # MUST
  namespace: nhs.acute
  version: 1
schema:
  namespace: nhs.acute
  version: 1                         # integer，不是 semver 字符串
sources: { ... }                     # MUST，至少一个
models: { ... }                      # models 与 links 至少一个非空
links: { ... }
```

明文凭证键（大小写不敏感，出现在 YAML 任意层）解析失败，错误为 `ErrInvalidMapping`：

`dsn` · `password` · `uri` · `url` · `token` · `secret` · `user`

DSN 只通过 `connection.dsnRef` 命名；真实值在 `Options.DSNRefs` 解析。未解析的 `dsnRef` → `ErrInvalidMapping`。

### 4.2 Source

```yaml
sources:
  primary:
    kind: sql              # 空或 sql；其他 kind 非法
    dialect: sqlite        # Core 不解释；Open 要求 sqlite 或空
    connection:
      dsnRef: secret://hospital/sqlite-dsn
```

v1 所有可写绑定必须落在**同一个 SQLite 连接/文件**（一个事务域）。跨域写入返回 `ErrTransactionDomain`（表已定义；当前单文件 Open 路径不会主动跨域）。

### 4.3 Model / Link 绑定

每个 model 与 link MUST 声明 `sourceRef`，且指向 `sources` 中已有名字。

```yaml
models:
  Patient:
    sourceRef: primary
    relation:
      kind: table          # table | view；缺省 table
      catalog:             # 空或 main；其他 → ErrInvalidMapping
      name: patient        # MUST；渲染前再过标识符白名单
    access: readWrite      # read | readWrite
    identity:
      strategy: sidecar    # sidecar | direct
      columns: [patient_id]
      insert:              # generated 时可空 columns
    tenant:
      strategy: column     # column | constant；connection 拒绝
      column: tenant_id
    system:
      strategy: sidecar    # sidecar | native
    fields:
      patientId:
        column: patient_id
      name:
        column: patient_name
        transform:
          kind: prefix     # 见 4.5
          arg: "x-"
```

Link 额外 MUST：

```yaml
links:
  AdmittedTo:
    # …与 model 相同的 sourceRef / relation / access / identity / tenant / system / fields
    from:
      object: Patient
      columns: [patient_id]
    to:
      object: Ward
      columns: [ward_id]
```

规则：

- `access: readWrite` + `relation.kind: view` → 编译/校验失败（`ErrInvalidMapping`），不是运行时只读错误。
- `access: read` 的 Query 成功；mutation → `ErrReadOnlyMapping`，零写入。
- identity 列 MUST 是 payload `fields` 中的某一 `column`，除非 `insert: generated`。
- 禁止把逻辑字段映射到 tenant 列（`Compile` 拒绝）。
- 逻辑字段名对应 ODL property；ODL Primary 被 projection 丢掉，因此可写 direct identity 必须用 payload 中的非 Primary 字段或 generated key。

### 4.4 Relation 种类

| kind | 读 | 写 |
|---|---|---|
| `table` | 是 | `access: readWrite` 时必须能确定 INSERT/UPDATE/DELETE 目标 |
| `view` | 是 | 否 |
| `sqlQuery` | 未实现 | — |

### 4.5 Transform

封闭集合。未知 kind → `ErrInvalidMapping`。

| kind | 可写 |
|---|---|
| （空）列直映 | 是 |
| `prefix` `suffix` `trim` `toUpper` `toLower` `map` `parseDate` `parseDateTime` | 是 |
| `coalesce` | 仅 `access: read` |
| `hash`、任意表达式、computed SQL | 否 |

Parser 接受 transform 字段；当前 sqliteobda 写路径按列直映执行。不可逆 transform 不得出现在可写绑定上。

### 4.6 最小可执行示例

与 `runtime/storage/sqliteobda/testdata/hospital.obda.yaml` 对齐：

```yaml
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig
metadata:
  name: hospital
  namespace: nhs.acute
  version: 1
schema:
  namespace: nhs.acute
  version: 1
sources:
  primary:
    kind: sql
    dialect: sqlite
    connection:
      dsnRef: secret://hospital/sqlite-dsn
models:
  Patient:
    sourceRef: primary
    relation:
      kind: table
      name: patient
    access: readWrite
    identity:
      strategy: sidecar
      columns: [patient_id]
    tenant:
      strategy: column
      column: tenant_id
    system:
      strategy: sidecar
    fields:
      patientId:
        column: patient_id
      name:
        column: patient_name
  Ward:
    sourceRef: primary
    relation:
      kind: table
      name: ward
    access: readWrite
    identity:
      strategy: sidecar
      columns: [ward_id]
    tenant:
      strategy: column
      column: tenant_id
    system:
      strategy: sidecar
    fields:
      wardId:
        column: ward_id
      name:
        column: ward_name
links:
  AdmittedTo:
    sourceRef: primary
    relation:
      kind: table
      name: admission
    access: readWrite
    identity:
      strategy: sidecar
      columns: [admission_id]
    from:
      object: Patient
      columns: [patient_id]
    to:
      object: Ward
      columns: [ward_id]
    tenant:
      strategy: column
      column: tenant_id
    system:
      strategy: sidecar
    fields:
      admissionId:
        column: admission_id
```

---

## 5. Identity

`GetLinks` / `Traverse` 只拿对象 id、不拿类型。id 在一个 provider 内必须可解码出类型，禁止扫多表猜测。

### 5.1 `sidecar`

- `_id` = UUIDv7（`runtime/internal/uuidv7`）。
- `of_object_meta` / `of_link_meta` 存 `engine_id` ↔ `(tenant_id, type, physical_key)`。
- CreateLink 可采纳 payload 中的 `_engineLinkId`；direct link **忽略** `_engineLinkId`。

### 5.2 `direct`

编码（`obda.EncodeDirect`）：

```text
base64url( JSON{"t": "<object-or-link-type>", "k": ["<col0>", ...]} )
```

无 padding（`RawURLEncoding`）。禁止 delimiter 拼接。解码失败、类型不匹配 → `ErrObjectNotFound` / `ErrLinkNotFound`。

### 5.3 physical_key

sidecar 查找键（`EncodePhysicalKey`）：单列 `fmt.Sprint`；复合列为 JSON 字符串数组。用于 meta、history、幂等与 cache 键。

---

## 6. Sidecar

`ApplySchema` 在同一 SQLite 文件创建 STRICT `of_*` 表。MUST NOT `ALTER` 业务表，也 MUST NOT 在业务表上建 BTREE。

| 表 | 职责 |
|---|---|
| `of_schema_versions` | schema JSON 快照 |
| `of_mapping_versions` | mapping 文档 + `dsn_ref` **名字**（禁止存 DSN） |
| `of_mapping_activation` | 单行 CAS（`id CHECK (id = 1)`）+ fingerprint |
| `of_object_meta` | `_id`、tenant、type、physical_key、version、时间戳、`deleted_at` |
| `of_link_meta` | 独立 link id、端点、version、时间戳、`deleted_at` |
| `of_object_history` / `of_link_history` | JSON 快照；硬删 **保留** history |
| `of_idempotency` | Bulk 预留（尚未接线） |
| `of_index_registry` | EnsureIndex 预留（尚未接线） |

`of_object_meta` 全量 UNIQUE `(tenant_id, object_type, physical_key)`：软删后再 Create 同一物理键被拒绝。

Cardinality：`ApplySchema` 按 schema 为 link 建部分唯一索引，例如 MANY_TO_ONE：

```sql
CREATE UNIQUE INDEX ... ON "of_link_meta" ("tenant_id", "from_id")
  WHERE "deleted_at" IS NULL AND "link_type" = '<LinkType>'
```

MANY_TO_MANY 不建该约束。冲突分类为 `ErrCardinalityViolation`。查询侧 `LIMIT 1` 不能代替写约束。

Backfill：sidecar `tenant_id` 复制自业务 tenant 列或 constant，不得用 `ApplySchema` 的 `RequestContext.TenantID` 盖写已有行。

Fingerprint：对 mapping 中 **已排序** 的 model 与 link 表做 `PRAGMA table_info` 摘要。激活后缓存在 `of_mapping_activation`；热路径 Get **不**每次 PRAGMA。`HealthCheck` 发现漂移后设 fail-closed，后续 `pin` 返回 `ErrSourceSchemaDrift`。

---

## 7. 租户

空 `TenantID` → `ErrTenantRequired`（含 `ApplySchema`）。

| strategy | 行为 |
|---|---|
| `column` | 每个读写注入租户谓词；值只来自 `RequestContext` |
| `constant` | 绑定只服务配置的那一个租户；其他租户当 not-found |
| `connection` | v1 拒绝 |

调用方 filter / properties 中的 `_tenantId` 不能扩大可见范围或改写存储租户。跨租户与缺失同一 `ErrObjectNotFound` / `ErrLinkNotFound`。Link 两端必须同租户。

---

## 8. 系统字段与删除

返回对象 MUST 含：`_tenantId` `_type` `_id` `_version` `_createdAt` `_updatedAt`；软删时含 `_deletedAt`。

返回 link 额外 MUST 含：`_fromType` `_fromId` `_toType` `_toId`。

缺失的系统列由 sidecar 提供，同一记录每次读取值稳定。

| 操作 | 行为 |
|---|---|
| `GetObject` / `GetLink` | 可返回软删行 |
| `QueryObjects` / `GetLinks` | 默认排除软删；`IncludeDeleted` 可包含 |
| 软删后再 Update | `ErrObjectNotFound` / `ErrLinkNotFound`，不是 `ErrVersionConflict` |
| 软删端点 `CreateLink` | `ErrObjectNotFound`，不插行 |
| 软删后再 soft delete | 幂等成功 |
| Hard delete 对象 | 同事务删除全部 inbound+outbound `of_link_meta`（含已软删 link）、删业务行与 object meta，**保留** `of_*_history` |
| 业务 XOR sidecar（split-brain） | 两侧都 not-found |

Engine `GetObject` 透传 SPI，因此注入 sqliteobda 后 Engine 能看见软删。这与 `memory.GetObject` 的 mask 分叉；本轮不改 Engine。

时间戳格式 RFC3339Nano。Boolean 读路径经 `NormalizeValue`：SQLite INTEGER 0/1 → Go `bool`。

---

## 9. 对象与查询

OCC：`expectedVersion` 在 sidecar 上原子 CAS。`expectedVersion == nil` 仍要求未删除存在行。并发同一 version：一成功一 `ErrVersionConflict`。

Query：

- `Limit <= 0` → 100；上限 1000
- identity 列做稳定 tie-breaker
- `Cursor` 保持空（SPI 尚未消费 cursor）
- `OrderBy` 必须是 mapping 逻辑名；非法标识符不得变成 SQL
- `AsOfTime` / `AsOfVersion` → `ErrUnsupportedCapability`（不得返回 live 行冒充历史）
- 当前实现要求单列 identity + tenant 列，否则 Query 返回 `ErrUnsupportedCapability`

---

## 10. Link 与遍历

CreateLink：两端类型匹配、同租户、可写、均未软删。Sidecar 铸独立 link id。

GetLinks：先解全局对象 id。默认排除软删 link。

Traverse（当前实现）：

- 逐步调用 `GetLinks`，visited canonical id 防环
- `path.Steps` 长度 > 8 → `ErrUnsupportedCapability`
- 未知或跨租户 start → `ErrObjectNotFound`
- **不是** recursive CTE（计划曾写 CTE；实现取逐步遍历）

---

## 11. 事务

`BeginTransaction` 钉住 tenant + compiled mapping + `*sql.Tx`。写连接先 `PRAGMA busy_timeout=5000` 再 `BeginTx`。测试 DSN 带 `_busy_timeout=5000`。打开 Tx 时并发 `GetObject` / `HealthCheck` 不得挂死。

Rollback 后业务行、object/link meta、history 均不可见。

当前缺口：`sqlTxn.UpdateLink` / `DeleteLink` 仍走公开方法（各自开事务）。对象动词与 `CreateLink` 已走 pinned Tx。

Engine 本轮无事务 API。

---

## 12. SPI 表面（相对代码，2026-08-21）

Provider 嵌入 `UnimplementedStorageProvider` 仅满足 Go 前向兼容。返回 `ErrUnimplemented` **不是**完整 v1 成功路径；U8 完成前不得宣称可注入生产。

| 方法 | 状态 |
|---|---|
| `ApplySchema` / `GetSchema` | 已实现。ApplySchema 不改业务表 DDL。 |
| `HealthCheck` / `Capabilities` | 已实现。Details 不含路径/DSN/SQL。 |
| `CreateObject` `GetObject` `UpdateObject` `DeleteObject` `QueryObjects` | 已实现 |
| `CreateLink` `GetLink` `UpdateLink` `DeleteLink` `GetLinks` `Traverse` | 已实现 |
| `BeginTransaction` | 已实现（见 §11 缺口） |
| `AggregateObjects` `SearchObjects` `BulkMutate` | **`ErrUnimplemented`** |
| `GetObjectAtVersion` `GetObjectAtTime` | **`ErrUnimplemented`** |
| `EnsureIndex` `DropIndex` `ListIndexes` | **`ErrUnimplemented`** |

`Capabilities()` 当前声明（方言探测 FTS5）：

```text
SupportsTransactions    = true
SupportsTemporalQueries = true   # 声明超前于实现；AsOf / temporal 方法尚未接线
SupportsFullTextSearch  = ProbeFTS5
SupportsGeoQueries      = false
SupportsGraphTraversal  = true
SupportsBulkMutations   = true   # 声明超前于实现
MaxTraversalDepth       = 8
ReplicationSupport      = none
```

计划中的收口（尚未落地）：

- Search：无 searchable 映射 + 非空 query → `ErrUnsupportedCapability`；空白 query → 空 hits。FTS5 `MATCH ?`，query 在 planner 转义。
- Bulk：租户作用域幂等键；同键不同 payload hash → `ErrIdempotencyConflict`；失败整笔回滚，无 resume。
- Temporal：只读 `of_*_history`；缺历史不得返回当前行。
- EnsureIndex：FTS5 可建；业务表 BTREE/HASH/GIN/GIST → `ErrUnsupportedIndexType`。

---

## 13. 错误契约

沿用既有 sentinel：`ErrObjectNotFound` `ErrLinkNotFound` `ErrInvalidObjectType` `ErrInvalidLinkType` `ErrVersionConflict` `ErrCardinalityViolation`。

OBDA 加法（`errors.Is`）：

| Sentinel | 何时 |
|---|---|
| `ErrReadOnlyMapping` | 对 `access: read` 绑定 mutation |
| `ErrUnsupportedCapability` | 方言或绑定做不到（AsOf、超深度 Traverse、无 FTS 的 Search） |
| `ErrInvalidMapping` | 解析/校验/编译失败 |
| `ErrMappingNotActive` | 未激活（HealthCheck/Capabilities 除外） |
| `ErrTenantRequired` | 空 tenant |
| `ErrUnsupportedIndexType` | 拒绝的物理索引类型 |
| `ErrSourceSchemaDrift` | fingerprint 变化后 fail-closed |
| `ErrIdempotencyConflict` | Bulk 同键不同 hash（尚未接线） |
| `ErrTransactionDomain` | 跨单文件事务域（尚未接线） |

`error.Error()`、日志、HealthCheck.Details MUST NOT 含文件路径、DSN 或 SQL 文本。驱动错误经 `Classify` 映射到上述 sentinel。

---

## 14. SQLite 方言剖面

- Placeholder：`?`
- 标识符：白名单 `^[A-Za-z_][A-Za-z0-9_]*$`，双引号包裹；嵌入 `"` 或非法字符在渲染前拒绝
- 分页：`LIMIT ? OFFSET ?`
- Sidecar：`CREATE TABLE ... STRICT`
- 并发：WAL + 写连接 `busy_timeout=5000`；不承诺高并发 SLA
- FTS5：启动探测；未编译则 capability false
- `NormalizeValue`：Boolean INTEGER→bool；datetime TEXT→RFC3339Nano 字符串
- Core 测试不得链接 `modernc.org/sqlite`

---

## 15. 安全边界

v1 仍强制：租户隔离、参数化值、标识符白名单、dsnRef 而非明文、公开错误脱敏、Query limit / Traverse 深度上界。

能调用 `StorageProvider` 即视为存储层可信调用方。授权与 consent 在 SPI 之上。无 ReBAC。

---

## 16. 与 overlay spec v1、design v1 的差异

相对 `open-foundry-obda-mapping-spec-v1.md`：

- 完整 `StorageProvider`，不是 OVERLAY read-through
- 没有 PostgreSQL+AGE 本体存储、没有 Sync Engine、没有 `sync.mode`
- YAML 形状是 `sourceRef` + `relation` + `identity.strategy`，不是 `source.kind` + `identity.fields[].target`
- 无 ReBAC 谓词注入

相对 `obda-mapping-design-v1.md`：

- 第一个方言是 SQLite，不是 MySQL 8
- mapping 由 `Open` 注入，不走 `ApplySchema` 第二参数
- Traverse 当前为逐步 GetLinks，不是 bounded recursive CTE
- 对象物理键唯一索引是全量 unique，不是仅 active 部分唯一
- package 是 `sqliteobda` / `dialect/sqlite`，不是 `mysqlobda`

---

## 17. 包布局（与代码一致）

```text
runtime/obda/
  doc.go mapping.go parse.go validate.go compiler.go planner.go identity.go
  sqlast/
  dialect/dialect.go
  dialect/sqlite/          # dialect.go introspect.go ddl.go fts.go errors.go quote.go
runtime/storage/sqliteobda/
  provider.go sidecar.go objects.go query.go links.go transaction.go
  testdata/*.obda.yaml
runtime/spi/errors.go      # OBDA 加法 sentinel
```

`options.go` / `health.go` / `registry.go` 未单列；compiled mapping 在 `Compile` + `activation`。

---

## 18. 实现进度（与计划同步）

| 单元 | 状态 |
|---|---|
| U1–U6 mapping / AST / SQLite dialect / sidecar / object / link | 完成 |
| U7 事务 | 部分（对象 + CreateLink） |
| U7 aggregate / search / bulk / temporal / index / AE1 | 未做 |
| U8 Engine smoke + origin-vs-memory | 未做 |

测试：`cd runtime && go test ./obda/... ./storage/sqliteobda/`，真实 `t.TempDir()` 文件库，无 MySQL 容器，无共享 `:memory:`。
