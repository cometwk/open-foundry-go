# (1)

我先读一下这份 spec 的更多上下文，以及相关的代码实现，再来给你详细解释。
让我再看一下 spec 里 identity strategy 的定义部分，以及 mapping 的完整示例。
好的，现在我对整个 Identity 机制有了完整理解，下面用具体例子来解释。

---

## Identity 到底在解决什么问题

核心矛盾：**引擎层**（engine）需要一个统一的 `_id` 来做 `GetObject`、`GetLinks`、`Traverse` 等操作，但**底层数据库**用的是业务主键（如 `patient_id`、`admission_id`）。spec 第 301 行说的就是约束：

> `GetLinks` / `Traverse` 只拿对象 id、不拿类型。id 在一个 provider 内必须可解码出类型，禁止扫多表猜测。

也就是说，引擎给你一个 `id` 字符串，provider 必须能从这一个字符串**单值反查出：类型是什么 + 物理主键是什么**，不能靠"扫所有表猜匹配"的方式。

spec 提供了两种策略来填这道鸿沟：`sidecar` 和 `direct`。

---

## 5.1 sidecar 策略 —— "影子表"方案

**思路**：底层业务表不动，在旁边建一张 `of_object_meta` 影子表存映射。

以 spec 示例里的 `Patient` 为例（第 233-251 行）：

```yaml
models:
  Patient:
    identity:
      strategy: sidecar
      columns: [patient_id]     # 业务主键列
```

业务表 `patient` 长这样：

| patient_id | tenant_id | patient_name |
|---|---|---|
| p1 | t1 | Ada |

`ApplySchema` 时 sidecar 会 **backfill**（见 `sidecar.go` 第 53-82 行）：扫业务表，对每一行生成一个 **UUIDv7** 作为 `engine_id`，把映射写进 `of_object_meta`：

| engine_id (UUIDv7) | tenant_id | object_type | physical_key | version |
|---|---|---|---|---|
| `0195a3f1-...` | t1 | Patient | `p1` | 1 |

其中 `physical_key` 由 `EncodePhysicalKey` 编码（`identity.go` 第 21-31 行）：
- **单列**：直接 `fmt.Sprint`，结果就是 `"p1"`
- **复合列**（如 `[ward_id, dept_id]`）：JSON 数组 `["W01","D02"]`

引擎层拿到 `engine_id = "0195a3f1-..."` 后：
- `GetObject("Patient", "0195a3f1-...")` → 查 `of_object_meta` 得到 `physical_key = "p1"` → 再查业务表 `WHERE patient_id = 'p1'`
- `GetLinks` / `Traverse` 也只传 `engine_id`，不传类型——因为 `of_object_meta` 里有 `object_type` 列

**关键约束**（第 340 行）：`of_object_meta` 上有 UNIQUE `(tenant_id, object_type, physical_key)`，软删后再 Create 同一物理键会被拒绝。

Link 的 sidecar 也一样，`of_link_meta` 存 `engine_id ↔ (tenant, link_type, from_id, to_id)`。

---

## 5.2 direct 策略 —— "自编码 ID"方案

**思路**：不建影子表，把**类型 + 物理键**直接编码进 id 字符串里。id 本身就是可逆的。

编码格式（`identity.go` 第 15-18 行）：

```go
func EncodeDirect(typ string, keys []string) string {
    b, _ := json.Marshal(directPayload{Type: typ, Keys: keys})
    return base64.RawURLEncoding.EncodeToString(b)
}
```

举例：`EncodeDirect("Patient", []string{"p1"})`

1. JSON：`{"t":"Patient","k":["p1"]}`
2. base64url 无 padding：`eyJ0IjoiUGF0aWVudCIsImsiOlsicDEiXX0`

所以这个 Patient 对象的 `_id` 就是 `eyJ0IjoiUGF0aWVudCIsImsiOlsicDEiXX0`。

测试用例（`objects_test.go` 第 313-335 行）验证了完整流程：

```go
created, err := p.CreateObject(ctx, "Patient", map[string]any{"patientId": "p1", "name": "Ada"})
id := created[spi.FieldID].(string)
want := obda.EncodeDirect("Patient", []string{"p1"})
// id == "eyJ0IjoiUGF0aWVudCIsImsiOlsicDEiXX0"
got, err := p.GetObject(ctx, "Patient", id)  // 能查回来
```

