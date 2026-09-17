package obda

import (
	"fmt"

	"github.com/openfoundry/runtime/spi"
)

// MergeDocuments unions models and links from one pack's mapping files
// into a single document. Headers must agree; duplicate model, link, or
// relation-table names are rejected. A single document is returned as-is.
func MergeDocuments(docs []*Document) (*Document, error) {
	if len(docs) == 0 {
		return nil, fmt.Errorf("%w: no mapping documents", spi.ErrInvalidMapping)
	}
	for i, d := range docs {
		if d == nil {
			return nil, fmt.Errorf("%w: mapping document %d is nil", spi.ErrInvalidMapping, i)
		}
	}
	if len(docs) == 1 {
		return docs[0], nil
	}
	first := docs[0]
	out := &Document{
		APIVersion: first.APIVersion,
		Kind:       first.Kind,
		Metadata:   first.Metadata,
		Schema:     first.Schema,
		Models:     map[string]Model{},
		Links:      map[string]Link{},
	}
	tables := map[string]string{}
	for _, d := range docs {
		if err := mergeCompatible(first, d); err != nil {
			return nil, err
		}
		if err := mergeModels(out, d, tables); err != nil {
			return nil, err
		}
		if err := mergeLinks(out, d, tables); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func mergeCompatible(first, d *Document) error {
	if d.APIVersion != first.APIVersion {
		return fmt.Errorf("%w: apiVersion %q does not match %q", spi.ErrInvalidMapping, d.APIVersion, first.APIVersion)
	}
	if d.Kind != first.Kind {
		return fmt.Errorf("%w: kind %q does not match %q", spi.ErrInvalidMapping, d.Kind, first.Kind)
	}
	if d.Schema.Namespace != first.Schema.Namespace {
		return fmt.Errorf("%w: schema namespace %q does not match %q", spi.ErrInvalidMapping, d.Schema.Namespace, first.Schema.Namespace)
	}
	if d.Schema.Version != first.Schema.Version {
		return fmt.Errorf("%w: schema version %d does not match %d", spi.ErrInvalidMapping, d.Schema.Version, first.Schema.Version)
	}
	return nil
}

func mergeModels(out, d *Document, tables map[string]string) error {
	for name, model := range d.Models {
		if _, ok := out.Models[name]; ok {
			return fmt.Errorf("%w: duplicate model %q", spi.ErrInvalidMapping, name)
		}
		if err := mergeTable(model.Relation.Name, tables); err != nil {
			return err
		}
		out.Models[name] = model
	}
	return nil
}

func mergeLinks(out, d *Document, tables map[string]string) error {
	for name, link := range d.Links {
		if _, ok := out.Links[name]; ok {
			return fmt.Errorf("%w: duplicate link %q", spi.ErrInvalidMapping, name)
		}
		if !link.Inline() {
			if err := mergeTable(link.Relation.Name, tables); err != nil {
				return err
			}
		}
		out.Links[name] = link
	}
	return nil
}

func mergeTable(table string, tables map[string]string) error {
	if table == "" {
		return nil
	}
	if _, ok := tables[table]; ok {
		return fmt.Errorf("%w: duplicate relation table %q", spi.ErrInvalidMapping, table)
	}
	tables[table] = table
	return nil
}
