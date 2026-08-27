# Open Foundry — OBDA Mapping Specification v3.0

**Status:** Requirements-aligned (direct-native, 2026-08-21)  
**YAML `apiVersion`:** `openfoundry.io/obda/v1`（文档版本与 YAML 版本独立）  
**Format:** `*.obda.yaml`  
**Runtime target:** `runtime/obda` + `runtime/storage/sqliteobda`  
**SPI:** `runtime/spi/provider.go`

**Origin:** `docs/brainstorms/2026-08-21-obda-direct-native-identity-requirements.md`  
**Identity rationale:** `docs/design/faq1-Identity.md`  
**Supersedes for implementation:** `docs/design/open-foundry-obda-mapping-spec-v2.md`（sidecar 时代）。v1 overlay 草案仍不要实现。

不修订 `docs/open-foundry-spec-v2.md`。

---

## 0. 与其他文档的关系

| 文档 | 角色 |
|---|---|
| `docs/brainstorms/2026-08-21-obda-direct-native-identity-requirements.md` | 行为需求（WHAT）。direct-native identity 要求。 |
| `docs/design/faq1-Identity.md` | identity 设计决策依据。 |
| `docs/design/obda-mapping-design-v1.md` | 机制草案。两层 Core/Dialect、identity 编码仍有效；sidecar、MySQL 剖面读成 SQLite+direct。 |
| `docs/design/open-foundry-obda-mapping-spec-v1.md` | **历史 overlay 草案。不要实现。** 只读 overlay、PostgreSQL+AGE、Sync Engine、`sqlQuery`、`sync.mode`、ReBAC 注入 SQL 均不是本 spec。但其中设计原则、TBox/ABox 分层、query pipeline 概念仍有参考价值。 |
| `docs/design/open-foundry-obda-mapping-spec-v2.md` | **sidecar 时代 spec，已被 v3 取代。** 其 SPI 表面、错误契约、事务、SQLite 方言、包布局等细节在本 spec 中继承并适配 direct-native。 |
| `docs/plans/2026-08-21-001-feat-obda-sqlite-storage-provider-plan.md` | HOW 与实现进度。 |
| 本文件 | 按 **当前 Go 代码** 锁定的 mapping 语言 + 运行时契约。冲突时：需求约束行为，本文件约束 YAML 与包边界，计划约束交付顺序。 |

---

## 1. Product

### 1.1 定义

OBDA Mapping 是 ODL `ObjectType` / `LinkType` 到物理关系的**可执行对应**。它不重述业务语义（类型、cardinality、`@primary`、constraint 仍以 ODL 为准）。

ODL 是 ontology schema 的 single source of truth。Open Foundry v2.0 已明确规定 Schema-Driven 原则：API、权限、SDK、UI 等均由 ontology schema 生成。而数据库只负责物理存储。

因此需要一个独立层：

```text
                 ODL / TBox
                     │
                     │ semantic definition
                     ▼
             OBDA Mapping
                     │
                     │ physical mapping
                     ▼
              Data Sources
```

本规范定义：

> **OBDA Mapping = ODL Semantic Model → Physical Data Model 的可执行映射。**

OBDA 不重新定义 ODL 语义，也不替代 ODL。

### 1.2 v3 产品

```text
ODL storage schema + *.obda.yaml
        |
        +-- optional DDL helper → CREATE mapped tables
        |
        v
ApplySchema MUST introspect: tables already exist
        |
        v
in-process compiled mapping
        |
        v
SQL-neutral Core → Dialect → SQLite
        |
        v
spi.StorageProvider on business tables only
```

- Engine `_id` **就是** 业务表 identity 列。
- 无 sidecar，无任何 `of_*` 表。
- `GetLinks` / `Traverse` 对业务表做参数化 JOIN。
- v1 方言仍是 SQLite。Core 不 import 驱动。

它是**可注入 Engine 的读写存储**。它**不是** Sync Engine overlay、不是 JDBC connector、不是第四个图数据库、也不做 ReBAC。

v1 只交付一个方言：**SQLite**（`modernc.org/sqlite`）。Core 把 `sources.*.dialect` 当不透明字符串；`sqliteobda.Open` 在无 sqlite 适配器时返回 `ErrInvalidMapping`。

### 1.3 设计原则

#### ODL 是 Semantic Source of Truth

OBDA MUST NOT 重新定义：

- ObjectType 的业务语义
- LinkType 的业务语义
- ODL Property 类型
- Cardinality
- `@primary`
- `@unique`
- `@constraint`
- `@immutable`
- `@computed`
- Namespace

例如 ODL：

```graphql
type Patient @objectType {
  id: ID! @primary
  nhsNumber: String @unique @indexed
  name: String!
  dateOfBirth: Date!
  status: PatientStatus!
}
```

OBDA 只描述：

```text
Patient
  ↓
patient（业务表）
```

而不再次声明：

```yaml
Patient:
  id: ID
  name: String
```

#### 边界

```text
ODL
  = What the world means

OBDA
  = Where/how the data is represented

Query IR
  = What the caller wants

Planner
  = How to execute it

SQL
  = Physical execution
```

---

## 2. Non-goals

- `identity.strategy: sidecar` / `system.strategy: sidecar`
- Provider 创建或读写 `of_*`
- `ApplySchema` `ALTER` 已有业务表
- 用「已生成 DDL」跳过存在性检查
- ReBAC / OpenFGA / 把权限谓词注入 SQL
- 跨库 JOIN、分布式事务、CDC / polling / batch / federation
- mapping 内任意 SQL、`sqlQuery` source、computed SQL、不可逆 `hash`
- 本轮交付 MySQL 或其他方言
- 本轮持久化 history / Bulk 幂等 / 改 Engine / 改 `runtime/projection`（Primary 仍被丢掉）
- Pack loader 加载 `*.obda.yaml`（mapping 由调用方注入）
- 把 Go/SQLite 测例接入现有 GitHub CI
- 改 `memory` provider、改 Engine Get-then-Update 语义

---

## 3. Architecture

### 3.1 包依赖

依赖单向：

```text
sqliteobda  →  runtime/obda  →  dialect.Dialect  ←  dialect/sqlite
```

| 包 | 职责 | MUST NOT |
|---|---|---|
| `runtime/obda` | YAML 模型、parse / validate / compile / planner / `EncodeDirect` 编解码 | import `modernc.org/sqlite`；sidecar 策略；发射带方言 quoting / `fts5` / `MATCH` 的 SQL 文本 |
| `runtime/obda/sqlast` | 封闭 AST（值只进 Param，标识符只进 Identifier），含 `Join` | 拼接运行时字面量 |
| `runtime/obda/dialect` | `Dialect` 接口与 `SQLStatement{SQL, Args}` | 绑定某一驱动 |
| `runtime/obda/dialect/sqlite` | 双引号 quoting、Render、introspect、**mapped-table** DDL 辅助、FTS helper、`Classify`、`NormalizeValue` | 生成 `of_*`；重定义 SPI 语义 |
| `runtime/storage/sqliteobda` | `StorageProvider`：Open、可选 init、ApplySchema 检查、CRUD、query、links、事务 | 查询 `of_*`；把 DSN 写进 YAML / 公开 error / HealthCheck.Details |