**解码路径**（`objects.go` 第 386-408 行）：`loadMeta` 先按 `engine_id` 查 `of_object_meta`（sidecar 路径）；查不到时，如果是 `direct` 策略，就 `DecodeDirect(id)` 反解出 `(type="Patient", keys=["p1"])`，再拿 `physical_key` 查 meta 表。

第 317 行的约束很关键：
> 无 padding（`RawURLEncoding`）。禁止 delimiter 拼接。解码失败、类型不匹配 → `ErrObjectNotFound` / `ErrLinkNotFound`。

"禁止 delimiter 拼接"意味着不能做 `type + ":" + key` 这种方式，因为分隔符可能在 key 值里出现，导致歧义。base64 + JSON 是确定性的。

对 Link，direct 策略也类似（`links.go` 第 78 行）：

```go
engineID = obda.EncodeDirect(l.Name, []string{admID})
// 例如 EncodeDirect("AdmittedTo", []string{"adm001"})
```

---

## 5.3 physical_key —— sidecar 的内部查找键

`physical_key` 不是给引擎用的 id，而是 sidecar 内部用来做唯一性和查找的。

- 单列：`fmt.Sprint(value)` → `"p1"`
- 复合列：JSON 字符串数组 → `["W01","D02"]`

它用在 `of_object_meta` 的 UNIQUE 约束、history 表、幂等键、cache 键里。

---

## 两种策略对比

| | sidecar | direct |
|---|---|---|
| id 形式 | UUIDv7（`0195a3f1-...`） | base64url（`eyJ0Ijoi...`） |
| id 可自解码类型？ | 否，需查 `of_*_meta` 表 | 是，base64 解码即得 |
| 需要影子表？ | 是 | 否（但仍用 meta 表做 version/history） |
| Create 时 id 来源 | 系统生成 UUIDv7 | 从业务主键编码 |
| Link 的 `_engineLinkId` | 可采纳用户传入 | **忽略**，用业务键编码 |
| 适用场景 | 业务表不可改、需要软删/历史 | id 需跨 provider 稳定、可重建 |

一句话总结：**Identity 就是"引擎 ID ↔ 数据库物理主键"的映射层**，sidecar 靠影子表查、direct 靠自编码字符串解码，两者都保证"只给一个 id 就能定位到类型和物理行"。


# (2)

不对。这里有一个关键的方向性理解偏差。让我结合代码来澄清。

## 核心纠正：EncodeDirect 的输出**不存进**业务表

你的理解里有一个隐含假设："业务表的 ID 必须用 EncodeDirect 算法生成并存储"。**实际上恰恰相反**。

看 `objects.go` 的 CreateObject 流程（第 72-100 行）：

```go
// 1. 从用户传入的 properties 里提取业务主键值
keys, err := identityFromProps(m, props)     // keys = ["p1"]

// 2. 业务表 INSERT —— 用的是原始业务键，不是 EncodeDirect 的结果
p.insertBusiness(tx, m, ctx.TenantID, props, keys)
// → INSERT INTO patient (patient_id, tenant_id, patient_name) VALUES ('p1', 't1', 'Ada')

// 3. 引擎 ID 是"派生"出来的，只存进 of_object_meta
engineID := obda.EncodeDirect(m.Name, stringify(keys))  // "eyJ0Ijoi..."
tx.Exec(`INSERT INTO of_object_meta (engine_id, ...) VALUES (?, ...)`, engineID, ...)
```

所以数据分布是：

| 位置 | 存什么 |
|---|---|
| 业务表 `patient` | `patient_id = 'p1'`（原始业务键，什么格式都行） |
| `of_object_meta` | `engine_id = 'eyJ0Ijoi...'`（派生的引擎 ID） |

**业务表完全不需要知道 EncodeDirect 的存在。**

## 你的类比应该这样修正

你说的"一般业务设计 id = UUID/雪花"——在那个模式里，UUID 是**生成后存进表的主键**。

而 direct 策略是反过来的：

```
一般设计:    生成 UUID → 存进业务表 → 全局用它当 id
direct策略:  业务表已有自己的 PK → 引擎层"包裹"一层 → engine_id = EncodeDirect(类型, 业务PK)
```

