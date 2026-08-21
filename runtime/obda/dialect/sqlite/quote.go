package sqlite

import (
	"fmt"
	"regexp"

	"github.com/openfoundry/runtime/obda/sqlast"
)

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func quote(id sqlast.Identifier) (string, error) {
	if !identRe.MatchString(id.Name) {
		return "", fmt.Errorf("sqlite: invalid identifier %q", id.Name)
	}
	if containsQuote(id.Name) {
		return "", fmt.Errorf("sqlite: identifier contains quote %q", id.Name)
	}
	return `"` + id.Name + `"`, nil
}

func containsQuote(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			return true
		}
	}
	return false
}
