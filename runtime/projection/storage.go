package projection

import (
	"sort"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
)

// ProjectStorage projects Ontology IR to a storage OntologySchema.
// Mirrors packages/api/src/schema-loader.ts convertObjectType / convertLinkType.
func ProjectStorage(o *ir.Ontology) spi.OntologySchema {
	schema := spi.OntologySchema{
		Version:     1,
		ObjectTypes: make([]spi.ObjectTypeDefinition, 0, len(o.Objects)),
		LinkTypes:   make([]spi.LinkTypeDefinition, 0, len(o.Links)),
	}

	objs := append([]ir.ObjectType{}, o.Objects...)
	sort.Slice(objs, func(i, j int) bool { return objs[i].Name < objs[j].Name })
	for _, obj := range objs {
		schema.ObjectTypes = append(schema.ObjectTypes, projectObject(obj))
	}

	links := append([]ir.LinkType{}, o.Links...)
	sort.Slice(links, func(i, j int) bool { return links[i].Name < links[j].Name })
	for _, link := range links {
		schema.LinkTypes = append(schema.LinkTypes, projectLink(link))
	}
	return schema
}

func projectObject(obj ir.ObjectType) spi.ObjectTypeDefinition {
	var props []spi.PropertyDefinition
	var indexes []spi.IndexDefinition

	for _, f := range obj.Fields {
		if f.Role == ir.RolePrimary || f.Role == ir.RoleComputed || f.Role == ir.RoleLinkNav {
			continue
		}
		props = append(props, spi.PropertyDefinition{
			Name:        f.Name,
			Type:        f.Type.Name,
			Required:    f.Type.NonNull,
			Description: f.Description,
			DefaultValue: f.Flags.Default,
		})
		if f.Flags.Unique {
			indexes = append(indexes, spi.IndexDefinition{Field: f.Name, IndexType: spi.IndexBTREE, Unique: true})
		} else if f.Flags.Indexed {
			indexes = append(indexes, spi.IndexDefinition{Field: f.Name, IndexType: spi.IndexBTREE})
		}
		if f.Flags.Searchable {
			indexes = append(indexes, spi.IndexDefinition{Field: f.Name, IndexType: spi.IndexFULLTEXT})
		}
	}

	def := spi.ObjectTypeDefinition{
		Name:       obj.Name,
		Properties: props,
	}
	if len(indexes) > 0 {
		def.Indexes = indexes
	}
	return def
}

func projectLink(link ir.LinkType) spi.LinkTypeDefinition {
	props := make([]spi.PropertyDefinition, 0, len(link.Fields))
	for _, f := range link.Fields {
		props = append(props, spi.PropertyDefinition{
			Name:     f.Name,
			Type:     f.Type.Name,
			Required: f.Type.NonNull,
		})
	}
	def := spi.LinkTypeDefinition{
		Name:        link.Name,
		FromType:    link.From,
		ToType:      link.To,
		Cardinality: spi.Cardinality(link.Cardinality),
	}
	if len(props) > 0 {
		def.Properties = props
	}
	return def
}
