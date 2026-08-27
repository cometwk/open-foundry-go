package api

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	graphql "github.com/graph-gophers/graphql-go"

	"github.com/openfoundry/runtime/engine"
	projgql "github.com/openfoundry/runtime/projection/graphql"
	"github.com/openfoundry/runtime/query"
	"github.com/openfoundry/runtime/spi"
)

type ctxKey struct{}

const (
	sentinelActorID = "phase6-api"
	sentinelTraceID = "phase6-trace"
)

// Server serves the IR-projected GraphQL schema and REST GET over one Engine.
type Server struct {
	engine *engine.Engine
	schema *graphql.Schema

	objectNames               map[string]bool
	objFields                 []objField
	objType, objPtrType       reflect.Type
	edgeType, edgePtrType     reflect.Type
	connType, connPtrType     reflect.Type
	hitType, hitPtrType       reflect.Type
	searchType, searchPtrType reflect.Type
}

// New builds an executable GraphQL schema from the Engine's Ontology IR.
func New(eng *engine.Engine) (*Server, error) {
	if eng == nil {
		return nil, fmt.Errorf("api: engine must be non-nil")
	}
	s := &Server{engine: eng}
	if err := s.buildResolverTypes(eng.Ontology()); err != nil {
		return nil, err
	}
	sdl := projgql.Generate(eng.Ontology(), eng.Capabilities())
	root, err := s.buildQueryRoot()
	if err != nil {
		return nil, err
	}
	schema, err := graphql.ParseSchema(sdl, root,
		graphql.UseFieldResolvers(),
		graphql.DisableIntrospection(),
	)
	if err != nil {
		return nil, fmt.Errorf("api: parse schema: %w", err)
	}
	s.schema = schema
	return s, nil
}

func (s *Server) Schema() *graphql.Schema { return s.schema }

func (s *Server) Engine() *engine.Engine { return s.engine }

func withRC(ctx context.Context, rc spi.RequestContext) context.Context {
	ctx = context.WithValue(ctx, ctxKey{}, rc)
	if memoFrom(ctx) == nil {
		ctx = context.WithValue(ctx, memoKey{}, newExpandMemo())
	}
	return ctx
}

func rcFrom(ctx context.Context) spi.RequestContext {
	rc, _ := ctx.Value(ctxKey{}).(spi.RequestContext)
	return rc
}

type gqlGetArgs struct {
	ID graphql.ID
}

func (s *Server) buildQueryRoot() (any, error) {
	objects := s.engine.Ontology().Objects
	fts := s.engine.Capabilities().SupportsFullTextSearch
	getFn := reflect.FuncOf([]reflect.Type{ctxType, reflect.TypeOf(gqlGetArgs{})}, []reflect.Type{s.objPtrType, errType}, false)
	listFn := reflect.FuncOf([]reflect.Type{ctxType, reflect.TypeOf(listArgs{})}, []reflect.Type{s.connPtrType, errType}, false)
	aggFn := reflect.TypeOf(func(context.Context, aggregateArgs) (*AggregateResult, error) { return nil, nil })
	searchFn := reflect.FuncOf([]reflect.Type{ctxType, reflect.TypeOf(searchArgs{})}, []reflect.Type{s.searchPtrType, errType}, false)

	var fields []reflect.StructField
	for _, obj := range objects {
		typ := obj.Name
		lower := projgql.LowerFirst(typ)
		fields = append(fields,
			reflect.StructField{Name: exportName(lower), Type: getFn, Tag: reflect.StructTag(`graphql:"` + lower + `"`)},
			reflect.StructField{Name: exportName(lower + "s"), Type: listFn, Tag: reflect.StructTag(`graphql:"` + lower + `s"`)},
			reflect.StructField{Name: exportName(lower + "Aggregate"), Type: aggFn, Tag: reflect.StructTag(`graphql:"` + lower + `Aggregate"`)},
		)
		if fts {
			gqlName := "search" + typ + "s"
			fields = append(fields, reflect.StructField{
				Name: exportName(gqlName),
				Type: searchFn,
				Tag:  reflect.StructTag(`graphql:"` + gqlName + `"`),
			})
		}
	}
	rootType := reflect.StructOf(fields)
	root := reflect.New(rootType).Elem()
	for _, obj := range objects {
		typ := obj.Name
		lower := projgql.LowerFirst(typ)
		root.FieldByName(exportName(lower)).Set(s.makeGet(typ))
		root.FieldByName(exportName(lower + "s")).Set(s.makeList(typ))
		root.FieldByName(exportName(lower + "Aggregate")).Set(reflect.ValueOf(s.makeAggregate(typ)))
		if fts {
			root.FieldByName(exportName("search" + typ + "s")).Set(s.makeSearch(typ))
		}
	}
	return root.Addr().Interface(), nil
}

