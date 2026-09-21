package client

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	graphql "github.com/graph-gophers/graphql-go"
	gqlerrors "github.com/graph-gophers/graphql-go/errors"
	"github.com/openfoundry/runtime/spi"
)

type stubExec struct {
	lastRC    spi.RequestContext
	lastQuery string
	lastVars  map[string]any
	res       *graphql.Response
}

func (s *stubExec) Exec(_ context.Context, rc spi.RequestContext, query string, vars map[string]any) *graphql.Response {
	s.lastRC = rc
	s.lastQuery = query
	s.lastVars = vars
	return s.res
}

func newStubClient(res *graphql.Response, opts ...ClientOption) (*Client, *stubExec) {
	stub := &stubExec{res: res}
	c := &Client{
		exec: stub,
		rc:   spi.RequestContext{TenantID: DefaultTenant, ActorID: DefaultActor},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, stub
}

func TestGqlExec_DecodesNamedField(t *testing.T) {
	c, stub := newStubClient(&graphql.Response{
		Data: json.RawMessage(`{"chat":{"id":"c1","title":"hi","visibility":"PRIVATE"}}`),
	})
	got, err := GqlExec[Chat](context.Background(), c, `query($id:ID!){ chat(id:$id){ id title visibility } }`, map[string]any{"id": "c1"}, "chat")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || got.ID != "c1" || got.Title != "hi" || got.Visibility != VisibilityPrivate {
		t.Fatalf("got = %+v", got)
	}
	if stub.lastRC.TenantID != DefaultTenant || stub.lastRC.ActorID != DefaultActor {
		t.Fatalf("rc = %+v", stub.lastRC)
	}
	if stub.lastVars["id"] != "c1" {
		t.Fatalf("vars = %#v", stub.lastVars)
	}
}

func TestGqlExec_NullField(t *testing.T) {
	c, _ := newStubClient(&graphql.Response{
		Data: json.RawMessage(`{"chat":null}`),
	})
	got, err := GqlExec[Chat](context.Background(), c, `{ chat(id:"x"){ id } }`, nil, "chat")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != nil {
		t.Fatalf("got = %+v, want nil", got)
	}
}

func TestGqlExec_MissingField(t *testing.T) {
	c, _ := newStubClient(&graphql.Response{
		Data: json.RawMessage(`{"other":{"id":"1"}}`),
	})
	got, err := GqlExec[Chat](context.Background(), c, `{ other { id } }`, nil, "chat")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != nil {
		t.Fatalf("got = %+v, want nil", got)
	}
}

func TestGqlExec_FirstGraphQLError(t *testing.T) {
	c, _ := newStubClient(&graphql.Response{
		Errors: []*gqlerrors.QueryError{
			{Message: "boom"},
			{Message: "second"},
		},
		Data: json.RawMessage(`{"chat":{"id":"c1"}}`),
	})
	got, err := GqlExec[Chat](context.Background(), c, `{ chat(id:"x"){ id } }`, nil, "chat")
	if got != nil {
		t.Fatalf("got = %+v", got)
	}
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v", err)
	}
}

func TestGqlExec_Connection(t *testing.T) {
	c, _ := newStubClient(&graphql.Response{
		Data: json.RawMessage(`{
			"chats":{
				"totalCount":1,
				"edges":[{"node":{"id":"c1","title":"t","visibility":"PUBLIC"},"cursor":"cur"}],
				"pageInfo":{"hasNextPage":false,"hasPreviousPage":false}
			}
		}`),
	})
	got, err := GqlExec[Connection[Chat]](context.Background(), c, `{ chats { totalCount edges { node { id title visibility } cursor } pageInfo { hasNextPage hasPreviousPage } } }`, nil, "chats")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || got.TotalCount != 1 || len(got.Edges) != 1 || got.Edges[0].Node.ID != "c1" {
		t.Fatalf("got = %+v", got)
	}
	if got.Edges[0].Node.Visibility != VisibilityPublic {
		t.Fatalf("visibility = %v", got.Edges[0].Node.Visibility)
	}
}

func TestCreateClient_Options(t *testing.T) {
	c, stub := newStubClient(&graphql.Response{Data: json.RawMessage(`{"chat":null}`)},
		WithTenant("other"), WithActor("alice"))
	_, _ = GqlExec[Chat](context.Background(), c, `{ chat(id:"x"){ id } }`, nil, "chat")
	if stub.lastRC.TenantID != "other" || stub.lastRC.ActorID != "alice" {
		t.Fatalf("rc = %+v", stub.lastRC)
	}
}

func TestDecodeField_BadJSON(t *testing.T) {
	_, err := decodeField[Chat](json.RawMessage(`not-json`), "chat")
	if err == nil {
		t.Fatal("want decode error")
	}
}

func TestGraphQLResource_GetById(t *testing.T) {
	c, stub := newStubClient(&graphql.Response{
		Data: json.RawMessage(`{"chat":{"id":"c1","title":"hi","visibility":"PRIVATE"}}`),
	})
	api := NewGraphQLResource[Chat, ChatFilter, ChatOrderBy](c, ResourceNames{
		Single:    "chat",
		List:      "chats",
		Aggregate: "chatAggregate",
		Search:    "searchChats",
	}, chatFields)
	got, err := api.GetById(context.Background(), "c1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || got.ID != "c1" {
		t.Fatalf("got = %+v", got)
	}
	if !strings.Contains(stub.lastQuery, "query GetChat") || !strings.Contains(stub.lastQuery, "chat(id: $id)") {
		t.Fatalf("query = %s", stub.lastQuery)
	}
}

func TestGraphQLResource_List(t *testing.T) {
	c, stub := newStubClient(&graphql.Response{
		Data: json.RawMessage(`{"chats":{"totalCount":0,"edges":[],"pageInfo":{"hasNextPage":false,"hasPreviousPage":false}}}`),
	})
	api := NewGraphQLResource[Chat, ChatFilter, ChatOrderBy](c, ResourceNames{
		Single: "chat", List: "chats",
	}, chatFields)
	first := 10
	got, err := api.List(context.Background(), ConnectionQueryParams[ChatFilter, ChatOrderBy]{First: &first})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || got.TotalCount != 0 {
		t.Fatalf("got = %+v", got)
	}
	if stub.lastVars["first"] != float64(10) {
		t.Fatalf("vars = %#v", stub.lastVars)
	}
}

func TestGraphQLResource_AggregateRequiresName(t *testing.T) {
	c, _ := newStubClient(&graphql.Response{})
	api := NewGraphQLResource[Chat, ChatFilter, ChatOrderBy](c, ResourceNames{Single: "chat", List: "chats"}, chatFields)
	_, err := api.Aggregate(context.Background(), AggregateQueryParams[ChatFilter]{
		Fields: []AggregateFieldInput{{Field: "*", Fn: AggCount}},
	})
	if err == nil || !strings.Contains(err.Error(), "aggregate") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewAPIs(t *testing.T) {
	c, _ := newStubClient(&graphql.Response{Data: json.RawMessage(`{"chat":null}`)})
	apis := NewAPIs(c)
	if apis.Account == nil || apis.Chat == nil || apis.Message == nil {
		t.Fatal("nil resource")
	}
	got, err := apis.Chat.GetById(context.Background(), "missing")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != nil {
		t.Fatalf("got = %+v", got)
	}
}
