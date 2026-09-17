package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/projection"
	"github.com/openfoundry/runtime/spi"
)

func packDir(c *Conf) string {
	return filepath.Join(c.BaseDir, "domain-packs", c.DomainPacks)
}

// LoadPack loads ODL and OBDA mappings from a pack directory.
func LoadPack(dir string) (*ir.Ontology, []pack.Mapping, spi.OntologySchema, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return nil, nil, spi.OntologySchema{}, err
	}
	if !st.IsDir() {
		return nil, nil, spi.OntologySchema{}, fmt.Errorf("domain-packs directory not found: %s", dir)
	}
	onto, err := pack.LoadDir(dir)
	if err != nil {
		return nil, nil, spi.OntologySchema{}, err
	}
	mappings, err := pack.LoadMappings(dir, onto)
	if err != nil {
		return nil, nil, spi.OntologySchema{}, err
	}
	return onto, mappings, projection.ProjectStorage(onto), nil
}

func requireOneMapping(mappings []pack.Mapping) error {
	if len(mappings) != 1 {
		return fmt.Errorf("mappings=%d", len(mappings))
	}
	return nil
}

// mappingBytes returns the raw OBDA mapping bytes for an already-validated
// single-mapping pack. requireOneMapping (pack.go) is the gate.
func mappingBytes(mappings []pack.Mapping) ([]byte, error) {
	if len(mappings) != 1 {
		return nil, fmt.Errorf("bootstrap: want exactly one OBDA mapping, got %d", len(mappings))
	}
	return mappings[0].Raw, nil
}
