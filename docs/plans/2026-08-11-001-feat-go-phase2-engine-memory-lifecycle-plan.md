---
date: 2026-08-11
type: feat
topic: go-phase2-engine-memory-lifecycle
title: "Go Phase 2 — Engine + Memory Object/Link Lifecycle - Plan"
origin: docs/design/draft.md
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
product_contract_source: ce-brainstorm
execution: code
---

# Go Phase 2 — Engine + Memory Object/Link Lifecycle - Plan

## Goal Capsule

- **Objective:** 在 `runtime/` 中加入 Go `Engine`，编排六个原子生命动词（Create/Get/Update/Delete Object + Create/Delete Link），覆盖一层只实现这六个动词＋Engine 写前读所需七个 SPI 方法的薄内存持久层。校验复用 Phase 1 的 TBox `ir.Validate`。
- **Product Authority:** 这是 Go-native Ontology Runtime 的 Phase 2，不是 TypeScript 逐行移植。`packages/engine`、`packages/storage-memory` 是行为参考；移植绑定 INTENT（动词步骤顺序、依赖方向），不移植 AST 形状。
- **Open Blockers:** 无。

## Product Contract

> Product Contract 全文保持不变——R1–R16、AE1–AE9、F1–F7 的 ID 与语义保持，仅译成中文呈现。原 Outstanding Questions 中的 Q1–Q4 在 Phase 2 规划阶段已解决，对应 KTD-1 至 KTD-7（见 Planning Contract）。

### Summary

Phase 2 使 Go Runtime 能端到端变更并查询 ABox 状态：薄层 Engine 编排六个对象/链接生命动词，叠在一层只实现这七个 SPI 方法的内存 Provider 之上。校验复用 Phase 1 的 `ir.Validate`；软删、版本自增、历史、事件、查询、事务被刻意推迟，以保持内存层薄度。

### Problem Frame

Phase 1 完成 Runtime 的"schema 侧"——SPI 类型、ODL parser、Ontology IR、storage projection 与 schema-only memory（`ApplySchema`/`GetSchema`）。Runtime 已能描述 TBox 语义，却无法变更或查询 ABox 状态：所有 object 与 link 的 SPI 方法仍经嵌入的 `UnimplementedStorageProvider` 返回 `ErrUnimplemented`（`runtime/spi/unimplemented.go:38,58,70`）。

`docs/design/draft.md` §4 命名 object lifecycle 为 Engine 的核心职责，并把 Phase 2 提议为 Memory → Engine → Object/Link lifecycle。Phase 1 Risk 表已经标记 "SPI surface drift vs TS"，缓解项是 "Phase 2 fills behavior"。Phase 2 对最小动词集合关闭这个差距，刻意不涵盖整个 SPI 表面。

### Requirements

**Engine —— 六个原子动词**

- R1. Create Object：Engine 先以 IR TBox 校验对象类型与 payload（类型存在、字段角色/类型接受给定属性），再委托 `storage.CreateObject` 写入，返回的对象包含系统字段（`_id`、`_type`、`_createdAt`、`_updatedAt`、`_tenantId`）。
- R2. Get Object：Engine 以 `(type, id)` 读取；缺失返回带类型的 not-found 错误。Engine 不在 storage 返回 not-found 时合成值。
- R3. Update Object：Engine 先读 existing（缺失 → 写前带类型 not-found 错误），合并 patch（剔除系统字段），校验合并后状态，再委托 `storage.UpdateObject` 写入，`expectedVersion=nil`。返回 `_updatedAt` 推进后的对象。
- R4. Delete Object (hard)：Engine 先读 existing（缺失 → 写前带类型 not-found 错误），通过 SPI `deleteObject(mode="hard")` 硬删。随后 `GetObject` 同 `(type, id)` 返回 not-found。软删（`mode="soft"`）Phase 2 内存不实现。
- R5. Create Link：Engine 校验 link 类型存在于 IR，通过 `GetObject` 断言 from/to 对象存在（缺失 → 写前带类型 not-found 错误），生成 UUIDv7 链接 id，委托 `storage.CreateLink` 写入，返回存储后的链接。
- R6. Delete Link：Engine 先读 existing 链接（缺失 → 写前带类型 not-found 错误），通过 SPI `deleteLink` 硬删。随后 `GetLink` 同 `(type, id)` 返回 not-found。
- R7. 校验复用 Phase 1 TBox `ir.Validate(*Ontology) error`（`runtime/ir/validate.go:34`）作为 schema-level 闸门；每个动词的运行时检查（类型存在于 IR、链接类型存在、from/to 满足链接 `from/to`、字段角色接受 payload）位于 Engine 侧且轻量；Engine 不重新实现 `ir.Validate`。
- R8. Engine 依赖 `spi.StorageProvider` 接口，不依赖具体 `memory.Provider`。装配发生于 composition root（构造时），镜像 TS `ObjectManager`/`LinkManager` 的 constructor 注入。

**Memory —— 薄持久化**

- R9. memory provider 为这六个动词所调用的 7 个 SPI 方法实现真实行为：`CreateObject`、`GetObject`、`UpdateObject`、`DeleteObject`、`CreateLink`、`GetLink`、`DeleteLink`。（`GetObject` 与 `GetLink` 因 Engine 写前读而必须实现。）
- R10. 对象以 `type:id` 为键存于 map。对象 doc 携带 `_id`、`_type`、`_tenantId`、`_createdAt`、`_updatedAt`。`_version` Phase 2 不存储（update 自增延后）；`_deletedAt` 与软删路径延后。
- R11. 链接以 `type:id` 为键存于 map。链接 doc 携带 `_id`、`_type`、`_fromId`、`_toId`、`_fromType`、`_toType`、`_createdAt`、`_updatedAt`、`_tenantId`。硬删从 map 移除条目。
- R12. memory provider 上其余 SPI 方法经嵌入的 `UnimplementedStorageProvider` 仍返回 `ErrUnimplemented`。它们是显式的 error-contract 表面，不是静默成功——验收契约由此覆盖。
- R13. 硬删从 map 移除对象/链接；不保留版本历史。Phase 2 不维护 `_versionHistory` map（TS memory provider 的 `_versionHistory` 延后）。

