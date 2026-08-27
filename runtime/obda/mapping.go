package obda

import "fmt"

// Document is a parsed *.obda.yaml mapping. Dialect is an opaque
// identifier here; a provider binds a concrete adapter at Open time.
type Document struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   Metadata          `yaml:"metadata"`
	Schema     SchemaRef         `yaml:"schema"`
	Sources    map[string]Source `yaml:"sources"`
	Models     map[string]Model  `yaml:"models"`
	Links      map[string]Link   `yaml:"links"`
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

// Source is a named SQL data source. Connection holds only a dsnRef;
// plaintext DSNs are rejected at parse.
type Source struct {
	Kind       string     `yaml:"kind"`
	Dialect    string     `yaml:"dialect"`
	Connection Connection `yaml:"connection"`
}

// Connection names a secret reference resolved at provider construction.
type Connection struct {
	DSNRef string `yaml:"dsnRef"`
}

// Model maps one ObjectType onto a physical relation.
type Model struct {
	SourceRef string           `yaml:"sourceRef"`
	Relation  Relation         `yaml:"relation"`
	Access    string           `yaml:"access"`
	Identity  Identity         `yaml:"identity"`
	Tenant    Tenant           `yaml:"tenant"`
	System    System           `yaml:"system"`
	Fields    map[string]Field `yaml:"fields"`
}

// Link maps one LinkType onto a physical relation.
type Link struct {
	SourceRef string           `yaml:"sourceRef"`
	Relation  Relation         `yaml:"relation"`
	Access    string           `yaml:"access"`
	Identity  Identity         `yaml:"identity"`
	From      Endpoint         `yaml:"from"`
	To        Endpoint         `yaml:"to"`
	Tenant    Tenant           `yaml:"tenant"`
	System    System           `yaml:"system"`
	Fields    map[string]Field `yaml:"fields"`
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
