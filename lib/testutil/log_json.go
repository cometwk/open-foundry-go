package testutil

import (
	"encoding/json"
	"fmt"
)

func PrintPretty(v any) {
	fmt.Println(Pretty(v))
}

func Pretty(v any) string {
	json, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(json)
}