**Conformance 验收**

- R14. 一组 Go-native 一致性子集镜像 `tests/spi-conformance/src/categories/crud.ts` 与 `links.ts` 的"意图"，仅覆盖六个实现动词：object 的 Create→Get→Update→Get→Delete→Get-not-found 往返，link 的 Create→Get→Delete→Get-not-found 往返。**不**移植 TS 一致性套件；**不**含基数或软删断言。
- R15. 一致性子集断言错误底限：R9 列表之外的每个 SPI 方法返回 `errors.Is(err, ErrUnimplemented)=true` 且方法名出现在错误消息中。
- R16. 一个 gold-path 端到端覆盖 F7：加载 `domain-packs/supply-chain`（复用 Phase 1 pack loader），project 到 `OntologySchema`，`ApplySchema`，然后在 Supplier、Part 与 Supplies 链接上演练六个动词（含删除与 post-delete not-found 读），七个实现方法零 `ErrUnimplemented` 命中。

### Key Decisions

- **Engine-first layering。** Engine 定义六个动词并依赖 `spi.StorageProvider`；Memory 只实现这些动词所调内容。这是相对 Phase 1 plan "Memory first" 框架的刻意倒转——Engine 拥有 lifecycle 形状，Memory 承载持久化下限。
- **Hard-delete only。** `deleteObject(mode)` 由 Engine 传 `mode="hard"`；memory 从 map 移除条目。TS 软删路径（`_deletedAt`、软删版本自增）与 `mode="soft"` 分支延后。内存不实现 `mode="soft"`，对此调用返回带 ErrUnimplemented 风味的说明性错误。
- **Versioning entirely omitted。** Update 不自增 `_version`；`expectedVersion` 传 `nil` 被 memory 忽略；`_version` 不存储。Phase 2 不能回退 TS `crud.ts` 的 "increments `_version`" / "versionConflict" 断言——这些与 Phase 3 的版本片一起落。
- **Cardinality enforcement deferred。** `createLink` 仅通过 `GetObject` 断言 from/to 对象存在。TS advisory `enforceCardinality`（`link-manager.ts:87`）不移植；`GetLinks` 保持 `ErrUnimplemented`。cardinality 随 Phase 3 一起入栈。
- **每动词 Validation 用 `ir.Validate` 作 TBox 闸门 + Engine 侧一个轻量属性形状检查。** 复用绑定在 TBox 层；Engine 只添加每动词的运行时检查（类型存在、链接类型存在、from/to 满足字段在字段段；required 字段在 Create 时存在），不重写 `ir.Validate`。
- **Conformance 是 Go-native 且 intent-mirroring，非 TS-ported。** 写一个最小 Go test harness 断言六个动词的往返 + ErrUnimplemented 底限，**不**转译 `tests/spi-conformance/src/categories/*.ts`。

### Key Flows

- F1. **Create Object** — Trigger：caller 调 `Engine.CreateObject(type, props, ctx)`。Actors：Engine、`ir.Ontology`、`spi.StorageProvider`。Steps：（1）构造时调用 TBox 闸门 `ir.Validate(ontology)` 一次；每调用再校验对象类型 + 字段角色对 IR；（2）`storage.CreateObject(ctx, type, props)`；（3）memory 标记系统字段，按 `type:id` 存储，返回对象；（4）Engine 返回对象。Outcome：stored object 对后续 Get 可见。Covers R1, R7, R8, R9, R10.
- F2. **Get Object** — Engine 调 `storage.GetObject(ctx, type, id)`；memory 返回 stored object 或带类型的 not-found。Covers R2, R9.
- F3. **Update Object** — Engine 先 `storage.GetObject`（缺失 → 写前带类型错误）；合并 patch 剔除系统字段；在合并后状态上做每调用 validation；`storage.UpdateObject(ctx, type, id, merged, nil)`；memory 推进 `_updatedAt`。Covers R3, R7, R9, R10.
- F4. **Delete Object** — Engine 先 `storage.GetObject`（缺失 → 写前带类型错误；版本号用于稍后事件层——Phase 2 读但不发）；`storage.DeleteObject(ctx, type, id, "hard")`；memory 从 map 移除条目。Outcome：后续 `GetObject` 返回 not-found。Covers R4, R9.
- F5. **Create Link** — Engine 在 IR 中校验链接类型；`storage.GetObject(linkDef.from, fromId)` 与 `storage.GetObject(linkDef.to, toId)`（任一缺失 → 写前带类型 not-found 错误）；Engine 生成 UUIDv7 link id；`storage.CreateLink(ctx, type, fromId, toId, props-with-_engineLinkId)`；memory 标记系统字段，按 `type:id` 存储。Covers R5, R7, R8, R9, R11.
- F6. **Delete Link** — Engine 先 `storage.GetLink`（缺失 → 写前带类型错误）；`storage.DeleteLink(ctx, type, id)`；memory 从 map 移除。Outcome：后续 `GetLink` 返回 not-found。Covers R6, R9.
- F7. **Gold Path** — Trigger：integration test 加载 supply-chain pack。Steps：（1）`pack.Load` → IR；（2）`projection.ProjectStorage(ir)` → `OntologySchema`；（3）`memory.ApplySchema` → `MigrationResult`；（4）`Engine.CreateObject("Supplier", {name:"Acme"})` → s；（5）`Engine.CreateObject("Part", {sku:"P1"})` → p；（6）`Engine.CreateLink("Supplies", s.id, p.id, {})` → l；（7）`Engine.GetObject("Supplier", s.id)` 返回 s；（8）`Engine.UpdateObject("Supplier", s.id, {name:"Acme Corp"})` → patch 反映；（9）`Engine.GetLink("Supplies", l.id)` 返回 l；（10）`Engine.DeleteLink("Supplies", l.id)`；（11）`Engine.GetLink` → not-found；（12）`Engine.DeleteObject("Supplier", s.id, "hard")` 与 `Engine.DeleteObject("Part", p.id, "hard")`；（13）`Engine.GetObject("Supplier", s.id)` → not-found。Outcome：管线全绿，七个实现方法零 `ErrUnimplemented` 命中。Covers F1–F6 集成, R16.

