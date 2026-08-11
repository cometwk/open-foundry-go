// Package conformance is the Go-native lifecycle conformance subset
// for the runtime. It mirrors the intent — not the AST — of the
// TypeScript SPI conformance suite at
// tests/spi-conformance/src/categories/{crud,links}.ts: it walks the
// six Phase 2 verbs (Create/Get/Update/Delete Object and Create/Delete
// Link), asserts the ErrUnimplemented floor for the SPI methods later
// phases leave unimplemented, and drives a single integration
// end-to-end through the supply-chain domain pack (the Gold Path).
//
// This is intentionally NOT a port of the TS suite. Phase 3 (U2) lifts
// _version bookkeeping, soft-delete writes, and temporal reads off the
// floor; cardinality, query, traversal, transactions, and event
// emission remain deferred and stay floored here until their units
// land. The package never asserts TS parity.
package conformance_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

// fixtureOntology is the small TBox the conformance subset exercises:
// a Supplier object type with a required name and an optional tier,
// a Part object type with a required sku, and a Supplies link
// Supplier→Part. ir.Validate accepts this (one Primary per object,
// link endpoints land on object types).
func fixtureOntology() *ir.Ontology {
	return &ir.Ontology{
		Namespace: &ir.Namespace{Name: "conformance"},
		Objects: []ir.ObjectType{
			{
				Name: "Supplier",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "name", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
					{Name: "tier", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
				},
			},
			{
				Name: "Part",
				Fields: []ir.Field{
					{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
					{Name: "sku", Type: ir.TypeRef{Name: "String", NonNull: true}, Role: ir.RoleProperty},
				},
			},
		},
		Links: []ir.LinkType{
			{Name: "Supplies", From: "Supplier", To: "Part", Cardinality: ir.CardinalityOneToMany},
		},
	}
}

func setup(t *testing.T) (*engine.Engine, spi.RequestContext) {
	t.Helper()
	e, err := engine.New(memory.New(), fixtureOntology())
	if err != nil {
		t.Fatalf("engine.New err = %v, want nil", err)
	}
	return e, spi.RequestContext{TenantID: "conformance-tenant", ActorID: "test"}
}

// TestConformance_ObjectRoundTrip walks the four object verbs:
// Create → Get → Update → Get → Delete → Get-not-found. Covers AE1
// (Create+Get round-trip), AE2 (Update reflects patch), AE3 (hard
// delete removes).
func TestConformance_ObjectRoundTrip(t *testing.T) {
	e, ctx := setup(t)

	// AE1: Create + Get round-trip returns system fields.
	obj, err := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme", "tier": "Gold"})
	if err != nil {
		t.Fatalf("CreateObject err = %v, want nil", err)
	}
	for _, k := range []string{"_id", "_type", "_createdAt", "_updatedAt", "_tenantId"} {
		if _, ok := obj[k]; !ok {
			t.Errorf("CreateObject missing system field %q (AE1)", k)
		}
	}
	if obj["_type"] != "Supplier" {
		t.Errorf("CreateObject _type = %v, want Supplier (AE1)", obj["_type"])
	}
	id := obj["_id"].(string)

	got, err := e.GetObject(ctx, "Supplier", id)
	if err != nil {
		t.Fatalf("GetObject err = %v, want nil (AE1)", err)
	}
	if got["name"] != "Acme" || got["tier"] != "Gold" {
		t.Errorf("GetObject name/tier = %v/%v, want Acme/Gold (AE1)", got["name"], got["tier"])
	}

	// AE2: Update reflects patch and merges (not replaces).
	updated, err := e.UpdateObject(ctx, "Supplier", id, map[string]any{"tier": "Platinum"}, nil)
	if err != nil {
		t.Fatalf("UpdateObject err = %v, want nil (AE2)", err)
	}
	if updated["tier"] != "Platinum" {
		t.Errorf("UpdateObject tier = %v, want Platinum (AE2)", updated["tier"])
	}
	if updated["name"] != "Acme" {
		t.Errorf("UpdateObject dropped unpatched name = %v, want Acme (AE2 merge)", updated["name"])
	}
	created := obj["_createdAt"]
	if updated["_createdAt"] != created {
		t.Errorf("UpdateObject _createdAt drifted = %v, want %v (AE2)", updated["_createdAt"], created)
	}

	// AE3: Hard delete removes; subsequent Get is not-found.
	if err := e.DeleteObject(ctx, "Supplier", id, "hard"); err != nil {
		t.Fatalf("DeleteObject err = %v, want nil (AE3)", err)
	}
	if _, err := e.GetObject(ctx, "Supplier", id); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("GetObject after hard delete err = %v, want ErrObjectNotFound (AE3)", err)
	}
}

