package obda

import (
	"sort"

	"github.com/openfoundry/runtime/spi"
)

// Physical is the dialect-neutral mapped-table expectation derived from Compiled.
type Physical struct {
	Tables []PhysicalTable
}

// PhysicalTable is one mapped relation: required columns and cardinality UNIQUEs.
type PhysicalTable struct {
	Name    string
	Columns []PhysicalColumn
	Uniques []UniqueSpec
}

// PhysicalColumn is a mapped column. SQL types stay in the dialect.
type PhysicalColumn struct {
	Name     string
	Nullable bool
}

// UniqueSpec is a cardinality unique key. ExcludeSoftDeleted means only live rows collide.
type UniqueSpec struct {
	Columns            []string
	ExcludeSoftDeleted bool
}

// ColumnNames returns column names in declaration order.
func (t PhysicalTable) ColumnNames() []string {
	out := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		out[i] = c.Name
	}
	return out
}

// PhysicalSchema derives the physical expectation. It contains no SQL types or dialect syntax.
func PhysicalSchema(compiled *Compiled) Physical {
	if compiled == nil {
		return Physical{}
	}
	inlineByHost := inlineLinksByHostTable(compiled)
	var out Physical
	modelNames := make([]string, 0, len(compiled.Models))
	for name := range compiled.Models {
		modelNames = append(modelNames, name)
	}
	sort.Strings(modelNames)
	for _, name := range modelNames {
		m := compiled.Models[name]
		cols, uniques := hostTableShape(m, inlineByHost[m.Table])
		out.Tables = append(out.Tables, PhysicalTable{
			Name:    m.Table,
			Columns: cols,
			Uniques: uniques,
		})
	}
	linkNames := make([]string, 0, len(compiled.Links))
	for name := range compiled.Links {
		linkNames = append(linkNames, name)
	}
	sort.Strings(linkNames)
	for _, name := range linkNames {
		l := compiled.Links[name]
		if l.Inline {
			continue
		}
		out.Tables = append(out.Tables, PhysicalTable{
			Name:    l.Table,
			Columns: linkColumns(l),
			Uniques: uniqueSpecs(l),
		})
	}
	return out
}

func inlineLinksByHostTable(compiled *Compiled) map[string][]*CompiledLink {
	out := map[string][]*CompiledLink{}
	names := make([]string, 0, len(compiled.Links))
	for name, l := range compiled.Links {
		if l != nil && l.Inline {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		l := compiled.Links[name]
		host := compiled.Models[l.HostModel]
		if host == nil {
			continue
		}
		out[host.Table] = append(out[host.Table], l)
	}
	return out
}

func hostTableShape(m *CompiledModel, inlines []*CompiledLink) ([]PhysicalColumn, []UniqueSpec) {
	cols := modelBusinessColumns(m)
	var uniques []UniqueSpec
	for _, l := range inlines {
		cols = appendColumn(cols, PhysicalColumn{Name: l.FKColumn, Nullable: l.FKNullable})
		if l.Cardinality == spi.CardinalityOneToOne {
			uniques = append(uniques, UniqueSpec{
				Columns:            appendTenant(l.TenantColumn, []string{l.FKColumn}),
				ExcludeSoftDeleted: !m.Omit.DeletedAt,
			})
		}
	}
	return appendSystemColumns(cols, m.Omit), uniques
}

func modelBusinessColumns(m *CompiledModel) []PhysicalColumn {
	var cols []PhysicalColumn
	cols = appendRequired(cols, m.IdentityColumns...)
	if m.TenantColumn != "" {
		cols = appendRequired(cols, m.TenantColumn)
	}
	for _, f := range m.Fields {
		cols = appendRequired(cols, f.Column)
	}
	return cols
}

func linkColumns(l *CompiledLink) []PhysicalColumn {
	var cols []PhysicalColumn
	cols = appendRequired(cols, l.IdentityColumns...)
	if l.TenantColumn != "" {
		cols = appendRequired(cols, l.TenantColumn)
	}
	cols = appendRequired(cols, l.FromColumns...)
	cols = appendRequired(cols, l.ToColumns...)
	for _, f := range l.Fields {
		cols = appendRequired(cols, f.Column)
	}
	return appendSystemColumns(cols, l.Omit)
}

func appendSystemColumns(cols []PhysicalColumn, omit OmitFlags) []PhysicalColumn {
	if !omit.Version {
		cols = appendRequired(cols, "version")
	}
	if !omit.CreatedAt {
		cols = appendRequired(cols, "created_at")
	}
	if !omit.UpdatedAt {
		cols = appendRequired(cols, "updated_at")
	}
	if !omit.DeletedAt {
		cols = appendColumn(cols, PhysicalColumn{Name: "deleted_at", Nullable: true})
	}
	return cols
}

func uniqueSpecs(l *CompiledLink) []UniqueSpec {
	excl := !l.Omit.DeletedAt
	switch l.Cardinality {
	case spi.CardinalityManyToOne:
		return []UniqueSpec{{Columns: appendTenant(l.TenantColumn, l.FromColumns), ExcludeSoftDeleted: excl}}
	case spi.CardinalityOneToMany:
		return []UniqueSpec{{Columns: appendTenant(l.TenantColumn, l.ToColumns), ExcludeSoftDeleted: excl}}
	case spi.CardinalityOneToOne:
		return []UniqueSpec{
			{Columns: appendTenant(l.TenantColumn, l.FromColumns), ExcludeSoftDeleted: excl},
			{Columns: appendTenant(l.TenantColumn, l.ToColumns), ExcludeSoftDeleted: excl},
		}
	default:
		return nil
	}
}

func appendTenant(tenant string, cols []string) []string {
	if tenant == "" {
		return append([]string(nil), cols...)
	}
	out := make([]string, 0, len(cols)+1)
	out = append(out, tenant)
	return append(out, cols...)
}

func appendRequired(dst []PhysicalColumn, names ...string) []PhysicalColumn {
	for _, n := range names {
		dst = appendColumn(dst, PhysicalColumn{Name: n, Nullable: false})
	}
	return dst
}

func appendColumn(dst []PhysicalColumn, col PhysicalColumn) []PhysicalColumn {
	if col.Name == "" {
		return dst
	}
	for _, existing := range dst {
		if existing.Name == col.Name {
			return dst
		}
	}
	return append(dst, col)
}
