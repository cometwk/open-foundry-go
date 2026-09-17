package orm

import (
	"fmt"
	"reflect"
	"slices"
	"time"

	"xorm.io/xorm"
	"xorm.io/xorm/schemas"
)

// UpdateByPK 根据主键更新记录，仅支持 struct 或 *struct。
// 表名与主键自动通过 xorm.TableInfo 推导。
// 总结：这类函数还是不可靠
func UpdateByPK(session *xorm.Session, bean any) (int64, error) {
	if session == nil {
		return 0, fmt.Errorf("session 不能为空")
	}
	if bean == nil {
		return 0, fmt.Errorf("bean 不能为空")
	}

	beanType := reflect.TypeOf(bean)
	if beanType.Kind() == reflect.Ptr {
		beanType = beanType.Elem()
	}
	if beanType.Kind() != reflect.Struct {
		return 0, fmt.Errorf("bean 仅支持 struct 或 *struct")
	}

	table, err := session.Engine().TableInfo(bean)
	if err != nil {
		return 0, fmt.Errorf("获取表结构失败: %w", err)
	}
	if table == nil {
		return 0, fmt.Errorf("无法解析表结构")
	}

	pkValue, hasPK := getPKValuesByTable(bean, table.PrimaryKeys, table.Columns())
	if !hasPK {
		return 0, fmt.Errorf("模型 %s 没有设置主键", table.Name)
	}
	if pkValue == nil || reflect.ValueOf(pkValue).IsZero() {
		return 0, fmt.Errorf("主键值不能为nil或0")
	}

	foundBeanPtr := reflect.New(beanType).Interface()
	ok, err := session.Table(table.Name).ID(pkValue).Get(foundBeanPtr)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("记录不存在, 不能更新")
	}

	updateCols := detectUpdateColsByBeanDiff(bean, foundBeanPtr, table.PrimaryKeys, table.Columns())
	if len(updateCols) == 0 {
		return 0, nil
	}

	return session.Table(table.Name).ID(pkValue).Cols(updateCols...).UseBool().Update(bean)
}

func getPKValuesByTable(bean any, primaryKeys []string, cols []*schemas.Column) (any, bool) {
	if len(primaryKeys) == 0 {
		return nil, false
	}

	beanVal := reflect.ValueOf(bean)
	if beanVal.Kind() == reflect.Ptr {
		beanVal = beanVal.Elem()
	}
	if beanVal.Kind() != reflect.Struct {
		return nil, false
	}

	pkValues := make([]any, len(primaryKeys))
	for i, pk := range primaryKeys {
		fieldName, ok := getFieldNameByColumnName(cols, pk)
		if !ok {
			return nil, false
		}
		field := beanVal.FieldByName(fieldName)
		if !field.IsValid() {
			return nil, false
		}
		fieldValue := field.Interface()
		if fieldValue == nil || reflect.ValueOf(fieldValue).IsZero() {
			return nil, false
		}
		pkValues[i] = fieldValue
	}

	if len(pkValues) == 1 {
		return pkValues[0], true
	}
	return pkValues, true
}

func getFieldNameByColumnName(cols []*schemas.Column, columnName string) (string, bool) {
	for _, col := range cols {
		if col.Name == columnName {
			return col.FieldName, true
		}
	}
	return "", false
}

func detectUpdateColsByBeanDiff(bean any, foundBean any, primaryKeys []string, cols []*schemas.Column) []string {
	beanVal := reflect.ValueOf(bean)
	if beanVal.Kind() == reflect.Ptr {
		beanVal = beanVal.Elem()
	}
	foundVal := reflect.ValueOf(foundBean)
	if foundVal.Kind() == reflect.Ptr {
		foundVal = foundVal.Elem()
	}

	updateCols := make([]string, 0, len(cols))
	for _, col := range cols {
		colName := col.Name
		if slices.Contains(primaryKeys, colName) {
			continue
		}

		beanField := beanVal.FieldByName(col.FieldName)
		foundField := foundVal.FieldByName(col.FieldName)
		if !beanField.IsValid() || !foundField.IsValid() {
			continue
		}

		if !fieldsEqual(beanField, foundField) {
			updateCols = append(updateCols, colName)
		}
	}
	return updateCols
}

func fieldsEqual(v1, v2 reflect.Value) bool {
	if v1.Kind() == reflect.Ptr && v2.Kind() == reflect.Ptr {
		if v1.IsNil() && v2.IsNil() {
			return true
		}
		if v1.IsNil() || v2.IsNil() {
			return false
		}
		return fieldsEqual(v1.Elem(), v2.Elem())
	}

	if v1.Type().String() == "time.Time" && v2.Type().String() == "time.Time" {
		t1 := v1.Interface().(time.Time)
		t2 := v2.Interface().(time.Time)
		return t1.Equal(t2)
	}

	return reflect.DeepEqual(v1.Interface(), v2.Interface())
}
