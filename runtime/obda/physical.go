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
	Columns []string
	Uniques []UniqueSpec
}

// UniqueSpec is a cardinality unique key. ExcludeSoftDeleted means only live rows collide.
type UniqueSpec struct {
	Columns            []string
	ExcludeSoftDeleted bool
}

// PhysicalSchema derives the physical expectation. It contains no SQL types or dialect syntax.
func PhysicalSchema(compiled *Compiled) Physical {
	if compiled == nil {
		return Physical{}
	}
	var out Physical
	modelNames := make([]string, 0, len(compiled.Models))
	for name := range compiled.Models {
		modelNames = append(modelNames, name)
	}
	sort.Strings(modelNames)
	for _, name := range modelNames {
		m := compiled.Models[name]
		out.Tables = append(out.Tables, PhysicalTable{
			Name:    m.Table,
			Columns: modelColumns(m),
		})
	}
	linkNames := make([]string, 0, len(compiled.Links))
	for name := range compiled.Links {
		linkNames = append(linkNames, name)
	}
	sort.Strings(linkNames)
	for _, name := range linkNames {
		l := compiled.Links[name]
		out.Tables = append(out.Tables, PhysicalTable{
			Name:    l.Table,
			Columns: linkColumns(l),
			Uniques: uniqueSpecs(l),
		})
	}
	return out
}

func modelColumns(m *CompiledModel) []string {
	var cols []string
	cols = appendUnique(cols, m.IdentityColumns...)
	if m.TenantColumn != "" {
		cols = appendUnique(cols, m.TenantColumn)
	}
	for _, f := range m.Fields {
		cols = appendUnique(cols, f.Column)
	}
	return appendSystemColumns(cols, m.Omit)
}

func linkColumns(l *CompiledLink) []string {
	var cols []string
	cols = appendUnique(cols, l.IdentityColumns...)
	if l.TenantColumn != "" {
		cols = appendUnique(cols, l.TenantColumn)
	}
	cols = appendUnique(cols, l.FromColumns...)
	cols = appendUnique(cols, l.ToColumns...)
	for _, f := range l.Fields {
		cols = appendUnique(cols, f.Column)
	}
	return appendSystemColumns(cols, l.Omit)
}

func appendSystemColumns(cols []string, omit OmitFlags) []string {
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
