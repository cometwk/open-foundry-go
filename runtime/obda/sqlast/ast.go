package sqlast

// Identifier is a compiled physical name. Runtime values never live here.
type Identifier struct {
	Name      string
	Qualifier string
}

// Param is a bound argument slot. Position is 1-based.
type Param struct {
	Position int
}

// Expr is an identifier, parameter, or literal placeholder node.
type Expr interface {
	isExpr()
}

func (Identifier) isExpr() {}
func (Param) isExpr()      {}

// Statement is a closed SQL plan. Dialects render it; Core does not emit SQL text.
type Statement interface {
	isStmt()
}

// Select is a read plan.
type Select struct {
	From    Identifier
	As      string
	Columns []Expr
	Joins   []Join
	Where   *Predicate
	Order   []Order
	Limit   *LimitOffset
	Search  *FullTextMatch
	CTE     []CommonTable
}

func (Select) isStmt() {}

// Insert writes one row.
type Insert struct {
	Table     Identifier
	Columns   []Identifier
	Values    []Expr
	Returning []Identifier
}

func (Insert) isStmt() {}

// Update assigns columns under a predicate.
type Update struct {
	Table Identifier
	Set   []Assignment
	Where *Predicate
}

func (Update) isStmt() {}

// Delete removes rows matching a predicate.
type Delete struct {
	Table Identifier
	Where *Predicate
}

func (Delete) isStmt() {}

// Join is an inner/left join against a compiled identifier.
type Join struct {
	Kind  string
	Table Identifier
	As    string
	On    *Predicate
}

// Predicate is a closed filter tree. Values are parameters, never raw SQL.
type Predicate struct {
	Op       string
	Field    *Identifier
	Other    *Identifier
	Value    Expr
	Children []*Predicate
}

// Order sorts by a compiled field.
type Order struct {
	Field Identifier
	Desc  bool
}

// LimitOffset is offset pagination.
type LimitOffset struct {
	Limit  Expr
	Offset Expr
}

// FullTextMatch is a dialect-neutral search against a logical search source.
type FullTextMatch struct {
	Source Identifier
	Query  Expr
}

// CommonTable is a named subquery used by traversal.
type CommonTable struct {
	Name Identifier
	Body *Select
}

// Assignment is SET col = expr.
type Assignment struct {
	Column Identifier
	Value  Expr
}

// AggregateSelect groups and reduces.
type AggregateSelect struct {
	From    Identifier
	GroupBy []Identifier
	Aggs    []Aggregate
	Where   *Predicate
	Limit   *LimitOffset
}

func (AggregateSelect) isStmt() {}

// Aggregate is one reduction (count/sum/avg/min/max).
type Aggregate struct {
	Fn    string
	Field *Identifier
	Alias Identifier
}

// CreateIndex / DropIndex are DDL plans for provider-owned indexes.
type CreateIndex struct {
	Name    Identifier
	Table   Identifier
	Columns []Identifier
	Unique  bool
	Kind    string
}

func (CreateIndex) isStmt() {}

type DropIndex struct {
	Name Identifier
}

func (DropIndex) isStmt() {}
