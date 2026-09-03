package mysql_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/obda"
	mysqldialect "github.com/openfoundry/runtime/obda/dialect/mysql"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

func TestQuoteIdentifier(t *testing.T) {
	d := mysqldialect.New()
	got, err := d.QuoteIdentifier(sqlast.Identifier{Name: "patient"})
	if err != nil || got != "`patient`" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	got, err = d.QuoteIdentifier(sqlast.Identifier{Qualifier: "l", Name: "to_id"})
	if err != nil || got != "`l`.`to_id`" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err := d.QuoteIdentifier(sqlast.Identifier{Name: "patient;drop"}); err == nil {
		t.Fatal("expected reject")
	}
	if _, err := d.QuoteIdentifier(sqlast.Identifier{Name: "pa`tient"}); err == nil {
		t.Fatal("expected reject quote")
	}
}

func TestPlaceholderIsQuestionMark(t *testing.T) {
	if got := mysqldialect.New().Placeholder(3); got != "?" {
		t.Fatalf("got=%q", got)
	}
}

func TestRenderSelectUsesBackticksAndParams(t *testing.T) {
	sel, args, err := obda.PlanGetObject(obda.ObjectBinding{
		Table:           "patient",
		TenantColumn:    "tenant_id",
		IdentityColumns: []string{"id"},
		SelectColumns:   []string{"patient_name"},
	}, "t1", []any{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := mysqldialect.New().Render(sel)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stmt.SQL, "`patient`") {
		t.Fatalf("sql=%s", stmt.SQL)
	}
	if strings.Contains(stmt.SQL, "t1") || strings.Contains(stmt.SQL, "p1") {
		t.Fatalf("value leaked into SQL: %s", stmt.SQL)
	}
	if len(args) != 2 {
		t.Fatalf("args=%v", args)
	}
}

func TestRenderTraverseChainedJoinShape(t *testing.T) {
	sel, _, err := obda.PlanTraverse(obda.ObjectBinding{
		Table:           "patient",
		TenantColumn:    "tenant_id",
		IdentityColumns: []string{"id"},
	}, []obda.TraverseHop{
		{
			Direction:       "outbound",
			LinkTable:       "admission",
			LinkTenant:      "tenant_id",
			LinkIdentityCol: "id",
			FromCol:         "from_id",
			ToCol:           "to_id",
			PrevIDCol:       "id",
			PrevTenantCol:   "tenant_id",
			TargetTable:     "ward",
			TargetIDCol:     "id",
			TargetTenantCol: "tenant_id",
			TargetSelect:    []string{"id", "tenant_id", "ward_name"},
		},
	}, "t1", "p1")
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := mysqldialect.New().Render(sel)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"INNER JOIN `admission` AS `l0`",
		"INNER JOIN `ward` AS `s1`",
		"`l0`.`deleted_at` IS NULL",
		"ORDER BY `s1`.`id` ASC",
	} {
		if !strings.Contains(stmt.SQL, want) {
			t.Fatalf("sql missing %q:\n%s", want, stmt.SQL)
		}
	}
}

func TestRenderInsertRejectsReturning(t *testing.T) {
	ins, _, err := obda.PlanCreateObject(obda.ObjectBinding{
		Table:        "patient",
		TenantColumn: "tenant_id",
		Writable:     true,
	}, []string{"id"}, []any{"p1"})
	if err != nil {
		t.Fatal(err)
	}
	ins.Returning = []sqlast.Identifier{{Name: "id"}}
	if _, err := mysqldialect.New().Render(ins); !errors.Is(err, spi.ErrUnsupportedCapability) {
		t.Fatalf("err=%v want ErrUnsupportedCapability", err)
	}
}

func TestRenderInsertPlain(t *testing.T) {
	ins, args, err := obda.PlanCreateObject(obda.ObjectBinding{
		Table:        "patient",
		TenantColumn: "tenant_id",
		Writable:     true,
	}, []string{"id", "tenant_id"}, []any{"p1", "t1"})
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := mysqldialect.New().Render(ins)
	if err != nil {
		t.Fatal(err)
	}
	if stmt.SQL != "INSERT INTO `patient` (`id`, `tenant_id`) VALUES (?, ?)" {
		t.Fatalf("sql=%s", stmt.SQL)
	}
	if len(args) != 2 {
		t.Fatalf("args=%v", args)
	}
}

func TestNormalizeBool(t *testing.T) {
	d := mysqldialect.New()
	v, err := d.NormalizeValue("Boolean", int64(1))
	if err != nil {
		t.Fatal(err)
	}
	if v != true {
		t.Fatalf("got=%v (%T)", v, v)
	}
	if v, err := d.NormalizeValue("Boolean", []byte("0")); err != nil || v != false {
		t.Fatalf("got=%v err=%v", v, err)
	}
}

func TestClassify(t *testing.T) {
	dup := errors.New("Error 1062: Duplicate entry 't1-p1' for key 'admission.admission_from_active'")
	if err := mysqldialect.Classify(dup); !errors.Is(err, spi.ErrCardinalityViolation) {
		t.Fatalf("err=%v want ErrCardinalityViolation", err)
	}
	ver := errors.New("Error 1062: Duplicate entry '1' for key 'patient.version_unique'")
	if err := mysqldialect.Classify(ver); !errors.Is(err, spi.ErrVersionConflict) {
		t.Fatalf("err=%v want ErrVersionConflict", err)
	}
	gone := errors.New("Error 1146: Table 'of_test.patient' doesn't exist")
	if err := mysqldialect.Classify(gone); !errors.Is(err, spi.ErrSourceSchemaDrift) {
		t.Fatalf("err=%v want ErrSourceSchemaDrift", err)
	}
	other := errors.New("Error 1049: Unknown database 'nope'")
	if err := mysqldialect.Classify(other); !errors.Is(err, spi.ErrSourceSchemaDrift) {
		t.Fatalf("err=%v want ErrSourceSchemaDrift", err)
	}
	if err := mysqldialect.Classify(errors.New("boom")); err.Error() != "boom" {
		t.Fatalf("unknown error must pass through: %v", err)
	}
	if err := mysqldialect.Classify(nil); err != nil {
		t.Fatalf("nil must stay nil: %v", err)
	}
}
