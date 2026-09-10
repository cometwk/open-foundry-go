package mysqlobda_test

import (
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/mysqlobda"
)

func TestInlineOptionalCreateLinkGetLinks(t *testing.T) {
	p, _, readerID, branchID := activateInline(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	link, err := p.CreateLink(ctx, "RegisteredAt", readerID, branchID, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := link[spi.FieldID].(string)
	if id == "" {
		t.Fatal("missing link id")
	}
	got, err := p.GetLink(ctx, "RegisteredAt", id)
	if err != nil {
		t.Fatal(err)
	}
	if got[spi.FieldID] != readerID {
		t.Fatalf("inline _id=%v want host %s", got[spi.FieldID], readerID)
	}
	if got[spi.LinkFieldFromID] != readerID || got[spi.LinkFieldToID] != branchID {
		t.Fatalf("%#v", got)
	}
	out, err := p.GetLinks(ctx, readerID, "RegisteredAt", "outbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("outbound=%d", len(out.Items))
	}
	in, err := p.GetLinks(ctx, branchID, "RegisteredAt", "inbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Items) != 1 {
		t.Fatalf("inbound=%d", len(in.Items))
	}
	_, err = p.CreateLink(ctx, "RegisteredAt", readerID, branchID, nil)
	if !errors.Is(err, spi.ErrCardinalityViolation) {
		t.Fatalf("second create err=%v", err)
	}
}

func TestInlineDeleteLink(t *testing.T) {
	p, _, readerID, branchID := activateInline(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	link, err := p.CreateLink(ctx, "RegisteredAt", readerID, branchID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteLink(ctx, "RegisteredAt", link[spi.FieldID].(string)); err != nil {
		t.Fatal(err)
	}
	page, err := p.GetLinks(ctx, readerID, "RegisteredAt", "outbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("items=%d", len(page.Items))
	}
}

func TestInlineRequiredObjectAPIs(t *testing.T) {
	p, db := openProvider(t, testdata(t, "library_inline.obda.yaml"))
	schema := inlineRequiredSchema()
	mustInit(t, db, testdata(t, "library_inline.obda.yaml"), schema)
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, schema); err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	br, err := p.CreateObject(ctx, "Branch", map[string]any{"name": "Central"})
	if err != nil {
		t.Fatal(err)
	}
	branchID := br[spi.FieldID].(string)
	_, err = p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("missing nav err=%v", err)
	}
	reader, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming", "branch": branchID})
	if err != nil {
		t.Fatal(err)
	}
	readerID := reader[spi.FieldID].(string)
	if _, err := p.CreateLink(ctx, "RegisteredAt", readerID, branchID, nil); !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("CreateLink err=%v", err)
	}
	if err := p.DeleteLink(ctx, "RegisteredAt", readerID); !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("DeleteLink err=%v", err)
	}
	br2, err := p.CreateObject(ctx, "Branch", map[string]any{"name": "West"})
	if err != nil {
		t.Fatal(err)
	}
	before := asIntVer(reader[spi.FieldVersion])
	updated, err := p.UpdateObject(ctx, "Reader", readerID, map[string]any{"branch": br2[spi.FieldID]}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if asIntVer(updated[spi.FieldVersion]) != before+1 {
		t.Fatalf("version=%v", updated[spi.FieldVersion])
	}
}

func TestInlineHardDeletePeer(t *testing.T) {
	p, _, readerID, branchID := activateInline(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateLink(ctx, "RegisteredAt", readerID, branchID, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteObject(ctx, "Branch", branchID, "hard"); err != nil {
		t.Fatal(err)
	}
	page, err := p.GetLinks(ctx, readerID, "RegisteredAt", "outbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("fk should be cleared: %d", len(page.Items))
	}
	if _, err := p.GetObject(ctx, "Reader", readerID); err != nil {
		t.Fatal(err)
	}
}

func TestInlineUpdateLinkUnsupported(t *testing.T) {
	p, _, readerID, branchID := activateInline(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	link, err := p.CreateLink(ctx, "RegisteredAt", readerID, branchID, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.UpdateLink(ctx, "RegisteredAt", link[spi.FieldID].(string), map[string]any{"x": 1}, nil)
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("err=%v", err)
	}
	_, err = p.CreateLink(ctx, "RegisteredAt", readerID, branchID, map[string]any{"extra": "no"})
	if !errors.Is(err, spi.ErrInvalidMapping) && !errors.Is(err, spi.ErrCardinalityViolation) {
		t.Fatalf("properties err=%v", err)
	}
}

func TestInlineConcurrentCreateLink(t *testing.T) {
	p, _, readerID, branchID := activateInline(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	var ok, fail int
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, err := p.CreateLink(ctx, "RegisteredAt", readerID, branchID, nil)
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
	p, _, readerID, branchID := activateInline(t)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateLink(ctx, "RegisteredAt", readerID, branchID, nil); err != nil {
		t.Fatal(err)
	}
	tr, err := p.Traverse(ctx, readerID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "RegisteredAt"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Nodes) != 1 || tr.Nodes[0][spi.FieldID] != branchID {
		t.Fatalf("%+v", tr.Nodes)
	}
}

func activateInline(t *testing.T) (*mysqlobda.Provider, *sql.DB, string, string) {
	t.Helper()
	raw := testdata(t, "library_inline.obda.yaml")
	p, db := openProvider(t, raw)
	schema := inlineSchema()
	mustInit(t, db, raw, schema)
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, schema); err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	br, err := p.CreateObject(ctx, "Branch", map[string]any{"name": "Central"})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"})
	if err != nil {
		t.Fatal(err)
	}
	return p, db, reader[spi.FieldID].(string), br[spi.FieldID].(string)
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
