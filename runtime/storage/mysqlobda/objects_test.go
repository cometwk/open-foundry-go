package mysqlobda_test

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/mysqlobda"
)

func TestCreateObjectHonorsEngineObjectID(t *testing.T) {
	p, db := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	want := "engine-object-1"
	created, err := p.CreateObject(ctx, "Reader", map[string]any{
		"name":                  "Xiao Ming",
		spi.FieldEngineObjectID: want,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created[spi.FieldID] != want {
		t.Fatalf("_id=%v want %s", created[spi.FieldID], want)
	}
	if _, ok := created[spi.FieldEngineObjectID]; ok {
		t.Fatalf("leaked %s", spi.FieldEngineObjectID)
	}
	var stored string
	if err := db.QueryRow(`SELECT id FROM reader WHERE id = ?`, want).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != want {
		t.Fatalf("column=%s", stored)
	}
}

func TestCreateGetSystemFieldsStable(t *testing.T) {
	p, _ := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := created[spi.FieldID].(string)
	if id == "" {
		t.Fatalf("missing id: %#v", created)
	}
	if created[spi.FieldVersion] != 1 {
		t.Fatalf("version=%v", created[spi.FieldVersion])
	}
	got, err := p.GetObject(ctx, "Reader", id)
	if err != nil {
		t.Fatal(err)
	}
	again, err := p.GetObject(ctx, "Reader", id)
	if err != nil {
		t.Fatal(err)
	}
	if got[spi.FieldID] != created[spi.FieldID] || again[spi.FieldID] != got[spi.FieldID] {
		t.Fatalf("id drifted: create=%v get=%v again=%v", created[spi.FieldID], got[spi.FieldID], again[spi.FieldID])
	}
	if got[spi.FieldVersion] != created[spi.FieldVersion] || again[spi.FieldCreatedAt] != got[spi.FieldCreatedAt] {
		t.Fatalf("system fields drifted: %#v vs %#v", got, again)
	}
}

func TestBooleanIsGoBool(t *testing.T) {
	raw := testdata(t, "library_bool.obda.yaml")
	p, db := openProvider(t, raw)
	mustInit(t, db, raw, branchSchema())
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, branchSchema()); err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Branch", map[string]any{"name": "Central", "allowInterLibraryLoan": true})
	if err != nil {
		t.Fatal(err)
	}
	id := created[spi.FieldID].(string)
	got, err := p.GetObject(ctx, "Branch", id)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := got["allowInterLibraryLoan"].(bool)
	if !ok || !v {
		t.Fatalf("allowInterLibraryLoan=%T %#v", got["allowInterLibraryLoan"], got["allowInterLibraryLoan"])
	}
}

func TestOCCConflict(t *testing.T) {
	p, _ := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"})
	if err != nil {
		t.Fatal(err)
	}
	id := created[spi.FieldID].(string)
	exp := 1
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.UpdateObject(ctx, "Reader", id, map[string]any{"name": "Xiao Hong"}, &exp)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	var okN, conflictN int
	for err := range errs {
		if err == nil {
			okN++
			continue
		}
		if errors.Is(err, spi.ErrVersionConflict) {
			conflictN++
			continue
		}
		t.Fatalf("unexpected %v", err)
	}
	if okN != 1 || conflictN != 1 {
		t.Fatalf("ok=%d conflict=%d", okN, conflictN)
	}
}

func TestCrossTenantGetIsNotFound(t *testing.T) {
	p, _ := activateReader(t)
	created, err := p.CreateObject(spi.RequestContext{TenantID: "t1"}, "Reader", map[string]any{"name": "Xiao Ming"})
	if err != nil {
		t.Fatal(err)
	}
	id := created[spi.FieldID].(string)
	errA := pGetErr(t, p, "t2", id)
	errB := pGetErr(t, p, "t1", "missing")
	if !errors.Is(errA, spi.ErrObjectNotFound) || !errors.Is(errB, spi.ErrObjectNotFound) {
		t.Fatalf("a=%v b=%v", errA, errB)
	}
	if errA.Error() != errB.Error() {
		t.Fatalf("distinguishable: %q vs %q", errA, errB)
	}
}

func TestReadOnlyMapping(t *testing.T) {
	raw := testdata(t, "library_read.obda.yaml")
	p, db := openProvider(t, raw)
	mustInit(t, db, raw, readerSchema())
	mustExec(t, db, `INSERT INTO reader (id, tenant_id, name, version, created_at, updated_at) VALUES ('r1','t1','Xiao Ming', 1, 't', 't')`)
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, readerSchema()); err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	page, err := p.QueryObjects(ctx, "Reader", spi.FilterExpression{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items=%d", len(page.Items))
	}
	_, err = p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Hong"})
	if !errors.Is(err, spi.ErrReadOnlyMapping) {
		t.Fatalf("err=%v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM reader`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("wrote through read-only: n=%d", n)
	}
}

func TestSoftDeleteGetAndQuery(t *testing.T) {
	p, _ := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"})
	if err != nil {
		t.Fatal(err)
	}
	id := created[spi.FieldID].(string)
	if err := p.DeleteObject(ctx, "Reader", id, "soft"); err != nil {
		t.Fatal(err)
	}
	got, err := p.GetObject(ctx, "Reader", id)
	if err != nil {
		t.Fatal(err)
	}
	if got[spi.FieldDeletedAt] == nil || got[spi.FieldDeletedAt] == "" {
		t.Fatalf("missing deletedAt: %#v", got)
	}
	page, err := p.QueryObjects(ctx, "Reader", spi.FilterExpression{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("default query included deleted: %+v", page.Items)
	}
	page, err = p.QueryObjects(ctx, "Reader", spi.FilterExpression{}, &spi.QueryOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("includeDeleted items=%d", len(page.Items))
	}
}

func TestHardDeleteRemovesRow(t *testing.T) {
	p, _ := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"})
	if err != nil {
		t.Fatal(err)
	}
	id := created[spi.FieldID].(string)
	if err := p.DeleteObject(ctx, "Reader", id, "hard"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GetObject(ctx, "Reader", id); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v", err)
	}
	page, err := p.QueryObjects(ctx, "Reader", spi.FilterExpression{}, &spi.QueryOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("hard-deleted still visible: %+v", page.Items)
	}
}

func TestEmptyTenantRejected(t *testing.T) {
	p, _ := activateReader(t)
	_, err := p.CreateObject(spi.RequestContext{}, "Reader", map[string]any{"name": "Xiao Ming"})
	if !errors.Is(err, spi.ErrTenantRequired) {
		t.Fatalf("err=%v", err)
	}
}

func TestTenantOverrideIgnored(t *testing.T) {
	p, db := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Reader", map[string]any{
		"name":            "Xiao Ming",
		spi.FieldTenantID: "INTRUDER",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created[spi.FieldTenantID] != "t1" {
		t.Fatalf("tenant=%v", created[spi.FieldTenantID])
	}
	var stored string
	if err := db.QueryRow(`SELECT tenant_id FROM reader WHERE id = ?`, created[spi.FieldID]).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "t1" {
		t.Fatalf("stored tenant=%s", stored)
	}
	_, err = p.GetObject(spi.RequestContext{TenantID: "INTRUDER"}, "Reader", created[spi.FieldID].(string))
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestSplitBrainNotFound(t *testing.T) {
	p, db := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"})
	if err != nil {
		t.Fatal(err)
	}
	id := created[spi.FieldID].(string)
	mustExec(t, db, `DELETE FROM reader WHERE id = ?`, id)
	if _, err := p.GetObject(ctx, "Reader", id); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("deleted row err=%v", err)
	}
	if _, err := p.GetObject(ctx, "Reader", "not-a-reader-row"); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("wrong id err=%v", err)
	}
}

func TestSoftDeletedCreateStillAllowsNewRow(t *testing.T) {
	p, _ := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteObject(ctx, "Reader", created[spi.FieldID].(string), "soft"); err != nil {
		t.Fatal(err)
	}
	again, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Hong"})
	if err != nil {
		t.Fatal(err)
	}
	if again[spi.FieldID] == created[spi.FieldID] {
		t.Fatal("expected a new generated id")
	}
}

func TestSoftDeletedUpdateNotFound(t *testing.T) {
	p, _ := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"})
	if err != nil {
		t.Fatal(err)
	}
	id := created[spi.FieldID].(string)
	if err := p.DeleteObject(ctx, "Reader", id, "soft"); err != nil {
		t.Fatal(err)
	}
	exp := 1
	_, err = p.UpdateObject(ctx, "Reader", id, map[string]any{"name": "Xiao Hong"}, &exp)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v", err)
	}
	if errors.Is(err, spi.ErrVersionConflict) {
		t.Fatal("soft-deleted update must not be version conflict")
	}
}

func TestDirectIdentityRoundTrip(t *testing.T) {
	p, _ := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"})
	if err != nil {
		t.Fatal(err)
	}
	id := created[spi.FieldID].(string)
	if len(id) != 36 || id[14] != '7' {
		t.Fatalf("id=%q want UUIDv7", id)
	}
	got, err := p.GetObject(ctx, "Reader", id)
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "Xiao Ming" {
		t.Fatalf("%#v", got)
	}
}

func TestWrongTypeIDNotFound(t *testing.T) {
	p, _ := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"}); err != nil {
		t.Fatal(err)
	}
	err := pGetErr(t, p, "t1", "00000000-0000-7000-0000-000000000000")
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("wrong id err=%v", err)
	}
	err = pGetErr(t, p, "t1", "not-an-id")
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("garbage id err=%v", err)
	}
}

