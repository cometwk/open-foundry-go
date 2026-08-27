package ir

// FieldRole is the semantic role of a field after ODL lower.
type FieldRole int

const (
	RoleProperty FieldRole = iota
	RolePrimary
	RoleParam
	RoleLinkNav
	RoleComputed
)

func (r FieldRole) String() string {
	switch r {
	case RolePrimary:
		return "primary"
	case RoleParam:
		return "param"
	case RoleLinkNav:
		return "linkNav"
	case RoleComputed:
		return "computed"
	default:
		return "property"
	}
}

// Cardinality for link types.
type Cardinality string

const (
	CardinalityOneToOne   Cardinality = "ONE_TO_ONE"
	CardinalityOneToMany  Cardinality = "ONE_TO_MANY"
	CardinalityManyToOne  Cardinality = "MANY_TO_ONE"
	CardinalityManyToMany Cardinality = "MANY_TO_MANY"
)

// Direction for link navigation fields.
type Direction string

const (
	DirectionInbound  Direction = "INBOUND"
	DirectionOutbound Direction = "OUTBOUND"
)

// Namespace identifies an ontology namespace.
type Namespace struct {
	Name    string
	Version string
}

// TypeRef is a field type reference.
type TypeRef struct {
	Name               string // The base type name (e.g., "String", "Patient", "PatientStatus").
	NonNull            bool   // Whether the field is non-null (has !).
	IsList             bool   // Whether the field is a list type.
	ListElementNonNull bool   // Whether list elements are non-null (e.g., [Patient!]!).
}

// LinkRef describes a @link navigation.
type LinkRef struct {
	Type      string
	Direction Direction
	History   bool
}

// ComputedRef describes a @computed field.
type ComputedRef struct {
	Fn    string
	Args  any
	Cache string
	TTL   string
}

// FieldFlags are storage-relevant annotations on a property.
type FieldFlags struct {
	Unique         bool
	Indexed        bool
	Searchable     bool
	Readonly       bool
	Immutable      bool
	Sensitive      bool
	Constraint     string
	Default        any
	Deprecated     string
	Terminology    string
	SearchWeight   *float64
	SearchAnalyzer string
}

// Field is a semantic field definition.
type Field struct {
	Name        string
	Type        TypeRef
	Description string
	Role        FieldRole
	Flags       FieldFlags
	Link        *LinkRef
	Computed    *ComputedRef
}

// ObjectType is an ontology object type.
type ObjectType struct {
	Name        string
	Description string
	Fields      []Field
	Implements  []string
	Constraints []string
}

// LinkType is an ontology link type.
type LinkType struct {
	Name        string
	Description string
	From        string
	To          string
	Cardinality Cardinality
	Fields      []Field
}

// ActionType is an action signature (params only).
type ActionType struct {
	Name        string
	Description string
	Fields      []Field
}

// EnumValue is one enum member.
type EnumValue struct {
	Name        string
	Description string
}

// EnumType is an enum definition.
type EnumType struct {
	Name        string
	Description string
	Values      []EnumValue
}

// InterfaceType is an interface definition.
type InterfaceType struct {
	Name        string
	Description string
	Fields      []Field
}

// ScalarType is a custom scalar.
type ScalarType struct {
	Name        string
	Description string
}

// Ontology is the Phase 1 TBox semantic IR.
type Ontology struct {
	Namespace  *Namespace
	Objects    []ObjectType
	Links      []LinkType
	Actions    []ActionType
	Enums      []EnumType
	Interfaces []InterfaceType
	Scalars    []ScalarType
}

// ObjectByName returns an object type by name.
func (o *Ontology) ObjectByName(name string) *ObjectType {
	for i := range o.Objects {
		if o.Objects[i].Name == name {
			return &o.Objects[i]
		}
	}
	return nil
}

// LinkByName returns a link type by name.
func (o *Ontology) LinkByName(name string) *LinkType {
	for i := range o.Links {
		if o.Links[i].Name == name {
			return &o.Links[i]
		}
	}
	return nil
}

// ActionByName returns an action type by name.
func (o *Ontology) ActionByName(name string) *ActionType {
	for i := range o.Actions {
		if o.Actions[i].Name == name {
			return &o.Actions[i]
		}
	}
	return nil
}
