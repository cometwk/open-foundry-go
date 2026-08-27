// Package graphql projects Ontology IR to a read-only GraphQL SDL string.
// The output is parsed at boot by graph-gophers/graphql-go; this package
// does not import a GraphQL AST.
package graphql

import (
	"sort"
	"strings"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
)

var builtinScalars = map[string]bool{
	"ID": true, "String": true, "Int": true, "Float": true, "Boolean": true,
	"Date": true, "DateTime": true, "Duration": true, "GeoPoint": true,
	"JSON": true, "URI": true,
}

var customScalars = []string{"Date", "DateTime", "Duration", "GeoPoint", "JSON", "URI"}

var stringFilterOps = []string{"eq", "ne", "in", "contains", "startsWith"}
var numericFilterOps = []string{"eq", "ne", "in", "gt", "gte", "lt", "lte"}
var booleanFilterOps = []string{"eq", "ne"}
var idFilterOps = []string{"eq", "ne", "in"}

var orderable = map[string]bool{
	"ID": true, "String": true, "Int": true, "Float": true,
	"Date": true, "DateTime": true, "Duration": true, "URI": true,
}

// Generate returns GraphQL SDL for a read-only projection of o.
// Search root fields are emitted only when caps.SupportsFullTextSearch is true.
func Generate(o *ir.Ontology, caps spi.StorageCapabilities) string {
	if o == nil {
		return ""
	}
	objects := append([]ir.ObjectType{}, o.Objects...)
	sort.Slice(objects, func(i, j int) bool { return objects[i].Name < objects[j].Name })

	objectNames := map[string]bool{}
	enumNames := map[string]bool{}
	for _, obj := range objects {
		objectNames[obj.Name] = true
	}
	for _, en := range o.Enums {
		enumNames[en.Name] = true
	}

	var b strings.Builder
	writeScalars(&b, o)
	writeEnums(&b, o)
	writeShared(&b)

	usedScalarFilters := map[string]bool{}
	usedEnumFilters := map[string]bool{}
	for _, obj := range objects {
		for _, f := range filterableFields(obj, objectNames) {
			if builtinScalars[f.Type.Name] {
				usedScalarFilters[f.Type.Name] = true
			}
			if enumNames[f.Type.Name] {
				usedEnumFilters[f.Type.Name] = true
			}
		}
	}
	for _, name := range sortedKeys(usedScalarFilters) {
		writeScalarFilter(&b, name)
	}
	for _, name := range sortedKeys(usedEnumFilters) {
		writeEnumFilter(&b, name)
	}

	for _, obj := range objects {
		writeObjectType(&b, obj, objectNames)
		writeConnection(&b, obj.Name)
		writeFilter(&b, obj, objectNames)
		writeOrderBy(&b, obj, objectNames)
		if caps.SupportsFullTextSearch {
			writeSearchTypes(&b, obj.Name)
		}
	}

	writeAggregateTypes(&b)
	writeQuery(&b, objects, caps)
	return b.String()
}

func writeScalars(b *strings.Builder, o *ir.Ontology) {
	used := map[string]bool{"DateTime": true, "JSON": true}
	collect := func(fields []ir.Field) {
		for _, f := range fields {
			if isCustomScalar(f.Type.Name) {
				used[f.Type.Name] = true
			}
		}
	}
	for _, obj := range o.Objects {
		collect(obj.Fields)
	}
	for _, link := range o.Links {
		collect(link.Fields)
	}
	for _, s := range o.Scalars {
		used[s.Name] = true
	}
	for _, name := range customScalars {
		if used[name] {
			b.WriteString("scalar ")
			b.WriteString(name)
			b.WriteByte('\n')
		}
	}
	for _, s := range o.Scalars {
		if !isCustomScalar(s.Name) {
			b.WriteString("scalar ")
			b.WriteString(s.Name)
			b.WriteByte('\n')
		}
	}
	b.WriteByte('\n')
}