`Compiled` mapping 不可变。一次 SPI 调用通过 `pin()` 钉住当前激活版本；`ApplySchema` 成功后进行中的 pinned 副本不变。

构造：

```go
sqliteobda.Open(db *sql.DB, mapping []byte, opts sqliteobda.Options{DSNRefs: map[string]string{...}})
```

未 `ApplySchema` 成功激活前，除 `HealthCheck` / `Capabilities` 外返回 `ErrMappingNotActive`。

### 3.2 核心架构

```text
                  ┌────────────────────┐
                  │      ODL / TBox    │
                  │                    │
                  │ Patient            │
                  │ Ward               │
                  │ AdmittedTo         │
                  └─────────┬──────────┘
                            │
                            ▼
                  ┌────────────────────┐
                  │    OBDA Metadata   │
                  │                    │
                  │ Source             │
                  │ Model / Relation   │
                  │ Identity (direct)  │
                  │ Field Mapping      │
                  │ Link Mapping       │
                  │ Transform          │
                  └─────────┬──────────┘
                            │
                            ▼
                  ┌────────────────────┐
                  │ Semantic Schema    │
                  │ Cache / Compiler   │
                  └─────────┬──────────┘
                            │
                            ▼
                  ┌────────────────────┐
                  │   Security Layer    │
                  │ AuthN / AuthZ      │
                  │（SPI 之上）          │
                  └─────────┬──────────┘
                            │
                            ▼
                  ┌────────────────────┐
                  │   Query Planner    │
                  │                    │
                  │ queryObjects()     │
                  │ getObject()        │
                  │ getLinks()        │
                  │ traverse()        │
                  └─────────┬──────────┘
                            │
                            ▼
                  ┌────────────────────┐
                  │ Relational Plan    │
                  │ (sqlast.Join)      │
                  └─────────┬──────────┘
                            │
                            ▼
                           SQL
                            │
                            ▼
                     SQLite (business tables)
```

### 3.3 四层语义架构

```text
┌───────────────────────────────┐
│            ODL                │
│       Business Semantics      │
│             TBox              │
└───────────────┬───────────────┘
                │
                ▼
┌───────────────────────────────┐
│           OBDA                │
│       Semantic → Physical     │
│                               │
│ Model / Property / Identity   │
│ Link / Source / Transform     │
└───────────────┬───────────────┘
                │
                ▼
┌───────────────────────────────┐
│       Security Layer          │
│     AuthN / AuthZ             │
│    （SPI 之上，不在 OBDA 内）    │
└───────────────┬───────────────┘
                │
                ▼
┌───────────────────────────────┐
│        Semantic Query         │
│                               │
│ GraphQL / traverse / Query IR │
└───────────────┬───────────────┘
                │
                ▼
┌───────────────────────────────┐
│        Physical Runtime       │
│                               │
│ SQL / SQLite business tables  │
└───────────────────────────────┘
```

### 3.4 TBox / OBDA / ABox

```text
TBox
│
│ ODL
│
├── Patient
├── Ward
├── AdmittedTo
└── Constraints
│
▼
OBDA
│
│ "Where/how?"
│
├── patient.id
├── ward.id
├── admission.from_id
└── admission.to_id
│
▼
ABox
│
│ actual rows
│
├── patient("patient-xxx",...)
├── ward("ward-yyy",...)
└── admission("admission-zzz",...)
```

> **ODL 是语义世界，OBDA 是世界与物理数据之间的桥。**

---

## 4. Mapping document

Parser 仍只认本节字段。`runtime` / `sync` / `sqlQuery` / 明文 `dsn` `password` `uri` `url` `token` `secret` `user` 非法。

### 4.1 文件命名

推荐：

```text
<name>.obda.yaml
```

例如：

```text
hospital.obda.yaml
pas.obda.yaml
crm.obda.yaml
erp.obda.yaml
```

一个 Domain Pack MAY 包含多个 OBDA 文件：

```text
nhs-acute/
├── schema/
│   ├── patient.odl
│   ├── ward.odl
│   └── links.odl
│
├── obda/
│   ├── pas.obda.yaml
│   ├── ehr.obda.yaml
│   └── bed-system.obda.yaml
│
├── actions/
├── permissions/
├── functions/
└── quality/
```

> 注：v3 当前 mapping 由 `Open` 注入，不走 Pack loader。此目录结构为推荐演进方向。

### 4.2 顶层

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

| 字段 | REQUIRED | 说明 |
|---|---:|---|
| `apiVersion` | MUST | OBDA 规范版本 |
| `kind` | MUST | 固定 `OBDAConfig` |
| `metadata.name` | MUST | Mapping 名 |
| `metadata.namespace` | MAY | Open Foundry namespace |
| `metadata.version` | MUST | Mapping 版本（独立于 ODL schema 版本） |
| `schema` | MUST | 对应 ODL schema |
| `sources` | MUST | 数据源定义 |
| `models` | MUST | ObjectType 映射 |
| `links` | MAY | LinkType 映射 |

明文凭证键（大小写不敏感，出现在 YAML 任意层）解析失败，错误为 `ErrInvalidMapping`：

`dsn` · `password` · `uri` · `url` · `token` · `secret` · `user`

DSN 只通过 `connection.dsnRef` 命名；真实值在 `Options.DSNRefs` 解析。未解析的 `dsnRef` → `ErrInvalidMapping`。

### 4.3 Source

```yaml
sources:
  primary:
    kind: sql              # 空或 sql；其他 kind 非法
    dialect: sqlite        # Core 不解释；Open 要求 sqlite 或空
    connection:
      dsnRef: secret://hospital/sqlite-dsn
```

v3 所有可写绑定必须落在**同一个 SQLite 连接/文件**（一个事务域）。跨域写入返回 `ErrTransactionDomain`（表已定义；当前单文件 Open 路径不会主动跨域）。

### 4.4 Model / Link 绑定

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
      strategy: direct     # sidecar → ErrInvalidMapping
      columns: [id]
      insert: generated    # provider 写入 EncodeDirect(type, UUIDv7)
    tenant:
      strategy: column     # column | constant；connection 拒绝
      column: tenant_id
    system:
      strategy: native     # sidecar → ErrInvalidMapping
      omit: []             # 可选：version, createdAt, updatedAt, deletedAt
    fields:
      name:
        column: patient_name
        transform:
          kind: prefix     # 见 §13
          arg: "x-"
