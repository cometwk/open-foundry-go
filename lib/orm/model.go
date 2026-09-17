package orm

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"

	"github.com/pkg/errors"
	"xorm.io/xorm"
	"xorm.io/xorm/schemas"
)

type impModel struct {
	engine *ormEngine
	ID     string // 模型ID, 一般对应表名
	Schema *schemas.Table
	Type   reflect.Type
}

type PageResult = Result[any]

func (m *impModel) NewEntity() any {
	return reflect.New(m.Type).Interface()
}

func (m *impModel) NewSlice() any {
	return reflect.New(reflect.SliceOf(m.Type)).Elem().Interface()
}

func (m *impModel) NewSlicePtr() any {
	return reflect.New(reflect.SliceOf(m.Type)).Interface()
}

func (m *impModel) Sync() error {
	fmt.Printf("\n\n")
	entity := m.NewEntity()
	// fmt.Printf("entity: %+v\n", entity)

	// 使用 m.db 替代 db
	err := m.engine.db.Table(m.ID).Sync(entity)
	if err != nil {
		// panic(err)
		fmt.Printf("sync error: %+v\n", err)
		return err
	}
	return nil
}

func (m *impModel) PrintLastSQL(session *xorm.Session) {
	sql, args := session.LastSQL()
	fmt.Printf("last sql: %s\n", sql)
	fmt.Printf("last args: %+v\n", args)
}

func (m *impModel) WithSession(session *xorm.Session) ModelOps {
	return &impOps{
		model:   m,
		engine:  m.engine,
		session: session, // 采用外部的session
	}
}

func (m *impModel) TableSchema() *schemas.Table {
	return m.Schema
}

// 如果row是map类型，需要转换为实体类型
func (m *impModel) mapToEntity(row InputRow) (any, error) {
	// 如果已经是结构体类型，直接返回
	// 检查类型是否匹配，需要考虑指针类型
	t := reflect.TypeOf(row)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == m.Type {
		return row, nil
	}

	if t.Kind() == reflect.Map {
		entity := reflect.New(m.Type).Interface()
		err := MapToValue(row.(map[string]any), entity)
		if err != nil {
			return nil, fmt.Errorf("failed to map to entity: %w", err)
		}
		return entity, nil
	}
	return nil, fmt.Errorf("mapToEntity error unknown type: %+v, %+v", t, row)
}

func (m *impModel) MapToEntity(row InputRow) (any, error) {
	return m.mapToEntity((row))
}
func (m *impModel) Close() error {
	// empty function
	return nil
}

//
// DB OPS
//

func (m *impModel) Insert(row InputRow) (int64, error) {
	session := m.engine.db.NewSession()
	defer session.Close()

	return m.insert(session, row)
}

func (m *impModel) insert(session *xorm.Session, row InputRow) (int64, error) {
	entity, err := m.mapToEntity(row)
	if err != nil {
		return 0, err
	}
	return m.insert0(session, entity)
}

func (m *impModel) insert0(session *xorm.Session, entity any) (int64, error) {
	i, err := session.Table(m.ID).Insert(entity)
	return i, err
}

func (m *impModel) UpdateByPK(row InputRow) (int64, error) {
	session := m.engine.db.NewSession()
	defer session.Close()
	return m.updateByPK(session, row)
}

func (m *impModel) UpdateIn(row InputRow, pkValues []any) (int64, error) {
	session := m.engine.db.NewSession()
	defer session.Close()
	return m.updateIn(session, row, pkValues)
}