### Acceptance Examples

- AE1. Covers F1, F2 / R1, R2. 已 apply supply-chain schema 下，`Engine.CreateObject("Supplier", {name:"Acme"})` 返回带 `_id`、`_type="Supplier"`、`_createdAt` 的对象；`Engine.GetObject("Supplier", _id)` 返回相同 payload。
- AE2. Covers R3 / F3. `Engine.UpdateObject("Supplier", s.id, {name:"Acme Corp"})` 后，`Engine.GetObject` 反映 `name="Acme Corp"`，且 `_updatedAt` 严格大于 `_createdAt`；其他系统字段未变。
- AE3. Covers R4 / F4. `Engine.DeleteObject("Supplier", s.id, "hard")` 后，`Engine.GetObject("Supplier", s.id)` 返回带类型的 not-found 错误。
- AE4. Covers R3 / F3. `Engine.UpdateObject("Supplier", "nope", {name:"x"})` 返回带类型的 not-found 错误，且从不调用 `storage.UpdateObject`。
- AE5. Covers R5, F5. Supplier `s` 与 Part `p` 已存在时，`Engine.CreateLink("Supplies", s.id, p.id, {})` 返回带 `_id`、`_type="Supplies"`、`_fromId=s.id`、`_toId=p.id` 的链接；`Engine.GetLink("Supplies", _id)` 返回它。
- AE6. Covers R5, F5. `Engine.CreateLink("Supplies", "missing-id", p.id, {})` 返回带类型 not-found 错误且不发生链接的 storage 写入；`to` 端缺失同理。
- AE7. Covers R6, F6. `Engine.DeleteLink("Supplies", l.id)` 后，`Engine.GetLink("Supplies", l.id)` 返回带类型 not-found 错误。
- AE8. Covers R12, R15. 对 memory provider 调用 `QueryObjects`、`GetLinks`、`Traverse`、`BeginTransaction`、`GetObjectAtVersion`、`UpdateLink`、`AggregateObjects` 均返回 `errors.Is(err, ErrUnimplemented)=true` 且方法名出现在错误消息中。
- AE9. Covers F7 / R16, R9–R12. F7 在 supply-chain pack 上端到端跑通，不产生错误且七个实现方法上零 `ErrUnimplemented` 命中。

### Scope Boundaries

**Deferred for later**

- 软删（`mode="soft"`）、`_deletedAt` 标志、软删上版本自增 —— Phase 3。
- Update 的 `_version` 自增与 `expectedVersion` 失配 / Version-Conflict 路径 —— Phase 3。Phase 2 内存不存储 `_version`。
- `getObjectAtVersion` / `getObjectAtTime` / 版本历史存储（TS `_versionHistory` map）—— Phase 3。
- Event emission（`EngineEventEmitter`、`EventBus`、CloudEvent envelope）—— Phase 3。动词步骤顺序保留了天然的 emit hook 槽位，但 Phase 2 不发。
- `CreateLink` 的 cardinality 强制（TS 的 advisory pre-check 经 `GetLinks` 计数）—— Phase 3，与 `GetLinks` 一起。
- `QueryObjects`、`AggregateObjects`、`SearchObjects`、`BulkMutate`、`GetLinks`、`Traverse`、index 方法 —— 越过 Phase 2。
- 事务（`BeginTransaction`、`Transaction` interface 方法）—— Phase 3+。
- Lineage、computed field、object sets —— Phase 3+；Phase 2 全不移植 `packages/engine/src/{lineage,computed,object-sets}`。

**Outside this product's identity**

- 逐行移植 `packages/engine` / `packages/storage-memory`。Phase 2 镜像 TS 绑定 intent（动词步骤顺序、依赖方向、写前的引用完整性）但 Go 代码是 Go-native。
- 把 TS 一致性套件搬到 Go。Phase 2 一致性是 Go-native、仅镜像 intent 的子集；不主张 TS 一致性套件 parity。
- 让 GraphQL 成为语义核心——按 `docs/design/draft.md` §15，IR 是语义核心，GraphQL 是 projection（Phase 6）。

### Dependencies / Assumptions

- Phase 1 `runtime/` 在分支 `feat/go-phase1-ontology-ir` 上稳定：SPI、IR、ODL parser、projection、schema-only memory 全绿。Phase 2 增量加包；会编辑 `runtime/storage/memory/provider.go` 来 override 七个方法，不应重写 Phase 1 的 SPI 与 IR 文件。
- Engine 仅依赖 `spi.StorageProvider`——TS 先例（`packages/engine/src/objects/object-manager.ts:8-20`、`packages/engine/src/links/link-manager.ts:9-18`；`packages/engine/package.json` 把 `@openfoundry/storage-memory` 列为 devDependency only）沿用。
- `ir.Validate(*Ontology) error` 可作 TBox 闸门复用；每对象的属性形状校验是 Engine 侧的，不属于 `ir.Validate`。
- supply-chain pack loader（Phase 1 U4 的 `runtime/pack/loader.go`）无需改动直接复用为 Gold Path seed。
- `CreateLink` 仅断言 from/to 存在不强制 cardinality——TS advisory `enforceCardinality` 刻意不移植。
- `_version`、`_deletedAt` 存储在 Phase 2 刻意缺省；Phase 3 重新引入。系统字段 `_id`、`_type`、`_tenantId`、`_createdAt`、`_updatedAt` 由 memory provider 在七个实现方法上写入，与 TS memory provider 的 stamping 行为一致。

