# Open Foundry — OBDA Mapping Specification v1.0

**Status:** Proposed  
**Based on:** Open Foundry — Technical Specification v2.0  
**Format:** `*.obda.yaml`  
**Purpose:** Define the physical mapping between Open Foundry ODL semantic types and external data sources.

---

## Overview & Architecture

### 1. 概述

Open Foundry ODL 定义业务语义模型：

```text
ObjectType
LinkType
Property
Identity
Constraint
Cardinality
Computed Field
Namespace
```

ODL 是 ontology schema 的 single source of truth。Open Foundry v2.0 已明确规定 Schema-Driven 原则：API、权限、SDK、UI 等均由 ontology schema 生成。

而数据库只负责物理存储：

```text
PostgreSQL
MySQL
SQL Server
SQLite
DuckDB
```

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

---

### 2. 设计原则

#### 2.1 ODL 是 Semantic Source of Truth

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
public.patient
```

而不再次声明：

```yaml
Patient:
  id: ID
  name: String
```

---

### 3. 核心架构

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
                  │ Model              │
                  │ Identity           │
                  │ Property Mapping   │
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
                  │  Security Layer    │
                  │ AuthN / AuthZ /    │
                  │ Consent            │
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
                  └─────────┬──────────┘
                            │
                            ▼
                           SQL
```

---

### 4. 文件命名

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
```

---

### 5. 顶层结构

规范结构：

```yaml
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig

metadata:
  name: <string>
  namespace: <namespace>
  version: <integer>
  description: <string>

schema:
  namespace: <ODL namespace>
  version: <ODL schema version>

sources:
  ...

models:
  ...

links:
  ...

runtime:
  ...

sync:
  ...
```

其中：

| 字段 | REQUIRED | 说明 |
|---|---:|---|
| `apiVersion` | MUST | OBDA 规范版本 |
| `kind` | MUST | 固定 `OBDAConfig` |
| `metadata.name` | MUST | Mapping 名 |
| `metadata.namespace` | MUST | Open Foundry namespace |
| `metadata.version` | MUST | Mapping 版本 |
| `schema` | MUST | 对应 ODL schema |
| `sources` | MUST | 数据源定义 |
| `models` | MUST | ObjectType 映射 |
| `links` | MAY | LinkType 映射 |
| `runtime` | MAY | Overlay cache / query limits 等执行参数。Overlay vs materialized 以 `sync.mode` 为准。 |
| `sync` | MAY | CDC / polling / batch 等摄入配置 |

---

### 6. 最小可读示例

假设 ODL：

```graphql
type Patient @objectType {
  id: ID! @primary
  nhsNumber: String @unique @indexed
  name: String!
  dateOfBirth: Date!
  status: PatientStatus!
  currentWard: Ward
    @link(type: "AdmittedTo", direction: OUTBOUND)
}

type Ward @objectType {
  id: ID! @primary
  name: String!
  specialty: String!
}

type AdmittedTo @linkType(
  from: "Patient"
  to: "Ward"
  cardinality: MANY_TO_ONE
) {
  id: ID! @primary
  admissionDate: DateTime!
}
```

对应：

```yaml
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig

metadata:
  name: hospital
  namespace: nhs.acute
  version: 1
  description: "Hospital relational data mapping"

schema:
  namespace: nhs.acute
  version: "1.4.0"

sources:

  hospital-db:
    kind: sql
    connector: jdbc
    dialect: postgresql

    connection:
      urlRef: "secret://hospital-db/jdbc-url"

models:

  Patient:

    source:
      kind: table
      schema: public
      name: patient

    identity:
      fields:
        - target: id
          source: patient_id
          transform:
            - prefix: "patient-"

    tenant:
      column: tenant_id

    system:
      deletedAt:
        source: deleted_at

    fields:

      nhsNumber:
        source: nhs_no

      name:
        source:
          expression: |
            concat(title, ' ', forename, ' ', surname)

      dateOfBirth:
        source: dob
        transform:
          - parseDate:
              format: "DD/MM/YYYY"

      status:
        source:
          expression: |
            CASE
              WHEN discharge_date IS NULL
                THEN 'ACTIVE'
              ELSE 'DISCHARGED'
            END

  Ward:

    source:
      kind: table
      schema: public
      name: ward

    identity:
      fields:
        - target: id
          source: ward_id
          transform:
            - prefix: "ward-"

    tenant:
      column: tenant_id

    system:
      deletedAt:
        source: deleted_at

    fields:

      name:
        source: name

      specialty:
        source: specialty


links:

  AdmittedTo:

    source:
      kind: table
      schema: public
      name: admission

    identity:
      fields:
        - target: id
          source: admission_id
          transform:
            - prefix: "admission-"

    from:
      object: Patient

      key:
        - target: id
          source: patient_id

          transform:
            - prefix: "patient-"

    to:
      object: Ward

      key:
        - target: id
          source: ward_id

          transform:
            - prefix: "ward-"

    tenant:
      column: tenant_id

    system:
      deletedAt:
        source: deleted_at

    fields:

      admissionDate:
        source: admission_datetime

runtime:

  cache:
    strategy: NONE

sync:

  mode: OVERLAY
  writeback: false
```

---

## Sources

### 7. `sources`

顶层键 MUST 为 `sources`（复数）。`source` 只用于 model/link 上的 relation descriptor，不得当作 datasource 名称字符串。

#### 7.1 SQL Source

```yaml
sources:

  hospital-db:
    kind: sql
    connector: jdbc
    dialect: postgresql

    connection:
      urlRef: secret://hospital-db/jdbc-url

    capabilities:
      temporal: false
      fullTextSearch: false
      transactions: false
```

`connection` MUST NOT contain plaintext credentials.

`secret://` MUST 仅在 runtime 解析，MUST NOT 写入 `obda.compiled.json`、explain、traces 或 introspection 输出。Overlay 查询 MUST 使用 least-privilege 只读数据库角色；若启用 writeback，MUST 使用独立写凭证。

`sources.<name>.capabilities` 为 MAY：声明该 source 是否支持 temporal / fullTextSearch / transactions。未声明视为 false。

Open Foundry v2.0 的安全要求明确禁止 plaintext secrets，并要求集成 Vault/Kubernetes Secrets。

---

### 8. Source Relation

OBDA Source SHOULD 支持：

```yaml
source:
  kind: table
  schema: public
  name: patient
```

```yaml
source:
  kind: view
  schema: public
  name: patient_current
```

以及：

```yaml
source:
  kind: sqlQuery
  query: |
    SELECT ...
```

这样一个 ODL ObjectType 不需要与单张物理表一一对应。

例如：

```text
Patient
   ↓
SQL View
```

或者：

```text
Patient
   ↓
SELECT
  ...
FROM patient
JOIN patient_status ...
```

