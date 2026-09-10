package mysqlobda_test

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/mysqlobda"
)

func TestSearchRelevanceAndHighlights(t *testing.T) {
	p := activateSearch(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	mustCreate(t, p, ctx, map[string]any{"name": "clinic clinic clinic", "city": "london"})
	mustCreate(t, p, ctx, map[string]any{"name": "clinic hospital", "city": "paris"})
	mustCreate(t, p, ctx, map[string]any{"name": "banana", "city": "tokyo"})

	res, err := p.SearchObjects(ctx, "Patient", spi.SearchQuery{Query: "clinic"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 2 {
		t.Fatalf("hits=%d want 2", len(res.Hits))
	}
	if res.Hits[0].Score <= res.Hits[1].Score {
		t.Fatalf("expected higher score first: %v then %v", res.Hits[0].Score, res.Hits[1].Score)
	}
	for _, h := range res.Hits {
		if h.Score <= 0 {
			t.Fatalf("score=%v want > 0", h.Score)
		}
		if got := h.Highlights["name"]; len(got) != 1 || got[0] == "" {
			t.Fatalf("name highlights=%v", h.Highlights)
		}
		if _, ok := h.Highlights["city"]; !ok {
			t.Fatalf("city should be highlighted: %v", h.Highlights)
		}
	}
	if _, ok := res.Hits[0].Highlights["score"]; ok {
		t.Fatal("undeclared field must not appear in highlights")
	}
}

func TestSearchBlankQueryEmpty(t *testing.T) {
	p := activateSearch(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	mustCreate(t, p, ctx, map[string]any{"name": "clinic", "city": "london"})
	for _, q := range []string{"", "   "} {
		res, err := p.SearchObjects(ctx, "Patient", spi.SearchQuery{Query: q})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Hits) != 0 || res.TotalCount != 0 {
			t.Fatalf("blank query %q hits=%d total=%d", q, len(res.Hits), res.TotalCount)
		}
	}
	// Blank query is empty even without a searchable mapping (AE2).
	p2, _ := activatePatient(t)
	res, err := p2.SearchObjects(ctx, "Patient", spi.SearchQuery{Query: "   "})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 || res.TotalCount != 0 {
		t.Fatalf("blank on unmapped model hits=%d total=%d", len(res.Hits), res.TotalCount)
	}
}

func TestSearchUnmappedNonEmpty(t *testing.T) {
	p, _ := activatePatient(t)
	_, err := p.SearchObjects(spi.RequestContext{TenantID: "t1"}, "Patient", spi.SearchQuery{Query: "clinic"})
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("err=%v want ErrUnsupportedCapability (AE1)", err)
	}
}

func TestSearchFieldsMustEqualDeclared(t *testing.T) {
	p := activateSearch(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	_, err := p.SearchObjects(ctx, "Patient", spi.SearchQuery{Query: "clinic", Fields: []string{"name"}})
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("subset err=%v want ErrUnsupportedCapability (AE3)", err)
	}
	_, err = p.SearchObjects(ctx, "Patient", spi.SearchQuery{Query: "clinic", Fields: []string{"name", "city", "addr"}})
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("superset err=%v want ErrUnsupportedCapability (AE3)", err)
	}
	_, err = p.SearchObjects(ctx, "Patient", spi.SearchQuery{Query: "clinic", Fields: []string{"nope"}})
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("unknown field err=%v want ErrUnsupportedCapability", err)
	}
	if _, err := p.SearchObjects(ctx, "Patient", spi.SearchQuery{Query: "clinic", Fields: []string{"city", "name"}}); err != nil {
		t.Fatalf("same set different order should pass: %v", err)
	}
}

func TestSearchSoftDeleteAndTenant(t *testing.T) {
	p := activateSearch(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	obj, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "clinic", "city": "london"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteObject(ctx, "Patient", obj[spi.FieldID].(string), "soft"); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, p, spi.RequestContext{TenantID: "t2"}, map[string]any{"name": "clinic", "city": "paris"})
	res, err := p.SearchObjects(ctx, "Patient", spi.SearchQuery{Query: "clinic"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("hits=%d want 0", len(res.Hits))
	}
}

func TestSearchFilterAndPagination(t *testing.T) {
	p := activateSearch(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	mustCreate(t, p, ctx, map[string]any{"name": "clinic", "city": "london"})
	mustCreate(t, p, ctx, map[string]any{"name": "clinic", "city": "paris"})
	mustCreate(t, p, ctx, map[string]any{"name": "clinic", "city": "tokyo"})

	eq := spi.FilterExpression{Field: "city", Operator: "eq", Value: "london"}
	res, err := p.SearchObjects(ctx, "Patient", spi.SearchQuery{Query: "clinic", Filter: &eq})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("eq filter hits=%d want 1", len(res.Hits))
	}

	ne := spi.FilterExpression{Field: "city", Operator: "ne", Value: "london"}
	_, err = p.SearchObjects(ctx, "Patient", spi.SearchQuery{Query: "clinic", Filter: &ne})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("ne filter err=%v want ErrInvalidMapping (AE7)", err)
	}
	and := spi.FilterExpression{And: []spi.FilterExpression{eq}}
	_, err = p.SearchObjects(ctx, "Patient", spi.SearchQuery{Query: "clinic", Filter: &and})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("And filter err=%v want ErrInvalidMapping (AE7)", err)
	}

	page, err := p.SearchObjects(ctx, "Patient", spi.SearchQuery{Query: "clinic", Limit: 1, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalCount != 3 {
		t.Fatalf("TotalCount=%d want 3", page.TotalCount)
	}
	if len(page.Hits) != 1 || !page.HasNextPage {
		t.Fatalf("hits=%d hasNext=%v", len(page.Hits), page.HasNextPage)
	}
	page2, err := p.SearchObjects(ctx, "Patient", spi.SearchQuery{Query: "clinic", Limit: 1, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Hits) != 1 {
		t.Fatalf("offset hits=%d want 1", len(page2.Hits))
	}
	if page.Hits[0].Object[spi.FieldID] == page2.Hits[0].Object[spi.FieldID] {
		t.Fatal("offset page should be a different hit")
	}
}

func activateSearch(t *testing.T) *mysqlobda.Provider {
	t.Helper()
	raw := testdata(t, "patient_fts.obda.yaml")
	p, db := openProvider(t, raw)
	schema := spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{Name: "Patient", Properties: []spi.PropertyDefinition{
				{Name: "name", Type: "String"},
				{Name: "city", Type: "String"},
			}},
		},
	}
	mustInit(t, db, raw, schema)
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, schema); err != nil {
		t.Fatal(err)
	}
	return p
}