### Outstanding Questions

Phase 2 规划阶段已解决原 Q1–Q4：

- Q1（Update 静默忽略非 nil `expectedVersion`）：决定为静默忽略；见 KTD-3 与用户上下文。
- Q2（Engine 包布局 `runtime/engine/`）：已确认。
- Q3（per-object validator 范围）：决定为最小化、Go-native、对 IR 的检查，**不**移植 TS `validateObjectProperties`；见 KTD-5。
- Q4（memory `_tenantId` 源 + read 上 tenant 隔离）：从 `RequestContext.TenantID` 读取，并在读/写两侧均做隔离；见 KTD-6。

剩余 implementation-time 细节见下方 Planning Contract 的 `Deferred to Implementation` 与 `Assumptions`。

### Sources / Research

- Origin：`docs/design/draft.md`（Phase 2 plan），branch `feat/go-phase1-ontology-ir`，`docs/plans/2026-08-10-001-feat-go-phase1-ontology-ir-plan.md`。
- Grounding dossier：`/tmp/compound-engineering/ce-brainstorm/phase2-grounding.md`（148 行 extraction-only，file:line quotes）。
- TS behavioral reference：
  - `packages/engine/src/objects/object-manager.ts` — TS Engine object 动词及步骤顺序（create:85→91→107；update:174→187→192→198→221；delete:252→264→267）。
  - `packages/engine/src/links/link-manager.ts` — TS link 动词；cardinality advisory `enforceCardinality`（87）；`_engineLinkId` UUIDv7 生成（92）。
  - `packages/engine/src/index.ts` — TS Engine public surface；Phase 2 仅移植前二者。
  - `packages/storage-memory/src/memory-storage-provider.ts` — TS memory map 结构（245-256）、update/create/soft-delete 行为、`MemoryTransaction`（125-239）。
  - `packages/engine/package.json:23-29` — `@openfoundry/storage-memory` devDependency only；prod deps 为 `@openfoundry/spi`、`@openfoundry/odl`、`@openfoundry/observability`。
  - `tests/spi-conformance/src/categories/{crud,links,...}.ts` — 十个类别；Phase 2 一致性仅镜像 `crud.ts` 与 `links.ts` 之 intent 六动词子集。
- Go Phase 1 foundation（已对源码 verified）：
  - `runtime/spi/provider.go:4-37` — 全 `StorageProvider` interface；`DeleteObject` 携带 `mode string`。
  - `runtime/spi/unimplemented.go:14,38,58,70` — stub 模式返回 `fmt.Errorf("%w: %s", ErrUnimplemented, methodName)`。
  - `runtime/spi/errors.go:6` — `var ErrUnimplemented = errors.New("openfoundry: unimplemented")`。
  - `runtime/spi/ontology.go:9-10,16,19,57-63` — `RequestContext.TenantID`、`OntologyObject`、`OntologyLink`、`LinkTypeDefinition{FromType,ToType}`。
  - `runtime/ir/validate.go:34` — `func Validate(o *Ontology) error`。
  - `runtime/ir/ontology.go:104,113,156,167,177` — `ObjectType`、`LinkType`、`Ontology`、`ObjectByName`、`LinkByName`。
  - `runtime/storage/memory/provider.go` — Phase 1 已实现四方法（`ApplySchema`、`GetSchema`、`HealthCheck`、`Capabilities`）；其余 SPI 方法 fall through 至 `ErrUnimplemented`。
- External：Phase 2 不引入新外部 Go 依赖。UUIDv7 从 TS `packages/engine/src/links/uuidv7.ts`（约 40 行，依赖 `crypto.randomBytes`）移植到 `runtime/internal/uuidv7`，使用 stdlib `crypto/rand`。

## Planning Contract

### Key Technical Decisions

- **KTD-1. Engine-first 层级。** Engine 在 `runtime/engine` 实现，持有 `spi.StorageProvider` + `*ir.Ontology`，由 `New(storage, ontology)` 注入并在构造时调用 `ir.Validate(ontology)` 一次。Engine 从不直接 import `runtime/storage/memory`，仅依赖 `spi` 接口。Phase 2 不拆分 `ObjectManager`/`LinkManager`——六个动词放一个 `Engine` 结构上，Phase 3 可重构。
- **KTD-2. UUIDv7 零依赖移植。** RFC 9562 generator 落在 `runtime/internal/uuidv7`，仅用 stdlib `crypto/rand`、`encoding/hex`、`fmt`。Engine 用它生成 `_engineLinkId`；memory 对 link 上 honor 该 `_engineLinkId`，对 object 上用它内部生成 `_id`（对齐 TS `_doCreateObject:346` 用 `genId()` 与 `_doCreateLink:432` honor `_engineLinkId`）。保持 Phase 1 的最小依赖姿态：`go.mod` 不增 require。
- **KTD-3. Hard-delete-only DeleteObject。** memory 仅实现 `mode="hard"`；`mode="soft"` 返回 `fmt.Errorf("%w: DeleteObject soft mode not supported in Phase 2", ErrUnimplemented)`，使软删路径走显式而不静默。软删 + `_deletedAt` + 版本自增 → Phase 3。Hard delete 是幂等的——已不存在的 id 删除无错，跨租户删除是 no-op（mirror TS `_doHardDeleteObject:409-420`）。
- **KTD-4. Update 合并 patch，不替换。** memory `UpdateObject` 在 existing 对象上合并 patch（系统字段在最后 re-stamp），与 TS `_doUpdateObject:377-386` 一致。Engine 在 update 上的 merge 仅用于 validation；storage 上 merge 是 authoritative。
- **KTD-5. 两层 Validation。** `ir.Validate` 一次调用（构造时，catch TBox）。每动词的 Engine 侧轻量校验器（U4 实现）：类型存在于 IR、字段角色接受 payload（Properties/Params 接受 scalar types、LinkNav/Computed 拒绝写入 payload）、Create 时 required 字段存在、scalar 类型匹配 `ir.TypeRef`。**不**移植 TS `validateObjectProperties`，不主张 TS 校验行为 parity。
- **KTD-6. Memory 边界租户隔离。** memory 在每次读（`GetObject`/`GetLink`/`UpdateObject`/`DeleteObject`/`UpdateLink`/`DeleteLink`）上比对 stored `_tenantId` 与 `ctx.TenantID`；失配 → 返回带类型 not-found（与 TS `_doUpdateObject:366`、`_doHardDeleteObject:413` 一致）。Engine 不直接做租户检查。
- **KTD-7. Engine 写前读。** Update/Delete Object、CreateLink、Delete Link 各在变更前先调等相关 read（`GetObject`/`GetLink`），在 storage 写入之前 fail-fast 抛出带类型 not-found；与 TS Engine 步骤顺序一致。