EncodeDirect 是一个**只读派生函数**，不是业务表的 ID 生成策略。业务表该用什么主键就用什么主键——整数、UUID、雪花、字符串 `"p1"` 都行。

## direct 策略真正的"前提"

前提不是"业务表必须用这个算法"，而是：

**1. 业务表必须有稳定的主键列，且映射里声明了它们**

```yaml
identity:
  strategy: direct
  columns: [patient_id]   # ← 这些列必须存在且有意义
```

**2. 这些主键值必须不可变**

这是最关键的约束。因为 `engine_id = EncodeDirect(type, key)` 是从业务键**计算**出来的，如果业务键变了：

```
创建时:  patient_id='p1' → engine_id = EncodeDirect("Patient", ["p1"])
改键后:  patient_id='p2' → 旧 engine_id 解码出 'p1'，查不到了
```

sidecar 策略没这个问题，因为 `engine_id` 是随机 UUIDv7，跟业务键解耦——改了业务键，`of_object_meta` 里的 `physical_key` 更新一下就行，`engine_id` 不变。

## 一句话总结

| | 谁生成 id | id 存在哪 | 业务表要不要改 |
|---|---|---|---|
| 你说的"一般设计" | 应用层生成 UUID/雪花 | **存进业务表** | 要，主键列改类型 |
| direct 策略 | 引擎层从业务键**派生** | 只存 `of_object_meta`，**不进业务表** | 不要，业务键原样保留 |

direct 的本质是"给已有业务键套一层类型信封"，不是"让业务表换一种 ID 算法"。

# (3)

好问题。让我先看 Traverse 和 link 查询的实际实现，看看 direct 策略下 JOIN 是怎么处理的。
现在让我看 GetLinks 的具体实现——Traverse 内部调用的就是它。
让我看一下 `lookupAnyObject`——这是 Traverse 中解析"对端"对象的关键。
现在全链路清楚了。回答你的两个问题。

## 问题 1：direct 到底要不要影子表？

对。spec 里写"否"的意思是——**direct 的 ID 自解码不需要查 `of_object_meta` 来反推类型**。但实际代码里 direct 仍然往 `of_object_meta` 和 `of_link_meta` 里写数据（`objects.go` 第 97-102 行）：

```go
// direct 策略也走这段 INSERT
tx.Exec(`INSERT INTO "of_object_meta" (engine_id, ...) VALUES (?, ...)`, engineID, ...)
```

因为 meta 表还承担 version、history、soft-delete、UNIQUE 约束等职责，这些跟 identity 策略无关。

## 问题 2：Traverse 涉及到 JOIN 怎么办？

**核心答案：Traverse 根本不做 SQL JOIN。**

你可能在想传统的关系型写法：

```sql
-- 传统思路：三层 JOIN
SELECT ward.* FROM patient
  JOIN admission ON patient.patient_id = admission.patient_id
  JOIN ward      ON admission.ward_id  = ward.ward_id
WHERE patient.patient_id = 'p1'
```

但 OBDA 架构不是这么干的。看 `links.go` 的 `GetLinks`（第 213-306 行）和 `Traverse`（第 308-367 行）：

### GetLinks：查的是 `of_link_meta`，不是业务表

```go
// 第 226 行 —— 查影子表，不是 business table
sel, args, _ := obda.PlanGetLinks("of_link_meta", "tenant_id", endCol, ctx.TenantID, objectID)
```

生成的 SQL 是：

```sql
SELECT engine_id, link_type, from_id, to_id, version, ...
FROM "of_link_meta"
WHERE tenant_id = ?    -- 't1'
  AND from_id = ?      -- <Patient 的 engine_id>  ← 这里用 engine ID 匹配
  AND link_type = ?    -- 'AdmittedTo'
  AND deleted_at IS NULL
```

`from_id` / `to_id` 是 **engine ID**（TEXT 列），无论 direct（base64）还是 sidecar（UUIDv7），都是用 `=` 做 TEXT 等值匹配。

### Traverse：Go 层 BFS 循环，不是一条 SQL

