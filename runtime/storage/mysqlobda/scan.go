package mysqlobda

type scanner interface {
	Scan(dest ...any) error
}

func scan(sc scanner, n int) ([]any, error) {
	dest := make([]any, n)
	ptrs := make([]any, n)
	for i := range dest {
		ptrs[i] = &dest[i]
	}
	if err := sc.Scan(ptrs...); err != nil {
		return nil, err
	}
	for i := range dest {
		dest[i] = unwrap(dest[i])
	}
	return dest, nil
}

func bizMap(vals []any, cols []string) map[string]any {
	out := make(map[string]any, len(cols))
	for i, c := range cols {
		out[c] = vals[i]
	}
	return out
}
