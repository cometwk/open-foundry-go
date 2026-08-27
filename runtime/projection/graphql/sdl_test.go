package graphql_test

import (
	"strings"
	"testing"

	graphqlgo "github.com/graph-gophers/graphql-go"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/pack"
	projgql "github.com/openfoundry/runtime/projection/graphql"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

func TestGenerate_SupplyChain_SearchAndNoWriteSurface(t *testing.T) {
	o := loadSupplyChain(t)
	sdl := projgql.Generate(o, memory.New().Capabilities())

	if _, err := graphqlgo.ParseSchema(sdl, nil); err != nil {
		t.Fatalf("ParseSchema(supply-chain SDL, nil) err = %v", err)
	}
	for _, want := range []string{
		"searchProducts(",
		"searchSuppliers(",
		"searchFacilitys(",
		"  product(id: ID!): Product",
		"  products(",
		"  productAggregate(",
		"  suppliers: [Supplier!]!",
		"  supplier: Supplier",
	} {
		if !strings.Contains(sdl, want) {
			t.Errorf("SDL missing %q", want)
		}
	}
	for _, forbid := range []string{
		"searchAll",
		"typeahead",
		"type Mutation",
		"type Subscription",
		"_redactedFields",
		"_consentRestricted",
		"SupplierFilter",
	} {
		if forbid == "SupplierFilter" {
			// ProductFilter is fine; PurchaseOrderFilter must not contain `supplier: SupplierFilter`.
			if strings.Contains(sdl, "supplier: SupplierFilter") {
				t.Errorf("SDL contains nested object filter field %q (KTD-15)", "supplier: SupplierFilter")
			}
			continue
		}
		if strings.Contains(sdl, forbid) {
			t.Errorf("SDL contains forbidden %q", forbid)
		}
	}
}

func TestGenerate_FTSOff_OmitsSearch(t *testing.T) {
	o := loadSupplyChain(t)
	sdl := projgql.Generate(o, spi.StorageCapabilities{SupportsFullTextSearch: false})
	if strings.Contains(sdl, "searchProducts") {
		t.Fatal("SDL with FTS off still has searchProducts")
	}
	if strings.Contains(sdl, "type SearchResult_Product") {
		t.Fatal("SDL with FTS off still has SearchResult_Product")
	}
	if _, err := graphqlgo.ParseSchema(sdl, nil); err != nil {
		t.Fatalf("ParseSchema(FTS off) err = %v", err)
	}
}

func TestGenerate_EveryObjectHasGetListAggregate(t *testing.T) {
	o := loadSupplyChain(t)
	sdl := projgql.Generate(o, memory.New().Capabilities())
	for _, obj := range o.Objects {
		lower := projgql.LowerFirst(obj.Name)
		for _, want := range []string{
			"  " + lower + "(id: ID!): " + obj.Name,
			"  " + lower + "s(",
			"  " + lower + "Aggregate(",
		} {
			if !strings.Contains(sdl, want) {
				t.Errorf("type %s missing query field containing %q", obj.Name, want)
			}
		}
	}
}

func TestLowerFirst(t *testing.T) {
	if got := projgql.LowerFirst("InventoryRecord"); got != "inventoryRecord" {
		t.Fatalf("LowerFirst(InventoryRecord) = %q", got)
	}
	if got := projgql.LowerFirst("Product"); got != "product" {
		t.Fatalf("LowerFirst(Product) = %q", got)
	}
}

func loadSupplyChain(t *testing.T) *ir.Ontology {
	t.Helper()
	dir, err := pack.SupplyChainDir()
	if err != nil {
		t.Fatalf("SupplyChainDir err = %v", err)
	}
	o, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir err = %v", err)
	}
	return o
}
