package api

import (
	"strings"

	"github.com/openfoundry/runtime/spi"
)

var filterOpMap = map[string]string{
	"eq":         "eq",
	"ne":         "neq",
	"gt":         "gt",
	"gte":        "gte",
	"lt":         "lt",
	"lte":        "lte",
	"in":         "in",
	"contains":   "contains",
	"startsWith": "startsWith",
	"exists":     "exists",
}

func convertFilter(in map[string]any) *spi.FilterExpression {
	if len(in) == 0 {
		return nil
	}
	var predicates []spi.FilterExpression
	for key, value := range in {
		if value == nil {
			continue
		}
		switch key {
		case "AND":
			subs := convertFilterList(value)
			if len(subs) > 0 {
				predicates = append(predicates, spi.FilterExpression{And: subs})
			}
		case "OR":
			subs := convertFilterList(value)
			if len(subs) > 0 {
				predicates = append(predicates, spi.FilterExpression{Or: subs})
			}
		case "NOT":
			m, ok := value.(map[string]any)
			if !ok {
				if gi, ok := value.(GQLInput); ok {
					m = gi
				}
			}
			if sub := convertFilter(m); sub != nil {
				predicates = append(predicates, spi.FilterExpression{Not: sub})
			}
		default:
			ops, ok := asMap(value)
			if !ok {
				continue
			}
			field := key
			if field == "id" {
				field = spi.FieldID
			}
			for op, opVal := range ops {
				if opVal == nil {
					continue
				}
				spiOp, ok := filterOpMap[op]
				if !ok {
					continue
				}
				predicates = append(predicates, spi.FilterExpression{
					Field:    field,
					Operator: spiOp,
					Value:    opVal,
				})
			}
		}
	}
	if len(predicates) == 0 {
		return nil
	}
	if len(predicates) == 1 {
		p := predicates[0]
		return &p
	}
	return &spi.FilterExpression{And: predicates}
}

func convertFilterList(value any) []spi.FilterExpression {
	var items []any
	switch v := value.(type) {
	case []any:
		items = v
	case []map[string]any:
		for _, m := range v {
			items = append(items, m)
		}
	case []GQLInput:
		for _, m := range v {
			items = append(items, map[string]any(m))
		}
	default:
		return nil
	}
	var out []spi.FilterExpression
	for _, item := range items {
		m, ok := asMap(item)
		if !ok {
			continue
		}
		if sub := convertFilter(m); sub != nil {
			out = append(out, *sub)
		}
	}
	return out
}

func convertOrderBy(in map[string]any) []spi.OrderBy {
	if len(in) == 0 {
		return nil
	}
	var out []spi.OrderBy
	for field, dir := range in {
		if dir == nil {
			continue
		}
		s, ok := dir.(string)
		if !ok {
			continue
		}
		if field == "id" {
			field = spi.FieldID
		}
		d := strings.ToLower(s)
		if d != "asc" && d != "desc" {
			continue
		}
		out = append(out, spi.OrderBy{Field: field, Direction: d})
	}
	return out
}

func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case GQLInput:
		return m, true
	default:
		return nil, false
	}
}

func emptyFilter() spi.FilterExpression {
	return spi.FilterExpression{}
}

func filterOrEmpty(in map[string]any) spi.FilterExpression {
	if f := convertFilter(in); f != nil {
		return *f
	}
	return emptyFilter()
}