```

`omit` 的 YAML 键名由实现锁定；语义见 §7。未列出的系统列必须出现在表上。

Link 额外 MUST：

```yaml
links:
  AdmittedTo:
    # …与 model 相同的 sourceRef / relation / access / identity / tenant / system / fields
    from:
      object: Patient
      columns: [from_id]   # 存 Patient 的 engine id（即 patient.id）
    to:
      object: Ward
      columns: [to_id]
```

规则：

- `access: readWrite` + `relation.kind: view` → 编译/校验失败（`ErrInvalidMapping`），不是运行时只读错误。
- `access: read` 的 Query 成功；mutation → `ErrReadOnlyMapping`，零写入。
- identity 列 MUST 是 payload `fields` 中的某一 `column`，除非 `insert: generated`。
- 禁止把逻辑字段映射到 tenant 列（`Compile` 拒绝）。
- 逻辑字段名对应 ODL property；ODL Primary 被 projection 丢掉，因此可写 direct identity 必须用 `insert: generated` 或 payload 中的非 Primary 字段。

### 4.5 Relation 种类

| kind | 读 | 写 |
|---|---|---|
| `table` | 是 | `access: readWrite` 时必须能确定 INSERT/UPDATE/DELETE 目标 |
| `view` | 是 | 否 |
| `sqlQuery` | 未实现 | — |

### 4.6 物理形状（对应物理表）

v3 中 Engine `_id` **就是**业务表 identity 列。因此物理表 MUST 包含：

- identity 列（存储 `EncodeDirect` 编码后的字符串）
- tenant 列（`strategy: column` 时）
- 未 omit 的系统列（`version`、`created_at`、`updated_at`、`deleted_at`）
- link 表额外包含 `from_id` / `to_id` 列

辅助 DDL 可生成，但激活前必须已出现在库中。

---

## 5. Identity

### 5.1 `EncodeDirect`

`EncodeDirect(typ, keys)`：

```text
base64url( JSON{"t": "<object-or-link-type>", "k": ["<col0>", ...]} )
```

无 padding（`RawURLEncoding`）。禁止 delimiter 拼接。

v3 中 identity 列 **存储该字符串本身**。`GetObject(type, id)`：`DecodeDirect(id)` 校验 `t == type`，然后 `WHERE id = ?` 绑定原始 id 字符串。不必再经 meta 表翻译。

### 5.2 复合键

复合键：`k` 为有序分量。禁止 `type + ":" + key`。

例如：

```text
hospital_id = 001
patient_id  = 123

↓

EncodeDirect("Patient", ["001", "123"])
→ base64url({"t":"Patient","k":["001","123"]})
```

OBDA runtime MUST retain enough metadata to translate：

```text
ODL ID
    ↓
SQL identity predicate (WHERE id = ?)
```

### 5.3 Identity Transform 的限制

v3 只允许 `insert: generated`（provider 写入 `EncodeDirect(type, UUIDv7)`）或用 payload 中已有列做 identity。

不可逆 transform（如 `hash`）MUST NOT 用于 identity。`hash` transform 本身在本 spec 中非法。

解码失败、类型不匹配 → `ErrObjectNotFound` / `ErrLinkNotFound`。

### 5.4 CreateLink

CreateLink 可把 Engine 的 `_engineLinkId` 放进 `k`，或忽略后自铸；**返回的 `_id` 永远是编码后的 PK**，不是裸 UUID。

---

## 6. ApplySchema vs optional DDL helper

两件独立的事。

### 6.1 Optional helper

根据 compiled mapping + ODL 属性类型，生成（并可执行）mapped 表的 `CREATE TABLE` / cardinality `CREATE UNIQUE INDEX`。

用途：空库初始化、测试 fixture。MUST NOT 创建 `of_*`。MUST NOT `ALTER` 已有表。

### 6.2 Mandatory existence check

`ApplySchema` **每次** 对 live 库 introspect：

1. 每个 mapped table 存在
2. identity、tenant、未 omit 的系统列存在
3. 写约束所需 UNIQUE 存在（按 cardinality）

任一项失败 → 不得激活。

**即使本进程刚跑过 §6.1，也必须走 §6.2。** 只生成未执行的 DDL、或 Exec 失败，检查必须失败。不得把 DDL 字符串当作存在证明。

表在但列/索引不符 → 失败（`ErrInvalidMapping` 或 `ErrSourceSchemaDrift`，实现选定一个并保持稳定）。不得 ALTER 修补。

激活结果只留在进程内。无 `of_mapping_activation`。`GetSchema` 读内存快照。`HealthCheck` 可用进程内 fingerprint；漂移后 fail-closed。

### 6.3 Cardinality 约束

`ApplySchema` 按 schema 为 link 建部分唯一索引（由辅助 DDL 生成或已存在于库中），例如 MANY_TO_ONE：

```sql
CREATE UNIQUE INDEX ... ON admission (tenant_id, from_id)
  WHERE deleted_at IS NULL
```

MANY_TO_ONE 用 `(tenant_id, from_id)`；ONE_TO_MANY 用 `(tenant_id, to_id)`；ONE_TO_ONE 两端各一。MANY_TO_MANY 不建该约束。省略 `deleted_at` 时 UNIQUE 不得依赖该列。

冲突分类为 `ErrCardinalityViolation`。查询侧 `LIMIT 1` 不能代替写约束。

### 6.4 Mapping Version

```yaml
metadata:
  version: 7
```

代表：

```text
OBDA Mapping v7
```

而不是：

```text
ODL schema v7
```

两者独立：

```text
ODL Schema
  1.4.0

OBDA Mapping
  v27
```

Runtime 必须检查：

```text
ODL Schema Version
          +
OBDA Mapping Version
          +
