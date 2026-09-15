package testdb

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
)

// SetupMySQL 设置 MySQL 容器
func SetupMySQL(ctx context.Context) (*sql.DB, func(), error) {
	mysqlContainer, err := mysql.Run(
		ctx,
		"mysql:8.4.11",
		mysql.WithDatabase("testdb"),
		mysql.WithUsername("root"),
		mysql.WithPassword("rootpass"),

		// 测试数据库全部放内存
		testcontainers.WithTmpfs(map[string]string{
			"/var/lib/mysql": "rw,noexec,nosuid,size=1024m",
		}),
	)
	if err != nil {
		return nil, nil, err
	}

	dsn, err := mysqlContainer.ConnectionString(ctx)
	if err != nil {
		_ = testcontainers.TerminateContainer(mysqlContainer)
		return nil, nil, err
	}
	fmt.Println("dsn", dsn)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		_ = testcontainers.TerminateContainer(mysqlContainer)
		return nil, nil, err
	}

	cleanup := func() {
		_ = db.Close()
		_ = testcontainers.TerminateContainer(mysqlContainer)
	}

	return db, cleanup, nil
}
