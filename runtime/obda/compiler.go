package obda

import (
	"fmt"
	"sort"

	"github.com/openfoundry/runtime/spi"
)

// Compiled is an immutable mapping bound to one ontology schema version.
type Compiled struct {
	Models map[string]*CompiledModel
	Links  map[string]*CompiledLink
}

// CompiledModel is the executable shape of one ObjectType binding.
type CompiledModel struct {
	Name             string
	Table            string
	Access           string
	IdentityStrategy string
	IdentityInsert   string
	IdentityColumns  []string
	TenantStrategy   string
	TenantColumn     string
	TenantValue      string
	SystemStrategy   string
	Omit             OmitFlags
	Fields           []CompiledField
	FieldByLogical   map[string]CompiledField
	FieldByColumn    map[string]CompiledField
	PropertyTypes    map[string]string
	SearchIndex      string
	SearchableFields []string
}

// CompiledField maps one logical property onto a physical column.
type CompiledField struct {
	Logical string
	Column  string
}

// CompiledLink is the executable shape of one LinkType binding.
type CompiledLink struct {
	Name             string
	Table            string
	Access           string
	IdentityStrategy string
	IdentityInsert   string
	IdentityColumns  []string
	FromObject       string
	FromColumns      []string
	ToObject         string
	ToColumns        []string
	TenantStrategy   string
	TenantColumn     string
	TenantValue      string
	SystemStrategy   string
	Cardinality      spi.Cardinality
	Omit             OmitFlags
	Fields           []CompiledField
	FieldByLogical   map[string]CompiledField
	FieldByColumn    map[string]CompiledField
	PropertyTypes    map[string]string
}

// Writable reports whether mutations are allowed.
func (l *CompiledLink) Writable() bool { return l.Access == "readWrite" }

// Writable reports whether mutations are allowed.
func (m *CompiledModel) Writable() bool { return m.Access == "readWrite" }

// Binding returns the planner view of this model.
func (m *CompiledModel) Binding() ObjectBinding {
	seen := map[string]struct{}{}
	cols := make([]string, 0, 8)
	add := func(c string) {
		if c == "" {
			return
		}
		if _, ok := seen[c]; ok {
			return
		}
		seen[c] = struct{}{}
		cols = append(cols, c)
	}
	for _, c := range m.IdentityColumns {
		add(c)
	}
	if m.TenantColumn != "" {
		add(m.TenantColumn)
	}
	for _, f := range m.Fields {
		add(f.Column)
	}
	if !m.Omit.Version {
		add("version")
	}
	if !m.Omit.CreatedAt {
		add("created_at")
	}
	if !m.Omit.UpdatedAt {
		add("updated_at")
	}
	if !m.Omit.DeletedAt {
		add("deleted_at")
	}
	return ObjectBinding{
		Table:            m.Table,
		TenantColumn:     m.TenantColumn,
		IdentityColumns:  append([]string(nil), m.IdentityColumns...),
		SelectColumns:    cols,
		Writable:         m.Writable(),
		SearchIndex:      m.SearchIndex,
		SearchableFields: append([]string(nil), m.SearchableFields...),
	}
}

// Binding returns the planner view of this link relation.
func (l *CompiledLink) Binding() ObjectBinding {
	seen := map[string]struct{}{}
	cols := make([]string, 0, 8)
	add := func(c string) {
		if c == "" {
			return
		}
		if _, ok := seen[c]; ok {
			return
		}
		seen[c] = struct{}{}
		cols = append(cols, c)
	}
	for _, c := range l.IdentityColumns {
		add(c)
	}
	if l.TenantColumn != "" {
		add(l.TenantColumn)
	}
	for _, c := range l.FromColumns {
		add(c)
	}
	for _, c := range l.ToColumns {
		add(c)
	}
	for _, f := range l.Fields {
		add(f.Column)
	}
	if !l.Omit.Version {
		add("version")
	}
	if !l.Omit.CreatedAt {
		add("created_at")
	}
	if !l.Omit.UpdatedAt {
		add("updated_at")
	}
	if !l.Omit.DeletedAt {
		add("deleted_at")
	}
	return ObjectBinding{
		Table:           l.Table,
		TenantColumn:    l.TenantColumn,
		IdentityColumns: append([]string(nil), l.IdentityColumns...),
		SelectColumns:   cols,
		Writable:        l.Writable(),
	}
}