### High-Level Technical Design

CREATE LINK 是六个动词中步骤最丰富的，下图给出 Engine→StorageProvider→memory 的调用时序；其他动词遵循同形（校验 → 写前读（如需要）→ storage 写入 → 返回）。Engine 在构造时已持 `*ir.Ontology` 并 `ir.Validate` 过。

```mermaid
sequenceDiagram
  participant Caller
  participant Engine
  participant Storage as spi.StorageProvider
  participant Memory
  Note over Engine: holds *ir.Ontology; ir.Validate at New()
  Caller->>Engine: CreateLink(type, fromId, toId, props, ctx)
  Engine->>Engine: lookup linkDef in ir.Ontology
  alt linkDef missing
    Engine-->>Caller: INVALID_LINK_TYPE
  end
  Engine->>Storage: GetObject(linkDef.From, fromId, ctx)
  alt not found (or tenant mismatch)
    Engine-->>Caller: OBJECT_NOT_FOUND
  end
  Engine->>Storage: GetObject(linkDef.To, toId, ctx)
  alt not found
    Engine-->>Caller: OBJECT_NOT_FOUND
  end
  Engine->>Engine: linkId = uuidv7.New()
  Engine->>Storage: CreateLink(ctx, type, fromId, toId, props + _engineLinkId)
  Memory->>Memory: strip _engineLinkId; stamp sys fields; tenant stamp; store type:id
  Storage-->>Engine: OntologyLink
  Engine-->>Caller: OntologyLink
```

### Assumptions

- ASN1. `ir.Validate` 已覆盖 Engine 不需重复的 TBox 检查（每 LinkType 的 from/to 类型存在、每 ObjectType 恰有一个 Primary）。每动词验证不重写这些。
- ASN2. 每动词 validation 刻意最小——**无** TS-parity `validateObjectProperties` 检查（无 constraint expressions、无 immutable 字段、无 field-level validators）。可能比 TS engine 宽松：记入 Deferred to Implementation 留作 Phase 3 候选。
- ASN3. Engine 每次 Update 都传 `expectedVersion=nil`；静默忽略而非冲突是 Phase 2 刻意选择（详见 KTD-3 + Deferred）。
- ASN4. memory hard delete 幂等；跨租户 hard delete 是 no-op（mirror TS `_doHardDeleteObject:417`）。
- ASN5. memory `CreateLink` 从已 apply 的 `OntologySchema` 找 `LinkTypeDefinition` 标记 `_fromType`/`_toType`；若 schema 未 apply 或 link type 未在 schema 中，default 为 "unknown"（mirror TS `_doCreateLink:434-435`）。
- ASN6. 七个实现方法以 `Runtime/storage/memory/provider.go` 的 `sync.Mutex` 单锁串行化；Phase 2 不上 RWMutex 或更细粒度锁，因为读多写少尚未成为压力。

## Implementation Units

### U1. Foundation — UUIDv7 helper + Engine struct

- **Goal:** RFC 9562 UUIDv7 generator 在 `runtime/internal/uuidv7` 中实现；Engine 结构持 `spi.StorageProvider` + `*ir.Ontology`；构造时调用 `ir.Validate` 一次。
- **Requirements:** R7, R8。
- **Dependencies:** 无（Phase 1 已提供 `spi`、`ir`、storage/memory 骨架）。
- **Files:**
  - Create: `runtime/internal/uuidv7/uuidv7.go`
  - Create: `runtime/internal/uuidv7/uuidv7_test.go`
  - Create: `runtime/engine/engine.go`（结构 + `New`，无动词体）
  - Create: `runtime/engine/engine_test.go`（构造测试）
- **Approach:** 移植 TS `generateUUIDv7`（`packages/engine/src/links/uuidv7.ts`，约 40 行；TS 用 Node `crypto.randomBytes`、Go 用 `crypto/rand.Read`）。`func New() string` 返回 lowercase hyphenated UUIDv7。Engine：`type Engine struct { storage spi.StorageProvider; ontology *ir.Ontology }; func New(storage spi.StorageProvider, ontology *ir.Ontology) (*Engine, error)`；构造时 `if err := ir.Validate(ontology); err != nil { return nil, err }`。Engine 不持有 schema projection（memory 持）。
- **Patterns to follow:** TS `packages/engine/src/links/uuidv7.ts`；`runtime/ir/validate.go:34` 的 `Validate` signature。
- **Test scenarios:** **Happy path**：以合法 IR + memory 构造 Engine，无错误。**Error path**：以 `ir.Validate` 会拒绝的 IR 构造 Engine，`New` 返回 Validate 错误。**UUIDv7 正则**：每个 id 严格匹配 `^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`。**UUIDv7 唯一性**：N=10000 连续生成全部不同。**UUIDv7 时间戳单调**：相邻 ids 的前 12 hex 字符非递减。**无新依赖**：Phase 2 后 `go.mod` 的 require 项与 Phase 1 一致。
- **Verification:** `go test ./internal/uuidv7/... ./engine/...` 绿；`go.mod` 仍只 require `gqlparser/v2` 与 `yaml.v3`。