---

## Models & Identity

### 9. Model

`models` 是整个 OBDA 规范最核心的部分。

一个 Model 对应一个 ODL `ObjectType`。

```yaml
models:

  Patient:
    source:
      ...

    identity:
      ...

    fields:
      ...

    tenant:
      ...

    system:
      ...
```

规则：

1. `models.X` 中的 `X` MUST 对应一个 ODL ObjectType。
2. 只有从外部 source 投影的 ObjectType 才 MUST 有 read mapping。Ontology Engine / SPI store 中的 native ObjectType MUST 在没有 mapping 的情况下可查询。
3. v1 OVERLAY：每个 ObjectType 在一份 active OBDA 文件中 MUST 恰好一个 read binding（`source` relation descriptor）。`bindings[]` 与跨 source 选择推迟到 phase 3 / MATERIALIZED Sync。
4. 多 Source 冲突处理属于 Sync Engine 的职责，不属于 ODL 语义。

原规范已经定义了 `SOURCE_PRIORITY`、`LAST_WRITE_WINS`、`MERGE`、`CUSTOM` 等冲突策略。

---

### 10. Identity Mapping

这是 OBDA 中最重要的 Mapping。

ODL 要求每一个 ObjectType 必须恰好一个 `@primary` 字段。

因此：

```yaml
identity:
  fields:
    - target: id
      source: patient_id
```

代表：

```text
SQL patient.patient_id
        ↓
ODL Patient.id
```

---

### 11. Composite Identity

虽然 ODL 对外只有一个 `id`，物理数据库允许 composite key：

```yaml
identity:

  fields:
    - target: id
      source:
        expression: |
          concat(hospital_id, ':', patient_id)
```

例如：

```text
hospital_id = 001
patient_id  = 123

↓

001:123
```

OBDA runtime MUST retain enough metadata to translate:

```text
ODL ID
```

back into:

```text
SQL identity predicate
```

---

### 12. Identity Transform 的限制

普通字段可以使用任意可执行 transform。

但是 Identity Transform 在 `OVERLAY` 查询模式下必须满足：

> **可逆或可编译为 SQL predicate。**

例如：

```yaml
transform:
  - prefix: patient-
```

很好：

```text
patient-123
    ↓
123
```

而：

```yaml
transform:
  - hash: sha256
```

则不适合直接反向查询：

```text
Patient.id = "..."
```

因为：

```text
hash(SQL column)
```

无法从输出 ID 反向恢复原值。

因此：

- v1 OVERLAY 的 Identity Transform MUST 可逆，并能编译为 SQL predicate。
- 不可逆 Identity Transform（如 `hash`）MUST NOT 用于 `getObject()` / `traverse()` pushdown。v1 不提供 lookup-strategy 例外。
- Compiler MUST reject unsupported identity mappings。

---

## Properties & Transforms

### 13. Property Mapping

最简单：

```yaml
fields:

  name:
    source: patient_name
```

对应：

```text
Patient.name
        ↓
patient.patient_name
```

---

### 14. Expression Mapping

允许：

```yaml
fields:

  name:
    source:
      expression: |
        concat(title, ' ', forename, ' ', surname)
```

或：

```yaml
fields:

  status:
    source:
      expression: |
        CASE
          WHEN discharge_date IS NULL
            THEN 'ACTIVE'
          ELSE 'DISCHARGED'
        END
```

这与 v2.0 已定义的 Sync Transform 思路一致，例如 `concat()`、`ifPresent()`、`coalesce()`、`map()`、`custom()` 等。

---

### 15. 推荐的 Source Expression 层级

Property Mapping 支持三种形式：

```yaml
source: column
```

```yaml
source:
  expression: SQL_EXPRESSION
```

```yaml
dateOfBirth:
  source: dob
  transform:
    - parseDate:
        format: "DD/MM/YYYY"
```

三者分别对应：

```text
Column Mapping
Expression Mapping
Transform Mapping
```

---

### 16. Transform Pipeline

Transform 可以组合：

```yaml
name:
  source: surname

  transform:
    - trim: {}
    - toUpper: {}
```

或者：

```yaml
dateOfBirth:
  source: dob

  transform:
    - parseDate:
        format: "DD/MM/YYYY"
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

---

### 17. 标准 Transform

第一版直接复用 v2.0 已定义的 Transform Vocabulary：

```text
concat()
prefix()
suffix()
parseDate()
parseDateTime()
toUpper()
toLower()
trim()
ifPresent()
coalesce()
map()
lookup()
hash()
custom()
```

这些 transform 已经属于 Open Foundry v2.0 Sync Engine 的既有定义。

OBDA Runtime SHOULD 尽量让同一套 transform 在：

```text
CDC
POLLING
BATCH
OVERLAY
```

中保持语义一致。

---

### 18. Enum Mapping

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

  source: status

  transform:
    - map:
        A: ACTIVE
        D: DISCHARGED
        X: DECEASED
```

---

### 19. Null Mapping

NULL SHOULD 保持 NULL：

```text
SQL NULL
   ↓
ODL null
```

除非显式：

```yaml
transform:
  - coalesce:
      value: UNKNOWN
```

OBDA MUST NOT 默默把 SQL NULL 转换成空字符串。

---

### 20. Type Mapping

OBDA Compiler MUST 根据 ODL 类型进行 SQL 类型验证。

基础类型：

| ODL | SQL 示例 |
|---|---|
| ID | VARCHAR/TEXT/UUID |
| String | VARCHAR/TEXT |
| Int | INTEGER |
| Float | DOUBLE PRECISION |
| Boolean | BOOLEAN |
| Date | DATE |
| DateTime | TIMESTAMP WITH TIME ZONE / equivalent |
| Duration | implementation-specific |
| JSON | JSON/JSONB |
| URI | VARCHAR/TEXT |
| GeoPoint | spatial type / JSON |

ODL v2.0 明确规定了这些基础 scalar。

---

## Links, Filters & Traversal

### 21. Link Mapping

LinkType 与 ObjectType 不同。

ODL：

```graphql
type AdmittedTo @linkType(
  from: "Patient"
  to: "Ward"
  cardinality: MANY_TO_ONE
)
```

Link 自己具有：

```text
_id
_fromType
_fromId
_toType
_toId
_version
_createdAt
_updatedAt
_deletedAt
```

这是 v2.0 明确定义的 `OntologyLink` 模型。

所以 `links` 必须是一等 Mapping。

---

### 22. Link Mapping 示例

```yaml
links:

  AdmittedTo:

    source:
      kind: table
      schema: public
      name: admission

    identity:
      fields:
        - target: id
          source: admission_id

    from:
      object: Patient
      key:
        - target: id
          source: patient_id

    to:
      object: Ward
      key:
        - target: id
          source: ward_id

    fields:

      admissionDate:
        source: admission_datetime
```

