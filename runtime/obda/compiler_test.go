package obda_test

import (
	"errors"
	"strings"
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
	raw := strings.Replace(validYAML, "insert: generated", "", 1)
	raw = strings.Replace(raw, "columns: [id]", "columns: [patient_id]", 1)
	doc, err := obda.Parse([]byte(raw))
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

func TestCompileOmitFlags(t *testing.T) {
	raw := strings.Replace(validYAML, "strategy: native", "strategy: native\n      omit: [version, deletedAt]", 1)
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := obda.Compile(spi.OntologySchema{
		ObjectTypes: []spi.ObjectTypeDefinition{{Name: "Patient", Properties: []spi.PropertyDefinition{{Name: "name", Type: "String"}}}},
		LinkTypes:   []spi.LinkTypeDefinition{{Name: "AdmittedTo"}},
	}, doc)
	if err != nil {
		t.Fatal(err)
	}
	if !compiled.Models["Patient"].Omit.Version || !compiled.Models["Patient"].Omit.DeletedAt {
		t.Fatalf("omit=%+v", compiled.Models["Patient"].Omit)
	}
	if compiled.Models["Patient"].Omit.CreatedAt || compiled.Models["Patient"].Omit.UpdatedAt {
		t.Fatalf("unexpected omit %+v", compiled.Models["Patient"].Omit)
	}
}
