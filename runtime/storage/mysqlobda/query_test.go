package mysqlobda_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/mysqlobda"
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

func TestQueryIDBatchBypassesPageCap(t *testing.T) {
	p, db := activateReader(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	first, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "r0"})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{first[spi.FieldID].(string)}
	var createdAt string
	if err := db.QueryRow(`SELECT created_at FROM reader LIMIT 1`).Scan(&createdAt); err != nil {
		t.Fatal(err)
	}
	n := mysqlobda.MaxPageLimit + 1
	const batch = 200
	for i := 1; i < n; {
		var b strings.Builder
		b.WriteString(`INSERT INTO reader (id, tenant_id, name, version, created_at, updated_at) VALUES `)
		args := make([]any, 0, batch*4)
		k := 0
		for ; i < n && k < batch; i, k = i+1, k+1 {
			if k > 0 {
				b.WriteByte(',')
			}
			id := fmt.Sprintf("batch-%d", i)
			ids = append(ids, id)
			b.WriteString(`(?, 't1', ?, 1, ?, ?)`)
			args = append(args, id, id, createdAt, createdAt)
		}
		mustExec(t, db, b.String(), args...)
	}
	ors := make([]spi.FilterExpression, 0, n)
	for _, id := range ids {
		ors = append(ors, spi.FilterExpression{Field: spi.FieldID, Operator: "eq", Value: id})
	}
	all, err := p.QueryObjects(ctx, "Reader", spi.FilterExpression{Or: ors}, &spi.QueryOptions{
		Limit: n, SkipTotalCount: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var stored int
	if err := db.QueryRow(`SELECT COUNT(*) FROM reader WHERE tenant_id = 't1'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if len(all.Items) != n {
		t.Fatalf("id batch items=%d hasNext=%v stored=%d ids=%d, want %d (must not clamp)", len(all.Items), all.HasNextPage, stored, len(ids), n)
	}
	clamped, err := p.QueryObjects(ctx, "Reader", spi.FilterExpression{}, &spi.QueryOptions{Limit: n})
	if err != nil {
		t.Fatal(err)
	}
	if len(clamped.Items) != mysqlobda.MaxPageLimit {
		t.Fatalf("plain page items=%d, want clamped %d", len(clamped.Items), mysqlobda.MaxPageLimit)
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
