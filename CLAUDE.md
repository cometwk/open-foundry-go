# CLAUDE.md

## 规范引用范围

引用 Open Foundry — Technical Specification v2.0 时，忽略其中 Sync Engine 与 Security/Governance（Authorisation、Permission、Audit 等）内容。

## 测试规范

- gold-path = domain-packs/library-pack/library-pack.md
- 单元测试需要案例时，采用 `domain-packs/library-pack/library-pack.md` 里的简化版。
- 本地配置统一走 `.env` 加载模式：`set -a; [ -f .env ] && . .env; set +a`
- MySQL 测试用真实数据库（`TEST_DB_URL`），**不要自行启动 MySQL Docker**。在 `runtime/` 下运行：

```bash
cd runtime && set -a; [ -f ../.env ] && . ../.env; set +a && \
go test ./storage/mysqlobda/... ./obda/dialect/mysql/... -v 2>&1 | tail -250
```

## 知识库

- `docs/solutions/` — 已归档的问题解决方案（bug 修复、最佳实践、模式），按类别分目录、带 YAML frontmatter（module、tags、problem_type），实现或调试相关领域时可查阅。
- `CONCEPTS.md` — 项目共享领域词汇表（实体、命名流程、状态概念），熟悉代码库或讨论领域概念时参考。

## domain-packs

当要求自动编写 domain-packs 时，只需要编写

- obda/
- schema/
- seeds/

不需要编写

- actions/
- permissions/

