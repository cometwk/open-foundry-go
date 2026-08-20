package engine

import (
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
)

// QueryObjects passes through to storage. No Engine-side filter rewrite.
func (e *Engine) QueryObjects(ctx spi.RequestContext, typ string, filter spi.FilterExpression, options *spi.QueryOptions) (spi.ObjectPage, error) {
	return e.storage.QueryObjects(ctx, typ, filter, options)
}

// AggregateObjects passes through to storage.
func (e *Engine) AggregateObjects(ctx spi.RequestContext, typ string, query spi.AggregateQuery) (spi.AggregateResult, error) {
	return e.storage.AggregateObjects(ctx, typ, query)
}

// SearchObjects passes through to storage. Blank queries follow memory
// semantics (empty hits), not GraphQL-layer validation errors.
func (e *Engine) SearchObjects(ctx spi.RequestContext, typ string, query spi.SearchQuery) (spi.SearchResult, error) {
	return e.storage.SearchObjects(ctx, typ, query)
}

// GetLinks passes through to storage. direction is the SPI wire value
// ("inbound" / "outbound").
func (e *Engine) GetLinks(ctx spi.RequestContext, objectID, linkType, direction string, options *spi.QueryOptions) (spi.LinkPage, error) {
	return e.storage.GetLinks(ctx, objectID, linkType, direction, options)
}

// Ontology returns the TBox bound at construction. API projections read
// types from here rather than keeping a second IR pointer.
func (e *Engine) Ontology() *ir.Ontology {
	return e.ontology
}

// Capabilities passes through to storage. The API layer reads
// SupportsFullTextSearch from here instead of importing a provider.
func (e *Engine) Capabilities() spi.StorageCapabilities {
	return e.storage.Capabilities()
}
