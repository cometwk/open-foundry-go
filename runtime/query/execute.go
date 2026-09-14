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
			got, err := expandTraverse(eng, ctx, startObj, ex.StartType, ex.StartID, path, ex.Project)
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
	page, err := eng.GetLinks(ctx, startID, steps[0].LinkType, steps[0].Direction, &spi.QueryOptions{
		Limit: hopCap, SkipTotalCount: true,
	})
	if err != nil {
		return nil, err
	}
	// HasNextPage means the provider's +1 probe saw more than hopCap rows.
	// Duplicate links must not mask overflow, so this does not wait for the
	// unique-neighbor window to fill.
	if page.HasNextPage {
		return nil, fmt.Errorf("%w: hard cap %d", spi.ErrTraversalLimitExceeded, hopCap)
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(page.Items))
	for _, link := range page.Items {
		tid := neighborID(link, startID, steps[0].Direction)
		if tid == "" || seen[tid] {
			continue
		}
		seen[tid] = true
		ids = append(ids, tid)
	}
	// One batched read replaces the per-neighbor GetObject loop; missing
	// objects prune silently, query errors propagate.
	found, err := hydrateByIDs(eng, ctx, target, ids, false)
	if err != nil {
		return nil, err
	}
	byID := map[string]spi.OntologyObject{}
	for _, o := range found {
		putObj(byID, o)
	}
	kids := make([]spi.OntologyObject, 0, len(ids))
	for _, id := range ids {
		if o, ok := byID[id]; ok {
			kids = append(kids, o)
		}
	}
	adj := map[string]map[string][]spi.OntologyObject{}
	appendAdj(adj, startID, field, kids)
	return &ExpandResult{FirstHop: kids, Terminals: kids, Adjacency: adj}, nil
}

func expandTraverse(eng *engine.Engine, ctx spi.RequestContext, startObj spi.OntologyObject, startType, startID string, fields []string, project map[string][]string) (*ExpandResult, error) {
	steps, err := resolveSteps(eng.Ontology(), startType, fields)
	if err != nil {
		return nil, err
	}
	tr, err := eng.Traverse(ctx, startID, spi.TraversalPath{Steps: steps}, &spi.TraversalOptions{
		Limit: hopCap, SkipTotalCount: true, StartConfirmed: true, Project: project,
	})
	if err != nil {
		return nil, err
	}
	terminalType, err := hopTargetType(eng.Ontology(), startType, fields)
	if err != nil {
		return nil, err
	}
	// Intermediates hydrate from Edges endpoints — same row window as Nodes,
	// so a truncated fan-out yields a consistent partial tree. Terminal ids
	// are excluded (tr.Nodes carries them); only those ids, not the whole
	// type, so a self-typed intermediate stays hydrated. Types named in
	// Project are skipped (HopObjects / skeletons fill them).
	terminalIDs := map[string]struct{}{}
	for _, n := range tr.Nodes {
		if id, _ := n[spi.FieldID].(string); id != "" {
			terminalIDs[id] = struct{}{}
		}
	}
	hydrated, err := hydrateEdges(eng, ctx, tr.Edges, terminalType, startType, startID, terminalIDs, false, project)
	if err != nil {
		return nil, err
	}
	return assemblePath(startID, startObj, fields, steps, tr, hydrated, project), nil
}
