# URL 查询参数与数据库查询映射规范

本文档定义了一套将 URL 查询字符串映射为数据库查询的标准约定，旨在为 RESTful API 提供一套灵活、可预测且安全的后端数据查询方案。

## 1. 语法概述

查询字符串由键值对构成，键（key）通过特定结构来定义查询操作。

- **基本结构**: `操作[参数1][参数2]=值`
- **分隔符**: 使用 `.` 作为键内部的分隔符，例如 `where.name.eq`。

---

## 2. 过滤 (`where`)

用于向数据库查询添加 `WHERE` 条件。

### 2.1. 基本过滤 (AND)

默认情况下，所有 `where` 条件将通过 `AND` 连接。

- **语法**: `where.列名.操作符=值`
- **示例**: 查询状态为 `active` **且** 年龄大于 `20` 的用户。
  - **URL Query**: `where.age.gt=20&where.status.eq=active`
  - **SQL (示意)**: `WHERE age > 20 AND status = 'active'`

### 2.2. 模糊搜索 (`q`)

系统提供两种便捷的模糊搜索方式，内部使用 `OR` 和 `LIKE` 连接。

1.  **全局模糊搜索**:
    - **语法**: `q=搜索词`
    - **说明**: 后端需预设一个字段列表（如 `['name', 'title']`），��查询会对列表中的所有字段执行 `OR LIKE` 操作。
    - **示例**: `q=keyword`
    - **SQL (示意)**: `WHERE (name LIKE '%keyword%' OR title LIKE '%keyword%')`

2.  **指定字段模糊搜索**:
    - **语法**: `q.列1.列2=搜索词`
    - **说明**: 对指定的 `列1`, `列2` 执行 `OR LIKE` 操作。
    - **示例**: `q.name.email=test`
    - **SQL (示意)**: `WHERE (name LIKE '%test%' OR email LIKE '%test%')`

### 2.3. 操作符列表

当前实现支持以下操作符：

| 操作符 | 描述 | 示例 (URL Query) | SQL (示意) |
|---|---|---|---|
| `eq` | 等于 | `where.name.eq=John` | `name = 'John'` |
| `neq` | 不等于 | `where.name.neq=John` | `name <> 'John'` |
| `gt` | 大于 | `where.age.gt=20` | `age > 20` |
| `lt` | 小于 | `where.age.lt=20` | `age < 20` |
| `gte` | 大于等于 | `where.age.gte=21` | `age >= 21` |
| `lte` | 小于等于 | `where.age.le=21` | `age <= 21` |
| `in` | 包含于 (多值用 `,` 分隔) | `where.status.in=active,pending` | `status IN ('active', 'pending')` |
| `notIn` | 不包含于 (多值用 `,` 分隔) | `where.status.notIn=archived` | `status NOT IN ('archived')` |
| `like` | LIKE 模糊匹配 | `where.name.like=%john%` | `name LIKE '%john%'` |
| `likes`| 多个 LIKE (AND) | `where.name.likes=john,doe` | `name LIKE '%john%' AND name LIKE '%doe%'` |
| `btw` | 在...之间 (用 `,` 分隔) | `where.age.btw=20,30` | `age BETWEEN 20 AND 30` |
| `null` | 是否为 NULL | `where.deleted_at.null=true` | `deleted_at IS NULL` |
| | | `where.deleted_at.null=false` | `deleted_at IS NOT NULL` |
| `time` | UTC时间范围 (格式 `YYYY-MM-DD HH:MM:SS`, 用 `,` 分隔) | `where.created_at.time=2024-01-01 00:00:00,2024-01-31 23:59:59` | `created_at BETWEEN '...' AND '...'` |
| `date` | 本地日期范围 (格式 `YYYY-MM-DD HH:MM:SS`, 用 `,` 分隔) | `where.created_at.time=2024-01-01 00:00:00,2024-01-31 23:59:59` | `created_at >= '...' AND  <='...'` |

### 2.4. 时间处理

