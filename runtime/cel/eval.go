// Package cel adapts packages/cel-evaluator for in-process evaluation.
// It never starts gRPC. Callers pass native maps; protobuf Values stay
// inside this package.
package cel

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/openfoundry/cel-evaluator/evaluator"
	"github.com/openfoundry/runtime/spi"
	"google.golang.org/protobuf/types/known/structpb"
)

var (
	evalOnce sync.Once
	evalInst *evaluator.Evaluator
	evalErr  error
)

func instance() (*evaluator.Evaluator, error) {
	evalOnce.Do(func() {
		evalInst, evalErr = evaluator.New()
	})
	return evalInst, evalErr
}

// Eval compiles and runs expr against vars with a dyn environment
// (no ODL typeEnv). Object-like maps strip SPI system fields, alias
// `_id` as `id`, and format time.Time as ISO-8601. Compile and runtime
// failures wrap spi.ErrCelEval.
func Eval(expr string, vars map[string]any) (any, error) {
	ev, err := instance()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", spi.ErrCelEval, err)
	}
	protoVars, err := toProtoVars(vars)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", spi.ErrCelEval, err)
	}
	result, err := ev.Evaluate(expr, protoVars, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", spi.ErrCelEval, err)
	}
	if result == nil {
		return nil, nil
	}
	return result.AsInterface(), nil
}

func toProtoVars(vars map[string]any) (map[string]*structpb.Value, error) {
	out := make(map[string]*structpb.Value, len(vars))
	for k, v := range vars {
		pv, err := structpb.NewValue(normalize(v))
		if err != nil {
			return nil, fmt.Errorf("variable %q: %w", k, err)
		}
		out[k] = pv
	}
	return out, nil
}

func normalize(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	case spi.OntologyObject:
		return normalizeMap(t)
	case map[string]any:
		return normalizeMap(t)
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = normalize(e)
		}
		return out
	default:
		return v
	}
}

func normalizeMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	var id any
	for k, v := range m {
		if k == spi.FieldID && v != nil {
			id = normalize(v)
		}
		if spi.IsSystemField(k) || strings.HasPrefix(k, "_") {
			continue
		}
		if v == nil {
			continue
		}
		out[k] = normalize(v)
	}
	if _, has := out["id"]; !has && id != nil {
		out["id"] = id
	}
	return out
}