---

### 23. Link Physical Model

一个 LinkType 可以物理实现为：

#### 23.1 Association Table

```text
patient
admission
ward
```

最典型。

#### 23.2 Foreign Key

例如：

```text
patient.ward_id
```

直接表示：

```text
Patient ── AdmittedTo ──> Ward
```

FK-backed link 没有独立 association 主键。v1 MUST 为 23.2 声明确定性 identity：hash(`from-key`, `to-key`, `linkType`, 以及声明的 uniqueness scope)。未声明 identity 的 FK link MUST 被 compiler 拒绝。

#### 23.3 View

```text
patient_current_ward
```

#### 23.4 SQL Query

```sql
SELECT ...
FROM ...
JOIN ...
```

因此：

> **LinkType 不要求对应一张 SQL table。**

这是 OBDA 与 ORM Mapping 最大的区别之一。

---

### 24. Link Cardinality

ODL 定义：

```text
ONE_TO_ONE
ONE_TO_MANY
MANY_TO_ONE
MANY_TO_MANY
```

OBDA MUST NOT重新声明 Cardinality。

Compiler MUST 从：

```text
ODL @linkType
```

获取 Cardinality。

OBDA 只能负责：

```text
如何找到 from
如何找到 to
```

MANY_TO_ONE / ONE_TO_ONE link mapping MUST 声明 uniqueness / current-row selector（unique key、`WHERE current_flag`、或 `ORDER BY … DESC LIMIT 1`）。Compiler MUST 拒绝缺少 selector 的 association table，否则历史行会破坏 ODL cardinality（例如 `AdmittedTo` 映射到多次入院的 `admission` 表）。

---

### 25. Link Direction

例如：

```graphql
Patient.currentWard:
  @link(
    type: "AdmittedTo",
    direction: OUTBOUND
  )
```

以及：

```graphql
Ward.patients:
  @link(
    type: "AdmittedTo",
    direction: INBOUND
  )
```

OBDA 只定义：

```text
AdmittedTo.from
AdmittedTo.to
```

Traversal Planner 决定 SQL Join 方向。

---

### 26. Filter Mapping

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
Patient.nhsNumber = "943 476 5919"
```

↓

```sql
patient.nhs_no = $1
```

v1 OVERLAY 中，可作为 FilterExpression 目标的字段 MUST 是 column mapping 或可逆 transform。CASE / `source.expression` / `computed.kind: sql` 字段 MUST NOT 作为 filter 目标，直到 compiler 定义 predicate inversion。Planner MUST 在渲染 overlay SQL 之前丢弃或拒绝调用者无权读取的字段谓词（含 `@sensitive`）。

---

### 27. Filter 不应该直接进入 SQL 字符串

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

```text
LIKE '%' || $1 || '%'
```

而不是字符串拼接。

---

### 28. Traversal Mapping

SPI 已定义：

```ts
traverse(
  startId: string,
  path: TraversalPath,
  options?: TraversalOptions
)
```

以及：

```ts
interface TraversalStep {
  linkType: string;
  direction: 'inbound' | 'outbound';
  filter?: FilterExpression;
  maxDepth?: number;
}
```



OBDA 的职责是：

```text
TraversalPath
      ↓
Link Mapping
      ↓
JOIN graph
      ↓
