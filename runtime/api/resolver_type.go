package api

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	graphql "github.com/graph-gophers/graphql-go"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
)

var (
	ctxType          = reflect.TypeOf((*context.Context)(nil)).Elem()
	errType          = reflect.TypeOf((*error)(nil)).Elem()
	placeholderPtr   = reflect.TypeOf((*struct{})(nil))
	placeholderSlice = reflect.SliceOf(placeholderPtr)
	idType           = reflect.TypeOf(graphql.ID(""))
	i32Type          = reflect.TypeOf(int32(0))
	f64Type          = reflect.TypeOf(float64(0))
	boolType         = reflect.TypeOf(false)
	strType          = reflect.TypeOf("")
	dtType           = reflect.TypeOf(DateTime(""))
	jsonType         = reflect.TypeOf(JSON{})
)

type objFieldKind int

const (
	fieldScalar objFieldKind = iota
	fieldLinkList
	fieldLinkOne
	fieldFK
	fieldComputed
)

type objField struct {
	gqlName string
	goName  string
	kind    objFieldKind
	typ     ir.TypeRef
}

func (s *Server) buildResolverTypes(ont *ir.Ontology) error {
	if ont == nil {
		return fmt.Errorf("api: ontology must be non-nil")
	}
	objectNames := map[string]bool{}
	for _, o := range ont.Objects {
		objectNames[o.Name] = true
	}
	s.objectNames = objectNames

	specs, err := collectObjFields(ont, objectNames)
	if err != nil {
		return err
	}
	s.objFields = specs

	fields := make([]reflect.StructField, 0, len(specs))
	var rewrites []struct {
		index int
		list  bool
	}
	for i, spec := range specs {
		ft, list, rewrite := spec.placeholderFuncType()
		if rewrite {
			rewrites = append(rewrites, struct {
				index int
				list  bool
			}{i, list})
		}
		fields = append(fields, reflect.StructField{
			Name: spec.goName,
			Type: ft,
			Tag:  reflect.StructTag(`graphql:"` + spec.gqlName + `"`),
		})
	}
	if len(fields) == 0 {
		fields = append(fields, reflect.StructField{Name: "Unused", Type: strType, Tag: `graphql:"-"`})
	}
	s.objType = reflect.StructOf(fields)
	s.objPtrType = reflect.PointerTo(s.objType)
	for _, rw := range rewrites {
		var real reflect.Type
		if rw.list {
			real = reflect.FuncOf([]reflect.Type{ctxType}, []reflect.Type{reflect.SliceOf(s.objPtrType), errType}, false)
		} else {
			real = reflect.FuncOf([]reflect.Type{ctxType}, []reflect.Type{s.objPtrType, errType}, false)
		}
		if err := rewriteStructFieldType(s.objType, rw.index, real); err != nil {
			return err
		}
	}

	s.edgeType = reflect.StructOf([]reflect.StructField{
		{Name: "Node", Type: s.objPtrType, Tag: `graphql:"node"`},
		{Name: "Cursor", Type: strType, Tag: `graphql:"cursor"`},
	})
	s.edgePtrType = reflect.PointerTo(s.edgeType)
	s.connType = reflect.StructOf([]reflect.StructField{
		{Name: "Edges", Type: reflect.SliceOf(s.edgePtrType), Tag: `graphql:"edges"`},
		{Name: "PageInfo", Type: reflect.TypeOf(pageInfo{}), Tag: `graphql:"pageInfo"`},
		{Name: "TotalCount", Type: i32Type, Tag: `graphql:"totalCount"`},
	})
	s.connPtrType = reflect.PointerTo(s.connType)
	s.hitType = reflect.StructOf([]reflect.StructField{
		{Name: "Node", Type: s.objPtrType, Tag: `graphql:"node"`},
		{Name: "Score", Type: f64Type, Tag: `graphql:"score"`},
	})
	s.hitPtrType = reflect.PointerTo(s.hitType)
	s.searchType = reflect.StructOf([]reflect.StructField{
		{Name: "Hits", Type: reflect.SliceOf(s.hitPtrType), Tag: `graphql:"hits"`},
		{Name: "TotalCount", Type: i32Type, Tag: `graphql:"totalCount"`},
		{Name: "HasNextPage", Type: boolType, Tag: `graphql:"hasNextPage"`},
	})
	s.searchPtrType = reflect.PointerTo(s.searchType)
	return nil
}

