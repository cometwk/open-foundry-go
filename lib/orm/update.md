# detectUpdateColsByBeanDiff 完备性分析

本文针对 `update.go` 中 `detectUpdateColsByBeanDiff(bean, foundBean, primaryKeys, cols)` 的行为完备性进行说明。

## 结论

该函数在“典型 ORM 实体更新”场景下是可用且有效的：  
- 能排除主键字段；
- 能基于当前对象与数据库对象逐字段比较，得到最小更新列集合；
- 对指针字段、`time.Time` 字段有专门比较逻辑。

但它并非“绝对完备”的通用差异引擎，仍存在可预期的边界与语义风险（见下文）。

## 当前覆盖能力

1. **主键排除**
   - 通过 `slices.Contains(primaryKeys, colName)` 跳过主键，避免误更新主键列。

2. **按 xorm 列定义遍历**
   - 仅遍历 `TableInfo` 返回的 `cols`，不会误比较非映射字段。

3. **字段映射方式明确**
   - 使用 `col.FieldName` 映射到结构体字段，避免按 tag 猜测带来的不确定性。

4. **差异判定规则**
   - 指针字段：递归解引用比较，支持 `nil`/非 `nil` 判定。
   - `time.Time`：使用 `Time.Equal`，避免直接 `DeepEqual` 的时区/单调时钟细节噪声。
   - 其他类型：`reflect.DeepEqual`。

5. **无变化短路**
   - 当 `updateCols` 为空时，上层 `UpdateByPK` 返回 `0, nil`，避免无意义 UPDATE。

## 已知边界与不完备点

1. **未处理“忽略零值”策略**
   - 当前函数只做“是否变化”比较，不支持“变化了但零值不更新”策略。
   - 如果业务期望 PATCH 语义（只更新显式传入字段），该函数本身不区分“未赋值”和“赋零值”。

2. **未处理“忽略字段”白/黑名单**
   - 无 `IgnoreCols` 机制，审计字段（如 `created_at`）是否更新完全依赖 bean 当前值与库值是否一致。

3. **类型可比性依赖 `DeepEqual` 语义**
   - 某些 driver/自定义类型（如 `sql.Null*`、decimal、自定义 scanner）可能出现“语义相等但结构不等”或反之。

4. **未处理 nil 与零值的业务等价关系**
   - 例如某些字段业务上把 `nil` 与 `""` 视为等价，但当前会视为变化。

5. **字段不可导出/不可寻址的反射边界**
   - 当前通过 `FieldByName` 读取字段值；对复杂嵌套、匿名字段覆盖等场景，依赖 xorm 的 `FieldName` 输出稳定性。

6. **并发窗口**
   - 差异列来自“先查后更”（SELECT + UPDATE），在高并发下可能出现中间写入导致的“基于旧快照比较”问题。
   - 这属于流程级边界，不是函数本身 bug，但影响“完备性”定义中的一致性。

7. **浮点与时间精度语义**
   - `DeepEqual` 对浮点严格比较；数据库精度截断后，可能产生反复更新或误判。
   - `time.Time.Equal` 已优化时间比较，但数据库端 round/truncate 仍可能导致差异抖动。

## 可判定为“完备”的前提条件

在以下前提下，可认为函数“工程上完备”：

- 实体字段类型与数据库回填类型稳定一致；
- 业务接受严格值比较（非 PATCH 语义）；
- 不要求忽略零值/忽略列；
- 并发一致性由事务或版本号机制在更高层保证。

## 建议补强项（按优先级）

1. **增加可选参数**
   - `IgnoreCols []string`
   - `IgnoreZeroValues bool`
   - `TreatNilAsZeroFor []string`（可选）

2. **增加专用比较器扩展点**
   - 按列注册比较函数：`map[string]func(a, b reflect.Value) bool`

3. **补充并发安全策略**
   - 支持版本号列（乐观锁）或在事务中执行“查+更”。

4. **补充测试矩阵**
   - 指针字段 nil/非 nil
   - `time.Time` 精度差异
   - `sql.Null*` / decimal 类型
   - 浮点字段
   - 无变化返回 `0, nil`
   - 主键排除验证

## 总体评价

`detectUpdateColsByBeanDiff` 目前属于“**实用型差异检测**”：  
对常规 CRUD 足够可靠，但不是“覆盖所有业务语义与类型生态”的全量完备实现。  
若要达到“严格完备”，建议引入可配置策略与类型比较扩展点。