func writeEnums(b *strings.Builder, o *ir.Ontology) {
	enums := append([]ir.EnumType{}, o.Enums...)
	sort.Slice(enums, func(i, j int) bool { return enums[i].Name < enums[j].Name })
	for _, en := range enums {
		b.WriteString("enum ")
		b.WriteString(en.Name)
		b.WriteString(" {\n")
		for _, v := range en.Values {
			b.WriteString("  ")
			b.WriteString(v.Name)
			b.WriteByte('\n')
		}
		b.WriteString("}\n\n")
	}
}

func writeShared(b *strings.Builder) {
	b.WriteString(`type PageInfo {
  hasNextPage: Boolean!
  hasPreviousPage: Boolean!
  startCursor: String
  endCursor: String
}

enum SortDirection {
  ASC
  DESC
}

`)
}

func writeScalarFilter(b *strings.Builder, typeName string) {
	ops := filterOps(typeName)
	b.WriteString("input ")
	b.WriteString(typeName)
	b.WriteString("Filter {\n")
	for _, op := range ops {
		b.WriteString("  ")
		b.WriteString(op)
		b.WriteString(": ")
		if op == "in" {
			b.WriteString("[")
			b.WriteString(typeName)
			b.WriteString("!]")
		} else {
			b.WriteString(typeName)
		}
		b.WriteByte('\n')
	}
	b.WriteString("}\n\n")
}

func writeEnumFilter(b *strings.Builder, name string) {
	b.WriteString("input ")
	b.WriteString(name)
	b.WriteString("Filter {\n")
	b.WriteString("  eq: ")
	b.WriteString(name)
	b.WriteByte('\n')
	b.WriteString("  ne: ")
	b.WriteString(name)
	b.WriteByte('\n')
	b.WriteString("  in: [")
	b.WriteString(name)
	b.WriteString("!]\n")
	b.WriteString("}\n\n")
}

func writeObjectType(b *strings.Builder, obj ir.ObjectType, objectNames map[string]bool) {
	b.WriteString("type ")
	b.WriteString(obj.Name)
	b.WriteString(" {\n")
	for _, f := range obj.Fields {
		b.WriteString("  ")
		b.WriteString(f.Name)
		b.WriteString(": ")
		b.WriteString(fieldGQLType(f, objectNames))
		b.WriteByte('\n')
	}
	b.WriteString("}\n\n")
}

func writeConnection(b *strings.Builder, typeName string) {
	b.WriteString("type ")
	b.WriteString(typeName)
	b.WriteString("Connection {\n")
	b.WriteString("  edges: [")
	b.WriteString(typeName)
	b.WriteString("Edge!]!\n")
	b.WriteString("  pageInfo: PageInfo!\n")
	b.WriteString("  totalCount: Int!\n")
	b.WriteString("}\n\n")
	b.WriteString("type ")
	b.WriteString(typeName)
	b.WriteString("Edge {\n")
	b.WriteString("  node: ")
	b.WriteString(typeName)
	b.WriteString("!\n")
	b.WriteString("  cursor: String!\n")
	b.WriteString("}\n\n")
}

func writeFilter(b *strings.Builder, obj ir.ObjectType, objectNames map[string]bool) {
	b.WriteString("input ")
	b.WriteString(obj.Name)
	b.WriteString("Filter {\n")
	for _, f := range filterableFields(obj, objectNames) {
		b.WriteString("  ")
		b.WriteString(f.Name)
		b.WriteString(": ")
		b.WriteString(f.Type.Name)
		b.WriteString("Filter\n")
	}
	b.WriteString("  AND: [")
	b.WriteString(obj.Name)
	b.WriteString("Filter!]\n")
	b.WriteString("  OR: [")
	b.WriteString(obj.Name)
	b.WriteString("Filter!]\n")
	b.WriteString("  NOT: ")
	b.WriteString(obj.Name)
	b.WriteString("Filter\n")
	b.WriteString("}\n\n")
}

func writeOrderBy(b *strings.Builder, obj ir.ObjectType, objectNames map[string]bool) {
	b.WriteString("input ")
	b.WriteString(obj.Name)
	b.WriteString("OrderBy {\n")
	for _, f := range filterableFields(obj, objectNames) {
		if orderable[f.Type.Name] || !builtinScalars[f.Type.Name] {
			b.WriteString("  ")
			b.WriteString(f.Name)
			b.WriteString(": SortDirection\n")
		}
	}
	b.WriteString("}\n\n")
}

