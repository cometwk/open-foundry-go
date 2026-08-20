package api

import (
	"context"
	"strings"
	"sync"

	graphql "github.com/graph-gophers/graphql-go"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/query"
	"github.com/openfoundry/runtime/spi"
)

type memoKey struct{}

type expandMemo struct {
	mu  sync.Mutex
	adj map[string]map[string][]spi.OntologyObject
}

func newExpandMemo() *expandMemo {
	return &expandMemo{adj: map[string]map[string][]spi.OntologyObject{}}
}

func memoFrom(ctx context.Context) *expandMemo {
	m, _ := ctx.Value(memoKey{}).(*expandMemo)
	return m
}

func (m *expandMemo) lookup(startID, field string) ([]spi.OntologyObject, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	fields, ok := m.adj[startID]
	if !ok {
		return nil, false
	}
	kids, ok := fields[field]
	if !ok {
		return nil, false
	}
	return kids, true
}

func (m *expandMemo) merge(src map[string]map[string][]spi.OntologyObject) {
	if m == nil || src == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for pid, fields := range src {
		if m.adj[pid] == nil {
			m.adj[pid] = map[string][]spi.OntologyObject{}
		}
		for field, kids := range fields {
			m.adj[pid][field] = unionNodes(m.adj[pid][field], kids)
		}
	}
}

func unionNodes(dst, src []spi.OntologyObject) []spi.OntologyObject {
	seen := map[string]bool{}
	out := make([]spi.OntologyObject, 0, len(dst)+len(src))
	for _, o := range dst {
		id, _ := o[spi.FieldID].(string)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, o)
	}
	for _, o := range src {
		id, _ := o[spi.FieldID].(string)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, o)
	}
	return out
}

func compileExpand(ctx context.Context, ont *ir.Ontology, startType, fieldName string) (*query.Expand, error) {
	ot := ont.ObjectByName(startType)
	if ot == nil {
		return nil, query.ErrInvalidFollowPath
	}
	f := fieldByName(ot, fieldName)
	if f == nil || f.Role != ir.RoleLinkNav || f.Link == nil {
		return nil, query.ErrInvalidFollowPath
	}
	names := graphql.SelectedFieldNames(ctx)
	rest := childLinkPaths(ont, f.Type.Name, names, "")
	if len(rest) == 0 {
		return &query.Expand{
			StartType: startType,
			Mode:      query.ExpandGetLinks,
			Paths:     [][]string{{fieldName}},
		}, nil
	}
	paths := make([][]string, 0, len(rest))
	for _, r := range rest {
		p := append([]string{fieldName}, r...)
		paths = append(paths, p)
	}
	return &query.Expand{
		StartType: startType,
		Mode:      query.ExpandTraverse,
		Paths:     paths,
	}, nil
}

func childLinkPaths(ont *ir.Ontology, typ string, names []string, prefix string) [][]string {
	ot := ont.ObjectByName(typ)
	if ot == nil {
		return nil
	}
	var links []string
	for _, name := range immediateSelected(names, prefix) {
		f := fieldByName(ot, name)
		if f != nil && f.Role == ir.RoleLinkNav && f.Link != nil {
			links = append(links, name)
		}
	}
	if len(links) == 0 {
		return nil
	}
	var paths [][]string
	for _, name := range links {
		f := fieldByName(ot, name)
		nextPrefix := name
		if prefix != "" {
			nextPrefix = prefix + "." + name
		}
		rest := childLinkPaths(ont, f.Type.Name, names, nextPrefix)
		if len(rest) == 0 {
			paths = append(paths, []string{name})
			continue
		}
		for _, r := range rest {
			paths = append(paths, append([]string{name}, r...))
		}
	}
	return paths
}

func immediateSelected(names []string, prefix string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		rest := n
		if prefix != "" {
			if n == prefix {
				continue
			}
			p := prefix + "."
			if !strings.HasPrefix(n, p) {
				continue
			}
			rest = strings.TrimPrefix(n, p)
		}
		name := rest
		if i := strings.IndexByte(rest, '.'); i >= 0 {
			name = rest[:i]
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
