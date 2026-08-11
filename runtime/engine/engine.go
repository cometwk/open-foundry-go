// Package engine orchestrates object and link lifecycle over a
// StorageProvider. It owns the binding-intent mirrors of the TypeScript
// ObjectManager / LinkManager: validate against the IR TBox, call SPI
// storage, and (in later phases) emit CloudEvents on state changes.
//
// Phase 2 surfaces the six atomic verbs Create/Get/Update/Delete Object
// and Create/Delete Link. Versioning, soft-delete, history, events,
// query, and transactions are deferred to Phase 3 and beyond.
//
// The Engine depends on the spi.StorageProvider interface only; the
// concrete storage backend is injected at construction. This protects
// the layer boundary between engine and storage (mirrors the TS
// dependency direction in packages/engine — storage-memory is a
// devDependency only).
package engine

import (
	"fmt"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
)

// Engine orchestrates object/link lifecycle verbs over a StorageProvider.
// The TBox is validated once at construction; per-verb runtime checks
// then run against the held *ir.Ontology.
type Engine struct {
	storage  spi.StorageProvider
	ontology *ir.Ontology
}

// New constructs an Engine bound to the given storage provider and TBox.
// ir.Validate is called once up front; construction fails if the TBox
// is semantically invalid — the Engine never runs verbs against an
// IR it cannot trust.
func New(storage spi.StorageProvider, ontology *ir.Ontology) (*Engine, error) {
	if storage == nil {
		return nil, fmt.Errorf("engine: storage provider must be non-nil")
	}
	if ontology == nil {
		return nil, fmt.Errorf("engine: ontology must be non-nil")
	}
	if err := ir.Validate(ontology); err != nil {
		return nil, fmt.Errorf("engine: ontology validation failed: %w", err)
	}
	return &Engine{storage: storage, ontology: ontology}, nil
}