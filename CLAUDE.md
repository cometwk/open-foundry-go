
当提到 Open Foundry — Technical Specification v2.0
时，要求先忽略一下内容：

- Sync Engine
- Security and Governance 相关的内容，如 Authorisation, Permission, Audit 等

单元测试：

- 当单元测试需要案例时，要求采用 domain-packs/library-pack/library-pack.md 里提到的简化版
- MySQL 测试时，加载 .env 中的 TEST_DB_URL 真实数据库，不要自行启动 mysql docker
