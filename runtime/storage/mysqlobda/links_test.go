package mysqlobda_test

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/mysqlobda"
)

func TestCreateLinkSystemFields(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	link, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := link[spi.FieldID].(string)
	if id == "" || link[spi.LinkFieldFromID] != readerID || link[spi.LinkFieldToID] != bookID {
		t.Fatalf("%#v", link)
	}
	got, err := p.GetLink(ctx, "Borrows", id)
	if err != nil {
		t.Fatal(err)
	}
	if got[spi.FieldID] != id || got[spi.FieldVersion] != 1 {
		t.Fatalf("%#v", got)
	}
}

func TestCardinalityManyToOne(t *testing.T) {
	p, db, readerID, bookID := activateLibrary(t, spi.CardinalityManyToOne)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil); err != nil {
		t.Fatal(err)
	}
	_, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil)
	if !errors.Is(err, spi.ErrCardinalityViolation) {
		t.Fatalf("err=%v want ErrCardinalityViolation", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM borrows WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("active links=%d", n)
	}
}

func TestCardinalityOneToOne(t *testing.T) {
	p, _, r1, b1 := activateLibrary(t, spi.CardinalityOneToOne)
	ctx := spi.RequestContext{TenantID: "t1"}
	r2obj, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Hong"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateLink(ctx, "Borrows", r1, b1, nil); err != nil {
		t.Fatal(err)
	}
	_, err = p.CreateLink(ctx, "Borrows", r2obj[spi.FieldID].(string), b1, nil)
	if !errors.Is(err, spi.ErrCardinalityViolation) {
		t.Fatalf("err=%v want ErrCardinalityViolation", err)
	}
}

func TestCardinalityManyToManyAllowsPair(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	a, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a[spi.FieldID] == b[spi.FieldID] {
		t.Fatal("expected distinct ids")
	}
}

func TestCardinalitySoftDeleteFreesSlot(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToOne)
	ctx := spi.RequestContext{TenantID: "t1"}
	link, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteLink(ctx, "Borrows", link[spi.FieldID].(string)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil); err != nil {
		t.Fatalf("soft-deleted link must free the active slot: %v", err)
	}
}

func TestCreateLinkSoftDeletedEndpoint(t *testing.T) {
	p, db, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	if err := p.DeleteObject(ctx, "Reader", readerID, "soft"); err != nil {
		t.Fatal(err)
	}
	_, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM borrows`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("inserted=%d", n)
	}
}

func TestCreateLinkCrossTenant(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	_, err := p.CreateLink(spi.RequestContext{TenantID: "t2"}, "Borrows", readerID, bookID, nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestUpdateLinkOCCAndSoftDelete(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	link, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := link[spi.FieldID].(string)
	exp := 1
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.UpdateLink(ctx, "Borrows", id, map[string]any{}, &exp)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	var okN, conflictN int
	for err := range errs {
		if err == nil {
			okN++
		} else if errors.Is(err, spi.ErrVersionConflict) {
			conflictN++
		} else {
			t.Fatalf("unexpected %v", err)
		}
	}
	if okN != 1 || conflictN != 1 {
		t.Fatalf("ok=%d conflict=%d", okN, conflictN)
	}
	if err := p.DeleteLink(ctx, "Borrows", id); err != nil {
		t.Fatal(err)
	}
	_, err = p.UpdateLink(ctx, "Borrows", id, map[string]any{}, nil)
	if !errors.Is(err, spi.ErrLinkNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestGetLinksAndTraverse(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil); err != nil {
		t.Fatal(err)
	}
	out, err := p.GetLinks(ctx, readerID, "Borrows", "outbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("outbound=%d", len(out.Items))
	}
	in, err := p.GetLinks(ctx, bookID, "Borrows", "inbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Items) != 1 {
		t.Fatalf("inbound=%d", len(in.Items))
	}
	tr, err := p.Traverse(ctx, readerID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "Borrows", Direction: "outbound"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Nodes) != 1 {
		t.Fatalf("nodes=%d", len(tr.Nodes))
	}
	assertTerminalOnly(t, tr)
	if tr.Nodes[0][spi.FieldID] != bookID {
		t.Fatalf("node=%v", tr.Nodes[0][spi.FieldID])
	}
	inTr, err := p.Traverse(ctx, bookID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "Borrows", Direction: "inbound"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(inTr.Nodes) != 1 || inTr.Nodes[0][spi.FieldID] != readerID {
		t.Fatalf("inbound nodes=%v", inTr.Nodes)
	}
	assertTerminalOnly(t, inTr)
	empty, err := p.Traverse(ctx, readerID, spi.TraversalPath{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertTerminalOnly(t, empty)
	if len(empty.Nodes) != 0 {
		t.Fatalf("empty steps nodes=%d", len(empty.Nodes))
	}
	_, err = p.Traverse(ctx, readerID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "NoSuch"}}}, nil)
	if !errors.Is(err, spi.ErrLinkNotFound) {
		t.Fatalf("unknown link err=%v", err)
	}
	_, err = p.Traverse(ctx, bookID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "Borrows", Direction: "outbound"}}}, nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("wrong start type err=%v", err)
	}
	tooDeep := make([]spi.TraversalStep, 9)
	for i := range tooDeep {
		tooDeep[i] = spi.TraversalStep{LinkType: "Borrows"}
	}
	_, err = p.Traverse(ctx, readerID, spi.TraversalPath{Steps: tooDeep}, nil)
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("depth err=%v", err)
	}
	_, err = p.Traverse(ctx, "missing", spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "Borrows"}}}, nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v", err)
	}
	_, err = p.Traverse(spi.RequestContext{TenantID: "t2"}, readerID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "Borrows"}}}, nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestHardDeleteCascadesLinks(t *testing.T) {
	p, db, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteObject(ctx, "Reader", readerID, "hard"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM borrows`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("borrows leftover=%d", n)
	}
	out, err := p.GetLinks(ctx, bookID, "Borrows", "inbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 0 {
		t.Fatalf("inbound leftover=%d", len(out.Items))
	}
}

