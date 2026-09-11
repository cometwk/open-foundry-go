// Package e2e holds the library-pack Gold Path integration: load the
// real domain pack, project to a storage schema, apply it through the
// memory provider, then walk the Phase 3 verb surface end-to-end
// against the simplified library types (Reader, Book, Branch and the
// Borrows / RegisteredAt / AvailableAt links). This is the Phase 3
// acceptance for F8 / R11 / AE11 — the single test that proves the
// Phase 1 → Phase 3 pipeline is wired correctly and the implemented
// SPI methods return zero ErrUnimplemented hits.
//
// The Gold Path does NOT copy domain-packs into runtime/. It loads
// the pack from the repo root via pack.LibraryPackDir(), so the
// library pack stays the source of truth. Tests only exercise the
// simplified subset described in domain-packs/library-pack/library-pack.md.
package e2e_test

import (
	"errors"
	"testing"

	"github.com/openfoundry/runtime/action"
	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/projection"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

// TestGoldPath_Library_F8 is the Phase 3 acceptance test. It walks
// the full pipeline: pack load → IR → OntologySchema projection →
// memory.ApplySchema → Engine verbs plus query/traverse/transaction/
// soft-delete on the simplified library types. Covers AE11 / F8 / R11.
func TestGoldPath_Library_F8(t *testing.T) {
	dir, err := pack.LibraryPackDir()
	if err != nil {
		t.Fatalf("pack.LibraryPackDir err = %v, want nil", err)
	}
	o, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("pack.LoadDir(library-pack) err = %v, want nil", err)
	}
	if o.Namespace == nil || o.Namespace.Name != "example.library" {
		t.Fatalf("loaded namespace = %+v, want example.library", o.Namespace)
	}

	schema := projection.ProjectStorage(o)
	if len(schema.ObjectTypes) == 0 {
		t.Fatal("ProjectStorage produced zero object types")
	}

	p := memory.New()
	ctx := spi.RequestContext{TenantID: "gold-path", ActorID: "test"}
	mr, err := p.ApplySchema(ctx, schema)
	if err != nil {
		t.Fatalf("ApplySchema err = %v, want nil", err)
	}
	if !mr.Success {
		t.Fatalf("ApplySchema result = %+v, want Success=true", mr)
	}

	e, err := engine.New(p, o)
	if err != nil {
		t.Fatalf("engine.New err = %v, want nil", err)
	}

	branch, err := e.CreateObject(ctx, "Branch", map[string]any{
		"name":                  "主馆",
		"maxBorrowPerReader":    3,
		"newBookProtectionDays": 7,
		"allowInterLibraryLoan": true,
	})
	if err != nil {
		t.Fatalf("CreateObject(Branch) err = %v, want nil", err)
	}
	branchID := branch["_id"].(string)

	reader, err := e.CreateObject(ctx, "Reader", map[string]any{
		"name":               "小红",
		"membershipLevel":    "BASIC",
		"currentBorrowCount": 0,
		"registeredDays":     30,
	})
	if err != nil {
		t.Fatalf("CreateObject(Reader) err = %v, want nil", err)
	}
	readerID := reader["_id"].(string)

	book, err := e.CreateObject(ctx, "Book", map[string]any{
		"isbn":            "9787508647357",
		"title":           "人类简史",
		"daysOnShelf":     90,
		"totalCopies":     4,
		"availableCopies": 3,
	})
	if err != nil {
		t.Fatalf("CreateObject(Book) err = %v, want nil", err)
	}
	bookID := book["_id"].(string)

	qpage, err := p.QueryObjects(ctx, "Reader", spi.FilterExpression{
		Field: "name", Operator: "eq", Value: "小红",
	}, &spi.QueryOptions{Limit: 10})
	if err != nil {
		t.Fatalf("QueryObjects after create err = %v, want nil (F8)", err)
	}
	if qpage.TotalCount < 1 {
		t.Fatalf("QueryObjects TotalCount = %d, want >=1 (F8)", qpage.TotalCount)
	}

	if _, err := e.CreateLink(ctx, "RegisteredAt", readerID, branchID, nil); err != nil {
		t.Fatalf("CreateLink(RegisteredAt) err = %v, want nil", err)
	}
	if _, err := e.CreateLink(ctx, "AvailableAt", bookID, branchID, nil); err != nil {
		t.Fatalf("CreateLink(AvailableAt) err = %v, want nil", err)
	}

	link, err := e.CreateLink(ctx, "Borrows", readerID, bookID, nil)
	if err != nil {
		t.Fatalf("CreateLink(Borrows) err = %v, want nil", err)
	}
	linkID := link["_id"].(string)
	if link["_type"] != "Borrows" {
		t.Errorf("CreateLink _type = %v, want Borrows", link["_type"])
	}

	glinks, err := p.GetLinks(ctx, readerID, "Borrows", "outbound", nil)
	if err != nil {
		t.Fatalf("GetLinks err = %v, want nil (F8)", err)
	}
	if glinks.TotalCount != 1 {
		t.Fatalf("GetLinks TotalCount = %d, want 1 (F8)", glinks.TotalCount)
	}
	trav, err := p.Traverse(ctx, readerID, spi.TraversalPath{
		Steps: []spi.TraversalStep{{LinkType: "Borrows", Direction: "outbound"}},
	}, nil)
	if err != nil {
		t.Fatalf("Traverse err = %v, want nil (F8)", err)
	}
	if trav.TotalCount != 1 {
		t.Fatalf("Traverse TotalCount = %d, want 1 (F8)", trav.TotalCount)
	}

	tx, err := p.BeginTransaction(ctx)
	if err != nil {
		t.Fatalf("BeginTransaction err = %v, want nil (F8)", err)
	}
	tmp, err := tx.CreateObject("Reader", map[string]any{
		"name":               "临时读者",
		"membershipLevel":    "BASIC",
		"currentBorrowCount": 0,
		"registeredDays":     1,
	})
	if err != nil {
		t.Fatalf("tx.CreateObject err = %v (F8)", err)
	}
	tmpID := tmp["_id"].(string)
	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx.Rollback err = %v (F8)", err)
	}
	if _, err := e.GetObject(ctx, "Reader", tmpID); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("GetObject after tx rollback err = %v, want ErrObjectNotFound (F8)", err)
	}

	gotReader, err := e.GetObject(ctx, "Reader", readerID)
	if err != nil {
		t.Fatalf("GetObject(Reader) err = %v, want nil", err)
	}
	if gotReader["name"] != "小红" {
		t.Errorf("GetObject(Reader).name = %v, want 小红", gotReader["name"])
	}

	updatedReader, err := e.UpdateObject(ctx, "Reader", readerID, map[string]any{"currentBorrowCount": 1}, nil)
	if err != nil {
		t.Fatalf("UpdateObject(Reader) err = %v, want nil", err)
	}
	if asInt(updatedReader["currentBorrowCount"]) != 1 {
		t.Errorf("UpdateObject currentBorrowCount = %v, want 1", updatedReader["currentBorrowCount"])
	}
	if updatedReader["name"] != "小红" {
		t.Errorf("UpdateObject dropped name = %v, want 小红 (merge)", updatedReader["name"])
	}

	if _, err := e.UpdateLink(ctx, "Borrows", linkID, map[string]any{}, nil); err != nil {
		t.Fatalf("UpdateLink err = %v, want nil (F8)", err)
	}
	gotLink, err := e.GetLink(ctx, "Borrows", linkID)
	if err != nil {
		t.Fatalf("GetLink err = %v, want nil", err)
	}
	if gotLink["_id"] != linkID {
		t.Errorf("GetLink _id = %v, want %v", gotLink["_id"], linkID)
	}

	if err := e.DeleteLink(ctx, "Borrows", linkID); err != nil {
		t.Fatalf("DeleteLink err = %v, want nil", err)
	}
	if _, err := e.GetLink(ctx, "Borrows", linkID); !errors.Is(err, spi.ErrLinkNotFound) {
		t.Fatalf("GetLink after delete err = %v, want ErrLinkNotFound", err)
	}

	if err := e.DeleteObject(ctx, "Reader", readerID, "soft"); err != nil {
		t.Fatalf("DeleteObject(Reader soft) err = %v, want nil (F8)", err)
	}
	if _, err := e.GetObject(ctx, "Reader", readerID); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("GetObject after soft delete err = %v, want ErrObjectNotFound (F8)", err)
	}
	incl, err := p.QueryObjects(ctx, "Reader", spi.FilterExpression{}, &spi.QueryOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatalf("QueryObjects(includeDeleted) err = %v (F8)", err)
	}
	softSeen := false
	for _, item := range incl.Items {
		if item["_id"] == readerID {
			softSeen = true
		}
	}
	if !softSeen {
		t.Fatal("QueryObjects(includeDeleted:true) missing soft-deleted Reader (F8)")
	}

	if err := e.DeleteObject(ctx, "Book", bookID, "hard"); err != nil {
		t.Fatalf("DeleteObject(Book) err = %v, want nil", err)
	}
}

