// Package uuidv7 mints RFC 9562 UUIDv7 identifiers for the Runtime
// layer. It is a thin façade over `github.com/google/uuid` so that
// callers (the Engine for link ids, the memory provider for object
// ids) import a single in-repo package with a consistent `New() string`
// shape; the exact UUID source can be swapped or augmented later
// without touching call-sites.
//
// UUIDv7 encodes a 48-bit Unix-millisecond timestamp in its most
// significant bits, producing ids that are both temporally sortable
// and unique across processes without coordination.
package uuidv7

import (
	"fmt"

	"github.com/google/uuid"
)

// New returns a freshly-minted UUIDv7 string in the canonical
// lower-case hyphenated form. Failures from the underlying crypto
// source are treated as fatal environment defects: callers (Engine
// generating link ids, memory generating object ids) cannot
// meaningfully fall back from missing system entropy.
func New() string {
	u, err := uuid.NewV7()
	if err != nil {
		panic(fmt.Sprintf("uuidv7: crypto source unavailable: %v", err))
	}
	return u.String()
}