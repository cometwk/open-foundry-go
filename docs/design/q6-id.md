
### 当前方案（EncodeDirect，编码在存储层）

```
1. 存储层创建对象
   CreateObject("Patient", {name: "Ada"})
   → UUIDv7 = "0198a3f1-7b2c-..."
   → EncodeDirect("Patient", ["0198a3f1-7b2c-..."])
   → base64url({"t":"Patient","k":["0198a3f1-7b2c-..."]})
   → "eyJ0IjoiUGF0aWVudCIsImsiOlsiMDE5OGEzZjEtN2IyYy0uLi4iXX0"

2. 存入数据库
   INSERT INTO patient (id, ...) VALUES ('eyJ0IjoiUGF0aWVudCIsImsiOlsiMDE5OGEzZjEtN2IyYy0uLi4iXX0', ...)

3. GraphQL 返回
   patient(id: "eyJ0IjoiUGF0aWVudCIsImsiOlsiMDE5OGEzZjEtN2IyYy0uLi4iXX0") {
     name      # → "Ada"
   }

4. 如果有 node(id:) 查询
   node(id: "eyJ0IjoiUGF0aWVudCIsImsiOlsiMDE5OGEzZjEtN2IyYy0uLi4iXX0")
   → DecodeDirect("eyJ0...") → type="Patient", key="0198a3f1-7b2c-..."
   → GetObject(ctx, "Patient", "0198a3f1-7b2c-...")
```

存储层把 type 编码进了 id 字符串。GraphQL 层只是原样透传（`node.go:119`: `graphql.ID(n.idString())`）。

---

### 新方案（presentation 层编码，存储层存裸 UUID）

```
1. 存储层创建对象
   CreateObject("Patient", {name: "Ada"})
   → UUIDv7 = "0198a3f1-7b2c-..."
   → 直接存入 id 列，不编码

2. 存入数据库
   INSERT INTO patient (id, ...) VALUES ('0198a3f1-7b2c-...', ...)

3. GraphQL 层编码 global ID（presentation 层做）
   存储层返回 _id = "0198a3f1-7b2c-..."
   GraphQL resolver 知道当前类型 = "Patient"
   → globalID = base64("Patient:0198a3f1-7b2c-...")
   → "UGF0aWVudDowMTk4YTNmMS03YjJjLS4uLg=="

   返回给客户端：
   patient(id: "UGF0aWVudDowMTk4YTNmMS03YjJjLS4uLg==") {
     name      # → "Ada"
   }

4. 如果有 node(id:) 查询
   node(id: "UGF0aWVudDowMTk4YTNmMS03YjJjLS4uLg==")
   → base64decode("UGF0aWVudDowMTk4YTNmMS03YjJjLS4uLg==")
   → "Patient:0198a3f1-7b2c-..."
   → split(":") → type="Patient", id="0198a3f1-7b2c-..."
   → GetObject(ctx, "Patient", "0198a3f1-7b2c-...")
```

**存储层存的是裸 UUID，GraphQL 层在出/入边界做 `base64(type:id)` 编解码。**

---

### 两条路径对比

```
                    EncodeDirect 方案              Presentation 编码方案
                    ─────────────────              ────────────────────
存储层 _id           eyJ0IjoiUGF0aWVudCIs...        0198a3f1-7b2c-...
数据库 id 列         eyJ0IjoiUGF0aWVudCIs...        0198a3f1-7b2c-...
JOIN 条件            WHERE from_id = 'eyJ0...'      WHERE from_id = '0198...'
GraphQL 返回的 id    eyJ0IjoiUGF0aWVudCIs...        UGF0aWVudDowMTk4...
客户端看到的 id      eyJ0IjoiUGF0aWVudCIs...        UGF0aWVudDowMTk4...
最终 GetObject       GetObject("Patient", "eyJ0..") GetObject("Patient", "0198..")
```

注意两列的**最后一行完全一致**——都走到 `GetObject(ctx, "Patient", "0198a3f1-7b2c-...")`。区别只是：

- EncodeDirect：编码发生在**存储层**，`id` 列里存的是编码后的 blob
- Presentation：编码发生在 **API 边界**，`id` 列里存的是裸 UUID，编码只在 HTTP 响应/请求时做一次

---

### 为什么 GraphQL 客户端不关心这个差异

GraphQL 规范定义 `ID` 类型为 **opaque string**——客户端不应该对 ID 的内部格式做任何假设。客户端拿到 `"UGF0aWVudDowMTk4..."` 就当不透明字符串用，下次查询原样传回来。服务端自己编解码。

这就是 Hasura、Postgraphile、Shopify、GitHub 的做法：

```
GitHub:      base64("04:User:12345")       → "MDQ6VXNlcjoxMjM0NQ=="
Hasura:      base64("patient:0198...")      → "cGF0aWVudDowMTk4..."
Open Foundry: base64("Patient:0198...")     → "UGF0aWVudDowMTk4..."
```

格式可以变，对客户端完全透明。

---

### 代码改动量

当前 `node.go` 已经知道 type 和 id：

```go
// node.go:32
func (n *node) idString() string {
    s, _ := n.obj[spi.FieldID].(string)
    return s
}
```

改动只需在**出方向**（resolver 返回 ID 时）加一行编码，在**入方向**（解析 client 传入的 ID 时）加一行解码：

```go
// 出方向：resolver 返回 id 时
func globalID(typ, id string) graphql.ID {
    return graphql.ID(base64.StdEncoding.EncodeToString([]byte(typ + ":" + id)))
}

// 入方向：node(id:) resolver 解析时
func parseGlobalID(gid graphql.ID) (typ, id string, err error) {
    raw, err := base64.StdEncoding.DecodeString(string(gid))
    // ...
    parts := strings.SplitN(string(raw), ":", 2)
    return parts[0], parts[1], nil
}
```

存储层、OBDA、SPI 完全不需要知道 `base64` 的存在。