```go
// links.go 第 322-364 行
frontier := []string{startID}           // 起点的 engine ID

for i, step := range path.Steps {      // 例如: AdmittedTo → UsedIn
    var next []string
    for _, id := range frontier {
        // ① 查 of_link_meta，拿到 link 行
        page, _ := p.GetLinks(ctx, id, step.LinkType, dir, ...)

        for _, e := range page.Items {
            // ② 从 link 行里取对端的 engine ID
            other := e[spi.LinkFieldToID].(string)   // 或 FromID

            // ③ 用 engine ID 加载对端对象（查 of_object_meta → 查 business table）
            obj, _ := p.lookupAnyObject(p.db, act, ctx.TenantID, other)
            next = append(next, other)
        }
    }
    frontier = next   // 下一跳的起点
}
```

### 完整数据流图

以 `Patient --AdmittedTo--> Ward` 为例，direct 策略下：

```
① CreateLink 时:
   of_link_meta:
   | engine_id          | link_type   | from_id              | to_id              |
   |---------------------|-------------|----------------------|---------------------|
   | eyJ0IjoiQW...      | AdmittedTo  | eyJ0IjoiUGF0aWVu...  | eyJ0IjoiV2FyZC4u  |
   |                     |             |  (Patient p1 的ID)    |  (Ward w1 的ID)     |

   business table admission:
   | admission_id | patient_id | ward_id | tenant_id |
   |--------------|------------|---------|-----------|
   | adm001       | p1         | w1      | t1        |

② Traverse(PatientID, [{LinkType: "AdmittedTo", Direction: "outbound"}]):

   Step 1: GetLinks(PatientID, "AdmittedTo", "outbound")
           → SELECT ... FROM of_link_meta WHERE from_id = <PatientID>
           → 返回 link 行，to_id = <WardID>

   Step 2: lookupAnyObject(<WardID>)
           → loadMeta: DecodeDirect(WardID) → ("Ward", ["w1"])
           → SELECT ... FROM of_object_meta WHERE physical_key = 'w1'
           → SELECT ... FROM ward WHERE ward_id = 'w1'    ← 单表查，不 JOIN

   Result: Ward 对象
```

### 为什么这样设计而不做 SQL JOIN？

| 原因 | 说明 |
|---|---|
| **多态性** | `of_link_meta` 里 `from_id` 可能指向 Patient、Supplier 等不同类型的对象，SQL JOIN 无法跨不同业务表 |
| **策略无关** | `from_id`/`to_id` 存的是 engine ID，direct 和 sidecar 统一处理，provider 层透明 |
| **OBDA 映射灵活** | link 的业务表结构各异（有的有中间表，有的用外键），统一走 `of_link_meta` 就不需要为每种映射写不同 JOIN |
| **软删/版本隔离** | `of_link_meta` 有 `deleted_at`、`version`，graph 层和数据层解耦 |

**一句话：direct 策略下，`of_link_meta` 的 `from_id`/`to_id` 存的就是 `EncodeDirect` 生成的 base64 字符串，JOIN 退化为 `WHERE from_id = 'eyJ0...'` 的 TEXT 等值匹配，然后逐跳 BFS 在 Go 层完成。不存在跨业务表的 SQL JOIN。**

# (4)

> 接下来，要求: 采用 direct 的策略，且不需要额外sidebar表的条件下，Traverse 必须支持 JOIN，如何才能做到？可以对被映射的业务表提出约束，比如要求有ID，且ID=EncodeDirect(UUID) ?


这是一个架构设计问题。让我先确认当前 planner 和 compiler 的数据结构，看看 JOIN 需要哪些信息。
让我看一下 sqlast 是否已支持 JOIN 节点，以及当前 System strategy 的处理。
现在我对整个架构有了完整的理解。关键发现：

1. **`sqlast.Select` 已经有 `Joins []Join` 字段**——AST 层面已支持 JOIN
2. **`system.strategy` 已接受 `"native"` 值**（validate.go 第 82 行）——只是 provider 侧尚未完全实现
3. **`CompiledLink` 已有 `FromObject/FromColumns/ToObject/ToColumns`**——JOIN 所需的绑定信息已在编译结果中

你的方向完全可行。下面是完整的设计方案。

---

## 核心思路：让 engine ID = 物理 PK，消灭映射层

当前架构的"映射开销"来自一个事实：**engine ID 和物理 PK 是两个不同的值，存在两张表里**。