func collectObjFields(ont *ir.Ontology, objectNames map[string]bool) ([]objField, error) {
	type seen struct {
		sig  string
		spec objField
	}
	byName := map[string]seen{}
	for _, obj := range ont.Objects {
		for _, f := range obj.Fields {
			if f.Role == ir.RoleParam {
				continue
			}
			spec := objField{
				gqlName: f.Name,
				goName:  exportName(f.Name),
				kind:    classifyField(f, objectNames),
				typ:     f.Type,
			}
			sig := fieldSig(spec)
			if prev, ok := byName[f.Name]; ok {
				if prev.sig != sig {
					return nil, fmt.Errorf("api: GraphQL field %q has incompatible types across ObjectTypes", f.Name)
				}
				continue
			}
			byName[f.Name] = seen{sig: sig, spec: spec}
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]objField, 0, len(names))
	for _, n := range names {
		out = append(out, byName[n].spec)
	}
	return out, nil
}

func classifyField(f ir.Field, objectNames map[string]bool) objFieldKind {
	switch f.Role {
	case ir.RoleLinkNav:
		if f.Type.IsList {
			return fieldLinkList
		}
		return fieldLinkOne
	case ir.RoleComputed:
		return fieldComputed
	default:
		if f.Role == ir.RoleProperty && objectNames[f.Type.Name] && !f.Type.IsList {
			return fieldFK
		}
		return fieldScalar
	}
}

func fieldSig(spec objField) string {
	switch spec.kind {
	case fieldLinkList:
		return "linkList"
	case fieldLinkOne, fieldFK:
		return "objPtr"
	case fieldComputed:
		return fmt.Sprintf("computed|%s|%v", scalarGo(spec.typ).String(), spec.typ.NonNull)
	default:
		return fmt.Sprintf("scalar|%s|%v", scalarGo(spec.typ).String(), spec.typ.NonNull)
	}
}

func (spec objField) placeholderFuncType() (ft reflect.Type, list, rewrite bool) {
	switch spec.kind {
	case fieldLinkList:
		return reflect.FuncOf([]reflect.Type{ctxType}, []reflect.Type{placeholderSlice, errType}, false), true, true
	case fieldLinkOne, fieldFK:
		return reflect.FuncOf([]reflect.Type{ctxType}, []reflect.Type{placeholderPtr, errType}, false), false, true
	case fieldComputed:
		out := spec.computedOutType()
		return reflect.FuncOf([]reflect.Type{ctxType}, []reflect.Type{out, errType}, false), false, false
	default:
		out := spec.scalarOutType()
		return reflect.FuncOf(nil, []reflect.Type{out}, false), false, false
	}
}

func (spec objField) scalarOutType() reflect.Type {
	base := scalarGo(spec.typ)
	if spec.typ.NonNull {
		return base
	}
	return reflect.PointerTo(base)
}

func (spec objField) computedOutType() reflect.Type {
	base := scalarGo(spec.typ)
	if spec.typ.NonNull {
		return base
	}
	return reflect.PointerTo(base)
}

func scalarGo(t ir.TypeRef) reflect.Type {
	switch t.Name {
	case "ID":
		return idType
	case "Int":
		return i32Type
	case "Float":
		return f64Type
	case "Boolean":
		return boolType
	case "DateTime":
		return dtType
	case "JSON":
		return jsonType
	default:
		return strType
	}
}

