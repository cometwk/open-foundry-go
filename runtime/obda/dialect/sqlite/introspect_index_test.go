package sqlite_test

import (
	"context"
	"testing"

	"github.com/openfoundry/runtime/obda"
	sqlitedialect "github.com/openfoundry/runtime/obda/dialect/sqlite"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

func TestInspectIndexesPartialUnique(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	compiled := hospitalCompiled(obda.OmitFlags{}, spi.CardinalityManyToOne)
	stmts, err := sqlitedialect.MappedTableStatements(compiled)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := sqlitedialect.InspectIndexes(ctx, db, sqlast.Identifier{Name: "admission"})
	if err != nil {
		t.Fatal(err)
	}
	if !sqlitedialect.HasUniqueIndex(idx, []string{"tenant_id", "from_id"}, true) {
		t.Fatalf("missing partial unique: %+v", idx)
	}
	if sqlitedialect.HasUniqueIndex(idx, []string{"tenant_id", "to_id"}, true) {
		t.Fatal("unexpected to_id unique for MANY_TO_ONE")
	}
}

func TestInspectIndexesOmitDeletedAt(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	compiled := hospitalCompiled(obda.OmitFlags{DeletedAt: true}, spi.CardinalityManyToOne)
	stmts, err := sqlitedialect.MappedTableStatements(compiled)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := sqlitedialect.InspectIndexes(ctx, db, sqlast.Identifier{Name: "admission"})
	if err != nil {
		t.Fatal(err)
	}
	if !sqlitedialect.HasUniqueIndex(idx, []string{"tenant_id", "from_id"}, false) {
		t.Fatalf("missing unique: %+v", idx)
	}
	if sqlitedialect.HasUniqueIndex(idx, []string{"tenant_id", "from_id"}, true) {
		t.Fatal("omitted deletedAt unique must not require deleted_at predicate")
	}
}

func TestTableExistsMissing(t *testing.T) {
	db := openDB(t)
	ok, err := sqlitedialect.TableExists(context.Background(), db, sqlast.Identifier{Name: "patient"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected missing")
	}
	if _, err := sqlitedialect.InspectTable(context.Background(), db, sqlast.Identifier{Name: "patient"}); err == nil {
		t.Fatal("InspectTable on missing table must fail")
	}
}
