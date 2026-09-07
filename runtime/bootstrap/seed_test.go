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
	if len(seed.Objects) != 3 {
		t.Fatalf("objects=%d, want 3", len(seed.Objects))
	}
	if len(seed.Links) != 2 {
		t.Fatalf("links=%d, want 2", len(seed.Links))
	}

	dune := seed.Objects[0]
	if dune.Type != "Book" || dune.Ref != "book-dune" {
		t.Fatalf("first object = %+v", dune)
	}
	if dune.Fields["title"] != "Dune" || dune.Fields["author"] != "Frank Herbert" {
		t.Fatalf("dune fields = %#v", dune.Fields)
	}
	if dune.Fields["isbn"] != "9780441013593" || dune.Fields["status"] != "AVAILABLE" {
		t.Fatalf("dune fields = %#v", dune.Fields)
	}

	ubik := seed.Objects[1]
	if ubik.Ref != "book-ubik" || ubik.Fields["title"] != "Ubik" || ubik.Fields["status"] != "ON_LOAN" {
		t.Fatalf("second object = %+v", ubik)
	}

	ada := seed.Objects[2]
	if ada.Type != "Member" || ada.Ref != "member-ada" {
		t.Fatalf("third object = %+v", ada)
	}
	if ada.Fields["name"] != "Ada Lovelace" || ada.Fields["memberNumber"] != "M-0001" {
		t.Fatalf("ada fields = %#v", ada.Fields)
	}

	owned := seed.Links[0]
	if owned.Type != "OwnedBy" || owned.From != "book-dune" || owned.To != "member-ada" {
		t.Fatalf("owned link = %+v", owned)
	}
	borrowed := seed.Links[1]
	if borrowed.Type != "BorrowedBy" || borrowed.From != "book-ubik" || borrowed.To != "member-ada" {
		t.Fatalf("borrowed link = %+v", borrowed)
	}
	if borrowed.Fields["borrowedAt"] != "2026-09-01T10:00:00Z" {
		t.Fatalf("borrowedAt = %#v", borrowed.Fields["borrowedAt"])
	}
	if borrowed.Fields["dueAt"] != "2026-09-15T10:00:00Z" {
		t.Fatalf("dueAt = %#v", borrowed.Fields["dueAt"])
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
	if first.CreatedObjects != 3 || first.CreatedLinks != 2 || first.SkippedObjects != 0 {
		t.Fatalf("first apply = %+v, want 3 objects + 2 links", first)
	}

	second, err := bootstrap.ApplySeeds(eng, seeds, ctx)
	if err != nil {
		t.Fatalf("ApplySeeds rerun: %v", err)
	}
	if second.CreatedObjects != 0 || second.SkippedObjects != 3 {
		t.Fatalf("second apply = %+v, want 3 skipped objects", second)
	}

	books, err := eng.QueryObjects(ctx, "Book", spi.FilterExpression{
		Field: "title", Operator: "eq", Value: "Dune",
	}, &spi.QueryOptions{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if books.TotalCount != 1 {
		t.Fatalf("Dune count=%d, want 1", books.TotalCount)
	}
	members, err := eng.QueryObjects(ctx, "Member", spi.FilterExpression{
		Field: "name", Operator: "eq", Value: "Ada Lovelace",
	}, &spi.QueryOptions{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if members.TotalCount != 1 {
		t.Fatalf("Ada count=%d, want 1", members.TotalCount)
	}

	other := spi.RequestContext{TenantID: "other", ActorID: "boot"}
	hidden, err := eng.QueryObjects(other, "Book", spi.FilterExpression{
		Field: "title", Operator: "eq", Value: "Dune",
	}, &spi.QueryOptions{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if hidden.TotalCount != 0 {
		t.Fatalf("cross-tenant Dune count=%d, want 0", hidden.TotalCount)
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
