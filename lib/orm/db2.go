package orm

import (
	"log/slog"

	"github.com/openfoundry/lib/env"
)

// 多数据库支持
var engine2 *ormEngine

func init() {
	if env.String("DB_URL2", "") != "" {
		// driver 可能跟 engine1 不同
		engine2 = newOrmEngine(env.String("DB_DRIVER2", env.MustString("DB_DRIVER")), env.MustString("DB_URL2"))
	}
}

func MustLoadStructModel2[T any]() Model {
	m, err := loadStructModel[T](engine2)
	if err != nil {
		panic(err)
	}
	return m
}

func InitDB2() {
	if engine2 == nil {
		panic("数据库 engine2 未初始化, 请检查 DB_URL2 环境变量是否已设置")
	}
	engine2.initDB()
}

func SetLogger2(logger *slog.Logger) {
	engine2.setLogger(logger.With(slog.String("module", "orm2")))
}

func PrintCreateTableSQL2[T any]() {
	err := printCreateTableSQL[T](engine2)
	if err != nil {
		panic(err)
	}
}
