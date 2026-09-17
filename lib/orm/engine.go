package orm

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"log/slog"

	"xorm.io/xorm"
	"xorm.io/xorm/names"
	"xorm.io/xorm/schemas"
)

// var xlog = logrus.WithField("module", "orm")

type InputRow = any // 输入参数要求map or struct ptr
type Result[T any] struct {
	Data     T     `json:"data"`
	Page     int64 `json:"page"`
	Pagesize int64 `json:"pagesize"`
	Total    int64 `json:"total"`
}

// 范型接口, 通过实体操作
type EntityOps[T any] interface {
	Get(pkValue any) (*T, error)
	Search(params map[string]string) ([]T, error)
	Count(params map[string]string) (int64, error)
	SearchPage(params map[string]string) (*Result[[]T], error)
	GetOne(pkValue any) (*T, error)

	Insert(entity *T) (int64, error)
	Upsert(entity *T) (int64, error)
	InsertOne(entity *T) error
	UpsertOne(entity *T) error

	Update(entity InputRow) (int64, error)
	// UpdateOne 能识别变更的记录字段, UpdateIn 和 UpdateWhere 暂时不能
	UpdateOne(entity InputRow) error
	UpdateIn(entity *T, pkValues []any) (int64, error)
	UpdateWhere(entity *T, params map[string]string) (int64, error)

	Delete(pkValue any) (int64, error)
	DeleteOne(pkValue any) error
	DeleteIn(pkValues []any) (int64, error)
	DeleteWhere(params map[string]string) (int64, error)

	WithSession(session *xorm.Session) EntityOps[T]
}

// 通用接口, 实体在输入参数要求map or struct, 输出参数肯定为struct ptr
type ModelOps interface {
	// 插入
	Insert(row InputRow) (int64, error)
	// 更新, 输入map，能识别需要更新的字段
	UpdateByPK(row InputRow) (int64, error)
	UpdateIn(row InputRow, pkValues []any) (int64, error)
	// 获取
	GetByPK(pkValue any) (any, error)
	// 删除
	DeleteByPK(pkValue any) (int64, error)
	DeleteIn(pkValues []any) (int64, error)
	DeleteWhere(params map[string]string) (int64, error)
	// 更新或插入
	Upsert(row InputRow) (int64, error)
	// 搜索
	Search(params map[string]string) (any, error)
	Count(params map[string]string) (int64, error)
	// 搜索分页
	SearchPage(params map[string]string) (*Result[any], error)

	WithSession(session *xorm.Session) ModelOps
	TableSchema() *schemas.Table
}

// 杂项
type Model interface {
	Sync() error
	NewEntity() any
	NewSlice() any
	NewSlicePtr() any
	PrintLastSQL(session *xorm.Session)
}

var (
	mapper = new(names.GonicMapper)
)

// 使用两个map分别存储id和type的映射, Go 遵循 "先写后读" (Init-then-read) 模式是线程安全的
type ormEngine struct {
	db              *xorm.Engine
	dbDriver        string
	dbUrl           string
	modelDefsByID   map[string]*impModel
	modelDefsByType map[reflect.Type]*impModel
}

func newOrmEngine(dbDriver, dbUrl string) *ormEngine {
	return &ormEngine{
		db:              nil,
		dbDriver:        dbDriver,
		dbUrl:           dbUrl,
		modelDefsByID:   make(map[string]*impModel),
		modelDefsByType: make(map[reflect.Type]*impModel),
	}
}

func (e *ormEngine) initDB() error {
	if e.db != nil {
		slog.Info("数据库已经初始化, 跳过初始化")
		return nil
	}
	engine, err := NewXormEngine(e.dbDriver, e.dbUrl)
	if err != nil {
		return err
	}
	e.db = engine
	return nil
}

func (e *ormEngine) setLogger(logger *slog.Logger) {
	xormLogger := NewXormLogrus(logger)
	e.db.SetLogger(xormLogger)
}

