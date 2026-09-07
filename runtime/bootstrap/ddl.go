package bootstrap

import (
	"database/sql"
	"fmt"

	"github.com/openfoundry/runtime/obda"
)

// PrintMappedDDL loads the configured pack and returns dialect DDL.
// It does not open a database or construct a provider.
func PrintMappedDDL(c *Conf, dialect string) ([]string, error) {
	return MappedDDL(c, dialect, false)
}

// MappedDDL loads the configured pack and returns dialect DDL.
// When force is true, DROP TABLE IF EXISTS statements precede CREATE.
func MappedDDL(c *Conf, dialect string, force bool) ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("bootstrap: conf required")
	}
	name := dialect
	if name == "" {
		name = c.DBDriver
	}
	return PackDDL(packDir(c), name, force)
}

// PrintPackDDL compiles the pack at dir and returns dialect DDL. No database session.
func PrintPackDDL(dir, dialect string) ([]string, error) {
	return PackDDL(dir, dialect, false)
}

// PackDDL compiles the pack at dir and returns dialect DDL.
// When force is true, DROP TABLE IF EXISTS statements precede CREATE.
func PackDDL(dir, dialect string, force bool) ([]string, error) {
	name, err := SQLName(dialect)
	if err != nil {
		return nil, err
	}
	_, mappings, schema, err := LoadPack(dir)
	if err != nil {
		return nil, err
	}
	if err := requireOneMapping(mappings); err != nil {
		return nil, err
	}
	compiled, err := obda.Compile(schema, mappings[0].Doc)
	if err != nil {
		return nil, err
	}
	stmts, err := MappedStatements(compiled, name)
	if err != nil {
		return nil, err
	}
	if !force {
		return stmts, nil
	}
	drops, err := DropStatements(compiled, name)
	if err != nil {
		return nil, err
	}
	return append(drops, stmts...), nil
}

// ExecStatements runs stmts against the configured database.
// dialect, if set, must match DB_DRIVER; empty uses the driver.
func ExecStatements(c *Conf, dialect string, stmts []string) error {
	if c == nil {
		return fmt.Errorf("bootstrap: conf required")
	}
	driver, err := SQLName(c.DBDriver)
	if err != nil {
		return err
	}
	if dialect != "" {
		name, err := SQLName(dialect)
		if err != nil {
			return err
		}
		if name != driver {
			return fmt.Errorf("bootstrap: cannot execute dialect %q against DB_DRIVER %q", name, driver)
		}
	}
	if c.DBURL == "" {
		return fmt.Errorf("bootstrap: DB_URL required")
	}
	db, err := sql.Open(driver, c.DBURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return err
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}
