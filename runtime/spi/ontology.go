package spi

import "time"

// DateTime is an RFC3339-compatible instant (SPI scalar).
type DateTime = time.Time

// RequestContext is tenant-scoped context passed to every SPI operation.
type RequestContext struct {
	TenantID string
	ActorID  string
	TraceID  string
}

// OntologyObject is a persisted ontology object with tenant isolation.
type OntologyObject map[string]any

// Reserved wire-field names for OntologyObject. These are the single source
// of truth for the _-prefixed storage-wire columns; storage backends and the
// engine use IsSystemField to strip/filter them from user payloads rather than
// re-deriving the set. Untyped string constants (no `string` type) so they
// remain usable as map keys and in concatenation without conversion.
// Mirrors TS OntologyObject's named reserved fields (packages/spi/src/ontology.ts).
const (
	FieldID        = "_id"
	FieldType      = "_type"
	FieldTenantID  = "_tenantId"
	FieldVersion   = "_version"
	FieldCreatedAt = "_createdAt"
	FieldUpdatedAt = "_updatedAt"
	FieldDeletedAt = "_deletedAt"
)

// IsSystemField reports whether k is one of the seven object reserved wire
// fields. Allocation-free switch (mirrors the prior memory.isSystemField
// shape at provider.go). The link-only reserved fields (_fromId, _toId,
// _fromType, _toType, _engineLinkId) are NOT object reserved and return false.
func IsSystemField(k string) bool {
	switch k {
	case FieldID, FieldType, FieldTenantID, FieldVersion,
		FieldCreatedAt, FieldUpdatedAt, FieldDeletedAt:
		return true
	}
	return false
}

// OntologyLink is a typed, directed relationship between two ontology objects.
type OntologyLink map[string]any

// Reserved wire-field names for OntologyLink beyond the seven object fields.
// A link carries every object reserved field plus these five link-specific
// endpoint/id fields. IsLinkSystemField is the superset membership helper.
const (
	LinkFieldFromID       = "_fromId"
	LinkFieldToID         = "_toId"
	LinkFieldFromType     = "_fromType"
	LinkFieldToType       = "_toType"
	LinkFieldEngineLinkID = "_engineLinkId"
)

// IsLinkSystemField reports whether k is reserved on a link: the seven object
// reserved fields (delegated to IsSystemField) plus the five link-specific
// fields. The seven base names are listed exactly once, in IsSystemField.
func IsLinkSystemField(k string) bool {
	return IsSystemField(k) ||
		k == LinkFieldFromID ||
		k == LinkFieldToID ||
		k == LinkFieldFromType ||
		k == LinkFieldToType ||
		k == LinkFieldEngineLinkID
}

// Cardinality mirrors ODL / SPI link cardinalities.
type Cardinality string

const (
	CardinalityOneToOne   Cardinality = "ONE_TO_ONE"
	CardinalityOneToMany  Cardinality = "ONE_TO_MANY"
	CardinalityManyToOne  Cardinality = "MANY_TO_ONE"
	CardinalityManyToMany Cardinality = "MANY_TO_MANY"
)

// IndexType for storage indexes.
type IndexType string

const (
	IndexBTREE    IndexType = "BTREE"
	IndexHASH     IndexType = "HASH"
	IndexGIN      IndexType = "GIN"
	IndexGIST     IndexType = "GIST"
	IndexFULLTEXT IndexType = "FULLTEXT"
)

// OntologySchema is the storage-oriented schema projection.
type OntologySchema struct {
	Version     int                   `json:"version"`
	ObjectTypes []ObjectTypeDefinition `json:"objectTypes"`
	LinkTypes   []LinkTypeDefinition   `json:"linkTypes"`
}

// ObjectTypeDefinition describes a persisted object type.
type ObjectTypeDefinition struct {
	Name       string              `json:"name"`
	Properties []PropertyDefinition `json:"properties"`
	Indexes    []IndexDefinition    `json:"indexes,omitempty"`
}

// LinkTypeDefinition describes a persisted link type.
type LinkTypeDefinition struct {
	Name       string               `json:"name"`
	FromType   string               `json:"fromType"`
	ToType     string               `json:"toType"`
	Cardinality Cardinality         `json:"cardinality"`
	Properties []PropertyDefinition `json:"properties,omitempty"`
}

// PropertyDefinition is a stored property.
type PropertyDefinition struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Required     bool   `json:"required,omitempty"`
	DefaultValue any    `json:"defaultValue,omitempty"`
	Description  string `json:"description,omitempty"`
}

// IndexDefinition describes a storage index.
type IndexDefinition struct {
	Field     string    `json:"field"`
	IndexType IndexType `json:"indexType"`
	Unique    bool      `json:"unique,omitempty"`
}

// MigrationResult is returned from ApplySchema.
type MigrationResult struct {
	Success     bool      `json:"success"`
	FromVersion int       `json:"fromVersion"`
	ToVersion   int       `json:"toVersion"`
	AppliedAt   DateTime  `json:"appliedAt"`
	Details     string    `json:"details,omitempty"`
}

