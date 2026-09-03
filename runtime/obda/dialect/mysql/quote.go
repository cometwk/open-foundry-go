package mysql

import (
	"fmt"
	"regexp"

	"github.com/openfoundry/runtime/obda/sqlast"
)

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func quote(id sqlast.Identifier) (string, error) {
	name, err := quoteName(id.Name)
	if err != nil {
		return "", err
	}
	if id.Qualifier == "" {
		return name, nil
	}
	q, err := quoteName(id.Qualifier)
	if err != nil {
		return "", err
	}
	return q + "." + name, nil
}

func quoteName(name string) (string, error) {
	if !identRe.MatchString(name) {
		return "", fmt.Errorf("mysql: invalid identifier %q", name)
	}
	if containsQuote(name) {
		return "", fmt.Errorf("mysql: identifier contains quote %q", name)
	}
	return "`" + name + "`", nil
}

func quoteTable(id sqlast.Identifier, alias string) (string, error) {
	tbl, err := quote(id)
	if err != nil {
		return "", err
	}
	if alias == "" {
		return tbl, nil
	}
	a, err := quoteName(alias)
	if err != nil {
		return "", err
	}
	return tbl + " AS " + a, nil
}

func containsQuote(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			return true
		}
	}
	return false
}
