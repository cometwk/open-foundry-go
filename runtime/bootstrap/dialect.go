package bootstrap

import (
	"fmt"

	"github.com/openfoundry/runtime/obda"
	mysqldialect "github.com/openfoundry/runtime/obda/dialect/mysql"
)

const (
	SQLMySQL = "mysql"
)

// SQLName accepts only mysql — the sole SQL dialect. Unknown names fail;
// there is no fallback. The memory backend is not a SQL dialect and never
// reaches this path.
func SQLName(name string) (string, error) {
	switch name {
	case SQLMySQL:
		return name, nil
	default:
		return "", fmt.Errorf("bootstrap: unsupported dialect %q", name)
	}
}

// MappedStatements renders compiled mapping DDL for one resolved dialect name.
func MappedStatements(compiled *obda.Compiled, dialect string) ([]string, error) {
	if _, err := SQLName(dialect); err != nil {
		return nil, err
	}
	return mysqldialect.MappedTableStatements(compiled)
}

// DropStatements renders DROP TABLE IF EXISTS for mapped tables, reverse of CREATE order.
func DropStatements(compiled *obda.Compiled, dialect string) ([]string, error) {
	if _, err := SQLName(dialect); err != nil {
		return nil, err
	}
	return mysqldialect.DropTableStatements(compiled)
}