SQL
```

---

### 29. Traversal 示例

调用：

```ts
traverse(
  "patient-123",
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
patient.patient_id

AdmittedTo
   ↓
admission.patient_id

AdmittedTo.to
   ↓
admission.ward_id

Ward.id
   ↓
ward.ward_id
```

最终：

```sql
SELECT w.*
FROM patient p
JOIN admission a
  ON a.patient_id = p.patient_id
JOIN ward w
  ON w.ward_id = a.ward_id
WHERE p.patient_id = $1
```

---

### 30. Multi-hop Traversal

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

---

### 31. `maxDepth`

ODL SPI 已规定：

```ts
maxDepth?: number
```

以及 provider capability：

```ts
maxTraversalDepth
```



OBDA Planner MUST 在 compile/plan 阶段检查：

```text requestedDepth <= provider.maxTraversalDepth
```

否则返回：

```text
TRAVERSAL_DEPTH_EXCEEDED
```

---

### 32. Variable-Length Traversal

建议 v1：

```text
MUST support fixed-depth traversal
MAY support maxDepth
```

例如：

```text
Patient
  └─ AdmittedTo → Ward
```

而：

```text
Patient
  └─* AdmittedTo → ...
```

即 variable-length path：

```text
1..N
```

建议 v2 才支持。

SQL backend 可通过：

```sql
WITH RECURSIVE
```

实现。

---

## Temporal, Tenancy & System Fields

### 33. Temporal Mapping

这是一个必须保留的能力。

SPI 已定义：

```ts
getObjectAtVersion()
getObjectAtTime()
```

并有：

```ts
QueryOptions {
  asOfVersion?
  asOfTime?
}
```



因此 OBDA 可以支持：

```yaml
temporal:

  validFrom:
    source: valid_from

  validTo:
    source: valid_to
```

Soft-delete 时间戳只映射在 `system.deletedAt`（见 §36），不放进 `temporal:`。默认查询已经 `WHERE deleted_at IS NULL`，把同一列同时当作 temporal tombstone 和业务 `status=DECEASED` 会让默认语义永远读不到 DECEASED。

---

### 34. Temporal Query

例如：

```text
Patient asOfTime(T)
```

编译为：

```sql
WHERE valid_from <= :T
AND (
  valid_to IS NULL
  OR valid_to > :T
)
```

---

### 35. Version Mapping

这里要非常谨慎：

v2.0 的 `_version` 是 **Ontology Object version**，不是数据库 row version。

因此不能简单：

```yaml
_version:
  source: version
```

就认为完成了 Temporal Semantics。

建议 v1：

```yaml
versioning:
  strategy: SOURCE_SEQUENCE
  sourceField: row_version
```

或：

```yaml
versioning:
  strategy: SNAPSHOT
```

由 Compiler 产生：

```text
Ontology Version Adapter
```

---

### 36. Soft Delete

ODL/SPI 明确要求：

```text
_deletedAt
```

默认查询排除 soft-deleted object。

OBDA：

```yaml
system:

  deletedAt:
    source: deleted_at
```

运行时默认：

```sql
WHERE deleted_at IS NULL
```

如果：

```ts
includeDeleted: true
```

则移除该 predicate。

---

### 37. Tenant Mapping

这是 OBDA 的强制要求之一。

v2.0 要求每个 Request 都有：

```ts
tenantId
```

SPI 必须 tenant scoped。

因此：

```yaml
tenant:

  column: tenant_id
```

运行时：

```sql
WHERE tenant_id = :tenantId
```

这个 predicate：

> **MUST 由 runtime 自动添加，不能由 LLM / Agent / Client 指定。**

v1 OVERLAY MUST 支持两种 tenant strategy：

- `COLUMN` — 源表确有 tenant 列时注入 `WHERE tenant_id = :tenantId`
- `CONSTANT` / `CONNECTION` — 将 Open Foundry `RequestContext.tenantId` 绑定到该 source；不要求物理 `tenant_id` 列（典型单租户 PAS）

每个 overlay model、link 与 `sqlQuery` source MUST 声明一种 strategy。Compiler MUST 拒绝无法把 tenant predicate 应用到最终 SQL 的 mapping。Overlay cache key MUST 包含 `tenantId`。

---

### 38. Tenant Source Isolation

如果源数据库本身已经按 tenant 隔离：

```yaml
tenant:
  strategy: DATABASE
```

则不需要 SQL predicate。

建议支持：

```text
COLUMN
DATABASE
SCHEMA
CONNECTION
```

但 v1 最先实现：

```text
COLUMN
CONSTANT
CONNECTION
```

---

### 39. System Fields

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



OBDA 可以定义：

```yaml
system:

  tenantId:
    source: tenant_id

  createdAt:
    source: created_at

  updatedAt:
    source: updated_at

  deletedAt:
    source: deleted_at
```

而：

```text
_type
```

和：

```text
_id
```

由 Model Identity 派生。

---

### 40. `createdBy / updatedBy`

如果来源系统有：

```text
created_by
updated_by
```

可以：

```yaml
system:

  createdBy:
    source: created_by

  updatedBy:
    source: updated_by
```

但它们仍然必须满足 ODL `@readonly` 语义。

---

## ODL Directives & Advanced Mapping

### 41. Computed Field

ODL 支持：

```graphql
@computed(
  fn: "countLinks",
  args: { type: "AdmittedTo" }
)
```

以及：

```text
LAZY
EAGER
TTL
```



OBDA SHOULD NOT 把所有 Computed Field 都强制物化。

支持：

```yaml
fields:

  currentOccupancy:

    computed:
      kind: sql
      expression: |
        ...
```

或者：

```yaml
fields:

  riskScore:
    computed:
      kind: runtime
      function: wardRiskScore
```

---

### 42. Computed Field Mapping 的优先级

推荐：

```text
ODL @computed
        │
        ▼
OBDA computed.sql
        │
        ├── 有 → SQL pushdown
        │
        └── 无 → Ontology Function
```

这样：

```text
SQL aggregation
```

和：

```text
Function runtime
```

都可以共存。

---

### 43. `@indexed`

ODL：

```graphql
name: String @indexed
```

定义的是：

> 该属性用于 structured/exact lookup。

OBDA 可以：

```yaml
indexes:
  - field: name
    source:
      index: idx_patient_name
```

但是：

> OBDA 不得改变 ODL 的 semantic indexing contract。

如果源系统没有对应 index，Compiler MAY warning。

---

### 44. `@searchable`

ODL 已把：

```text
@indexed
```

和：

```text
@searchable
```

明确区分。

OBDA 可以声明：

```yaml
search:

  name:
    expression:
      sql: |
        to_tsvector('english', patient_name)

    index: idx_patient_name_fts
```

但如果 source 不支持：

```text
supportsFullTextSearch
```

则 Planner 必须根据 capability 决定是否暴露 search API。v2.0 已明确要求 capability-driven API surface。

---

### 45. `@unique`

ODL：

```graphql
nhsNumber: String @unique
```

对于 `OVERLAY`：

```text
@unique
```

是 Semantic Contract。

Compiler SHOULD 检查：

```sql
COUNT(DISTINCT nhs_number)
```

但：

> OBDA 不应该自动修改数据库 schema。

因为底层数据库可能是 external system。

---

### 46. `@constraint`

`@constraint` 属于 ODL 业务语义，不能复制到 OBDA。

例如：

```graphql
capacity: Int!
  @constraint(expr: "value > 0")
```

OBDA 不需要：

```yaml
constraint:
  sql: "capacity > 0"
```

但是：

> Query-time optimization MAY push down equivalent predicates。

例如：

```text
CEL Constraint
    ↓
Query Planner
    ↓
SQL Predicate
```

这属于 compiler optimization，而非 Mapping Semantic。

---

### 47. `@sensitive`

`@sensitive` 也不属于 OBDA Mapping。

ODL 定义：

```text
Patient.name @sensitive
```

OBDA 只提供：

```text
source column
```

Security Layer 仍然必须在 query pipeline 里执行 field redaction。

v2.0 明确规定 Overlay 对象也必须经过与 native object 相同的 security pipeline。

---

### 48. Permission 不放进 `obda.yaml`

强烈建议：

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

v2.0 已定义 ReBAC/OpenFGA 模型，并规定 schema/object/action/field 等不同权限层级。

OBDA 负责：

```text
Where is the data?
```

Security 负责：

```text
Who can see it?
```

---

### 49. `@terminology`

例如：

```graphql
code: CodeableConcept!
  @terminology(system: "http://snomed.info/sct")
```

OBDA：

```yaml
fields:

  code:
    source:
      expression: |
        json_build_object(
          'system', code_system,
          'code', code,
          'display', code_display
        )
```

但 terminology binding 仍由 ODL 控制。

---

### 50. Interface Mapping

例如：

```graphql
interface Identifiable {
  id: ID! @primary
}
```

Interfaces 不需要独立 OBDA mapping。

规则：

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

---

### 51. Link Property Mapping

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
        source: admission_datetime

      expectedDischarge:
        source: expected_discharge
```

这是必须支持的，因为 v2.0 明确把 Link 当作带自己属性和自己 ID 的一等实体。

---

### 52. Many-to-Many

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

    source:
      kind: table
      name: doctor_ward

    from:
      object: Doctor
      key:
        - target: id
          source: doctor_id

    to:
      object: Ward
      key:
        - target: id
          source: ward_id
```

---

### 53. Source JOIN Mapping

复杂 Link 可以：

```yaml
source:

  kind: sqlQuery

  query: |
    SELECT
      a.id,
      a.patient_id,
      w.ward_id,
      a.admission_date
    FROM admission a
    JOIN ward w
      ON w.ward_id = a.ward_id
```

于是：

```text
LinkMapping
```

直接建立在一个 logical relation 上。

---

### 54. SQL Source 的安全限制

Datasource `sources.*.kind` 仍为 `sql`（JDBC）。Relation 的 `kind` MUST 为 `table` | `view` | `sqlQuery`，字段名为 `query`，不得使用 `source.sql`。

`sqlQuery` 与 field `expression:`、`computed.kind: sql`：

- MUST 是常量只读 SELECT，无 bind parameters、无多语句、无 DDL/DML
- MUST 只引用 mapping 中已声明的 relations / columns / 函数 allowlist
- MUST NOT 包含用户输入或 runtime 值拼接
- MUST NOT 跨未声明 schema
- Mapping 文件是 privileged artifact，激活前 MUST 经人工审查

禁止：

```yaml
query: |
  SELECT ...
  WHERE id = '${userInput}'
```

也禁止 mapping SQL 中的 `:status` 一类 runtime bind；过滤走 FilterExpression AST（§26–§27）。

---

## Runtime & Overlay

### 55. Runtime Mode

Overlay vs materialized MUST 使用 v2 已有的 `sync.mode`，不要再引入并行的 `runtime.mode`：

```yaml
sync:
  mode: OVERLAY   # OVERLAY | CDC | POLLING | BATCH
```

`runtime` 只承载 cache 与 query limits：

```yaml
runtime:
  cache:
    strategy: NONE
    ttl: PT5M
  query:
    maxTraversalDepth: 8
    timeout: PT2S
```

`runtime.query.maxTraversalDepth` / `runtime.query.timeout` 为 MAY。

#### OVERLAY

数据保留在 source。v2.0 已明确规定 Overlay 是 read-through，不将对象复制到 ontology store。

OBDA 是 Sync Engine overlay/mapping compiler，**不是**与 TypeDB / Neo4j / PostgreSQL+AGE 对等的第四个 StorageProvider。Ontology Engine 把 OVERLAY ObjectType 路由到 connector 读路径；`applySchema`、Action 事务、lineage 与 OVERLAY→CDC 物化继续走已有 SPI store。

#### MATERIALIZED

由 `sync.mode: CDC` / `POLLING` / `BATCH` 驱动，写入 ontology store。参与 Action 的 ObjectType MUST 使用 materialized/CDC binding；纯 OVERLAY 对象保持只读（除非 `writeback: true`）。

---

### 56. 推荐不要单独复制一套 `MATERIALIZED`

Open Foundry v2.0 已经定义：

```text
CDC
POLLING
BATCH
OVERLAY
```

所以建议只写 v2 字段：

```yaml
sync:
  mode: OVERLAY
```

或者：

```yaml
sync:
  mode: CDC
```

不要同时写 `runtime.mode` 与 `sync.mode`。

这样可以保持 v2.0 compatibility。

---

### 57. Overlay Cache

v2.0 已定义：

```yaml
cacheStrategy: TTL
cacheTTL: "PT5M"
```



建议新 OBDA 写法：

```yaml
runtime:
  cache:
    strategy: NONE
    ttl: PT5M
```

v1 默认 `strategy: NONE`。若使用 TTL，cache entry MUST 以 `(tenantId, actorId, purpose, objectType, objectId, mappingVersion)` 为键，或只缓存 pre-security 物理行并在 hit 时重跑 ReBAC / consent / redaction。Cache MUST 在 `openfoundry.consent.revoked` 与 permission-tuple 变更时立即失效，不得只靠 TTL。

支持：

```text
NONE
TTL
```

未来 MAY：

```text
LRU
VERSION
CDC_INVALIDATED
```

---

### 58. Writeback

Overlay 默认：

```yaml
writeback: false
```

这是 v2.0 明确规定的。

只有：

```yaml
writeback: true
```

并且 Connector 实现：

```ts
write?(record)
```

时才允许写回。

但我建议：

> **OBDA Mapping 不直接定义任意 mutation。**

写操作仍然：

```text
Action
 ↓
Action Framework
 ↓
writeback
```

而不是：

```text
GraphQL mutation
 ↓
SQL UPDATE
```

Writeback SQL MUST 应用 tenant 与 ReBAC predicates，只写 Action authz/consent 之后映射过的可变列，并使用与 overlay 只读角色分离的写凭证。`writeback: true` 不是 Security Layer 的替代。

这与 v2.0 “所有 mutation 必须进入 Action Pipeline”的设计保持一致。

---

### 59. Provenance

v2.0 的 `FieldProvenance` 已明确：

```text
connector
sourceSystem
syncRunId
mappingVersion
sourcePointer
```



因此 OBDA MUST 提供可定位 source 的 metadata：

```yaml
provenance:

  sourceSystem: PAS

  sourcePointer:
    table: patient
    key:
      - patient_id

  mappingVersion: 1
```

对于 Overlay：

```text
kind = OVERLAY
```

并报告：

```text
connector
sourceSystem
sourcePointer
```

这与 v2.0 Overlay lineage 定义一致。

---

### 60. Mapping Version

Mapping 是独立 artifact，所以：

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

---

### 61. Compatibility Matrix

Runtime 必须检查：

```text
ODL Schema Version
          +
OBDA Mapping Version
          +
Source Schema Version
```

形成：

```text
Semantic Schema
Physical Mapping
Physical Source
```

三方一致。

---

## Validation & Compiler

### 62. Compiler Validation

`obda validate` 至少需要执行：

```text
1. YAML Schema Validation
2. ODL Reference Validation
3. ObjectType Mapping Validation
4. Property Validation
5. Identity Validation
6. Link Validation
7. Cardinality Validation
8. SQL Expression Validation
9. Type Compatibility
10. Source Capability Validation
11. Tenant Mapping Validation
12. Temporal Mapping Validation
13. Security-related metadata validation
```

---

### 63. Object Validation Rules

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
id 是否 mapped
```

如果 ODL 中没有：

```text
Patient
```

则：

```text
OBDA_UNKNOWN_OBJECT_TYPE
```

---

### 64. Field Validation

例如：

```yaml
fields:
  name:
    source: patient_name
```

Compiler MUST 检查：

```text
Patient.name
```

是否存在。

如果 ODL 被修改成：

```text
fullName
```

而 Mapping 没改：

```text
OBDA_UNKNOWN_PROPERTY
```

---

### 65. Link Validation

例如：

```yaml
links:
  AdmittedTo:
```

Compiler MUST 检查：

```text
AdmittedTo
```

是否：

```graphql
@linkType
```

并检查：

```text
from.object == @linkType.from
to.object   == @linkType.to
```

---

### 66. Direction Validation

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

必须使用：

```text
INBOUND
```

---

### 67. Physical Join Validation

Compiler SHOULD 检查：

```text
from.key.target
```

与：

```text
models[from.object].identity
```

兼容。

例如：

```yaml
Patient.id
```

必须能被解析到：

```text
patient.patient_id
```

否则：

```text
OBDA_INVALID_LINK_KEY
```

---

### 68. SQL Type Validation

例如：

```text
ODL:
Patient.dateOfBirth : Date

SQL:
patient_name : VARCHAR
```

则：

```text
OBDA_TYPE_MISMATCH
```

---

## Query Pipeline

### 69. Query Pushdown Contract

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
Node.js
      ↓
Filter
      ↓
Projection
```

---

### 70. Query Compilation Pipeline

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

---

### 71. Query IR

建议：

```ts
interface SemanticQueryPlan {
  root: ObjectRef;

  joins: SemanticJoin[];

  predicates: Predicate[];

  projections: Projection[];

  orderBy: OrderBy[];

  limit?: number;
  offset?: number;

  temporal?: TemporalPredicate;

  tenant: TenantScope;
}
```

OBDA Compiler 只负责：

```text
Semantic Query
        ↓
Relational Query
```

---

### 72. `traverse()` 的最终执行模型

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
  Relational Algebra
          │
          ▼
        SQL
```

因此：

> `traverse()` 是 Semantic Graph API，而不是 Graph Database API。

---

### 73. SQL Backend 不需要 Graph Database

Overlay SQL 可以把固定深度 `traverse()` 编译为 JOIN，**但这不是第四个 StorageProvider**。

```text
Open Foundry
    │
    ├── TypeDB
    ├── Neo4j
    └── PostgreSQL + AGE     ← v1 本体存储 / ReBAC 物化 / Action 事务
            │
            └── OVERLAY read adapter
                  PostgreSQL / MySQL / DuckDB via OBDA
```

v1 storage provider 仍是 PostgreSQL + Apache AGE。OBDA overlay 是该 provider 前面的 read-through adapter，不能替代 AGE 上的 native objects、link identity 与 ReBAC 物化。

---

### 74. Schema Cache

建议 OBDA 编译后的运行时缓存：

```text
SemanticSchemaCache
```

结构类似：

```ts
interface SemanticSchemaCache {
  namespace: string;
  odlVersion: string;
  mappingVersion: number;

  objects: Map<string, ObjectMapping>;

  links: Map<string, LinkMapping>;

  sources: Map<string, SourceMapping>;
}
```

它对应 v2.0 所要求的 schema registry / runtime compiled snapshot 思路。v2.0 已明确 Git-backed schema 为主、数据库 registry 为 runtime cache。

---

### 75. Compiled Artifact

建议 `obda compile` 生成：

```text
.obda.yaml
       ↓
   compiler
       ↓
obda.compiled.json
```

内容：

```json
{
  "namespace": "nhs.acute",
  "schemaVersion": "1.4.0",
  "mappingVersion": 7,
  "models": {},
  "links": {},
  "sources": {}
}
```

Production runtime 直接加载：

```text
compiled artifact
```

而不是每次解析 YAML。

---

### 76. `explain`

应该提供：

```bash
obda explain \
  --object Patient \
  --id patient-123
```

输出：

```text
ODL:
  Patient.id

OBDA:
  public.patient.patient_id

Transform:
  remove prefix "patient-"

SQL:
  SELECT ...
  FROM public.patient
  WHERE patient_id = $1
```

`explain` / traces / provenance MUST 对 `@sensitive` bind values 与列 payload 做 redaction。对 live identifiers 执行 explain MUST 经过与 `getObject` 相同的 ReBAC/consent 检查，并审计该 CLI/API 调用。Compiled artifact、explain 与 spans MUST NOT 包含 `secret://` 解析后的 JDBC URL。

---

### 77. `explain traverse`

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

这是开发 Agent Query DSL 时非常有用的能力。

---

### 78. `validate`

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
✓ SQL expressions valid
```

---

### 79. Source Introspection

因为 v2.0 Connector 已定义：

```ts
discoverSchema(): Promise<SourceSchema>
```



所以可以：

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

`obda introspect` / `obda generate` / `obda explain` 是 privileged operator 操作：MUST 认证授权、默认非生产、并审计。生成结果 MUST 保持 candidate-only，直到人工审查标记 `@sensitive` 与 tenant isolation。

---

### 80. Auto-Mapping

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

而不是直接修改 production mapping。

v1 作者路径 SHOULD 先于 Explain/IR 提供 `obda generate`（从 source introspection 生成 candidate mapping，永不静默应用到 production）。

---

### 81. Mapping Diff

类似 ODL schema diff：

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

---

### 82. Mapping Compatibility

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
修改 SQL expression
→ COMPATIBLE / BREAKING
```

取决于是否改变 semantics。

---

### 83. Mapping Version Rollback

类似 ODL schema：

```bash
obda rollback \
  --from-version 8 \
  --to-version 7
```

生成：

```text
OBDA mapping v9
```

其 effective mapping 等价于 v7。

这样与 v2.0 的 forward-only schema rollback 设计一致。

---

## Multi-source

### 84. Multiple Source Mapping

v1 OVERLAY 禁止同一 ObjectType 多个 Overlay bindings。一份 active OBDA 文件里，一个 ObjectType 的 read path 恰好来自一个 source relation。

`bindings[]` 仅允许出现在 MATERIALIZED mappings，并使用 Sync `conflictResolution`。跨 source JOIN 与 cost-based source selection 属于 v2 / phase 3。

错误（v1）：

```yaml
models:
  Patient:
    bindings:
      - fromSource: pas
        source:
          kind: table
          name: patient
      - fromSource: ehr
        source:
          kind: table
          name: patient_current
```

`bindings[].fromSource` 引用顶层 `sources` 的键；不要把 `source:` 写成 datasource 名称字符串。

---

### 85. Source Priority

Materialized Sync 可以：

```yaml
sync:

  conflictResolution: SOURCE_PRIORITY

  sourcePriority:
    - EHR
    - PAS
    - FHIR
```

这沿用 v2.0 已定义的 conflict resolution。

`OVERLAY` 则不应该自动选择“谁更新”，而应该由 Query Planner 按 Binding Capability 与 policy 决定。

---

### 86. Cross-source Relationship

未来可以：

```text
Customer
   │
   │ orders
   ▼
Order
```

其中：

```text
Customer → PostgreSQL
Order   → ClickHouse
```

OBDA：

```yaml
links:

  Orders:
    fromSource: crm
    toSource: warehouse
```

但是：

> Cross-source JOIN 建议作为 v2 capability，而不是 v1 必须能力。

---

### 87. Federation

跨 Open Foundry instance 不属于 OBDA。

边界：

```text
OBDA
SQL / external source

Federation
Open Foundry instance / Open Foundry instance
```

v2.0 已明确 federation 是 instance-to-instance、tenant-scoped。

---

## Security

### 88. Security Boundary

完整查询执行链：

```text
Agent
  ↓
API
  ↓
Security
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

否则会绕过 v2.0 要求的 Security Layer。

---

### 89. Permission Predicate 注入

例如：

```text
User Alice
   ↓
ReBAC
   ↓
allowed Patient IDs
```

最终：

```sql
WHERE
    patient.tenant_id = :tenant
AND patient.patient_id IN (...)
```

或者：

```sql
JOIN authorization_scope ...
```

但这个 predicate：

> 不属于 OBDA YAML。

它由 Security Planner 注入。

Security Planner MUST 把 object-level ReBAC（以及 tenant）谓词注入每一条 overlay SQL。当谓词无法 pushdown（表达式 mapping、`sqlQuery`、过大 ID 集）时，查询 MUST fail-closed 被拒绝，不得先 SELECT 再在内存中求交。

---

### 90. Query Execution Order

推荐严格：

```text
1. Authentication
2. Authorization
3. Consent
4. Semantic Query Validation
5. OBDA Resolution
6. Query Planning
7. SQL Execution
8. Field Redaction
9. Provenance
10. Response
```

`queryObjects` 在 SQL 返回行之前不知道 subject IDs，因此 overlay consent 是 fail-closed 的 post-ReBAC、pre-response 过滤，并进入 cache key。Overlay cache MUST 在 `openfoundry.consent.revoked` 时立即 purge，而不是等待 TTL。Field redaction（步骤 8）不能保护一份从 raw SQL 填充、且未按 principal 分键的 cache。

这一点与 v2.0 Action Pipeline 中“Authorize / Consent 先于读取业务状态”的安全原则一致。

---

### 91. Provenance of Virtual Query

对于 Overlay：

```text
Patient.name
```

应能得到：

```json
{
  "kind": "OVERLAY",
  "connector": "jdbc",
  "sourceSystem": "PAS",
  "sourcePointer": {
    "table": "patient",
    "column": "patient_name",
    "key": {
      "patient_id": "123"
    }
  }
}
```

这与 v2.0 的 Overlay lineage 定义保持一致。

---

## Observability

### 92. Observability

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

这样可以与 v2.0 现有的：

```text
openfoundry.engine.traverse
openfoundry.sync.map
```

观察体系衔接。

---

### 93. Query Cost

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

这对于 v2.0 的 GraphQL complexity / governance 要求很重要。

---

### 94. Capability Detection

OBDA Backend SHOULD 映射到：

```ts
StorageCapabilities
```

例如：

```text
supportsGraphTraversal = true
supportsTemporalQueries = true
supportsFullTextSearch = false
```



如果：

```text
supportsFullTextSearch = false
```

那么：

```text
searchFoos
```

可以不生成。

这与 v2.0 Query API 的 capability-driven design 一致。

---

## v1 / v2 Scope

### 95. v1 必须支持的 Source Kind

建议：

```text
TABLE
VIEW
SQLQUERY
```

---

### 96. v1 必须支持的 Mapping Kind

```text
Object Mapping
Property Mapping
Identity Mapping
Link Mapping
Link Property Mapping
```

---

### 97. v1 必须支持的 Query Capability

```text
getObject
queryObjects
getLinks
traverse
```

---

### 98. v1 必须支持的 Semantic Features

```text
Identity
Property
Filter
AND
OR
NOT
Pagination
Order By
Tenant
Soft Delete
```

这些都直接对应 v2.0 SPI 已定义能力。

---

### 99. v1 SHOULD 支持

```text
Date / DateTime
Enum mapping
SQL expression
Computed SQL
Overlay cache
Temporal asOfTime
Provenance
Explain
Introspection
```

---

### 100. v2 再支持

```text
Variable-length traversal
Recursive CTE
Cross-source joins
Distributed query
Federated source
Graph shortest path
Cost-based source selection
Materialized view routing
```

---

## Examples & Conclusion

### 101. Complete Recommended YAML

下面给出一个更完整的 production-style 示例：

```yaml
apiVersion: openfoundry.io/obda/v1
kind: OBDAConfig

metadata:
  name: nhs-pas
  namespace: nhs.acute
  version: 12
  description: "PAS relational data projection for the NHS Acute ontology"

schema:
  namespace: nhs.acute
  version: "1.4.0"

sources:

  pas:
    kind: sql
    connector: jdbc
    dialect: postgresql

    connection:
      urlRef: "secret://pas/jdbc-url"

    capabilities:
      temporal: true
      fullTextSearch: true
      transactions: true

models:

  Patient:

    source:
      kind: table
      schema: public
      name: patient

    identity:

      fields:

        - target: id

          source: patient_id

          transform:
            - prefix: "patient-"

    tenant:
      column: tenant_id

    system:

      createdAt:
        source: created_at

      updatedAt:
        source: updated_at

      deletedAt:
        source: deleted_at

    fields:

      nhsNumber:
        source: nhs_no

      name:
        source:
          expression: |
            trim(
              concat(
                title,
                ' ',
                forename,
                ' ',
                surname
              )
            )

      dateOfBirth:

        source: dob

        transform:
          - parseDate:
              format: "DD/MM/YYYY"

      status:

        source:
          expression: |
            CASE
              WHEN discharge_date IS NULL
                THEN 'ACTIVE'
              ELSE 'DISCHARGED'
            END

      clinicalNotes:
        source: clinical_notes

  Ward:

    source:
      kind: table
      schema: public
      name: ward

    identity:

      fields:

        - target: id
          source: ward_id

          transform:
            - prefix: "ward-"

    tenant:
      column: tenant_id

    fields:

      name:
        source: name

      specialty:
        source: specialty

      capacity:
        source: capacity

links:

  AdmittedTo:

    source:
      kind: table
      schema: public
      name: admission

    identity:

      fields:

        - target: id
          source: admission_id

          transform:
            - prefix: "admission-"

    tenant:
      column: tenant_id

    from:

      object: Patient

      key:

        - target: id

          source: patient_id

          transform:
            - prefix: "patient-"

    to:

      object: Ward

      key:

        - target: id

          source: ward_id

          transform:
            - prefix: "ward-"

    fields:

      admissionDate:
        source: admission_datetime

      expectedDischarge:
        source: expected_discharge

runtime:

  mode: OVERLAY

  cache:
    strategy: TTL
    ttl: PT5M

  query:

    maxTraversalDepth: 8
    timeout: PT2S

sync:

  mode: OVERLAY

  writeback: false
```

---

### 102. 推荐的 JSON Schema

`obda.yaml` 必须提供 JSON Schema：

```text
obda-config.schema.json
```

这样：

```text
VS Code
Cursor
JetBrains
CI
CLI
```

都可以直接 validation。

---

### 103. 推荐目录结构

建议把 v2.0 当前的 datasource mapping 演进成：

```text
openfoundry/
├── domain-packs/
│   └── nhs-acute/
│
│       ├── schema/
│       │   ├── patient.odl
│       │   ├── ward.odl
│       │   └── links.odl
│       │
│       ├── obda/
│       │   ├── pas.obda.yaml
│       │   ├── ehr.obda.yaml
│       │   └── bed.obda.yaml
│       │
│       ├── actions/
│       ├── permissions/
│       ├── functions/
│       └── quality/
```

---

### 104. 与 v2.0 当前 `datasource mapping` 的关系

原规范目前已经有：

```yaml
datasource:
connector:
connection:

mapping:
  objectType:
  primaryKey:
  properties:
  links:

sync:
  mode:
```

这是一个很好的 MVP mapping。



建议不要废弃，而是演进：

```text
datasource mapping
        ↓
OBDA Mapping v1
```

兼容规则：

```text
mapping.objectType
        ↓
models.<ObjectType>

mapping.primaryKey
        ↓
models.<ObjectType>.identity

mapping.properties
        ↓
models.<ObjectType>.fields

mapping.links
        ↓
links.<LinkType>

sync
        ↓
sync
```

---

### 105. 最终语义模型

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
   PostgreSQL     MySQL       APIs/FHIR
```

---

### 106. 更重要的关系：TBox / OBDA / ABox

这个设计之后可以非常清晰：

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
├── patient.patient_id
├── ward.ward_id
├── admission.patient_id
└── admission.ward_id
│
▼
ABox
│
│ actual rows
│
├── patient(123,...)
├── ward(7,...)
└── admission(...)
```

因此：

> **ODL 是语义世界，OBDA 是世界与物理数据之间的桥。**

---

### 107. Query 的最终链路

这样，之前讨论的 `traverse()` 就完整闭环：

```text
Agent
  │
  ▼
Security (AuthN / AuthZ / Consent)
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
Relational Algebra
  │
  ▼
SQL
  │
  ▼
PostgreSQL
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

---

### 108. 最终推荐的 Open Foundry 四层语义架构

我会正式把它定义成：

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
│     AuthN / AuthZ / Consent   │
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
│ SQL / Graph / API / FHIR      │
└───────────────────────────────┘
```

---

### 109. 一个非常重要的边界

我建议明确规定：

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

千万不要演化成：

```text
ODL
+
OBDA
+
SQL
```

全部混在一个文件里。

---

### 110. 与 Hasura 思路的最终关系

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

Hasura 只作为 prior-art 比较（Source / Model / Relationship / Schema Cache / Query Planning），不是产品别名。工件名称保持 **OBDA Mapping**（`*.obda.yaml`，`kind: OBDAConfig`）。「Semantic Data Mapping Metadata」只是 OBDA Mapping 的一句 gloss，不是第二正式名。

---

### 111. 推荐 v1 实现边界

第一阶段只需要完成：

```text
✓ SQL Source
✓ Table
✓ View
✓ Object Mapping
✓ Identity Mapping
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
✓ OVERLAY
✓ Explain
✓ Validate
✓ Schema Cache
✓ obda generate (candidate only)
```

这已经足以证明整个架构。

然后第二阶段：

```text
+ SQL Query Source
+ Temporal
+ Computed SQL
+ Search
+ Writeback
+ Materialized Sync
```

第三阶段：

```text
+ Cross-source Join
+ Recursive Traversal
+ Federated Semantic Query
+ Cost-based Planner
```

---

### 112. 结论

结合 v2.0，我认为最好的调整不是简单新增：

```text
xxx.obda.yaml
```

而是把它正式定义为：

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
SQL / Graph / API
```

其中最重要的三个设计决定是：

**第一：`models` 对应 ODL `ObjectType`，`links` 对应 ODL `LinkType`。**

**第二：Identity Mapping 是核心，不是普通 Column Mapping。** 因为 `getObject()`、`getLinks()`、`traverse()` 都依赖 ODL ID → 物理 key 的可执行映射。

**第三：`OVERLAY` 是 v1 的第一实现模式，但是 Sync Engine 的 read-through on-ramp，不是把 PostgreSQL 变成第四个 Semantic Backend。** 这与 v2.0 Overlay 语义一致；Action 事务、schema registry 与 ReBAC 物化仍走 PostgreSQL+AGE SPI。参与 Action 的 ObjectType 需要 materialized/CDC binding。

从工程落地角度，我会把下一步定成这四个产物：

```text
obda-config.schema.json
        +
obda.yaml example
        +
OBDA Mapping IR
        +
PostgreSQL OBDA Compiler
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

---

## Deferred / Open Questions

### From 2026-08-14 review

- **New language duplicates v2 mapping** — 1. 概述 / 104 (P0, scope-guardian, product-lens, confidence 100)

  Pack authors would have to learn and maintain a second mapping format while v2 Domain Packs already ship `datasources/*.yaml` for the same ODL-to-source job. Shipping `*.obda.yaml` as a parallel language splits the pack contract and doubles compiler surface. Related: Hasura as product identity vs YAML authors; whether Ontop or view-only mapping was evaluated; whether v1 should be dialect-declared and expression-free.

- **Spec exceeds mapping-language goal** — Purpose / 3 / 69–77 / 112 (P0, scope-guardian, adversarial, confidence 100)

  Implementers will treat a Query Planner, Semantic Query IR, optimizer, explain CLI, and PostgreSQL compiler as v1 mapping work. The stated goal is a physical mapping between ODL types and sources. Publishing the full 112-section contract before one Overlay getObject path runs may freeze the wrong IR.

- **v1 MUST/SHOULD/phase lists conflict** — 8, 33, 59, 62, 95–100, 111 (P0, coherence, product-lens, scope-guardian, confidence 100)

  Implementers cannot tell what v1 must ship. §§95–100 treat SQL query sources as MUST while §111 puts them in phase 2; Temporal / Provenance / Explain flip between MUST, SHOULD, and phase 2. Authoritative inventory is either §§95–100 or §111 — the review did not pick one.

- **Overlay cannot populate ReBAC graph** — 55 / 89 (P0, adversarial, confidence 100)

  OVERLAY never writes objects or links into the ontology store, so OpenFGA ListObjects has no admitted_to tuples. The spec still requires Security before SQL and injects allowed IDs as WHERE IN. Overlay-first therefore either returns nothing (fail closed) or skips graph-derived checks. Needs an explicit ReBAC tuple-projection contract.

- **Mapping-only premise already falsified** — 2 / 14 / 41 / 44 (P1, adversarial, confidence 75)

  Recommended examples encode Patient.status and occupancy as CASE / computed.sql inside obda.yaml, while the spec says OBDA MUST NOT restate ODL semantics. Either drop the purity claim or ban CASE/computed.sql from v1.

- **IN-list ReBAC pushdown will not scale** — 69 / 89 (P1, adversarial, confidence 75)

  A trust-wide ListObjects result of 10^5–10^6 IDs cannot be injected as `patient_id IN (...)`. The JOIN authorization_scope sketch has no schema or writer. List queries need a temp-table / COPY / semi-join contract and a named error for oversized ID sets.
  