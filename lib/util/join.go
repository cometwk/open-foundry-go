package util

import (
	"fmt"
	"strings"
)

func AnySliceJoin(args []any, sep string) string {
	var sb strings.Builder
	for i, v := range args {
		if i > 0 {
			sb.WriteString(sep)
		}
		sb.WriteString(fmt.Sprint(v))
	}
	return sb.String()
}
