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
		Table:           "patient",
		TenantColumn:    "tenant_id",
		IdentityColumns: []string{"patient_id"},
		SelectColumns:   []string{"patient_id", "patient_name", "tenant_id"},
		Writable:        true,
		SearchIndex:     "patient_search",
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
	if len(args) != 2 {
		t.Fatalf("args=%v", args)
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

func TestPlanCreateReadOnly(t *testing.T) {
	b := patientBinding()
	b.Writable = false
	_, _, err := obda.PlanCreateObject(b, []string{"patient_name"}, []any{"x"})
	if !errors.Is(err, spi.ErrReadOnlyMapping) {
		t.Fatalf("err=%v", err)
	}
}
