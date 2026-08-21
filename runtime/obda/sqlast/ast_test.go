package sqlast_test

import (
	"testing"

	"github.com/openfoundry/runtime/obda/sqlast"
)

func TestSelectHoldsIdentifiersNotSQL(t *testing.T) {
	s := sqlast.Select{
		From:    sqlast.Identifier{Name: "patient"},
		Columns: []sqlast.Expr{sqlast.Identifier{Name: "patient_id"}},
		Where: &sqlast.Predicate{
			Op:    "eq",
			Field: &sqlast.Identifier{Name: "tenant_id"},
			Value: sqlast.Param{Position: 1},
		},
	}
	if s.From.Name != "patient" {
		t.Fatalf("from=%q", s.From.Name)
	}
	if _, ok := s.Where.Value.(sqlast.Param); !ok {
		t.Fatalf("value=%T want Param", s.Where.Value)
	}
}
