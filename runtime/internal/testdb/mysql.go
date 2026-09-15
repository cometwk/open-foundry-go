// Package testdb opens the TEST_DB_URL MySQL database for integration tests.
// The DSN already names the database. Open drops tables around the test;
// OpenKeep drops only on connect; Connect leaves existing tables intact.
package testdb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"

	"github.com/openfoundry/runtime/internal/sqlopen"
)

var (
	loadEnvOnce sync.Once
	mu          sync.Mutex
	ready       = sync.NewCond(&mu)
	refs        = make(map[*testing.T]int)
)

// LoadEnv loads repo-root .env so TEST_DB_URL is visible under `go test`.
// `go test` sets cwd to the package directory, so this walks parents until
// it finds a .env (runtime/storage/mysqlobda is three levels below the repo
// root; e2e is only two).
func LoadEnv() {
	loadEnvOnce.Do(func() {
		dir, err := os.Getwd()
		if err != nil {
			return
		}
		for i := 0; i < 8; i++ {
			if err := godotenv.Load(filepath.Join(dir, ".env")); err == nil {
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				return
			}
			dir = parent
		}
	})
}

// DSN returns TEST_DB_URL after LoadEnv. Empty means MySQL tests should skip
// or fall back to memory.
func DSN() string {
	LoadEnv()
	return strings.TrimSpace(os.Getenv("TEST_DB_URL"))
}

type openOptions struct {
	dropOnOpen    bool
	dropOnCleanup bool
	logSQL        bool
	driver        string
}

// Open connects to the database named in TEST_DB_URL and drops existing tables
// so InitMappedSchema can recreate a fresh schema. Cleanup drops tables again.
// It does not CREATE DATABASE. Tests skip when TEST_DB_URL is unset. Access is
// serialized across distinct tests that share the same DSN. Nested Open on
// the same *testing.T just increments a refcount.
func Open(t *testing.T) *sql.DB {
	t.Helper()
	return open(t, openOptions{dropOnOpen: true, dropOnCleanup: true})
}

// OpenKeep is Open without dropping tables on cleanup. Use it to rebuild a
// schema that later reuse-mode tests will keep. SQL is logged via sqlopen.
func OpenKeep(t *testing.T) *sql.DB {
	t.Helper()
	return open(t, openOptions{dropOnOpen: true, dropOnCleanup: false, logSQL: true})
}

// Connect opens TEST_DB_URL without dropping tables on connect or cleanup.
// SQL is logged via sqlopen.
func Connect(t *testing.T) *sql.DB {
	t.Helper()
	return open(t, openOptions{dropOnOpen: false, dropOnCleanup: false, logSQL: true})
}

// OpenDriver is Open/OpenKeep/Connect with an explicit database/sql driver
// name (for e2e counting wrappers). Empty driver is invalid.
func OpenDriver(t *testing.T, driver string, dropOnOpen, dropOnCleanup bool) *sql.DB {
	t.Helper()
	if strings.TrimSpace(driver) == "" {
		t.Fatal("testdb: OpenDriver requires a driver name")
	}
	return open(t, openOptions{driver: driver, dropOnOpen: dropOnOpen, dropOnCleanup: dropOnCleanup})
}

func open(t *testing.T, opts openOptions) *sql.DB {
	t.Helper()
	dsn := DSN()
	if dsn == "" {
		t.Skip("TEST_DB_URL not set; MySQL integration tests skipped")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse TEST_DB_URL: %v", err)
	}
	if cfg.DBName == "" {
		t.Fatal("TEST_DB_URL must include a database name")
	}

	acquire(t)
	ok := false
	defer func() {
		if !ok {
			release(t)
		}
	}()

	var db *sql.DB
	switch {
	case opts.driver != "":
		db, err = sqlopen.Open(opts.driver, dsn)
	case opts.logSQL:
		db, err = sqlopen.Open("mysql", dsn)
	default:
		db, err = sqlopen.Open("mysql", dsn)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if opts.dropOnOpen {
		if err := DropTables(db); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if opts.dropOnCleanup {
			_ = DropTables(db)
		}
		_ = db.Close()
		release(t)
	})
	ok = true
	return db
}

func acquire(t *testing.T) {
	mu.Lock()
	defer mu.Unlock()
	for len(refs) > 0 && refs[t] == 0 {
		ready.Wait()
	}
	refs[t]++
}

func release(t *testing.T) {
	mu.Lock()
	defer mu.Unlock()
	if refs[t] <= 0 {
		panic("testdb: release without matching acquire")
	}
	refs[t]--
	if refs[t] == 0 {
		delete(refs, t)
		ready.Broadcast()
	}
}

// DropTables drops every table in DATABASE(). Views and other objects are left
// alone. FOREIGN_KEY_CHECKS is cleared for the session during the drop.
func DropTables(db *sql.DB) error {
	rows, err := db.Query(`SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if strings.Contains(name, "`") {
			return fmt.Errorf("testdb: refuse to drop table %q", name)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	if _, err := db.Exec("SET FOREIGN_KEY_CHECKS=0"); err != nil {
		return err
	}
	defer db.Exec("SET FOREIGN_KEY_CHECKS=1")
	for _, name := range names {
		if _, err := db.Exec("DROP TABLE IF EXISTS `" + name + "`"); err != nil {
			return err
		}
	}
	return nil
}
