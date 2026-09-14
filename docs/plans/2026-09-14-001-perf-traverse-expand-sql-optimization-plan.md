---
title: Traverse Expand SQL Optimization - Plan
type: perf
date: 2026-09-14
topic: traverse-expand-sql-optimization
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
product_contract_source: ce-brainstorm
execution: code
---

# Traverse Expand SQL Optimization - Plan

## Goal Capsule

- **Objective:** 分三阶段优化 Go 运行时 Expand 执行路径的落库 SQL：截断显式化（P1，正确性）、消除死重 SQL 6→3（P2）、投影感知单条取数 3→2（P3）。
- **Product authority:** docs/draft/cursor_unit_test_design_compliance.md 与 runtime/e2e/just_test_2_opt.md 的分析，加 2026-09-14 对话确认的三级优先级与截断「方案一」。
- **Open blockers:** 无。超限=硬错误已定案；规划期四项技术选型已裁决（见 Key Technical Decisions）。
- **Execution:** code — Go runtime（mysqlobda provider、query 执行层、spi、api 层、内存 provider、conformance、e2e）。

---

## Product Contract

Product Contract 修订记录（规划期代码核实后的澄清级修订，产品范围未变）：R2/R5/R6/AE2 扩展到一跳叶子路径；Success Criteria 的行为变更清单按代码事实修正（GraphQL 顶层 List 默认页在 api 层、不受影响）。

### Summary

分三阶段优化 Expand 路径的 SQL 执行：先让超限截断显式报错并恢复真实分页上限；再砍掉 Expand 路径的死重 COUNT 与起点二次读取；最后让 Traverse 的单条 SQL 按 GraphQL selection 顺带取回中间字段，取代事后水合。2 跳嵌套查询的 SQL 条数从 6 降到 2。

### Problem Frame

一条 2 跳嵌套查询（`book { branches { name readers { name id } } }`）当前落库 6 条 SQL，其中三条是死重：Traverse 内部把起点对象重读一遍；GraphQL 嵌套字段从不消费 `totalCount` 却固定执行两条 COUNT（Traverse 一条、水合一条）；SQL 已经 JOIN 了中间表却不投影其业务列，事后又用两条 SQL 把中间对象查回来。

上一轮计划（2026-09-11-002）把「Traverse 不返回 `Visited`」执行成了「SQL 不得投影中间列」——把公共 SPI 契约与内部 SQL 投影混为一谈，是上述浪费的架构根源。同时 MySQL provider 的分页上限被临时硬编码为 10（注释明示测试用），超过 10 条的图被静默截断，GraphQL 与 REST 调用方均无感知，是数据正确性风险而非性能问题。以上全部事实已逐条对照代码独立核验（12/12 确认）。

### Key Decisions

- **KD1 阶段序：正确性 → 死重 → 投影。** 用户排序；三个阶段各自独立可交付、独立验收，前阶段不依赖后阶段。
- **KD2 超限 = 硬错误。** 检出超限即让查询失败并返回明确错误；「部分数据 + IsTruncated 标记」的方案二搁置，日后如需可与 P3 的契约扩展合并升级。
- **KD3 Expand 路径 COUNT 无条件关闭，不做 selection 感知。** 嵌套 `@link` 字段是普通列表、从不读 `totalCount`，REST follow 也忽略 count——「按需关闭」没有额外收益；顶层 List 查询保留 COUNT。
- **KD4 opt-in 定向投影，不恢复统一 `Visited`。** 公共 `TraversalResult` 契约与内部 SQL 投影解耦：新能力以可选参数进入，不传时行为不变；memory provider 与 conformance 同步。
- **KD5 翻案边界。** 推翻上轮 KTD2 的「中间列不投影」读法；上轮 U3「不改 JOIN 形状」继续有效——本轮只在既有 JOIN 上追加投影列。

### Requirements

**P1 — 截断防护（正确性）**

- R1. MySQL provider 分页上限恢复正式值：默认页 100、最大页 1000，移除临时测试值 10。
- R2. Traverse 与一跳叶子（GetLinks 型 Expand）的数据页以「上限 +1」探顶，仅在硬上限处检出超限并返回显式超限错误；恰好等于上限不报错，分页取数语义不变，不再静默截断。
- R3. 超限错误对 GraphQL 与 REST 两条入口均可见，错误信息指明硬上限值。
- R4. 现行固化静默截断的测试用例翻转为断言超限报错，并补充恰好等于上限不报错的边界断言。

**P2 — 死重消除（6→3）**

