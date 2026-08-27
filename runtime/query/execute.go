package query

import (
	"errors"
	"fmt"

	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/spi"
)

// ErrInvalidFollowPath is returned when an Expand path is not a sequence
// of RoleLinkNav field names on the types at each hop. HTTP maps this to
// 400 INVALID_FOLLOW_PATH and must not call SPI.
var ErrInvalidFollowPath = errors.New("invalid follow path")

// Execute runs one Query IR op against Engine. Projections must not call SPI.
func Execute(eng *engine.Engine, ctx spi.RequestContext, op Op) (Result, error) {
	switch {
	case op.Get != nil:
		return execGet(eng, ctx, op.Get)
	case op.List != nil:
		page, err := eng.QueryObjects(ctx, op.List.Type, op.List.Filter, op.List.Options)
		return Result{Page: page}, err
	case op.Aggregate != nil:
		got, err := eng.AggregateObjects(ctx, op.Aggregate.Type, op.Aggregate.Query)
		return Result{Aggregate: got}, err
	case op.Search != nil:
		got, err := eng.SearchObjects(ctx, op.Search.Type, op.Search.Query)
		return Result{Search: got}, err
	case op.Expand != nil:
		return execExpand(eng, ctx, op.Expand)
	default:
		return Result{}, fmt.Errorf("query: empty op")
	}
}

func execGet(eng *engine.Engine, ctx spi.RequestContext, g *Get) (Result, error) {
	var opts *engine.GetObjectOpts
	if g.Computed != nil {
		opts = &engine.GetObjectOpts{ComputedFields: *g.Computed}
	}
	obj, err := eng.GetObjectOpts(ctx, g.Type, g.ID, opts)
	if err != nil {
		return Result{}, err
	}
	return Result{Object: obj}, nil
}

func execExpand(eng *engine.Engine, ctx spi.RequestContext, ex *Expand) (Result, error) {
	if len(ex.Paths) == 0 {
		return Result{}, fmt.Errorf("%w: empty path", ErrInvalidFollowPath)
	}
	ont := eng.Ontology()
	for _, path := range ex.Paths {
		if _, err := resolveSteps(ont, ex.StartType, path); err != nil {
			return Result{}, err
		}
	}

	var startObj spi.OntologyObject
	if ex.CheckStart {
		obj, err := eng.GetObject(ctx, ex.StartType, ex.StartID)
		if err != nil {
			return Result{}, err
		}
		startObj = obj
	}

	out := &ExpandResult{
		FirstHop:  []spi.OntologyObject{},
		Terminals: []spi.OntologyObject{},
		Adjacency: map[string]map[string][]spi.OntologyObject{},
	}

	switch ex.Mode {
	case ExpandGetLinks:
		path := ex.Paths[0]
		if len(path) != 1 {
			return Result{}, fmt.Errorf("%w: GetLinks expects a one-field path", ErrInvalidFollowPath)
		}
		got, err := expandGetLinks(eng, ctx, ex.StartType, ex.StartID, path[0])
		if err != nil {
			return Result{}, err
		}
		return Result{Expand: got}, nil
	case ExpandTraverse:
		termSeen := map[string]bool{}
		for _, path := range ex.Paths {
			got, err := expandTraverse(eng, ctx, startObj, ex.StartType, ex.StartID, path)
			if err != nil {
				return Result{}, err
			}
			mergeAdj(out.Adjacency, got.Adjacency)
			if len(path) > 0 {
				out.FirstHop = unionByID(out.FirstHop, out.Adjacency[ex.StartID][path[0]])
			}
			for _, n := range got.Terminals {
				id := objectID(n)
				if id == "" || termSeen[id] {
					continue
				}
				termSeen[id] = true
				out.Terminals = append(out.Terminals, n)
			}
		}
		return Result{Expand: out}, nil
	default:
		return Result{}, fmt.Errorf("query: unknown expand mode %d", ex.Mode)
	}
}

func expandGetLinks(eng *engine.Engine, ctx spi.RequestContext, startType, startID, field string) (*ExpandResult, error) {
	steps, err := resolveSteps(eng.Ontology(), startType, []string{field})
	if err != nil {
		return nil, err
	}
	target, err := hopTargetType(eng.Ontology(), startType, []string{field})
	if err != nil {
		return nil, err
	}
	page, err := eng.GetLinks(ctx, startID, steps[0].LinkType, steps[0].Direction, &spi.QueryOptions{Limit: HopCap})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	kids := make([]spi.OntologyObject, 0, len(page.Items))
	for _, link := range page.Items {
		tid := neighborID(link, startID, steps[0].Direction)
		if tid == "" || seen[tid] {
			continue
		}
		seen[tid] = true
		obj, err := eng.GetObject(ctx, target, tid)
		if err != nil {
			if errors.Is(err, spi.ErrObjectNotFound) {
				continue
			}
			return nil, err
		}
		kids = append(kids, obj)
		if len(kids) >= HopCap {
			break
		}
	}
	adj := map[string]map[string][]spi.OntologyObject{}
	appendAdj(adj, startID, field, kids)
	return &ExpandResult{FirstHop: kids, Terminals: kids, Adjacency: adj}, nil
}

func expandTraverse(eng *engine.Engine, ctx spi.RequestContext, startObj spi.OntologyObject, startType, startID string, fields []string) (*ExpandResult, error) {
	steps, err := resolveSteps(eng.Ontology(), startType, fields)
	if err != nil {
		return nil, err
	}
	tr, err := eng.Traverse(ctx, startID, spi.TraversalPath{Steps: steps}, &spi.TraversalOptions{Limit: HopCap})
	if err != nil {
		return nil, err
	}
	return assemblePath(startID, startObj, fields, steps, tr), nil
}
