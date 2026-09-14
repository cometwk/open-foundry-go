package query

import (
	"errors"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
)

func TestExecute_GetListAggregateSearch_MatchEngine(t *testing.T) {
	e, ctx, ids := seedNav(t, memory.New())

	got, err := Execute(e, ctx, Op{Get: &Get{Type: "A", ID: ids.a}})
	if err != nil {
		t.Fatalf("Get err = %v", err)
	}
	direct, err := e.GetObject(ctx, "A", ids.a)
	if err != nil {
		t.Fatalf("engine GetObject err = %v", err)
	}
	if got.Object[spi.FieldID] != direct[spi.FieldID] || got.Object["name"] != "root" {
		t.Fatalf("Get = %+v, want %+v", got.Object, direct)
	}

	page, err := Execute(e, ctx, Op{List: &List{Type: "A", Options: &spi.QueryOptions{Limit: 10}}})
	if err != nil {
		t.Fatalf("List err = %v", err)
	}
	engPage, err := e.QueryObjects(ctx, "A", spi.FilterExpression{}, &spi.QueryOptions{Limit: 10})
	if err != nil {
		t.Fatalf("engine QueryObjects err = %v", err)
	}
	if page.Page.TotalCount != engPage.TotalCount {
		t.Fatalf("List TotalCount = %d, engine = %d", page.Page.TotalCount, engPage.TotalCount)
	}

	agg, err := Execute(e, ctx, Op{Aggregate: &Aggregate{Type: "A", Query: spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "*", Fn: "count", Alias: "n"}},
	}}})
	if err != nil {
		t.Fatalf("Aggregate err = %v", err)
	}
	engAgg, err := e.AggregateObjects(ctx, "A", spi.AggregateQuery{
		Fields: []spi.AggregateField{{Field: "*", Fn: "count", Alias: "n"}},
	})
	if err != nil {
		t.Fatalf("engine Aggregate err = %v", err)
	}
	if agg.Aggregate.TotalGroups != engAgg.TotalGroups {
		t.Fatalf("Aggregate groups = %d, engine = %d", agg.Aggregate.TotalGroups, engAgg.TotalGroups)
	}

	search, err := Execute(e, ctx, Op{Search: &Search{Type: "A", Query: spi.SearchQuery{Query: "root", Limit: 10}}})
	if err != nil {
		t.Fatalf("Search err = %v", err)
	}
	engSearch, err := e.SearchObjects(ctx, "A", spi.SearchQuery{Query: "root", Limit: 10})
	if err != nil {
		t.Fatalf("engine Search err = %v", err)
	}
	if search.Search.TotalCount != engSearch.TotalCount {
		t.Fatalf("Search TotalCount = %d, engine = %d", search.Search.TotalCount, engSearch.TotalCount)
	}
}