Source Schema Version
```

形成 Semantic Schema / Physical Mapping / Physical Source 三方一致。

---

## 7. System fields

### 7.1 返回结构

返回对象 MUST 含：`_tenantId` `_type` `_id`；若未 omit：`_version` `_createdAt` `_updatedAt`；软删时含 `_deletedAt`。
返回 link 另加：`_fromType` `_fromId` `_toType` `_toId`。

Open Foundry Object 包含：

```text
_tenantId
_type
_id
_version
_createdAt
_updatedAt
_deletedAt
```

`_type` 和 `_id` 由 Model Identity 派生。缺失的系统列由业务表提供（native strategy），同一记录每次读取值稳定。

### 7.2 omit 行为

| omit | 行为 |
|---|---|
| `deletedAt` | 无软删；soft `DeleteObject` → `ErrUnsupportedCapability`；列表不加删除谓词 |
| `version` | 无 CAS；非空 `expectedVersion` → `ErrUnsupportedCapability`；返回 `_version = 0` |
| `createdAt` / `updatedAt` | 不写这些列；返回中可缺对应字段 |
| 未 omit | 列必须在表上；OCC / 软删按该列执行 |

identity 与 tenant **不可 omit**。

### 7.3 删除语义

| 操作 | 行为 |
|---|---|
| `GetObject` / `GetLink` | 可返回软删行 |
| `QueryObjects` / `GetLinks` | 默认排除软删；`IncludeDeleted` 可包含 |
| 软删后再 Update | `ErrObjectNotFound` / `ErrLinkNotFound`，不是 `ErrVersionConflict` |
| 软删端点 `CreateLink` | `ErrObjectNotFound`，不插行 |
| 软删后再 soft delete | 幂等成功 |
| Hard delete | 同事务删除全部以其 id 为 `from_id`/`to_id` 的 link 行，删业务行。无 history 可留。 |

时间戳格式 RFC3339Nano。Boolean 读路径经 `NormalizeValue`：SQLite INTEGER 0/1 → Go `bool`。

Engine `GetObject` 透传 SPI，因此注入 sqliteobda 后 Engine 能看见软删。这与 `memory.GetObject` 的 mask 分叉；本轮不改 Engine。

### 7.4 `createdBy / updatedBy`

如果来源系统有 `created_by` / `updated_by`，可以映射到系统字段。但它们仍然必须满足 ODL `@readonly` 语义。本轮 v3 不强制实现。

---

## 8. Query, links, traverse

### 8.1 Query

- `Limit <= 0` → 100；上限 1000
- identity 列做稳定 tie-breaker
- `Cursor` 保持空（SPI 尚未消费 cursor）
- `OrderBy` 必须是 mapping 逻辑名；非法标识符不得变成 SQL
- `AsOfTime` / `AsOfVersion` → `ErrUnsupportedCapability`（不得返回 live 行冒充历史）
- 当前实现要求单列 identity + tenant 列，否则 Query 返回 `ErrUnsupportedCapability`
- JOIN `of_object_meta` **禁止**

### 8.2 OCC

`expectedVersion` 在业务表 `version` 列上原子 CAS。`expectedVersion == nil` 仍要求未删除存在行。并发同一 version：一成功一 `ErrVersionConflict`。

### 8.3 GetLinks

GetLinks：先解全局对象 id（`DecodeDirect`）。默认排除软删 link。

### 8.4 Traverse

Traverse：planner 产出固定 `path.Steps` 的链式 `sqlast.Join`（FROM 起点对象表，每跳 JOIN link 表再 JOIN 目标对象表）。同一条 SQL 带每跳 `tenant_id` 及未 omit 的 `deleted_at IS NULL`。结果只交终点 `Nodes`；sqliteobda 本轮 `Edges` / `Visited` 为空。深度 > 8 → `ErrUnsupportedCapability`。未知/跨租户/错类型 start → `ErrObjectNotFound`。禁止再对影子表 BFS。**不是** recursive CTE。多态「一列指向多种 ObjectType」不支持：from/to 在 mapping 里 typed。

### 8.5 Traversal 示例

调用：

```ts
traverse(
  "patient-xxx",
  {
    steps: [
      {
        linkType: "AdmittedTo",
        direction: "outbound"
      }
    ]
  }
)
```

OBDA：

```text
Patient.id
   ↓
patient.id

AdmittedTo
   ↓
admission.from_id

AdmittedTo.to
   ↓
admission.to_id

Ward.id
   ↓
ward.id
```

最终：

```sql
SELECT w.id, w.ward_name, w.tenant_id, w.version, w.created_at, w.updated_at, w.deleted_at
FROM patient p
JOIN admission a
  ON a.from_id = p.id AND a.tenant_id = p.tenant_id
JOIN ward w
  ON w.id = a.to_id AND w.tenant_id = a.tenant_id
WHERE p.id = $1
  AND p.tenant_id = $2
  AND a.deleted_at IS NULL
  AND w.deleted_at IS NULL
```

装配为 `Nodes`（终点 Ward）。`IncludeDeleted` 时去掉沿途软删谓词。起点表不加 `deleted_at` 谓词。

### 8.6 Multi-hop Traversal

例如：

```text
Patient
  ↓
AdmittedTo
  ↓
Ward
  ↓
BelongsTo
  ↓
Trust
```

Planner 产生：

```text
Patient
   ↓
patient
   ↓
admission
   ↓
ward
   ↓
ward_trust
   ↓
trust
```

这使得 `traverse()` 可以在 SQL backend 上实现，而不需要实际使用 graph database。

因此：

> `traverse()` 是 Semantic Graph API，而不是 Graph Database API。

### 8.7 Filter Mapping

SPI 的 FilterExpression 已明确支持：

```text
eq
neq
gt
gte
lt
lte
in
contains
startsWith
exists
AND
OR
NOT
```

OBDA Compiler MUST 将这些 Semantic Predicate 翻译为 source predicate。

例如：

```text
Patient.name = "Smith"
```

↓

```sql
patient.patient_name = $1
```

内部 MUST 采用 AST：

```text
FieldPredicate
      ↓
Mapping Resolver
      ↓
SqlExpression
```

例如：

```ts
{
  field: "name",
  operator: "contains",
  value: "Smith"
}
```

变成：

```sql
LIKE '%' || $1 || '%'
```

而不是字符串拼接。

可作为 FilterExpression 目标的字段 MUST 是 column mapping 或可逆 transform。

### 8.8 `maxDepth`

ODL SPI 已规定：

```ts
maxDepth?: number
```

以及 provider capability：

```ts
maxTraversalDepth
```

OBDA Planner MUST 在 compile/plan 阶段检查：

```text
requestedDepth <= provider.maxTraversalDepth
```

否则返回：

```text
ErrUnsupportedCapability
```

### 8.9 Query Pushdown Contract

OBDA Runtime SHOULD 最大化 source pushdown。

例如：

```text
Filter
+
Projection
+
Join
+
Limit
```

应该尽量变成：

```sql
SELECT ...
FROM ...
JOIN ...
WHERE ...
LIMIT ...
```

而不是：

```text
SELECT *
      ↓
Go runtime
      ↓
Filter
      ↓
Projection
```

### 8.10 Query Compilation Pipeline

推荐：

```text
SPI Query
   ↓
Semantic Query IR
   ↓
Schema Resolver
   ↓
OBDA Mapping Resolver
   ↓
Relational Algebra
   ↓
Optimizer
   ↓
SQL AST
   ↓
Dialect Renderer
   ↓
SQL
```

不要：

```text
TraversalPath
   ↓
SQL string
```

### 8.11 `traverse()` 的最终执行模型

```text
traverse(startId, path)
          │
          ▼
     Traversal IR
          │
          ▼
     ODL Resolver
          │
          ▼
     Link Resolver
          │
          ▼
     OBDA Resolver
          │
          ▼
  Relational Algebra (sqlast.Join)
          │
          ▼
        SQL
