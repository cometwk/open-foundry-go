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

## 1. Product

OBDA Mapping 是 ODL `ObjectType` / `LinkType` 到物理关系的可执行对应。ODL 仍是语义源。

v3 产品：

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

---

## 2. Non-goals

- `identity.strategy: sidecar` / `system.strategy: sidecar`
- Provider 创建或读写 `of_*`
- `ApplySchema` `ALTER` 已有业务表
- 用「已生成 DDL」跳过存在性检查
- ReBAC、跨库 JOIN、mapping 内任意 SQL
- 本轮持久化 history / Bulk 幂等 / 改 Engine / 改 memory

---

## 3. Architecture

```text
sqliteobda → runtime/obda → dialect.Dialect ← dialect/sqlite
```

| 包 | 职责 | MUST NOT |
|---|---|---|
| `runtime/obda` | parse / validate / compile / planner / `EncodeDirect` | sidecar 策略；发射方言 SQL 文本 |
| `runtime/obda/sqlast` | 封闭 AST，含 `Join` | 拼接运行时字面量 |
| `runtime/obda/dialect/sqlite` | quote、Render、introspect、**mapped-table** DDL 辅助、Classify、NormalizeValue | 生成 `of_*` |
| `runtime/storage/sqliteobda` | Open、可选 init、ApplySchema 检查、CRUD、JOIN | 查询 `of_*`；把 DSN 写入 YAML/error |

`Compiled` 不可变。`pin()` 钉住进程内激活版本。未激活（含重启后未再 ApplySchema）时，除 `HealthCheck` / `Capabilities` 外 → `ErrMappingNotActive`。

构造不变：

```text
sqliteobda.Open(db, mappingBytes, Options{DSNRefs})
```

---

## 4. Mapping document

Parser 仍只认本节字段。`runtime` / `sync` / `sqlQuery` / 明文 `dsn` `password` `uri` `url` `token` `secret` `user` 非法。

### 4.1 Top-level

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
models: { ... }
links: { ... }
```

`catalog` 仅空或 `main`。可写绑定同一 SQLite 文件。

### 4.2 Model / Link

v3 **只允许**：

```yaml
identity:
  strategy: direct          # sidecar → ErrInvalidMapping
  columns: [id]
  insert: generated         # provider 写入 EncodeDirect(type, UUIDv7)
tenant:
  strategy: column          # column | constant；connection 拒绝
  column: tenant_id
system:
  strategy: native          # sidecar → ErrInvalidMapping
  omit: []                  # 可选：version, createdAt, updatedAt, deletedAt
```

`omit` 的 YAML 键名由实现锁定；语义见 §7。未列出的系统列必须出现在表上。

Link：

```yaml
from:
  object: Patient
  columns: [from_id]        # 存 Patient 的 engine id（即 patient.id）
to:
  object: Ward
  columns: [to_id]