func TestExecute_Expand_GetLinksVsTraverseVsFork(t *testing.T) {
	rec := &countStore{inner: memory.New()}
	e, ctx, ids := seedNav(t, rec)
	rec.getLinks, rec.traverse, rec.getObject, rec.queryObjects = 0, 0, 0, 0

	leaf, err := Execute(e, ctx, Op{Expand: &Expand{
		StartType: "A", StartID: ids.a, Mode: ExpandGetLinks,
		Paths: [][]string{{"leaf"}},
	}})
	if err != nil {
		t.Fatalf("leaf Expand err = %v", err)
	}
	if rec.getLinks != 1 || rec.traverse != 0 {
		t.Fatalf("leaf GetLinks/Traverse = %d/%d, want 1/0", rec.getLinks, rec.traverse)
	}
	if rec.getObject != 0 || rec.queryObjects != 1 {
		t.Fatalf("leaf GetObject/QueryObjects = %d/%d, want 0/1 (no per-object N+1)", rec.getObject, rec.queryObjects)
	}
	if len(leaf.Expand.FirstHop) != 1 || leaf.Expand.FirstHop[0]["name"] != "L1" {
		t.Fatalf("leaf FirstHop = %+v", leaf.Expand.FirstHop)
	}

	rec.getLinks, rec.traverse, rec.getObject, rec.queryObjects = 0, 0, 0, 0
	two, err := Execute(e, ctx, Op{Expand: &Expand{
		StartType: "A", StartID: ids.a, Mode: ExpandTraverse,
		Paths: [][]string{{"b", "c"}},
	}})
	if err != nil {
		t.Fatalf("2-hop Expand err = %v", err)
	}
	if rec.traverse != 1 || rec.getLinks != 0 {
		t.Fatalf("2-hop GetLinks/Traverse = %d/%d, want 0/1", rec.getLinks, rec.traverse)
	}
	if rec.getObject != 0 || rec.queryObjects != 1 {
		t.Fatalf("2-hop GetObject/QueryObjects = %d/%d, want 0/1 (intermediate B batch)", rec.getObject, rec.queryObjects)
	}
	if rec.lastTraverse == nil || !rec.lastTraverse.StartConfirmed || !rec.lastTraverse.SkipTotalCount {
		t.Fatalf("2-hop Traverse options = %+v, want StartConfirmed+SkipTotalCount", rec.lastTraverse)
	}
	if len(two.Expand.Terminals) != 1 || two.Expand.Terminals[0]["name"] != "C1" {
		t.Fatalf("2-hop terminals = %+v", two.Expand.Terminals)
	}
	if len(two.Expand.Adjacency[ids.b]["c"]) != 1 {
		t.Fatalf("adjacency b.c = %+v", two.Expand.Adjacency[ids.b]["c"])
	}

	rec.getLinks, rec.traverse, rec.getObject, rec.queryObjects = 0, 0, 0, 0
	proj, err := Execute(e, ctx, Op{Expand: &Expand{
		StartType: "A", StartID: ids.a, Mode: ExpandTraverse,
		Paths:   [][]string{{"b", "c"}},
		Project: map[string][]string{"B": {"name"}},
	}})
	if err != nil {
		t.Fatalf("projected Expand err = %v", err)
	}
	if rec.queryObjects != 0 {
		t.Fatalf("projected QueryObjects = %d, want 0 (B came from Traverse)", rec.queryObjects)
	}
	if rec.lastTraverse == nil || rec.lastTraverse.Project["B"] == nil {
		t.Fatalf("projected Traverse options = %+v", rec.lastTraverse)
	}
	if len(proj.Expand.FirstHop) != 1 || proj.Expand.FirstHop[0]["name"] != "B1" {
		t.Fatalf("projected FirstHop = %+v", proj.Expand.FirstHop)
	}

	rec.getLinks, rec.traverse, rec.getObject, rec.queryObjects = 0, 0, 0, 0
	skel, err := Execute(e, ctx, Op{Expand: &Expand{
		StartType: "A", StartID: ids.a, Mode: ExpandTraverse,
		Paths:   [][]string{{"b", "c"}},
		Project: map[string][]string{"B": {}},
	}})
	if err != nil {
		t.Fatalf("skeleton Expand err = %v", err)
	}
	if rec.queryObjects != 0 {
		t.Fatalf("skeleton QueryObjects = %d, want 0", rec.queryObjects)
	}
	if len(skel.Expand.FirstHop) != 1 || objectID(skel.Expand.FirstHop[0]) != ids.b {
		t.Fatalf("skeleton FirstHop = %+v", skel.Expand.FirstHop)
	}
	if _, has := skel.Expand.FirstHop[0]["name"]; has {
		t.Fatalf("skeleton must not carry name: %+v", skel.Expand.FirstHop[0])
	}

	rec.getLinks, rec.traverse, rec.getObject, rec.queryObjects = 0, 0, 0, 0
	fork, err := Execute(e, ctx, Op{Expand: &Expand{
		StartType: "A", StartID: ids.a, Mode: ExpandTraverse,
		Paths: [][]string{{"b", "c"}, {"b", "d"}},
	}})
	if err != nil {
		t.Fatalf("fork Expand err = %v", err)
	}
	if rec.traverse != 2 || rec.getLinks != 0 {
		t.Fatalf("fork GetLinks/Traverse = %d/%d, want 0/2", rec.getLinks, rec.traverse)
	}
	if rec.queryObjects != 2 {
		t.Fatalf("fork QueryObjects = %d, want 2 (one per path)", rec.queryObjects)
	}
	if len(fork.Expand.FirstHop) != 1 {
		t.Fatalf("fork FirstHop len = %d (shared prefix should dedupe B)", len(fork.Expand.FirstHop))
	}
	if len(fork.Expand.Adjacency[ids.b]["c"]) != 1 || len(fork.Expand.Adjacency[ids.b]["d"]) != 1 {
		t.Fatalf("fork b.c/b.d = %+v", fork.Expand.Adjacency[ids.b])
	}
}

