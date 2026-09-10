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
	mustCreate(t, p, ctx, "Book", map[string]any{"title": "physics physics physics", "isbn": "9781000000001", "daysOnShelf": 100})
	mustCreate(t, p, ctx, "Book", map[string]any{"title": "physics cosmology", "isbn": "9781000000002", "daysOnShelf": 200})
	mustCreate(t, p, ctx, "Book", map[string]any{"title": "gardening manual", "isbn": "9781000000003", "daysOnShelf": 300})

	res, err := p.SearchObjects(ctx, "Book", spi.SearchQuery{Query: "physics"})
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
		// Highlights must be the whole declared-field value, not a snippet.
		wantTitle, _ := h.Object["title"].(string)
		if got := h.Highlights["title"]; len(got) != 1 || got[0] != wantTitle {
			t.Fatalf("title highlights=%v want [%q]", got, wantTitle)
		}
		wantISBN, _ := h.Object["isbn"].(string)
		if got := h.Highlights["isbn"]; len(got) != 1 || got[0] != wantISBN {
			t.Fatalf("isbn highlights=%v want [%q]", got, wantISBN)
		}
		// "daysOnShelf" is a mapped field but not in search.fields — never highlighted.
		if _, ok := h.Highlights["daysOnShelf"]; ok {
			t.Fatal("undeclared field must not appear in highlights")
		}
	}
}

func TestSearchBlankQueryEmpty(t *testing.T) {
	p := activateSearch(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	mustCreate(t, p, ctx, "Book", map[string]any{"title": "physics", "isbn": "9781000000001"})
	for _, q := range []string{"", "   "} {
		res, err := p.SearchObjects(ctx, "Book", spi.SearchQuery{Query: q})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Hits) != 0 || res.TotalCount != 0 {
			t.Fatalf("blank query %q hits=%d total=%d", q, len(res.Hits), res.TotalCount)
		}
	}
	// Blank query is empty even without a searchable mapping (AE2).
	p2, _ := activateReader(t)
	res, err := p2.SearchObjects(ctx, "Reader", spi.SearchQuery{Query: "   "})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 || res.TotalCount != 0 {
		t.Fatalf("blank on unmapped model hits=%d total=%d", len(res.Hits), res.TotalCount)
	}
}

func TestSearchUnmappedNonEmpty(t *testing.T) {
	p, _ := activateReader(t)
	_, err := p.SearchObjects(spi.RequestContext{TenantID: "t1"}, "Reader", spi.SearchQuery{Query: "physics"})
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("err=%v want ErrUnsupportedCapability (AE1)", err)
	}
}

func TestSearchFieldsMustEqualDeclared(t *testing.T) {
	p := activateSearch(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	_, err := p.SearchObjects(ctx, "Book", spi.SearchQuery{Query: "physics", Fields: []string{"title"}})
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("subset err=%v want ErrUnsupportedCapability (AE3)", err)
	}
	_, err = p.SearchObjects(ctx, "Book", spi.SearchQuery{Query: "physics", Fields: []string{"title", "isbn", "daysOnShelf"}})
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("superset err=%v want ErrUnsupportedCapability (AE3)", err)
	}
	_, err = p.SearchObjects(ctx, "Book", spi.SearchQuery{Query: "physics", Fields: []string{"nope"}})
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("unknown field err=%v want ErrUnsupportedCapability", err)
	}
	if _, err := p.SearchObjects(ctx, "Book", spi.SearchQuery{Query: "physics", Fields: []string{"isbn", "title"}}); err != nil {
		t.Fatalf("same set different order should pass: %v", err)
	}
}

func TestSearchSoftDeleteAndTenant(t *testing.T) {
	p := activateSearch(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	obj, err := p.CreateObject(ctx, "Book", map[string]any{"title": "physics", "isbn": "9781000000001"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteObject(ctx, "Book", obj[spi.FieldID].(string), "soft"); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, p, spi.RequestContext{TenantID: "t2"}, "Book", map[string]any{"title": "physics", "isbn": "9781000000002"})
	res, err := p.SearchObjects(ctx, "Book", spi.SearchQuery{Query: "physics"})
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
	mustCreate(t, p, ctx, "Book", map[string]any{"title": "physics volume", "isbn": "9781000000001"})
	mustCreate(t, p, ctx, "Book", map[string]any{"title": "physics revised", "isbn": "9781000000002"})
	mustCreate(t, p, ctx, "Book", map[string]any{"title": "physics primer", "isbn": "9781000000003"})

	eq := spi.FilterExpression{Field: "isbn", Operator: "eq", Value: "9781000000002"}
	res, err := p.SearchObjects(ctx, "Book", spi.SearchQuery{Query: "physics", Filter: &eq})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("eq filter hits=%d want 1", len(res.Hits))
	}

	ne := spi.FilterExpression{Field: "isbn", Operator: "ne", Value: "9781000000002"}
	_, err = p.SearchObjects(ctx, "Book", spi.SearchQuery{Query: "physics", Filter: &ne})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("ne filter err=%v want ErrInvalidMapping (AE7)", err)
	}
	and := spi.FilterExpression{And: []spi.FilterExpression{eq}}
	_, err = p.SearchObjects(ctx, "Book", spi.SearchQuery{Query: "physics", Filter: &and})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("And filter err=%v want ErrInvalidMapping (AE7)", err)
	}

	page, err := p.SearchObjects(ctx, "Book", spi.SearchQuery{Query: "physics", Limit: 1, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalCount != 3 {
		t.Fatalf("TotalCount=%d want 3", page.TotalCount)
	}
	if len(page.Hits) != 1 || !page.HasNextPage {
		t.Fatalf("hits=%d hasNext=%v", len(page.Hits), page.HasNextPage)
	}
	page2, err := p.SearchObjects(ctx, "Book", spi.SearchQuery{Query: "physics", Limit: 1, Offset: 1})
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
	raw := testdata(t, "library_fts.obda.yaml")
	p, db := openProvider(t, raw)
	mustInit(t, db, raw, bookSchema())
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, bookSchema()); err != nil {
		t.Fatal(err)
	}
	return p
}