### U2. Memory — object 持久化（4 个 SPI override）

- **Goal:** 在 memory.Provider 上 override `CreateObject`、`GetObject`、`UpdateObject`、`DeleteObject`；以 `type:id` 为键的 mutex-protected map 背书。
- **Requirements:** R9, R10, R2, R3, R4 backend。Covers F1–F4 后端。
- **Dependencies:** U1（UUIDv7 生成 object id）。
- **Files:**
  - Modify: `runtime/storage/memory/provider.go`（加 objects map 字段 + 4 个 override + clone helper）
  - Create: `runtime/storage/memory/provider_object_test.go`
- **Approach:** `Provider` 加 `objects map[string]spi.OntologyObject` 字段（key `type:id`）。复用 clone-on-return 模式（Phase 1 `cloneSchema` 用 JSON marshal）。`CreateObject`：`_id=uuidv7.New()`、`_type=typ`、`_tenantId=ctx.TenantID`、`_createdAt=_updatedAt=now()`、`...properties`。`GetObject`：missing 或 tenant 失配 → 带类型 not-found 错误（`fmt.Errorf("%w: %s/%s not found", ErrObjectNotFound, typ, id)` 或沿用 Phase 1 错误风格）。`UpdateObject`：existing 缺失/tenant 失配 → not-found；合并 patch（系统字段 re-stamp at end）；推进 `_updatedAt`；`expectedVersion` 静默忽略（KTD-3）。`DeleteObject`：`mode=="hard"` → 从 map 删除（幂等）；`mode!="hard"` → 返回 `fmt.Errorf("%w: DeleteObject soft mode not supported in Phase 2", ErrUnimplemented)`；跨租户失配 → no-op 或 not-found。所有 mutation 都在 `mu.Lock()` 下。
- **Patterns to follow:** TS `_doCreateObject`(345-361)、`_doUpdateObject`(363-390)、`_doHardDeleteObject`(409-420)；Phase 1 `cloneSchema` 模式。
- **Test scenarios:** **Happy Create+Get 往返**（`Covers AE1` 的 memory 半边）：创建返回带系统字段的对象；Get 读回相同字段。**Get 跨租户隔离**：tenant A 创建；tenant B Get 返回 not-found 错误。**Get 缺失** → 带类型 not-found。**Update 合并**：create `{name:"x"}`、update `{name:"y"}` → 读回 `{name:"y"}`。**Update 保留系统字段**：`_id/_type/_tenantId/_createdAt` 不变；`_updatedAt` 严格更新。**Update 缺失** → not-found。**Update 跨租户缺失** → not-found（无泄漏）。**Hard Delete 移除**：随后 Get 缺失。**Hard Delete 幂等**：删已不存在的 id 无错。**Soft Delete 不支持**（`Covers AE8` 的 DeleteObject 子项）：`DeleteObject(mode="soft")` 返回 ErrUnimplemented 风味错误。**Hard Delete 跨租户**：tenant A 创建、tenant B 删除 → no-op（仍能从 tenant A 读到）。
- **Verification:** `go test ./storage/memory/...` 绿；`QueryObjects`/`BeginTransaction`/`GetObjectAtVersion` 等仍经 `UnimplementedStorageProvider` 返回 `ErrUnimplemented`。

### U3. Memory — link 持久化（3 个 SPI override）

- **Goal:** 在 memory.Provider 上 override `CreateLink`、`GetLink`、`DeleteLink`。
- **Requirements:** R5, R6 后端, R11。
- **Dependencies:** U1（UUIDv7）。
- **Files:**
  - Modify: `runtime/storage/memory/provider.go`（加 links map 字段 + 3 个 override + `_engineLinkId` strip）
  - Create: `runtime/storage/memory/provider_link_test.go`
- **Approach:** 加 `links map[string]spi.OntologyLink`（key `type:id`）。`CreateLink`：从 `properties` 读 `_engineLinkId`，若为 string 用之，否则 `uuidv7.New()`；从 `properties` strip `_engineLinkId`（mirror TS `_doCreateLink:437`）；查已 apply 的 `OntologySchema` LinkTypeDefinitions（按 type 名）取 `fromType`/`toType`，未找到 default `"unknown"`；戳 `_id`、`_type=typ`、`_fromId`、`_toId`、`_fromType`、`_toType`、`_tenantId=ctx.TenantID`、`_createdAt=_updatedAt=now()`、余下用户属性。`GetLink`：missing 或 tenant 失配 → not-found。`DeleteLink`：硬删从 map 删，幂等，跨租户 no-op。
- **Patterns to follow:** TS `_doCreateLink`(422-453)、`_doDeleteLink`(488-495)；`link-manager.ts:95` 的 `_engineLinkId` 透传。
- **Test scenarios:** **Happy Create Link 接受 `_engineLinkId`**：存储的 `_id` 等于传入 `_engineLinkId`；用户属性中无 `_engineLinkId`。**Happy Create Link 无 `_engineLinkId`**：内部生成 UUIDv7。**Create Link 标 `_fromType`/`_toType`**：apply 一个 fixture schema（含一个 link type），create link，`_fromType`/`_toType` 等于 schema 中定义。**GetLink 往返**。**GetLink 跨租户隔离**。**GetLink 缺失** → not-found。**Hard Delete Link 移除**：后续 GetLink 缺失。**Hard Delete Link 幂等**。**Hard Delete Link 跨租户**：no-op，留给 rightful tenant。
- **Verification:** `go test ./storage/memory/...` 绿；`GetLinks`/`Traverse`/`UpdateLink` 仍返回 `ErrUnimplemented`。

### U4. Engine — object 动词 + 每动词 validation

- **Goal:** Engine 上实现 `CreateObject`/`GetObject`/`UpdateObject`/`DeleteObject`；含每动词 Engine 侧轻量 validation。
- **Requirements:** R1, R2, R3, R4, R7。Covers F1–F4, AE1–AE4。
- **Dependencies:** U1（Engine 结构）, U2（memory object ops）。
- **Files:**
  - Modify: `runtime/engine/engine.go`（加 4 个动词 + engine 内部 perverb validator helper）
  - Create: `runtime/engine/objects_test.go`
