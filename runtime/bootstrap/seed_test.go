package bootstrap_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openfoundry/runtime/bootstrap"
	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/projection"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

func TestLoadPackSeeds_LibraryDemo(t *testing.T) {
	dir, err := pack.LibraryPackDir()
	if err != nil {
		t.Fatal(err)
	}
	seeds, err := bootstrap.LoadPackSeeds(dir)
	if err != nil {
		t.Fatalf("LoadPackSeeds: %v", err)
	}
	if len(seeds) != 1 {
		t.Fatalf("manifests=%d, want 1", len(seeds))
	}
	seed := seeds[0]
	if seed.PackName != "library" {
		t.Fatalf("packName=%q, want library", seed.PackName)
	}
	if len(seed.Objects) != 14 {
		t.Fatalf("objects=%d, want 14", len(seed.Objects))
	}
	if len(seed.Links) != 21 {
		t.Fatalf("links=%d, want 21", len(seed.Links))
	}

	byRef := map[string]bootstrap.SeedObject{}
	for _, obj := range seed.Objects {
		byRef[obj.Ref] = obj
	}
	sapiens, ok := byRef["book_sapiens"]
	if !ok || sapiens.Type != "Book" {
		t.Fatalf("book_sapiens = %+v", sapiens)
	}
	if sapiens.Fields["title"] != "人类简史" {
		t.Fatalf("sapiens fields = %#v", sapiens.Fields)
	}
	hong, ok := byRef["xiao_hong"]
	if !ok || hong.Type != "Reader" {
		t.Fatalf("xiao_hong = %+v", hong)
	}
	if hong.Fields["membershipLevel"] != "BASIC" {
		t.Fatalf("xiao_hong fields = %#v", hong.Fields)
	}

	var registered, borrows, available int
	for _, link := range seed.Links {
		switch link.Type {
		case "RegisteredAt":
			registered++
		case "Borrows":
			borrows++
		case "AvailableAt":
			available++
		default:
			t.Fatalf("unexpected simplified link %s", link.Type)
		}
	}
	if registered != 4 || borrows != 6 || available != 11 {
		t.Fatalf("links registered=%d borrows=%d available=%d", registered, borrows, available)
	}
}

func TestLoadPackSeeds_OmitsSeedKey(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":         "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\n",
		"schema/models.odl": widgetODL,
	})
	got, err := bootstrap.LoadPackSeeds(dir)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
}

func TestLoadPackSeeds_MissingFileSkipped(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":         "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\nseed:\n  - seeds/missing.yaml\n",
		"schema/models.odl": widgetODL,
	})
	got, err := bootstrap.LoadPackSeeds(dir)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("got %#v, want nil (missing seed file is skipped)", got)
	}
}

func TestLoadPackSeeds_InvalidYAMLSkipped(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":         "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\nseed:\n  - seeds/bad.yaml\n  - seeds/ok.yaml\n",
		"schema/models.odl": widgetODL,
		"seeds/bad.yaml":    "- just a list\n",
		"seeds/ok.yaml": `objects:
  - type: Widget
    ref: root
    fields:
      name: Root
links:
  - type: BelongsTo
    from: child
    to: root
`,
	})
	got, err := bootstrap.LoadPackSeeds(dir)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(got) != 1 {
		t.Fatalf("manifests=%d, want 1 (invalid file skipped)", len(got))
	}
	if got[0].PackName != "fixture" || len(got[0].Objects) != 1 || len(got[0].Links) != 1 {
		t.Fatalf("got %#v", got[0])
	}
	if got[0].Objects[0].Ref != "root" || got[0].Links[0].From != "child" {
		t.Fatalf("got %#v", got[0])
	}
}