// TestGoldPath_BorrowBook_F1 is the Phase 4 acceptance: load BorrowBook
// YAML from the real pack, resolve Reader/Book, pass CEL preconditions,
// then the caller hand-writes UpdateObject / CreateLink. Evaluation
// does not mutate ontology state. Covers AE6 / F1. Uses the S1 pair
// from the simplified library case (xiao_hong / book_sapiens).
func TestGoldPath_BorrowBook_F1(t *testing.T) {
	dir, err := pack.LibraryPackDir()
	if err != nil {
		t.Fatalf("pack.LibraryPackDir err = %v, want nil", err)
	}
	o, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("pack.LoadDir(library-pack) err = %v, want nil", err)
	}
	manifests, err := pack.LoadActions(dir, o)
	if err != nil {
		t.Fatalf("pack.LoadActions err = %v, want nil", err)
	}

	p := memory.New()
	ctx := spi.RequestContext{TenantID: "gold-path-borrow", ActorID: "test"}
	if _, err := p.ApplySchema(ctx, projection.ProjectStorage(o)); err != nil {
		t.Fatalf("ApplySchema err = %v, want nil", err)
	}
	e, err := engine.New(p, o)
	if err != nil {
		t.Fatalf("engine.New err = %v, want nil", err)
	}

	reader, err := e.CreateObject(ctx, "Reader", map[string]any{
		"name":               "小红",
		"membershipLevel":    "BASIC",
		"currentBorrowCount": 0,
		"registeredDays":     30,
	})
	if err != nil {
		t.Fatalf("CreateObject(Reader) err = %v, want nil", err)
	}
	book, err := e.CreateObject(ctx, "Book", map[string]any{
		"isbn":            "9787508647357",
		"title":           "人类简史",
		"daysOnShelf":     90,
		"totalCopies":     4,
		"availableCopies": 3,
	})
	if err != nil {
		t.Fatalf("CreateObject(Book) err = %v, want nil", err)
	}

	params := map[string]any{
		"reader": reader["_id"].(string),
		"book":   book["_id"].(string),
	}
	result, err := action.Evaluate(ctx, e, o, manifests, action.Request{
		Name:   "BorrowBook",
		Params: params,
		Actor:  action.Actor{ID: "lib-1", Type: "user", Roles: []string{"librarian"}},
	})
	if err != nil {
		t.Fatalf("Evaluate(BorrowBook) err = %v, want nil", err)
	}

	links, err := p.GetLinks(ctx, reader["_id"].(string), "Borrows", "outbound", nil)
	if err != nil {
		t.Fatalf("GetLinks after eval err = %v", err)
	}
	if links.TotalCount != 0 {
		t.Fatalf("Borrows count after Evaluate = %d, want 0 (eval must not write)", links.TotalCount)
	}

	updatedBook, err := e.UpdateObject(ctx, "Book", result.Objects["book"]["_id"].(string), map[string]any{
		"availableCopies": asInt(result.Objects["book"]["availableCopies"]) - 1,
	}, nil)
	if err != nil {
		t.Fatalf("UpdateObject(Book) err = %v, want nil", err)
	}
	updatedReader, err := e.UpdateObject(ctx, "Reader", result.Objects["reader"]["_id"].(string), map[string]any{
		"currentBorrowCount": asInt(result.Objects["reader"]["currentBorrowCount"]) + 1,
	}, nil)
	if err != nil {
		t.Fatalf("UpdateObject(Reader) err = %v, want nil", err)
	}
	if _, err := e.CreateLink(ctx, "Borrows", reader["_id"].(string), book["_id"].(string), nil); err != nil {
		t.Fatalf("CreateLink(Borrows) err = %v, want nil", err)
	}

	gotBook, err := e.GetObject(ctx, "Book", book["_id"].(string))
	if err != nil {
		t.Fatalf("GetObject(Book) err = %v, want nil", err)
	}
	if asInt(gotBook["availableCopies"]) != 2 {
		t.Errorf("availableCopies = %v, want 2", gotBook["availableCopies"])
	}
	if gotBook["title"] != "人类简史" {
		t.Errorf("title = %v, want 人类简史", gotBook["title"])
	}
	if asInt(updatedBook["availableCopies"]) != 2 {
		t.Errorf("updated book availableCopies = %v, want 2", updatedBook["availableCopies"])
	}
	if asInt(updatedReader["currentBorrowCount"]) != 1 {
		t.Errorf("updated reader currentBorrowCount = %v, want 1", updatedReader["currentBorrowCount"])
	}
	gotLinks, err := p.GetLinks(ctx, reader["_id"].(string), "Borrows", "outbound", nil)
	if err != nil {
		t.Fatalf("GetLinks after write err = %v", err)
	}
	if gotLinks.TotalCount != 1 {
		t.Fatalf("Borrows count after write = %d, want 1", gotLinks.TotalCount)
	}
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return -1
	}
}