- **Approach:** engine 在 `ontology.ObjectByName(type)` 查类型有效性（缺失 → `INVALID_OBJECT_TYPE` 风味错）。每动词 validator 检查：Create 上 required 字段存在；字段角色检查（Properties/Params 接受 scalar、Param/LinkNav/Computed 拒绝写入 payload）；scalar 类型对 `ir.TypeRef` 匹配。Create：先校验，再 `storage.CreateObject`。Get：`storage.GetObject`，透传。Update：`storage.GetObject` 先（缺失 → `OBJECT_NOT_FOUND`）；合并 patch（剔除系统字段）；校验合并；`storage.UpdateObject(ctx, type, id, merged, nil)`。Delete：`storage.GetObject` 先（缺失 → `OBJECT_NOT_FOUND`）；`storage.DeleteObject(ctx, type, id, "hard")`。动词 return 前留一个 comment stub 标注 `// TODO(Phase 3): emitObject* via event bus`，位置不动 TS step order。
- **Patterns to follow:** TS ObjectManager `create`(73-117)、`update`(159-232)、`delete`(238-275)；engine 的 event hook 留 stub。
- **Test scenarios:** **Covers AE1**：Create+Get 往返；返回带系统字段。**Error Create 未知类型**：`CreateObject("Nonexistent", {}, ctx)` 返回 validation 错误（storage 不被调用）。**Error Create 缺 required**：fixture IR 上 Supplier 必填 name，create 不带 name 返回 validation 错误。**Error Create 含 LinkNav/Computed 角色**：以一个链接字段或 computed 字段 payload 创建 → 拒绝。**Happy Get**：读回 stored 对象。**Error Get 缺失**：带类型 not-found，不发生 storage 写入。**Covers AE2**：Update 反映 patch；合并而非替换。**Covers AE4**：Update 缺失 id → `OBJECT_NOT_FOUND` 在 `storage.UpdateObject` 之前。**Happy Hard Delete**（`Covers AE3`）：后续 Get not-found。**Error Delete 缺失**：`OBJECT_NOT_FOUND` 在 `storage.DeleteObject` 之前。**接口不耦合**：编译期证明 `Engine` 仅持有 `spi.StorageProvider`，不 import `runtime/storage/memory`。
- **Verification:** `go test ./engine/...` 绿；Engine 仅依赖 `spi`；test 用一个内存 fixture IR + 内存或假 provider 完成。

### U5. Engine — link 动词 + 引用完整性

- **Goal:** Engine 上实现 `CreateLink`/`DeleteLink` 与 `GetLink` 透传；引用完整性经 `GetObject` 检查。
- **Requirements:** R5, R6。Covers F5, F6, AE5–AE7。
- **Dependencies:** U1（Engine + UUIDv7）, U4（复用 Engine 内部 GetObject 调用）, U3（memory link ops）。
- **Files:**
  - Modify: `runtime/engine/engine.go`
  - Create: `runtime/engine/links_test.go`
- **Approach:** CreateLink：`ontology.LinkByName(type)` 查 link 类型（缺失 → `INVALID_LINK_TYPE`）；读 `linkDef.From`/`linkDef.To`；`storage.GetObject(linkDef.From, fromId, ctx)`（缺失 → `OBJECT_NOT_FOUND`）；对 `to` 重复；`linkId = uuidv7.New()`；把 `_engineLinkId: linkId` 合入 properties；`storage.CreateLink(ctx, type, fromId, toId, props-with-engineLinkId)`。**跳过 `enforceCardinality`**（KTD scope 延后）；不调 `GetLinks`。DeleteLink：`storage.GetLink(ctx, type, linkId)`（缺失 → `LINK_NOT_FOUND`）；`storage.DeleteLink(ctx, type, linkId)`。GetLink：透传。
- **Patterns to follow:** TS LinkManager `createLink`(62-113)、`deleteLink`(190-228)；**跳过 `enforceCardinality`(87)** 与 `assertObjectExists` 的细节差异由 Engine 侧 GetObject 处理。
- **Test scenarios:** **Covers AE5**：Create Link 返回带系统字段的链接。**Error Create Link 未知类型**：返回 `INVALID_LINK_TYPE` 风味错，storage 不被调。**Covers AE6（from）**：from-object 缺失 → `OBJECT_NOT_FOUND` 在 `storage.CreateLink` 之前。**Covers AE6（to）**：to-object 缺失 → `OBJECT_NOT_FOUND`。**Happy Engine 生成 `_engineLinkId`**：UUIDv7 new + 合入 properties；memory 存储的 `_id` 等于它。**Happy GetLink 往返**。**Error GetLink 缺失** → `LINK_NOT_FOUND`。**Covers AE7**：Hard Delete Link 移除；后续 GetLink not-found。**Error Delete Link 缺失**：`LINK_NOT_FOUND` 在 `storage.DeleteLink` 之前。
- **Verification:** `go test ./engine/...` 绿；CreateLink 不调 `GetLinks`（cardinality 延后）。

### U6. Conformance 子集 + Gold Path

- **Goal:** Go-native 一致性 harness 镜像 `crud.ts` + `links.ts` 的 intent，覆盖六个动词；ErrUnimplemented floor 断言；supply-chain 端到端 Gold Path。
- **Requirements:** R14, R15, R16, AE8, AE9。
- **Dependencies:** U1–U5 全部。
- **Files:**
  - Create: `runtime/conformance/conformance_test.go`
  - Create: `runtime/e2e/supply_chain_test.go` 或在 Phase 1 `runtime/pack/gold_test.go` 上扩展
