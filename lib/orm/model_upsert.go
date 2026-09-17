package orm

import (
	"fmt"
	"reflect"
	"time"

	"xorm.io/xorm"
)

type UpsertOptions struct {
	IgnoreCols       []string // 指定忽略的列（不参与更新）
	IgnoreZeroValues bool     // 是否忽略零值字段（默认true）
	UpdateMode       bool     // 更新模式, 不允许插入
}

func (m *impModel) upsert0(session *xorm.Session, row InputRow) (int64, error) {
	return m.UpsertWithOptions(session, row, &UpsertOptions{
		IgnoreZeroValues: true,
	})
}

func (m *impModel) UpsertWithOptions(session *xorm.Session, row InputRow, opts *UpsertOptions) (int64, error) {
	entity, updateCols, err := m.rowToUpdateEntity(row)
	if err != nil {
		return 0, err
	}

	if opts == nil {
		opts = &UpsertOptions{
			IgnoreZeroValues: true,
		}
	}

	return m.upsertWithOptionsEntity(session, entity, updateCols, opts)
}

func (m *impModel) upsertWithOptionsEntity(session *xorm.Session, entity any, updateCols []string, opts *UpsertOptions) (int64, error) {
	pkValues, hasPK := GetPkValues(session.Engine(), entity, m.Schema.PrimaryKeys)

	// 如果没有主键，直接插入
	if !hasPK {
		if opts.UpdateMode {
			return 0, fmt.Errorf("模型 %s 没有设置主键", m.ID)
		}
		return session.Table(m.ID).Insert(entity)
	}

	// 检查记录是否存在
	foundEntity, err := m.getByPK(session, pkValues)
	if err != nil {
		return 0, err
	}

	// 如果记录不存在，执行插入
	if foundEntity == nil {
		if opts.UpdateMode {
			return 0, fmt.Errorf("记录不存在, 不能更新")
		}
		return session.Table(m.ID).Insert(entity)
	}

	// 记录存在，执行更新
	if len(updateCols) == 0 {
		updateCols, err = m.detectUpdateColsByValue(entity, foundEntity, opts)
		if err != nil {
			return 0, err
		}
	}

	// 确保至少有一个更新列，避免更新失败
	updateCols = m.ensureUpdateCols(updateCols)

	// 执行更新（使用 UseBool() 确保布尔字段正确更新）
	return session.Table(m.ID).ID(pkValues).Cols(updateCols...).UseBool().Update(entity)
}

// fieldsEqual 比较两个字段是否相等
func (m *impModel) fieldsEqual(v1, v2 reflect.Value) bool {
	// 处理指针类型
	if v1.Kind() == reflect.Ptr && v2.Kind() == reflect.Ptr {
		if v1.IsNil() && v2.IsNil() {
			return true
		}
		if v1.IsNil() || v2.IsNil() {
			return false
		}
		return m.fieldsEqual(v1.Elem(), v2.Elem())
	}

	// 处理时间类型
	if v1.Type().String() == "time.Time" && v2.Type().String() == "time.Time" {
		t1 := v1.Interface().(time.Time)
		t2 := v2.Interface().(time.Time)
		return t1.Equal(t2)
	}

	// 其他类型使用 DeepEqual
	return reflect.DeepEqual(v1.Interface(), v2.Interface())
}

// filterValidColumns 过滤有效的列名
func (m *impModel) filterValidColumns(cols []string) []string {
	var validCols []string
	for _, col := range cols {
		if m.Schema.GetColumn(col) != nil {
			validCols = append(validCols, col)
		}
	}
	return validCols
}