- R5. 查询选项增加「是否计算总数」开关，默认保持现行为；Expand 执行路径（Traverse、GetLinks 与水合）一律关闭，顶层 List 与引擎惰性计数字段保持开启。
- R6. 查询执行层为 Expand 操作统一设置「起点已确认」标记，Traverse 据此跳过内部起点重读；起点缺失的既有语义对未设置标记的调用方不变。
- R7. REST follow-links 与 GraphQL 嵌套 expand 同等受益（无 COUNT、无起点双读）。

**P3 — 投影感知（3→2）**

- R8. Traverse 接受按 selection 的投影提示（终点字段、中间类型字段、边必要列），投影列追加在既有 JOIN 上，不新增 JOIN 形状。
- R9. 已投影的中间类型不再触发事后水合；路径中出现但无选区字段的中间类型由边端点合成仅含标识的骨架对象、同样免查；未提供提示结构的调用方沿用现有水合路径。
- R10. inline 合成边只投影必要列（标识、租户、外键、版本、时间戳），不再整行重复。
- R11. SPI 扩展向后兼容：不传新选项时行为与现状一致；memory provider 同步实现；conformance 覆盖新选项。

**横切 — 验收基线**

- R12. e2e 为 2 跳金路径分阶段断言 SQL 条数（6→3→2）与结果正确性，不再只断言无 error；断言仅在真实 MySQL 后端生效。

无 Key Flows 节：本轮不新增用户可见流程，行为面就是 SQL 计数与超限错误，由 Acceptance Examples 覆盖。

### Acceptance Examples

2 跳 `branches { readers }` 查询的六条 SQL 在各阶段的去留：

| SQL（现状） | P1 | P2 | P3 |
| --- | --- | --- | --- |
| 1 根 Get(Book) | 保留 | 保留 | 保留 |
| 2 Traverse 内部起点重读 | 保留 | 砍 | 砍 |
| 3 Traverse COUNT | 保留 | 砍 | 砍 |
| 4 Traverse 数据页 JOIN | 保留（+1 探顶） | 保留 | 保留（追加中间列投影） |
| 5 水合 COUNT(Branch) | 保留 | 砍 | 砍 |
| 6 水合页(Branch.name) | 保留 | 保留 | 砍 |

- AE1. **Given** 种子为两读者一分馆（远低于上限），**When** P2 完成后执行 2 跳查询，**Then** 恰好 3 条 SQL（1、4、6）且结果完整。**Covers R5, R6, R12.**
- AE2. **Given** 某 fan-out 超过硬上限，**When** 执行嵌套查询（多跳或一跳叶子），**Then** 查询失败且错误指明超限，而非返回截断数据。**Covers R2, R3.**
- AE3. **Given** 调用方未使用任何新选项，**When** 走 Traverse / 水合，**Then** 行为与现状一致（含 TotalCount 语义）。**Covers R5, R11.**
- AE4. **Given** 根对象不存在，**When** 执行 GraphQL 查询，**Then** 根仍返回 null，语义不变。**Covers R6.**
- AE5. **Given** P3 完成后的同一 2 跳查询，**Then** 恰好 2 条 SQL，Branch.name 来自 Traverse 数据页本身。**Covers R8, R9, R12.**

### Success Criteria

- 2 跳金路径 SQL 条数 6→3→2 由 e2e 断言分阶段锁定（MySQL 后端）。
- 任何超限截断都表现为显式错误，多跳与一跳叶子路径全链路无静默数据丢失。
- 既有 GraphQL/REST 外部行为不变；实际的行为变更为：超限报错（新增）、SPI 层默认页 10→100（与内存 provider 既有默认对齐）、聚合分组页 10→100、展开行窗口 10→1000（正确性修复）。GraphQL 顶层 List 默认页在 api 层（20），不受影响。
- 三个阶段各自独立合入后测试全绿。

### Scope Boundaries

