---
title: "会话双文件持久化 - Plan"
type: refactor
date: 2026-09-30
topic: session-persistence
artifact_contract: ce-unified-plan/v1
artifact_readiness: requirements-only
product_contract_source: ce-brainstorm
execution: code
---

# 会话双文件持久化 - Plan

## Goal Capsule

- **Objective:** `agent/engine/session.go` 会话持久化从「单文件全量覆盖」改为「sessionId 必填 + 消息增量追加」的双文件布局。
- **Product authority:** 用户直接指定（2026-09-30 对话确认，含存储布局选项）。
- **Open blockers:** 无。

---

## Product Contract

### Summary

会话存储拆为两个文件：`{id}.json` 只存 `Session` 结构（Metadata + SystemPrompt 快照），消息逐条 append 到 `{id}.jsonl`；`SaveSession` 的 sessionId 必填、入参消息语义变为「本次新增」；`LoadSession` 合并两个文件还原完整 `Session`（struct 定义不变，`Messages` 字段仅内存使用）。

### Problem Frame

现状每次保存把完整 `Session`（含全部消息）`MarshalIndent` 后全量覆盖写 `.agent/sessions/{id}.json`（agent/engine/session.go:131-136）：长会话下每轮 agent 回复都是 O(n) 序列化 + 重写。`sessionId ...string` 可选参数让「新建」与「续存」两种语义混在一个签名里，调用方各自兜底生成 id。

### Key Decisions

- **纯双文件，不兼容旧格式** — 消息唯一来源是 `.jsonl`，拒绝「无 jsonl 时回退读 json 内嵌消息」的双读取路径；旧单文件会话只保留元数据可用。
- **增量计算责任在调用方** — `SaveSession` 不做 diff/去重，传入即增量；避免引擎层为算增量而全量读旧消息。
- **元数据小文件可重写，消息大文件只追加** — 每轮 O(1) 追加 + O(1) 元数据重写，取代 O(n) 全量序列化。
- **SystemPrompt 每次保存仍重建快照** — 保持现有行为（含记忆召回可能的 LLM 调用副作用，注释警示保留）。

### Requirements

**API 语义**

- R1. `SaveSession` 的 `sessionId` 为必填参数；空值直接报错，不再内部生成 id。
- R2. `SaveSession` 的 `messages` 语义为「本次新增的消息」，只追加，不重写既有消息。

**存储布局**

- R3. 每个会话在 `.agent/sessions/` 下两个文件：`{id}.json` 存 `Session` 结构（Metadata + SystemPrompt），`{id}.jsonl` 存消息，一行一条 `aisdk.UIMessage`，以 append 方式打开写入。
- R4. `{id}.json` 不再内嵌消息数据（落盘时 `messages` 为空）。
- R5. 追加时元数据维护：`createdAt` 与 `title` 首次创建时确定且此后保持稳定；每次保存更新 `updatedAt`，`messageCount` 按累加更新。

**加载与兼容**

- R6. `LoadSession` 从 `{id}.json` 还原 Metadata 与 SystemPrompt，从 `{id}.jsonl` 流式读出全部消息合并为完整 `Session`；`.jsonl` 缺失或某行损坏时跳过该行，不回退读 `{id}.json` 内嵌消息。
- R7. 旧格式单文件会话（messages 内嵌的 `{id}.json`，如现存的 `.agent/sessions/2-abc.json`）无迁移、无回退：元数据可列出，消息视为空。
- R8. `ListSessions` 只把 `{id}.json` 当会话文件，`.jsonl` 不得被收录。

**调用方**

- R9. chat 路由流结束时只追加本轮新增消息，不再传全量 `args.Messages`。
- R10. sessions POST 路由按追加语义保存：新建传初始消息，续传只传增量；sessionId 同样必填（参数校验拦截，不再由 route 侧生成兜底 id——原兜底与测试意图相悖，属既有分叉，此次一并对齐）。

### Scope Boundaries

- 不迁移、不转换旧格式会话文件。
- 不做消息去重/防重复追加保护——同一批次重复调用会重复入库，信任调用方。
- 不做 `.jsonl` 压缩、轮转或 fsync 刷盘策略。

### Acceptance Examples

- AE1. **首次保存** — Given 新 sessionId 与 1 条用户消息，When `SaveSession`，Then 生成 `{id}.json`（createdAt=updatedAt、messageCount=1）与单行 `{id}.jsonl`。
- AE2. **追加** — Given 已含 1 条消息的会话，When 同 id 追加 2 条新消息，Then `.jsonl` 共 3 行、messageCount=3、createdAt/title 不变、updatedAt 更新。
- AE3. **加载合并** — When `LoadSession`，Then 返回完整 `Session`，messages 按行序来自 `.jsonl`。
- AE4. **旧格式无回退** — Given 内嵌 messages 的旧 `{id}.json` 且无 `.jsonl`，When `LoadSession`，Then Metadata 正常、Messages 为空。
- AE5. **sessionId 必填** — When `SaveSession(ctx, cwd, "", msgs)`，Then 返回错误，不产生新文件。

### Sources

- `agent/engine/session.go:102` — 现签名（variadic sessionId）与全量覆盖写。
- `agent/route/chat/handler.go:132`、`agent/route/sessions/route.go:66` — 仅有的两个生产调用方，均传全量消息。
- `agent/engine/session_test.go:126` — `TestSessionSaveOverwrite` 依赖覆盖语义，需改写为追加语义。
- `aisdk.UIMessage` 自带 `MarshalJSON`/`UnmarshalJSON`，含工具调用的消息可完整 round-trip（见 `LoadSession` 现注释）。
