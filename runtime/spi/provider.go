package spi

// StorageProvider is the primary SPI contract all storage backends implement.
type StorageProvider interface {
	ApplySchema(ctx RequestContext, schema OntologySchema) (MigrationResult, error)
	GetSchema(ctx RequestContext, version *int) (OntologySchema, error)

	CreateObject(ctx RequestContext, typ string, properties map[string]any) (OntologyObject, error)
	GetObject(ctx RequestContext, typ, id string) (OntologyObject, error)
	UpdateObject(ctx RequestContext, typ, id string, properties map[string]any, expectedVersion *int) (OntologyObject, error)
	DeleteObject(ctx RequestContext, typ, id, mode string) error
	QueryObjects(ctx RequestContext, typ string, filter FilterExpression, options *QueryOptions) (ObjectPage, error)
	AggregateObjects(ctx RequestContext, typ string, query AggregateQuery) (AggregateResult, error)
	SearchObjects(ctx RequestContext, typ string, query SearchQuery) (SearchResult, error)
	BulkMutate(ctx RequestContext, request BulkMutationRequest) (BulkMutationResult, error)

	CreateLink(ctx RequestContext, typ, fromID, toID string, properties map[string]any) (OntologyLink, error)
	GetLink(ctx RequestContext, typ, linkID string) (OntologyLink, error)
	UpdateLink(ctx RequestContext, typ, linkID string, properties map[string]any, expectedVersion *int) (OntologyLink, error)
	DeleteLink(ctx RequestContext, typ, linkID string) error
	GetLinks(ctx RequestContext, objectID, linkType, direction string, options *QueryOptions) (LinkPage, error)
	Traverse(ctx RequestContext, startID string, path TraversalPath, options *TraversalOptions) (TraversalResult, error)

	BeginTransaction(ctx RequestContext) (Transaction, error)

	GetObjectAtVersion(ctx RequestContext, typ, id string, version int) (OntologyObject, error)
	GetObjectAtTime(ctx RequestContext, typ, id string, timestamp DateTime) (OntologyObject, error)

	EnsureIndex(ctx RequestContext, typ string, index IndexDefinition) error
	DropIndex(ctx RequestContext, typ, field string) error
	ListIndexes(ctx RequestContext, typ string) ([]IndexDefinition, error)

	HealthCheck() (HealthStatus, error)
	Capabilities() StorageCapabilities

	mustEmbedUnimplementedStorageProvider()
}
