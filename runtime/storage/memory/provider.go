package memory

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/openfoundry/runtime/spi"
)

// Provider is a Phase 1 memory StorageProvider implementing schema apply/get only.
type Provider struct {
	spi.UnimplementedStorageProvider
	mu       sync.Mutex
	schemas  map[int]spi.OntologySchema
	current  int
}

// New creates an empty memory provider.
func New() *Provider {
	return &Provider{
		schemas: map[int]spi.OntologySchema{},
	}
}

// ApplySchema stores a schema version (idempotent for same version).
func (p *Provider) ApplySchema(_ spi.RequestContext, schema spi.OntologySchema) (spi.MigrationResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	from := p.current
	cloned, err := cloneSchema(schema)
	if err != nil {
		return spi.MigrationResult{}, err
	}
	p.schemas[schema.Version] = cloned
	p.current = schema.Version
	return spi.MigrationResult{
		Success:     true,
		FromVersion: from,
		ToVersion:   schema.Version,
		AppliedAt:   time.Now().UTC(),
	}, nil
}

// GetSchema returns a schema version, or the latest when version is nil.
func (p *Provider) GetSchema(_ spi.RequestContext, version *int) (spi.OntologySchema, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.current
	if version != nil {
		v = *version
	}
	schema, ok := p.schemas[v]
	if !ok {
		return spi.OntologySchema{}, fmt.Errorf("schema version %d not found", v)
	}
	return cloneSchema(schema)
}

// HealthCheck reports a healthy memory provider.
func (p *Provider) HealthCheck() (spi.HealthStatus, error) {
	return spi.HealthStatus{Healthy: true, Provider: "memory", LatencyMs: 0}, nil
}

// Capabilities returns Phase 1 capabilities (schema only).
func (p *Provider) Capabilities() spi.StorageCapabilities {
	return spi.StorageCapabilities{
		SupportsTransactions:   false,
		SupportsTemporalQueries: false,
		SupportsFullTextSearch: false,
		SupportsGeoQueries:     false,
		SupportsGraphTraversal: false,
		SupportsBulkMutations:  false,
		MaxTraversalDepth:      0,
		ReplicationSupport:     spi.ReplicationNone,
	}
}

func cloneSchema(s spi.OntologySchema) (spi.OntologySchema, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return spi.OntologySchema{}, err
	}
	var out spi.OntologySchema
	if err := json.Unmarshal(b, &out); err != nil {
		return spi.OntologySchema{}, err
	}
	return out, nil
}
