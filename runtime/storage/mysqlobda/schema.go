package mysqlobda

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/openfoundry/runtime/obda"
	mysqldialect "github.com/openfoundry/runtime/obda/dialect/mysql"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

// InitMappedSchema executes mapped-table CREATE statements. ApplySchema never
// calls this; tests and operators opt in separately. MySQL has no
// "CREATE INDEX IF NOT EXISTS", so it only runs against a fresh schema.
func InitMappedSchema(db *sql.DB, compiled *obda.Compiled) error {
	stmts, err := mysqldialect.MappedTableStatements(compiled)
	if err != nil {
		return err
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) verifyMappedSchema(compiled *obda.Compiled) error {
	ctx := context.Background()
	for _, tbl := range obda.PhysicalSchema(compiled).Tables {
		if err := p.verifyTable(ctx, tbl.Name, tbl.ColumnNames()); err != nil {
			return err
		}
		if err := p.verifyUniques(ctx, tbl); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) verifyTable(ctx context.Context, table string, required []string) error {
	id := sqlast.Identifier{Name: table}
	ok, err := mysqldialect.TableExists(ctx, p.db, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: table %q does not exist", spi.ErrInvalidMapping, table)
	}
	snap, err := mysqldialect.InspectTable(ctx, p.db, id)
	if err != nil {
		return err
	}
	have := map[string]struct{}{}
	for _, c := range snap.Columns {
		have[c.Name] = struct{}{}
	}
	for _, col := range required {
		if _, ok := have[col]; !ok {
			return fmt.Errorf("%w: table %q missing column %q", spi.ErrSourceSchemaDrift, table, col)
		}
	}
	return nil
}

func (p *Provider) verifyUniques(ctx context.Context, tbl obda.PhysicalTable) error {
	if len(tbl.Uniques) == 0 {
		return nil
	}
	idx, err := mysqldialect.InspectIndexes(ctx, p.db, sqlast.Identifier{Name: tbl.Name})
	if err != nil {
		return err
	}
	for _, spec := range tbl.Uniques {
		if !mysqldialect.HasUniqueIndex(idx, spec.Columns, spec.ExcludeSoftDeleted) {
			return fmt.Errorf("%w: table %q missing unique index on %v", spi.ErrSourceSchemaDrift, tbl.Name, spec.Columns)
		}
	}
	return nil
}
