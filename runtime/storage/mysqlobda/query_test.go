package mysqlobda_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/openfoundry/runtime/spi"
)

func TestQueryLimitZeroMeansHundred(t *testing.T) {
	p, _ := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	for i := 0; i < 3; i++ {
		id := string(rune('a' + i))
		if _, err := p.CreateObject(ctx, "Reader", map[string]any{"name": id}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := p.QueryObjects(ctx, "Reader", spi.FilterExpression{}, &spi.QueryOptions{Limit: 0})
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

func TestQuerySkipTotalCount(t *testing.T) {
	p, _ := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	for i := 0; i < 3; i++ {
		if _, err := p.CreateObject(ctx, "Reader", map[string]any{"name": string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	counted, err := p.QueryObjects(ctx, "Reader", spi.FilterExpression{}, nil)
	if err != nil || counted.TotalCount != 3 {
		t.Fatalf("default TotalCount=%d err=%v, want 3", counted.TotalCount, err)
	}
	skipped, err := p.QueryObjects(ctx, "Reader", spi.FilterExpression{}, &spi.QueryOptions{SkipTotalCount: true})
	if err != nil {
		t.Fatal(err)
	}
	if skipped.TotalCount != 0 || len(skipped.Items) != 3 {
		t.Fatalf("skip TotalCount=%d items=%d", skipped.TotalCount, len(skipped.Items))
	}
}

func TestOrderByInvalidRejected(t *testing.T) {
	p, _ := activateReader(t)
	_, err := p.QueryObjects(spi.RequestContext{TenantID: "t1"}, "Reader", spi.FilterExpression{}, &spi.QueryOptions{
		OrderBy: []spi.OrderBy{{Field: "name;drop"}},
	})
	if err == nil {
		t.Fatal("expected reject")
	}
	if errors.Is(err, spi.ErrUnimplemented) {
		t.Fatal("still unimplemented")
	}
}

func TestAsOfTimeNotLiveRow(t *testing.T) {
	p, _ := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"}); err != nil {
		t.Fatal(err)
	}
	asOf := time.Now().UTC()
	page, err := p.QueryObjects(ctx, "Reader", spi.FilterExpression{}, &spi.QueryOptions{AsOfTime: &asOf})
	if err == nil && len(page.Items) > 0 {
		t.Fatal("AsOfTime must not return live rows")
	}
	if err != nil && errors.Is(err, spi.ErrUnimplemented) {
		t.Fatal("still unimplemented")
	}
}

func TestQueryFilterNonEqRejected(t *testing.T) {
	p, _ := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	// ne must be rejected — it must not be silently compiled as eq (the old bug).
	_, err := p.QueryObjects(ctx, "Reader", spi.FilterExpression{Field: "name", Operator: "ne", Value: "Xiao Ming"}, nil)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("ne filter should return ErrInvalidMapping, got %v", err)
	}
	// And compound must also be rejected.
	_, err = p.QueryObjects(ctx, "Reader", spi.FilterExpression{And: []spi.FilterExpression{
		{Field: "name", Operator: "eq", Value: "Xiao Ming"},
	}}, nil)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("And compound should return ErrInvalidMapping, got %v", err)
	}
}

func TestQueryFilterOrOfIds(t *testing.T) {
	p, _ := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	ids := make([]string, 0, 3)
	for _, name := range []string{"Reader A", "Reader B", "Reader C"} {
		obj, err := p.CreateObject(ctx, "Reader", map[string]any{"name": name})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, fmt.Sprint(obj[spi.FieldID]))
	}
	// Or over _id fetches exactly the requested set — the batch-by-ids shape
	// engine hydration relies on.
	page, err := p.QueryObjects(ctx, "Reader", spi.FilterExpression{Or: []spi.FilterExpression{
		{Field: spi.FieldID, Operator: "eq", Value: ids[0]},
		{Field: spi.FieldID, Operator: "eq", Value: ids[1]},
	}}, &spi.QueryOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, o := range page.Items {
		got[fmt.Sprint(o[spi.FieldID])] = true
	}
	if len(got) != 2 || !got[ids[0]] || !got[ids[1]] || got[ids[2]] {
		t.Fatalf("or filter returned wrong set: %v (want %s,%s; exclude %s)", got, ids[0], ids[1], ids[2])
	}
	// Nested compound inside Or is rejected.
	_, err = p.QueryObjects(ctx, "Reader", spi.FilterExpression{Or: []spi.FilterExpression{
		{Or: []spi.FilterExpression{{Field: "name", Operator: "eq", Value: "Reader A"}}},
	}}, nil)
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("nested Or should return ErrInvalidMapping, got %v", err)
	}
	// Cross-tenant: the or filter stays scoped to ctx tenant.
	other := spi.RequestContext{TenantID: "t2"}
	page, err = p.QueryObjects(other, "Reader", spi.FilterExpression{Or: []spi.FilterExpression{
		{Field: spi.FieldID, Operator: "eq", Value: ids[0]},
	}}, &spi.QueryOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("or filter leaked across tenants: %d items", len(page.Items))
	}
}