```

---

## 9. Tenant, errors, security

### 9.1 租户

空 `TenantID` → `ErrTenantRequired`（含 `ApplySchema`）。

| strategy | 行为 |
|---|---|
| `column` | 每个读写注入租户谓词；值只来自 `RequestContext` |
| `constant` | 绑定只服务配置的那一个租户；其他租户当 not-found |
| `connection` | v3 拒绝 |

调用方 filter / properties 中的 `_tenantId` 不能扩大可见范围或改写存储租户。跨租户与缺失同一 `ErrObjectNotFound` / `ErrLinkNotFound`。Link 两端必须同租户。

> tenant predicate MUST 由 runtime 自动添加，不能由 LLM / Agent / Client 指定。

### 9.2 错误契约

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

不再出现依赖 sidecar 的 split-brain。缺表/缺列在 ApplySchema 阶段失败，不在每个 Get 上猜。

### 9.3 安全边界

v3 仍强制：租户隔离、参数化值、标识符白名单、dsnRef 而非明文、公开错误脱敏、Query limit / Traverse 深度上界。

完整查询执行链：

```text
Agent
  ↓
API
  ↓
Security (AuthN / AuthZ)
  ↓
Semantic Query
  ↓
OBDA
  ↓
SQL
```

不能：

```text
Agent
  ↓
OBDA
  ↓
SQL
```

能调用 `StorageProvider` 即视为存储层可信调用方。授权与 consent 在 SPI 之上。无 ReBAC。

参数化 SQL、标识符白名单、dsnRef、公开错误不含路径/DSN/SQL。

### 9.4 Permission 不放进 `obda.yaml`

```text
OBDA
≠
Permission
```

不要：

```yaml
permissions:
```

原因：

```text
ODL
  +
OpenFGA
  +
Security Layer
```

才是权限真源。

OBDA 负责：

```text
Where is the data?
```

Security 负责：

```text
Who can see it?
```

### 9.5 `@sensitive`

`@sensitive` 不属于 OBDA Mapping。

ODL 定义：

```text
Patient.name @sensitive
```

OBDA 只提供：

```text
source column
```

Security Layer 仍然必须在 query pipeline 里执行 field redaction。

---

## 10. SPI surface

Provider 嵌入 `UnimplementedStorageProvider` 仅满足 Go 前向兼容。返回 `ErrUnimplemented` **不是**完整 v1 成功路径。

| 方法 | v3 |
|---|---|
| `ApplySchema` / `GetSchema` | 有。ApplySchema = 检查 + 进程内激活，不建 `of_*` |
| `HealthCheck` / `Capabilities` | 有。Details 不含路径/DSN/SQL。 |
| `CreateObject` `GetObject` `UpdateObject` `DeleteObject` `QueryObjects` | 有。业务表。 |
| `CreateLink` `GetLink` `UpdateLink` `DeleteLink` `GetLinks` `Traverse` | 有。业务表 JOIN。Traverse 只交终点 Nodes。 |
| `BeginTransaction` | 有（见 §12） |
| `GetObjectAtVersion` `GetObjectAtTime` | `ErrUnsupportedCapability` 或 `ErrUnimplemented`，不得装成成功 |
| `AggregateObjects` `SearchObjects` `BulkMutate` | `ErrUnimplemented` |
| `EnsureIndex` `DropIndex` `ListIndexes` | `ErrUnimplemented` |

`Capabilities()` 不得把未实现的 temporal/bulk 报成 true。当前声明（方言探测 FTS5）：

```text
SupportsTransactions    = true
SupportsTemporalQueries = false   # v3 不再超前声明
SupportsFullTextSearch  = ProbeFTS5
SupportsGeoQueries      = false
SupportsGraphTraversal  = true
SupportsBulkMutations   = false   # v3 不再超前声明
MaxTraversalDepth       = 8
ReplicationSupport      = none
```

计划中的收口（尚未落地）：

- Search：无 searchable 映射 + 非空 query → `ErrUnsupportedCapability`；空白 query → 空 hits。FTS5 `MATCH ?`，query 在 planner 转义。
- Bulk：租户作用域幂等键；同键不同 payload hash → `ErrIdempotencyConflict`；失败整笔回滚，无 resume。
- Temporal：只读 history 表（v3 无 history → 不可用）；缺历史不得返回当前行。
- EnsureIndex：FTS5 可建；业务表 BTREE/HASH/GIN/GIST → `ErrUnsupportedIndexType`。

---

## 11. SQLite dialect

- Placeholder：`?`
- 标识符：白名单 `^[A-Za-z_][A-Za-z0-9_]*$`，双引号包裹；嵌入 `"` 或非法字符在渲染前拒绝
- 分页：`LIMIT ? OFFSET ?`
- STRICT 业务表由辅助 DDL 生成时可用
- 并发：WAL + 写连接 `busy_timeout=5000`；不承诺高并发 SLA
- FTS5：启动探测；未编译则 capability false
- `NormalizeValue`：Boolean INTEGER 0/1 → Go `bool`；datetime TEXT → RFC3339Nano 字符串
- Core 测试不得链接 `modernc.org/sqlite`
- 删除 `SidecarStatements`；代之以 mapped-table DDL 生成器

---

## 12. Transaction

`BeginTransaction` 钉住 tenant + compiled mapping + `*sql.Tx`。写连接先 `PRAGMA busy_timeout=5000` 再 `BeginTx`。测试 DSN 带 `_busy_timeout=5000`。打开 Tx 时并发 `GetObject` / `HealthCheck` 不得挂死。

Rollback 后业务行均不可见。

当前缺口：`sqlTxn.UpdateLink` / `DeleteLink` 仍走公开方法（各自开事务）。对象动词与 `CreateLink` 已走 pinned Tx。

Engine 本轮无事务 API。

---

## 13. Transforms

### 13.1 封闭集合

未知 kind → `ErrInvalidMapping`。

| kind | 可写 |
|---|---|
| （空）列直映 | 是 |
| `prefix` `suffix` `trim` `toUpper` `toLower` `map` `parseDate` `parseDateTime` | 是 |
| `coalesce` | 仅 `access: read` |
| `hash`、任意表达式、computed SQL | 否（非法） |

Parser 接受 transform 字段；当前 sqliteobda 写路径按列直映执行。不可逆 transform 不得出现在可写绑定上。

### 13.2 Transform Pipeline

Transform 可以组合：

```yaml
name:
  column: surname
  transform:
    - kind: trim
    - kind: toUpper
```

或者：

```yaml
dateOfBirth:
  column: dob
  transform:
    kind: parseDate
    arg: "DD/MM/YYYY"
```

推荐内部 IR：

```text
Column
 ↓
Trim
 ↓
Lower
 ↓
Map
 ↓
Cast
 ↓
ODL Value
```

### 13.3 Enum Mapping

例如 ODL：

```graphql
enum PatientStatus {
  ACTIVE
  DISCHARGED
  DECEASED
}
```

