package dialect

import (
	"github.com/openfoundry/runtime/obda/sqlast"
)

// SQLStatement is parameterized SQL. Args are never concatenated into SQL.
type SQLStatement struct {
	SQL  string
	Args []any
}

// Capabilities describes engine features, not per-model mapping availability.
type Capabilities struct {
	Transactions     bool
	Savepoints       bool
	RecursiveCTE     bool
	FullTextSearch   bool
	GeneratedColumns bool
	JSON             bool
	Spatial          bool
	OnlineIndex      bool
}

// Dialect converts a neutral plan into executable SQL for one database family.
type Dialect interface {
	Name() string
	Capabilities() Capabilities
	QuoteIdentifier(sqlast.Identifier) (string, error)
	Placeholder(position int) string
	Render(sqlast.Statement) (SQLStatement, error)
	NormalizeValue(odlType string, v any) (any, error)
}
