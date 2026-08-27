package spi

import (
	"fmt"
	"time"
)

// UnimplementedStorageProvider embeds into concrete providers and returns
// ErrUnimplemented for every method unless overridden.
type UnimplementedStorageProvider struct{}

func (UnimplementedStorageProvider) mustEmbedUnimplementedStorageProvider() {}

func unimplemented(method string) error {
	return fmt.Errorf("%w: %s", ErrUnimplemented, method)
}

func (UnimplementedStorageProvider) ApplySchema(RequestContext, OntologySchema) (MigrationResult, error) {
	return MigrationResult{}, unimplemented("ApplySchema")
}

func (UnimplementedStorageProvider) GetSchema(RequestContext, *int) (OntologySchema, error) {
	return OntologySchema{}, unimplemented("GetSchema")
}

func (UnimplementedStorageProvider) CreateObject(RequestContext, string, map[string]any) (OntologyObject, error) {
	return nil, unimplemented("CreateObject")
}

func (UnimplementedStorageProvider) GetObject(RequestContext, string, string) (OntologyObject, error) {
	return nil, unimplemented("GetObject")
}

func (UnimplementedStorageProvider) UpdateObject(RequestContext, string, string, map[string]any, *int) (OntologyObject, error) {
	return nil, unimplemented("UpdateObject")
}

func (UnimplementedStorageProvider) DeleteObject(RequestContext, string, string, string) error {
	return unimplemented("DeleteObject")
}

func (UnimplementedStorageProvider) QueryObjects(RequestContext, string, FilterExpression, *QueryOptions) (ObjectPage, error) {
	return ObjectPage{}, unimplemented("QueryObjects")
}

func (UnimplementedStorageProvider) AggregateObjects(RequestContext, string, AggregateQuery) (AggregateResult, error) {
	return AggregateResult{}, unimplemented("AggregateObjects")
}

func (UnimplementedStorageProvider) SearchObjects(RequestContext, string, SearchQuery) (SearchResult, error) {
	return SearchResult{}, unimplemented("SearchObjects")
}

func (UnimplementedStorageProvider) BulkMutate(RequestContext, BulkMutationRequest) (BulkMutationResult, error) {
	return BulkMutationResult{}, unimplemented("BulkMutate")
}

func (UnimplementedStorageProvider) CreateLink(RequestContext, string, string, string, map[string]any) (OntologyLink, error) {
	return nil, unimplemented("CreateLink")
}

func (UnimplementedStorageProvider) GetLink(RequestContext, string, string) (OntologyLink, error) {
	return nil, unimplemented("GetLink")
}

func (UnimplementedStorageProvider) UpdateLink(RequestContext, string, string, map[string]any, *int) (OntologyLink, error) {
	return nil, unimplemented("UpdateLink")
}

func (UnimplementedStorageProvider) DeleteLink(RequestContext, string, string) error {
	return unimplemented("DeleteLink")
}

func (UnimplementedStorageProvider) GetLinks(RequestContext, string, string, string, *QueryOptions) (LinkPage, error) {
	return LinkPage{}, unimplemented("GetLinks")
}

func (UnimplementedStorageProvider) Traverse(RequestContext, string, TraversalPath, *TraversalOptions) (TraversalResult, error) {
	return TraversalResult{}, unimplemented("Traverse")
}

func (UnimplementedStorageProvider) BeginTransaction(RequestContext) (Transaction, error) {
	return nil, unimplemented("BeginTransaction")
}

func (UnimplementedStorageProvider) GetObjectAtVersion(RequestContext, string, string, int) (OntologyObject, error) {
	return nil, unimplemented("GetObjectAtVersion")
}

func (UnimplementedStorageProvider) GetObjectAtTime(RequestContext, string, string, time.Time) (OntologyObject, error) {
	return nil, unimplemented("GetObjectAtTime")
}

func (UnimplementedStorageProvider) EnsureIndex(RequestContext, string, IndexDefinition) error {
	return unimplemented("EnsureIndex")
}

func (UnimplementedStorageProvider) DropIndex(RequestContext, string, string) error {
	return unimplemented("DropIndex")
}

func (UnimplementedStorageProvider) ListIndexes(RequestContext, string) ([]IndexDefinition, error) {
	return nil, unimplemented("ListIndexes")
}

func (UnimplementedStorageProvider) HealthCheck() (HealthStatus, error) {
	return HealthStatus{}, unimplemented("HealthCheck")
}

func (UnimplementedStorageProvider) Capabilities() StorageCapabilities {
	return StorageCapabilities{}
}
