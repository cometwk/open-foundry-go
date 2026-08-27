package sqliteobda_test

import (
	"testing"
	"time"

	"github.com/openfoundry/runtime/spi"
)

func TestTransactionRollbackHidesWrites(t *testing.T) {
	p, _, patientID, wardID := activateHospital(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.GetObject(ctx, "Patient", patientID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := p.BeginTransaction(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.UpdateObject("Patient", patientID, map[string]any{"name": "Rolled"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.CreateLink("AdmittedTo", patientID, wardID, nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	got, err := p.GetObject(ctx, "Patient", patientID)
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != created["name"] {
		t.Fatalf("update leaked after rollback: %v", got["name"])
	}
	page, err := p.GetLinks(ctx, patientID, "AdmittedTo", "outbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("link leaked after rollback: %d", len(page.Items))
	}
	if _, err := tx.CreateObject("Patient", map[string]any{"name": "X"}); err == nil {
		t.Fatal("expected closed tx to fail")
	}
}

func TestTransactionCommitVisible(t *testing.T) {
	p, _, patientID, wardID := activateHospital(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	tx, err := p.BeginTransaction(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.UpdateObject("Patient", patientID, map[string]any{"name": "Committed"}, nil); err != nil {
		t.Fatal(err)
	}
	link, err := tx.CreateLink("AdmittedTo", patientID, wardID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := p.GetObject(ctx, "Patient", patientID)
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "Committed" {
		t.Fatalf("name=%v", got["name"])
	}
	if _, err := p.GetLink(ctx, "AdmittedTo", link[spi.FieldID].(string)); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentGetDuringTx(t *testing.T) {
	p, _ := activatePatient(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	created, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := p.BeginTransaction(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	done := make(chan error, 1)
	go func() {
		_, err := p.GetObject(ctx, "Patient", created[spi.FieldID].(string))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("GetObject blocked while transaction open")
	}
	st, err := p.HealthCheck()
	if err != nil {
		t.Fatal(err)
	}
	if st.Provider != "sqliteobda" {
		t.Fatalf("%+v", st)
	}
}
