package sqliteobda_test

import (
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/sqliteobda"
)

func TestCreateLinkSystemFields(t *testing.T) {
	p, _, patientID, wardID := activateHospital(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	link, err := p.CreateLink(ctx, "AdmittedTo", patientID, wardID, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := link[spi.FieldID].(string)
	if id == "" || link[spi.LinkFieldFromID] != patientID || link[spi.LinkFieldToID] != wardID {
		t.Fatalf("%#v", link)
	}
	got, err := p.GetLink(ctx, "AdmittedTo", id)
	if err != nil {
		t.Fatal(err)
	}
	if got[spi.FieldID] != id || got[spi.FieldVersion] != 1 {
		t.Fatalf("%#v", got)
	}
}

func TestCardinalityManyToOne(t *testing.T) {
	p, db, patientID, wardID := activateHospital(t, spi.CardinalityManyToOne)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateLink(ctx, "AdmittedTo", patientID, wardID, nil); err != nil {
		t.Fatal(err)
	}
	_, err := p.CreateLink(ctx, "AdmittedTo", patientID, wardID, nil)
	if !errors.Is(err, spi.ErrCardinalityViolation) {
		t.Fatalf("err=%v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM of_link_meta WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("active links=%d", n)
	}
}

func TestCardinalityOneToOne(t *testing.T) {
	p, _, p1, w1 := activateHospital(t, spi.CardinalityOneToOne)
	ctx := spi.RequestContext{TenantID: "t1"}
	p2obj, err := p.CreateObject(ctx, "Patient", map[string]any{"patientId": "p2", "name": "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateLink(ctx, "AdmittedTo", p1, w1, nil); err != nil {
		t.Fatal(err)
	}
	_, err = p.CreateLink(ctx, "AdmittedTo", p2obj[spi.FieldID].(string), w1, nil)
	if !errors.Is(err, spi.ErrCardinalityViolation) {
		t.Fatalf("err=%v", err)
	}
}

func TestCardinalityManyToManyAllowsPair(t *testing.T) {
	p, _, patientID, wardID := activateHospital(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	a, err := p.CreateLink(ctx, "AdmittedTo", patientID, wardID, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.CreateLink(ctx, "AdmittedTo", patientID, wardID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a[spi.FieldID] == b[spi.FieldID] {
		t.Fatal("expected distinct ids")
	}
}

func TestCreateLinkSoftDeletedEndpoint(t *testing.T) {
	p, db, patientID, wardID := activateHospital(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	if err := p.DeleteObject(ctx, "Patient", patientID, "soft"); err != nil {
		t.Fatal(err)
	}
	_, err := p.CreateLink(ctx, "AdmittedTo", patientID, wardID, nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM of_link_meta`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("inserted=%d", n)
	}
}

func TestCreateLinkCrossTenant(t *testing.T) {
	p, _, patientID, wardID := activateHospital(t, spi.CardinalityManyToMany)
	_, err := p.CreateLink(spi.RequestContext{TenantID: "t2"}, "AdmittedTo", patientID, wardID, nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestUpdateLinkOCCAndSoftDelete(t *testing.T) {
	p, _, patientID, wardID := activateHospital(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	link, err := p.CreateLink(ctx, "AdmittedTo", patientID, wardID, nil)
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
			_, err := p.UpdateLink(ctx, "AdmittedTo", id, map[string]any{}, &exp)
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
	if err := p.DeleteLink(ctx, "AdmittedTo", id); err != nil {
		t.Fatal(err)
	}
	_, err = p.UpdateLink(ctx, "AdmittedTo", id, map[string]any{}, nil)
	if !errors.Is(err, spi.ErrLinkNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestGetLinksAndTraverse(t *testing.T) {
	p, _, patientID, wardID := activateHospital(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	if _, err := p.CreateLink(ctx, "AdmittedTo", patientID, wardID, nil); err != nil {
		t.Fatal(err)
	}
	out, err := p.GetLinks(ctx, patientID, "AdmittedTo", "outbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("outbound=%d", len(out.Items))
	}
	in, err := p.GetLinks(ctx, wardID, "AdmittedTo", "inbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Items) != 1 {
		t.Fatalf("inbound=%d", len(in.Items))
	}
	tr, err := p.Traverse(ctx, patientID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "AdmittedTo", Direction: "outbound"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Nodes) != 1 || len(tr.Edges) != 1 {
		t.Fatalf("nodes=%d edges=%d visited=%d", len(tr.Nodes), len(tr.Edges), len(tr.Visited))
	}
	if tr.Nodes[0][spi.FieldID] != wardID {
		t.Fatalf("node=%v", tr.Nodes[0][spi.FieldID])
	}
	_, err = p.Traverse(ctx, "missing", spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "AdmittedTo"}}}, nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v", err)
	}
	_, err = p.Traverse(spi.RequestContext{TenantID: "t2"}, patientID, spi.TraversalPath{Steps: []spi.TraversalStep{{LinkType: "AdmittedTo"}}}, nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestSidecarLinkAdoptsEngineLinkID(t *testing.T) {
	p, _, patientID, wardID := activateHospital(t, spi.CardinalityManyToMany)
	ctx := spi.RequestContext{TenantID: "t1"}
	link, err := p.CreateLink(ctx, "AdmittedTo", patientID, wardID, map[string]any{
		spi.LinkFieldEngineLinkID: "engine-link-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if link[spi.FieldID] != "engine-link-1" {
		t.Fatalf("got %v", link[spi.FieldID])
	}
}

func activateHospital(t *testing.T, card spi.Cardinality) (*sqliteobda.Provider, *sql.DB, string, string) {
	t.Helper()
	raw := testdata(t, "hospital.obda.yaml")
	p, db := openProvider(t, raw)
	schema := hospitalSchema(card)
	mustInit(t, db, raw, schema)
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: "t1"}, schema); err != nil {
		t.Fatal(err)
	}
	ctx := spi.RequestContext{TenantID: "t1"}
	pat, err := p.CreateObject(ctx, "Patient", map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	ward, err := p.CreateObject(ctx, "Ward", map[string]any{"name": "A"})
	if err != nil {
		t.Fatal(err)
	}
	return p, db, pat[spi.FieldID].(string), ward[spi.FieldID].(string)
}
