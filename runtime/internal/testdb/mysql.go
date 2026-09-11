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
)

// LoadEnv loads repo-root .env so TEST_DB_URL is visible under `go test`.
func LoadEnv() {
	loadEnvOnce.Do(func() {
		for _, path := range []string{
			".env",
			"../.env",
			filepath.Join("..", "..", ".env"),
		} {
			if err := godotenv.Load(path); err == nil {
				return
			}
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
}

// Open connects to the database named in TEST_DB_URL and drops existing tables
// so InitMappedSchema can recreate a fresh schema. Cleanup drops tables again.
// It does not CREATE DATABASE. Tests skip when TEST_DB_URL is unset. Access is
// serialized so packages that share the same DSN do not clobber each other.
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

	mu.Lock()
	ok := false
	defer func() {
		if !ok {
			mu.Unlock()
		}
	}()

	var db *sql.DB
	if opts.logSQL {
		db, err = sqlopen.Open("mysql", dsn)
	} else {
		db, err = sql.Open("mysql", dsn)
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
		mu.Unlock()
	})
	ok = true
	return db
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