func TestOmitDeletedAtRejectsSoftDelete(t *testing.T) {
	raw := []byte(strings.Replace(string(testdata(t, "library.obda.yaml")), "strategy: native", "strategy: native\n      omit: [deletedAt]", 1))
	p, db := openProvider(t, raw)
	mustInit(t, db, raw, readerSchema())
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, readerSchema()); err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"})
	if err != nil {
		t.Fatal(err)
	}
	err = p.DeleteObject(ctx, "Reader", created[spi.FieldID].(string), "soft")
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("err=%v", err)
	}
}

func TestOmitVersionRejectsExpectedVersion(t *testing.T) {
	raw := []byte(strings.Replace(string(testdata(t, "library.obda.yaml")), "strategy: native", "strategy: native\n      omit: [version]", 1))
	p, db := openProvider(t, raw)
	mustInit(t, db, raw, readerSchema())
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, readerSchema()); err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"})
	if err != nil {
		t.Fatal(err)
	}
	exp := 1
	_, err = p.UpdateObject(ctx, "Reader", created[spi.FieldID].(string), map[string]any{"name": "Xiao Hong"}, &exp)
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("err=%v", err)
	}
}

func pGetErr(t *testing.T, p *mysqlobda.Provider, tenant, id string) error {
	t.Helper()
	_, err := p.GetObject(spi.RequestContext{TenantID: tenant}, "Reader", id)
	return err
}

func activateReader(t *testing.T) (*mysqlobda.Provider, *sql.DB) {
	t.Helper()
	raw := testdata(t, "library.obda.yaml")
	p, db := openProvider(t, raw)
	mustInit(t, db, raw, readerSchema())
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, readerSchema()); err != nil {
		t.Fatal(err)
	}
	return p, db
}