你的提议——要求业务表 PK 列直接存 `EncodeDirect(type, UUID)`——让两者合二为一：

```
当前:   patient.patient_id = "p1"  ←→  of_object_meta.engine_id = "eyJ0..."  (两张表)
你的方案: patient.id = "eyJ0..."                                          (一张表)
```

这样 `of_object_meta` 和 `of_link_meta` 就**没有存在必要**了。

## 业务表约束

对被映射的业务表提出以下约束：

**对象表：**
```sql
CREATE TABLE patient (
  id          TEXT PRIMARY KEY,           -- = EncodeDirect("Patient", UUIDv7)，引擎生成
  tenant_id   TEXT NOT NULL,
  patient_name TEXT,
  deleted_at  TEXT,                        -- system: native
  version     INTEGER NOT NULL DEFAULT 1,  -- system: native
  created_at  TEXT NOT NULL,              -- system: native
  updated_at  TEXT NOT NULL               -- system: native
);

CREATE TABLE ward (
  id          TEXT PRIMARY KEY,           -- = EncodeDirect("Ward", UUIDv7)
  tenant_id   TEXT NOT NULL,
  ward_name   TEXT,
  deleted_at  TEXT,
  version     INTEGER NOT NULL DEFAULT 1,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
```

**链接表：**
```sql
CREATE TABLE admission (
  id          TEXT PRIMARY KEY,           -- = EncodeDirect("AdmittedTo", UUIDv7)
  tenant_id   TEXT NOT NULL,
  from_id     TEXT NOT NULL,              -- = patient.id (EncodeDirect 值)
  to_id       TEXT NOT NULL,              -- = ward.id (EncodeDirect 值)
  deleted_at  TEXT,
  version     INTEGER NOT NULL DEFAULT 1,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  FOREIGN KEY (from_id) REFERENCES patient(id),
  FOREIGN KEY (to_id) REFERENCES ward(id)
);
```

## OBDA 映射

```yaml
models:
  Patient:
    identity:
      strategy: direct
      columns: [id]
      insert: generated          # 引擎生成 EncodeDirect("Patient", UUIDv7)
    tenant:
      strategy: column
      column: tenant_id
    system:
      strategy: native            # 系统字段在业务表上，不建 sidecar
    fields:
      name:
        column: patient_name

  Ward:
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
    relation:
      kind: table
      name: admission
    identity:
      strategy: direct
      columns: [id]
      insert: generated
    from:
      object: Patient
      columns: [from_id]          # 直接引用 patient.id
    to:
      object: Ward
      columns: [to_id]             # 直接引用 ward.id
    tenant:
      strategy: column
      column: tenant_id
    system:
      strategy: native
    fields: {}
```

## 生成的 SQL

### GetLinks（单跳，带 JOIN）

当前实现查 `of_link_meta`，新方案直接 JOIN 业务表：

```sql
-- GetLinks(ctx, patientID, "AdmittedTo", "outbound")
SELECT l.id, l.tenant_id, l.from_id, l.to_id, l.version, 
       l.created_at, l.updated_at, l.deleted_at,
       w.id, w.ward_name
FROM admission l
JOIN ward w ON l.to_id = w.id       -- CompiledLink.ToTable + CompiledLink.ToColumns[0] = CompiledModel.IdentityColumns[0]
WHERE l.from_id = ?                   -- = patientID (EncodeDirect("Patient", UUIDv7))
  AND l.tenant_id = ?
  AND l.deleted_at IS NULL
  AND w.deleted_at IS NULL
ORDER BY l.id
LIMIT ? OFFSET ?
```

### Traverse（多跳，单条 SQL 链式 JOIN）

```sql
-- Traverse(ctx, patientID, [{AdmittedTo, outbound}, {LocatedIn, outbound}])
-- 最终节点: Hospital
SELECT DISTINCT p2.id, p2.tenant_id, p2.hospital_name,
       p2.version, p2.created_at, p2.updated_at
FROM patient p0
JOIN admission l1  ON p0.id = l1.from_id    -- hop 1: AdmittedTo outbound
JOIN ward     p1  ON l1.to_id = p1.id
JOIN location l2  ON p1.id = l2.from_id      -- hop 2: LocatedIn outbound
JOIN hospital p2  ON l2.to_id = p2.id
WHERE p0.id = ?                               -- startID
  AND l1.tenant_id = ? AND p1.tenant_id = ?
  AND l2.tenant_id = ? AND p2.tenant_id = ?
  AND l1.deleted_at IS NULL AND l2.deleted_at IS NULL
  AND p1.deleted_at IS NULL AND p2.deleted_at IS NULL
```

