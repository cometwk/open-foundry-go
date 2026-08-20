package query

import (
	"fmt"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
)

func resolveSteps(ont *ir.Ontology, startType string, fields []string) ([]spi.TraversalStep, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("%w: empty path", ErrInvalidFollowPath)
	}
	typ := startType
	steps := make([]spi.TraversalStep, 0, len(fields))
	for _, name := range fields {
		if name == "" {
			return nil, fmt.Errorf("%w: empty field", ErrInvalidFollowPath)
		}
		ot := ont.ObjectByName(typ)
		if ot == nil {
			return nil, fmt.Errorf("%w: unknown type %s", ErrInvalidFollowPath, typ)
		}
		f := fieldByName(ot, name)
		if f == nil || f.Role != ir.RoleLinkNav || f.Link == nil {
			return nil, fmt.Errorf("%w: %s.%s is not an @link field", ErrInvalidFollowPath, typ, name)
		}
		dir := "inbound"
		if f.Link.Direction == ir.DirectionOutbound {
			dir = "outbound"
		}
		steps = append(steps, spi.TraversalStep{LinkType: f.Link.Type, Direction: dir})
		typ = f.Type.Name
	}
	return steps, nil
}

func fieldByName(ot *ir.ObjectType, name string) *ir.Field {
	for i := range ot.Fields {
		if ot.Fields[i].Name == name {
			return &ot.Fields[i]
		}
	}
	return nil
}

func hopTargetType(ont *ir.Ontology, startType string, fields []string) (string, error) {
	typ := startType
	for _, name := range fields {
		ot := ont.ObjectByName(typ)
		if ot == nil {
			return "", fmt.Errorf("%w: unknown type %s", ErrInvalidFollowPath, typ)
		}
		f := fieldByName(ot, name)
		if f == nil {
			return "", fmt.Errorf("%w: %s.%s", ErrInvalidFollowPath, typ, name)
		}
		typ = f.Type.Name
	}
	return typ, nil
}

func neighborID(link spi.OntologyLink, fromID, direction string) string {
	from, _ := link[spi.LinkFieldFromID].(string)
	to, _ := link[spi.LinkFieldToID].(string)
	if direction == "outbound" {
		if from == fromID {
			return to
		}
		return ""
	}
	if to == fromID {
		return from
	}
	return ""
}

func putObj(dst map[string]spi.OntologyObject, obj spi.OntologyObject) {
	if obj == nil {
		return
	}
	id, _ := obj[spi.FieldID].(string)
	if id != "" {
		dst[id] = obj
	}
}

func objectID(obj spi.OntologyObject) string {
	id, _ := obj[spi.FieldID].(string)
	return id
}

func unionByID(dst, src []spi.OntologyObject) []spi.OntologyObject {
	seen := map[string]bool{}
	out := make([]spi.OntologyObject, 0, len(dst)+len(src))
	for _, o := range dst {
		id := objectID(o)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, o)
	}
	for _, o := range src {
		id := objectID(o)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, o)
	}
	return out
}

func appendAdj(adj map[string]map[string][]spi.OntologyObject, parentID, field string, kids []spi.OntologyObject) {
	if adj[parentID] == nil {
		adj[parentID] = map[string][]spi.OntologyObject{}
	}
	adj[parentID][field] = unionByID(adj[parentID][field], kids)
}

func mergeAdj(dst, src map[string]map[string][]spi.OntologyObject) {
	if dst == nil {
		return
	}
	for pid, fields := range src {
		for field, kids := range fields {
			appendAdj(dst, pid, field, kids)
		}
	}
}

func neighbors(parentID string, step spi.TraversalStep, edges []spi.OntologyLink, objs map[string]spi.OntologyObject, hopCap int) []spi.OntologyObject {
	var out []spi.OntologyObject
	seen := map[string]bool{}
	for _, e := range edges {
		if e[spi.FieldType] != step.LinkType {
			continue
		}
		nid := neighborID(e, parentID, step.Direction)
		if nid == "" || seen[nid] {
			continue
		}
		obj, ok := objs[nid]
		if !ok {
			continue
		}
		seen[nid] = true
		out = append(out, obj)
		if len(out) >= hopCap {
			break
		}
	}
	return out
}

func assemblePath(startID string, startObj spi.OntologyObject, fields []string, steps []spi.TraversalStep, tr spi.TraversalResult) *ExpandResult {
	objs := map[string]spi.OntologyObject{}
	putObj(objs, startObj)
	for _, o := range tr.Visited {
		putObj(objs, o)
	}
	for _, o := range tr.Nodes {
		putObj(objs, o)
	}
	adj := map[string]map[string][]spi.OntologyObject{}
	frontier := []string{startID}
	for i, field := range fields {
		var next []string
		nextSeen := map[string]bool{}
		for _, pid := range frontier {
			nbs := neighbors(pid, steps[i], tr.Edges, objs, HopCap)
			appendAdj(adj, pid, field, nbs)
			for _, n := range nbs {
				nid := objectID(n)
				if nid == "" || nextSeen[nid] {
					continue
				}
				nextSeen[nid] = true
				next = append(next, nid)
			}
		}
		frontier = next
	}
	var first []spi.OntologyObject
	if fields[0] != "" && adj[startID] != nil {
		first = adj[startID][fields[0]]
	}
	terminals := make([]spi.OntologyObject, 0, len(frontier))
	for _, id := range frontier {
		if o, ok := objs[id]; ok {
			terminals = append(terminals, o)
		}
	}
	if first == nil {
		first = []spi.OntologyObject{}
	}
	return &ExpandResult{FirstHop: first, Terminals: terminals, Adjacency: adj}
}