func (m *impModel) updateIn(session *xorm.Session, row InputRow, pkValues []any) (int64, error) {
	if len(pkValues) == 0 {
		return 0, fmt.Errorf("主键值不能为空")
	}
	if len(m.Schema.PrimaryKeys) != 1 {
		return 0, fmt.Errorf("模型 %s 的 updateIn 操作不支持复合主键", m.ID)
	}
	entity, err := m.mapToEntity(row)
	if err != nil {
		return 0, err
	}
	column := m.Schema.PrimaryKeys[0]
	cols := m.Schema.ColumnsSeq()
	cols = slices.DeleteFunc(cols, func(col string) bool {
		return col == column
	})
	i, err := session.Table(m.ID).In(column, pkValues).Cols(cols...).Update(entity)
	return i, err
}

func (m *impModel) UpdateWhere(row InputRow, params map[string]string) (int64, error) {
	session := m.engine.db.NewSession()
	defer session.Close()
	return m.updateWhere(session, row, params)
}

func (m *impModel) updateWhere(session *xorm.Session, row InputRow, params map[string]string) (int64, error) {
	err := BindQueryString(session, params)
	if err != nil {
		return 0, err
	}
	entity, err := m.mapToEntity(row)
	if err != nil {
		return 0, err
	}
	return session.Table(m.ID).Update(entity)
}

// pkValue 可以是单个值，也可以是多个值
func (m *impModel) GetByPK(pkValue any) (any, error) {
	session := m.engine.db.NewSession()
	defer session.Close()
	return m.getByPK(session, pkValue)
}

func (m *impModel) getByPK(session *xorm.Session, pkValue any) (any, error) {
	entity := m.NewEntity()
	ok, err := session.Table(m.ID).ID(pkValue).Get(entity)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return entity, nil
}

func (m *impModel) DeleteByPK(pkValue any) (int64, error) {
	session := m.engine.db.NewSession()
	defer session.Close()
	return m.deleteByPK(session, pkValue)
}

func (m *impModel) deleteByPK(session *xorm.Session, pkValue any) (int64, error) {
	entity := m.NewEntity()
	i, err := session.Table(m.ID).ID(pkValue).Delete(entity)
	return i, err
}

func (m *impModel) DeleteIn(pkValues []any) (int64, error) {
	session := m.engine.db.NewSession()
	defer session.Close()
	return m.deleteIn(session, pkValues)
}

func (m *impModel) deleteIn(session *xorm.Session, pkValues []any) (int64, error) {
	if len(m.Schema.PrimaryKeys) != 1 {
		return 0, fmt.Errorf("模型 %s 的 deleteIn 操作不支持复合主键", m.ID)
	}
	entity := m.NewEntity()
	column := m.Schema.PrimaryKeys[0]
	i, err := session.Table(m.ID).In(column, pkValues).Delete(entity)
	return i, err
}

func (m *impModel) DeleteWhere(params map[string]string) (int64, error) {
	session := m.engine.db.NewSession()
	defer session.Close()
	return m.deleteWhere(session, params)
}

