package orm

import (
	"fmt"
	"reflect"

	"github.com/pkg/errors"
	"xorm.io/xorm"
)

// model 是 any 类型，model2 是泛型版本
type impEntity[T any] struct {
	model   *impModel
	engine  *ormEngine
	session *xorm.Session
}

var _ EntityOps[any] = &impEntity[any]{}

func newEntityModel[T any](m *impModel) EntityOps[T] {
	return &impEntity[T]{
		model:   m,
		engine:  m.engine,
		session: nil,
	}
}

// WithSession returns a new EntityOps with a session for transaction.
func (tx *impEntity[T]) WithSession(session *xorm.Session) EntityOps[T] {
	return &impEntity[T]{
		model:   tx.model,
		engine:  tx.engine,
		session: session, // 采用外部的session
	}
}

func (tx *impEntity[T]) Insert(entity *T) (int64, error) {
	if tx.session != nil {
		return tx.model.insert(tx.session, entity)
	}
	return tx.model.Insert(entity)
}

func (tx *impEntity[T]) InsertOne(entity *T) error {
	n, err := tx.Insert(entity)
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("insert no rows affected")
	}
	return nil
}

func (tx *impEntity[T]) Update(entity InputRow) (int64, error) {
	if tx.session != nil {
		return tx.model.updateByPK(tx.session, entity)
	}
	return tx.model.UpdateByPK(entity)
}

func (tx *impEntity[T]) UpdateOne(entity InputRow) error {
	n, err := tx.Update(entity)
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("update no rows affected")
	}
	return nil
}

func (tx *impEntity[T]) UpdateIn(entity *T, pkValues []any) (int64, error) {
	if tx.session != nil {
		return tx.model.updateIn(tx.session, entity, pkValues)
	}
	return tx.model.UpdateIn(entity, pkValues)
}

func (tx *impEntity[T]) UpdateWhere(entity *T, params map[string]string) (int64, error) {
	if tx.session != nil {
		return tx.model.updateWhere(tx.session, entity, params)
	}
	return tx.model.UpdateWhere(entity, params)
}

func (tx *impEntity[T]) GetOne(pkValue any) (*T, error) {
	entity, err := tx.Get(pkValue)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New("entity not found")
	}
	return entity, nil
}
func (tx *impEntity[T]) Get(pkValue any) (*T, error) {
	var entity T
	var sess *xorm.Session
	if tx.session != nil {
		sess = tx.session
	} else {
		sess = tx.engine.db.NewSession()
		defer sess.Close()
	}

	ok, err := sess.Table(tx.model.ID).ID(pkValue).Get(&entity)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &entity, nil
}

func (tx *impEntity[T]) Delete(pkValue any) (int64, error) {
	if pkValue == nil || reflect.ValueOf(pkValue).IsZero() {
		return 0, fmt.Errorf("主键值不能为nil或为0")
	}
	if tx.session != nil {
		return tx.model.deleteByPK(tx.session, pkValue)
	}
	return tx.model.DeleteByPK(pkValue)
}

func (tx *impEntity[T]) DeleteOne(pkValue any) error {
	n, err := tx.Delete(pkValue)
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("delete no rows affected")
	}
	return nil
}

func (tx *impEntity[T]) DeleteIn(pkValues []any) (int64, error) {
	if len(pkValues) == 0 {
		return 0, fmt.Errorf("主键值不能为空")
	}
	if tx.session != nil {
		return tx.model.deleteIn(tx.session, pkValues)
	}
	return tx.model.DeleteIn(pkValues)
}

func (tx *impEntity[T]) DeleteWhere(params map[string]string) (int64, error) {
	if tx.session != nil {
		return tx.model.deleteWhere(tx.session, params)
	}
	return tx.model.DeleteWhere(params)
}

func (tx *impEntity[T]) Upsert(entity *T) (int64, error) {
	if tx.session != nil {
		return tx.model.upsert(tx.session, entity)
	}
	return tx.model.Upsert(entity)
}

func (tx *impEntity[T]) UpsertOne(entity *T) error {
	n, err := tx.Upsert(entity)
	if err != nil {
		return err
	}
	if n == 0 {
		// Upsert can legitimately affect 0 rows if the data is identical.
		// This might not be an error condition.
		// For now, we keep the original behavior.
		return errors.New("upsert no rows affected")
	}
	return nil
}

func (tx *impEntity[T]) Search(params map[string]string) ([]T, error) {
	var r any
	var err error
	if tx.session != nil {
		r, err = tx.model.search(tx.session, params)
	} else {
		r, err = tx.model.Search(params)
	}

	if err != nil {
		return nil, err
	}
	return r.([]T), nil
}

func (tx *impEntity[T]) Count(params map[string]string) (int64, error) {
	if tx.session != nil {
		return tx.model.count(tx.session, params)
	} else {
		return tx.model.Count(params)
	}
}

func (tx *impEntity[T]) SearchPage(params map[string]string) (*Result[[]T], error) {
	var pageResult *PageResult
	var err error
	if tx.session != nil {
		pageResult, err = tx.model.searchPage(tx.session, params)
	} else {
		pageResult, err = tx.model.SearchPage(params)
	}

	if err != nil {
		return nil, err
	}

	// Convert pageResult.Data from any (which is likely []any) to []T
	data, ok := pageResult.Data.([]T)
	if !ok {
		return nil, fmt.Errorf("failed to assert page data to type []T")
	}

	return &Result[[]T]{
		Data:     data,
		Page:     pageResult.Page,
		Pagesize: pageResult.Pagesize,
		Total:    pageResult.Total,
	}, nil
}
