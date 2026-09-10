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
	raw := strings.Replace(validYAML, "insert: generated", "insert: provided", 1)
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

func TestCompileSearchFields(t *testing.T) {
	raw := strings.Replace(validYAML,
		"    fields:\n      name:\n        column: patient_name",
		"    search:\n      fields: [name, city]\n    fields:\n      name:\n        column: patient_name\n      city:\n        column: city_name", 1)
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := obda.Compile(spi.OntologySchema{
		ObjectTypes: []spi.ObjectTypeDefinition{{Name: "Patient", Properties: []spi.PropertyDefinition{
			{Name: "name", Type: "String"},
			{Name: "city", Type: "String"},
		}}},
		LinkTypes: []spi.LinkTypeDefinition{{Name: "AdmittedTo"}},
	}, doc)
	if err != nil {
		t.Fatal(err)
	}
	m := compiled.Models["Patient"]
	if got, want := m.SearchableFields, []string{"patient_name", "city_name"}; !eqStrings(got, want) {
		t.Fatalf("SearchableFields=%v want %v", got, want)
	}
	if m.SearchIndex != "" {
		t.Fatalf("SearchIndex should be zero, got %q", m.SearchIndex)
	}
}

func TestCompileSearchUnknownField(t *testing.T) {
	raw := strings.Replace(validYAML, "    fields:", "    search:\n      fields: [nonexistent]\n    fields:", 1)
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, err = obda.Compile(spi.OntologySchema{
		ObjectTypes: []spi.ObjectTypeDefinition{{Name: "Patient", Properties: []spi.PropertyDefinition{{Name: "name", Type: "String"}}}},
		LinkTypes:   []spi.LinkTypeDefinition{{Name: "AdmittedTo"}},
	}, doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("unknown search field should return ErrInvalidMapping, got %v", err)
	}
}

func TestCompileSearchNonTextType(t *testing.T) {
	raw := strings.Replace(validYAML,
		"    fields:\n      name:\n        column: patient_name",
		"    search:\n      fields: [age]\n    fields:\n      name:\n        column: patient_name\n      age:\n        column: patient_age", 1)
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, err = obda.Compile(spi.OntologySchema{
		ObjectTypes: []spi.ObjectTypeDefinition{{Name: "Patient", Properties: []spi.PropertyDefinition{
			{Name: "name", Type: "String"},
			{Name: "age", Type: "int"},
		}}},
		LinkTypes: []spi.LinkTypeDefinition{{Name: "AdmittedTo"}},
	}, doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("non-text search field should return ErrInvalidMapping, got %v", err)
	}
}

func TestCompileSearchEmptyFields(t *testing.T) {
	raw := strings.Replace(validYAML, "    fields:", "    search:\n      fields: []\n    fields:", 1)
	doc, err := obda.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, err = obda.Compile(spi.OntologySchema{
		ObjectTypes: []spi.ObjectTypeDefinition{{Name: "Patient", Properties: []spi.PropertyDefinition{{Name: "name", Type: "String"}}}},
		LinkTypes:   []spi.LinkTypeDefinition{{Name: "AdmittedTo"}},
	}, doc)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("empty search fields list should return ErrInvalidMapping, got %v", err)
	}
}

func TestCompileNoSearchLeavesZero(t *testing.T) {
	doc, err := obda.Parse([]byte(validYAML))
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
	m := compiled.Models["Patient"]
	if len(m.SearchableFields) != 0 {
		t.Fatalf("SearchableFields should be empty, got %v", m.SearchableFields)
	}
}
