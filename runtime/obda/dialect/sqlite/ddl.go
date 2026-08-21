package sqlite

import (
	"fmt"
	"strings"

	"github.com/openfoundry/runtime/obda/sqlast"
)

// SidecarStatements is the STRICT metadata schema, one statement per entry.
func SidecarStatements(prefix string) ([]string, error) {
	stem := strings.TrimSuffix(prefix, "_")
	if stem == "" {
		stem = "of"
	}
	if _, err := quote(sqlast.Identifier{Name: stem}); err != nil {
		return nil, fmt.Errorf("sidecar prefix: %w", err)
	}
	return []string{
		`CREATE TABLE IF NOT EXISTS of_schema_versions (
  version INTEGER PRIMARY KEY,
  snapshot TEXT NOT NULL
) STRICT`,
		`CREATE TABLE IF NOT EXISTS of_mapping_versions (
  version INTEGER PRIMARY KEY,
  document TEXT NOT NULL,
  dsn_ref TEXT NOT NULL DEFAULT ''
) STRICT`,
		`CREATE TABLE IF NOT EXISTS of_mapping_activation (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  mapping_version INTEGER NOT NULL,
  schema_version INTEGER NOT NULL,
  fingerprint TEXT NOT NULL DEFAULT ''
) STRICT`,
		`CREATE TABLE IF NOT EXISTS of_object_meta (
  engine_id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  object_type TEXT NOT NULL,
  physical_key TEXT NOT NULL,
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT
) STRICT`,
		`CREATE TABLE IF NOT EXISTS of_link_meta (
  engine_id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  link_type TEXT NOT NULL,
  from_id TEXT NOT NULL,
  to_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT
) STRICT`,
		`CREATE TABLE IF NOT EXISTS of_object_history (
  engine_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  tenant_id TEXT NOT NULL,
  snapshot TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (engine_id, version)
) STRICT`,
		`CREATE TABLE IF NOT EXISTS of_link_history (
  engine_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  tenant_id TEXT NOT NULL,
  snapshot TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (engine_id, version)
) STRICT`,
		`CREATE TABLE IF NOT EXISTS of_idempotency (
  tenant_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  result TEXT NOT NULL,
  PRIMARY KEY (tenant_id, idempotency_key)
) STRICT`,
		`CREATE TABLE IF NOT EXISTS of_index_registry (
  object_type TEXT NOT NULL,
  field TEXT NOT NULL,
  index_type TEXT NOT NULL,
  PRIMARY KEY (object_type, field)
) STRICT`,
		`CREATE UNIQUE INDEX IF NOT EXISTS of_object_meta_key
  ON of_object_meta (tenant_id, object_type, physical_key)`,
	}, nil
}
