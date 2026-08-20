package api

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

type pageInfo struct {
	HasNextPage     bool
	HasPreviousPage bool
	StartCursor     *string
	EndCursor       *string
}

type Connection struct {
	Edges      []*Edge
	PageInfo   pageInfo
	TotalCount int32
}

type Edge struct {
	Node   *node
	Cursor string
}

type SearchHit struct {
	Node  *node
	Score float64
}

type SearchResult struct {
	Hits        []*SearchHit
	TotalCount  int32
	HasNextPage bool
}

type AggregateGroup struct {
	Keys   JSON
	Values JSON
}

type AggregateResult struct {
	Groups      []*AggregateGroup
	TotalGroups int32
}

type listArgs struct {
	Filter  GQLInput
	OrderBy GQLInput
	First   *int32
	After   *string
	Last    *int32
	Before  *string
}

type aggregateArgs struct {
	Filter  GQLInput
	GroupBy *[]string
	Fields  []aggregateFieldInput
}

type aggregateFieldInput struct {
	Field string
	Fn    string
	Alias *string
}

type searchArgs struct {
	Query  string
	Fields *[]string
	Filter GQLInput
	First  *int32
	After  *string
}

func encodeCursor(offset int) string {
	return base64.StdEncoding.EncodeToString([]byte("cursor:" + strconv.Itoa(offset)))
}

func decodeCursor(cursor string) (int, error) {
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor")
	}
	s := string(raw)
	if !strings.HasPrefix(s, "cursor:") {
		return 0, fmt.Errorf("invalid cursor format: %s", cursor)
	}
	n, err := strconv.Atoi(s[len("cursor:"):])
	if err != nil {
		return 0, fmt.Errorf("invalid cursor format: %s", cursor)
	}
	return n, nil
}

func resolvePagination(args listArgs) (offset, limit int, err error) {
	offset = 0
	limit = defaultPageSize
	if args.After != nil && *args.After != "" {
		n, err := decodeCursor(*args.After)
		if err != nil {
			return 0, 0, err
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
	if args.Before != nil && *args.Before != "" {
		beforeOffset, err := decodeCursor(*args.Before)
		if err != nil {
			return 0, 0, err
		}
		requestedLast := defaultPageSize
		if args.Last != nil {
			requestedLast = int(*args.Last)
		}
		if requestedLast > maxPageSize {
			requestedLast = maxPageSize
		}
		offset = beforeOffset - requestedLast
		if offset < 0 {
			offset = 0
		}
		limit = requestedLast
	}
	return offset, limit, nil
}

func buildConnection(nodes []*node, totalCount, offset int) *Connection {
	edges := make([]*Edge, len(nodes))
	for i, n := range nodes {
		edges[i] = &Edge{Node: n, Cursor: encodeCursor(offset + i)}
	}
	var start, end *string
	if len(edges) > 0 {
		s := edges[0].Cursor
		e := edges[len(edges)-1].Cursor
		start, end = &s, &e
	}
	return &Connection{
		Edges: edges,
		PageInfo: pageInfo{
			HasNextPage:     offset+len(nodes) < totalCount,
			HasPreviousPage: offset > 0,
			StartCursor:     start,
			EndCursor:       end,
		},
		TotalCount: int32(totalCount),
	}
}
