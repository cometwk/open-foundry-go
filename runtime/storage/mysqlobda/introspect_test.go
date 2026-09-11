package mysqlobda_test

import (
	"context"
	"strings"
	"testing"

	mysqldialect "github.com/openfoundry/runtime/obda/dialect/mysql"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/storage/mysqlobda"
)

func TestHasUniqueIndex(t *testing.T) {
	indexes := []mysqlobda.Index{
		{Name: "PRIMARY", Unique: true, Columns: []string{"id"}},
		{Name: "admission_from_active", Unique: true, Columns: []string{"tenant_id", "from_id", mysqldialect.ActiveKeyColumn}},
		{Name: "plain", Unique: false, Columns: []string{"tenant_id", "from_id", mysqldialect.ActiveKeyColumn}},
	}
	if !mysqlobda.HasUniqueIndex(indexes, []string{"tenant_id", "from_id"}, true) {
		t.Fatal("active-key unique index should match")
	}
	if mysqlobda.HasUniqueIndex(indexes, []string{"tenant_id", "from_id"}, false) {
		t.Fatal("plain columns must not match when index carries of_active")
	}
	if mysqlobda.HasUniqueIndex(indexes[:1], []string{"tenant_id", "from_id"}, true) {
		t.Fatal("missing index must not match")
	}
}

func TestHasFulltextIndex(t *testing.T) {
	indexes := []mysqlobda.Index{
		{Name: "PRIMARY", Unique: true, Type: "BTREE", Columns: []string{"id"}},
		{Name: "ft_patient", Type: "FULLTEXT", Columns: []string{"patient_name", "city_name"}},
		{Name: "idx_btree", Type: "BTREE", Columns: []string{"patient_name", "city_name"}},
	}
	if !mysqlobda.HasFulltextIndex(indexes, []string{"city_name", "patient_name"}) {
		t.Fatal("FULLTEXT index with reordered columns should match")
	}
	onlyBtree := []mysqlobda.Index{indexes[0], indexes[2]}
	if mysqlobda.HasFulltextIndex(onlyBtree, []string{"patient_name", "city_name"}) {
		t.Fatal("BTREE index must not match FULLTEXT")
	}
	if mysqlobda.HasFulltextIndex(indexes, []string{"id"}) {
		t.Fatal("non-matching columns should not match")
	}
}

func TestInspectRejectsInvalidIdent(t *testing.T) {
	id := sqlast.Identifier{Name: "bad-name"}
	if _, err := mysqlobda.InspectTable(context.Background(), nil, id); err == nil || !strings.Contains(err.Error(), "invalid identifier") {
		t.Fatalf("InspectTable err=%v", err)
	}
	if _, err := mysqlobda.TableExists(context.Background(), nil, id); err == nil || !strings.Contains(err.Error(), "invalid identifier") {
		t.Fatalf("TableExists err=%v", err)
	}
	if _, err := mysqlobda.InspectIndexes(context.Background(), nil, id); err == nil || !strings.Contains(err.Error(), "invalid identifier") {
		t.Fatalf("InspectIndexes err=%v", err)
	}
}
