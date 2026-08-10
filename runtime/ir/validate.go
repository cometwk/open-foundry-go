package ir

import (
	"fmt"
	"strings"
)

// Error is a semantic validation error.
type Error struct {
	Code    string
	Message string
	Type    string
	Field   string
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.Code)
	b.WriteString(": ")
	b.WriteString(e.Message)
	if e.Type != "" {
		b.WriteString(" (type=")
		b.WriteString(e.Type)
		if e.Field != "" {
			b.WriteString(" field=")
			b.WriteString(e.Field)
		}
		b.WriteString(")")
	}
	return b.String()
}

// Validate performs post-merge semantic checks on an Ontology.
func Validate(o *Ontology) error {
	if o == nil {
		return &Error{Code: "EMPTY", Message: "ontology is nil"}
	}

	typeNames := map[string]string{}
	for _, obj := range o.Objects {
		if prev, ok := typeNames[obj.Name]; ok {
			return &Error{Code: "DUPLICATE_TYPE", Message: fmt.Sprintf("duplicate type %q (was %s)", obj.Name, prev), Type: obj.Name}
		}
		typeNames[obj.Name] = "object"
	}
	for _, link := range o.Links {
		if prev, ok := typeNames[link.Name]; ok {
			return &Error{Code: "DUPLICATE_TYPE", Message: fmt.Sprintf("duplicate type %q (was %s)", link.Name, prev), Type: link.Name}
		}
		typeNames[link.Name] = "link"
	}
	for _, en := range o.Enums {
		typeNames[en.Name] = "enum"
	}
	for _, iface := range o.Interfaces {
		typeNames[iface.Name] = "interface"
	}

	for _, obj := range o.Objects {
		primaries := 0
		for _, f := range obj.Fields {
			if f.Role == RolePrimary {
				primaries++
			}
		}
		if primaries != 1 {
			return &Error{
				Code:    "PRIMARY_COUNT",
				Message: fmt.Sprintf("object %q must have exactly one primary field, found %d", obj.Name, primaries),
				Type:    obj.Name,
			}
		}
	}

	for _, link := range o.Links {
		if typeNames[link.From] != "object" {
			return &Error{Code: "LINK_ENDPOINT", Message: fmt.Sprintf("link %q from %q is not an object type", link.Name, link.From), Type: link.Name}
		}
		if typeNames[link.To] != "object" {
			return &Error{Code: "LINK_ENDPOINT", Message: fmt.Sprintf("link %q to %q is not an object type", link.Name, link.To), Type: link.Name}
		}
	}

	for _, act := range o.Actions {
		for _, f := range act.Fields {
			if f.Role != RoleParam {
				return &Error{
					Code:    "ACTION_PARAM",
					Message: fmt.Sprintf("action %q field %q must be a param", act.Name, f.Name),
					Type:    act.Name,
					Field:   f.Name,
				}
			}
		}
	}

	for _, en := range o.Enums {
		seen := map[string]bool{}
		for _, v := range en.Values {
			if seen[v.Name] {
				return &Error{Code: "ENUM_DUP", Message: fmt.Sprintf("enum %q has duplicate value %q", en.Name, v.Name), Type: en.Name}
			}
			seen[v.Name] = true
		}
	}

	return nil
}
