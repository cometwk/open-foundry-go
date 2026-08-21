package obda

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type directPayload struct {
	Type string   `json:"t"`
	Keys []string `json:"k"`
}

// EncodeDirect packs a typed physical key. It is reversible and delimiter-free.
func EncodeDirect(typ string, keys []string) string {
	b, _ := json.Marshal(directPayload{Type: typ, Keys: keys})
	return base64.RawURLEncoding.EncodeToString(b)
}

// EncodePhysicalKey stores identity column values for sidecar lookup.
func EncodePhysicalKey(vals []any) string {
	if len(vals) == 1 {
		return fmt.Sprint(vals[0])
	}
	keys := make([]string, len(vals))
	for i, v := range vals {
		keys[i] = fmt.Sprint(v)
	}
	b, _ := json.Marshal(keys)
	return string(b)
}

// DecodeDirect unpacks an id produced by EncodeDirect.
func DecodeDirect(id string) (typ string, keys []string, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", nil, fmt.Errorf("identity: decode")
	}
	var p directPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.Type == "" {
		return "", nil, fmt.Errorf("identity: decode")
	}
	return p.Type, p.Keys, nil
}
