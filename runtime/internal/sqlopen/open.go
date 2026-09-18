package sqlopen

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/qustavo/sqlhooks/v2"
)

// Hooks satisfies the sqlhook.Hooks interface
type Hooks struct{}

var LogSQL = false

func (h *Hooks) Before(ctx context.Context, query string, args ...interface{}) (context.Context, error) {
	return context.WithValue(ctx, "begin", time.Now()), nil
}

// Before 在 db.QueryContext 会触发2次，After 会触发1次
func (h *Hooks) After(ctx context.Context, query string, args ...interface{}) (context.Context, error) {
	begin := ctx.Value("begin").(time.Time)
	if LogSQL {
		slog.InfoContext(ctx, query, "args", args, "took", time.Since(begin).Milliseconds())
		// slog.InfoContext(ctx, fmt.Sprintf("%s\n%q\n", query, args), "took", time.Since(begin))
		// slog.InfoContext(ctx, fmt.Sprintf(". took: %s\n", time.Since(begin)))
	}
	return ctx, nil
}

func init() {
	sql.Register("mysql-hooks", sqlhooks.Wrap(&mysql.MySQLDriver{}, &Hooks{}))
}

func Open(driverName, dataSourceName string) (*sql.DB, error) {
	// TODO: 设置最大连接数、最大空闲连接数、最大连接时间
	return sql.Open(driverName+"-hooks", dataSourceName)
}
