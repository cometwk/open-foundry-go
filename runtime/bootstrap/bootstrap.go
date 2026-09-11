package bootstrap

import (
	"fmt"

	"github.com/openfoundry/runtime/pack"
)

// mappingBytes returns the raw OBDA mapping bytes for an already-validated
// single-mapping pack. requireOneMapping (pack.go) is the gate.
func mappingBytes(mappings []pack.Mapping) ([]byte, error) {
	if len(mappings) != 1 {
		return nil, fmt.Errorf("bootstrap: want exactly one OBDA mapping, got %d", len(mappings))
	}
	return mappings[0].Raw, nil
}