func TestLoadPackSeeds_SkipsInvalidEntries(t *testing.T) {
	dir := writePack(t, map[string]string{
		"pack.yaml":         "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\nseed:\n  - seeds/mixed.yaml\n",
		"schema/models.odl": widgetODL,
		"seeds/mixed.yaml": `objects:
  - not-an-object
  - type: 1
  - type: Widget
    ref: keep-me
    fields:
      name: Keep
  - type: Widget
    fields: []
links:
  - type: BelongsTo
    from: keep-me
  - type: BelongsTo
    from: keep-me
    to: other
`,
	})
	got, err := bootstrap.LoadPackSeeds(dir)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(got) != 1 {
		t.Fatalf("manifests=%d, want 1", len(got))
	}
	if len(got[0].Objects) != 2 {
		t.Fatalf("objects=%d, want 2 valid", len(got[0].Objects))
	}
	if got[0].Objects[0].Ref != "keep-me" {
		t.Fatalf("first valid object = %+v", got[0].Objects[0])
	}
	if got[0].Objects[1].Ref != "" || got[0].Objects[1].Fields == nil {
		t.Fatalf("object with empty fields = %+v", got[0].Objects[1])
	}
	if len(got[0].Links) != 1 || got[0].Links[0].To != "other" {
		t.Fatalf("links = %#v", got[0].Links)
	}
}

func TestApplySeeds_LibraryDemoIdempotent(t *testing.T) {
	dir, err := pack.LibraryPackDir()
	if err != nil {
		t.Fatal(err)
	}
	onto, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	seeds, err := bootstrap.LoadPackSeeds(dir)
	if err != nil {
		t.Fatalf("LoadPackSeeds: %v", err)
	}
	store := memory.New()
	if _, err := store.ApplySchema(spi.RequestContext{TenantID: "default"}, projection.ProjectStorage(onto)); err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(store, onto)
	if err != nil {
		t.Fatal(err)
	}
	ctx := bootstrap.SeedContext("default")
	first, err := bootstrap.ApplySeeds(eng, seeds, ctx)
	if err != nil {
		t.Fatalf("ApplySeeds: %v", err)
	}
	if first.CreatedObjects != 14 || first.CreatedLinks != 21 || first.SkippedObjects != 0 {
		t.Fatalf("first apply = %+v, want 14 objects + 21 links", first)
	}

	second, err := bootstrap.ApplySeeds(eng, seeds, ctx)
	if err != nil {
		t.Fatalf("ApplySeeds rerun: %v", err)
	}
	if second.CreatedObjects != 0 || second.SkippedObjects != 14 {
		t.Fatalf("second apply = %+v, want 14 skipped objects", second)
	}

	books, err := eng.QueryObjects(ctx, "Book", spi.FilterExpression{
		Field: "title", Operator: "eq", Value: "人类简史",
	}, &spi.QueryOptions{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if books.TotalCount != 1 {
		t.Fatalf("人类简史 count=%d, want 1", books.TotalCount)
	}
	readers, err := eng.QueryObjects(ctx, "Reader", spi.FilterExpression{
		Field: "membershipLevel", Operator: "eq", Value: "BASIC",
	}, &spi.QueryOptions{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if readers.TotalCount != 1 {
		t.Fatalf("BASIC reader count=%d, want 1", readers.TotalCount)
	}

	other := spi.RequestContext{TenantID: "other", ActorID: "boot"}
	hidden, err := eng.QueryObjects(other, "Book", spi.FilterExpression{
		Field: "title", Operator: "eq", Value: "人类简史",
	}, &spi.QueryOptions{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if hidden.TotalCount != 0 {
		t.Fatalf("cross-tenant 人类简史 count=%d, want 0", hidden.TotalCount)
	}
}

func TestSeedContext_BlankIsSystem(t *testing.T) {
	ctx := bootstrap.SeedContext("  ")
	if ctx.TenantID != bootstrap.DefaultSeedTenant || ctx.ActorID != bootstrap.SeedActorID {
		t.Fatalf("ctx=%+v", ctx)
	}
	ctx = bootstrap.SeedContext("default")
	if ctx.TenantID != "default" {
		t.Fatalf("tenant=%q", ctx.TenantID)
	}
}

func TestLoadPackSeeds_MissingManifest(t *testing.T) {
	_, err := bootstrap.LoadPackSeeds(filepath.Join(os.TempDir(), "no-such-pack"))
	if err == nil {
		t.Fatal("err = nil, want missing pack.yaml")
	}
}
