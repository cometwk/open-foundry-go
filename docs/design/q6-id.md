# Q6: 对外 ID 形态

**决定：存储层与 HTTP 都返回裸 `_id`。现在不做编码。**

`insert: generated` 时 Engine 铸造 UUIDv7，identity 列原样存放。GraphQL 与 REST 原样透传 SPI `_id`。`docs/design/obda-spec-v3.md` §5 与 `docs/brainstorms/2026-09-07-obda-single-column-identity-requirements.md` R12 为准。

---

## 现行路径

```
1. Engine 创建对象
   CreateObject("Patient", {name: "Ada"})
   → UUIDv7 = "0198a3f1-7b2c-..."
   → 写入 patient.id，不编码

2. GraphQL
   query { patient(id: "0198a3f1-7b2c-...") { id name } }
   # 根字段名 patient 就是类型；id 是裸 UUID

3. REST
   GET /api/v1/patient/0198a3f1-7b2c-...
   # 类型在 URL path 里
```

`runtime/api/node.go` 的 `idString()` 直接返回 `obj[_id]`。`schema.go` 只生成 `patient(id:)`、`ward(id:)` 这类 type-specific 根字段，没有 `node(id:)`。

---

## 为什么 client 不需要编码

client 永远不会在「不知道类型」的上下文里使用这个 id。每次 query 的根字段名或 REST path 已经带类型。

主流 GraphQL client（Apollo、Relay、URQL）的缓存 key 默认是 `__typename + id`：

```text
Apollo cache key = "Patient:0198a3f1-7b2c-..."
```

即使两个类型碰巧有相同 UUID，`Patient:xxx` 与 `Ward:xxx` 仍不撞。唯一性在服务端也是 `(type, id)`。

---

## 将来若加 Node 接口

```graphql
query {
  node(id: "0198a3f1-7b2c-...") {    # ← 这里没有类型
    ... on Patient { name }
    ... on Ward { name }
  }
}
```

裸 UUID 无法告诉服务端这是 Patient 还是 Ward。到那时再在 `node` resolver 入口加 `base64(type:id)` 编解码即可。即使有 Node 接口，GraphQL 响应里也始终带 `__typename`。

现在不为它买单。

---

## 已拒绝的方案

**存储层 `EncodeDirect`：** 把 `{"t":"Patient","k":[uuid]}` 打进 PK。JOIN 比较编码 blob，sqlite `_id` 与 memory 裸 UUID 分叉。已删除。

**HTTP presentation 编码：** 存储裸 UUID，出/入边界做 `base64(type:id)`。对当前 schema 没有收益——根字段和 REST path 已带类型。留到 `node(id:)`。