func NewXormEngine(dbDriver, dbUrl string) (*xorm.Engine, error) {
	engine, err := xorm.NewEngine(dbDriver, dbUrl)
	if err != nil {
		return nil, fmt.Errorf("数据库 xorm engine 初始化失败: %w", err)
	}

	engine.EnableSessionID(true) // 启用 session id

	// 全部采用 UTC 时区
	engine.TZLocation = time.UTC // 应用时使用 UTC
	engine.DatabaseTZ = time.UTC // 数据库存储时使用 UTC

	engine.SetMapper(mapper)
	engine.SetMaxOpenConns(10)

	{
		_, err := engine.Query("select 1")
		if err != nil {
			return nil, fmt.Errorf("数据库连接失败: %w", err)
		}
	}

	engine.ShowSQL(true)
	slog.Info("数据库初始化成功", "DB_DRIVER", dbDriver, "DB_URL", dbUrl)
	return engine, nil
}

// session绑定ctx, 否则采用默认的engine ctx
func (e *ormEngine) mustSession(c context.Context) *xorm.Session {
	// see: https://gitea.com/xorm/xorm/issues/1491
	session := e.db.NewSession()
	session.Context(c)

	return session
}

func (e *ormEngine) mustModel(id string) *impModel {
	if modelDef, ok := e.modelDefsByID[id]; ok {
		return modelDef
	}
	panic(fmt.Sprintf("model def not found: %s", id))
}

func (e *ormEngine) allModels() []*impModel {
	models := make([]*impModel, 0, len(e.modelDefsByType))
	for _, modelDef := range e.modelDefsByType {
		models = append(models, modelDef)
	}
	// sort by id
	slices.SortFunc(models, func(i, j *impModel) int {
		return strings.Compare(i.ID, j.ID)
	})
	return models
}

////
//// 范型函数，下面的函数不能放在 ormEngine 中，因为需要使用泛型
////

func loadStructModel[T any](e *ormEngine) (Model, error) {
	var o T

	entityType := reflect.TypeOf(o)
	if entityType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("type parameter T must be a struct type")
	}

	schema, err := e.db.TableInfo(&o)
	if err != nil {
		return nil, fmt.Errorf("failed to get table info for type %v: %w", entityType, err)
	}

	id := schema.Name
	model := &impModel{
		engine: e,
		ID:     id,
		Type:   entityType,
		Schema: schema,
	}

	e.modelDefsByID[id] = model
	e.modelDefsByType[entityType] = model
	return model, nil
}

func getEntityModel[T any](e *ormEngine) (EntityOps[T], error) {
	var o T
	entityType := reflect.TypeOf(o)
	if modelDef, ok := e.modelDefsByType[entityType]; ok {
		return newEntityModel[T](modelDef), nil
	}
	return nil, fmt.Errorf("model def not found for type: %v", reflect.TypeOf(o))
}

func mustEntityModel[T any](e *ormEngine) EntityOps[T] {
	def, err := getEntityModel[T](e)
	if err == nil {
		return def
	}
	var o T
	panic(fmt.Sprintf("model def not found for type: %v, with error: %v", reflect.TypeOf(o), err))
}

func getModelOps[T any](e *ormEngine) (ModelOps, error) {
	var o T
	entityType := reflect.TypeOf(o)
	if modelDef, ok := e.modelDefsByType[entityType]; ok {
		return newModelOps(modelDef), nil
	}
	return nil, fmt.Errorf("model def not found for type: %v", reflect.TypeOf(o))
}
func mustModelOps[T any](e *ormEngine) ModelOps {
	def, err := getModelOps[T](e)
	if err == nil {
		return def
	}
	var o T
	panic(fmt.Sprintf("model def not found for type: %v, with error: %v", reflect.TypeOf(o), err))
}

func printCreateTableSQL[T any](e *ormEngine) error {
	var o T
	entityType := reflect.TypeOf(o)
	modelDef, ok := e.modelDefsByType[entityType]
	if !ok {
		return fmt.Errorf("model not found for type: %v", reflect.TypeOf(o))
	}
	table := modelDef.Schema
	db := e.db
	sql, _, err := db.Dialect().CreateTableSQL(context.Background(), db.DB(), table, table.Name)
	if err != nil {
		return fmt.Errorf("failed to create %s table: %v", table.Name, err)
	}
	fmt.Printf("%s;\n\n", sql)
	return nil
}

// 返回结构体字段名，比如 "usr_name" → "Name"
func GetStructFieldNameByDBName(engine *xorm.Engine, bean any, dbField string) (string, bool) {
	table, err := engine.TableInfo(bean)
	if err != nil {
		return "", false
	}
	if col := table.GetColumn(dbField); col != nil {
		return col.FieldName, true
	}
	// 若用户没有定义, 则自动转换？算了，有点隐晦，不自动转换了
	// fieldName := mapper.Table2Obj(dbField)
	// return fieldName, true
	return "", false
}