func (s *Server) bind(n *node, elem reflect.Value) {
	for _, spec := range s.objFields {
		ft, ok := s.objType.FieldByName(spec.goName)
		if !ok {
			continue
		}
		elem.FieldByName(spec.goName).Set(s.makeObjFunc(n, spec, ft.Type))
	}
}

func (s *Server) makeObjFunc(n *node, spec objField, fnType reflect.Type) reflect.Value {
	return reflect.MakeFunc(fnType, func(in []reflect.Value) []reflect.Value {
		f := n.irField(spec.gqlName)
		kind := spec.kind
		if f != nil {
			kind = classifyField(*f, s.objectNames)
		}
		switch kind {
		case fieldLinkList:
			ctx := in[0].Interface().(context.Context)
			list, err := s.resolveLink(ctx, n, spec.gqlName)
			return s.retObjSlice(list, err)
		case fieldLinkOne:
			ctx := in[0].Interface().(context.Context)
			list, err := s.resolveLink(ctx, n, spec.gqlName)
			if err != nil || len(list) == 0 {
				return s.retObjPtr(nil, err)
			}
			return s.retObjPtr(list[0], nil)
		case fieldFK:
			ctx := in[0].Interface().(context.Context)
			v, err := s.resolveFK(ctx, n, spec.gqlName)
			return s.retObjPtr(v, err)
		case fieldComputed:
			ctx := in[0].Interface().(context.Context)
			return s.retComputed(n, spec, fnType.Out(0), ctx)
		default:
			return []reflect.Value{n.scalarOut(spec, fnType.Out(0))}
		}
	})
}

func (s *Server) retObjSlice(list []any, err error) []reflect.Value {
	sliceT := reflect.SliceOf(s.objPtrType)
	if err != nil {
		return []reflect.Value{reflect.Zero(sliceT), reflect.ValueOf(err)}
	}
	sl := reflect.MakeSlice(sliceT, 0, len(list))
	for _, o := range list {
		if o == nil {
			continue
		}
		sl = reflect.Append(sl, reflect.ValueOf(o))
	}
	return []reflect.Value{sl, reflect.Zero(errType)}
}

func (s *Server) retObjPtr(v any, err error) []reflect.Value {
	if err != nil {
		return []reflect.Value{reflect.Zero(s.objPtrType), reflect.ValueOf(err)}
	}
	if v == nil {
		return []reflect.Value{reflect.Zero(s.objPtrType), reflect.Zero(errType)}
	}
	return []reflect.Value{reflect.ValueOf(v), reflect.Zero(errType)}
}

func (s *Server) retComputed(n *node, spec objField, outT reflect.Type, ctx context.Context) []reflect.Value {
	f := n.irField(spec.gqlName)
	if f == nil {
		return []reflect.Value{reflect.Zero(outT), reflect.Zero(errType)}
	}
	v, err := s.engine.ComputeField(rcFrom(ctx), n.typ, n.idString(), spec.gqlName)
	if err != nil {
		return []reflect.Value{reflect.Zero(outT), reflect.ValueOf(err)}
	}
	if v == nil {
		return []reflect.Value{reflect.Zero(outT), reflect.Zero(errType)}
	}
	conv, err := convertComputed(v, outT)
	if err != nil {
		return []reflect.Value{reflect.Zero(outT), reflect.ValueOf(err)}
	}
	return []reflect.Value{conv, reflect.Zero(errType)}
}

func convertComputed(v any, outT reflect.Type) (reflect.Value, error) {
	want := outT
	ptr := outT.Kind() == reflect.Ptr
	if ptr {
		want = outT.Elem()
	}
	nv, ok := coerceScalar(v, want)
	if !ok {
		return reflect.Value{}, fmt.Errorf("computed: expected %s, got %T", want, v)
	}
	if !ptr {
		return nv, nil
	}
	p := reflect.New(want)
	p.Elem().Set(nv)
	return p, nil
}

