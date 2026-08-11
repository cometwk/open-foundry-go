package memory_test

import (
	"testing"

	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/projection"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

func TestApplyGetRoundTrip(t *testing.T) {
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatal(err)
	}
	onto, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	schema := projection.ProjectStorage(onto)
	p := memory.New()
	ctx := spi.RequestContext{TenantID: "t1"}

	res, err := p.ApplySchema(ctx, schema)
	if err != nil || !res.Success || res.FromVersion != 0 || res.ToVersion != 1 {
		t.Fatalf("apply: %+v err=%v", res, err)
	}

	got, err := p.GetSchema(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ObjectTypes) != len(schema.ObjectTypes) || len(got.LinkTypes) != len(schema.LinkTypes) {
		t.Fatalf("got objects=%d links=%d want %d %d", len(got.ObjectTypes), len(got.LinkTypes), len(schema.ObjectTypes), len(schema.LinkTypes))
	}

	// Mutation isolation
	got.ObjectTypes[0].Name = "MUTATED"
	got2, err := p.GetSchema(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got2.ObjectTypes[0].Name == "MUTATED" {
		t.Fatal("getSchema should return a clone")
	}
}

func TestGetSchemaMissingVersion(t *testing.T) {
	p := memory.New()
	v := 99
	_, err := p.GetSchema(spi.RequestContext{TenantID: "t"}, &v)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReapplySameVersion(t *testing.T) {
	p := memory.New()
	ctx := spi.RequestContext{TenantID: "t"}
	schema := spi.OntologySchema{Version: 1, ObjectTypes: []spi.ObjectTypeDefinition{{Name: "A", Properties: nil}}}
	if _, err := p.ApplySchema(ctx, schema); err != nil {
		t.Fatal(err)
	}
	res, err := p.ApplySchema(ctx, schema)
	if err != nil || !res.Success || res.ToVersion != 1 {
		t.Fatalf("%+v %v", res, err)
	}
}