```

规则：

- `readWrite` + `view` → `ErrInvalidMapping`
- identity 列必须是 payload `fields` 的 `column`，除非 `insert: generated`
- 禁止把逻辑字段映射到 tenant 列
- projection 仍丢掉 ODL Primary：可写 PK 用 `insert: generated` 或非 Primary 字段

### 4.3 Transforms

可写：空、`prefix` `suffix` `trim` `toUpper` `toLower` `map` `parseDate` `parseDateTime`  
只读：`coalesce`  
`hash` / 任意 SQL → 非法

### 4.4 Canonical example

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

## 5. Identity

`EncodeDirect(typ, keys)`：

```text
base64url( JSON{"t": "<type>", "k": ["<key0>", ...]} )   # Raw, no padding
```

v3 中 identity 列 **存储该字符串本身**。`GetObject(type, id)`：`DecodeDirect(id)` 校验 `t == type`，然后 `WHERE id = ?` 绑定原始 id 字符串。不必再经 meta 表翻译。

复合键：`k` 为有序分量。禁止 `type + ":" + key`。

解码失败、类型不匹配 → `ErrObjectNotFound` / `ErrLinkNotFound`。

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

---

## 7. System fields

返回对象：`_tenantId` `_type` `_id`；若未 omit：`_version` `_createdAt` `_updatedAt`；软删时 `_deletedAt`。  
返回 link 另加 `_fromType` `_fromId` `_toType` `_toId`。

| omit | 行为 |
|---|---|
| `deletedAt` | 无软删；soft `DeleteObject` → `ErrUnsupportedCapability`；列表不加删除谓词 |
| `version` | 无 CAS；非空 `expectedVersion` → `ErrUnsupportedCapability`；返回 `_version = 0` |
| `createdAt` / `updatedAt` | 不写这些列；返回中可缺对应字段 |
| 未 omit | 列必须在表上；OCC / 软删按该列执行 |

identity 与 tenant **不可 omit**。

Hard delete：删对象行，并同事务删以其 id 为 `from_id`/`to_id` 的 link 行。无 history 可留。

---

## 8. Query, links, traverse

Query：`Limit<=0` → 100，上限 1000；identity 列 tie-breaker；`Cursor` 空；`AsOf*` → `ErrUnsupportedCapability`。OrderBy 为 mapping 逻辑名。JOIN `of_object_meta` **禁止**。

GetLinks / Traverse：planner 产出 `sqlast.Join`。每跳 `tenant_id`（及未 omit 的 `deleted_at IS NULL`）在同一 SQL。深度 > 8 → `ErrUnsupportedCapability`。未知/跨租户 start → `ErrObjectNotFound`。

本轮不要求一条 SQL 表达可变深度递归；固定 `path.Steps` 的链式 JOIN 即可。禁止再对影子表 BFS。

多态「一列指向多种 ObjectType」不支持：from/to 在 mapping 里 typed。

---

## 9. Tenant, errors, security

空 tenant → `ErrTenantRequired`。值只来自 `RequestContext`。跨租户 ≡ not-found。

Sentinel 沿用 v2 加法集合。不再出现依赖 sidecar 的 split-brain。缺表/缺列在 ApplySchema 阶段失败，不在每个 Get 上猜。

参数化 SQL、标识符白名单、dsnRef、公开错误不含路径/DSN/SQL。无 ReBAC。

---

## 10. SPI this round

| 方法 | v3 |
|---|---|
| `ApplySchema` / `GetSchema` / `HealthCheck` / `Capabilities` | 有。ApplySchema = 检查 + 进程内激活，不建 `of_*` |
| Object CRUD / `QueryObjects` | 业务表 |
| Link CRUD / `GetLinks` / `Traverse` | 业务表 JOIN |
| `BeginTransaction` | 本地 SQL 事务，钉住 tenant + compiled |
| `GetObjectAtVersion` / `GetObjectAtTime` / `BulkMutate` | `ErrUnsupportedCapability` 或 `ErrUnimplemented`，不得装成成功 |
| Aggregate / Search / EnsureIndex | 本轮可不交付；不得 silently 空成功冒充能力 |

`Capabilities` 不得把未实现的 temporal/bulk 报成 true。

---

## 11. SQLite dialect

- `?` 占位符；标识符 `^[A-Za-z_][A-Za-z0-9_]*$` 双引号
- STRICT 业务表由辅助 DDL 生成时可用
- WAL + `busy_timeout=5000`
- `NormalizeValue`：INTEGER 0/1 → bool
- 删除 `SidecarStatements`；代之以 mapped-table DDL 生成器

---

## 12. Removed vs v2

| v2 | v3 |
|---|---|
| `sidecar` identity + `of_*_meta` | 删除 |
| `_id` 与物理 PK 两套值 | 合一 |
| Traverse = GetLinks BFS on `of_link_meta` | JOIN 业务表 |
| ApplySchema 创建 sidecar | 禁止；改为存在性检查 |
| 无自动业务表 DDL | 可选辅助，且不能跳过检查 |
| 系统列缺失由 sidecar 补 | 列在表上，或 mapping omit |
| 持久化 mapping/schema 版本表 | 进程内激活 |

`docs/design/open-foundry-obda-mapping-spec-v1.md` 的 overlay / AGE / ReBAC 叙事仍然作废。
