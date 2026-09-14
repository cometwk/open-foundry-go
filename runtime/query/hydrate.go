package query

import (
	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/spi"
)

// hydrateByIDs batch-fetches one type's objects by id through QueryObjects
// (an or-of-eq on _id). Empty ids skip the query entirely. Ids missing from
// the page are silently dropped — callers prune those branches; hard errors
// propagate.
func hydrateByIDs(eng *engine.Engine, ctx spi.RequestContext, typ string, ids []string, includeDeleted bool) ([]spi.OntologyObject, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ors := make([]spi.FilterExpression, 0, len(ids))
	for _, id := range ids {
		ors = append(ors, spi.FilterExpression{Field: spi.FieldID, Operator: "eq", Value: id})
	}
	page, err := eng.QueryObjects(ctx, typ, spi.FilterExpression{Or: ors}, &spi.QueryOptions{
		Limit:          len(ids),
		IncludeDeleted: includeDeleted,
		SkipTotalCount: true,
	})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// hydrateEdges returns payloads for every object endpoint referenced by
// edges, keyed by object id. Buckets come from the edges' own endpoint
// types, so a link type repeating across hops stays correct. Terminal ids
// (carried by tr.Nodes) and the start object's own id are removed from
// their buckets — but only those specific ids, not the entire type. This
// keeps a self-typed intermediate hydrated even when it shares a type with
// the terminal or start. Every remaining type costs at most one
// QueryObjects.
func hydrateEdges(eng *engine.Engine, ctx spi.RequestContext, edges []spi.OntologyLink, terminalType, startType, startID string, terminalIDs map[string]struct{}, includeDeleted bool, project map[string][]string) (map[string]spi.OntologyObject, error) {
	buckets := map[string]map[string]struct{}{}
	for _, e := range edges {
		collectEndpoint(buckets, e[spi.LinkFieldFromType], e[spi.LinkFieldFromID])
		collectEndpoint(buckets, e[spi.LinkFieldToType], e[spi.LinkFieldToID])
	}
	if termBucket := buckets[terminalType]; termBucket != nil {
		for id := range terminalIDs {
			delete(termBucket, id)
		}
		if len(termBucket) == 0 {
			delete(buckets, terminalType)
		}
	}
	if start := buckets[startType]; start != nil {
		delete(start, startID)
		if len(start) == 0 {
			delete(buckets, startType)
		}
	}
	objs := map[string]spi.OntologyObject{}
	for typ, set := range buckets {
		if _, skip := project[typ]; skip {
			continue
		}
		ids := make([]string, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		found, err := hydrateByIDs(eng, ctx, typ, ids, includeDeleted)
		if err != nil {
			return nil, err
		}
		for _, o := range found {
			putObj(objs, o)
		}
	}
	return objs, nil
}

func collectEndpoint(buckets map[string]map[string]struct{}, typ, id any) {
	t, _ := typ.(string)
	i, _ := id.(string)
	if t == "" || i == "" {
		return
	}
	if buckets[t] == nil {
		buckets[t] = map[string]struct{}{}
	}
	buckets[t][i] = struct{}{}
}
