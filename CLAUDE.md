
当提到 Open Foundry — Technical Specification v2.0
时，要求先忽略一下内容：

- Sync Engine
- Security and Governance 相关的内容，如 Authorisation, Permission, Audit 等


### 单元测试规范

1. **测试案例要求**
* 当单元测试需要案例时，要求采用 `domain-packs/library-pack/library-pack.md` 里提到的简化版。


2. **环境变量加载模式**
在运行需要本地配置的测试时，统一采用以下通用加载模式（确保 `.env` 中的环境变量能自动导出并传递给 Go 测试子进程）：
```bash
set -a; [ -f .env ] && . .env; set +a

```


3. **MySQL 测试要求**
* 运行 MySQL 测试前，先通过上述模式加载 `.env`，使 `TEST_DB_URL` 生效。
* 采用真实数据库，**不要自行启动 MySQL Docker**。


**标准执行命令示例：**
```bash
set -a; [ -f ../.env ] && . ../.env; set +a; \
go test ./storage/mysqlobda/... ./obda/dialect/mysql/... -v 2>&1 | tail -250

```


### 知识库

`docs/solutions/` — 已归档的问题解决方案（bug 修复、最佳实践、模式），按类别分目录，带 YAML frontmatter（module、tags、problem_type），在相关领域实现或调试时可查阅。
`CONCEPTS.md` — 项目共享领域词汇表（实体、命名流程、状态概念），熟悉代码库或讨论领域概念时可参考。