func TestExecute_Expand_UnknownField_NoStorage(t *testing.T) {
	rec := &countStore{inner: memory.New()}
	e, ctx, ids := seedNav(t, rec)
	rec.getLinks, rec.traverse, rec.getObject = 0, 0, 0
	_, err := Execute(e, ctx, Op{Expand: &Expand{
		StartType: "A", StartID: ids.a, Mode: ExpandTraverse,
		Paths:      [][]string{{"name"}},
		CheckStart: true,
	}})
	if !errors.Is(err, ErrInvalidFollowPath) {
		t.Fatalf("err = %v, want ErrInvalidFollowPath", err)
	}
	if rec.getLinks != 0 || rec.traverse != 0 || rec.getObject != 0 {
		t.Fatalf("SPI GetObject/GetLinks/Traverse = %d/%d/%d, want 0/0/0", rec.getObject, rec.getLinks, rec.traverse)
	}
}

type navIDs struct{ a, b, c, d, l string }

func seedNav(t *testing.T, store spi.StorageProvider) (*engine.Engine, spi.RequestContext, navIDs) {
	t.Helper()
	ont := navOntology()
	ctx := spi.RequestContext{TenantID: "t", ActorID: "test"}
	if _, err := store.ApplySchema(ctx, spi.OntologySchema{
		Version: 1,
		ObjectTypes: []spi.ObjectTypeDefinition{
			{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D"}, {Name: "L"},
		},
		LinkTypes: []spi.LinkTypeDefinition{
			{Name: "AB", FromType: "A", ToType: "B", Cardinality: spi.CardinalityOneToMany},
			{Name: "AL", FromType: "A", ToType: "L", Cardinality: spi.CardinalityOneToMany},
			{Name: "BC", FromType: "B", ToType: "C", Cardinality: spi.CardinalityOneToMany},
			{Name: "BD", FromType: "B", ToType: "D", Cardinality: spi.CardinalityOneToMany},
		},
	}); err != nil {
		t.Fatalf("ApplySchema err = %v", err)
	}
	e, err := engine.New(store, ont)
	if err != nil {
		t.Fatalf("engine.New err = %v", err)
	}
	a, err := e.CreateObject(ctx, "A", map[string]any{"name": "root"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.CreateObject(ctx, "B", map[string]any{"name": "B1"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := e.CreateObject(ctx, "C", map[string]any{"name": "C1"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := e.CreateObject(ctx, "D", map[string]any{"name": "D1"})
	if err != nil {
		t.Fatal(err)
	}
	l, err := e.CreateObject(ctx, "L", map[string]any{"name": "L1"})
	if err != nil {
		t.Fatal(err)
	}
	mustLink(t, e, ctx, "AB", a, b)
	mustLink(t, e, ctx, "AL", a, l)
	mustLink(t, e, ctx, "BC", b, c)
	mustLink(t, e, ctx, "BD", b, d)
	return e, ctx, navIDs{
		a: a[spi.FieldID].(string),
		b: b[spi.FieldID].(string),
		c: c[spi.FieldID].(string),
		d: d[spi.FieldID].(string),
		l: l[spi.FieldID].(string),
	}
}

func mustLink(t *testing.T, e *engine.Engine, ctx spi.RequestContext, typ string, from, to spi.OntologyObject) {
	t.Helper()
	if _, err := e.CreateLink(ctx, typ, from[spi.FieldID].(string), to[spi.FieldID].(string), nil); err != nil {
		t.Fatalf("CreateLink %s err = %v", typ, err)
	}
}

func navOntology() *ir.Ontology {
	return &ir.Ontology{
		Namespace: &ir.Namespace{Name: "test"},
		Objects: []ir.ObjectType{
			{Name: "A", Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
				{Name: "name", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
				{Name: "b", Type: ir.TypeRef{Name: "B", IsList: true}, Role: ir.RoleLinkNav, Link: &ir.LinkRef{Type: "AB", Direction: ir.DirectionOutbound}},
				{Name: "leaf", Type: ir.TypeRef{Name: "L", IsList: true}, Role: ir.RoleLinkNav, Link: &ir.LinkRef{Type: "AL", Direction: ir.DirectionOutbound}},
			}},
			{Name: "B", Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
				{Name: "name", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
				{Name: "c", Type: ir.TypeRef{Name: "C", IsList: true}, Role: ir.RoleLinkNav, Link: &ir.LinkRef{Type: "BC", Direction: ir.DirectionOutbound}},
				{Name: "d", Type: ir.TypeRef{Name: "D", IsList: true}, Role: ir.RoleLinkNav, Link: &ir.LinkRef{Type: "BD", Direction: ir.DirectionOutbound}},
			}},
			{Name: "C", Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
				{Name: "name", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
			}},
			{Name: "D", Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
				{Name: "name", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
			}},
			{Name: "L", Fields: []ir.Field{
				{Name: "id", Type: ir.TypeRef{Name: "ID"}, Role: ir.RolePrimary},
				{Name: "name", Type: ir.TypeRef{Name: "String"}, Role: ir.RoleProperty},
			}},
		},
		Links: []ir.LinkType{
			{Name: "AB", From: "A", To: "B", Cardinality: ir.CardinalityOneToMany},
			{Name: "AL", From: "A", To: "L", Cardinality: ir.CardinalityOneToMany},
			{Name: "BC", From: "B", To: "C", Cardinality: ir.CardinalityOneToMany},
			{Name: "BD", From: "B", To: "D", Cardinality: ir.CardinalityOneToMany},
		},
	}
}

type countStore struct {
	spi.UnimplementedStorageProvider
	inner              spi.StorageProvider
	getLinks, traverse int
	getObject          int
	queryObjects       int
	lastTraverse       *spi.TraversalOptions
}

func (c *countStore) ApplySchema(ctx spi.RequestContext, s spi.OntologySchema) (spi.MigrationResult, error) {
	return c.inner.ApplySchema(ctx, s)
}
func (c *countStore) Capabilities() spi.StorageCapabilities { return c.inner.Capabilities() }
func (c *countStore) CreateObject(ctx spi.RequestContext, typ string, p map[string]any) (spi.OntologyObject, error) {
	return c.inner.CreateObject(ctx, typ, p)
}
func (c *countStore) GetObject(ctx spi.RequestContext, typ, id string) (spi.OntologyObject, error) {
	c.getObject++
	return c.inner.GetObject(ctx, typ, id)
}
func (c *countStore) CreateLink(ctx spi.RequestContext, typ, fromID, toID string, p map[string]any) (spi.OntologyLink, error) {
	return c.inner.CreateLink(ctx, typ, fromID, toID, p)
}
func (c *countStore) GetLinks(ctx spi.RequestContext, objectID, linkType, direction string, options *spi.QueryOptions) (spi.LinkPage, error) {
	c.getLinks++
	return c.inner.GetLinks(ctx, objectID, linkType, direction, options)
}
func (c *countStore) Traverse(ctx spi.RequestContext, startID string, path spi.TraversalPath, options *spi.TraversalOptions) (spi.TraversalResult, error) {
	c.traverse++
	c.lastTraverse = options
	return c.inner.Traverse(ctx, startID, path, options)
}
func (c *countStore) QueryObjects(ctx spi.RequestContext, typ string, filter spi.FilterExpression, options *spi.QueryOptions) (spi.ObjectPage, error) {
	c.queryObjects++
	return c.inner.QueryObjects(ctx, typ, filter, options)
}
func (c *countStore) AggregateObjects(ctx spi.RequestContext, typ string, q spi.AggregateQuery) (spi.AggregateResult, error) {
	return c.inner.AggregateObjects(ctx, typ, q)
}
func (c *countStore) SearchObjects(ctx spi.RequestContext, typ string, q spi.SearchQuery) (spi.SearchResult, error) {
	return c.inner.SearchObjects(ctx, typ, q)
}
func (c *countStore) DeleteObject(ctx spi.RequestContext, typ, id, mode string) error {
	return c.inner.DeleteObject(ctx, typ, id, mode)
}
func (c *countStore) GetLink(ctx spi.RequestContext, typ, id string) (spi.OntologyLink, error) {
	return c.inner.GetLink(ctx, typ, id)
}
func (c *countStore) UpdateObject(ctx spi.RequestContext, typ, id string, p map[string]any, ev *int) (spi.OntologyObject, error) {
	return c.inner.UpdateObject(ctx, typ, id, p, ev)
}
func (c *countStore) UpdateLink(ctx spi.RequestContext, typ, id string, p map[string]any, ev *int) (spi.OntologyLink, error) {
	return c.inner.UpdateLink(ctx, typ, id, p, ev)
}
func (c *countStore) DeleteLink(ctx spi.RequestContext, typ, id string) error {
	return c.inner.DeleteLink(ctx, typ, id)
}

func TestExecute_Expand_LeafBatchHydrationScalesWithTypes(t *testing.T) {
	rec := &countStore{inner: memory.New()}
	e, ctx, ids := seedNav(t, rec)
	// Two more leaves under the same link type: M neighbors, still exactly
	// one hydration query and zero per-object reads.
	for _, name := range []string{"L2", "L3"} {
		l, err := e.CreateObject(ctx, "L", map[string]any{"name": name})
		if err != nil {
			t.Fatal(err)
		}
		mustLink(t, e, ctx, "AL", aobj(ids.a), l)
	}
	rec.queryObjects, rec.getObject = 0, 0
	leaf, err := Execute(e, ctx, Op{Expand: &Expand{
		StartType: "A", StartID: ids.a, Mode: ExpandGetLinks,
		Paths: [][]string{{"leaf"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if rec.getObject != 0 || rec.queryObjects != 1 {
		t.Fatalf("M=3 leaf GetObject/QueryObjects = %d/%d, want 0/1", rec.getObject, rec.queryObjects)
	}
	if len(leaf.Expand.FirstHop) != 3 {
		t.Fatalf("FirstHop = %d, want 3", len(leaf.Expand.FirstHop))
	}
}

func aobj(id string) spi.OntologyObject { return spi.OntologyObject{spi.FieldID: id} }

func TestExecute_Expand_LeafOverflow(t *testing.T) {
	restore := OverrideHopCap(2)
	t.Cleanup(restore)

	leaf := Op{Expand: &Expand{
		StartType: "A", Mode: ExpandGetLinks, Paths: [][]string{{"leaf"}},
	}}

	t.Run("HasNextPage is overflow", func(t *testing.T) {
		rec := &linksPageStore{countStore: &countStore{inner: memory.New()}}
		e, ctx, ids := seedNav(t, rec)
		leaf.Expand.StartID = ids.a
		rec.page = spi.LinkPage{
			Items: []spi.OntologyLink{{
				spi.LinkFieldFromID: ids.a,
				spi.LinkFieldToID:   ids.l,
			}},
			HasNextPage: true,
		}
		_, err := Execute(e, ctx, leaf)
		if !errors.Is(err, spi.ErrTraversalLimitExceeded) {
			t.Fatalf("err=%v want ErrTraversalLimitExceeded", err)
		}
		if !strings.Contains(err.Error(), "2") {
			t.Fatalf("error %q missing cap", err)
		}
	})

	t.Run("window not full succeeds", func(t *testing.T) {
		e, ctx, ids := seedNav(t, memory.New())
		leaf.Expand.StartID = ids.a
		got, err := Execute(e, ctx, leaf)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Expand.FirstHop) != 1 {
			t.Fatalf("FirstHop=%d want 1", len(got.Expand.FirstHop))
		}
	})

	t.Run("exact window without next page succeeds", func(t *testing.T) {
		e, ctx, ids := seedNav(t, memory.New())
		l2, err := e.CreateObject(ctx, "L", map[string]any{"name": "L2"})
		if err != nil {
			t.Fatal(err)
		}
		mustLink(t, e, ctx, "AL", aobj(ids.a), l2)
		leaf.Expand.StartID = ids.a
		got, err := Execute(e, ctx, leaf)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Expand.FirstHop) != 2 {
			t.Fatalf("FirstHop=%d want 2", len(got.Expand.FirstHop))
		}
	})

	t.Run("duplicate links still overflow", func(t *testing.T) {
		rec := &linksPageStore{countStore: &countStore{inner: memory.New()}}
		e, ctx, ids := seedNav(t, rec)
		leaf.Expand.StartID = ids.a
		dup := spi.OntologyLink{
			spi.LinkFieldFromID: ids.a,
			spi.LinkFieldToID:   ids.l,
		}
		rec.page = spi.LinkPage{
			Items:       []spi.OntologyLink{dup, dup, dup},
			HasNextPage: true,
		}
		_, err := Execute(e, ctx, leaf)
		if !errors.Is(err, spi.ErrTraversalLimitExceeded) {
			t.Fatalf("err=%v want ErrTraversalLimitExceeded", err)
		}
	})
}

type linksPageStore struct {
	*countStore
	page spi.LinkPage
}

func (s *linksPageStore) GetLinks(ctx spi.RequestContext, objectID, linkType, direction string, options *spi.QueryOptions) (spi.LinkPage, error) {
	s.getLinks++
	return s.page, nil
}

func TestExecute_Expand_OneHopTraverseSkipsHydration(t *testing.T) {
	rec := &countStore{inner: memory.New()}
	e, ctx, ids := seedNav(t, rec)
	// Single-hop traverse: terminals come back as Nodes and the start
	// endpoint is excluded, so nothing hydrates.
	_, err := Execute(e, ctx, Op{Expand: &Expand{
		StartType: "A", StartID: ids.a, Mode: ExpandTraverse,
		Paths: [][]string{{"b"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if rec.queryObjects != 0 {
		t.Fatalf("1-hop QueryObjects = %d, want 0", rec.queryObjects)
	}
}

func TestHydrateByIDs_DeletedVisibilityAndTenantScope(t *testing.T) {
	e, ctx, ids := seedNav(t, memory.New())
	if err := e.DeleteObject(ctx, "B", ids.b, "soft"); err != nil {
		t.Fatal(err)
	}
	// Default read hides soft-deleted; IncludeDeleted surfaces it.
	got, err := hydrateByIDs(e, ctx, "B", []string{ids.b}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("soft-deleted hydrated: %+v", got)
	}
	got, err = hydrateByIDs(e, ctx, "B", []string{ids.b}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0][spi.FieldID] != ids.b {
		t.Fatalf("IncludeDeleted = %+v", got)
	}
	// Misses prune silently: absent ids return an empty page, not an error.
	got, err = hydrateByIDs(e, ctx, "B", []string{"nope"}, false)
	if err != nil || len(got) != 0 {
		t.Fatalf("miss = %+v err=%v", got, err)
	}
	// Tenant scope comes from ctx: another tenant sees nothing — even with
	// IncludeDeleted, which must not widen tenant visibility.
	other := spi.RequestContext{TenantID: "t2", ActorID: "test"}
	got, err = hydrateByIDs(e, other, "B", []string{ids.b}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("cross-tenant hydration leaked: %+v", got)
	}
}
