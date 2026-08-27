package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DateTime is the GraphQL DateTime scalar. Values are RFC3339 strings,
// matching memory's storage wire format.
type DateTime string

func (DateTime) ImplementsGraphQLType(name string) bool { return name == "DateTime" }

func (t *DateTime) UnmarshalGraphQL(input any) error {
	s, ok := input.(string)
	if !ok {
		return fmt.Errorf("DateTime expects string, got %T", input)
	}
	*t = DateTime(s)
	return nil
}

func (t DateTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(t))
}

// JSON is the GraphQL JSON scalar used by AggregateGroup keys/values.
type JSON struct{ V any }

func (JSON) ImplementsGraphQLType(name string) bool { return name == "JSON" }

func (j *JSON) UnmarshalGraphQL(input any) error {
	j.V = input
	return nil
}

func (j JSON) MarshalJSON() ([]byte, error) {
	if j.V == nil {
		return []byte("null"), nil
	}
	return json.Marshal(j.V)
}

// GQLInput unpacks any GraphQL input object (Filter / OrderBy) into a map
// so the API layer does not need per-type Go structs.
type GQLInput map[string]any

func (GQLInput) ImplementsGraphQLType(name string) bool {
	return strings.HasSuffix(name, "Filter") || strings.HasSuffix(name, "OrderBy")
}

func (g *GQLInput) UnmarshalGraphQL(input any) error {
	if input == nil {
		*g = nil
		return nil
	}
	m, ok := input.(map[string]any)
	if !ok {
		return fmt.Errorf("input object expects map, got %T", input)
	}
	*g = m
	return nil
}

func (GQLInput) Nullable() {}
