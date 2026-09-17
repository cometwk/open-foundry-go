package orm

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/pkg/errors"
	"xorm.io/xorm"
)

// 兼容sqlx

// 查询单行记录，如果返回 0 行或多行都是错误
func SelectOneX(ctx context.Context, ql string, dest any, params ...interface{}) error {
	session := MustSession(ctx)
	defer session.Close()

	return SelectOneTX(session, ql, dest, params...)
}

// 查询单行记录，如果返回 0 行或多行都是错误
func SelectOneTX(session *xorm.Session, ql string, dest any, params ...interface{}) error {
	ok, err := session.SQL(ql, params...).Get(dest)
	if err != nil {
		return err
	}
	if !ok {
		return errors.Errorf("SQL: '%s' 未返回结果, 期望 1 行", ql)
	}
	return nil
}

func SelectOne(ql string, dest any, params ...interface{}) error {
	session := NewSession()
	defer session.Close()

	return SelectOneTX(session, ql, dest, params...)
}

// dest = slice of struct
func SelectX(ctx context.Context, ql string, dest any, params ...any) error {
	// SQL 中不能包含 in 语句
	session := MustSession(ctx)
	defer session.Close()

	return session.SQL(ql, params...).Find(dest)
}
func Select(ql string, dest any, params ...any) error {
	session := NewSession()
	defer session.Close()

	return session.SQL(ql, params...).Find(dest)
}

func UpsertX(ctx context.Context, entity interface{}) (int64, error) {
	session := MustSession(ctx)
	defer session.Close()

	return UpsertWithSession(session, entity)
}
func Upsert(entity interface{}) (int64, error) {
	session := NewSession()
	defer session.Close()

	return UpsertWithSession(session, entity)
}

func UpsertWithSession(session *xorm.Session, entity interface{}) (int64, error) {
	engine := session.Engine()
	schema, err := engine.TableInfo(entity)
	if err != nil {
		return 0, errors.Wrapf(err, "schema not found")
	}

	pkValues, hasPK := GetPkValues(engine, entity, schema.PrimaryKeys)
	// 根据键值判断执行更新还是插入
	if hasPK {
		// 先查询，如果存在则更新，不存在则插入
		foundEntity := reflect.New(schema.Type).Interface()
		found, err := session.ID(pkValues).Get(foundEntity)
		if err != nil {
			return 0, err
		}

		if found {
			// 比较且合并entity和foundEntity的值
			entityVal := reflect.ValueOf(entity).Elem()
			foundEntityVal := reflect.ValueOf(foundEntity).Elem()

			cols := []string{}
			for _, col := range schema.Columns() {
				colName := col.Name
				fieldName := col.FieldName
				v := entityVal.FieldByName(fieldName)
				foundV := foundEntityVal.FieldByName(fieldName)

				if !v.IsValid() || !foundV.IsValid() {
					slog.Error("upsert field not found",
						"table", schema.Name,
						"colName", colName,
						"fieldName", fieldName,
						"v", v.IsValid(),
						"foundV", foundV.IsValid(),
					)
					panic("upsert() field not found:" + "table " + schema.Name + ", col=" + colName + ", field=" + fieldName)
				}

				if !reflect.DeepEqual(v.Interface(), foundV.Interface()) {
					cols = append(cols, colName)
				}
			}
			if len(cols) == 0 {
				return 0, nil
			}
			return session.ID(pkValues).Cols(cols...).Update(entity)
		}
	}

	// 执行插入
	i, err := session.Insert(entity)
	return i, err
}

// 执行 insert/update/delete，必须且只能影响 1 行
func ExecOneX(ctx context.Context, ql string, params ...any) error {
	session := MustSession(ctx)
	defer session.Close()
	return ExecOneTX(session, ql, params...)
}
func ExecOneTX(session *xorm.Session, ql string, params ...any) error {
	p := []interface{}{ql}
	p = append(p, params...)
	rows, err := session.Exec(p...)
	if err != nil {
		return err
	}
	rowsAffected, err := rows.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return fmt.Errorf("SQL: '%s' 影响了 %d 行, 期望 1 行", ql, rowsAffected)
	}
	return nil
}
func ExecOne(ql string, params ...any) error {
	session := NewSession()
	defer session.Close()

	return ExecOneTX(session, ql, params...)
}

// 执行 insert/update/delete
func ExecX(ctx context.Context, ql string, params ...any) error {
	session := MustSession(ctx)
	defer session.Close()

	return ExecTX(session, ql, params...)
}
func ExecTX(session *xorm.Session, ql string, params ...any) error {
	p := []interface{}{ql}
	p = append(p, params...)
	_, err := session.Exec(p...)
	if err != nil {
		return err
	}
	return nil
}
func Exec(ql string, params ...any) error {
	session := NewSession()
	defer session.Close()

	return ExecTX(session, ql, params...)
}

func MustAffected1Row(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("SQL: 影响了 %d 行, 期望 1 行", n)
	}
	return nil
}
