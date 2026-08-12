# OntologyObject：Go `map[string]any` vs TS interface

## 根因

| | TS | Go |
|---|---|---|
| 类型 | interface：命名字段 + `[key: string]: unknown` | `struct` 无任意键；`map[string]any` 无静态字段 |
| 实现选择 | interface 文档化契约；运行期仍是键袋 | `map[string]any`（plan U1：概念搬运 TS SPI 形状；helper/AST 留实现期） |

选 map 的理由：扁平 JSON 线形状（`_` 系统键与用户键混排）；`json.Marshal`/`Unmarshal` 一行 clone（`runtime/storage/memory/provider.go:150-161`）；与 TS 运行期存储方式一致。

## 当前代价

1. **保留字段无单一事实源** — 三处可漂移：`isSystemField`（7）、`isLinkSystemField`（12，超集含 `_fromId`/`_toId`/`_fromType`/`_toType`/`_engineLinkId`）、`doCreate/Update/DeleteObjectUnlocked` 字面量
2. **读取全 `any`** — 如 `obj["_tenantId"] != ctx.TenantID`（251）、`obj["_deletedAt"] != nil`；脏数据静默判 false，无编译期检查
3. **类型断言散落** — `obj["_id"].(string)`（228）等；须 comma-ok 或 panic
4. **`_version` int/float64 宽化** — `objectVersionInt`（428-438）容忍 `int`/`float64`/`int64`；JSON clone 把 int 拓宽为 float64（any 的直接代价）
5. **契约不可发现** — `OntologyObject map[string]any`（`runtime/spi/ontology.go:15`）类型层无字段清单
6. **`_` 命名空间靠散落约定** — `SearchObjects` 用 `HasPrefix(k, "_")`（1537）；`isSystemField` 用 switch；用户属性 `_foo` 行为取决于哪个 helper
7. **object/link 不对称** — 同为 `map[string]any`；link 保留字段更多，类型层无区分

## 决策

### 不做：struct + 自定义扁平 JSON

```go
type OntologyObject struct {
    TenantID   string         `json:"_tenantId"`
    Properties map[string]any `json:"-"` // 需 MarshalJSON/UnmarshalJSON 摊平
}
```

- 线形状须扁平（TS 契约、conformance、`cloneObject` 往返）
- OntologyObject + OntologyLink 各需一套 Marshal/Unmarshal；加字段须同步
- filter/search/traverse/BulkMutate 等「均匀键袋」逻辑须拆 `SystemField` vs `Properties[field]`
- struct `int` 字段与 JSON-clone 宽化惯例冲突；`objectVersionInt` 已表明设计允许 JSON 损耗

> **💡** 在出现「从不 JSON 往返」的后端（Postgres/AGE 行映射）之前，不值得为编译期字段安全付出此代价。

### 做：保留 map，常量化保留字段（纯重构，零行为变化）

```go
// spi/ontology.go
const (
    FieldID        = "_id"
    FieldType      = "_type"
    FieldTenantID  = "_tenantId"
    FieldVersion   = "_version"
    FieldCreatedAt = "_createdAt"
    FieldUpdatedAt = "_updatedAt"
    FieldDeletedAt = "_deletedAt"
)
const (
    LinkFieldFromID   = "_fromId"
    LinkFieldToID     = "_toId"
    LinkFieldFromType = "_fromType"
    LinkFieldToType   = "_toType"
    LinkFieldEngineID = "_engineLinkId"
)
```

- `isSystemField` / `isLinkSystemField` → 常量集合
- `doCreate/Update/DeleteObjectUnlocked` 字面量 → 常量
- 可导出 `IsSystemField(string) bool`、`IsLinkSystemField(string) bool` 供 projection/后端共享

收益：单一事实源（1/6）；SPI 契约文档（5）；符号引用防 typo（2）；不碰线形状与 clone。不解决 `_version` any 宽化（JSON-clone 固有代价）。

## 结论

- **原因**：Go 无法单类型表达「命名字段 + 任意键」；map 保扁平 JSON + JSON-clone
- **改法**：struct+自定义 JSON 不做；常量化 + 导出 helper 应做
- **债务**：保留字段多处重声明；any 读取/断言；`_version` 宽化补丁；object/link 无类型区分；`_` 命名空间散落
