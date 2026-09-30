package util

import (
	"fmt"

	"github.com/google/uuid"
)

// New returns a freshly-minted UUIDv7 string in the canonical
// lower-case hyphenated form. Failures from the underlying crypto
// source are treated as fatal environment defects: callers (Engine
// generating link ids, memory generating object ids) cannot
// meaningfully fall back from missing system entropy.
func UUIDv7() string {
	u, err := uuid.NewV7()
	if err != nil {
		panic(fmt.Sprintf("uuidv7: crypto source unavailable: %v", err))
	}
	return u.String()
}
