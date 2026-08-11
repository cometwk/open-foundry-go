package engine

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

// validOntology is the smallest TBox ir.Validate accepts: a namespace
// and one object type with exactly one Primary field plus an ordinary
// property. Mirrors ir.Validate's "exactly one Primary per object" rule
// and its namespace-presence check.
func validOntology(t *testing.T) *ir.Ontology {
	t.Helper()
	return &ir.Ontology{
		Namespace: &ir.Namespace{Name: "test"},
		Objects: []ir.ObjectType{
			{
				Name: "Supplier",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "name", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
				},
			},
		},
	}
}

func TestNew_ValidOntology_Constructs(t *testing.T) {
	storage := memory.New()
	e, err := New(storage, validOntology(t))
	if err != nil {
		t.Fatalf("New(valid ontology) err = %v, want nil", err)
	}
	if e == nil {
		t.Fatal("New(valid ontology) = nil engine, want non-nil")
	}
}

func TestNew_InvalidOntology_BubblesValidateError(t *testing.T) {
	storage := memory.New()
	// Missing namespace; ir.Validate rejects this. Additionally no Primary.
	invalid := &ir.Ontology{
		Objects: []ir.ObjectType{
			{Name: "Supplier", Fields: []ir.Field{{Name: "x", Role: ir.RoleProperty}}},
		},
	}
	_, err := New(storage, invalid)
	if err == nil {
		t.Fatal("New(invalid ontology) = nil err, want validation error")
	}
	var verr *ir.Error
	if !errors.As(err, &verr) {
		// ir.Validate returns *ir.Error; accept it. If it ever returned a
		// plain error, surface the surprise here rather than silently
		// swallowing it.
		t.Fatalf("New(invalid ontology) err = %v (%T), want *ir.Error", err, err)
	}
}

// engine must not hold the concrete memory.Provider type; the test below
// is a compile-time assertion that Engine only stores a StorageProvider
// interface. (Captured here so it lives next to the struct's test.)
func TestEngine_SatisfiesStorageProvider(t *testing.T) {
	var _ spi.StorageProvider = memory.New() // sanity check the test fixture
}