// TestConformance_LinkRoundTrip walks Create → Get → Delete →
// Get-not-found on a link, with referential integrity checked on
// Create. Covers AE5 (Create returns system fields), AE6 (from/to
// missing → OBJECT_NOT_FOUND before storage write), AE7 (hard delete
// removes).
func TestConformance_LinkRoundTrip(t *testing.T) {
	e, ctx := setup(t)
	s, _ := e.CreateObject(ctx, "Supplier", map[string]any{"name": "Acme"})
	pt, _ := e.CreateObject(ctx, "Part", map[string]any{"sku": "P1"})

	// AE5: Create Link returns system fields.
	link, err := e.CreateLink(ctx, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	if err != nil {
		t.Fatalf("CreateLink err = %v, want nil (AE5)", err)
	}
	for _, k := range []string{"_id", "_type", "_fromId", "_toId", "_createdAt", "_updatedAt", "_tenantId"} {
		if _, ok := link[k]; !ok {
			t.Errorf("CreateLink missing system field %q (AE5)", k)
		}
	}
	if link["_type"] != "Supplies" {
		t.Errorf("CreateLink _type = %v, want Supplies (AE5)", link["_type"])
	}
	linkID := link["_id"].(string)

	got, err := e.GetLink(ctx, "Supplies", linkID)
	if err != nil {
		t.Fatalf("GetLink err = %v, want nil (AE5 round-trip)", err)
	}
	if got["_id"] != linkID {
		t.Errorf("GetLink _id = %v, want %v (AE5)", got["_id"], linkID)
	}

	// AE6: from-object missing → ErrObjectNotFound before write.
	if _, err := e.CreateLink(ctx, "Supplies", "missing-from", pt["_id"].(string), nil); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("CreateLink(missing from) err = %v, want ErrObjectNotFound (AE6)", err)
	}
	// AE6: to-object missing → ErrObjectNotFound before write.
	if _, err := e.CreateLink(ctx, "Supplies", s["_id"].(string), "missing-to", nil); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("CreateLink(missing to) err = %v, want ErrObjectNotFound (AE6)", err)
	}

	// AE7: Hard delete removes; subsequent GetLink is not-found.
	if err := e.DeleteLink(ctx, "Supplies", linkID); err != nil {
		t.Fatalf("DeleteLink err = %v, want nil (AE7)", err)
	}
	if _, err := e.GetLink(ctx, "Supplies", linkID); !errors.Is(err, spi.ErrLinkNotFound) {
		t.Fatalf("GetLink after delete err = %v, want ErrLinkNotFound (AE7)", err)
	}
}

// TestConformance_ErrUnimplementedFloor asserts every SPI method
// Phase 2 deliberately leaves unimplemented still surfaces
// ErrUnimplemented (and the method name appears in the error
// message), mirroring TS PlatformError surfacing. Covers AE8 / R15.
func TestConformance_ErrUnimplementedFloor(t *testing.T) {
	p := memory.New()
	ctx := spi.RequestContext{TenantID: "tnt", ActorID: "test"}

	cases := []struct {
		name string
		fn   func() error
	}{
		// Phase 3 (U4): QueryObjects/AggregateObjects/SearchObjects
		// implemented — removed from the ErrUnimplemented floor. Positive
		// coverage lives in runtime/storage/memory/provider_query_test.go.
		{"BulkMutate", func() error {
			_, err := p.BulkMutate(ctx, spi.BulkMutationRequest{})
			return err
		}},
		{"UpdateLink", func() error {
			_, err := p.UpdateLink(ctx, "Supplies", "x", map[string]any{}, nil)
			return err
		}},
		{"GetLinks", func() error {
			_, err := p.GetLinks(ctx, "x", "Supplies", "outbound", nil)
			return err
		}},
		{"Traverse", func() error {
			_, err := p.Traverse(ctx, "x", spi.TraversalPath{}, nil)
			return err
		}},
		{"BeginTransaction", func() error {
			_, err := p.BeginTransaction(ctx)
			return err
		}},
		// Phase 3 (U2): GetObjectAtVersion/GetObjectAtTime implemented —
		// removed from the ErrUnimplemented floor. Positive coverage lives
		// in runtime/storage/memory/provider_version_test.go.
		{"EnsureIndex", func() error {
			return p.EnsureIndex(ctx, "Supplier", spi.IndexDefinition{})
		}},
		{"DropIndex", func() error {
			return p.DropIndex(ctx, "Supplier", "name")
		}},
		{"ListIndexes", func() error {
			_, err := p.ListIndexes(ctx, "Supplier")
			return err
		}},
	}
	for _, c := range cases {
		err := c.fn()
		if !errors.Is(err, spi.ErrUnimplemented) {
			t.Errorf("%s err = %v, want errors.Is ErrUnimplemented (AE8)", c.name, err)
			continue
		}
		// The error message must carry the method name so callers
		// can see which SPI method is the floor.
		if !strings.Contains(err.Error(), c.name) {
			t.Errorf("%s err = %q, want message to contain method name (AE8 message-name contract)", c.name, err.Error())
		}
	}
}
