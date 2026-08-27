package odl

import (
	"fmt"
	"strconv"

	"github.com/openfoundry/runtime/ir"
	"github.com/vektah/gqlparser/v2/ast"
)

// Lower converts a gqlparser SchemaDocument into Ontology IR.
// The returned IR does not retain GraphQL AST nodes.
func Lower(doc *ast.SchemaDocument) (*ir.Ontology, error) {
	if doc == nil {
		return nil, fmt.Errorf("odl: nil document")
	}
	out := &ir.Ontology{}

	for _, schemaDef := range doc.Schema {
		if ns := namespaceFromDirectives(schemaDef.Directives); ns != nil && out.Namespace == nil {
			out.Namespace = ns
		}
	}
	for _, schemaDef := range doc.SchemaExtension {
		if ns := namespaceFromDirectives(schemaDef.Directives); ns != nil && out.Namespace == nil {
			out.Namespace = ns
		}
	}

	for _, def := range doc.Definitions {
		if err := lowerDefinition(def, out); err != nil {
			return nil, err
		}
	}
	for _, def := range doc.Extensions {
		if err := lowerDefinition(def, out); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func lowerDefinition(def *ast.Definition, out *ir.Ontology) error {
	switch def.Kind {
	case ast.Object:
		return lowerObjectLike(def, out)
	case ast.Enum:
		out.Enums = append(out.Enums, lowerEnum(def))
	case ast.Interface:
		out.Interfaces = append(out.Interfaces, lowerInterface(def))
	case ast.Scalar:
		out.Scalars = append(out.Scalars, ir.ScalarType{
			Name:        def.Name,
			Description: def.Description,
		})
	}
	return nil
}

// ParseAndLower parses sources and lowers to IR.
func ParseAndLower(sources ...Source) (*ir.Ontology, error) {
	doc, err := Parse(sources...)
	if err != nil {
		return nil, err
	}
	return Lower(doc)
}

func namespaceFromDirectives(dirs ast.DirectiveList) *ir.Namespace {
	d := dirs.ForName("namespace")
	if d == nil {
		return nil
	}
	return &ir.Namespace{
		Name:    stringArg(d, "name"),
		Version: stringArg(d, "version"),
	}
}

func lowerObjectLike(def *ast.Definition, out *ir.Ontology) error {
	if d := def.Directives.ForName("linkType"); d != nil {
		out.Links = append(out.Links, ir.LinkType{
			Name:        def.Name,
			Description: def.Description,
			From:        stringArg(d, "from"),
			To:          stringArg(d, "to"),
			Cardinality: ir.Cardinality(enumOrStringArg(d, "cardinality", "MANY_TO_MANY")),
			Fields:      lowerFields(def.Fields, false),
		})
		return nil
	}
	if def.Directives.ForName("actionType") != nil {
		out.Actions = append(out.Actions, ir.ActionType{
			Name:        def.Name,
			Description: def.Description,
			Fields:      lowerFields(def.Fields, true),
		})
		return nil
	}
	// Default object type
	obj := ir.ObjectType{
		Name:        def.Name,
		Description: def.Description,
		Fields:      lowerFields(def.Fields, false),
		Implements:  append([]string{}, def.Interfaces...),
	}
	if c := def.Directives.ForName("constraint"); c != nil {
		obj.Constraints = append(obj.Constraints, stringArg(c, "expr"))
	}
	out.Objects = append(out.Objects, obj)
	return nil
}

func lowerEnum(def *ast.Definition) ir.EnumType {
	vals := make([]ir.EnumValue, 0, len(def.EnumValues))
	for _, v := range def.EnumValues {
		vals = append(vals, ir.EnumValue{Name: v.Name, Description: v.Description})
	}
	return ir.EnumType{Name: def.Name, Description: def.Description, Values: vals}
}

func lowerInterface(def *ast.Definition) ir.InterfaceType {
	return ir.InterfaceType{
		Name:        def.Name,
		Description: def.Description,
		Fields:      lowerFields(def.Fields, false),
	}
}

func lowerFields(fields ast.FieldList, actionParams bool) []ir.Field {
	out := make([]ir.Field, 0, len(fields))
	for _, f := range fields {
		out = append(out, lowerField(f, actionParams))
	}
	return out
}

func lowerField(f *ast.FieldDefinition, actionParams bool) ir.Field {
	field := ir.Field{
		Name:        f.Name,
		Description: f.Description,
		Type:        lowerType(f.Type),
		Role:        ir.RoleProperty,
	}
	if actionParams {
		field.Role = ir.RoleParam
	}

	for _, d := range f.Directives {
		switch d.Name {
		case "primary":
			field.Role = ir.RolePrimary
		case "param":
			field.Role = ir.RoleParam
		case "unique":
			field.Flags.Unique = true
		case "indexed":
			field.Flags.Indexed = true
		case "readonly":
			field.Flags.Readonly = true
		case "immutable":
			field.Flags.Immutable = true
		case "sensitive":
			field.Flags.Sensitive = true
		case "link":
			field.Role = ir.RoleLinkNav
			field.Link = &ir.LinkRef{
				Type:      stringArg(d, "type"),
				Direction: ir.Direction(enumOrStringArg(d, "direction", "OUTBOUND")),
				History:   boolArg(d, "history"),
			}
		case "computed":
			field.Role = ir.RoleComputed
			field.Computed = &ir.ComputedRef{
				Fn:    stringArg(d, "fn"),
				Args:  valueArg(d, "args"),
				Cache: enumOrStringArg(d, "cache", ""),
				TTL:   stringArg(d, "ttl"),
			}
		case "constraint":
			field.Flags.Constraint = stringArg(d, "expr")
		case "default":
			field.Flags.Default = valueArg(d, "value")
		case "deprecated":
			field.Flags.Deprecated = stringArg(d, "reason")
		case "terminology":
			field.Flags.Terminology = stringArg(d, "system")
		case "searchable":
			field.Flags.Searchable = true
			if w := floatArg(d, "weight"); w != nil {
				field.Flags.SearchWeight = w
			}
			field.Flags.SearchAnalyzer = stringArg(d, "analyzer")
		}
	}
	return field
}

func lowerType(t *ast.Type) ir.TypeRef {
	ref := ir.TypeRef{}
	if t == nil {
		return ref
	}
	ref.NonNull = t.NonNull
	if t.Elem != nil {
		ref.IsList = true
		ref.ListElementNonNull = t.Elem.NonNull
		ref.Name = namedType(t.Elem)
		return ref
	}
	ref.Name = t.NamedType
	return ref
}

func namedType(t *ast.Type) string {
	if t == nil {
		return ""
	}
	if t.NamedType != "" {
		return t.NamedType
	}
	if t.Elem != nil {
		return namedType(t.Elem)
	}
	return ""
}

func stringArg(d *ast.Directive, name string) string {
	arg := d.Arguments.ForName(name)
	if arg == nil || arg.Value == nil {
		return ""
	}
	switch arg.Value.Kind {
	case ast.StringValue, ast.EnumValue:
		return arg.Value.Raw
	default:
		return arg.Value.Raw
	}
}

func enumOrStringArg(d *ast.Directive, name, fallback string) string {
	v := stringArg(d, name)
	if v == "" {
		return fallback
	}
	return v
}

func boolArg(d *ast.Directive, name string) bool {
	arg := d.Arguments.ForName(name)
	if arg == nil || arg.Value == nil {
		return false
	}
	return arg.Value.Raw == "true"
}

func floatArg(d *ast.Directive, name string) *float64 {
	arg := d.Arguments.ForName(name)
	if arg == nil || arg.Value == nil {
		return nil
	}
	f, err := strconv.ParseFloat(arg.Value.Raw, 64)
	if err != nil {
		return nil
	}
	return &f
}

func valueArg(d *ast.Directive, name string) any {
	arg := d.Arguments.ForName(name)
	if arg == nil || arg.Value == nil {
		return nil
	}
	return decodeValue(arg.Value)
}

func decodeValue(v *ast.Value) any {
	if v == nil {
		return nil
	}
	switch v.Kind {
	case ast.StringValue, ast.EnumValue:
		return v.Raw
	case ast.IntValue:
		i, _ := strconv.Atoi(v.Raw)
		return i
	case ast.FloatValue:
		f, _ := strconv.ParseFloat(v.Raw, 64)
		return f
	case ast.BooleanValue:
		return v.Raw == "true"
	case ast.NullValue:
		return nil
	case ast.ListValue:
		out := make([]any, 0, len(v.Children))
		for _, c := range v.Children {
			out = append(out, decodeValue(c.Value))
		}
		return out
	case ast.ObjectValue:
		out := map[string]any{}
		for _, c := range v.Children {
			out[c.Name] = decodeValue(c.Value)
		}
		return out
	default:
		return v.Raw
	}
}