// HealthStatus is returned from HealthCheck.
type HealthStatus struct {
	Healthy   bool           `json:"healthy"`
	Provider  string         `json:"provider"`
	LatencyMs int            `json:"latencyMs"`
	Details   map[string]any `json:"details,omitempty"`
}

// ReplicationCapability describes replication support.
type ReplicationCapability string

const (
	ReplicationNone                   ReplicationCapability = "NONE"
	ReplicationStreaming              ReplicationCapability = "STREAMING_REPLICATION"
	ReplicationPointInTimeRecovery    ReplicationCapability = "POINT_IN_TIME_RECOVERY"
	ReplicationBoth                   ReplicationCapability = "BOTH"
)

// StorageCapabilities describes provider features.
type StorageCapabilities struct {
	SupportsTransactions     bool                  `json:"supportsTransactions"`
	SupportsTemporalQueries  bool                  `json:"supportsTemporalQueries"`
	SupportsFullTextSearch   bool                  `json:"supportsFullTextSearch"`
	SupportsGeoQueries       bool                  `json:"supportsGeoQueries"`
	SupportsGraphTraversal   bool                  `json:"supportsGraphTraversal"`
	SupportsBulkMutations    bool                  `json:"supportsBulkMutations"`
	MaxTraversalDepth        int                   `json:"maxTraversalDepth"`
	ReplicationSupport       ReplicationCapability `json:"replicationSupport"`
}

// FilterExpression is a field or logical predicate.
type FilterExpression struct {
	Field    string             `json:"field,omitempty"`
	Operator string             `json:"operator,omitempty"`
	Value    any                `json:"value,omitempty"`
	And      []FilterExpression `json:"and,omitempty"`
	Or       []FilterExpression `json:"or,omitempty"`
	Not      *FilterExpression  `json:"not,omitempty"`
}

// QueryOptions for object/link queries.
type QueryOptions struct {
	Limit          int
	Offset         int
	OrderBy        []OrderBy
	IncludeDeleted bool
	AsOfVersion    *int
	AsOfTime       *DateTime
}

// OrderBy sorts a query result.
type OrderBy struct {
	Field     string
	Direction string // asc | desc
}

// TraversalPath is a multi-step graph walk.
type TraversalPath struct {
	Steps []TraversalStep
}

// TraversalStep is one hop in a traversal.
type TraversalStep struct {
	LinkType  string
	Direction string // inbound | outbound
	Filter    *FilterExpression
	MaxDepth  int
}

// TraversalOptions for traverse.
type TraversalOptions struct {
	Limit          int
	Offset         int
	IncludeDeleted bool
}

// ObjectPage is a page of objects.
type ObjectPage struct {
	Items       []OntologyObject
	TotalCount  int
	HasNextPage bool
	Cursor      string
}

// LinkPage is a page of links.
type LinkPage struct {
	Items       []OntologyLink
	TotalCount  int
	HasNextPage bool
	Cursor      string
}

// TraversalResult is the result of a graph traversal.
type TraversalResult struct {
	Nodes      []OntologyObject
	Edges      []OntologyLink
	TotalCount int
}

// BulkMutationRequest batches object mutations.
type BulkMutationRequest struct {
	IdempotencyKey string
	Operations     []BulkOperation
}

// BulkOperation is one bulk mutation.
type BulkOperation struct {
	Type       string
	ObjectType string
	ID         string
	Properties map[string]any
	Mode       string // soft | hard for delete
}

// BulkMutationResult summarizes bulk mutate.
type BulkMutationResult struct {
	Accepted int
	Failed   int
	Errors   []BulkMutationError
}

// BulkMutationError describes a failed bulk op.
type BulkMutationError struct {
	OperationIndex int
	Code           string
	Message        string
}

// AggregateQuery for aggregateObjects.
type AggregateQuery struct {
	Fields  []AggregateField
	GroupBy []string
	Filter  *FilterExpression
	OrderBy []OrderBy
	Limit   int
	Offset  int
}

// AggregateField is one aggregation.
type AggregateField struct {
	Field string
	Fn    string
	Alias string
}

// AggregateResult holds aggregate groups.
type AggregateResult struct {
	Groups      []AggregateGroup
	TotalGroups int
}

// AggregateGroup is one group key set.
type AggregateGroup struct {
	Keys   map[string]any
	Values map[string]any
}

// SearchQuery for full-text search.
type SearchQuery struct {
	Query  string
	Fields []string
	Filter *FilterExpression
	Limit  int
	Offset int
}

// SearchResult holds search hits.
type SearchResult struct {
	Hits        []SearchHit
	TotalCount  int
	HasNextPage bool
}

// SearchHit is one search match.
type SearchHit struct {
	Object     OntologyObject
	Score      float64
	Highlights map[string][]string
}
