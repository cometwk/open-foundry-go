package orm

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/openfoundry/lib/util"
	"xorm.io/xorm"
)

// 新的 update 方式， 根据 map 字段名来更新
func (m *impModel) updateByPK(session *xorm.Session, row InputRow) (int64, error) {
	entity, updateCols, err := m.rowToUpdateEntity(row)
	if err != nil {
		return 0, err
	}
	if len(updateCols) == 0 {
		// 若输入对象就是实体对象，采用 upsert 逻辑
		return m.upsertWithOptionsEntity(session, entity, nil, &UpsertOptions{
			IgnoreZeroValues: false,
			UpdateMode:       true,
		})
	}

	pkValue, hasPK := GetPkValues(session.Engine(), entity, m.Schema.PrimaryKeys)
	if !hasPK {
		return 0, fmt.Errorf("模型 %s 没有设置主键", m.ID)
	}
	if pkValue == nil || reflect.ValueOf(pkValue).IsZero() {
		return 0, fmt.Errorf("主键值不能为nil或0")
	}

	return session.Table(m.ID).ID(pkValue).Cols(updateCols...).UseBool().Update(entity)
}

// 如果row是map类型，需要转换为实体类型
// 返回: (entity, updateCols, error)
// - entity: 转换后的实体对象
// - updateCols: 需要更新的列名列表（如果输入是结构体，返回 nil，表示需要更新所有非主键列）
func (m *impModel) rowToUpdateEntity(row InputRow) (any, []string, error) {
	// 如果已经是结构体类型，直接返回
	// 检查类型是否匹配，需要考虑指针类型
	t := reflect.TypeOf(row)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == m.Type {
		// 输入是结构体类型，返回 nil 表示需要更新所有非主键列
		// 调用方可以根据需要决定如何处理
		return row, nil, nil
	}

	var rowMap map[string]any
	if t.Kind() != reflect.Map {
		// 非 map 类型，转换为 map
		rowMap = util.StructToMapFast(row)
	} else {
		rowMap = row.(map[string]any)
	}
	entity := reflect.New(m.Type).Interface()
	err := MapToValue(rowMap, entity)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to map to entity: %w", err)
	}

	// 检测需要更新的字段（基于 map 中的 key）
	updateCols, err := m.mapKeysToUpdateCols(entity, rowMap)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to map keys to update cols: %w", err)
	}
	return entity, updateCols, nil
}

// 根据 map keys 生成需要更新的字段名列表
// mapRow 中的 key 应该是 JSON tag 名称，函数会将其转换为数据库列名
func (m *impModel) mapKeysToUpdateCols(entity any, mapRow map[string]any) ([]string, error) {
	// 预分配容量以提高性能
	updateCols := make([]string, 0, len(mapRow))

	for mapKey := range mapRow {
		colName, ok := GetDBNameByJSONTag(m.engine.db, entity, mapKey)
		if !ok {
			continue // 跳过无法映射的字段
		}

		// 跳过主键列
		if slices.Contains(m.Schema.PrimaryKeys, colName) {
			continue
		}

		// 添加到更新列列表
		updateCols = append(updateCols, colName)
	}

	return updateCols, nil
}

// ensureUpdateCols 确保至少有一个更新列，避免更新失败
// 如果没有指定更新列，优先使用 Updated 字段，否则使用主键字段
func (m *impModel) ensureUpdateCols(updateCols []string) []string {
	if len(updateCols) == 0 {
		// 直接返回空数组容易引起歧义，至少更新一个字段
		if m.Schema.Updated != "" {
			updateCols = append(updateCols, m.Schema.Updated)
		} else {
			updateCols = append(updateCols, m.Schema.PrimaryKeys...)
		}
	}
	return updateCols
}

// detectChangedColumns 根据对比新老记录字段的值，来确定需要更新的字段列表名
func (m *impModel) detectUpdateColsByValue(entity, foundEntity any, opts *UpsertOptions) ([]string, error) {
	entityVal := reflect.ValueOf(entity).Elem()
	foundEntityVal := reflect.ValueOf(foundEntity).Elem()

	var updateCols []string
	ignoreColsMap := make(map[string]bool)
	for _, col := range opts.IgnoreCols {
		ignoreColsMap[col] = true
	}

	for _, col := range m.Schema.Columns() {
		colName := col.Name

		// 跳过忽略的列
		if ignoreColsMap[colName] {
			continue
		}

		// 跳过主键列
		if slices.Contains(m.Schema.PrimaryKeys, colName) {
			continue
		}

		fieldName, ok := GetStructFieldNameByDBName(m.engine.db, entity, colName)
		if !ok {
			continue // 跳过无法映射的字段
		}

		entityField := entityVal.FieldByName(fieldName)
		foundField := foundEntityVal.FieldByName(fieldName)

		if !entityField.IsValid() || !foundField.IsValid() {
			continue
		}

		// 如果忽略零值且当前字段是零值，跳过
		if opts.IgnoreZeroValues && entityField.IsZero() {
			continue
		}

		// 比较字段值是否发生变化
		if !m.fieldsEqual(entityField, foundField) {
			updateCols = append(updateCols, colName)
		}
	}

	return updateCols, nil
}
