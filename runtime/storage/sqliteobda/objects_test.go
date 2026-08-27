package sqliteobda_test

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/sqliteobda"
)

func TestCreateGetSystemFieldsStable(t *testing.T) {
	p, _ := activatePatient(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada"})
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
	got, err := p.GetObject(ctx, "Patient", id)
	if err != nil {
		t.Fatal(err)
	}
	again, err := p.GetObject(ctx, "Patient", id)
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
	raw := testdata(t, "patient_bool.obda.yaml")
	p, db := openProvider(t, raw)
	schema := patientSchema()
	schema.ObjectTypes[0].Properties = append(schema.ObjectTypes[0].Properties, spi.PropertyDefinition{Name: "active", Type: "Boolean"})
	mustInit(t, db, raw, schema)
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, schema); err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada", "active": true})
	if err != nil {
		t.Fatal(err)
	}
	id := created[spi.FieldID].(string)
	got, err := p.GetObject(ctx, "Patient", id)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := got["active"].(bool)
	if !ok || !v {
		t.Fatalf("active=%T %#v", got["active"], got["active"])
	}
}

func TestOCCConflict(t *testing.T) {
	p, _ := activatePatient(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada"})
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
			_, err := p.UpdateObject(ctx, "Patient", id, map[string]any{"name": "X"}, &exp)
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
	p, _ := activatePatient(t)
	created, err := p.CreateObject(spi.RequestContext{TenantID: "t1"}, "Patient", map[string]any{"name": "Ada"})
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
	raw := testdata(t, "patient_read.obda.yaml")
	p, db := openProvider(t, raw)
	mustInit(t, db, raw, patientSchema())
	mustExec(t, db, `INSERT INTO patient (id, tenant_id, patient_name, version, created_at, updated_at) VALUES ('p1','t1','Ada', 1, 't', 't')`)
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, patientSchema()); err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	page, err := p.QueryObjects(ctx, "Patient", spi.FilterExpression{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items=%d", len(page.Items))
	}
	_, err = p.CreateObject(ctx, "Patient", map[string]any{"name": "Bob"})
	if !errors.Is(err, spi.ErrReadOnlyMapping) {
		t.Fatalf("err=%v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM patient`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("wrote through read-only: n=%d", n)
	}
}

func TestSoftDeleteGetAndQuery(t *testing.T) {
	p, _ := activatePatient(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	id := created[spi.FieldID].(string)
	if err := p.DeleteObject(ctx, "Patient", id, "soft"); err != nil {
		t.Fatal(err)
	}
	got, err := p.GetObject(ctx, "Patient", id)
	if err != nil {
		t.Fatal(err)
	}
	if got[spi.FieldDeletedAt] == nil || got[spi.FieldDeletedAt] == "" {
		t.Fatalf("missing deletedAt: %#v", got)
	}
	page, err := p.QueryObjects(ctx, "Patient", spi.FilterExpression{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("default query included deleted: %+v", page.Items)
	}
	page, err = p.QueryObjects(ctx, "Patient", spi.FilterExpression{}, &spi.QueryOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("includeDeleted items=%d", len(page.Items))
	}
}

func TestHardDeleteRemovesRow(t *testing.T) {
	p, _ := activatePatient(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	id := created[spi.FieldID].(string)
	if err := p.DeleteObject(ctx, "Patient", id, "hard"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GetObject(ctx, "Patient", id); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v", err)
	}
	page, err := p.QueryObjects(ctx, "Patient", spi.FilterExpression{}, &spi.QueryOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("hard-deleted still visible: %+v", page.Items)
	}
}

func TestEmptyTenantRejected(t *testing.T) {
	p, _ := activatePatient(t)
	_, err := p.CreateObject(spi.RequestContext{}, "Patient", map[string]any{"name": "Ada"})
	if !errors.Is(err, spi.ErrTenantRequired) {
		t.Fatalf("err=%v", err)
	}
}

func TestTenantOverrideIgnored(t *testing.T) {
	p, db := activatePatient(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Patient", map[string]any{
		"name":            "Ada",
		spi.FieldTenantID: "INTRUDER",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created[spi.FieldTenantID] != "t1" {
		t.Fatalf("tenant=%v", created[spi.FieldTenantID])
	}
	var stored string
	if err := db.QueryRow(`SELECT tenant_id FROM patient WHERE id = ?`, created[spi.FieldID]).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "t1" {
		t.Fatalf("stored tenant=%s", stored)
	}
	_, err = p.GetObject(spi.RequestContext{TenantID: "INTRUDER"}, "Patient", created[spi.FieldID].(string))
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestSplitBrainNotFound(t *testing.T) {
	p, db := activatePatient(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	id := created[spi.FieldID].(string)
	mustExec(t, db, `DELETE FROM patient WHERE id = ?`, id)
	if _, err := p.GetObject(ctx, "Patient", id); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("deleted row err=%v", err)
	}
	if _, err := p.GetObject(ctx, "Patient", obda.EncodeDirect("Ward", []string{"x"})); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("wrong type err=%v", err)
	}
}

func TestSoftDeletedCreateStillAllowsNewRow(t *testing.T) {
	p, _ := activatePatient(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteObject(ctx, "Patient", created[spi.FieldID].(string), "soft"); err != nil {
		t.Fatal(err)
	}
	again, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada2"})
	if err != nil {
		t.Fatal(err)
	}
	if again[spi.FieldID] == created[spi.FieldID] {
		t.Fatal("expected a new generated id")
	}
}

func TestSoftDeletedUpdateNotFound(t *testing.T) {
	p, _ := activatePatient(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	id := created[spi.FieldID].(string)
	if err := p.DeleteObject(ctx, "Patient", id, "soft"); err != nil {
		t.Fatal(err)
	}
	exp := 1
	_, err = p.UpdateObject(ctx, "Patient", id, map[string]any{"name": "X"}, &exp)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v", err)
	}
	if errors.Is(err, spi.ErrVersionConflict) {
		t.Fatal("soft-deleted update must not be version conflict")
	}
}

func TestDirectIdentityRoundTrip(t *testing.T) {
	p, _ := activatePatient(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	id := created[spi.FieldID].(string)
	typ, keys, err := obda.DecodeDirect(id)
	if err != nil || typ != "Patient" || len(keys) != 1 || keys[0] == "" {
		t.Fatalf("id=%q typ=%q keys=%v err=%v", id, typ, keys, err)
	}
	got, err := p.GetObject(ctx, "Patient", id)
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "Ada" {
		t.Fatalf("%#v", got)
	}
}

func TestWrongTypeIDNotFound(t *testing.T) {
	p, _ := activatePatient(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	_, keys, err := obda.DecodeDirect(created[spi.FieldID].(string))
	if err != nil {
		t.Fatal(err)
	}
	err = pGetErr(t, p, "t1", obda.EncodeDirect("Ward", keys))
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("wrong type err=%v", err)
	}
	err = pGetErr(t, p, "t1", "not-an-encoded-id")
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("garbage id err=%v", err)
	}
}

func TestOmitDeletedAtRejectsSoftDelete(t *testing.T) {
	raw := []byte(strings.Replace(string(testdata(t, "patient.obda.yaml")), "strategy: native", "strategy: native\n      omit: [deletedAt]", 1))
	p, db := openProvider(t, raw)
	mustInit(t, db, raw, patientSchema())
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, patientSchema()); err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	err = p.DeleteObject(ctx, "Patient", created[spi.FieldID].(string), "soft")
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("err=%v", err)
	}
}

func TestOmitVersionRejectsExpectedVersion(t *testing.T) {
	raw := []byte(strings.Replace(string(testdata(t, "patient.obda.yaml")), "strategy: native", "strategy: native\n      omit: [version]", 1))
	p, db := openProvider(t, raw)
	mustInit(t, db, raw, patientSchema())
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, patientSchema()); err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	exp := 1
	_, err = p.UpdateObject(ctx, "Patient", created[spi.FieldID].(string), map[string]any{"name": "X"}, &exp)
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("err=%v", err)
	}
}

func pGetErr(t *testing.T, p *sqliteobda.Provider, tenant, id string) error {
	t.Helper()
	_, err := p.GetObject(spi.RequestContext{TenantID: tenant}, "Patient", id)
	return err
}

func activatePatient(t *testing.T) (*sqliteobda.Provider, *sql.DB) {
	t.Helper()
	raw := testdata(t, "patient.obda.yaml")
	p, db := openProvider(t, raw)
	mustInit(t, db, raw, patientSchema())
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, patientSchema()); err != nil {
		t.Fatal(err)
	}
	return p, db
}
