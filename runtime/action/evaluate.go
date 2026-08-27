package action

import (
	"fmt"
	"time"

	runcel "github.com/openfoundry/runtime/cel"
	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/spi"
)

// Actor is the fixture identity for actor.hasRole. Roles are a caller-supplied
// list; this phase does not consult OpenFGA.
type Actor struct {
	ID    string
	Type  string
	Roles []string
}

// Request names an action and supplies param values. Object-typed params
// are IDs (strings); they are resolved through Engine.GetObject before CEL.
type Request struct {
	Name   string
	Params map[string]any
	Actor  Actor
}

// Result holds the object snapshots resolved for CEL. Successful evaluation
// does not mutate ontology state; the caller may hand-write Engine verbs.
type Result struct {
	Objects map[string]spi.OntologyObject
}

// Evaluate resolves object params, then evaluates manifest preconditions
// in order against a captured snapshot. It never calls Create/Update/Delete.
func Evaluate(ctx spi.RequestContext, eng *engine.Engine, onto *ir.Ontology, manifests []Manifest, req Request) (*Result, error) {
	m := Lookup(manifests, req.Name)
	if m == nil {
		return nil, fmt.Errorf("action: %q is not loaded", req.Name)
	}
	sig := onto.ActionByName(req.Name)
	if sig == nil {
		return nil, fmt.Errorf("action: %q has no ActionType in the ontology", req.Name)
	}

	params := req.Params
	if params == nil {
		params = map[string]any{}
	}

	resolved := make(map[string]any, len(sig.Fields))
	objects := make(map[string]spi.OntologyObject)
	for i := range sig.Fields {
		f := &sig.Fields[i]
		if f.Role != ir.RoleParam {
			continue
		}
		val, ok := params[f.Name]
		if f.Type.NonNull && (!ok || isMissing(val)) {
			return nil, fmt.Errorf("action %s: missing required param %q", req.Name, f.Name)
		}
		if !ok || val == nil {
			resolved[f.Name] = nil
			continue
		}

		objType := onto.ObjectByName(f.Type.Name)
		if objType == nil {
			resolved[f.Name] = val
			continue
		}
		id, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("action %s: param %q must be an object id string, got %T", req.Name, f.Name, val)
		}
		obj, err := eng.GetObject(ctx, f.Type.Name, id)
		if err != nil {
			return nil, err
		}
		resolved[f.Name] = obj
		objects[f.Name] = obj
	}

	actorType := req.Actor.Type
	if actorType == "" {
		actorType = "user"
	}
	vars := make(map[string]any, len(resolved)+3)
	for k, v := range resolved {
		vars[k] = v
	}
	vars["params"] = params
	vars["actor"] = map[string]any{
		"id":    req.Actor.ID,
		"roles": req.Actor.Roles,
		"type":  actorType,
	}
	vars["now"] = time.Now().UTC().Format(time.RFC3339Nano)

	for _, pre := range m.Preconditions {
		out, err := runcel.Eval(pre.Expr, vars)
		if err != nil {
			return nil, err
		}
		if out != true {
			return nil, fmt.Errorf("%w: %s", spi.ErrPreconditionFailed, pre.Error)
		}
	}
	return &Result{Objects: objects}, nil
}

func isMissing(val any) bool {
	if val == nil {
		return true
	}
	s, ok := val.(string)
	return ok && s == ""
}
