package testutil

import (
	"encoding/json"
	"fmt"
)

func PrintPretty(v any) {
	json, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(json))
}