SQL：

```text
A
D
X
```

可以：

```yaml
status:
  column: status
  transform:
    kind: map
    args:
      A: ACTIVE
      D: DISCHARGED
      X: DECEASED
```

### 13.4 Null Mapping

NULL SHOULD 保持 NULL：

```text
SQL NULL
   ↓
ODL null
```

除非显式：

```yaml
transform:
  kind: coalesce
  arg: UNKNOWN
```

OBDA MUST NOT 默默把 SQL NULL 转换成空字符串。

### 13.5 Type Mapping

OBDA Compiler MUST 根据 ODL 类型进行 SQL 类型验证。

基础类型：

| ODL | SQL 示例 |
|---|---|
| ID | TEXT/VARCHAR/UUID |
| String | TEXT/VARCHAR |
| Int | INTEGER |
| Float | REAL/DOUBLE PRECISION |
| Boolean | INTEGER (0/1) |
| Date | TEXT (ISO date) |
| DateTime | TEXT (RFC3339Nano) |
| Duration | implementation-specific |
| JSON | TEXT/JSON |
| URI | TEXT/VARCHAR |
| GeoPoint | JSON |

SQLite 是动态类型；type validation 在 Compiler 层基于 ODL 类型与 transform 语义。

---

## 14. Validation & compiler

### 14.1 Compiler Validation

`obda validate` 至少需要执行：

```text
1. YAML Schema Validation
2. ODL Reference Validation
3. ObjectType Mapping Validation
4. Property Validation
5. Identity Validation
6. Link Validation
7. Cardinality Validation
8. Transform Validation
9. Type Compatibility
10. Source Capability Validation
11. Tenant Mapping Validation
12. System Field / omit Validation
```

### 14.2 Object Validation Rules

例如：

```yaml
models:
  Patient:
    ...
```

Compiler MUST 检查：

```text
Patient 是否存在于 ODL
Patient 是否为 @objectType
Patient 是否有 @primary
identity 列是否 mapped
```

如果 ODL 中没有 `Patient`：

```text
OBDA_UNKNOWN_OBJECT_TYPE
```

### 14.3 Field Validation

例如：

```yaml
fields:
  name:
    column: patient_name
```

Compiler MUST 检查 `Patient.name` 是否存在。

如果 ODL 被修改成 `fullName` 而 Mapping 没改：

```text
OBDA_UNKNOWN_PROPERTY
```

### 14.4 Link Validation

例如：

```yaml
links:
  AdmittedTo:
```

Compiler MUST 检查 `AdmittedTo` 是否为 `@linkType`，并检查：

```text
from.object == @linkType.from
to.object   == @linkType.to
```

### 14.5 Direction Validation

如果：

```text
ODL:
Patient.currentWard
  direction = OUTBOUND
```

那么：

```text
traverse(Patient, OUTBOUND)
```

必须走：

```text
AdmittedTo.from → AdmittedTo.to
```

而：

```text
Ward → Patient
```

必须使用 `INBOUND`。

### 14.6 Physical Join Validation

Compiler SHOULD 检查 `from.columns` 与 `models[from.object].identity` 兼容。

例如 `Patient.id` 必须能被解析到 `patient.id`，否则：

```text
OBDA_INVALID_LINK_KEY
```

### 14.7 SQL Type Validation

例如：

```text
ODL:
Patient.dateOfBirth : Date

SQL:
patient_name : TEXT
```

OBDA Compiler SHOULD 基于 ODL 类型与 transform 检查兼容性。

---

## 15. ODL directives & advanced mapping

### 15.1 Computed Field

ODL 支持：

```graphql
@computed(
  fn: "countLinks",
  args: { type: "AdmittedTo" }
)
```

v3 不支持 mapping 内 computed SQL。`computed.kind: sql` 非法。

### 15.2 `@indexed`

ODL：

```graphql
name: String @indexed
```

定义的是：

> 该属性用于 structured/exact lookup。

OBDA 不改变 ODL 的 semantic indexing contract。如果源系统没有对应 index，Compiler MAY warning。

### 15.3 `@unique`

对于可写绑定：

```text
@unique
```

是 Semantic Contract。OBDA 不应该自动修改数据库 schema。Compiler SHOULD 检查 `COUNT(DISTINCT ...)` 但不自动建索引。

### 15.4 `@constraint`

`@constraint` 属于 ODL 业务语义，不能复制到 OBDA。

Query-time optimization MAY push down equivalent predicates。这属于 compiler optimization，而非 Mapping Semantic。

### 15.5 `@terminology`

例如：

```graphql
code: CodeableConcept!
  @terminology(system: "http://snomed.info/sct")
```

OBDA 只提供 source column。terminology binding 仍由 ODL 控制。

### 15.6 Interface Mapping

Interfaces 不需要独立 OBDA mapping。

```text
Interface
   ↓
ObjectType implementation
   ↓
Object mapping
```

因此：

```yaml
models:
  Patient:
    ...
  Ward:
    ...
```

不需要：

```yaml
interfaces:
  Identifiable:
```

### 15.7 Link Property Mapping

LinkType 可以带自己的属性：

```graphql
type AdmittedTo @linkType(...) {
  id: ID! @primary
  admissionDate: DateTime!
  expectedDischarge: DateTime
}
```

OBDA：

```yaml
links:
  AdmittedTo:
    fields:
      admissionDate:
        column: admission_datetime
      expectedDischarge:
        column: expected_discharge
```

这是必须支持的，因为 Link 当作带自己属性和自己 ID 的一等实体。

### 15.8 Many-to-Many

例如：

```text
Doctor
   │
   │ WorksAt
   ▼
Ward
```

物理：

```text
doctor_ward
```

Mapping：

```yaml
links:
  WorksAt:
    sourceRef: primary
    relation:
      kind: table
      name: doctor_ward
    from:
      object: Doctor
      columns: [from_id]
    to:
      object: Ward
      columns: [to_id]
```

### 15.9 Permission 不放进 `obda.yaml`

见 §9.4。

---

## 16. Tooling

### 16.1 `explain`

应该提供：

```bash
obda explain \
  --object Patient \
  --id patient-xxx
```

输出：

```text
ODL:
  Patient.id

OBDA:
  patient.id

Identity:
  DecodeDirect → type=Patient, keys=[...]

SQL:
  SELECT ...
  FROM patient
  WHERE id = $1
    AND tenant_id = $2
```

`explain` / traces / provenance MUST 对 `@sensitive` bind values 与列 payload 做 redaction。

### 16.2 `explain traverse`

例如：

```bash
obda explain-traverse \
  Patient \
  AdmittedTo \
  Ward
```

输出：

```text
Patient
  └── AdmittedTo
        └── Ward

Physical Plan:

patient
   |
admission
   |
ward
```

以及：

```sql
SELECT ...
FROM patient
JOIN admission ...
JOIN ward ...
```

