package sqliteobda

import (
	"fmt"
	"time"

	"github.com/openfoundry/runtime/internal/uuidv7"
	"github.com/openfoundry/runtime/obda"
	sqlitedialect "github.com/openfoundry/runtime/obda/dialect/sqlite"
	"github.com/openfoundry/runtime/obda/sqlast"
)

// backfill inserts sidecar meta for business rows that have no matching meta.
// tenant_id is copied from the business tenant column (or the constant),
// never from ApplySchema's RequestContext.
func (p *Provider) backfill(db DBTX, compiled *obda.Compiled) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, m := range compiled.Models {
		if m.SystemStrategy != "sidecar" {
			continue
		}
		if len(m.IdentityColumns) == 0 {
			continue
		}
		cols := append([]string(nil), m.IdentityColumns...)
		if m.TenantStrategy == "column" && m.TenantColumn != "" {
			cols = append(cols, m.TenantColumn)
		}
		sel := &sqlast.Select{
			From:    sqlast.Identifier{Name: m.Table},
			Columns: make([]sqlast.Expr, len(cols)),
		}
		for i, c := range cols {
			sel.Columns[i] = sqlast.Identifier{Name: c}
		}
		stmt, err := p.dialect.Render(sel)
		if err != nil {
			return err
		}
		rows, err := db.Query(stmt.SQL)
		if err != nil {
			return sqlitedialect.Classify(err)
		}
		func() {
			defer rows.Close()
			dest := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range dest {
				ptrs[i] = &dest[i]
			}
			for rows.Next() {
				if err = rows.Scan(ptrs...); err != nil {
					return
				}
				idVals := make([]any, len(m.IdentityColumns))
				for i := range m.IdentityColumns {
					idVals[i] = dest[i]
				}
				tenant := m.TenantValue
				if m.TenantStrategy == "column" {
					tenant = fmt.Sprint(dest[len(m.IdentityColumns)])
				}
				if tenant == "" {
					continue
				}
				pk := obda.EncodePhysicalKey(idVals)
				var exists int
				if err = db.QueryRow(
					`SELECT COUNT(*) FROM "of_object_meta" WHERE tenant_id = ? AND object_type = ? AND physical_key = ?`,
					tenant, m.Name, pk,
				).Scan(&exists); err != nil {
					return
				}
				if exists > 0 {
					continue
				}
				_, err = db.Exec(
					`INSERT INTO "of_object_meta" (engine_id, tenant_id, object_type, physical_key, version, created_at, updated_at) VALUES (?, ?, ?, ?, 1, ?, ?)`,
					uuidv7.New(), tenant, m.Name, pk, now, now,
				)
				if err != nil {
					return
				}
			}
			err = rows.Err()
		}()
		if err != nil {
			return err
		}
	}
	return nil
}
