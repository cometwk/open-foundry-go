// Package query is the Query IR compile target for Go Runtime reads.
// Projections construct one tagged op per resolver (or REST request);
// Execute is the only path from those ops to Engine.
package query

import (
	"github.com/openfoundry/runtime/spi"
)

// HopCap is the MANY cap per start object per hop (Phase 6 linkPageLimit).
const HopCap = 1000

// Op is exactly one Query IR operation.
type Op struct {
	Get       *Get
	List      *List
	Aggregate *Aggregate
	Search    *Search
	Expand    *Expand
}

// Get loads one object. Computed nil skips LAZY fields; a non-nil empty
// slice evaluates every LAZY computed field (REST GET). Named entries
// evaluate only those fields.
type Get struct {
	Type     string
	ID       string
	Computed *[]string
}

// List is QueryObjects plus already-converted SPI filter/page options.
type List struct {
	Type    string
	Filter  spi.FilterExpression
	Options *spi.QueryOptions
}

// Aggregate is an Engine AggregateObjects pass-through.
type Aggregate struct {
	Type  string
	Query spi.AggregateQuery
}

// Search is an Engine SearchObjects pass-through.
type Search struct {
	Type  string
	Query spi.SearchQuery
}

// ExpandMode chooses GetLinks vs Traverse for graph navigation.
type ExpandMode int

const (
	// ExpandGetLinks is a 1-hop leaf @link (no nested @link in the child selection).
	ExpandGetLinks ExpandMode = iota
	// ExpandTraverse walks each linear @link path with one Traverse call.
	ExpandTraverse
)

// Expand navigates RoleLinkNav fields from one start object.
// Paths are Ontology IR field names, not SPI link types.
type Expand struct {
	StartType  string
	StartID    string
	Mode       ExpandMode
	Paths      [][]string
	CheckStart bool
}

// Result is the Execute output for one op.
type Result struct {
	Object    spi.OntologyObject
	Page      spi.ObjectPage
	Aggregate spi.AggregateResult
	Search    spi.SearchResult
	Expand    *ExpandResult
}

// ExpandResult is adjacency for GraphQL assembly plus REST terminals.
type ExpandResult struct {
	FirstHop  []spi.OntologyObject
	Terminals []spi.OntologyObject
	Adjacency map[string]map[string][]spi.OntologyObject
}
