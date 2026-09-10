package obda_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/obda/dialect"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

type fakeDialect struct{}

func (fakeDialect) Name() string { return "fake" }

func (fakeDialect) Capabilities() dialect.Capabilities { return dialect.Capabilities{} }

func (fakeDialect) QuoteIdentifier(id sqlast.Identifier) (string, error) {
	return "#" + id.Name, nil
}

func (fakeDialect) Placeholder(position int) string {
	return fmt.Sprintf("$%d", position)
}

func (fakeDialect) NormalizeValue(_ string, v any) (any, error) { return v, nil }

func (d fakeDialect) Render(stmt sqlast.Statement) (dialect.SQLStatement, error) {
	switch s := stmt.(type) {
	case *sqlast.Select:
		return d.renderSelect(s)
	case *sqlast.Insert:
		return d.renderInsert(s)
	default:
		return dialect.SQLStatement{}, fmt.Errorf("unsupported %T", stmt)
	}
}

func (d fakeDialect) renderSelect(s *sqlast.Select) (dialect.SQLStatement, error) {
	from, err := d.QuoteIdentifier(s.From)
	if err != nil {
		return dialect.SQLStatement{}, err
	}
	sql := "SELECT * FROM " + from
	if s.Search != nil {
		src, err := d.QuoteIdentifier(s.Search.Source)
		if err != nil {
			return dialect.SQLStatement{}, err
		}
		sql += " SEARCH " + src + " " + d.Placeholder(2)
	}
	return dialect.SQLStatement{SQL: sql, Args: nil}, nil
}

func (d fakeDialect) renderInsert(s *sqlast.Insert) (dialect.SQLStatement, error) {
	tbl, err := d.QuoteIdentifier(s.Table)
	if err != nil {
		return dialect.SQLStatement{}, err
	}
	return dialect.SQLStatement{SQL: "INSERT INTO " + tbl, Args: nil}, nil
}

func patientBinding() obda.ObjectBinding {
	return obda.ObjectBinding{
		Table:            "patient",
		TenantColumn:     "tenant_id",
		IdentityColumns:  []string{"patient_id"},
		SelectColumns:    []string{"patient_id", "patient_name", "tenant_id"},
		Writable:         true,
		SearchIndex:      "patient_search",
		SearchableFields: []string{"patient_name", "city_name"},
	}
}

func TestPlanGetObjectUsesParamsNotSQL(t *testing.T) {
	sel, args, err := obda.PlanGetObject(patientBinding(), "t1", []any{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[0] != "t1" || args[1] != "p1" {
		t.Fatalf("args=%v", args)
	}
	out, err := fakeDialect{}.Render(sel)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.SQL, `"`) || strings.Contains(strings.ToLower(out.SQL), "fts5") {
		t.Fatalf("core/fake SQL leaked dialect: %s", out.SQL)
	}
	if !strings.Contains(out.SQL, "#patient") {
		t.Fatalf("sql=%s", out.SQL)
	}
}

func TestPlanSearchHasFullTextMatchWithoutFTSKeyword(t *testing.T) {
	sel, args, err := obda.PlanSearch(patientBinding(), "t1", "flu")
	if err != nil {
		t.Fatal(err)
	}
	if sel.Search == nil {
		t.Fatal("expected FullTextMatch")
	}
	if _, ok := sel.Search.Query.(sqlast.Param); !ok {
		t.Fatalf("query=%T", sel.Search.Query)
	}
	if got, want := namesOf(sel.Search.Columns), []string{"patient_name", "city_name"}; !eqStringSlice(got, want) {
		t.Fatalf("columns=%v want %v", got, want)
	}
	if len(args) != 3 || args[0] != "flu" || args[1] != "t1" || args[2] != "flu" {
		t.Fatalf("args=%v want [query, tenant, query]", args)
	}
	out, err := fakeDialect{}.Render(sel)
	if err != nil {
		t.Fatal(err)
	}
	low := strings.ToLower(out.SQL)
	if strings.Contains(low, "fts5") || strings.Contains(low, "match") {
		t.Fatalf("search plan leaked FTS SQL: %s", out.SQL)
	}
}

func TestPlanSearchEmptySearchableFields(t *testing.T) {
	b := patientBinding()
	b.SearchableFields = nil
	_, _, err := obda.PlanSearch(b, "t1", "flu")
	if !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("err=%v want ErrUnsupportedCapability", err)
	}
}