对比当前 BFS 循环（每跳 N+1 次查询），这是**单次 SQL、数据库引擎优化**。

## 需要的代码改动

改动集中在 planner 和 provider，AST 层无需改动（`sqlast.Join` 已存在）。

**planner 新增：**

```go
// PlanGetLinksJoin 生成 link 表 JOIN target 表的 SQL
func PlanGetLinksJoin(
    linkBinding LinkBinding,     // admission 表, from_id/to_id 列
    targetBinding ObjectBinding, // ward 表, id 列, select 列
    tenant, objectID string,
    direction string,            // "outbound" → from_id; "inbound" → to_id
) (*sqlast.Select, []any, error)

// PlanTraverse 生成多跳链式 JOIN 的 SQL
func PlanTraverse(
    compiled *Compiled,
    startType string,           // 从 DecodeDirect(startID) 得到
    steps []TraversalStep,
    tenant, startID string,
) (*sqlast.Select, []any, error)
```

`PlanTraverse` 的核心逻辑：遍历 steps，对每一步从 `compiled.Links[step.LinkType]` 拿到 link binding，从 `compiled.Models[link.FromObject/ToObject]` 拿到对象 binding，拼接 `sqlast.Join{Table: ..., As: ..., On: ...}`。

**provider 改动（`sqliteobda`）：**

| 方法 | 当前 | 改动 |
|---|---|---|
| `CreateObject` | INSERT 业务表 + INSERT `of_object_meta` | `native` 模式下只 INSERT 业务表（含 `version/created_at/updated_at`） |
| `GetObject` | 查 `of_object_meta` → 查业务表 | `native+direct` 下：`DecodeDirect(id)` 得 type → 直接查业务表 `WHERE id = ?` |
| `GetLinks` | 查 `of_link_meta` → assembleLink | `native` 下：用 `PlanGetLinksJoin` 单条 SQL |
| `Traverse` | BFS 循环调 `GetLinks` | `native` 下：用 `PlanTraverse` 单条 SQL |
| `DeleteObject` | UPDATE `of_object_meta.deleted_at` | `native` 下：UPDATE 业务表 `deleted_at` |
| `ApplySchema` | 建 `of_*` 表 + backfill | `native` 下：跳过 sidecar 建表和 backfill |

## 代价与取舍

| 维度 | sidecar（当前） | direct-native（你的方案） |
|---|---|---|
| Traverse 实现 | Go BFS 循环，N+1 查询 | 单条 SQL JOIN，数据库优化 |
| 业务表要求 | 任意（可映射遗留表） | 必须用 `id = EncodeDirect(type, UUID)` 作 PK |
| 系统字段 | 在 `of_*` 表 | 在业务表上（`deleted_at`/`version` 列） |
| 历史快照 | `of_object_history` | ❌ 丢失（需另建 history 表或 DB trigger） |
| 幂等性 | `of_idempotency` | ❌ 丢失 |
| Schema 漂移检测 | `of_mapping_activation` fingerprint | ❌ 丢失 |
| 软删 UNIQUE | `of_object_meta` UNIQUE 约束 | 业务表自身约束 |
| 多态 link | `of_link_meta.from_id` 可指向任意类型 | ❌ link 表 FK 是类型化的 |
| 适用场景 | 遗留数据库、OBDA 只读映射 | 新建系统（greenfield）、对性能要求高 |

**核心取舍一句话**：direct-native 用"必须控制业务表 schema"换来了"Traverse 走 SQL JOIN"。对于遗留数据库（不能改 PK 格式），仍然需要 sidecar 方案。

你的方案是对的——`id = EncodeDirect(type, UUID)` 这个约束让 engine ID 和物理 PK 统一，`of_*_meta` 表的全部职责都被吸收回业务表，JOIN 就变得自然了。代码改动量也不大，因为 `sqlast.Join` 和 `CompiledLink` 的 from/to 绑定信息都已经有了。