### 16.3 `validate`

```bash
obda validate hospital.obda.yaml
```

输出：

```text
✓ ODL namespace found
✓ ODL schema version compatible
✓ Patient mapping valid
✓ Ward mapping valid
✓ AdmittedTo mapping valid
✓ Identity mapping valid
✓ Link cardinality compatible
✓ Tenant mapping valid
✓ System fields valid
```

### 16.4 Source Introspection

因为 v2.0 Connector 已定义 `discoverSchema()`，所以可以：

```bash
obda introspect \
  --source hospital-db
```

产生：

```yaml
models:
  Patient:
    ...
```

也就是：

```text
Database Schema
       ↓
Candidate OBDA
```

`obda introspect` / `obda generate` / `obda explain` 是 privileged operator 操作：MUST 认证授权、默认非生产、并审计。生成结果 MUST 保持 candidate-only，直到人工审查。

### 16.5 Auto-Mapping

推荐提供：

```bash
obda generate \
  --odl schema/ \
  --source hospital-db
```

生成：

```text
candidate.obda.yaml
```

而不是直接修改 production mapping。永不静默应用到 production。

### 16.6 Mapping Diff

```bash
obda diff \
  --from obda.v7.yaml \
  --to obda.v8.yaml
```

输出：

```text
MODIFIED Patient.name
  old: patient_name
  new: full_name

ADDED Patient.email

MODIFIED AdmittedTo
  source: admission
  target source: patient_admission
```

### 16.7 Mapping Compatibility

定义：

```text
SAFE
COMPATIBLE
BREAKING
```

例如：

```text
新增 field mapping
→ SAFE
```

```text
修改 transform
→ COMPATIBLE / BREAKING
```

取决于是否改变 semantics。

### 16.8 Mapping Version Rollback

```bash
obda rollback \
  --from-version 8 \
  --to-version 7
```

生成：

```text
OBDA mapping v9
```

其 effective mapping 等价于 v7。与 v2.0 的 forward-only schema rollback 设计一致。

---

## 17. Observability

### 17.1 Spans

OBDA SHOULD emit spans：

```text
openfoundry.obda.plan
openfoundry.obda.compile
openfoundry.obda.source_query
openfoundry.obda.traverse
```

并包含：

```text
mapping.name
mapping.version
source.name
source.dialect
object.type
link.type
query.depth
```

### 17.2 Query Cost

OBDA Planner SHOULD 生成：

```text
estimated cost
estimated rows
join count
scan count
```

例如：

```text
Patient → Admission → Ward

joins: 2
estimated rows: 1
cost: 3.4ms
```

### 17.3 Capability Detection

OBDA Backend SHOULD 映射到：

```ts
StorageCapabilities
```

例如：

```text
supportsGraphTraversal = true
supportsTemporalQueries = false
supportsFullTextSearch = ProbeFTS5
```

如果：

```text
supportsFullTextSearch = false
```

那么 `searchFoos` 可以不生成。这与 v2.0 Query API 的 capability-driven design 一致。

### 17.4 Schema Cache

建议 OBDA 编译后的运行时缓存：

```text
SemanticSchemaCache
```

结构类似：

```ts
interface SemanticSchemaCache {
  namespace: string;
  odlVersion: number;
  mappingVersion: number;

  objects: Map<string, ObjectMapping>;

  links: Map<string, LinkMapping>;

  sources: Map<string, SourceMapping>;
}
```

v3 中激活结果只留在进程内，无持久化版本表。

### 17.5 Provenance

对于 v3 direct-native：

```text
Patient.name
```

应能得到：

```json
{
  "kind": "NATIVE",
  "sourceSystem": "sqlite",
  "sourcePointer": {
    "table": "patient",
    "column": "patient_name",
    "key": {
      "id": "patient-xxx"
    }
  }
}
```

---

## 18. Removed vs v2

| v2 | v3 |
|---|---|
| `sidecar` identity + `of_*_meta` | 删除 |
| `_id` 与物理 PK 两套值 | 合一 |
| Traverse = GetLinks BFS on `of_link_meta` | JOIN 业务表；Traverse 只交终点 Nodes |
| ApplySchema 创建 sidecar | 禁止；改为存在性检查 |
| 无自动业务表 DDL | 可选辅助，且不能跳过检查 |
| 系统列缺失由 sidecar 补 | 列在表上，或 mapping omit |
| 持久化 mapping/schema 版本表 | 进程内激活 |
| `of_object_meta` 全量 UNIQUE | 业务表 PK + cardinality UNIQUE |
| `SidecarStatements` | mapped-table DDL 生成器 |
| `system.strategy: sidecar` | `system.strategy: native` only |
| `identity.strategy: sidecar` | `identity.strategy: direct` only |

相对 `open-foundry-obda-mapping-spec-v2.md`：

- 完整 `StorageProvider`，不是 OVERLAY read-through
- 没有 PostgreSQL+AGE 本体存储、没有 Sync Engine、没有 `sync.mode`
- YAML 形状是 `sourceRef` + `relation` + `identity.strategy: direct`，不是 `source.kind` + `identity.fields[].target`
- 无 ReBAC 谓词注入
- Traverse 为链式 JOIN（只交终点 Nodes），不是 BFS on `of_link_meta`
- 对象物理键唯一索引是全量 unique，不是仅 active 部分唯一（v2 sidecar）
- mapping 由 `Open` 注入，不走 `ApplySchema` 第二参数

相对 `open-foundry-obda-mapping-spec-v1.md`：

- 不是 OVERLAY read-through
- 没有 PostgreSQL+AGE 本体存储、没有 Sync Engine、没有 `sync.mode`
- 没有 `sqlQuery` source、没有 `source.expression`、没有 computed SQL
- 没有 ReBAC 谓词注入
- 没有 overlay cache、没有 writeback
- v1 的 overlay / AGE / ReBAC 叙事仍然作废

---

## 19. Package layout

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

> 注：`sidecar.go` 在 v3 中将改为 native 存储逻辑或重命名。`options.go` / `health.go` / `registry.go` 未单列；compiled mapping 在 `Compile` + `activation`。

---

## 20. Implementation progress

| 单元 | 状态 |
|---|---|
| U1–U6 mapping / AST / SQLite dialect / sidecar / object / link | 完成（v3 需适配 direct-native） |
| U7 事务 | 部分（对象 + CreateLink） |
| U7 aggregate / search / bulk / temporal / index / AE1 | 未做 |
| U8 Engine smoke + origin-vs-memory | 未做 |

测试：`cd runtime && go test ./obda/... ./storage/sqliteobda/`，真实 `t.TempDir()` 文件库，无 MySQL 容器，无共享 `:memory:`。

---