func namesOf(ids []sqlast.Identifier) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.Name
	}
	return out
}

func eqStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPlanCreatePutsValuesInArgs(t *testing.T) {
	ins, args, err := obda.PlanCreateObject(patientBinding(), []string{"tenant_id", "patient_name"}, []any{"t1", "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[1] != "Ada" {
		t.Fatalf("args=%v", args)
	}
	if _, ok := ins.Values[0].(sqlast.Param); !ok {
		t.Fatalf("values[0]=%T", ins.Values[0])
	}
}

func TestPlanQueryUnknownField(t *testing.T) {
	_, _, err := obda.PlanQuery(patientBinding(), "t1", spi.FilterExpression{Field: "nope", Operator: "eq", Value: 1})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("err=%v", err)
	}
}

func TestPlanQueryUnsupportedOperator(t *testing.T) {
	b := patientBinding()
	// Non-eq operators must be rejected, not silently compiled as eq.
	_, _, err := obda.PlanQuery(b, "t1", spi.FilterExpression{Field: "patient_id", Operator: "gt", Value: 1})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("gt operator should return ErrInvalidMapping, got %v", err)
	}
	// "eq" operator passes.
	if _, _, err := obda.PlanQuery(b, "t1", spi.FilterExpression{Field: "patient_id", Operator: "eq", Value: 1}); err != nil {
		t.Fatalf("eq operator should pass, got %v", err)
	}
	// Empty operator defaults to eq and passes.
	if _, _, err := obda.PlanQuery(b, "t1", spi.FilterExpression{Field: "patient_id", Value: 1}); err != nil {
		t.Fatalf("empty operator should default to eq, got %v", err)
	}
	// And compound must be rejected (only single-leaf eq supported).
	_, _, err = obda.PlanQuery(b, "t1", spi.FilterExpression{And: []spi.FilterExpression{
		{Field: "patient_id", Operator: "eq", Value: 1},
	}})
	if !errors.Is(err, spi.ErrInvalidMapping) {
		t.Fatalf("And compound should return ErrInvalidMapping, got %v", err)
	}
}

func TestPlanGetLinksJoinUsesParams(t *testing.T) {
	sel, args, err := obda.PlanGetLinksJoin(obda.LinkJoinBinding{
		LinkTable:     "admission",
		LinkTenant:    "tenant_id",
		EndpointCol:   "from_id",
		PeerFKCol:     "to_id",
		PeerTable:     "ward",
		PeerIDCol:     "id",
		PeerTenantCol: "tenant_id",
		SelectColumns: []string{"id", "from_id", "to_id"},
	}, "t1", "o1")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[0] != "t1" || args[1] != "o1" {
		t.Fatalf("args=%v", args)
	}
	if len(sel.Joins) != 1 || sel.Joins[0].Table.Name != "ward" {
		t.Fatalf("joins=%+v", sel.Joins)
	}
}