func (s *Server) makeGet(typ string) reflect.Value {
	ft := reflect.FuncOf([]reflect.Type{ctxType, reflect.TypeOf(gqlGetArgs{})}, []reflect.Type{s.objPtrType, errType}, false)
	return reflect.MakeFunc(ft, func(in []reflect.Value) []reflect.Value {
		ctx := in[0].Interface().(context.Context)
		args := in[1].Interface().(gqlGetArgs)
		res, err := query.Execute(s.engine, rcFrom(ctx), query.Op{Get: &query.Get{Type: typ, ID: string(args.ID)}})
		if err != nil {
			if errors.Is(err, spi.ErrObjectNotFound) {
				return s.retPair(s.objPtrType, nil, nil)
			}
			return s.retPair(s.objPtrType, nil, err)
		}
		return s.retPair(s.objPtrType, s.wrap(typ, res.Object), nil)
	})
}

func (s *Server) makeList(typ string) reflect.Value {
	ft := reflect.FuncOf([]reflect.Type{ctxType, reflect.TypeOf(listArgs{})}, []reflect.Type{s.connPtrType, errType}, false)
	return reflect.MakeFunc(ft, func(in []reflect.Value) []reflect.Value {
		ctx := in[0].Interface().(context.Context)
		args := in[1].Interface().(listArgs)
		offset, limit, err := resolvePagination(args)
		if err != nil {
			return s.retPair(s.connPtrType, nil, err)
		}
		opts := &spi.QueryOptions{Limit: limit, Offset: offset, OrderBy: convertOrderBy(args.OrderBy)}
		res, err := query.Execute(s.engine, rcFrom(ctx), query.Op{List: &query.List{
			Type:    typ,
			Filter:  filterOrEmpty(args.Filter),
			Options: opts,
		}})
		if err != nil {
			return s.retPair(s.connPtrType, nil, err)
		}
		nodes := make([]any, 0, len(res.Page.Items))
		for _, item := range res.Page.Items {
			nodes = append(nodes, s.wrap(typ, item))
		}
		return s.retPair(s.connPtrType, s.buildConnection(nodes, res.Page.TotalCount, offset), nil)
	})
}

func (s *Server) makeAggregate(typ string) func(context.Context, aggregateArgs) (*AggregateResult, error) {
	return func(ctx context.Context, args aggregateArgs) (*AggregateResult, error) {
		q := spi.AggregateQuery{Filter: convertFilter(args.Filter)}
		if args.GroupBy != nil {
			q.GroupBy = *args.GroupBy
		}
		for _, f := range args.Fields {
			alias := f.Field + "_" + strings.ToLower(f.Fn)
			if f.Alias != nil && *f.Alias != "" {
				alias = *f.Alias
			}
			q.Fields = append(q.Fields, spi.AggregateField{
				Field: f.Field,
				Fn:    strings.ToLower(f.Fn),
				Alias: alias,
			})
		}
		got, err := query.Execute(s.engine, rcFrom(ctx), query.Op{Aggregate: &query.Aggregate{Type: typ, Query: q}})
		if err != nil {
			return nil, err
		}
		out := &AggregateResult{TotalGroups: int32(got.Aggregate.TotalGroups)}
		for i := range got.Aggregate.Groups {
			g := got.Aggregate.Groups[i]
			out.Groups = append(out.Groups, &AggregateGroup{
				Keys:   JSON{V: g.Keys},
				Values: JSON{V: g.Values},
			})
		}
		if out.Groups == nil {
			out.Groups = []*AggregateGroup{}
		}
		return out, nil
	}
}

func (s *Server) makeSearch(typ string) reflect.Value {
	ft := reflect.FuncOf([]reflect.Type{ctxType, reflect.TypeOf(searchArgs{})}, []reflect.Type{s.searchPtrType, errType}, false)
	return reflect.MakeFunc(ft, func(in []reflect.Value) []reflect.Value {
		ctx := in[0].Interface().(context.Context)
		args := in[1].Interface().(searchArgs)
		offset := 0
		limit := defaultPageSize
		if args.After != nil && *args.After != "" {
			n, err := decodeCursor(*args.After)
			if err != nil {
				return s.retPair(s.searchPtrType, nil, err)
			}
			offset = n + 1
		}
		if args.First != nil {
			limit = int(*args.First)
			if limit < 0 {
				limit = 0
			}
			if limit > maxPageSize {
				limit = maxPageSize
			}
		}
		sq := spi.SearchQuery{
			Query:  args.Query,
			Filter: convertFilter(args.Filter),
			Limit:  limit,
			Offset: offset,
		}
		if args.Fields != nil {
			sq.Fields = *args.Fields
		}
		got, err := query.Execute(s.engine, rcFrom(ctx), query.Op{Search: &query.Search{Type: typ, Query: sq}})
		if err != nil {
			return s.retPair(s.searchPtrType, nil, err)
		}
		nodes := make([]any, 0, len(got.Search.Hits))
		scores := make([]float64, 0, len(got.Search.Hits))
		for _, h := range got.Search.Hits {
			nodes = append(nodes, s.wrap(typ, h.Object))
			scores = append(scores, h.Score)
		}
		return s.retPair(s.searchPtrType, s.buildSearchResult(nodes, scores, got.Search.TotalCount, got.Search.HasNextPage), nil)
	})
}

func exportName(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Exec runs a GraphQL query against the schema with the given request context.
func (s *Server) Exec(ctx context.Context, rc spi.RequestContext, query string, vars map[string]any) *graphql.Response {
	return s.schema.Exec(withRC(ctx, rc), query, "", vars)
}
