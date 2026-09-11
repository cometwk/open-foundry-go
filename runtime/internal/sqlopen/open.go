package sqlopen

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/qustavo/sqlhooks/v2"
	"modernc.org/sqlite"

	"github.com/go-sql-driver/mysql"
)

// Hooks satisfies the sqlhook.Hooks interface
type Hooks struct{}

func (h *Hooks) Before(ctx context.Context, query string, args ...interface{}) (context.Context, error) {
	fmt.Printf("> %s\n%q\n", query, args)
	return context.WithValue(ctx, "begin", time.Now()), nil
}

func (h *Hooks) After(ctx context.Context, query string, args ...interface{}) (context.Context, error) {
	begin := ctx.Value("begin").(time.Time)
	fmt.Printf(". took: %s\n", time.Since(begin))
	return ctx, nil
}

func init() {
	sql.Register("sqlite-hooks", sqlhooks.Wrap(&sqlite.Driver{}, &Hooks{}))
	sql.Register("mysql-hooks", sqlhooks.Wrap(&mysql.MySQLDriver{}, &Hooks{}))
}

func Open(driverName, dataSourceName string) (*sql.DB, error) {
	// TODO: 设置最大连接数、最大空闲连接数、最大连接时间
	return sql.Open(driverName+"-hooks", dataSourceName)
}
