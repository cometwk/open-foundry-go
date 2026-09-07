package obda

import (
	"fmt"
	"sort"

	"github.com/openfoundry/runtime/spi"
)

func compileLink(name string, l Link, def spi.LinkTypeDefinition, objects map[string]spi.ObjectTypeDefinition, compiledModels map[string]*CompiledModel) (*CompiledLink, error) {
	wantInline, err := resolveInline(name, l, def)
	if err != nil {
		return nil, err
	}
	if wantInline {
		return compileInlineLink(name, l, def, objects, compiledModels)
	}
	return compileTableLink(name, l, def)
}

func resolveInline(name string, l Link, def spi.LinkTypeDefinition) (bool, error) {
	eligible := inlineEligible(def)
	switch l.Relation.Kind {
	case "inline":
		if !eligible {
			return false, fmt.Errorf("%w: link %q cannot be inline", spi.ErrInvalidMapping, name)
		}
		return true, nil
	case "table", "view":
		return false, nil
	case "":
		if def.Cardinality == spi.CardinalityManyToMany && !hasBusinessProperties(def) {
			return false, fmt.Errorf("%w: link %q MANY_TO_MANY requires kind: table", spi.ErrInvalidMapping, name)
		}
		if eligible {
			return true, nil
		}
		if l.Relation.Name == "" {
			return false, fmt.Errorf("%w: link %q requires kind: table", spi.ErrInvalidMapping, name)
		}
		return false, nil
	default:
		return false, fmt.Errorf("%w: link %q relation.kind %q", spi.ErrInvalidMapping, name, l.Relation.Kind)
	}
}

func inlineEligible(def spi.LinkTypeDefinition) bool {
	if hasBusinessProperties(def) {
		return false
	}
	switch def.Cardinality {
	case spi.CardinalityManyToOne, spi.CardinalityOneToMany, spi.CardinalityOneToOne:
		return true
	default:
		return false
	}
}

func hasBusinessProperties(def spi.LinkTypeDefinition) bool {
	for _, p := range def.Properties {
		if isLinkIdentityProperty(p) {
			continue
		}
		return true
	}
	return false
}

func isLinkIdentityProperty(p spi.PropertyDefinition) bool {
	return p.Name == "id" && (p.Type == "ID" || p.Type == "")
}

func compileTableLink(name string, l Link, def spi.LinkTypeDefinition) (*CompiledLink, error) {
	cl := &CompiledLink{
		Name:             name,
		Table:            l.Relation.Name,
		Access:           l.Access,
		IdentityStrategy: l.Identity.Strategy,
		IdentityInsert:   l.Identity.Insert,
		IdentityColumns:  append([]string(nil), l.Identity.Columns...),
		FromObject:       l.From.Object,
		FromColumns:      append([]string(nil), l.From.Columns...),
		ToObject:         l.To.Object,
		ToColumns:        append([]string(nil), l.To.Columns...),
		TenantStrategy:   l.Tenant.Strategy,
		TenantColumn:     l.Tenant.Column,
		TenantValue:      l.Tenant.Value,
		SystemStrategy:   l.System.Strategy,
		Cardinality:      def.Cardinality,
		FieldByLogical:   map[string]CompiledField{},
		FieldByColumn:    map[string]CompiledField{},
		PropertyTypes:    map[string]string{},
	}
	omit, err := parseOmit(l.System.Omit)
	if err != nil {
		return nil, fmt.Errorf("%w: link %q: %v", spi.ErrInvalidMapping, name, err)
	}
	cl.Omit = omit
	for _, p := range def.Properties {
		cl.PropertyTypes[p.Name] = p.Type
	}
	for logical, f := range l.Fields {
		cf := CompiledField{Logical: logical, Column: f.Column}
		cl.Fields = append(cl.Fields, cf)
		cl.FieldByLogical[logical] = cf
		cl.FieldByColumn[f.Column] = cf
	}
	sort.Slice(cl.Fields, func(i, j int) bool { return cl.Fields[i].Logical < cl.Fields[j].Logical })
	if l.Identity.Insert != "generated" {
		for _, col := range l.Identity.Columns {
			if _, ok := cl.FieldByColumn[col]; !ok {
				return nil, fmt.Errorf("%w: link %q identity column %q is not a payload field", spi.ErrInvalidMapping, name, col)
			}
		}
	}
	return cl, nil
}

