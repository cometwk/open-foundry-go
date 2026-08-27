---
date: 2026-08-20
topic: ir-dynamic-object-resolvers
amends: docs/brainstorms/2026-08-20-go-phase6-query-ir-traverse-requirements.md
---

# Requirements: IR-dynamic GraphQL object resolvers

## Summary

GraphQL ObjectType 字段从 Ontology IR 在启动时绑定，不再在 `runtime/api/node.go` 为每个 pack 字段写死方法。Query 根已经按 IR 动态绑定；对象字段、Relay `node`、search hit 走同一套。对外 GraphQL/REST 契约与 Query IR 执行规则不变。

---

## Problem Frame

002 计划把嵌套 `@link` 接到 Query IR，但对象 resolver 仍是 supply-chain（外加合成 fixture）字段方法的并集。新 ODL 字段或合成 `@link` 名若不改 `node.go`，`ParseSchema` 失败或字段解析不到。`TestNode_CoversSupplyChainFields` 把这层硬编码锁成了安全网，也锁住了可扩展性。

Query 根已用 `reflect.StructOf` 按 ObjectType 绑定 get/list/aggregate/search。对象层仍落后。

---

## Key Decisions

- **启动时按 IR 绑定一个对象 resolver 并集。** 所有 ObjectType 的 GraphQL 字段出现在同一运行时类型上，与今天共享 `*node` 方法集相同；闭包携带 `{engine, type, object}`，按该类型上的字段 Role 分派。
- **不按 ObjectType 各生成一套 Go 类型。** Product ↔ Supplier 等环会迫使递归类型图；并集类型复用 Query 根已验证的模式。
- **列表与 search 的 `node` 必须是同一运行时对象类型。** 否则 `products { sku }` / `searchProducts { node { sku } }` 会丢字段。
- **不改公开契约。** 嵌套 `@link`、1 跳 `GetLinks` / 更深 `Traverse`、REST follow、SDL 形状、空值为 GraphQL `null` 均保持 002。
- **不改 TypeScript。**

---

## Requirements

**Binding**

- R1. GraphQL ObjectType 上的每个 Ontology IR 字段都能解析，无需为该字段名在 Go 源码中增加方法。
- R2. 绑定发生在 `api.New`，输入是当前 Engine 的 Ontology IR。换 pack / 合成 IR 不改 `runtime/api` 源码。
- R3. 分派按字段 Role：`@link` 走既有 Expand / `GetLinks`/`Traverse`；对象型 FK 走 Get；`@computed` 走 `ComputeField`；标量与 primary 从对象属性读。
- R4. Go 返回值的空值与 SDL 一致：非空标量不是指针；可空标量与 FK / 单数 `@link` 用指针；非空列表为空切片而不是 GraphQL `null`。

**Surfaces**

- R5. get-by-id、Relay 列表 `edges.node`、per-type search `hits.node`、嵌套 `@link` 与 FK 展开，都使用同一套对象字段绑定。
- R6. Query 根仍按 IR 动态绑定；本需求不把 Query 根改回静态结构体。

**Compatibility**

- R7. 002 的 GraphQL 执行规则与 REST follow 行为不变。
- R8. 本轮不修改 TypeScript。

---

## Acceptance Examples

- AE1. **Covers R1, R2.** Given 仅存在于 ODL / 合成 IR、且 `runtime/api` 源码中没有对应方法名的字段。When `api.New` 解析该 ontology 的 schema 并查询该字段。Then ParseSchema 成功且响应含该字段值。

- AE2. **Covers R1, R3.** Given 合成分叉 IR（`a { b { c { name } d { name } } }`），Go 源码无 `B`/`C`/`D`/`Leaf` 方法。When 执行该查询。Then 形状与 002 AE3 相同；两次 `Traverse`，无 `GetLinks`。

- AE3. **Covers R5.** Given supply-chain Product。When `products { sku }` 与 `product(id) { sku }`。Then 两处都能读到 `sku`。

- AE4. **Covers R7.** Given 已 seed 的 1 跳 `suppliers` 与 2 跳 `inventoryRecords { trackedProduct }`。When 执行原金路径。Then 1 跳仍 `GetLinks`，2 跳仍 `Traverse`；无 InventoryOf 时 `trackedProduct` 为 null、FK `product` 有值。

---

## Success Criteria

- 任意本 Phase 可 `ParseSchema` 的 pack，对象字段不依赖 `runtime/api` 里的硬编码方法名。
- 002 金路径与合成分叉/混排测试仍然通过。

---

## Scope Boundaries

**In scope**

- GraphQL 对象字段、Relay connection `node`、search hit `node` 的 IR 绑定
- 删除 pack / fixture 专用的硬编码对象字段方法
- 把「IR 字段都有 resolver」的测试从反射 `*node` 方法集改为「任意 IR 可 ParseSchema 并读字段」

**Deferred for later**

- 按 ObjectType 拆分 Go 类型
- GraphQL Mutation / Subscription
- TS planner 动态 resolver

**Outside this change's identity**

- 改公开 SDL 或 REST 信封
- 把 Query IR 执行规则再谈一遍

---

## Dependencies / Assumptions

- Query 根的 `reflect.StructOf` + `UseFieldResolvers` 已能被 graph-gophers 执行。
- 对象字段绑定可以复用现有 `resolveLink` / `resolveFK` / `ComputeField` 与请求内 Expand memo。
- 同一 GraphQL 字段名在不同 ObjectType 上若出现，其 Go 签名必须兼容（今日 pack 满足；冲突时 `New` 失败而不是静默选一个）。

---

## Outstanding Questions

**Deferred to Planning / implementation**

- 运行时类型如何表达「返回自身指针」的 `@link`（实现细节，不改变 R1–R8）

---

## Sources / Research

- Deferred item in `docs/plans/2026-08-20-002-feat-go-phase6-query-ir-traverse-plan.md`：硬编码 `node` 方法
- `runtime/api/schema.go` — Query 根 `reflect.StructOf`
- `runtime/api/node.go` — 硬编码字段方法；`resolveLink` / `resolveFK`
- `runtime/api/pagination.go` — `Edge.Node` / `SearchHit.Node` 现为 `*node`
- `runtime/api/resolvers_test.go` — `TestNode_CoversSupplyChainFields`；合成 `B`/`C`/`D`/`Leaf`
