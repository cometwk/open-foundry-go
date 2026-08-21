package sqlite

import "github.com/openfoundry/runtime/obda/sqlast"

// FTSTableSQL creates a provider-owned FTS5 virtual table keyed by engine id.
func FTSTableSQL(name string) (string, error) {
	q, err := quote(sqlast.Identifier{Name: name})
	if err != nil {
		return "", err
	}
	return "CREATE VIRTUAL TABLE IF NOT EXISTS " + q + " USING fts5(engine_id UNINDEXED, tenant_id UNINDEXED, body)", nil
}
