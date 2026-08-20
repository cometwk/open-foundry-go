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
}

// New builds an executable GraphQL schema from the Engine's Ontology IR.
func New(eng *engine.Engine) (*Server, error) {
	if eng == nil {
		return nil, fmt.Errorf("api: engine must be non-nil")
	}
	s := &Server{engine: eng}
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
	return context.WithValue(ctx, ctxKey{}, rc)
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
	getFn := reflect.TypeOf(func(context.Context, gqlGetArgs) (*node, error) { return nil, nil })
	listFn := reflect.TypeOf(func(context.Context, listArgs) (*Connection, error) { return nil, nil })
	aggFn := reflect.TypeOf(func(context.Context, aggregateArgs) (*AggregateResult, error) { return nil, nil })
	searchFn := reflect.TypeOf(func(context.Context, searchArgs) (*SearchResult, error) { return nil, nil })

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
		root.FieldByName(exportName(lower)).Set(reflect.ValueOf(s.makeGet(typ)))
		root.FieldByName(exportName(lower + "s")).Set(reflect.ValueOf(s.makeList(typ)))
		root.FieldByName(exportName(lower + "Aggregate")).Set(reflect.ValueOf(s.makeAggregate(typ)))
		if fts {
			root.FieldByName(exportName("search" + typ + "s")).Set(reflect.ValueOf(s.makeSearch(typ)))
		}
	}
	return root.Addr().Interface(), nil
}

func (s *Server) makeGet(typ string) func(context.Context, gqlGetArgs) (*node, error) {
	return func(ctx context.Context, args gqlGetArgs) (*node, error) {
		obj, err := s.engine.GetObject(rcFrom(ctx), typ, string(args.ID))
		if err != nil {
			if errors.Is(err, spi.ErrObjectNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return s.wrap(typ, obj), nil
	}
}

func (s *Server) makeList(typ string) func(context.Context, listArgs) (*Connection, error) {
	return func(ctx context.Context, args listArgs) (*Connection, error) {
		offset, limit, err := resolvePagination(args)
		if err != nil {
			return nil, err
		}
		opts := &spi.QueryOptions{Limit: limit, Offset: offset, OrderBy: convertOrderBy(args.OrderBy)}
		page, err := s.engine.QueryObjects(rcFrom(ctx), typ, filterOrEmpty(args.Filter), opts)
		if err != nil {
			return nil, err
		}
		nodes := make([]*node, 0, len(page.Items))
		for _, item := range page.Items {
			nodes = append(nodes, s.wrap(typ, item))
		}
		return buildConnection(nodes, page.TotalCount, offset), nil
	}
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
		got, err := s.engine.AggregateObjects(rcFrom(ctx), typ, q)
		if err != nil {
			return nil, err
		}
		out := &AggregateResult{TotalGroups: int32(got.TotalGroups)}
		for i := range got.Groups {
			g := got.Groups[i]
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

func (s *Server) makeSearch(typ string) func(context.Context, searchArgs) (*SearchResult, error) {
	return func(ctx context.Context, args searchArgs) (*SearchResult, error) {
		offset := 0
		limit := defaultPageSize
		if args.After != nil && *args.After != "" {
			n, err := decodeCursor(*args.After)
			if err != nil {
				return nil, err
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
		got, err := s.engine.SearchObjects(rcFrom(ctx), typ, sq)
		if err != nil {
			return nil, err
		}
		hits := make([]*SearchHit, 0, len(got.Hits))
		for _, h := range got.Hits {
			hits = append(hits, &SearchHit{Node: s.wrap(typ, h.Object), Score: h.Score})
		}
		return &SearchResult{
			Hits:        hits,
			TotalCount:  int32(got.TotalCount),
			HasNextPage: got.HasNextPage,
		}, nil
	}
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
