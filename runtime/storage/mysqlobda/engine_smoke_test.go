package mysqlobda_test

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
)

func TestCapabilitiesTemporalAndBulkOff(t *testing.T) {
	p, _ := activateReader(t)
	caps := p.Capabilities()
	if caps.SupportsTemporalQueries || caps.SupportsBulkMutations {
		t.Fatalf("%+v", caps)
	}
	if !caps.SupportsGraphTraversal || caps.MaxTraversalDepth != 8 {
		t.Fatalf("%+v", caps)
	}
	if !caps.SupportsTransactions || !caps.SupportsFullTextSearch {
		t.Fatalf("%+v", caps)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.GetObjectAtVersion(ctx, "Reader", "x", 1); !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("GetObjectAtVersion err=%v", err)
	}
	if _, err := p.BulkMutate(ctx, spi.BulkMutationRequest{}); !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("BulkMutate err=%v", err)
	}
}

func TestEngineSmokeRawIDs(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ont := libraryIR()
	e, err := engine.New(p, ont)
	if err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	obj, err := e.CreateObject(ctx, "Reader", map[string]any{"name": "Lao Wang"})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := obj[spi.FieldID].(string)
	if len(id) != 36 || id[14] != '7' {
		t.Fatalf("object id=%q want UUIDv7", id)
	}
	link, err := e.CreateLink(ctx, "Borrows", readerID, bookID, nil)
	if err != nil {
		t.Fatal(err)
	}
	lid, _ := link[spi.FieldID].(string)
	if len(lid) != 36 || lid[14] != '7' {
		t.Fatalf("link id=%q want UUIDv7", lid)
	}
}

// libraryIR is the simplified library-pack IR: Reader / Book joined by Borrows
// (简化版, namespace example.library).
func libraryIR() *ir.Ontology {
	return &ir.Ontology{
		Namespace: &ir.Namespace{Name: "example.library"},
		Objects: []ir.ObjectType{
			{
				Name: "Reader",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "name", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
				},
			},
			{
				Name: "Book",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "title", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
				},
			},
		},
		Links: []ir.LinkType{
			{Name: "Borrows", From: "Reader", To: "Book", Cardinality: ir.CardinalityManyToMany},
		},
	}
}
