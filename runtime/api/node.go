package api

import (
	"context"
	"errors"
	"reflect"

	graphql "github.com/graph-gophers/graphql-go"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/query"
	"github.com/openfoundry/runtime/spi"
)

// node is the per-object payload captured by IR-bound GraphQL field funcs.
type node struct {
	srv *Server
	typ string
	obj spi.OntologyObject
}

func (s *Server) wrap(typ string, obj spi.OntologyObject) any {
	if obj == nil {
		return nil
	}
	n := &node{srv: s, typ: typ, obj: obj}
	v := reflect.New(s.objType)
	s.bind(n, v.Elem())
	return v.Interface()
}

func (n *node) idString() string {
	s, _ := n.obj[spi.FieldID].(string)
	return s
}

func (n *node) irField(name string) *ir.Field {
	ot := n.srv.engine.Ontology().ObjectByName(n.typ)
	if ot == nil {
		return nil
	}
	return fieldByName(ot, name)
}

func (s *Server) resolveLink(ctx context.Context, n *node, fieldName string) ([]any, error) {
	if kids, ok := memoFrom(ctx).lookup(n.idString(), fieldName); ok {
		return s.wrapMany(fieldName, n.typ, kids), nil
	}
	ex, err := compileExpand(ctx, s.engine.Ontology(), n.typ, fieldName)
	if err != nil {
		return []any{}, nil
	}
	ex.StartID = n.idString()
	res, err := query.Execute(s.engine, rcFrom(ctx), query.Op{Expand: ex})
	if err != nil {
		return nil, err
	}
	if res.Expand != nil {
		memoFrom(ctx).merge(res.Expand.Adjacency)
		return s.wrapMany(fieldName, n.typ, res.Expand.FirstHop), nil
	}
	return []any{}, nil
}

func (s *Server) resolveFK(ctx context.Context, n *node, fieldName string) (any, error) {
	ot := s.engine.Ontology().ObjectByName(n.typ)
	if ot == nil {
		return nil, nil
	}
	f := fieldByName(ot, fieldName)
	if f == nil {
		return nil, nil
	}
	raw := n.obj[fieldName]
	id, _ := raw.(string)
	if id == "" {
		return nil, nil
	}
	res, err := query.Execute(s.engine, rcFrom(ctx), query.Op{Get: &query.Get{Type: f.Type.Name, ID: id}})
	if err != nil {
		if errors.Is(err, spi.ErrObjectNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return s.wrap(f.Type.Name, res.Object), nil
}

func fieldByName(ot *ir.ObjectType, name string) *ir.Field {
	for i := range ot.Fields {
		if ot.Fields[i].Name == name {
			return &ot.Fields[i]
		}
	}
	return nil
}

func (n *node) scalarOut(spec objField, outT reflect.Type) reflect.Value {
	f := n.irField(spec.gqlName)
	if f == nil {
		return reflect.Zero(outT)
	}
	if outT.Kind() == reflect.Ptr {
		v, ok := n.nullableScalar(*f, outT.Elem())
		if !ok {
			return reflect.Zero(outT)
		}
		p := reflect.New(outT.Elem())
		p.Elem().Set(v)
		return p
	}
	return n.nonNullScalar(*f, outT)
}

func (n *node) nonNullScalar(f ir.Field, outT reflect.Type) reflect.Value {
	switch outT {
	case idType:
		if f.Role == ir.RolePrimary {
			return reflect.ValueOf(graphql.ID(n.idString()))
		}
		return reflect.ValueOf(graphql.ID(n.str(f.Name)))
	case i32Type:
		return reflect.ValueOf(n.i32(f.Name))
	case f64Type:
		return reflect.ValueOf(n.f64(f.Name))
	case boolType:
		b, _ := n.obj[f.Name].(bool)
		return reflect.ValueOf(b)
	case dtType:
		return reflect.ValueOf(DateTime(n.str(f.Name)))
	case jsonType:
		return reflect.ValueOf(JSON{V: n.obj[f.Name]})
	default:
		return reflect.ValueOf(n.str(f.Name))
	}
}

func (n *node) nullableScalar(f ir.Field, elem reflect.Type) (reflect.Value, bool) {
	v, ok := n.obj[f.Name]
	if !ok || v == nil {
		return reflect.Value{}, false
	}
	cv, ok := coerceScalar(v, elem)
	return cv, ok
}

func (n *node) str(field string) string {
	s, _ := n.obj[field].(string)
	return s
}

func (n *node) i32(field string) int32 {
	i, _ := toInt32(n.obj[field])
	return i
}

func (n *node) f64(field string) float64 {
	f, _ := toFloat64(n.obj[field])
	return f
}

func toInt32(v any) (int32, bool) {
	switch n := v.(type) {
	case int32:
		return n, true
	case int:
		return int32(n), true
	case int64:
		return int32(n), true
	case float64:
		return int32(n), true
	default:
		return 0, false
	}
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
