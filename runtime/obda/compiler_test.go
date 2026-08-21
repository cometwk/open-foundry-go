package obda_test

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/spi"
)

func TestCompileUnknownModel(t *testing.T) {
	doc, err := obda.Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	_, err = obda.Compile(spi.OntologySchema{
		ObjectTypes: []spi.ObjectTypeDefinition{{Name: "Other"}},
	}, doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileRequiresIdentityField(t *testing.T) {
	doc, err := obda.Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	_, err = obda.Compile(spi.OntologySchema{
		ObjectTypes: []spi.ObjectTypeDefinition{{Name: "Patient", Properties: []spi.PropertyDefinition{{Name: "name", Type: "String"}}}},
	}, doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}