func writeSearchTypes(b *strings.Builder, typeName string) {
	b.WriteString("type SearchHit_")
	b.WriteString(typeName)
	b.WriteString(" {\n  node: ")
	b.WriteString(typeName)
	b.WriteString("!\n  score: Float!\n}\n\n")
	b.WriteString("type SearchResult_")
	b.WriteString(typeName)
	b.WriteString(" {\n  hits: [SearchHit_")
	b.WriteString(typeName)
	b.WriteString("!]!\n  totalCount: Int!\n  hasNextPage: Boolean!\n}\n\n")
}

func writeAggregateTypes(b *strings.Builder) {
	b.WriteString(`enum AggregateFunction {
  COUNT
  SUM
  AVG
  MIN
  MAX
}

input AggregateFieldInput {
  field: String!
  fn: AggregateFunction!
  alias: String
}

type AggregateGroup {
  keys: JSON!
  values: JSON!
}

type AggregateResult {
  groups: [AggregateGroup!]!
  totalGroups: Int!
}

`)
}

func writeQuery(b *strings.Builder, objects []ir.ObjectType, caps spi.StorageCapabilities) {
	b.WriteString("type Query {\n")
	for _, obj := range objects {
		lower := LowerFirst(obj.Name)
		b.WriteString("  ")
		b.WriteString(lower)
		b.WriteString("(id: ID!): ")
		b.WriteString(obj.Name)
		b.WriteByte('\n')
		b.WriteString("  ")
		b.WriteString(lower)
		b.WriteString("s(filter: ")
		b.WriteString(obj.Name)
		b.WriteString("Filter, orderBy: ")
		b.WriteString(obj.Name)
		b.WriteString("OrderBy, first: Int, after: String, last: Int, before: String): ")
		b.WriteString(obj.Name)
		b.WriteString("Connection!\n")
		b.WriteString("  ")
		b.WriteString(lower)
		b.WriteString("Aggregate(filter: ")
		b.WriteString(obj.Name)
		b.WriteString("Filter, groupBy: [String!], fields: [AggregateFieldInput!]!): AggregateResult!\n")
		if caps.SupportsFullTextSearch {
			b.WriteString("  search")
			b.WriteString(obj.Name)
			b.WriteString("s(query: String!, fields: [String!], filter: ")
			b.WriteString(obj.Name)
			b.WriteString("Filter, first: Int, after: String): SearchResult_")
			b.WriteString(obj.Name)
			b.WriteString("!\n")
		}
	}
	b.WriteString("}\n")
}

func filterableFields(obj ir.ObjectType, objectNames map[string]bool) []ir.Field {
	var out []ir.Field
	for _, f := range obj.Fields {
		if f.Type.IsList {
			continue
		}
		if f.Role == ir.RoleLinkNav || f.Role == ir.RoleComputed {
			continue
		}
		if objectNames[f.Type.Name] {
			continue
		}
		out = append(out, f)
	}
	return out
}

func fieldGQLType(f ir.Field, objectNames map[string]bool) string {
	t := f.Type
	// Object-valued FK properties can miss at read time (KTD-7 nested null).
	// Keep list @link nullability from IR so empty navigation is [] not null.
	if f.Role == ir.RoleProperty && objectNames[t.Name] && !t.IsList {
		return t.Name
	}
	if t.IsList {
		elem := t.Name
		if t.ListElementNonNull {
			elem += "!"
		}
		inner := "[" + elem + "]"
		if t.NonNull {
			return inner + "!"
		}
		return inner
	}
	if t.NonNull {
		return t.Name + "!"
	}
	return t.Name
}

func filterOps(typeName string) []string {
	switch typeName {
	case "ID":
		return idFilterOps
	case "Boolean":
		return booleanFilterOps
	case "Int", "Float", "Date", "DateTime", "Duration":
		return numericFilterOps
	default:
		return stringFilterOps
	}
}

func isCustomScalar(name string) bool {
	for _, s := range customScalars {
		if s == name {
			return true
		}
	}
	return false
}

// LowerFirst maps TypeName to the REST/GraphQL root prefix (product, inventoryRecord).
func LowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
