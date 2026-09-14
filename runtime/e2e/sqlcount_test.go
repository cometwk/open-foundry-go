package e2e_test

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/qustavo/sqlhooks/v2"
)

// sqlCountDriver is unique: sql.Register panics on a duplicate name, and
// sqlopen already owns "mysql-hooks".
const sqlCountDriver = "mysql-e2e-count"

type sqlCounter struct {
	mu    sync.Mutex
	stmts []string
}

func (c *sqlCounter) Before(ctx context.Context, query string, _ ...interface{}) (context.Context, error) {
	if isSchemaNoise(query) {
		return ctx, nil
	}
	c.mu.Lock()
	c.stmts = append(c.stmts, query)
	c.mu.Unlock()
	return ctx, nil
}

func (c *sqlCounter) After(ctx context.Context, _ string, _ ...interface{}) (context.Context, error) {
	return ctx, nil
}

func (c *sqlCounter) Reset() {
	c.mu.Lock()
	c.stmts = nil
	c.mu.Unlock()
}

func (c *sqlCounter) Snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return collapseHookDupes(c.stmts)
}

// collapseHookDupes halves consecutive identical statements. sqlhooks v2
// fires Before on both QueryerContext and Stmt for one database/sql call,
// so a real 6-query expand shows up as 12 identical pairs.
func collapseHookDupes(qs []string) []string {
	var out []string
	for i := 0; i < len(qs); {
		j := i + 1
		for j < len(qs) && qs[j] == qs[i] {
			j++
		}
		keep := (j - i) / 2
		if keep == 0 {
			keep = 1
		}
		for k := 0; k < keep; k++ {
			out = append(out, qs[i])
		}
		i = j
	}
	return out
}

var e2eSQL = &sqlCounter{}

func init() {
	sql.Register(sqlCountDriver, sqlhooks.Wrap(&mysql.MySQLDriver{}, e2eSQL))
}

func resetSQLCount() { e2eSQL.Reset() }

func isSchemaNoise(q string) bool {
	u := strings.ToUpper(q)
	return strings.Contains(u, "INFORMATION_SCHEMA") ||
		strings.Contains(u, "FOREIGN_KEY_CHECKS")
}

// assertTwoHopSQLBaseline locks the current expand SQL shape. Memory has
// no SQL — skip rather than fake a count. P2 drops the two COUNT queries.
func assertTwoHopSQLBaseline(t *testing.T, backend string) {
	t.Helper()
	if backend != backendMySQL {
		return
	}
	got := e2eSQL.Snapshot()
	wantRoles := []string{
		"root_get",
		"start_reload",
		"traverse_page",
		"hydrate_page",
	}
	if len(got) != len(wantRoles) {
		t.Fatalf("2-hop SQL count = %d, want %d:\n%s", len(got), len(wantRoles), formatSQL(got))
	}
	for i, q := range got {
		role := classifyTwoHopSQL(i, q)
		if role != wantRoles[i] {
			t.Fatalf("SQL[%d] role = %s, want %s\n%s\nall:\n%s", i, role, wantRoles[i], q, formatSQL(got))
		}
	}
}

func classifyTwoHopSQL(i int, q string) string {
	u := strings.ToUpper(q)
	count := strings.Contains(u, "COUNT(*)")
	join := strings.Contains(u, " JOIN ")
	switch {
	case count && join:
		return "traverse_count"
	case count:
		return "hydrate_count"
	case join:
		return "traverse_page"
	case strings.Contains(u, "`BRANCH`") || strings.Contains(u, " FROM BRANCH"):
		return "hydrate_page"
	case strings.Contains(u, "`BOOK`") || strings.Contains(u, " FROM BOOK"):
		if i == 0 {
			return "root_get"
		}
		return "start_reload"
	default:
		return "other"
	}
}

func TestCollapseHookDupes(t *testing.T) {
	got := collapseHookDupes([]string{"a", "a", "b", "b", "c", "c"})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("%v", got)
	}
	if keep := collapseHookDupes([]string{"solo"}); len(keep) != 1 || keep[0] != "solo" {
		t.Fatalf("%v", keep)
	}
}

func formatSQL(qs []string) string {
	var b strings.Builder
	for i, q := range qs {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimSpace(q))
	}
	return b.String()
}
