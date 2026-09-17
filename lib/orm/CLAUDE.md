- 用 bool 字段 和 UseBool()
- bool 字段针对 queryString 有问题
    - 更新
    - 查询
    - 最好采用 int
- [x] model.Update 可以自动判断需要更新的字段列表, map or entity

---

- `package orm` 是采用 "xorm.io/xorm" 实现的model操作。
- model 可以根据两种方式定义：标准的 xorm struct 的定义方式，和类似 xorm struct 的json定义方式
- `query.go` 实现了一种查询方式，`query.md` 描述该查询方式

