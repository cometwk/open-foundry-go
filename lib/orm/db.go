package orm

import (
	"context"
	"log/slog"
	"reflect"

	"xorm.io/xorm"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib" // 必须使用 stdlib 包适配 database/sql
	_ "github.com/mattn/go-sqlite3"    // file:test.db?_busy_timeout=5000&_journal_mode=WAL
	"github.com/openfoundry/lib/env"
)

// engine1 as default engine
var engine1 *ormEngine = nil

func MustModel(id string) Model {
	m := engine1.mustModel(id)
	return m
}

func MustModelOpsById(id string, session *xorm.Session) ModelOps {
	m := engine1.mustModel(id)
	ops := newModelOps(m)
	if session != nil {
		ops = ops.WithSession(session)
	}
	return ops
}

func MustModelOps[T any]() ModelOps {
	return mustModelOps[T](engine1)
}

func MustLoadStructModel[T any]() Model {
	m, err := loadStructModel[T](engine1)
	if err != nil {
		panic(err)
	}
	return m
}

func MustEntityOps[T any]() EntityOps[T] {
	return mustEntityModel[T](engine1)
}

func MustSession(ctx context.Context) *xorm.Session {
	return engine1.mustSession(ctx)
}

func NewSession() *xorm.Session {
	return engine1.db.NewSession()
}

func InitDefaultDB() *xorm.Engine {
	return InitDB(env.MustString("DB_DRIVER"), env.MustString("DB_URL"))
}

func InitEngine(engine *xorm.Engine) {
	engine1 = &ormEngine{
		db:              engine,
		modelDefsByID:   make(map[string]*impModel),
		modelDefsByType: make(map[reflect.Type]*impModel),
	}
}

func InitDB(dbDriver, dbUrl string) *xorm.Engine {
	if engine1 != nil {
		slog.Debug("engine1 already initialized")
		return engine1.db
	}
	if engine1 == nil {
		engine1 = newOrmEngine(dbDriver, dbUrl)
	}
	err := engine1.initDB()
	if err != nil {
		panic(err)
	}
	SetLogger(nil)
	return engine1.db
}

func MustDB() *xorm.Engine {
	if engine1 == nil || engine1.db == nil {
		panic("db not initialized")
	}
	return engine1.db
}

func GetDB() *xorm.Engine {
	if engine1 == nil {
		return nil
	}
	return engine1.db
}

type ctxKey string

const sessionKey = ctxKey("__orm_session_key")

// SessionFromContext 从 context 中获取 session
// 通过 ctx 传递 session , 主要用于传递事务上下文
//
// session.ctx 不允许访问，故自定义处理
func SessionFromContext(c context.Context) *xorm.Session {
	if s, ok := c.Value(sessionKey).(*xorm.Session); ok {
		return s
	}
	return nil
}

// WithSession 将 session 存储到 context 中
func WithSession(c context.Context, session *xorm.Session) context.Context {
	return context.WithValue(c, sessionKey, session)
}

func SetLogger(logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	engine1.setLogger(logger.With(slog.String("module", "orm")))
}

func PrintCreateTableSQL[T any]() {
	err := printCreateTableSQL[T](engine1)
	if err != nil {
		panic(err)
	}
}

func AllModels() []*impModel {
	return engine1.allModels()
}
