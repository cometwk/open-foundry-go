package obda

import "fmt"

// Document is a parsed *.obda.yaml mapping. YAML describes logical
// bindings only; the database connection is injected at provider.Open.
type Document struct {
	APIVersion string           `yaml:"apiVersion"`
	Kind       string           `yaml:"kind"`
	Metadata   Metadata         `yaml:"metadata"`
	Schema     SchemaRef        `yaml:"schema"`
	Models     map[string]Model `yaml:"models"`
	Links      map[string]Link  `yaml:"links"`
}

// Metadata names a mapping document version.
type Metadata struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
	Version   int    `yaml:"version"`
}

// SchemaRef identifies the ODL namespace this mapping applies to.
type SchemaRef struct {
	Namespace string `yaml:"namespace"`
	Version   int    `yaml:"version"`
}

// Model maps one ObjectType onto a physical relation.
type Model struct {
	Relation Relation         `yaml:"relation"`
	Access   string           `yaml:"access"`
	Identity Identity         `yaml:"identity"`
	Tenant   Tenant           `yaml:"tenant"`
	System   System           `yaml:"system"`
	Fields   map[string]Field `yaml:"fields"`
}

// Link maps one LinkType onto a physical relation.
type Link struct {
	Relation Relation         `yaml:"relation"`
	Access   string           `yaml:"access"`
	Identity Identity         `yaml:"identity"`
	From     Endpoint         `yaml:"from"`
	To       Endpoint         `yaml:"to"`
	Tenant   Tenant           `yaml:"tenant"`
	System   System           `yaml:"system"`
	Fields   map[string]Field `yaml:"fields"`
}

// Relation names a table or view. Catalog must be empty or "main" for SQLite.
type Relation struct {
	Kind    string `yaml:"kind"`
	Catalog string `yaml:"catalog"`
	Name    string `yaml:"name"`
}

// Identity is always a reversible typed encoding of the physical key.
type Identity struct {
	Strategy string   `yaml:"strategy"`
	Columns  []string `yaml:"columns"`
	Insert   string   `yaml:"insert"`
}

// Tenant injects isolation. SQLite v1 allows column and constant only.
type Tenant struct {
	Strategy string `yaml:"strategy"`
	Column   string `yaml:"column"`
	Value    string `yaml:"value"`
}

// System places engine fields on the mapped relation.
// Omit lists system columns that must not be read or written:
// version, createdAt, updatedAt, deletedAt.
type System struct {
	Strategy string   `yaml:"strategy"`
	Omit     []string `yaml:"omit"`
}

// OmitFlags records which native system columns the mapping skips.
type OmitFlags struct {
	Version   bool
	CreatedAt bool
	UpdatedAt bool
	DeletedAt bool
}

func parseOmit(omit []string) (OmitFlags, error) {
	var f OmitFlags
	for _, name := range omit {
		switch name {
		case "version":
			f.Version = true
		case "createdAt":
			f.CreatedAt = true
		case "updatedAt":
			f.UpdatedAt = true
		case "deletedAt":
			f.DeletedAt = true
		default:
			return OmitFlags{}, fmt.Errorf("unknown omit %q", name)
		}
	}
	return f, nil
}

// Field maps a logical property onto a column and optional transform.
type Field struct {
	Column    string    `yaml:"column"`
	Transform Transform `yaml:"transform"`
}

// Transform is a closed reversible (or read-only coalesce) mapping.
type Transform struct {
	Kind string `yaml:"kind"`
	Arg  string `yaml:"arg"`
}

// Endpoint identifies the object type and physical key columns of a link end.
type Endpoint struct {
	Object  string   `yaml:"object"`
	Columns []string `yaml:"columns"`
}