func coerceScalar(v any, want reflect.Type) (reflect.Value, bool) {
	switch want {
	case i32Type:
		i, ok := toInt32(v)
		if !ok {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(i), true
	case f64Type:
		f, ok := toFloat64(v)
		if !ok {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(f), true
	case boolType:
		b, ok := v.(bool)
		if !ok {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(b), true
	case dtType:
		s, ok := v.(string)
		if !ok {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(DateTime(s)), true
	case idType:
		s, ok := v.(string)
		if !ok {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(graphql.ID(s)), true
	case jsonType:
		return reflect.ValueOf(JSON{V: v}), true
	default:
		s, ok := v.(string)
		if !ok {
			return reflect.Value{}, false
		}
		return reflect.ValueOf(s), true
	}
}

func (s *Server) wrapMany(fieldName, startType string, objs []spi.OntologyObject) []any {
	target := startType
	if ot := s.engine.Ontology().ObjectByName(startType); ot != nil {
		if f := fieldByName(ot, fieldName); f != nil {
			target = f.Type.Name
		}
	}
	out := make([]any, 0, len(objs))
	for _, obj := range objs {
		typ := target
		if t, _ := obj[spi.FieldType].(string); t != "" {
			typ = t
		}
		out = append(out, s.wrap(typ, obj))
	}
	return out
}

func (s *Server) buildConnection(nodes []any, totalCount, offset int) any {
	edges := reflect.MakeSlice(reflect.SliceOf(s.edgePtrType), len(nodes), len(nodes))
	var start, end *string
	for i, n := range nodes {
		e := reflect.New(s.edgeType)
		if n != nil {
			e.Elem().FieldByName("Node").Set(reflect.ValueOf(n))
		}
		cur := encodeCursor(offset + i)
		e.Elem().FieldByName("Cursor").Set(reflect.ValueOf(cur))
		edges.Index(i).Set(e)
		if i == 0 {
			c := cur
			start = &c
		}
		if i == len(nodes)-1 {
			c := cur
			end = &c
		}
	}
	conn := reflect.New(s.connType)
	conn.Elem().FieldByName("Edges").Set(edges)
	conn.Elem().FieldByName("PageInfo").Set(reflect.ValueOf(pageInfo{
		HasNextPage:     offset+len(nodes) < totalCount,
		HasPreviousPage: offset > 0,
		StartCursor:     start,
		EndCursor:       end,
	}))
	conn.Elem().FieldByName("TotalCount").Set(reflect.ValueOf(int32(totalCount)))
	return conn.Interface()
}

func (s *Server) buildSearchResult(hits []any, scores []float64, totalCount int, hasNext bool) any {
	hs := reflect.MakeSlice(reflect.SliceOf(s.hitPtrType), len(hits), len(hits))
	for i, n := range hits {
		h := reflect.New(s.hitType)
		if n != nil {
			h.Elem().FieldByName("Node").Set(reflect.ValueOf(n))
		}
		h.Elem().FieldByName("Score").Set(reflect.ValueOf(scores[i]))
		hs.Index(i).Set(h)
	}
	out := reflect.New(s.searchType)
	out.Elem().FieldByName("Hits").Set(hs)
	out.Elem().FieldByName("TotalCount").Set(reflect.ValueOf(int32(totalCount)))
	out.Elem().FieldByName("HasNextPage").Set(reflect.ValueOf(hasNext))
	return out.Interface()
}

func (s *Server) retPair(ptrT reflect.Type, v any, err error) []reflect.Value {
	if err != nil {
		return []reflect.Value{reflect.Zero(ptrT), reflect.ValueOf(err)}
	}
	if v == nil {
		return []reflect.Value{reflect.Zero(ptrT), reflect.Zero(errType)}
	}
	return []reflect.Value{reflect.ValueOf(v), reflect.Zero(errType)}
}
