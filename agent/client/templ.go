package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	graphql "github.com/graph-gophers/graphql-go"
	"github.com/openfoundry/runtime/api"
	"github.com/openfoundry/runtime/spi"
)

// Defaults mirror templ.ts DEFAULT_HEADERS (tenant gold) and e2e/demo ActorID.
const (
	DefaultTenant = "gold"
	DefaultActor  = "test"
)

type execer interface {
	Exec(ctx context.Context, rc spi.RequestContext, query string, vars map[string]any) *graphql.Response
}

// Client wraps an in-process api.Server.Exec transport with a bound RequestContext.
type Client struct {
	exec execer
	rc   spi.RequestContext
}

type ClientOption func(*Client)

// WithRequestContext replaces the bound tenant/actor context.
func WithRequestContext(rc spi.RequestContext) ClientOption {
	return func(c *Client) { c.rc = rc }
}

// WithTenant overrides TenantID (like swapping X-OpenFoundry-Tenant).
func WithTenant(tenantID string) ClientOption {
	return func(c *Client) { c.rc.TenantID = tenantID }
}

// WithActor overrides ActorID.
func WithActor(actorID string) ClientOption {
	return func(c *Client) { c.rc.ActorID = actorID }
}

// CreateClient binds srv and default gold/test request context (templ.ts createClient).
func CreateClient(srv *api.Server, opts ...ClientOption) *Client {
	c := &Client{
		exec: srv,
		rc:   spi.RequestContext{TenantID: DefaultTenant, ActorID: DefaultActor},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// GqlExec runs query via Server.Exec, then unmarshals data[field] into T.
// A JSON null or missing field returns (nil, nil). GraphQL errors return the first message.
func GqlExec[T any](ctx context.Context, c *Client, query string, vars map[string]any, field string) (*T, error) {
	res := c.exec.Exec(ctx, c.rc, query, vars)
	if len(res.Errors) > 0 {
		return nil, errors.New(res.Errors[0].Message)
	}
	return decodeField[T](res.Data, field)
}

func decodeField[T any](data json.RawMessage, field string) (*T, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	raw, ok := root[field]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- Shared result / filter types (templ.ts) ---

type SortDirection string

const (
	SortAsc  SortDirection = "ASC"
	SortDesc SortDirection = "DESC"
)

type PageInfo struct {
	HasNextPage     bool    `json:"hasNextPage"`
	HasPreviousPage bool    `json:"hasPreviousPage"`
	StartCursor     *string `json:"startCursor,omitempty"`
	EndCursor       *string `json:"endCursor,omitempty"`
}

type Edge[T any] struct {
	Node   T      `json:"node"`
	Cursor string `json:"cursor"`
}

type Connection[T any] struct {
	Edges      []Edge[T] `json:"edges"`
	PageInfo   PageInfo  `json:"pageInfo"`
	TotalCount int       `json:"totalCount"`
}

type SearchHit[T any] struct {
	Node  T       `json:"node"`
	Score float64 `json:"score"`
}

type SearchResult[T any] struct {
	Hits        []SearchHit[T] `json:"hits"`
	TotalCount  int            `json:"totalCount"`
	HasNextPage bool           `json:"hasNextPage"`
}

type AggregateFunction string

const (
	AggCount AggregateFunction = "COUNT"
	AggSum   AggregateFunction = "SUM"
	AggAvg   AggregateFunction = "AVG"
	AggMin   AggregateFunction = "MIN"
	AggMax   AggregateFunction = "MAX"
)

type AggregateFieldInput struct {
	Field string            `json:"field"`
	Fn    AggregateFunction `json:"fn"`
	Alias string            `json:"alias,omitempty"`
}

type AggregateGroup struct {
	Keys   map[string]any `json:"keys"`
	Values map[string]any `json:"values"`
}

type AggregateResult struct {
	Groups      []AggregateGroup `json:"groups"`
	TotalGroups int              `json:"totalGroups"`
}

type IDFilter struct {
	Eq *string  `json:"eq,omitempty"`
	Ne *string  `json:"ne,omitempty"`
	In []string `json:"in,omitempty"`
}

type StringFilter struct {
	Eq         *string  `json:"eq,omitempty"`
	Ne         *string  `json:"ne,omitempty"`
	In         []string `json:"in,omitempty"`
	Contains   *string  `json:"contains,omitempty"`
	StartsWith *string  `json:"startsWith,omitempty"`
}

type BooleanFilter struct {
	Eq *bool `json:"eq,omitempty"`
	Ne *bool `json:"ne,omitempty"`
}

type IntFilter struct {
	Eq  *int  `json:"eq,omitempty"`
	Ne  *int  `json:"ne,omitempty"`
	In  []int `json:"in,omitempty"`
	Gt  *int  `json:"gt,omitempty"`
	Gte *int  `json:"gte,omitempty"`
	Lt  *int  `json:"lt,omitempty"`
	Lte *int  `json:"lte,omitempty"`
}

type ConnectionQueryParams[TFilter, TOrderBy any] struct {
	Filter  *TFilter  `json:"filter,omitempty"`
	OrderBy *TOrderBy `json:"orderBy,omitempty"`
	First   *int      `json:"first,omitempty"`
	After   *string   `json:"after,omitempty"`
	Last    *int      `json:"last,omitempty"`
	Before  *string   `json:"before,omitempty"`
}

type AggregateQueryParams[TFilter any] struct {
	Filter  *TFilter              `json:"filter,omitempty"`
	GroupBy []string              `json:"groupBy,omitempty"`
	Fields  []AggregateFieldInput `json:"fields"`
}

type SearchQueryParams[TFilter any] struct {
	Query  string   `json:"query"`
	Fields []string `json:"fields,omitempty"`
	Filter *TFilter `json:"filter,omitempty"`
	First  *int     `json:"first,omitempty"`
	After  *string  `json:"after,omitempty"`
}

// ResourceNames mirrors templ.ts ResourceNames.
type ResourceNames struct {
	Single    string // e.g. account
	List      string // e.g. accounts
	Aggregate string // e.g. accountAggregate; empty = unsupported
	Search    string // e.g. searchAccounts; empty = unsupported
}

const connectionFieldsTpl = `
  totalCount
  edges {
    node { NODE_FIELDS }
    cursor
  }
  pageInfo {
    hasNextPage
    hasPreviousPage
    startCursor
    endCursor
  }
`

const searchFieldsTpl = `
  totalCount
  hasNextPage
  hits {
    score
    node { NODE_FIELDS }
  }
`

const aggregateFields = `
  totalGroups
  groups {
    keys
    values
  }
`

func capitalize(value string) string {
	if value == "" {
		return value
	}
	r, size := utf8.DecodeRuneInString(value)
	return string(unicode.ToUpper(r)) + value[size:]
}

func pickNodeFields(defaultFields string, override []string) string {
	if len(override) > 0 && override[0] != "" {
		return override[0]
	}
	return defaultFields
}

func toVars(v any) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// GraphQLResource mirrors templ.ts GraphQLResource; transport is Client.GqlExec / Server.Exec.
type GraphQLResource[TNode, TFilter, TOrderBy any] struct {
	client     *Client
	names      ResourceNames
	nodeFields string
	typeName   string
}

func NewGraphQLResource[TNode, TFilter, TOrderBy any](
	c *Client,
	names ResourceNames,
	nodeFields string,
) *GraphQLResource[TNode, TFilter, TOrderBy] {
	return &GraphQLResource[TNode, TFilter, TOrderBy]{
		client:     c,
		names:      names,
		nodeFields: nodeFields,
		typeName:   capitalize(names.Single),
	}
}

func (r *GraphQLResource[TNode, TFilter, TOrderBy]) GetById(ctx context.Context, id string, nodeFields ...string) (*TNode, error) {
	fields := pickNodeFields(r.nodeFields, nodeFields)
	query := fmt.Sprintf(`
      query Get%s($id: ID!) {
        %s(id: $id) {
          %s
        }
      }
    `, r.typeName, r.names.Single, fields)
	return GqlExec[TNode](ctx, r.client, query, map[string]any{"id": id}, r.names.Single)
}

func (r *GraphQLResource[TNode, TFilter, TOrderBy]) List(
	ctx context.Context,
	params ConnectionQueryParams[TFilter, TOrderBy],
	nodeFields ...string,
) (*Connection[TNode], error) {
	fields := pickNodeFields(r.nodeFields, nodeFields)
	sel := strings.Replace(connectionFieldsTpl, "NODE_FIELDS", fields, 1)
	query := fmt.Sprintf(`
      query List%s(
        $filter: %sFilter
        $orderBy: %sOrderBy
        $first: Int
        $after: String
        $last: Int
        $before: String
      ) {
        %s(
          filter: $filter
          orderBy: $orderBy
          first: $first
          after: $after
          last: $last
          before: $before
        ) {
          %s
        }
      }
    `, capitalize(r.names.List), r.typeName, r.typeName, r.names.List, sel)
	vars, err := toVars(params)
	if err != nil {
		return nil, err
	}
	return GqlExec[Connection[TNode]](ctx, r.client, query, vars, r.names.List)
}

func (r *GraphQLResource[TNode, TFilter, TOrderBy]) Aggregate(
	ctx context.Context,
	params AggregateQueryParams[TFilter],
) (*AggregateResult, error) {
	if r.names.Aggregate == "" {
		return nil, fmt.Errorf("aggregate query is not configured for %s", r.names.Single)
	}
	query := fmt.Sprintf(`
      query Aggregate%s(
        $filter: %sFilter
        $groupBy: [String!]
        $fields: [AggregateFieldInput!]!
      ) {
        %s(
          filter: $filter
          groupBy: $groupBy
          fields: $fields
        ) {
          %s
        }
      }
    `, r.typeName, r.typeName, r.names.Aggregate, aggregateFields)
	vars, err := toVars(params)
	if err != nil {
		return nil, err
	}
	return GqlExec[AggregateResult](ctx, r.client, query, vars, r.names.Aggregate)
}

func (r *GraphQLResource[TNode, TFilter, TOrderBy]) Search(
	ctx context.Context,
	params SearchQueryParams[TFilter],
) (*SearchResult[TNode], error) {
	if r.names.Search == "" {
		return nil, fmt.Errorf("search query is not configured for %s", r.names.Single)
	}
	sel := strings.Replace(searchFieldsTpl, "NODE_FIELDS", r.nodeFields, 1)
	query := fmt.Sprintf(`
      query Search%s(
        $query: String!
        $fields: [String!]
        $filter: %sFilter
        $first: Int
        $after: String
      ) {
        %s(
          query: $query
          fields: $fields
          filter: $filter
          first: $first
          after: $after
        ) {
          %s
        }
      }
    `, capitalize(r.names.List), r.typeName, r.names.Search, sel)
	vars, err := toVars(params)
	if err != nil {
		return nil, err
	}
	return GqlExec[SearchResult[TNode]](ctx, r.client, query, vars, r.names.Search)
}
