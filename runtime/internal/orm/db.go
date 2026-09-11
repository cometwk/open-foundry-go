package orm

import (
	"fmt"
	"log/slog"
	"time"

	_ "xorm.io/builder"
	"xorm.io/xorm"
)

func NewEngine(dbDriver, dbUrl string) (*xorm.Engine, error) {
	engine, err := xorm.NewEngine(dbDriver, dbUrl)
	if err != nil {
		return nil, fmt.Errorf("数据库 xorm engine 初始化失败: %w", err)
	}

	engine.EnableSessionID(true) // 启用 session id

	// 全部采用 UTC 时区
	engine.TZLocation = time.UTC // 应用时使用 UTC
	engine.DatabaseTZ = time.UTC // 数据库存储时使用 UTC

	// engine.SetMapper(mapper)
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
