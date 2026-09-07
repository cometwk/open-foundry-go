package bootstrap

import (
	"fmt"

	"github.com/openfoundry/runtime/obda"
)

// PrintMappedDDL loads the configured pack and returns dialect DDL.
// It does not open a database or construct a provider.
func PrintMappedDDL(c *Conf, dialect string) ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("bootstrap: conf required")
	}
	name := dialect
	if name == "" {
		name = c.DBDriver
	}
	return PrintPackDDL(packDir(c), name)
}

// PrintPackDDL compiles the pack at dir and returns dialect DDL. No database session.
func PrintPackDDL(dir, dialect string) ([]string, error) {
	name, err := SQLName(dialect)
	if err != nil {
		return nil, err
	}
	_, mappings, schema, err := LoadPack(dir)
	if err != nil {
		return nil, err
	}
	if err := requireOneMapping(mappings); err != nil {
		return nil, err
	}
	compiled, err := obda.Compile(schema, mappings[0].Doc)
	if err != nil {
		return nil, err
	}
	return MappedStatements(compiled, name)
}
