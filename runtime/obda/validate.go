package obda

import (
	"fmt"
	"strings"

	"github.com/openfoundry/runtime/spi"
)

var writableTransforms = map[string]struct{}{
	"":              {},
	"prefix":        {},
	"suffix":        {},
	"trim":          {},
	"toUpper":       {},
	"toLower":       {},
	"map":           {},
	"parseDate":     {},
	"parseDateTime": {},
}

var readOnlyTransforms = map[string]struct{}{
	"coalesce": {},
}

// Validate checks mapping semantics that do not need a database.
// Dialect remains an opaque identifier; adapter binding happens at Open.
func Validate(doc *Document) error {
	if doc == nil {
		return fmt.Errorf("%w: empty document", spi.ErrInvalidMapping)
	}
	if doc.APIVersion != "openfoundry.io/obda/v1" {
		return fmt.Errorf("%w: unsupported apiVersion %q", spi.ErrInvalidMapping, doc.APIVersion)
	}
	if doc.Kind != "OBDAConfig" {
		return fmt.Errorf("%w: unsupported kind %q", spi.ErrInvalidMapping, doc.Kind)
	}
	if doc.Metadata.Name == "" {
		return fmt.Errorf("%w: metadata.name required", spi.ErrInvalidMapping)
	}
	if len(doc.Sources) == 0 {
		return fmt.Errorf("%w: sources required", spi.ErrInvalidMapping)
	}
	for name, src := range doc.Sources {
		if src.Kind != "" && src.Kind != "sql" {
			return fmt.Errorf("%w: source %q kind %q", spi.ErrInvalidMapping, name, src.Kind)
		}
		if src.Connection.DSNRef == "" {
			return fmt.Errorf("%w: source %q missing connection.dsnRef", spi.ErrInvalidMapping, name)
		}
	}
	if len(doc.Models) == 0 && len(doc.Links) == 0 {
		return fmt.Errorf("%w: models or links required", spi.ErrInvalidMapping)
	}
	for name, m := range doc.Models {
		if err := validateBinding(doc, name, m.SourceRef, m.Relation, m.Access, m.Identity, m.Tenant, m.System, m.Fields); err != nil {
			return err
		}
	}
	for name, l := range doc.Links {
		if err := validateBinding(doc, name, l.SourceRef, l.Relation, l.Access, l.Identity, l.Tenant, l.System, l.Fields); err != nil {
			return err
		}
		if l.From.Object == "" || len(l.From.Columns) == 0 {
			return fmt.Errorf("%w: link %q missing from", spi.ErrInvalidMapping, name)
		}
		if l.To.Object == "" || len(l.To.Columns) == 0 {
			return fmt.Errorf("%w: link %q missing to", spi.ErrInvalidMapping, name)
		}
	}
	return nil
}

func validateBinding(doc *Document, name, sourceRef string, rel Relation, access string, id Identity, tenant Tenant, system System, fields map[string]Field) error {
	if sourceRef == "" {
		return fmt.Errorf("%w: %q missing sourceRef", spi.ErrInvalidMapping, name)
	}
	if _, ok := doc.Sources[sourceRef]; !ok {
		return fmt.Errorf("%w: %q unknown sourceRef %q", spi.ErrInvalidMapping, name, sourceRef)
	}
	if rel.Name == "" {
		return fmt.Errorf("%w: %q missing relation.name", spi.ErrInvalidMapping, name)
	}
	kind := rel.Kind
	if kind == "" {
		kind = "table"
	}
	if kind != "table" && kind != "view" {
		return fmt.Errorf("%w: %q relation.kind %q", spi.ErrInvalidMapping, name, kind)
	}
	if cat := strings.TrimSpace(rel.Catalog); cat != "" && cat != "main" {
		return fmt.Errorf("%w: %q catalog %q (empty or main only)", spi.ErrInvalidMapping, name, rel.Catalog)
	}
	if access != "read" && access != "readWrite" {
		return fmt.Errorf("%w: %q access %q", spi.ErrInvalidMapping, name, access)
	}
	if access == "readWrite" && kind == "view" {
		return fmt.Errorf("%w: %q view cannot be readWrite", spi.ErrInvalidMapping, name)
	}
	if id.Strategy != "direct" && id.Strategy != "sidecar" {
		return fmt.Errorf("%w: %q identity.strategy %q", spi.ErrInvalidMapping, name, id.Strategy)
	}
	if len(id.Columns) == 0 && id.Insert != "generated" {
		return fmt.Errorf("%w: %q identity.columns empty", spi.ErrInvalidMapping, name)
	}
	if err := validateTenant(name, tenant); err != nil {
		return err
	}
	if system.Strategy != "sidecar" && system.Strategy != "native" {
		return fmt.Errorf("%w: %q system.strategy %q", spi.ErrInvalidMapping, name, system.Strategy)
	}
	writable := access == "readWrite"
	for fname, f := range fields {
		kind := f.Transform.Kind
		if kind == "" {
			continue
		}
		if _, ok := writableTransforms[kind]; ok {
			continue
		}
		if _, ok := readOnlyTransforms[kind]; ok {
			if writable {
				return fmt.Errorf("%w: %q field %q transform %q is not writable", spi.ErrInvalidMapping, name, fname, kind)
			}
			continue
		}
		return fmt.Errorf("%w: %q field %q transform %q", spi.ErrInvalidMapping, name, fname, kind)
	}
	return nil
}

func validateTenant(name string, tenant Tenant) error {
	switch tenant.Strategy {
	case "column":
		if tenant.Column == "" {
			return fmt.Errorf("%w: %q tenant.column required", spi.ErrInvalidMapping, name)
		}
	case "constant":
		if tenant.Value == "" {
			return fmt.Errorf("%w: %q tenant.value required", spi.ErrInvalidMapping, name)
		}
	case "connection":
		return fmt.Errorf("%w: %q tenant.strategy connection is not supported", spi.ErrInvalidMapping, name)
	case "":
		return fmt.Errorf("%w: %q missing tenant strategy", spi.ErrInvalidMapping, name)
	default:
		return fmt.Errorf("%w: %q tenant.strategy %q", spi.ErrInvalidMapping, name, tenant.Strategy)
	}
	return nil
}
