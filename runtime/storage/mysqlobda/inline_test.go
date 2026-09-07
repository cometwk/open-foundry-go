package mysqlobda_test

import (
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/mysqlobda"
)

func TestInlineOptionalCreateLinkGetLinks(t *testing.T) {
	p, _, bookID, memberID := activateInline(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	link, err := p.CreateLink(ctx, "OwnedBy", bookID, memberID, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := link[spi.FieldID].(string)
	if id == "" {
		t.Fatal("missing link id")
	}
	got, err := p.GetLink(ctx, "OwnedBy", id)
	if err != nil {
		t.Fatal(err)
	}
	if got[spi.LinkFieldFromID] != bookID || got[spi.LinkFieldToID] != memberID {
		t.Fatalf("%#v", got)
	}
	out, err := p.GetLinks(ctx, bookID, "OwnedBy", "outbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("outbound=%d", len(out.Items))
	}
	in, err := p.GetLinks(ctx, memberID, "OwnedBy", "inbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Items) != 1 {
		t.Fatalf("inbound=%d", len(in.Items))
	}
	_, err = p.CreateLink(ctx, "OwnedBy", bookID, memberID, nil)
	if !errors.Is(err, spi.ErrCardinalityViolation) {
		t.Fatalf("second create err=%v", err)
	}
}

func TestInlineDeleteLink(t *testing.T) {
	p, _, bookID, memberID := activateInline(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	link, err := p.CreateLink(ctx, "OwnedBy", bookID, memberID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteLink(ctx, "OwnedBy", link[spi.FieldID].(string)); err != nil {
		t.Fatal(err)
	}
	page, err := p.GetLinks(ctx, bookID, "OwnedBy", "outbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("items=%d", len(page.Items))
	}
}

func TestInlineRequiredObjectAPIs(t *testing.T) {
	p, db := openProvider(t, testdata(t, "inline.obda.yaml"))
	schema := inlineRequiredSchema()
	mustInit(t, db, testdata(t, "inline.obda.yaml"), schema)
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, schema); err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	mem, err := p.CreateObject(ctx, "Member", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	memberID := mem[spi.FieldID].(string)
	_, err = p.CreateObject(ctx, "Book", map[string]any{"title": "Go"})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("missing nav err=%v", err)
	}
	book, err := p.CreateObject(ctx, "Book", map[string]any{"title": "Go", "owner": memberID})
	if err != nil {
		t.Fatal(err)
	}
	bookID := book[spi.FieldID].(string)
	if _, err := p.CreateLink(ctx, "OwnedBy", bookID, memberID, nil); !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("CreateLink err=%v", err)
	}
	if err := p.DeleteLink(ctx, "OwnedBy", obda.EncodeDirect("OwnedBy", []string{bookID})); !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("DeleteLink err=%v", err)
	}
	mem2, err := p.CreateObject(ctx, "Member", map[string]any{"name": "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	before := asIntVer(book[spi.FieldVersion])
	updated, err := p.UpdateObject(ctx, "Book", bookID, map[string]any{"owner": mem2[spi.FieldID]}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if asIntVer(updated[spi.FieldVersion]) != before+1 {
		t.Fatalf("version=%v", updated[spi.FieldVersion])
	}
}

func TestInlineHardDeletePeer(t *testing.T) {
	p, _, bookID, memberID := activateInline(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateLink(ctx, "OwnedBy", bookID, memberID, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteObject(ctx, "Member", memberID, "hard"); err != nil {
		t.Fatal(err)
	}
	page, err := p.GetLinks(ctx, bookID, "OwnedBy", "outbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("fk should be cleared: %d", len(page.Items))
	}
	if _, err := p.GetObject(ctx, "Book", bookID); err != nil {
		t.Fatal(err)
	}
}

func TestInlineUpdateLinkUnsupported(t *testing.T) {
	p, _, bookID, memberID := activateInline(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	link, err := p.CreateLink(ctx, "OwnedBy", bookID, memberID, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.UpdateLink(ctx, "OwnedBy", link[spi.FieldID].(string), map[string]any{"x": 1}, nil)
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("err=%v", err)
	}
	_, err = p.CreateLink(ctx, "OwnedBy", bookID, memberID, map[string]any{"extra": "no"})
	if !errors.Is(err, spi.ErrInvalidMapping) && !errors.Is(err, spi.ErrCardinalityViolation) {
		t.Fatalf("properties err=%v", err)
	}
}

func TestInlineConcurrentCreateLink(t *testing.T) {
	p, _, bookID, memberID := activateInline(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	var ok, fail int
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, err := p.CreateLink(ctx, "OwnedBy", bookID, memberID, nil)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				ok++
			} else {
				fail++
			}
		}()
	}
	wg.Wait()
	if ok != 1 || fail != 1 {
		t.Fatalf("ok=%d fail=%d", ok, fail)
	}
}

func TestInlineTraverseOneHop(t *testing.T) {
	p, _, bookID, memberID := activateInline(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateLink(ctx, "OwnedBy", bookID, memberID, nil); err != nil {
		t.Fatal(err)
	}
	tr, err := p.Traverse(ctx, bookID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "OwnedBy"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Nodes) != 1 || tr.Nodes[0][spi.FieldID] != memberID {
		t.Fatalf("%+v", tr.Nodes)
	}
}

func activateInline(t *testing.T) (*mysqlobda.Provider, *sql.DB, string, string) {
	t.Helper()
	raw := testdata(t, "inline.obda.yaml")
	p, db := openProvider(t, raw)
	schema := inlineSchema()
	mustInit(t, db, raw, schema)
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, schema); err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	mem, err := p.CreateObject(ctx, "Member", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	book, err := p.CreateObject(ctx, "Book", map[string]any{"title": "Go"})
	if err != nil {
		t.Fatal(err)
	}
	return p, db, book[spi.FieldID].(string), mem[spi.FieldID].(string)
}

func inlineRequiredSchema() spi.OntologySchema {
	s := inlineSchema()
	s.ObjectTypes[0].Navigations[0].NonNull = true
	return s
}

func asIntVer(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case int32:
		return int(n)
	}
	return 0
}
