package pack

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/projection"
)

// Mapping is one pack.yaml obda: entry after parse, validate, and ODL compile.
type Mapping struct {
	Path string
	Raw  []byte
	Doc  *obda.Document
}

// LoadMappings reads pack.yaml's obda: list (paths as declared, no glob),
// parses each *.obda.yaml, validates, compiles against the pack ontology,
// and rejects cross-file model / link / relation-table collisions.
// An omitted obda: key returns (nil, nil). An explicit empty list is an error.
func LoadMappings(packDir string, onto *ir.Ontology) ([]Mapping, error) {
	m, err := readManifest(packDir)
	if err != nil {
		return nil, err
	}
	if m.OBDA == nil {
		return nil, nil
	}
	if len(m.OBDA) == 0 {
		return nil, fmt.Errorf("pack: %s has empty obda list", packDir)
	}
	if onto == nil {
		return nil, fmt.Errorf("pack: ontology required")
	}

	schema := projection.ProjectStorage(onto)
	out := make([]Mapping, 0, len(m.OBDA))
	seenModels := map[string]string{}
	seenLinks := map[string]string{}
	seenTables := map[string]string{}

	for _, rel := range m.OBDA {
		path := filepath.Join(packDir, rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("pack: read mapping %s: %w", rel, err)
		}
		doc, err := obda.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("pack: parse mapping %s: %w", rel, err)
		}
		if err := obda.Validate(doc); err != nil {
			return nil, fmt.Errorf("pack: validate mapping %s: %w", rel, err)
		}
		if _, err := obda.Compile(schema, doc); err != nil {
			return nil, fmt.Errorf("pack: compile mapping %s: %w", rel, err)
		}
		if err := registerMapping(rel, doc, seenModels, seenLinks, seenTables); err != nil {
			return nil, err
		}
		out = append(out, Mapping{Path: rel, Raw: raw, Doc: doc})
	}
	return out, nil
}

func registerMapping(rel string, doc *obda.Document, models, links, tables map[string]string) error {
	for name, model := range doc.Models {
		if prev, ok := models[name]; ok {
			return fmt.Errorf("pack: mapping %s: duplicate model %q (already in %s)", rel, name, prev)
		}
		models[name] = rel
		if err := registerTable(rel, model.Relation.Name, tables); err != nil {
			return err
		}
	}
	for name, link := range doc.Links {
		if prev, ok := links[name]; ok {
			return fmt.Errorf("pack: mapping %s: duplicate link %q (already in %s)", rel, name, prev)
		}
		links[name] = rel
		if err := registerTable(rel, link.Relation.Name, tables); err != nil {
			return err
		}
	}
	return nil
}

func registerTable(rel, table string, tables map[string]string) error {
	if prev, ok := tables[table]; ok {
		return fmt.Errorf("pack: mapping %s: duplicate relation table %q (already in %s)", rel, table, prev)
	}
	tables[table] = rel
	return nil
}
