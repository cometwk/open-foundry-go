package engine

import (
	"fmt"
	"strings"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
)

// GetObjectOpts controls LAZY @computed evaluation on read (KTD-16).
//
// ComputedFields:
//   - nil (or a nil *GetObjectOpts): do not evaluate computed fields
//   - empty non-nil slice: evaluate every LAZY computed field on the type
//   - named entries: evaluate only those fields
type GetObjectOpts struct {
	ComputedFields []string
}

// GetObject reads through to SPI with no computed enrichment. Existing
// callers keep the Phase 2/3 contract.
func (e *Engine) GetObject(ctx spi.RequestContext, typ, id string) (spi.OntologyObject, error) {
	return e.GetObjectOpts(ctx, typ, id, nil)
}

// GetObjectOpts reads an object and optionally merges LAZY @computed fields.
func (e *Engine) GetObjectOpts(ctx spi.RequestContext, typ, id string, opts *GetObjectOpts) (spi.OntologyObject, error) {
	obj, err := e.storage.GetObject(ctx, typ, id)
	if err != nil {
		return nil, err
	}
	if opts == nil || opts.ComputedFields == nil {
		return obj, nil
	}
	return e.mergeComputed(ctx, typ, id, obj, opts.ComputedFields)
}

func (e *Engine) mergeComputed(ctx spi.RequestContext, typ, id string, obj spi.OntologyObject, want []string) (spi.OntologyObject, error) {
	def := e.ontology.ObjectByName(typ)
	if def == nil {
		return obj, nil
	}
	wanted := map[string]bool{}
	all := len(want) == 0
	if !all {
		for _, n := range want {
			wanted[n] = true
		}
	}
	out := cloneObject(obj)
	for _, f := range def.Fields {
		if f.Role != ir.RoleComputed || f.Computed == nil {
			continue
		}
		if !all && !wanted[f.Name] {
			continue
		}
		if !strings.EqualFold(f.Computed.Cache, "LAZY") && f.Computed.Cache != "" {
			continue
		}
		val, err := e.evalComputed(ctx, id, f)
		if err != nil {
			return nil, err
		}
		out[f.Name] = val
	}
	return out, nil
}

// ComputeField evaluates one LAZY @computed field without merging the rest
// of the object. GraphQL field resolvers call this only when the field is
// selected.
func (e *Engine) ComputeField(ctx spi.RequestContext, typ, id, field string) (any, error) {
	def := e.ontology.ObjectByName(typ)
	if def == nil {
		return nil, fmt.Errorf("%w: %s", spi.ErrInvalidObjectType, typ)
	}
	f := findField(def, field)
	if f == nil || f.Role != ir.RoleComputed || f.Computed == nil {
		return nil, fmt.Errorf("engine: %s.%s is not a computed field", typ, field)
	}
	return e.evalComputed(ctx, id, *f)
}

func (e *Engine) evalComputed(ctx spi.RequestContext, objectID string, f ir.Field) (any, error) {
	switch f.Computed.Fn {
	case "countLinks":
		return e.countLinks(ctx, objectID, f.Computed.Args)
	default:
		return nil, fmt.Errorf("engine: unknown computed fn %q on field %s", f.Computed.Fn, f.Name)
	}
}

func (e *Engine) countLinks(ctx spi.RequestContext, objectID string, args any) (int, error) {
	linkType, direction, err := parseCountLinksArgs(args)
	if err != nil {
		return 0, err
	}
	page, err := e.GetLinks(ctx, objectID, linkType, direction, nil)
	if err != nil {
		return 0, err
	}
	return page.TotalCount, nil
}

func parseCountLinksArgs(args any) (linkType, direction string, err error) {
	m, ok := args.(map[string]any)
	if !ok || m == nil {
		return "", "", fmt.Errorf("engine: countLinks requires args with type")
	}
	raw, ok := m["type"]
	if !ok {
		return "", "", fmt.Errorf("engine: countLinks requires args.type")
	}
	linkType, ok = raw.(string)
	if !ok || linkType == "" {
		return "", "", fmt.Errorf("engine: countLinks args.type must be a string")
	}
	direction = "inbound"
	if d, ok := m["direction"].(string); ok && d != "" {
		direction = strings.ToLower(d)
	}
	return linkType, direction, nil
}

func cloneObject(obj spi.OntologyObject) spi.OntologyObject {
	out := make(spi.OntologyObject, len(obj)+1)
	for k, v := range obj {
		out[k] = v
	}
	return out
}
