package odl

import (
	"fmt"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"github.com/vektah/gqlparser/v2/parser"
)

// Source is a named ODL document.
type Source struct {
	Name    string
	Content string
}

// Parse parses one or more ODL sources into a gqlparser SchemaDocument.
// Callers must Lower the result into ir.Ontology; do not treat the AST as IR.
func Parse(sources ...Source) (*ast.SchemaDocument, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("odl: no sources")
	}
	asts := make([]*ast.Source, 0, len(sources))
	for _, s := range sources {
		name := s.Name
		if name == "" {
			name = "schema.odl"
		}
		asts = append(asts, &ast.Source{Name: name, Input: s.Content})
	}
	doc, err := parser.ParseSchemas(asts...)
	if err != nil {
		return nil, fmt.Errorf("odl parse: %w", formatGQLError(err))
	}
	return doc, nil
}

// ParseString parses a single unnamed ODL string.
func ParseString(content string) (*ast.SchemaDocument, error) {
	return Parse(Source{Name: "inline.odl", Content: content})
}

func formatGQLError(err error) error {
	if err == nil {
		return nil
	}
	if list, ok := err.(gqlerror.List); ok {
		parts := make([]string, 0, len(list))
		for _, e := range list {
			parts = append(parts, e.Error())
		}
		return fmt.Errorf("%s", strings.Join(parts, "; "))
	}
	return err
}