func TestEngineLinkIDInDecodeK(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	link, err := p.CreateLink(ctx, "Borrows", readerID, bookID, map[string]any{
		spi.LinkFieldEngineLinkID: "engine-link-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := link[spi.FieldID].(string)
	if id != "engine-link-1" {
		t.Fatalf("id=%q want engine-link-1", id)
	}
}

func TestGetLinksHidesDeletedPeer(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	link, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteObject(ctx, "Book", bookID, "soft"); err != nil {
		t.Fatal(err)
	}
	out, err := p.GetLinks(ctx, readerID, "Borrows", "outbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 0 {
		t.Fatalf("outbound included deleted peer: %+v", out.Items)
	}
	got, err := p.GetLink(ctx, "Borrows", link[spi.FieldID].(string))
	if err != nil {
		t.Fatal(err)
	}
	if got[spi.LinkFieldToID] != bookID {
		t.Fatalf("%#v", got)
	}
	withDel, err := p.GetLinks(ctx, readerID, "Borrows", "outbound", &spi.QueryOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(withDel.Items) != 0 {
		t.Fatalf("GetLinks IncludeDeleted still hid peer: %+v", withDel.Items)
	}
}

func TestTraverseTwoHopTerminal(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	branch, err := p.CreateObject(ctx, "Branch", map[string]any{"name": "Central"})
	if err != nil {
		t.Fatal(err)
	}
	branchID := branch[spi.FieldID].(string)
	if _, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateLink(ctx, "AvailableAt", bookID, branchID, nil); err != nil {
		t.Fatal(err)
	}
	path := spi.TraversalPath{Steps: []spi.TraversalStep{
		{LinkType: "Borrows", Direction: "outbound"},
		{LinkType: "AvailableAt", Direction: "outbound"},
	}}
	tr, err := p.Traverse(ctx, readerID, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Nodes) != 1 || tr.Nodes[0][spi.FieldID] != branchID {
		t.Fatalf("nodes=%v", tr.Nodes)
	}
	assertTerminalOnly(t, tr)
	if err := p.DeleteObject(ctx, "Book", bookID, "soft"); err != nil {
		t.Fatal(err)
	}
	hidden, err := p.Traverse(ctx, readerID, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden.Nodes) != 0 {
		t.Fatalf("soft-deleted middle still present: %+v", hidden.Nodes)
	}
	shown, err := p.Traverse(ctx, readerID, path, &spi.TraversalOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(shown.Nodes) != 1 || shown.Nodes[0][spi.FieldID] != branchID {
		t.Fatalf("IncludeDeleted middle=%v", shown.Nodes)
	}
}

func TestTraverseDuplicateTerminalsAndPaging(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil); err != nil {
		t.Fatal(err)
	}
	tr, err := p.Traverse(ctx, readerID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "Borrows"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Nodes) != 2 || tr.TotalCount != 2 {
		t.Fatalf("dup nodes=%d total=%d", len(tr.Nodes), tr.TotalCount)
	}
	if tr.Nodes[0][spi.FieldID] != bookID || tr.Nodes[1][spi.FieldID] != bookID {
		t.Fatalf("dup ids=%v %v", tr.Nodes[0][spi.FieldID], tr.Nodes[1][spi.FieldID])
	}
	page, err := p.Traverse(ctx, readerID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "Borrows"}}}, &spi.TraversalOptions{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Nodes) != 1 || page.TotalCount != 2 {
		t.Fatalf("page nodes=%d total=%d", len(page.Nodes), page.TotalCount)
	}
	past, err := p.Traverse(ctx, readerID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "Borrows"}}}, &spi.TraversalOptions{Limit: 10, Offset: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(past.Nodes) != 0 || past.TotalCount != 2 {
		t.Fatalf("offset past nodes=%d total=%d", len(past.Nodes), past.TotalCount)
	}
}