func compileInlineLink(name string, l Link, def spi.LinkTypeDefinition, objects map[string]spi.ObjectTypeDefinition, compiledModels map[string]*CompiledModel) (*CompiledLink, error) {
	hostSide, err := inlineHostSide(name, l, def)
	if err != nil {
		return nil, err
	}
	hostName := l.From.Object
	peerName := l.To.Object
	fkCols := l.To.Columns
	if hostSide == "to" {
		hostName, peerName = l.To.Object, l.From.Object
		fkCols = l.From.Columns
	}
	if len(fkCols) != 1 {
		return nil, fmt.Errorf("%w: link %q inline FK column required on non-host endpoint", spi.ErrInvalidMapping, name)
	}
	fk := fkCols[0]
	hostModel, ok := compiledModels[hostName]
	if !ok {
		return nil, fmt.Errorf("%w: link %q host model %q not compiled", spi.ErrInvalidMapping, name, hostName)
	}
	if err := checkFKCollision(name, fk, hostModel); err != nil {
		return nil, err
	}
	hostDef := objects[hostName]
	nav, err := hostNavigation(name, hostDef, def.Name, hostSide)
	if err != nil {
		return nil, err
	}
	access := l.Access
	if access == "" {
		access = "readWrite"
	}
	fromCols := append([]string(nil), l.From.Columns...)
	toCols := append([]string(nil), l.To.Columns...)
	cl := &CompiledLink{
		Name:             name,
		Table:            hostModel.Table,
		Access:           access,
		IdentityStrategy: hostModel.IdentityStrategy,
		IdentityInsert:   hostModel.IdentityInsert,
		IdentityColumns:  append([]string(nil), hostModel.IdentityColumns...),
		FromObject:       l.From.Object,
		FromColumns:      fromCols,
		ToObject:         l.To.Object,
		ToColumns:        toCols,
		TenantStrategy:   hostModel.TenantStrategy,
		TenantColumn:     hostModel.TenantColumn,
		TenantValue:      hostModel.TenantValue,
		SystemStrategy:   hostModel.SystemStrategy,
		Cardinality:      def.Cardinality,
		Omit:             hostModel.Omit,
		FieldByLogical:   map[string]CompiledField{},
		FieldByColumn:    map[string]CompiledField{},
		PropertyTypes:    map[string]string{},
		Inline:           true,
		HostModel:        hostName,
		HostNavField:     nav.Field,
		FKColumn:         fk,
		FKNullable:       !nav.NonNull,
	}
	_ = peerName
	return cl, nil
}

func inlineHostSide(name string, l Link, def spi.LinkTypeDefinition) (string, error) {
	switch def.Cardinality {
	case spi.CardinalityManyToOne:
		return "from", nil
	case spi.CardinalityOneToMany:
		return "to", nil
	case spi.CardinalityOneToOne:
		if l.Host != "from" && l.Host != "to" {
			return "", fmt.Errorf("%w: link %q ONE_TO_ONE inline requires host: from or host: to", spi.ErrInvalidMapping, name)
		}
		return l.Host, nil
	default:
		return "", fmt.Errorf("%w: link %q cannot be inline", spi.ErrInvalidMapping, name)
	}
}

func hostNavigation(linkName string, host spi.ObjectTypeDefinition, linkType, hostSide string) (spi.LinkNavigation, error) {
	wantDir := "OUTBOUND"
	if hostSide == "to" {
		wantDir = "INBOUND"
	}
	for _, n := range host.Navigations {
		if n.LinkType == linkType && (n.Direction == wantDir || n.Direction == "") {
			return n, nil
		}
	}
	return spi.LinkNavigation{}, fmt.Errorf("%w: link %q missing host navigation on %s", spi.ErrInvalidMapping, linkName, host.Name)
}

func checkFKCollision(linkName, fk string, host *CompiledModel) error {
	for _, c := range host.IdentityColumns {
		if c == fk {
			return fmt.Errorf("%w: link %q FK %q collides with identity", spi.ErrInvalidMapping, linkName, fk)
		}
	}
	if host.TenantColumn == fk {
		return fmt.Errorf("%w: link %q FK %q collides with tenant", spi.ErrInvalidMapping, linkName, fk)
	}
	if _, ok := host.FieldByColumn[fk]; ok {
		return fmt.Errorf("%w: link %q FK %q collides with field", spi.ErrInvalidMapping, linkName, fk)
	}
	switch fk {
	case "version", "created_at", "updated_at", "deleted_at":
		return fmt.Errorf("%w: link %q FK %q collides with system column", spi.ErrInvalidMapping, linkName, fk)
	}
	return nil
}
