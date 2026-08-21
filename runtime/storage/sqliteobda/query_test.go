package sqliteobda_test

import (
	"errors"
	"testing"
	"time"

	"github.com/openfoundry/runtime/spi"
)

func TestQueryLimitZeroMeansHundred(t *testing.T) {
	p, db := activatePatient(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	for i := 0; i < 3; i++ {
		id := string(rune('a' + i))
		if _, err := p.CreateObject(ctx, "Patient", map[string]any{"patientId": id, "name": id}); err != nil {
			t.Fatal(err)
		}
	}
	_ = db
	page, err := p.QueryObjects(ctx, "Patient", spi.FilterExpression{}, &spi.QueryOptions{Limit: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("items=%d (limit 0 should default to 100)", len(page.Items))
	}
	if page.Cursor != "" {
		t.Fatalf("cursor should stay empty: %q", page.Cursor)
	}
}

func TestOrderByInvalidRejected(t *testing.T) {
	p, _ := activatePatient(t)
	_, err := p.QueryObjects(spi.RequestContext{TenantID: "t1"}, "Patient", spi.FilterExpression{}, &spi.QueryOptions{
		OrderBy: []spi.OrderBy{{Field: "patient;drop"}},
	})
	if err == nil {
		t.Fatal("expected reject")
	}
	if errors.Is(err, spi.ErrUnimplemented) {
		t.Fatal("still unimplemented")
	}
}

func TestAsOfTimeNotLiveRow(t *testing.T) {
	p, _ := activatePatient(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateObject(ctx, "Patient", map[string]any{"patientId": "p1", "name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	asOf := time.Now().UTC()
	page, err := p.QueryObjects(ctx, "Patient", spi.FilterExpression{}, &spi.QueryOptions{AsOfTime: &asOf})
	if err == nil && len(page.Items) > 0 {
		t.Fatal("AsOfTime must not return live rows")
	}
	if err != nil && errors.Is(err, spi.ErrUnimplemented) {
		t.Fatal("still unimplemented")
	}
}
