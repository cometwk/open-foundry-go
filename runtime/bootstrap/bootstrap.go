package bootstrap

import (
	"database/sql"
	"fmt"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/projection"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/sqliteobda"
	"gopkg.in/yaml.v3"
)

// Config is dialect-neutral assembly input. OpenSQLite binds SQLite;
// a future MySQL entry point can share this shape without changing SPI.
type Config struct {
	DB       *sql.DB
	Ontology *ir.Ontology
	Mappings []pack.Mapping
	TenantID string
}

// OpenSQLite constructs a SQLite OBDA provider from already-loaded pack
// mappings and activates it. The caller opens the database; this function
// does not import a driver or build an Engine.
func OpenSQLite(cfg Config) (spi.StorageProvider, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("bootstrap: db required")
	}
	if cfg.Ontology == nil {
		return nil, fmt.Errorf("bootstrap: ontology required")
	}
	if cfg.TenantID == "" {
		return nil, spi.ErrTenantRequired
	}
	if len(cfg.Mappings) == 0 {
		return nil, fmt.Errorf("bootstrap: no OBDA mapping")
	}

	raw, err := mappingBytes(cfg.Mappings)
	if err != nil {
		return nil, err
	}
	p, err := sqliteobda.Open(cfg.DB, raw, sqliteobda.Options{})
	if err != nil {
		return nil, err
	}
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: cfg.TenantID}, projection.ProjectStorage(cfg.Ontology)); err != nil {
		return nil, err
	}
	return p, nil
}

func mappingBytes(mappings []pack.Mapping) ([]byte, error) {
	if len(mappings) == 1 {
		return mappings[0].Raw, nil
	}
	doc, err := mergeDocuments(mappings)
	if err != nil {
		return nil, err
	}
	raw, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: marshal merged mapping: %w", err)
	}
	return raw, nil
}

func mergeDocuments(mappings []pack.Mapping) (*obda.Document, error) {
	first := mappings[0].Doc
	if first == nil {
		return nil, fmt.Errorf("bootstrap: mapping %s has nil document", mappings[0].Path)
	}
	out := obda.Document{
		APIVersion: first.APIVersion,
		Kind:       first.Kind,
		Metadata:   first.Metadata,
		Schema:     first.Schema,
		Models:     map[string]obda.Model{},
		Links:      map[string]obda.Link{},
	}
	tables := map[string]string{}
	for _, m := range mappings {
		if m.Doc == nil {
			return nil, fmt.Errorf("bootstrap: mapping %s has nil document", m.Path)
		}
		for name, model := range m.Doc.Models {
			if _, ok := out.Models[name]; ok {
				return nil, fmt.Errorf("bootstrap: mapping %s: duplicate model %q", m.Path, name)
			}
			if prev, ok := tables[model.Relation.Name]; ok {
				return nil, fmt.Errorf("bootstrap: mapping %s: duplicate relation table %q (already in %s)", m.Path, model.Relation.Name, prev)
			}
			out.Models[name] = model
			tables[model.Relation.Name] = m.Path
		}
		for name, link := range m.Doc.Links {
			if _, ok := out.Links[name]; ok {
				return nil, fmt.Errorf("bootstrap: mapping %s: duplicate link %q", m.Path, name)
			}
			if prev, ok := tables[link.Relation.Name]; ok {
				return nil, fmt.Errorf("bootstrap: mapping %s: duplicate relation table %q (already in %s)", m.Path, link.Relation.Name, prev)
			}
			out.Links[name] = link
			tables[link.Relation.Name] = m.Path
		}
	}
	return &out, nil
}