## 21. Canonical example

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
      strategy: direct
      columns: [id]
      insert: generated
    tenant:
      strategy: column
      column: tenant_id
    system:
      strategy: native
    fields:
      name:
        column: patient_name
  Ward:
    sourceRef: primary
    relation:
      kind: table
      name: ward
    access: readWrite
    identity:
      strategy: direct
      columns: [id]
      insert: generated
    tenant:
      strategy: column
      column: tenant_id
    system:
      strategy: native
    fields:
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
      strategy: direct
      columns: [id]
      insert: generated
    from:
      object: Patient
      columns: [from_id]
    to:
      object: Ward
      columns: [to_id]
    tenant:
      strategy: column
      column: tenant_id
    system:
      strategy: native
    fields: {}
```

对应物理形状（辅助 DDL 可生成，但激活前必须已出现在库中）：

```sql
CREATE TABLE patient (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  patient_name TEXT,
  version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT
);
CREATE TABLE ward ( /* 同形 */ );
CREATE TABLE admission (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  from_id TEXT NOT NULL,
  to_id TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT
);
CREATE UNIQUE INDEX admission_from_active
  ON admission (tenant_id, from_id) WHERE deleted_at IS NULL;
```

MANY_TO_ONE 用 `(tenant_id, from_id)`；ONE_TO_MANY 用 `(tenant_id, to_id)`；ONE_TO_ONE 两端各一。省略 `deleted_at` 时 UNIQUE 不得依赖该列。

---

## 22. Scope summary

### 22.1 本轮必须支持的 Source Kind

```text
TABLE
VIEW
```

### 22.2 本轮必须支持的 Mapping Kind

```text
Object Mapping
Property Mapping
Identity Mapping (direct)
Link Mapping
Link Property Mapping
```

### 22.3 本轮必须支持的 Query Capability

```text
getObject
queryObjects
getLinks
traverse
```

### 22.4 本轮必须支持的 Semantic Features

```text
Identity (direct)
Property
Filter
AND
OR
NOT
Pagination
Order By
Tenant
Soft Delete (unless omitted)
OCC (unless version omitted)
```

### 22.5 本轮 SHOULD 支持

```text
Date / DateTime
Enum mapping
Transform pipeline
Explain
Validate
Introspection
```

### 22.6 未来再支持

```text
Variable-length traversal (recursive CTE)
Cross-source joins
Distributed query
Federated source
Graph shortest path
Cost-based source selection
Materialized view routing
History / temporal
Bulk mutations
Search (FTS5)
Writeback
```

---

## 23. Design principles & conclusion

### 23.1 最终语义模型

最终整个 Open Foundry 可以清楚地分成：

```text
                 ODL
              Semantic TBox
                   │
                   │
        ┌──────────▼──────────┐
        │      OBDA           │
        │                     │
        │ Object Mapping      │
        │ Property Mapping    │
        │ Link Mapping        │
        │ Identity Mapping    │
        │ Source Mapping      │
        └──────────┬──────────┘
                   │
                   ▼
            Physical Sources
                   │
       ┌───────────┼────────────┐
       ▼           ▼            ▼
   SQLite      PostgreSQL     APIs/FHIR
```

### 23.2 Query 的最终链路

```text
Agent
  │
  ▼
Security (AuthN / AuthZ)
  │
  ▼
Semantic Query
  │
  ▼
traverse(
  startId,
  path,
  options
)
  │
  ▼
Traversal IR
  │
  ▼
ODL TBox
  │
  ▼
OBDA Mapping
  │
  ▼
Relational Algebra (sqlast.Join)
  │
  ▼
SQL
  │
  ▼
SQLite (business tables)
```

因此：

```text
Agent 不需要知道 SQL schema
Agent 不需要知道 FK
Agent 不需要知道 JOIN
Agent 不需要知道 patient_id
Agent 只需要知道 ODL
```

这正是这个方案最大的价值。

### 23.3 与 Hasura 思路的关系

如果借鉴 Hasura，建议借鉴它的：

```text
Source
Model
Relationship
Metadata
Schema Cache
Query Planning
Data Connector
```

然后替换成 Open Foundry：

```text
Data Source
ObjectType
LinkType
OBDA Metadata
Semantic Schema Cache
Semantic Query Planner
OBDA Connector
```

所以最终可以把 Open Foundry OBDA 理解成：

> **Ontology-aware query compiler over mapping metadata**

工件名称保持 **OBDA Mapping**（`*.obda.yaml`，`kind: OBDAConfig`）。

### 23.4 三个最重要的设计决定

**第一：`models` 对应 ODL `ObjectType`，`links` 对应 ODL `LinkType`。**

**第二：Identity Mapping 是核心，不是普通 Column Mapping。** 因为 `getObject()`、`getLinks()`、`traverse()` 都依赖 ODL ID → 物理 key 的可执行映射。v3 中 identity 列直接存储 `EncodeDirect` 编码后的字符串，无中间表。

**第三：Engine `_id` 就是业务表 identity 列。** 无 sidecar，无 `of_*` 表。`GetLinks` / `Traverse` 对业务表做参数化 JOIN。这是可注入 Engine 的完整读写存储。

### 23.5 推荐的 JSON Schema

`obda.yaml` 应提供 JSON Schema：

```text
obda-config.schema.json
```

这样 VS Code、Cursor、JetBrains、CI、CLI 都可以直接 validation。

### 23.6 实现边界

第一阶段只需要完成：

```text
✓ SQLite Source
✓ Table / View
✓ Object Mapping (direct identity)
✓ Identity Mapping (direct, EncodeDirect)
✓ Property Mapping
✓ Link Mapping
✓ Filter
✓ AND / OR / NOT
✓ getObject
✓ queryObjects
✓ getLinks
✓ traverse
✓ Tenant
✓ Soft Delete
✓ OCC
✓ Explain
✓ Validate
✓ Schema Cache
✓ obda generate (candidate only)
```

然后第二阶段：

```text
+ Temporal (if history table added)
+ Search (FTS5)
+ Bulk mutations
+ EnsureIndex
```

第三阶段：

```text
+ Cross-source Join
+ Recursive Traversal
+ Federated Semantic Query
+ Cost-based Planner
+ Other dialects (MySQL, PostgreSQL, DuckDB)
```

### 23.7 结论

结合 v2.0，OBDA Mapping 正式定义为：

> **OBDA Mapping**（Open Foundry Semantic Data Mapping Metadata 的正式名）

并遵循：

```text
ODL
   ↓
OBDA Metadata
   ↓
Semantic Schema Cache
   ↓
Semantic Query Planner
   ↓
SQL / SQLite
```

从工程落地角度，下一步定成这四个产物：

```text
obda-config.schema.json
        +
obda.yaml example
        +
OBDA Mapping IR
        +
SQLite OBDA Compiler (direct-native)
```

这样就可以直接验证：

```text
ODL
  +
obda.yaml
  ↓
traverse()
  ↓
SQL
```

是否能够完整跑通。
