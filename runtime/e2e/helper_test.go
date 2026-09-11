package e2e_test

import (
	"database/sql"
	"net/http/httptest"
	"testing"

	"github.com/openfoundry/runtime/api"
	"github.com/openfoundry/runtime/bootstrap"
	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/internal/testdb"
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/projection"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
	"github.com/openfoundry/runtime/storage/mysqlobda"
)

const (
	backendMemory = "memory"
	backendMySQL  = "mysql"
)

// goldHTTPEnv is the shared GraphQL/REST gold-path fixture: real library
// pack, storage provider (memory or MySQL), seeded simplified ABox, and
// an httptest server.
type goldHTTPEnv struct {
	Backend  string
	PackDir  string
	Ontology *ir.Ontology
	Provider spi.StorageProvider
	Engine   *engine.Engine
	Ctx      spi.RequestContext
	IDs      libraryIDs
	Server   *httptest.Server
}

// setupGoldHTTP loads library-pack, picks memory vs MySQL from TEST_DB_URL,
// applies schema, seeds seeds/simple.yaml, and starts the HTTP API.
func setupGoldHTTP(t *testing.T) goldHTTPEnv {
	t.Helper()

	dir, err := pack.LibraryPackDir()
	if err != nil {
		t.Fatalf("LibraryPackDir err = %v", err)
	}
	onto, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir err = %v", err)
	}
	schema := projection.ProjectStorage(onto)
	ctx := spi.RequestContext{TenantID: "gold", ActorID: "test"}

	backend, provider := openLibraryStorage(t, dir, onto, schema)
	mr, err := provider.ApplySchema(ctx, schema)
	if err != nil || !mr.Success {
		t.Fatalf("ApplySchema (%s) err = %v result = %+v", backend, err, mr)
	}

	eng, err := engine.New(provider, onto)
	if err != nil {
		t.Fatalf("engine.New err = %v", err)
	}
	ids := seedLibrary(t, eng, dir, ctx)

	srv, err := api.New(eng)
	if err != nil {
		t.Fatalf("api.New err = %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	return goldHTTPEnv{
		Backend:  backend,
		PackDir:  dir,
		Ontology: onto,
		Provider: provider,
		Engine:   eng,
		Ctx:      ctx,
		IDs:      ids,
		Server:   ts,
	}
}

// openLibraryStorage returns memory when TEST_DB_URL is unset; otherwise a
// MySQL OBDA provider against the database named in TEST_DB_URL.
func openLibraryStorage(t *testing.T, packDir string, onto *ir.Ontology, schema spi.OntologySchema) (string, spi.StorageProvider) {
	t.Helper()
	if testdb.DSN() == "" {
		return backendMemory, memory.New()
	}

	mappings, err := pack.LoadMappings(packDir, onto)
	if err != nil {
		t.Fatalf("LoadMappings err = %v", err)
	}
	if len(mappings) != 1 {
		t.Fatalf("library mappings = %d, want 1", len(mappings))
	}
	raw := mappings[0].Raw
	db := testdb.Open(t)
	mustInit(t, db, raw, schema)
	p, err := mysqlobda.Open(db, raw, mysqlobda.Options{})
	if err != nil {
		t.Fatalf("mysqlobda.Open err = %v", err)
	}
	return backendMySQL, p
}

func mustInit(t *testing.T, db *sql.DB, mapping []byte, schema spi.OntologySchema) {
	t.Helper()
	if err := mysqlobda.InitMappedSchema(db, compileMapping(t, mapping, schema)); err != nil {
		t.Fatal(err)
	}
}

func compileMapping(t *testing.T, mapping []byte, schema spi.OntologySchema) *obda.Compiled {
	t.Helper()
	doc, err := obda.Parse(mapping)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := obda.Compile(schema, doc)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

type libraryIDs struct {
	xiaoHong                   string
	sapiens, tb1, tb2, quantum string
	west                       string
}

func seedLibrary(t *testing.T, e *engine.Engine, packDir string, ctx spi.RequestContext) libraryIDs {
	t.Helper()
	seeds, err := bootstrap.LoadPackSeeds(packDir)
	if err != nil {
		t.Fatalf("LoadPackSeeds: %v", err)
	}
	result, err := bootstrap.ApplySeeds(e, seeds, ctx)
	if err != nil {
		t.Fatalf("ApplySeeds: %v", err)
	}
	if result.CreatedObjects != 14 || result.CreatedLinks != 21 {
		t.Fatalf("ApplySeeds = %+v, want 14 objects + 21 links (simplified seed)", result)
	}
	return libraryIDs{
		xiaoHong: lookupBy(t, e, ctx, "Reader", "name", "小红"),
		sapiens:  lookupBy(t, e, ctx, "Book", "title", "人类简史"),
		tb1:      lookupBy(t, e, ctx, "Book", "title", "三体·卷1"),
		tb2:      lookupBy(t, e, ctx, "Book", "title", "三体·卷2"),
		quantum:  lookupBy(t, e, ctx, "Book", "title", "量子纠缠导论"),
		west:     lookupBy(t, e, ctx, "Branch", "name", "西区馆"),
	}
}

func lookupBy(t *testing.T, e *engine.Engine, ctx spi.RequestContext, typ, field, value string) string {
	t.Helper()
	page, err := e.QueryObjects(ctx, typ, spi.FilterExpression{
		Field: field, Operator: "eq", Value: value,
	}, &spi.QueryOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalCount != 1 || len(page.Items) != 1 {
		t.Fatalf("%s.%s=%q count=%d items=%d", typ, field, value, page.TotalCount, len(page.Items))
	}
	id, _ := page.Items[0]["_id"].(string)
	if id == "" {
		t.Fatalf("missing id for %s.%s=%q", typ, field, value)
	}
	return id
}