// Compile checks a mapping document against an ontology schema and
// returns an immutable compiled form.
func Compile(schema spi.OntologySchema, doc *Document) (*Compiled, error) {
	if err := Validate(doc); err != nil {
		return nil, err
	}
	models := map[string]spi.ObjectTypeDefinition{}
	for _, o := range schema.ObjectTypes {
		models[o.Name] = o
	}
	links := map[string]spi.LinkTypeDefinition{}
	for _, l := range schema.LinkTypes {
		links[l.Name] = l
	}
	out := &Compiled{
		Models: map[string]*CompiledModel{},
		Links:  map[string]*CompiledLink{},
	}
	for name, m := range doc.Models {
		def, ok := models[name]
		if !ok && len(schema.ObjectTypes) > 0 {
			return nil, fmt.Errorf("%w: model %q not in schema", spi.ErrInvalidMapping, name)
		}
		cm, err := compileModel(name, m, def)
		if err != nil {
			return nil, err
		}
		out.Models[name] = cm
	}
	for name, l := range doc.Links {
		def, ok := links[name]
		if !ok && len(schema.LinkTypes) > 0 {
			return nil, fmt.Errorf("%w: link %q not in schema", spi.ErrInvalidMapping, name)
		}
		cl := &CompiledLink{
			Name:             name,
			Table:            l.Relation.Name,
			Access:           l.Access,
			IdentityStrategy: l.Identity.Strategy,
			IdentityInsert:   l.Identity.Insert,
			IdentityColumns:  append([]string(nil), l.Identity.Columns...),
			FromObject:       l.From.Object,
			FromColumns:      append([]string(nil), l.From.Columns...),
			ToObject:         l.To.Object,
			ToColumns:        append([]string(nil), l.To.Columns...),
			TenantStrategy:   l.Tenant.Strategy,
			TenantColumn:     l.Tenant.Column,
			TenantValue:      l.Tenant.Value,
			SystemStrategy:   l.System.Strategy,
			Cardinality:      def.Cardinality,
			FieldByLogical:   map[string]CompiledField{},
			FieldByColumn:    map[string]CompiledField{},
			PropertyTypes:    map[string]string{},
		}
		omit, err := parseOmit(l.System.Omit)
		if err != nil {
			return nil, fmt.Errorf("%w: link %q: %v", spi.ErrInvalidMapping, name, err)
		}
		cl.Omit = omit
		for _, p := range def.Properties {
			cl.PropertyTypes[p.Name] = p.Type
		}
		for logical, f := range l.Fields {
			cf := CompiledField{Logical: logical, Column: f.Column}
			cl.Fields = append(cl.Fields, cf)
			cl.FieldByLogical[logical] = cf
			cl.FieldByColumn[f.Column] = cf
		}
		sort.Slice(cl.Fields, func(i, j int) bool { return cl.Fields[i].Logical < cl.Fields[j].Logical })
		if l.Identity.Insert != "generated" {
			for _, col := range l.Identity.Columns {
				if _, ok := cl.FieldByColumn[col]; !ok {
					return nil, fmt.Errorf("%w: link %q identity column %q is not a payload field", spi.ErrInvalidMapping, name, col)
				}
			}
		}
		out.Links[name] = cl
	}
	return out, nil
}

func compileModel(name string, m Model, def spi.ObjectTypeDefinition) (*CompiledModel, error) {
	types := map[string]string{}
	for _, p := range def.Properties {
		types[p.Name] = p.Type
	}
	cm := &CompiledModel{
		Name:             name,
		Table:            m.Relation.Name,
		Access:           m.Access,
		IdentityStrategy: m.Identity.Strategy,
		IdentityInsert:   m.Identity.Insert,
		IdentityColumns:  append([]string(nil), m.Identity.Columns...),
		TenantStrategy:   m.Tenant.Strategy,
		TenantColumn:     m.Tenant.Column,
		TenantValue:      m.Tenant.Value,
		SystemStrategy:   m.System.Strategy,
		FieldByLogical:   map[string]CompiledField{},
		FieldByColumn:    map[string]CompiledField{},
		PropertyTypes:    types,
	}
	omit, err := parseOmit(m.System.Omit)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %v", spi.ErrInvalidMapping, name, err)
	}
	cm.Omit = omit
	for logical, f := range m.Fields {
		if f.Column == "" {
			return nil, fmt.Errorf("%w: %q field %q missing column", spi.ErrInvalidMapping, name, logical)
		}
		if m.Tenant.Strategy == "column" && f.Column == m.Tenant.Column {
			return nil, fmt.Errorf("%w: %q field %q maps tenant column", spi.ErrInvalidMapping, name, logical)
		}
		cf := CompiledField{Logical: logical, Column: f.Column}
		cm.Fields = append(cm.Fields, cf)
		cm.FieldByLogical[logical] = cf
		cm.FieldByColumn[f.Column] = cf
	}
	sort.Slice(cm.Fields, func(i, j int) bool { return cm.Fields[i].Logical < cm.Fields[j].Logical })
	if m.Identity.Insert != "generated" {
		for _, col := range m.Identity.Columns {
			if _, ok := cm.FieldByColumn[col]; !ok {
				return nil, fmt.Errorf("%w: %q identity column %q is not a payload field", spi.ErrInvalidMapping, name, col)
			}
		}
	}
	return cm, nil
}