- 直链 JOIN（draft §6 的跨中间表两跳直连）继续搁置，沿用上轮 U3。
- 嵌套字段 Connection 化与 `IsTruncated`/extensions 截断标记（方案二）不做，本轮以硬错误兜底。
- 根读取合并进 Traverse 的字面 1-SQL 情形不追，P3 以 2 条 SQL 为验收线。
- 分页上限配置化（env/config 驱动）另议，本轮恢复常量即可。
- 内存 provider 的超限/上限语义对齐不做：超限报错本轮声明为 MySQL provider 语义，其 e2e 仅在 MySQL 模式生效。内存后端今日并非无截断——Traverse 有静默的 10000 节点上限（`maxTraversalNodes`），该已知静默行为连同其对齐一并出范围（后续如需，可在 provider 语义同步时对齐哨兵）。
- TS 侧 packages/*（Apollo、storage-postgres、spi 类型）不在本轮范围。

### Dependencies / Assumptions

- 支撑本计划的 12 项代码事实已独立核验确认（锚点见 Sources），无未验证假设。
- HopCap=1000 与恢复后的 MaxPageLimit=1000 对齐，「+1」探针在第 1001 行检出溢出，同时缓解上轮计划 Risk 节「双 1000 无余量」的预警。
- 探顶与投影都运行在租户 + 软删过滤后的 JOIN 之上：跨租户行不会触发超限，投影中间体与水合同为仅存活对象——此为既有语义，不做「修复」。
- e2e 依赖 `TEST_DB_URL` 提供的真实 MySQL（仓库约定不自启 Docker）；未设置时 e2e 静默回退内存后端，SQL 计数与超限断言必须按后端门控。

### Outstanding Questions

规划期已全部裁决：原四项 Deferred to Planning 分别落在 Planning Contract 的 KTD1（COUNT 开关形态）、KTD3（超限错误类型与 REST 映射）、KTD5（投影提示结构）、KTD7（有界水合批量绕过页上限）。无遗留阻塞项；2026-09-14 评审产生的待决修复见文末 Deferred / Open Questions（非阻塞，建议在对应单元实施前裁决）。

### Sources / Research

- docs/draft/cursor_unit_test_design_compliance.md — 原始六问题分析（Cursor 导出对话）。
- runtime/e2e/just_test_2_opt.md — 三级优先级梳理与截断「方案一」。
- docs/plans/2026-09-11-002-refactor-traverse-nodes-edges-plan.md — KTD2/U3（本轮部分翻案、部分延续）与 F1/F2 验收基线。
- docs/solutions/architecture-patterns/traverse-nodes-edges-batch-hydration.md — Nodes+Edges 契约、桶投影机制、MaxPageLimit 警告（本轮实施的直接前驱学习）。
- docs/solutions/design-patterns/mysql-fulltext-search-and-eq-only-filter.md — 位置参数按 `?` 出现序绑定、计数子查询约束、gold 测试方法论。
- 关键代码锚点：runtime/storage/mysqlobda/query.go:105-111（临时上限 10）；runtime/storage/mysqlobda/links.go:340 与 :466-490（GetLinks 的 +1 探下一页 vs Traverse 的 COUNT）；links.go:392-394（Traverse 起点重读）；runtime/query/hydrate.go:38-73（逐类型水合，按边端点分桶、非选区驱动）；runtime/spi/ontology.go:197-204 与 :248-258（QueryOptions / TraversalResult）；runtime/api/node.go:45-63（嵌套字段为普通列表、不读 totalCount）；runtime/api/schema.go:142-165（顶层 List 唯一 GraphQL count 消费点）；runtime/api/http.go:90-114（REST follow：CheckStart、错误链、忽略 count）；runtime/storage/mysqlobda/links_test.go:423-452（固化静默截断的测试）。
- 规划期新增锚点：runtime/engine/computed.go:95-105（countLinks 惰性计数字段，第二 count 消费者）；runtime/api/compile.go:31-84,95-103,150-176（memo 键、GetLinks/Traverse 模式选择、SelectedFieldNames 匹配器）；runtime/api/pagination.go:11-12,81-99（GraphQL List 默认 20、上限 100 在 api 层）；runtime/query/execute.go:60-67,112-157,164-186（CheckStart、expandGetLinks 丢弃 HasNextPage、expandTraverse）；runtime/query/expand.go:133-155（neighbors 缺对象即剪枝）；runtime/storage/memory/provider.go:758-885,1054（内存 Traverse 无上限、默认页 100）；runtime/conformance/conformance_phase3_test.go:18-31,134-153（内存直驱、nil options 断言 TotalCount）；runtime/internal/sqlopen/open.go 与 runtime/testdb/mysql.go（sqlhooks 设施）；runtime/e2e/helper_test.go:68,143-176（后端静默回退、provider 接受 *sql.DB）。

---

## Planning Contract

### Key Technical Decisions

- **KTD1 COUNT 跳过是零值=计数的 opt-in 布尔开关，QueryOptions 与 TraversalOptions 各一份。** 方向被代码逼定：引擎的惰性计数字段（runtime/engine/computed.go:95-105）与 conformance（nil options）都依赖无参调用仍然计数，默认跳过会破坏它们。开关同时被 QueryObjects、GetLinks、Traverse 三个读路径尊重（QueryObjects 与 GetLinks 共用 QueryOptions）；引擎内部的 countLinks 永不设置。跳过时 TotalCount 返回 0，语义为「未计算」；HasNextPage 不受影响（来自既有 +1 探测）。
- **KTD2 超限探顶只在硬上限处报错。** 有效限额（分页、小限额取数）超量保持今天的页语义、以 TotalCount 为准；恰好等于上限不报错（探针行取到第 N 行即丢弃）。Traverse 数据页取 `min(请求, MaxPageLimit)+1` 行；仅当请求限额 ≥ MaxPageLimit（硬上限生效）且探针行命中时报错并丢弃探针行，亚上限的探针行按既有页语义静默丢弃。一跳叶子路径不改 provider，由查询执行层消费 GetLinks 既有的 HasNextPage——HopCap 限额取回后只要 HasNextPage=true 即报同一错误（runtime/query/execute.go:112-157 现在直接丢弃该信号）；不去重窗口判满，接受「仅重复链接溢出也报错」，堵住重复行掩蔽静默截断的角落。超限按行计数（并行链路的重复终点计入，与 Nodes 行语义一致）。此语义为 MySQL provider 本轮声明（见 Scope Boundaries）。
- **KTD3 超限错误是 spi 哨兵，REST 走独立请求级错误码。** 哨兵放 runtime/spi/errors.go 的既有 var block，遵循 errors.Is 消费、`fmt.Errorf("%w:…")` 包装携带上限值的约定。GraphQL 侧 resolveLink 原样传播进 errors[]（仅 ErrObjectNotFound 被遮蔽，超限不在遮蔽之列）。REST 侧在 serveRESTFollow 的 errors.Is 链（runtime/api/http.go:97-107，先于 500 兜底）加分支，用 writeError 返回请求级错误（查询要求过多，非服务端故障），区别于 400 无效路径 / 404 起点不存在 / 500 内部错误；具体状态码与码值实现期定（默认倾向 422 + TRAVERSAL_LIMIT_EXCEEDED）。
- **KTD4 起点重读跳过是 TraversalOptions 的 opt-in 标记，由查询执行层在所有 Expand 操作上设置。** GraphQL 根对象已被物化（memo/根 Get）、REST 已过 CheckStart 的 GetObject，查询层据此设置标记；provider 默认（未设置）保留 ErrObjectNotFound 语义，runtime/storage/mysqlobda/links_test.go:251-258 的缺失起点用例保持绿。内存 provider 无起点检查（缺失起点返回空），无需镜像。两个 provider 对软删起点的既有分歧（mysqlobda 可见 / memory 遮蔽）保持不统一，不在本轮拉齐。
- **KTD5 投影提示按类型携带字段清单，结果落 TraversalResult 的可选逐跳负载字段；`Visited` 保持退役。** 提示在 api 层从 compileExpand 既有的 SelectedFieldNames 匹配器提取（复用同一解析器，别名/片段行为一致，runtime/api/compile.go:95-103,150-176），随 Expand IR 下传。planner 顺着既有 TraverseLayout 桶机制在终点列后追加中间类型列桶——只改 SELECT 列表，JOIN/WHERE/ORDER/args 全部不动；计数子查询（若请求）维持单列终点标识，桶列不得泄入（MySQL 派生表重名列限制）。行扫描按桶偏移切片，边装配复用 assembleLink/assembleInlineLink（与 GetLinks 字节级一致）。空选区的中间类型由边端点合成仅含 id 的骨架对象——expand.go neighbors() 对缺失对象剪枝，骨架保证子树不丢；水合去重按 id 不按类型（历史 bug 教训）。
- **KTD6 memo 键加入选区指纹。** 现键为 (startID, fieldName)（runtime/api/compile.go:31-46），同字段不同子选择会复用首次的部分投影对象、二次渲染出 null。P3 起键内含投影提示指纹；无提示（P2 及之前）行为不变。
- **KTD7 有界 by-ids 水合批量绕过页上限夹断。** hydrateByIDs 的 `Limit: len(ids)` 是调用方约束的有界批次，不再被 pageLimitOffset 夹到 MaxPageLimit（runtime/storage/mysqlobda/query.go:116-127）；修复「批量大于夹断值静默剪枝」的既有隐患（上轮学习文档明示）。机制为 provider 内部检测：mysqlobda 在 translateFilter 已识别的 or-of-eq-on-`_id` 批形状上跳过夹断——不新增 SPI 选项，无 conformance/内存同步义务；普通分页查询不受影响。
- **KTD8 SQL 计数断言靠 mysqlobda 的驱动级计数钩子，仅 MySQL 后端启用。** 仓库已有 sqlhooks 注册设施（runtime/internal/sqlopen/open.go）；在 e2e 装配处（runtime/e2e/helper_test.go，provider 接受 *sql.DB）包一层计数 driver/Options 钩子，按 GraphQL 操作重置与计数。内存后端无 SQL 概念，断言按后端门控跳过；单层的 countStore 只数 SPI 调用（1 次 QueryObjects = 2 条 SQL），不能混用。

### High-Level Technical Design

Expand 执行路径与新选项的贯通（默认=现行为）：

```mermaid
flowchart TB
  IN[GraphQL resolveLink / REST follow] --> EX{Expand: selection 嵌套 @link?}
  EX -- 否·叶子 --> GL["expandGetLinks → GetLinks<br/>COUNT 视开关 / limit+1 页"]
  EX -- 是·多跳 --> TR["expandTraverse → Traverse<br/>起点重读视标记 / COUNT 视开关 / cap+1 页"]
  GL -- "窗口满且 HasNextPage" --> ERR[超限哨兵错误]
  TR -- "cap+1 探针行命中" --> ERR
  ERR --> GQ[GraphQL errors[] 原样]
  ERR --> RS[REST 独立请求级错误码]
  TR -- 投影提示命中 --> PAY[中间列桶随行带回 → 免水合]
  GL --> HY[hydrateEdges 按类型批量]
  TR --> HY
  HY -- 类型未被投影 --> QO["QueryObjects 水合<br/>COUNT 视开关 / 绕过页夹断"]
  OPT["查询层设置：SkipTotalCount / 起点已确认 / 投影提示<br/>（SPI 默认值 = 现行为）"] -.-> GL
  OPT -.-> TR
  OPT -.-> QO
```

列桶布局演进（2 跳 = junction + inline，方向性示意）：

```text
现状：  [ s2.* 终点 ]  [ l0.* hop0 边 ]  [ s2.* hop1 inline 宿主整行 ]
P3 后： [ s2 选区列 ]  [ l0 必要列 ]  [ s1 选区列·新中间桶 ]  [ s2 最小边列 ]
计数子查询（若请求）：仅单列终点标识——桶列一律不进入
```

### Sequencing

P1（正确性）= U1 → U2 → U3；P2（死重）= U4 → U5；P3（投影）= U6 → U7。每阶段独立可合入；U3 的基线断言先锁现状 6 条，后续单元逐阶段改期望值（4 → 3 → 2）。

---

## Implementation Units

### U1. 恢复分页上限与 Traverse 探顶硬错误（P1）

- **Goal:** MySQL provider 恢复正式分页上限；Traverse 数据页 `LIMIT cap+1` 探顶，命中即报超限哨兵错误，静默截断消失。
- **Requirements:** R1, R2, R4
- **Dependencies:** 无
- **Files:** runtime/storage/mysqlobda/query.go；runtime/spi/errors.go；runtime/storage/mysqlobda/links.go；runtime/storage/mysqlobda/links_test.go
- **Approach:** 恢复 DefaultPageLimit=100、MaxPageLimit=1000（去掉「临时 10」）；新增超限哨兵（KTD3 约定）；Traverse 页查询取 `min(请求, MaxPageLimit)+1` 行——仅当请求限额 ≥ MaxPageLimit 且探针行命中时报超限错误并丢弃探针行，亚上限时探针行按既有页语义静默丢弃（KTD2）。
- **Patterns to follow:** GetLinks 既有 `limit+1`/HasNextPage 探测（runtime/storage/mysqlobda/links.go:340,366-369）；spi/errors.go 哨兵 var block 与 errors.Is 消费。
- **Test scenarios:**
  - 翻转 TestTraverseLimitDefaultsAndCap：seed MaxPageLimit+1 行、请求 HopCap，断言收到超限错误（Covers R4）。
  - 边界：seed 恰好 MaxPageLimit 行、请求 HopCap，不报错且返回全部行（Covers R2）。
  - 分页不受影响：`Traverse(Limit:1, Offset:1)` 超过 2 行的数据上正常翻页、不报错（KTD2）。
  - 缺失/跨租户起点仍报 ErrObjectNotFound（既有用例保持绿）。
  - 错误可被 errors.Is 识别，包装信息含上限值（R3）。
- **Verification:** mysqlobda 全部测试绿；超限用例在真实 MySQL（TEST_DB_URL）通过。

### U2. 叶子路径防截断与超限错误贯通两入口（P1）

- **Goal:** 一跳叶子 Expand 的静默截断同样变硬错误；超限错误在 GraphQL errors[] 与 REST 独立错误码可见。
- **Requirements:** R2, R3
- **Dependencies:** U1（哨兵错误）
- **Files:** runtime/query/execute.go；runtime/api/http.go；runtime/e2e/graphql_test.go；runtime/e2e/rest_test.go
- **Approach:** expandGetLinks 消费既有 HasNextPage：HopCap 限额取回后 HasNextPage=true 即返回同一超限哨兵——不以去重窗口判满，重复链接不得掩蔽溢出（runtime/query/execute.go:112-157 现丢弃该信号）。REST 在 serveRESTFollow 的 errors.Is 链加分支 + writeError 请求级错误码（先于 500 兜底）。查询层守卫的触发阈值需要可测缝（从 ir.HopCap 读取改为可注入/包级变量），便于小规模单测。
- **Patterns to follow:** http.go 既有 errors.Is 链与 writeError（runtime/api/http.go:97-107,147-154）；e2e 既有状态码断言（runtime/e2e/rest_test.go:57）与 gql 辅助（runtime/e2e/graphql_test.go:205）。
- **Test scenarios:**
  - 叶子 expand 窗口满 + HasNextPage → 超限错误（Covers AE2 的叶子分支）。
  - 叶子 expand 窗口未满（含恰好满但无下一页）→ 正常返回。
  - 重复链接占满取回行（去重邻居数 < HopCap）但 HasNextPage=true → 仍报超限（堵掩蔽角落，Covers AE2 叶子分支）。
  - GraphQL e2e（MySQL 模式）：超大 fan-out 查询的 errors[] 含超限信息（Covers AE2）。
  - REST e2e（MySQL 模式）：follow 超限返回独立错误码而非 500，响应含上限值（R3）。
- **Verification:** 两入口 e2e 绿；内存模式对应用例按门控跳过。

### U3. e2e SQL 计数装置与基线锁定（P1 收尾）

- **Goal:** 提供 SQL 语句计数钩子，先把 2 跳金路径的现状 6 条锁成基线断言。
- **Requirements:** R12
- **Dependencies:** U1（上限恢复后基线才是「正式」形态）
- **Files:** runtime/e2e/helper_test.go；runtime/storage/mysqlobda/（Options 或驱动包装挂点）
- **Approach:** 依 KTD8：sqlhooks/驱动包装计数，按 GraphQL 操作重置；仅 MySQL 后端启用。基线断言当前 6 条与六条 SQL 的角色对应（根 Get、起点重读、Traverse COUNT、数据页、水合 COUNT、水合页）。
- **Execution note:** 特征化优先——先锁现状再动执行层；后续单元只改期望值，不重建装置。
- **Test scenarios:**
  - 2 跳金路径断言恰好 6 条 SQL 且角色匹配（现状特征化）。
  - 同一查询断言结果 JSON 完整（计数断言不牺牲正确性）。
  - 内存后端：断言整体跳过、不误报。
- **Verification:** MySQL 模式 e2e 绿，基线=6。

### U4. COUNT 跳过开关（P2）

- **Goal:** Expand 三条读路径不再执行 COUNT；默认行为与其余调用方完全不变。
- **Requirements:** R5, R7, R11
- **Dependencies:** U3（改基线期望 6→4）
- **Files:** runtime/spi/ontology.go；runtime/storage/mysqlobda/query.go；runtime/storage/mysqlobda/links.go；runtime/storage/memory/provider.go；runtime/query/execute.go；runtime/query/hydrate.go；runtime/conformance/（新增用例）；runtime/query/execute_test.go
- **Approach:** 依 KTD1：QueryOptions 与 TraversalOptions 各加零值=计数的跳过布尔；mysqlobda 三处（QueryObjects/GetLinks/Traverse）开关生效、跳过时 TotalCount=0 且不发 COUNT SQL（Traverse 计数子query整体不发）；内存 provider 同语义（计数本来免费，语义对齐即可）；查询执行层在 Expand 的 GetLinks、Traverse、水合三处设置。countLinks（nil options）与顶层 List 不设置。
- **Patterns to follow:** QueryOptions 既有字段演进；conformance 既有 TestConformance_* 形态（runtime/conformance/conformance_phase3_test.go）。
- **Test scenarios:**
  - 默认（不设开关）：COUNT 照发、TotalCount 照算——countLinks 回归用例（Covers AE3）。
  - Expand 路径设开关：Traverse/GetLinks/水合均无 COUNT SQL，结果不变（Covers R5, R7）。
  - e2e（MySQL）：2 跳查询 6→4 条（SQL 3、5 消失）。
  - conformance：开关默认计数、设置后 TotalCount=0 两个方向（R11）。
  - 跳过时 HasNextPage 语义不变（叶子路径）。
- **Verification:** 全仓 go test 绿；e2e 期望改 4。

### U5. 起点重读跳过与有界水合免夹断（P2 收尾）

- **Goal:** Traverse 不再二次读取起点；by-ids 水合批量不再被页上限静默剪枝。
- **Requirements:** R6, R7
- **Dependencies:** U4（同阶段顺序合入）
- **Files:** runtime/spi/ontology.go；runtime/storage/mysqlobda/links.go；runtime/storage/mysqlobda/query.go；runtime/query/execute.go；runtime/query/hydrate.go
- **Approach:** 依 KTD4：TraversalOptions 加「起点已确认」标记，查询执行层对所有 Expand 操作设置；mysqlobda Traverse 见标记跳过 loadObject。依 KTD7：mysqlobda 在 or-of-eq-on-`_id` 批形状上内部跳过 pageLimitOffset 夹断，不新增 SPI 选项。
- **Patterns to follow:** links_test.go:251-258 缺失起点语义用例（必须保持绿）。
- **Test scenarios:**
  - GraphQL 2 跳：起点只读一次（e2e SQL 计数 4→3，Covers AE1）。
  - REST follow：CheckStart 的 GetObject 后 Traverse 不再读起点。
  - 未设标记（直连 SPI / conformance）：缺失起点仍 ErrObjectNotFound（Covers AE3, AE4）。
  - 水合批量 > MaxPageLimit 个 id：全部取回、无静默丢行（KTD7）。
  - 普通分页查询仍受 MaxPageLimit 约束（夹断旁路不外溢）。
- **Verification:** e2e 期望改 3；mysqlobda/conformance 绿。

### U6. 投影感知 Traverse（P3）

- **Goal:** 单条 Traverse SQL 按 selection 取回中间字段，取代该类型的事后水合；空选区类型以骨架免查。
- **Requirements:** R8, R9, R11（并达成 R12 终值 2）
- **Dependencies:** U5
- **Files:** runtime/spi/ontology.go；runtime/obda/planner.go；runtime/obda/planner_test.go；runtime/storage/mysqlobda/links.go；runtime/storage/memory/provider.go；runtime/api/compile.go；runtime/query/execute.go；runtime/query/expand.go；runtime/conformance/（新增用例）
- **Approach:** 依 KTD5/KTD6：提示结构（类型→字段清单）进 TraversalOptions，api 层用 compileExpand 的 SelectedFieldNames 匹配器提取；planner 经 TraverseLayout 追加中间列桶（列序与扫描按桶偏移，args 顺序重排遵循 `?` 出现序）；TraversalResult 加可选逐跳负载字段（未提示=现状形状）；expand 层合成空选区骨架、跳过对应水合；memo 键加选区指纹。内存 provider 按提示过滤对象字段实现同语义。
- **Patterns to follow:** 既有逐跳桶机制与 TraverseLayout（runtime/obda/planner.go:459-484）；assembleLink/assembleInlineLink 复用；planner goldens 的偏移量断言形态（planner_test.go:683-776）。
- **Test scenarios:**
  - e2e（MySQL）：2 跳查询 3→2 条 SQL，Branch.name 来自数据页（Covers AE5）。
  - 无任何提示（向后兼容）：走现有水合路径、结果与现状一致（Covers AE3）。
  - 空选区中间类型（`branches { readers { name } }` 不选 branch 字段）：该类型零水合 SQL，子树完整（骨架生效，Covers R9）。
  - 同字段两片段不同子选择：互不复用 memo，无 null 字段（KTD6）。
  - 计数子查询仍为单列终点标识（桶列不泄入，MySQL 派生表约束）。
  - planner goldens 重算：中间桶偏移/限定符序列；args 切片锁全文。
  - conformance：提示语义两 provider 一致（R11）。
- **Verification:** e2e 期望改 2；planner/mysqlobda/conformance 绿。

### U7. inline 合成边最小列集（P3 收尾）

- **Goal:** inline 跳的合成边桶只投影必要列（标识、租户、外键、版本、时间戳），消除宿主整行二次投影。
- **Requirements:** R10
- **Dependencies:** U6（列桶布局稳定后再收窄）
- **Files:** runtime/storage/mysqlobda/links.go；runtime/obda/planner.go；runtime/obda/planner_test.go
- **Approach:** 收窄 inline 跳 HostSelect 至边合成所需最小集（links.go:442-447 现取宿主全行）；扫描与 goldens 随列数调整。GetLinks 的 inline 扫描路径（links.go:283-287）同规则收敛，保持两出口一致。
- **Patterns to follow:** TestPlanTraverseLayoutInlineHopAliases 的别名断言（planner_test.go:725-776）。
- **Test scenarios:**
  - inline 跳桶列数=最小集、无业务列（goldens 重算）。
  - 合成边身份与属性和收窄前逐字节一致（边 id/时间戳不因收窄变化）。
  - 2 跳 e2e 保持 2 条 SQL、结果不变（回归）。
- **Verification:** goldens/mysqlobda 绿；e2e 维持 2。

---

## Verification Contract

- 单元与集成：`cd runtime && set -a; [ -f ../.env ] && . ../.env; set +a && go test ./...`（MySQL 相关套件经 TEST_DB_URL 打真实库，不自启 Docker）。
- 定向：`go test ./storage/mysqlobda/... ./obda/... ./query/... ./api/... ./conformance/... -v`；e2e：`go test ./e2e -v`（或 Makefile 的 test-mysql / e2e 目标）。
- SQL 计数断言（U3 装置）按后端门控：仅 TEST_DB_URL 存在的 MySQL 模式生效，内存模式跳过。
- 阶段出口：P1 后=超限不再静默（多跳+叶子）、基线 6 锁定；P2 后=e2e 期望 3（AE1）；P3 后=e2e 期望 2（AE5）；全程 conformance 绿、countLinks 回归绿。
- 质量门：默认行为零回归是硬门——任何「不传新选项行为改变」的用例失败即阻断合入。

## Definition of Done

- R1–R12 全部满足；AE1–AE5 在对应阶段末端通过（AE2 含叶子分支）。
- 三个阶段各自独立合入后全仓测试绿；SQL 计数断言分阶段定格 6→3→2。
- 无遗留实验与死代码：探针缝（可注入阈值）以最小形态保留或还原，废弃尝试不留在 diff。
- 超限语义、COUNT 语义、投影语义均有测试锁定（含恰好等于上限、无参调用方、空选区骨架三类边界）。
- Product Contract 修订记录在案；CONCEPTS.md 的 Projection-aware Traverse / Overflow probe 词条与实现一致。

---

## Deferred / Open Questions

本节是 ce-doc-review 工作流的暂存区（staging area），不属于计划正文；后续评审轮次应忽略其内容作为评审对象。

### From 2026-09-14 headless review

评审团队：coherence、feasibility、product-lens、scope-guardian、adversarial（5 persona）。feasibility 两条发现因缺 `severity` 字段在校验层丢弃，实质内容由 coherence/adversarial 覆盖。

**已应用（5）：**

- KD5：裸引用「U3」与本计划 U3 撞名 → 补「上轮」限定（coherence，safe_auto，锚点 100）。
- 2026-09-14 交互轮（最佳判断批量，用户 Proceed 确认）应用以下 4 项，修法已改入正文：
  - KTD2 + U2 叶子溢出守卫去掉「窗口已满」合取——`HasNextPage=true` 即报超限，堵重复链接掩蔽（adversarial，P1）。
  - KTD2 + U1 Traverse 溢出条件补硬上限合取——仅「请求限额 ≥ MaxPageLimit 且探针行命中」报错（coherence + adversarial，P2，锚点 100）。
  - Scope Boundaries 修正「内存亦无上限」假前提——如实写明内存有静默 10000 节点截断并以此声明出范围（adversarial，P2）。
  - KTD7 删去「QueryOptions 精确批量标记」选项，钉死为 provider 内部 or-of-eq 批形状检测（scope-guardian，P2）。

**FYI 观察（5，无需决策）：**

- KTD7 夹断修复缺 R 编号——验收门「R1–R12 全部满足」探测不到它的删除（product-lens）。
- 硬错误上线而缓解杠杆（分页上限配置化）无跟进归属，>1000 fan-out 消费者在配置化落地前只能改写查询（product-lens）。
- U3 计数钩子应只做 e2e 侧驱动包装，勿加生产 Options 面（scope-guardian）。
- HopCap ≤ MaxPageLimit 耦合不变量无守护：U2 把阈值改成可注入变量 + 配置化另议，恰好是会让叶子守卫变死代码的两个改动；建议加耦合断言（adversarial）。
- 多跳硬上限实为「路径行乘积」（50 分馆 × 21 读者即 1050 行报错），并非「每跳 1000」；建议在 KTD2/成功标准中言明（adversarial）。

**残余关切（4）：**

- `TEST_DB_URL` 未设置时 e2e 静默回退内存——绿灯验证不了任何核心主张（SQL 计数、超限、6→3→2），计划无探测手段（adversarial）。
- 计数驱动须以新唯一名注册（`sql.Register` 重名 panic），与既有 `mysql-hooks` 共存（feasibility + adversarial）。
- AE2 的 MySQL e2e 需每用例播种 >1000 行链接，成本未估（feasibility）。
- KTD8 计数「按 GraphQL 操作」重置，但 U5 还断言 REST 路径行为——重置语义需覆盖 REST 域（scope-guardian）。

**遗留问题（3）：**

- REST 超限状态码确认 422（vs 413/400）——U2 接线前定，属公共 API 契约（feasibility + adversarial）。
- 硬错误若在生产高频出现，方案二（部分数据 + IsTruncated）是否并入 P3 契约扩展（product-lens）。
- 叶子防截断走查询层守卫（KTD2 现方案）还是给 GetLinks 也上 provider 级 cap+1 探针使两路对称（adversarial）。

Restated: 4（residual/deferred 与可执行发现重复，已抑制）；Dropped（锚点 0/25）: 0。