func TestPlanGetLinksJoinInlineOutbound(t *testing.T) {
	sel, args, err := obda.PlanGetLinksJoin(obda.LinkJoinBinding{
		LinkTable:     "book",
		LinkTenant:    "tenant_id",
		EndpointCol:   "id",
		PeerTable:     "member",
		PeerIDCol:     "id",
		PeerTenantCol: "tenant_id",
		SelectColumns: []string{"id", "owner_id"},
		Inline:        true,
		FKColumn:      "owner_id",
		HostPKCol:     "id",
	}, "t1", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[1] != "b1" {
		t.Fatalf("args=%v", args)
	}
	if sel.From.Name != "book" {
		t.Fatalf("from=%s", sel.From.Name)
	}
	if len(sel.Joins) != 1 || sel.Joins[0].Table.Name != "member" {
		t.Fatalf("joins=%+v", sel.Joins)
	}
	if sel.Joins[0].Table.Name == "owned_by" {
		t.Fatal("must not join owned_by")
	}
	if !hasNotNull(sel.Where, "l", "owner_id") {
		t.Fatalf("outbound wants FK IS NOT NULL: %+v", sel.Where)
	}
	if !hasNull(sel.Where, "l", "deleted_at") || !hasNull(sel.Where, "p", "deleted_at") {
		t.Fatalf("soft-delete: %+v", sel.Where)
	}
}

func TestPlanGetLinksJoinInlineInbound(t *testing.T) {
	sel, _, err := obda.PlanGetLinksJoin(obda.LinkJoinBinding{
		LinkTable:     "book",
		LinkTenant:    "tenant_id",
		EndpointCol:   "owner_id",
		PeerTable:     "member",
		PeerIDCol:     "id",
		PeerTenantCol: "tenant_id",
		SelectColumns: []string{"id", "owner_id"},
		Inline:        true,
		FKColumn:      "owner_id",
		HostPKCol:     "id",
	}, "t1", "m1")
	if err != nil {
		t.Fatal(err)
	}
	if sel.From.Name != "book" || sel.Joins[0].Table.Name != "member" {
		t.Fatalf("from=%s joins=%+v", sel.From.Name, sel.Joins)
	}
	if hasNotNull(sel.Where, "l", "owner_id") {
		t.Fatal("inbound must not require FK IS NOT NULL beyond equality")
	}
}

func TestPlanGetLinksJoinInlineIncludeDeleted(t *testing.T) {
	sel, _, err := obda.PlanGetLinksJoin(obda.LinkJoinBinding{
		LinkTable:       "book",
		LinkTenant:      "tenant_id",
		EndpointCol:     "id",
		PeerTable:       "member",
		PeerIDCol:       "id",
		PeerTenantCol:   "tenant_id",
		Inline:          true,
		FKColumn:        "owner_id",
		HostPKCol:       "id",
		OmitLinkDeleted: true,
		OmitPeerDeleted: true,
	}, "t1", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if hasNull(sel.Where, "l", "deleted_at") || hasNull(sel.Where, "p", "deleted_at") {
		t.Fatalf("includeDeleted: %+v", sel.Where)
	}
}

func TestPlanCreateReadOnly(t *testing.T) {
	b := patientBinding()
	b.Writable = false
	_, _, err := obda.PlanCreateObject(b, []string{"patient_name"}, []any{"x"})
	if !errors.Is(err, spi.ErrReadOnlyMapping) {
		t.Fatalf("err=%v", err)
	}
}

func admittedHop(omitLink, omitTarget bool) obda.TraverseHop {
	return obda.TraverseHop{
		Direction:         "outbound",
		LinkTable:         "admission",
		LinkTenant:        "tenant_id",
		LinkIdentityCol:   "id",
		FromCol:           "from_id",
		ToCol:             "to_id",
		PrevIDCol:         "id",
		PrevTenantCol:     "tenant_id",
		TargetTable:       "ward",
		TargetIDCol:       "id",
		TargetTenantCol:   "tenant_id",
		TargetSelect:      []string{"id", "ward_name", "tenant_id"},
		OmitLinkDeleted:   omitLink,
		OmitTargetDeleted: omitTarget,
	}
}

func startPatient() obda.ObjectBinding {
	return obda.ObjectBinding{
		Table:           "patient",
		TenantColumn:    "tenant_id",
		IdentityColumns: []string{"id"},
		SelectColumns:   []string{"id", "patient_name", "tenant_id"},
	}
}

func TestPlanTraverseOneHopOutbound(t *testing.T) {
	sel, args, err := obda.PlanTraverse(startPatient(), []obda.TraverseHop{admittedHop(false, false)}, "t1", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[0] != "t1" || args[1] != "p1" {
		t.Fatalf("args=%v", args)
	}
	if sel.From.Name != "patient" || sel.As != "s0" {
		t.Fatalf("from=%+v as=%s", sel.From, sel.As)
	}
	if len(sel.Joins) != 2 {
		t.Fatalf("joins=%d", len(sel.Joins))
	}
	if sel.Joins[0].Table.Name != "admission" || sel.Joins[1].Table.Name != "ward" {
		t.Fatalf("joins=%+v", sel.Joins)
	}
	for _, c := range sel.Columns {
		id, ok := c.(sqlast.Identifier)
		if !ok || id.Qualifier != "s1" {
			t.Fatalf("column=%+v", c)
		}
	}
	if !hasNull(sel.Where, "l0", "deleted_at") || !hasNull(sel.Where, "s1", "deleted_at") {
		t.Fatalf("missing deleted_at: %+v", sel.Where)
	}
	if hasNull(sel.Where, "s0", "deleted_at") {
		t.Fatal("start table must not filter deleted_at")
	}
	if len(sel.Order) < 2 {
		t.Fatalf("order=%+v", sel.Order)
	}
	if sel.Order[0].Field.Qualifier != "s1" || sel.Order[0].Field.Name != "id" {
		t.Fatalf("terminal order=%+v", sel.Order[0])
	}
	if sel.Order[1].Field.Qualifier != "l0" || sel.Order[1].Field.Name != "id" {
		t.Fatalf("link order=%+v", sel.Order[1])
	}
}

func TestPlanTraverseTwoHops(t *testing.T) {
	hops := []obda.TraverseHop{
		admittedHop(false, false),
		{
			Direction:         "outbound",
			LinkTable:         "ward_trust",
			LinkTenant:        "tenant_id",
			LinkIdentityCol:   "id",
			FromCol:           "from_id",
			ToCol:             "to_id",
			PrevIDCol:         "id",
			PrevTenantCol:     "tenant_id",
			TargetTable:       "trust",
			TargetIDCol:       "id",
			TargetTenantCol:   "tenant_id",
			TargetSelect:      []string{"id", "trust_name"},
			OmitLinkDeleted:   false,
			OmitTargetDeleted: false,
		},
	}
	sel, _, err := obda.PlanTraverse(startPatient(), hops, "t1", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sel.Joins) != 4 {
		t.Fatalf("joins=%d", len(sel.Joins))
	}
	if sel.Joins[2].Table.Name != "ward_trust" || sel.Joins[3].Table.Name != "trust" {
		t.Fatalf("joins=%+v", sel.Joins)
	}
	for _, c := range sel.Columns {
		id, ok := c.(sqlast.Identifier)
		if !ok || id.Qualifier != "s2" {
			t.Fatalf("column=%+v", c)
		}
	}
}

func TestPlanTraverseIncludeDeletedOmitsNulls(t *testing.T) {
	sel, _, err := obda.PlanTraverse(startPatient(), []obda.TraverseHop{admittedHop(true, true)}, "t1", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if hasNull(sel.Where, "l0", "deleted_at") || hasNull(sel.Where, "s1", "deleted_at") {
		t.Fatalf("includeDeleted still has deleted_at: %+v", sel.Where)
	}
}

func TestPlanTraverseInboundSwapsEndpoint(t *testing.T) {
	h := admittedHop(true, true)
	h.Direction = "inbound"
	h.TargetTable = "patient"
	h.TargetSelect = []string{"id"}
	h.PrevIDCol = "id"
	sel, _, err := obda.PlanTraverse(obda.ObjectBinding{
		Table: "ward", TenantColumn: "tenant_id", IdentityColumns: []string{"id"},
	}, []obda.TraverseHop{h}, "t1", "w1")
	if err != nil {
		t.Fatal(err)
	}
	on := sel.Joins[0].On
	if !hasColEq(on, "l0", "to_id", "s0", "id") {
		t.Fatalf("inbound endpoint not to_id: %+v", on)
	}
}

func ownedByInlineHop() obda.TraverseHop {
	return obda.TraverseHop{
		Direction:         "outbound",
		Inline:            true,
		FKColumn:          "owner_id",
		FKOnPrev:          true,
		LinkIdentityCol:   "id",
		PrevIDCol:         "id",
		PrevTenantCol:     "tenant_id",
		TargetTable:       "member",
		TargetIDCol:       "id",
		TargetTenantCol:   "tenant_id",
		TargetSelect:      []string{"id", "name"},
		OmitLinkDeleted:   false,
		OmitTargetDeleted: false,
	}
}

func TestPlanTraverseInlineOneHop(t *testing.T) {
	sel, args, err := obda.PlanTraverse(obda.ObjectBinding{
		Table: "book", TenantColumn: "tenant_id", IdentityColumns: []string{"id"},
	}, []obda.TraverseHop{ownedByInlineHop()}, "t1", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 {
		t.Fatalf("args=%v", args)
	}
	if len(sel.Joins) != 1 || sel.Joins[0].Table.Name != "member" {
		t.Fatalf("joins=%+v", sel.Joins)
	}
	if !hasColEq(sel.Joins[0].On, "s1", "id", "s0", "owner_id") {
		t.Fatalf("on=%+v", sel.Joins[0].On)
	}
	for _, j := range sel.Joins {
		if j.Table.Name == "owned_by" {
			t.Fatal("must not join owned_by")
		}
	}
}

func TestPlanTraverseInlineThenTable(t *testing.T) {
	hops := []obda.TraverseHop{
		ownedByInlineHop(),
		{
			Direction:         "outbound",
			LinkTable:         "borrowed_by",
			LinkTenant:        "tenant_id",
			LinkIdentityCol:   "id",
			FromCol:           "from_id",
			ToCol:             "to_id",
			PrevIDCol:         "id",
			PrevTenantCol:     "tenant_id",
			TargetTable:       "loan",
			TargetIDCol:       "id",
			TargetTenantCol:   "tenant_id",
			TargetSelect:      []string{"id"},
			OmitLinkDeleted:   false,
			OmitTargetDeleted: false,
		},
	}
	sel, _, err := obda.PlanTraverse(obda.ObjectBinding{
		Table: "book", TenantColumn: "tenant_id", IdentityColumns: []string{"id"},
	}, hops, "t1", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sel.Joins) != 3 {
		t.Fatalf("joins=%d %+v", len(sel.Joins), sel.Joins)
	}
	if sel.Joins[0].Table.Name != "member" || sel.Joins[1].Table.Name != "borrowed_by" || sel.Joins[2].Table.Name != "loan" {
		t.Fatalf("joins=%+v", sel.Joins)
	}
	for _, j := range sel.Joins {
		if j.Table.Name == "owned_by" {
			t.Fatal("inline hop must not name owned_by")
		}
	}
}

func TestPlanTraverseInlineIncludeDeleted(t *testing.T) {
	h := ownedByInlineHop()
	h.OmitLinkDeleted = true
	h.OmitTargetDeleted = true
	sel, _, err := obda.PlanTraverse(obda.ObjectBinding{
		Table: "book", TenantColumn: "tenant_id", IdentityColumns: []string{"id"},
	}, []obda.TraverseHop{h}, "t1", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if hasNull(sel.Where, "s0", "deleted_at") || hasNull(sel.Where, "s1", "deleted_at") {
		t.Fatalf("includeDeleted: %+v", sel.Where)
	}
}

func hasNull(p *sqlast.Predicate, qual, name string) bool {
	if p == nil {
		return false
	}
	if p.Op == "is_null" && p.Field != nil && p.Field.Qualifier == qual && p.Field.Name == name {
		return true
	}
	for _, c := range p.Children {
		if hasNull(c, qual, name) {
			return true
		}
	}
	return false
}

func hasNotNull(p *sqlast.Predicate, qual, name string) bool {
	if p == nil {
		return false
	}
	if p.Op == "is_not_null" && p.Field != nil && p.Field.Qualifier == qual && p.Field.Name == name {
		return true
	}
	for _, c := range p.Children {
		if hasNotNull(c, qual, name) {
			return true
		}
	}
	return false
}

func hasColEq(p *sqlast.Predicate, aq, an, bq, bn string) bool {
	if p == nil {
		return false
	}
	if p.Op == "col_eq" && p.Field != nil && p.Other != nil &&
		p.Field.Qualifier == aq && p.Field.Name == an &&
		p.Other.Qualifier == bq && p.Other.Name == bn {
		return true
	}
	for _, c := range p.Children {
		if hasColEq(c, aq, an, bq, bn) {
			return true
		}
	}
	return false
}