- **URL 查询参数**: 所有时间相关的查询参数（如 `time` 操作符）都应使用 **本地时间** 格式。
- **后端处理**: 后端在接收到时间参数后，必须将其从本地时间转换为 **UTC 时间**，然后用于数据库查询。数据库中所有时间字段均存储为 UTC 格式。

---

## 3. 字段选择 (`select`)

指定返回结果中包含哪些字段。

- **语法**: `select=列1,列2`
- **示例**: 只返回 `id` 和 `name` 字段。
  - **URL Query**: `select=id,name`
  - **SQL (示意)**: `SELECT id, name FROM ...`

---

## 4. 排序 (`order`)

控制结果的排序。

- **语法**: `order=列1.方向,列2.方向`
  - `方向` 是可选的，可为 `asc` (升序) 或 `desc` (降序)，默认为 `asc`。
- **示例**: 按 `created_at` 降序，再按 `name` 升序排序。
  - **URL Query**: `order=created_at.desc,name.asc`
  - **SQL (示意)**: `ORDER BY created_at DESC, name ASC`

---

## 5. 分页

通过参数控制返回数据的分页。

- **语法**: `page=页码&pagesize=每页数量`
- **说明**:
  - `page` 默认为 `0` 或 `1` (取决于后端实现)，`pagesize` 默认为 `10`。
  - 后端应限制 `pagesize` 的最大值（如 `500`）以防���用。
- **示例**: 获取第 2 页，每页 20 条数据。
  - **URL Query**: `page=2&pagesize=20`
  - **SQL (示意)**: `LIMIT 20 OFFSET 20`

---

## 6. 关联查询


TODO:
- `取消 # 符号，它在url规则中，有特殊含义 `
- `~ 符号, user~name`

当前实现对关联查询的支持有限，仅支持对单层关联表的字段进行过滤。

- **语法**: `where.表名#列名.操作符=值`
- **说明**: 使用 `#` 连接关联表名和列名。后端代码会将其转换为 `表名.列名`。
- **示例**: 查询关联的 `user_profile` 表中 `city` 为 'Beijing' 的记录。
  - **URL Query**: `where.user_profile#city.eq=Beijing`
  - **SQL (示意)**: `... WHERE user_profile.city = 'Beijing'`
- **注意**: 
  - 复杂的多层关联（如 `where.othertable.friends.status.eq=enabled`）当前**不被支持**。
  - 只支持简单的left join 关联查询.
  - 功能未实现，好像还得先定义关联 on xxx.field = yyy.field 关系才行
  - 目前需要手工设置 `x.field=y.field`

---

## 7. 未来规划：OR 分组查询

> **注意**: 以下为设计规划，当前版本尚未实现。

为了支持更复杂的 `(A OR B) AND C` 逻辑，计划引入 `OR` 分组。

- **语法**: `or[分组号].列名.操作符=值`
- **说明**:
  - 同一个分组号内的条件将用 `OR` 连接。
  - 不同分组号之间、以及 `or` 分组与 `where` 条件之间，将用 `AND` 连接。
- **示例**: 查询 (`status` 为 `pending` **或** `review_needed`) **且** `age` 大于 60 的用户。
  - **URL Query**: `or[1].status.eq=pending&or[1].status.eq=review_needed&where.age.gt=60`
  - **SQL (示意)**: `WHERE (status = 'pending' OR status = 'review_needed') AND age > 60`

---

## 8. 安全建议

- **字段白名单**: **严禁**将用户传入的任何字段名、表名直接用于 SQL 查询。后端必须为每个模型维护一个允许查询、过滤和排序的字段白名单。任何不在白名单内的参数都应被忽略或直接拒绝。
- **输入验证**: 所有传入的值都应经过严格的类型校验和净化，以防止 SQL 注入。
- **查询复杂度限制**: 考虑对查询的复杂度进行限制（例如，限制 `in` 操作符的值的数量），以防止拒绝服务攻击。

### 白名单