- **Approach:** 一致性 harness 用 `setup()` 生成小型 fixture IR（一个 `Supplier` 对象类型 带 `name` required、一个 `Supplies` 链接类型 from Supplier to Part），wire Engine + memory。每动词往返断言覆盖 AE1–AE7。ErrUnimplemented floor：遍历 Phase 2 **未**实现的 SPI 方法（`QueryObjects`、`AggregateObjects`、`SearchObjects`、`BulkMutate`、`UpdateLink`、`GetLinks`、`Traverse`、`BeginTransaction`、`GetObjectAtVersion`、`GetObjectAtTime`、`EnsureIndex`、`DropIndex`、`ListIndexes`）逐一断言返回 `errors.Is(err, ErrUnimplemented)=true`。Gold Path：复用 Phase 1 `runtime/pack/loader.go` → 取 `domain-packs/supply-chain` → IR → `projection.ProjectStorage` → `memory.ApplySchema` → Engine verbs 按 F7；单 integration test 断言无错误且七个实现方法零 `ErrUnimplemented` 命中。
- **Patterns to follow:** TS `tests/spi-conformance/src/categories/{crud,links}.ts` 的断言形状；Phase 1 `runtime/pack/loader_test.go` 的 pack-load 模板。
- **Test scenarios:** **AE1–AE7 继承为标记断言**（`Covers AE1`…`AE7`）。**Covers AE8**：ErrUnimplemented floor 跨 13+ 个未实现 SPI 方法上的 `errors.Is` 与 message-name 断言。**Covers AE9 / F7**：supply-chain Gold Path end-to-end；无错误；七个实现方法零 `ErrUnimplemented` 命中。**Edge**：Gold Path 不复制 `domain-packs` 进 `runtime/`（路径解析走 repo-root）。
- **Verification:** `go test ./...` 在 `runtime/` 下全绿；Gold Path 不复制 `domain-packs`。

## Verification Contract

| AE / Requirement | Verifying suite |
|---|---|
| AE1 (Create+Get object round-trip) | `runtime/engine/objects_test.go` + `runtime/storage/memory/provider_object_test.go` |
| AE2 (Update reflects patch) | `runtime/engine/objects_test.go` |
| AE3 (Hard delete removes) | `runtime/engine/objects_test.go` |
| AE4 (Update on missing id fails before storage) | `runtime/engine/objects_test.go` |
| AE5 (Create+Get link round-trip) | `runtime/engine/links_test.go` + `runtime/storage/memory/provider_link_test.go` |
| AE6 (Create link referential integrity) | `runtime/engine/links_test.go` |
| AE7 (Delete link removes) | `runtime/engine/links_test.go` |
| AE8 (ErrUnimplemented floor) | `runtime/conformance/conformance_test.go` |
| AE9 / F7 (supply-chain Gold Path) | `runtime/e2e/supply_chain_test.go` |
| R12 (其余 SPI 经 ErrUnimplemented) | U6 conformance#errorFloor + AE8 |
| R14 / R15 (Go-native 一致性子集) | `runtime/conformance/conformance_test.go` |

**Repo command (实施时执行)：** `cd runtime && go test ./...`。

## Definition of Done

- `cd runtime && go test ./...` 全绿。
- memory.Provider 上 Phase 2 恰加 7 个 SPI override；其余 13+ 个仍经 `UnimplementedStorageProvider` 返回 `ErrUnimplemented` 且方法名在消息中。
- `runtime/go.mod` 不增新外部 require 项（仅 stdlib + Phase 1 两个模块）。
- Engine 与 memory 互不直接 import；两者均 import `runtime/spi`（接口边界）。
- Gold Path F7 不复制 `domain-packs/supply-chain` 进 `runtime/`；路径走 repo-relative。
- Phase 1 schema 测试仍绿（`runtime/spi`、`runtime/storage/memory` 的 schema 往返）。
- Product Contract R1–R16 / AE1–AE9 / F1–F7 ID 与语义不变；brainstorm Product Contract 仅译文呈现。

## Risks & Dependencies

| Risk | Mitigation |
|---|---|
| `ir.Validate` 只做 TBox——每动词属性形状不被它覆盖 | U4 Engine 侧每动词 validator 解决（KTD-5） |
| memory 读侧租户隔离可能撞 Phase 3 conformance TS multi-tenancy | Phase 2 Go-native 一致性不移植 TS multi-tenancy 类别；推迟到 Phase 3 |
| UUIDv7 移植 bug（RFC 9562 bit 布局） | U1 单测：严格正则 + N=10000 唯一性 + 时间戳单调 |
| memory 未 strip `_engineLinkId` → 泄漏进用户属性 | U3 test：stored `_id` 等于传入；用户属性不含它 |
| Soft delete "mode=soft" 静默成功而非显式错误 | KTD-3 明确返回 ErrUnimplemented 风味错；AE8 覆盖 |
| Phase 2 无 cardinality 强制；TS engine 的 advisory 缺失 | Go-native 一致性不测 1:1 link 重复；记 Q + 推迟到 Phase 3 |
| Engine-feeling 接口层测试 hard-coupling 到 memory | U4 test 用 fixture/memory 或 stub provider，断言 Engine 不 import `runtime/storage/memory` |

## Deferred to Implementation

- 具体 helper 名（如 `ErrObjectNotFound` 是否新增还是复用 `fmt.Errorf("not found: %s/%s", typ, id)`）。
- 是否为 `mode` 非法值（非 "soft" 也非 "hard"）单独返回错误或一律按"非 hard 即不支持"处理。
- Engine per-verb validator 是否做 scalar 类型深查（如 `Int` vs `int64`），还是按 `ir.TypeRef` 命名粗匹配。
- 一致性 harness 是否抽出 `(*Engine, *memory.Provider)` 返回 helper 让 U6 多用例复用，还是每个 test case 独立 setup。
- `_fromType`/`_toType` 在 schema 未 apply 时的 default `"unknown"` 是否值得在 conformance 上断言。
- Engine `New` 是否暴露一个不带 ontology 的 nil-safe 形式以备纯 storage-only 测试。