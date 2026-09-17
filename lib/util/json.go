package util

import (
	"encoding/json"
)

func MustPrettyJsonString(v any) string {
	json, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(json)
}

func MustJsonString(v any) string {
	json, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(json)
}

func MustParseJson[T any](data []byte) *T {
	var v T
	err := json.Unmarshal(data, &v)
	if err != nil {
		panic(err)
	}
	return &v
}
