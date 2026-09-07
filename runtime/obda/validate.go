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
	if len(doc.Models) == 0 && len(doc.Links) == 0 {
		return fmt.Errorf("%w: models or links required", spi.ErrInvalidMapping)
	}
	for name, m := range doc.Models {
		if err := validateBinding(name, m.Relation, m.Access, m.Identity, m.Tenant, m.System, m.Fields); err != nil {
			return err
		}
	}
	for name, l := range doc.Links {
		if l.From.Object == "" || l.To.Object == "" {
			return fmt.Errorf("%w: link %q missing from/to object", spi.ErrInvalidMapping, name)
		}
		if l.Inline() {
			if err := validateInlineLink(name, l); err != nil {
				return err
			}
			continue
		}
		if err := validateBinding(name, l.Relation, l.Access, l.Identity, l.Tenant, l.System, l.Fields); err != nil {
			return err
		}
		if len(l.From.Columns) == 0 || len(l.To.Columns) == 0 {
			return fmt.Errorf("%w: link %q missing from", spi.ErrInvalidMapping, name)
		}
	}
	return nil
}

func validateBinding(name string, rel Relation, access string, id Identity, tenant Tenant, system System, fields map[string]Field) error {
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
	if id.Strategy != "direct" {
		return fmt.Errorf("%w: %q identity.strategy %q (direct only)", spi.ErrInvalidMapping, name, id.Strategy)
	}
	if len(id.Columns) == 0 && id.Insert != "generated" {
		return fmt.Errorf("%w: %q identity.columns empty", spi.ErrInvalidMapping, name)
	}
	if err := validateTenant(name, tenant); err != nil {
		return err
	}
	if system.Strategy != "native" {
		return fmt.Errorf("%w: %q system.strategy %q (native only)", spi.ErrInvalidMapping, name, system.Strategy)
	}
	if _, err := parseOmit(system.Omit); err != nil {
		return fmt.Errorf("%w: %q system.omit: %v", spi.ErrInvalidMapping, name, err)
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

func validateInlineLink(name string, l Link) error {
	if l.Relation.Kind != "" && l.Relation.Kind != "inline" {
		return fmt.Errorf("%w: %q relation.kind %q", spi.ErrInvalidMapping, name, l.Relation.Kind)
	}
	if l.Access != "read" && l.Access != "readWrite" && l.Access != "" {
		return fmt.Errorf("%w: %q access %q", spi.ErrInvalidMapping, name, l.Access)
	}
	fromN, toN := len(l.From.Columns), len(l.To.Columns)
	if (fromN == 0 && toN == 0) || (fromN > 0 && toN > 0) {
		return fmt.Errorf("%w: link %q inline needs FK columns on exactly one endpoint", spi.ErrInvalidMapping, name)
	}
	if fromN > 1 || toN > 1 {
		return fmt.Errorf("%w: link %q inline FK must be one column", spi.ErrInvalidMapping, name)
	}
	if l.Host != "" && l.Host != "from" && l.Host != "to" {
		return fmt.Errorf("%w: link %q host %q (from or to)", spi.ErrInvalidMapping, name, l.Host)
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