func (m *impModel) deleteWhere(session *xorm.Session, params map[string]string) (int64, error) {
	err := BindQueryString(session, params)
	if err != nil {
		return 0, err
	}

	entity := m.NewEntity()
	n, err := session.Table(m.ID).Delete(entity)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (m *impModel) Upsert(row InputRow) (int64, error) {
	session := m.engine.db.NewSession()
	defer session.Close()
	return m.upsert(session, row)
}

func (m *impModel) upsert(session *xorm.Session, row InputRow) (int64, error) {
	return m.upsert0(session, row)
}

// Search return anonymous entity slice
func (m *impModel) Search(params map[string]string) (any, error) {
	session := m.engine.db.NewSession()
	defer session.Close()
	return m.search(session, params)
}

func (m *impModel) search(session *xorm.Session, params map[string]string) (any, error) {
	err := BindQueryString(session, params)
	if err != nil {
		return nil, err
	}
	slicePtr := reflect.New(reflect.SliceOf(m.Type)).Interface()
	err = session.Table(m.ID).Find(slicePtr)
	if err != nil {
		return nil, err
	}
	// 将slicePtr转换为slice
	slice := reflect.ValueOf(slicePtr).Elem().Interface()
	return slice, nil
}

func (m *impModel) Count(params map[string]string) (int64, error) {
	session := m.engine.db.NewSession()
	defer session.Close()
	return m.count(session, params)
}

func (m *impModel) count(session *xorm.Session, params map[string]string) (int64, error) {
	err := BindQueryString(session, params)
	if err != nil {
		return 0, err
	}
	entity := m.NewEntity()
	return session.Table(m.ID).Count(entity)
}

func (m *impModel) SearchPage(params map[string]string) (*PageResult, error) {
	session := m.engine.db.NewSession()
	defer session.Close()
	return m.searchPage(session, params)
}

func (m *impModel) searchPage(session *xorm.Session, params map[string]string) (*PageResult, error) {
	page := 0
	pagesize := 10
	var err error
	if p, ok := params["page"]; ok {
		page, err = strconv.Atoi(p)
		if err != nil {
			return nil, errors.New("invalid page parameter")
		}
		delete(params, "page")
	}
	if ps, ok := params["pagesize"]; ok {
		pagesize, err = strconv.Atoi(ps)
		if err != nil {
			return nil, errors.New("invalid pagesize parameter")
		}
		delete(params, "pagesize")
	}
	if pagesize > 500 {
		return nil, errors.New("pagesize 最大值 500")
	}

	err = BindQueryString(session, params)
	if err != nil {
		return nil, err
	}
	rowsPtr := m.NewSlicePtr()
	count, err := session.Table(m.ID).Limit(pagesize, page*pagesize).FindAndCount(rowsPtr)
	if err != nil {
		return nil, err
	}

	rows := reflect.ValueOf(rowsPtr).Elem().Interface()

	return &PageResult{
		Data:     rows,
		Page:     int64(page),
		Pagesize: int64(pagesize),
		Total:    count,
	}, nil
}

// TX OPS

type impOps struct {
	model   *impModel
	engine  *ormEngine
	session *xorm.Session
}

func newModelOps(m *impModel) ModelOps {
	return &impOps{
		model:   m,
		engine:  m.engine,
		session: nil,
	}
}

// 事务
func (tx *impOps) WithSession(session *xorm.Session) ModelOps {
	return &impOps{
		model:   tx.model,
		engine:  tx.engine,
		session: session, // 采用外部的session
	}
}

func (tx *impOps) TableSchema() *schemas.Table {
	return tx.model.TableSchema()
}

func (tx *impOps) Insert(row InputRow) (int64, error) {
	return tx.model.insert(tx.session, row)
}

func (tx *impOps) UpdateByPK(row InputRow) (int64, error) {
	return tx.model.updateByPK(tx.session, row)
}

func (tx *impOps) UpdateIn(row InputRow, pkValues []any) (int64, error) {
	return tx.model.updateIn(tx.session, row, pkValues)
}

func (tx *impOps) GetByPK(pkValue any) (any, error) {
	return tx.model.getByPK(tx.session, pkValue)
}

func (tx *impOps) DeleteByPK(pkValue any) (int64, error) {
	return tx.model.deleteByPK(tx.session, pkValue)
}

func (tx *impOps) DeleteIn(pkValues []any) (int64, error) {
	return tx.model.deleteIn(tx.session, pkValues)
}

func (tx *impOps) DeleteWhere(params map[string]string) (int64, error) {
	return tx.model.deleteWhere(tx.session, params)
}

func (tx *impOps) Upsert(row InputRow) (int64, error) {
	return tx.model.upsert(tx.session, row)
}

func (tx *impOps) Search(params map[string]string) (any, error) {
	return tx.model.search(tx.session, params)
}

func (tx *impOps) Count(params map[string]string) (int64, error) {
	return tx.model.count(tx.session, params)
}

func (tx *impOps) SearchPage(params map[string]string) (*PageResult, error) {
	return tx.model.searchPage(tx.session, params)
}
