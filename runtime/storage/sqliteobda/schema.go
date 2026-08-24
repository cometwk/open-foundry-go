package sqliteobda

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/openfoundry/runtime/obda"
	sqlitedialect "github.com/openfoundry/runtime/obda/dialect/sqlite"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

// InitMappedSchema executes mapped-table CREATE statements. ApplySchema never
// calls this; tests and operators opt in separately.
func InitMappedSchema(db *sql.DB, compiled *obda.Compiled) error {
	stmts, err := sqlitedialect.MappedTableStatements(compiled)
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
	names := make([]string, 0, len(compiled.Models))
	for name := range compiled.Models {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		m := compiled.Models[name]
		if err := p.verifyTable(ctx, m.Table, requiredModelColumns(m)); err != nil {
			return err
		}
	}
	linkNames := make([]string, 0, len(compiled.Links))
	for name := range compiled.Links {
		linkNames = append(linkNames, name)
	}
	sort.Strings(linkNames)
	for _, name := range linkNames {
		l := compiled.Links[name]
		if err := p.verifyTable(ctx, l.Table, requiredLinkColumns(l)); err != nil {
			return err
		}
		if err := p.verifyCardinalityIndexes(ctx, l); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) verifyTable(ctx context.Context, table string, required []string) error {
	id := sqlast.Identifier{Name: table}
	ok, err := sqlitedialect.TableExists(ctx, p.db, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: table %q does not exist", spi.ErrInvalidMapping, table)
	}
	snap, err := sqlitedialect.InspectTable(ctx, p.db, id)
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

func (p *Provider) verifyCardinalityIndexes(ctx context.Context, l *obda.CompiledLink) error {
	specs := uniqueSpecs(l)
	if len(specs) == 0 {
		return nil
	}
	idx, err := sqlitedialect.InspectIndexes(ctx, p.db, sqlast.Identifier{Name: l.Table})
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if !sqlitedialect.HasUniqueIndex(idx, spec, !l.Omit.DeletedAt) {
			return fmt.Errorf("%w: table %q missing unique index on %v", spi.ErrSourceSchemaDrift, l.Table, spec)
		}
	}
	return nil
}

func uniqueSpecs(l *obda.CompiledLink) [][]string {
	switch l.Cardinality {
	case spi.CardinalityManyToOne:
		return [][]string{appendTenantCols(l.TenantColumn, l.FromColumns)}
	case spi.CardinalityOneToMany:
		return [][]string{appendTenantCols(l.TenantColumn, l.ToColumns)}
	case spi.CardinalityOneToOne:
		return [][]string{
			appendTenantCols(l.TenantColumn, l.FromColumns),
			appendTenantCols(l.TenantColumn, l.ToColumns),
		}
	default:
		return nil
	}
}

func appendTenantCols(tenant string, cols []string) []string {
	if tenant == "" {
		return append([]string(nil), cols...)
	}
	out := make([]string, 0, len(cols)+1)
	out = append(out, tenant)
	return append(out, cols...)
}

func requiredModelColumns(m *obda.CompiledModel) []string {
	return requiredColumns(m.IdentityColumns, m.TenantColumn, m.Fields, m.Omit)
}

func requiredLinkColumns(l *obda.CompiledLink) []string {
	cols := requiredColumns(l.IdentityColumns, l.TenantColumn, l.Fields, l.Omit)
	cols = appendUnique(cols, l.FromColumns...)
	cols = appendUnique(cols, l.ToColumns...)
	return cols
}

func requiredColumns(identity []string, tenant string, fields []obda.CompiledField, omit obda.OmitFlags) []string {
	var cols []string
	cols = appendUnique(cols, identity...)
	if tenant != "" {
		cols = appendUnique(cols, tenant)
	}
	for _, f := range fields {
		cols = appendUnique(cols, f.Column)
	}
	if !omit.Version {
		cols = appendUnique(cols, "version")
	}
	if !omit.CreatedAt {
		cols = appendUnique(cols, "created_at")
	}
	if !omit.UpdatedAt {
		cols = appendUnique(cols, "updated_at")
	}
	if !omit.DeletedAt {
		cols = appendUnique(cols, "deleted_at")
	}
	return cols
}

func appendUnique(dst []string, names ...string) []string {
	seen := map[string]struct{}{}
	for _, n := range dst {
		seen[n] = struct{}{}
	}
	for _, n := range names {
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		dst = append(dst, n)
	}
	return dst
}