// GetDBNameByStructFieldName 从结构体字段名获取数据库列名，比如 "Name" → "usr_name"
func GetDBNameByStructFieldName(engine *xorm.Engine, bean any, fieldName string) (string, bool) {
	table, err := engine.TableInfo(bean)
	if err != nil {
		return "", false
	}
	// 遍历所有列，查找匹配的字段名
	for _, col := range table.Columns() {
		if col.FieldName == fieldName {
			return col.Name, true
		}
	}
	return "", false
}

// GetDBNameByJSONTag 从 JSON 字段名获取数据库列名
// 支持 JSON 字段名（如 "name"，通过 json tag 定义）
func GetDBNameByJSONTag(engine *xorm.Engine, bean any, key string) (string, bool) {
	table, err := engine.TableInfo(bean)
	if err != nil {
		return "", false
	}

	// 遍历所有列，查找匹配的字段名或 JSON tag
	beanType := reflect.TypeOf(bean)
	if beanType.Kind() == reflect.Ptr {
		beanType = beanType.Elem()
	}

	for _, col := range table.Columns() {
		// 检查 JSON tag 是否匹配
		if beanType.Kind() == reflect.Struct {
			field, ok := beanType.FieldByName(col.FieldName)
			if ok {
				jsonTag := field.Tag.Get("json")
				if jsonTag != "" && jsonTag != "-" {
					// 处理 json tag 中可能包含的选项，如 "name,omitempty"
					jsonName := strings.Split(jsonTag, ",")[0]
					if jsonName == key {
						return col.Name, true
					}
				}
			}
		}
	}

	return "", false
}

// GetDBNameByFieldNameOrJSONTag 从结构体字段名或 JSON 字段名获取数据库列名
// 支持以下情况：
// 1. 直接是数据库列名（如 "usr_name"）
// 2. 结构体字段名（如 "Name"）
// 3. JSON 字段名（如 "name"，通过 json tag 定义）
func GetDBNameByFieldNameOrJSONTag(engine *xorm.Engine, bean any, key string) (string, bool) {
	table, err := engine.TableInfo(bean)
	if err != nil {
		return "", false
	}

	// 首先检查是否直接是数据库列名
	if col := table.GetColumn(key); col != nil {
		return col.Name, true
	}

	// 遍历所有列，查找匹配的字段名或 JSON tag
	beanType := reflect.TypeOf(bean)
	if beanType.Kind() == reflect.Ptr {
		beanType = beanType.Elem()
	}

	for _, col := range table.Columns() {
		// 检查字段名是否匹配
		if col.FieldName == key {
			return col.Name, true
		}

		// 检查 JSON tag 是否匹配
		if beanType.Kind() == reflect.Struct {
			field, ok := beanType.FieldByName(col.FieldName)
			if ok {
				jsonTag := field.Tag.Get("json")
				if jsonTag != "" && jsonTag != "-" {
					// 处理 json tag 中可能包含的选项，如 "name,omitempty"
					jsonName := strings.Split(jsonTag, ",")[0]
					if jsonName == key {
						return col.Name, true
					}
				}
			}
		}
	}

	return "", false
}

// GetPkValues from entity by primary key names, return pkValues(any, or []any) and hasPK(bool)
func GetPkValues(engine *xorm.Engine, entity any, primaryKeys []string) (any, bool) {
	val := reflect.ValueOf(entity)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	pkValues := make([]any, len(primaryKeys))
	for i, pk := range primaryKeys {
		pkName, ok := GetStructFieldNameByDBName(engine, entity, pk)
		if !ok {
			return nil, false
		}
		pkField := val.FieldByName(pkName)
		if !pkField.IsValid() {
			return nil, false
		}
		pkValue := pkField.Interface()
		// 检查主键值是否为空
		if pkValue == nil || reflect.ValueOf(pkValue).IsZero() {
			return nil, false
		}
		pkValues[i] = pkValue
	}

	if len(pkValues) == 1 {
		return pkValues[0], true
	}

	return pkValues, len(pkValues) > 0
}
