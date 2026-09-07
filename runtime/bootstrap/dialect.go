package bootstrap

import (
	"fmt"

	"github.com/openfoundry/runtime/obda"
	mysqldialect "github.com/openfoundry/runtime/obda/dialect/mysql"
	sqlitedialect "github.com/openfoundry/runtime/obda/dialect/sqlite"
)

const (
	SQLMySQL  = "mysql"
	SQLSQLite = "sqlite"
)

// SQLName accepts only mysql and sqlite. Unknown names fail; there is no fallback.
func SQLName(name string) (string, error) {
	switch name {
	case SQLMySQL, SQLSQLite:
		return name, nil
	default:
		return "", fmt.Errorf("bootstrap: unsupported dialect %q", name)
	}
}

// MappedStatements renders compiled mapping DDL for one resolved dialect name.
func MappedStatements(compiled *obda.Compiled, dialect string) ([]string, error) {
	name, err := SQLName(dialect)
	if err != nil {
		return nil, err
	}
	switch name {
	case SQLMySQL:
		return mysqldialect.MappedTableStatements(compiled)
	default:
		return sqlitedialect.MappedTableStatements(compiled)
	}
}

// DropStatements renders DROP TABLE IF EXISTS for mapped tables, reverse of CREATE order.
func DropStatements(compiled *obda.Compiled, dialect string) ([]string, error) {
	name, err := SQLName(dialect)
	if err != nil {
		return nil, err
	}
	switch name {
	case SQLMySQL:
		return mysqldialect.DropTableStatements(compiled)
	default:
		return sqlitedialect.DropTableStatements(compiled)
	}
}