func TestTraverseLimitDefaultsAndCap(t *testing.T) {
	p, db, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil); err != nil {
		t.Fatal(err)
	}
	var fromID, toID, createdAt string
	if err := db.QueryRow(`SELECT from_id, to_id, created_at FROM borrows LIMIT 1`).Scan(&fromID, &toID, &createdAt); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 1001; i++ {
		mustExec(t, db, `INSERT INTO borrows (id, tenant_id, from_id, to_id, version, created_at, updated_at) VALUES (?, 't1', ?, ?, 1, ?, ?)`,
			fmt.Sprintf("extra-%d", i), fromID, toID, createdAt, createdAt)
	}
	zero, err := p.Traverse(ctx, readerID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "Borrows"}}}, &spi.TraversalOptions{Limit: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(zero.Nodes) != 100 || zero.TotalCount != 1001 {
		t.Fatalf("limit 0 nodes=%d total=%d", len(zero.Nodes), zero.TotalCount)
	}
	capped, err := p.Traverse(ctx, readerID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "Borrows"}}}, &spi.TraversalOptions{Limit: 1001})
	if err != nil {
		t.Fatal(err)
	}
	if len(capped.Nodes) != 1000 || capped.TotalCount != 1001 {
		t.Fatalf("limit 1001 nodes=%d total=%d", len(capped.Nodes), capped.TotalCount)
	}
}

func TestTraverseBrokenTypeChainEmpty(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil); err != nil {
		t.Fatal(err)
	}
	tr, err := p.Traverse(ctx, readerID, spi.TraversalPath{Steps: []spi.TraversalStep{
		{LinkType: "Borrows", Direction: "outbound"},
		{LinkType: "Borrows", Direction: "outbound"},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertTerminalOnly(t, tr)
	if len(tr.Nodes) != 0 || tr.TotalCount != 0 {
		t.Fatalf("broken chain nodes=%d total=%d", len(tr.Nodes), tr.TotalCount)
	}
}

func TestTraverseSoftDeletedStartStillFound(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteObject(ctx, "Reader", readerID, "soft"); err != nil {
		t.Fatal(err)
	}
	tr, err := p.Traverse(ctx, readerID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "Borrows"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Nodes) != 1 || tr.Nodes[0][spi.FieldID] != bookID {
		t.Fatalf("soft-deleted start nodes=%v", tr.Nodes)
	}
}

func TestTraverseSoftDeletedHopLink(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	branch, err := p.CreateObject(ctx, "Branch", map[string]any{"name": "Central"})
	if err != nil {
		t.Fatal(err)
	}
	branchID := branch[spi.FieldID].(string)
	link, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateLink(ctx, "AvailableAt", bookID, branchID, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteLink(ctx, "Borrows", link[spi.FieldID].(string)); err != nil {
		t.Fatal(err)
	}
	path := spi.TraversalPath{Steps: []spi.TraversalStep{
		{LinkType: "Borrows", Direction: "outbound"},
		{LinkType: "AvailableAt", Direction: "outbound"},
	}}
	hidden, err := p.Traverse(ctx, readerID, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden.Nodes) != 0 {
		t.Fatalf("soft-deleted hop link still present: %+v", hidden.Nodes)
	}
	shown, err := p.Traverse(ctx, readerID, path, &spi.TraversalOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(shown.Nodes) != 1 || shown.Nodes[0][spi.FieldID] != branchID {
		t.Fatalf("IncludeDeleted hop link=%v", shown.Nodes)
	}
}

func TestTraverseHidesDeletedPeerUntilIncludeDeleted(t *testing.T) {
	p, _, readerID, bookID := activateLibrary(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateLink(ctx, "Borrows", readerID, bookID, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteObject(ctx, "Book", bookID, "soft"); err != nil {
		t.Fatal(err)
	}
	hidden, err := p.Traverse(ctx, readerID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "Borrows"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden.Nodes) != 0 {
		t.Fatalf("default included deleted peer: %+v", hidden.Nodes)
	}
	shown, err := p.Traverse(ctx, readerID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "Borrows"}}}, &spi.TraversalOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(shown.Nodes) != 1 || shown.Nodes[0][spi.FieldID] != bookID {
		t.Fatalf("IncludeDeleted peer=%v", shown.Nodes)
	}
}

func assertTerminalOnly(t *testing.T, tr spi.TraversalResult) {
	t.Helper()
	if tr.Edges == nil || tr.Visited == nil {
		t.Fatalf("nil graph slices edges=%v visited=%v", tr.Edges, tr.Visited)
	}
	if len(tr.Edges) != 0 || len(tr.Visited) != 0 {
		t.Fatalf("edges=%d visited=%d", len(tr.Edges), len(tr.Visited))
	}
}

func activateLibrary(t *testing.T, card spi.Cardinality) (*mysqlobda.Provider, *sql.DB, string, string) {
	t.Helper()
	raw := testdata(t, "library_links.obda.yaml")
	p, db := openProvider(t, raw)
	schema := librarySchema(card)
	mustInit(t, db, raw, schema)
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, schema); err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	reader, err := p.CreateObject(ctx, "Reader", map[string]any{"name": "Xiao Ming"})
	if err != nil {
		t.Fatal(err)
	}
	book, err := p.CreateObject(ctx, "Book", map[string]any{"title": "Three Body Vol 1"})
	if err != nil {
		t.Fatal(err)
	}
	return p, db, reader[spi.FieldID].(string), book[spi.FieldID].(string)
}
