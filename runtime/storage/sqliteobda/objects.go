package sqliteobda

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/openfoundry/runtime/internal/uuidv7"
	"github.com/openfoundry/runtime/obda"
	sqlitedialect "github.com/openfoundry/runtime/obda/dialect/sqlite"
	"github.com/openfoundry/runtime/spi"
)

type metaRow struct {
	EngineID  string
	TenantID  string
	Type      string
	Key       string
	Version   int
	CreatedAt string
	UpdatedAt string
	DeletedAt sql.NullString
}

func (p *Provider) begin() (*sql.Tx, *sql.Conn, error) {
	ctx := context.Background()
	conn, err := p.db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return tx, conn, nil
}

func (a *activation) model(typ string) (*obda.CompiledModel, error) {
	m, ok := a.compiled.Models[typ]
	if !ok {
		return nil, spi.ErrObjectNotFound
	}
	return m, nil
}

func (p *Provider) CreateObject(ctx spi.RequestContext, typ string, properties map[string]any) (spi.OntologyObject, error) {
	act, err := p.pin(ctx)
	if err != nil {
		return nil, err
	}
	return withTx(p, func(tx DBTX) (spi.OntologyObject, error) {
		return p.createObjectTx(tx, act, ctx, typ, properties)
	})
}

func (p *Provider) createObjectTx(tx DBTX, act *activation, ctx spi.RequestContext, typ string, properties map[string]any) (spi.OntologyObject, error) {
	m, err := act.model(typ)
	if err != nil {
		return nil, err
	}
	if !m.Writable() {
		return nil, spi.ErrReadOnlyMapping
	}
	props := copyUserProps(properties)
	keys, err := identityFromProps(m, props)
	if err != nil {
		return nil, err
	}
	pk := obda.EncodePhysicalKey(keys)
	engineID := uuidv7.New()
	if m.IdentityStrategy == "direct" {
		engineID = obda.EncodeDirect(m.Name, stringify(keys))
	}
	now := nowRFC3339()
	var existing string
	err = tx.QueryRow(
		`SELECT engine_id FROM "of_object_meta" WHERE tenant_id = ? AND object_type = ? AND physical_key = ?`,
		ctx.TenantID, m.Name, pk,
	).Scan(&existing)
	if err == nil {
		return nil, fmt.Errorf("%w: physical key exists", spi.ErrInvalidMapping)
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, sqlitedialect.Classify(err)
	}
	if err := p.insertBusiness(tx, m, ctx.TenantID, props, keys); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(
		`INSERT INTO "of_object_meta" (engine_id, tenant_id, object_type, physical_key, version, created_at, updated_at) VALUES (?, ?, ?, ?, 1, ?, ?)`,
		engineID, ctx.TenantID, m.Name, pk, now, now,
	); err != nil {
		return nil, sqlitedialect.Classify(err)
	}
	obj, err := p.loadObject(tx, m, ctx.TenantID, engineID)
	if err != nil {
		return nil, err
	}
	if err := writeObjectHistory(tx, engineID, ctx.TenantID, 1, obj, now); err != nil {
		return nil, err
	}
	return obj, nil
}

func withTx[T any](p *Provider, fn func(DBTX) (T, error)) (T, error) {
	var zero T
	tx, conn, err := p.begin()
	if err != nil {
		return zero, err
	}
	defer func() { _ = conn.Close() }()
	defer func() { _ = tx.Rollback() }()
	v, err := fn(tx)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, err
	}
	return v, nil
}

func (p *Provider) GetObject(ctx spi.RequestContext, typ, id string) (spi.OntologyObject, error) {
	act, err := p.pin(ctx)
	if err != nil {
		return nil, err
	}
	m, err := act.model(typ)
	if err != nil {
		return nil, spi.ErrObjectNotFound
	}
	return p.loadObject(p.db, m, ctx.TenantID, id)
}

func (p *Provider) UpdateObject(ctx spi.RequestContext, typ, id string, properties map[string]any, expectedVersion *int) (spi.OntologyObject, error) {
	act, err := p.pin(ctx)
	if err != nil {
		return nil, err
	}
	return withTx(p, func(tx DBTX) (spi.OntologyObject, error) {
		return p.updateObjectTx(tx, act, ctx, typ, id, properties, expectedVersion)
	})
}

func (p *Provider) updateObjectTx(tx DBTX, act *activation, ctx spi.RequestContext, typ, id string, properties map[string]any, expectedVersion *int) (spi.OntologyObject, error) {
	m, err := act.model(typ)
	if err != nil {
		return nil, spi.ErrObjectNotFound
	}
	if !m.Writable() {
		return nil, spi.ErrReadOnlyMapping
	}
	props := copyUserProps(properties)
	meta, err := p.loadMeta(tx, m, ctx.TenantID, id)
	if err != nil {
		return nil, err
	}
	if meta == nil || meta.DeletedAt.Valid {
		return nil, spi.ErrObjectNotFound
	}
	now := nowRFC3339()
	var res sql.Result
	if expectedVersion != nil {
		res, err = tx.Exec(
			`UPDATE "of_object_meta" SET version = version + 1, updated_at = ? WHERE engine_id = ? AND tenant_id = ? AND version = ? AND deleted_at IS NULL`,
			now, meta.EngineID, ctx.TenantID, *expectedVersion,
		)
	} else {
		res, err = tx.Exec(
			`UPDATE "of_object_meta" SET version = version + 1, updated_at = ? WHERE engine_id = ? AND tenant_id = ? AND deleted_at IS NULL`,
			now, meta.EngineID, ctx.TenantID,
		)
	}
	if err != nil {
		return nil, sqlitedialect.Classify(err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		cur, rerr := p.loadMeta(tx, m, ctx.TenantID, meta.EngineID)
		if rerr != nil {
			return nil, rerr
		}
		if cur == nil || cur.DeletedAt.Valid {
			return nil, spi.ErrObjectNotFound
		}
		return nil, spi.ErrVersionConflict
	}
	keys, err := decodePhysicalKey(m, meta.Key)
	if err != nil {
		return nil, spi.ErrObjectNotFound
	}
	if err := p.updateBusiness(tx, m, ctx.TenantID, keys, props); err != nil {
		return nil, err
	}
	obj, err := p.loadObject(tx, m, ctx.TenantID, meta.EngineID)
	if err != nil {
		return nil, err
	}
	ver, _ := obj[spi.FieldVersion].(int)
	if err := writeObjectHistory(tx, meta.EngineID, ctx.TenantID, ver, obj, now); err != nil {
		return nil, err
	}
	return obj, nil
}

func (p *Provider) DeleteObject(ctx spi.RequestContext, typ, id, mode string) error {
	act, err := p.pin(ctx)
	if err != nil {
		return err
	}
	_, err = withTx(p, func(tx DBTX) (struct{}, error) {
		return struct{}{}, p.deleteObjectTx(tx, act, ctx, typ, id, mode)
	})
	return err
}

func (p *Provider) deleteObjectTx(tx DBTX, act *activation, ctx spi.RequestContext, typ, id, mode string) error {
	m, err := act.model(typ)
	if err != nil {
		return nil
	}
	if !m.Writable() {
		return spi.ErrReadOnlyMapping
	}
	meta, err := p.loadMeta(tx, m, ctx.TenantID, id)
	if err != nil {
		return err
	}
	if meta == nil {
		return nil
	}
	if mode != "hard" {
		if meta.DeletedAt.Valid {
			return nil
		}
		now := nowRFC3339()
		if _, err := tx.Exec(
			`UPDATE "of_object_meta" SET deleted_at = ?, updated_at = ?, version = version + 1 WHERE engine_id = ? AND tenant_id = ? AND deleted_at IS NULL`,
			now, now, meta.EngineID, ctx.TenantID,
		); err != nil {
			return sqlitedialect.Classify(err)
		}
		obj, err := p.loadObject(tx, m, ctx.TenantID, meta.EngineID)
		if err != nil {
			return err
		}
		ver, _ := obj[spi.FieldVersion].(int)
		return writeObjectHistory(tx, meta.EngineID, ctx.TenantID, ver, obj, now)
	}
	keys, err := decodePhysicalKey(m, meta.Key)
	if err != nil {
		return spi.ErrObjectNotFound
	}
	if _, err := tx.Exec(
		`DELETE FROM "of_link_meta" WHERE tenant_id = ? AND (from_id = ? OR to_id = ?)`,
		ctx.TenantID, meta.EngineID, meta.EngineID,
	); err != nil {
		return sqlitedialect.Classify(err)
	}
	del, args, err := obda.PlanDeleteObject(m.Binding(), ctx.TenantID, keys)
	if err != nil {
		return err
	}
	stmt, err := p.dialect.Render(del)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(stmt.SQL, args...); err != nil {
		return sqlitedialect.Classify(err)
	}
	if _, err := tx.Exec(`DELETE FROM "of_object_meta" WHERE engine_id = ? AND tenant_id = ?`, meta.EngineID, ctx.TenantID); err != nil {
		return sqlitedialect.Classify(err)
	}
	return nil
}

func (p *Provider) insertBusiness(tx DBTX, m *obda.CompiledModel, tenant string, props map[string]any, keys []any) error {
	cols := make([]string, 0, len(m.Fields)+2)
	vals := make([]any, 0, len(m.Fields)+2)
	seen := map[string]struct{}{}
	for i, col := range m.IdentityColumns {
		cols = append(cols, col)
		vals = append(vals, keys[i])
		seen[col] = struct{}{}
	}
	if m.TenantColumn != "" {
		cols = append(cols, m.TenantColumn)
		vals = append(vals, tenant)
		seen[m.TenantColumn] = struct{}{}
	}
	for _, f := range m.Fields {
		if _, ok := seen[f.Column]; ok {
			continue
		}
		v, ok := props[f.Logical]
		if !ok {
			continue
		}
		cols = append(cols, f.Column)
		vals = append(vals, writeValue(m.PropertyTypes[f.Logical], v))
		seen[f.Column] = struct{}{}
	}
	ins, args, err := obda.PlanCreateObject(m.Binding(), cols, vals)
	if err != nil {
		return err
	}
	stmt, err := p.dialect.Render(ins)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(stmt.SQL, args...); err != nil {
		return sqlitedialect.Classify(err)
	}
	return nil
}

func (p *Provider) updateBusiness(tx DBTX, m *obda.CompiledModel, tenant string, keys []any, props map[string]any) error {
	cols := make([]string, 0, len(props))
	vals := make([]any, 0, len(props))
	idCols := map[string]struct{}{}
	for _, c := range m.IdentityColumns {
		idCols[c] = struct{}{}
	}
	if m.TenantColumn != "" {
		idCols[m.TenantColumn] = struct{}{}
	}
	for _, f := range m.Fields {
		if _, skip := idCols[f.Column]; skip {
			continue
		}
		v, ok := props[f.Logical]
		if !ok {
			continue
		}
		cols = append(cols, f.Column)
		vals = append(vals, writeValue(m.PropertyTypes[f.Logical], v))
	}
	if len(cols) == 0 {
		return nil
	}
	upd, args, err := obda.PlanUpdateObject(m.Binding(), tenant, keys, cols, vals)
	if err != nil {
		return err
	}
	stmt, err := p.dialect.Render(upd)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(stmt.SQL, args...); err != nil {
		return sqlitedialect.Classify(err)
	}
	return nil
}

func (p *Provider) loadObject(tx DBTX, m *obda.CompiledModel, tenant, id string) (spi.OntologyObject, error) {
	meta, err := p.loadMeta(tx, m, tenant, id)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, spi.ErrObjectNotFound
	}
	keys, err := decodePhysicalKey(m, meta.Key)
	if err != nil {
		return nil, spi.ErrObjectNotFound
	}
	biz, err := p.loadBusiness(tx, m, tenant, keys)
	if err != nil {
		return nil, err
	}
	if biz == nil {
		return nil, spi.ErrObjectNotFound
	}
	return p.assemble(m, meta, biz)
}

func (p *Provider) loadMeta(tx DBTX, m *obda.CompiledModel, tenant, id string) (*metaRow, error) {
	row, err := scanMeta(tx,
		`SELECT engine_id, tenant_id, object_type, physical_key, version, created_at, updated_at, deleted_at FROM "of_object_meta" WHERE engine_id = ? AND tenant_id = ? AND object_type = ?`,
		id, tenant, m.Name,
	)
	if err != nil {
		return nil, err
	}
	if row != nil {
		return row, nil
	}
	if m.IdentityStrategy != "direct" {
		return nil, nil
	}
	typ, keys, derr := obda.DecodeDirect(id)
	if derr != nil || typ != m.Name {
		return nil, nil
	}
	pk := obda.EncodePhysicalKey(stringsToAny(keys))
	return scanMeta(tx,
		`SELECT engine_id, tenant_id, object_type, physical_key, version, created_at, updated_at, deleted_at FROM "of_object_meta" WHERE tenant_id = ? AND object_type = ? AND physical_key = ?`,
		tenant, m.Name, pk,
	)
}

func scanMeta(tx DBTX, q string, args ...any) (*metaRow, error) {
	row := &metaRow{}
	var ver int64
	err := tx.QueryRow(q, args...).Scan(&row.EngineID, &row.TenantID, &row.Type, &row.Key, &ver, &row.CreatedAt, &row.UpdatedAt, &row.DeletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row.Version = int(ver)
	return row, nil
}

func (p *Provider) loadBusiness(tx DBTX, m *obda.CompiledModel, tenant string, keys []any) (map[string]any, error) {
	b := m.Binding()
	if b.TenantColumn == "" {
		return nil, spi.ErrInvalidMapping
	}
	sel, args, err := obda.PlanGetObject(b, tenant, keys)
	if err != nil {
		return nil, err
	}
	stmt, err := p.dialect.Render(sel)
	if err != nil {
		return nil, err
	}
	dest := make([]any, len(b.SelectColumns))
	ptrs := make([]any, len(dest))
	for i := range dest {
		ptrs[i] = &dest[i]
	}
	if err := tx.QueryRow(stmt.SQL, args...).Scan(ptrs...); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, sqlitedialect.Classify(err)
	}
	out := map[string]any{}
	for i, col := range b.SelectColumns {
		out[col] = unwrap(dest[i])
	}
	return out, nil
}

func (p *Provider) assemble(m *obda.CompiledModel, meta *metaRow, biz map[string]any) (spi.OntologyObject, error) {
	obj := spi.OntologyObject{
		spi.FieldID:        meta.EngineID,
		spi.FieldType:      m.Name,
		spi.FieldTenantID:  meta.TenantID,
		spi.FieldVersion:   meta.Version,
		spi.FieldCreatedAt: meta.CreatedAt,
		spi.FieldUpdatedAt: meta.UpdatedAt,
	}
	if meta.DeletedAt.Valid {
		obj[spi.FieldDeletedAt] = meta.DeletedAt.String
	}
	for _, f := range m.Fields {
		raw := biz[f.Column]
		v, err := p.dialect.NormalizeValue(m.PropertyTypes[f.Logical], raw)
		if err != nil {
			return nil, err
		}
		obj[f.Logical] = v
	}
	return obj, nil
}

func writeObjectHistory(tx DBTX, engineID, tenant string, version int, obj spi.OntologyObject, at string) error {
	snap, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`INSERT INTO "of_object_history" (engine_id, version, tenant_id, snapshot, updated_at) VALUES (?, ?, ?, ?, ?)`,
		engineID, version, tenant, string(snap), at,
	)
	return err
}

func identityFromProps(m *obda.CompiledModel, props map[string]any) ([]any, error) {
	out := make([]any, len(m.IdentityColumns))
	for i, col := range m.IdentityColumns {
		f, ok := m.FieldByColumn[col]
		if !ok {
			return nil, spi.ErrInvalidMapping
		}
		v, ok := props[f.Logical]
		if !ok || v == nil {
			return nil, fmt.Errorf("%w: missing identity field", spi.ErrInvalidMapping)
		}
		out[i] = v
	}
	return out, nil
}

func decodePhysicalKey(m *obda.CompiledModel, pk string) ([]any, error) {
	if len(m.IdentityColumns) == 1 {
		return []any{pk}, nil
	}
	var keys []string
	if err := json.Unmarshal([]byte(pk), &keys); err != nil {
		return nil, err
	}
	return stringsToAny(keys), nil
}

func copyUserProps(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		if spi.IsSystemField(k) {
			continue
		}
		out[k] = v
	}
	return out
}

func writeValue(odlType string, v any) any {
	switch strings.ToLower(odlType) {
	case "boolean", "bool":
		switch t := v.(type) {
		case bool:
			if t {
				return 1
			}
			return 0
		case int:
			if t != 0 {
				return 1
			}
			return 0
		case int64:
			if t != 0 {
				return 1
			}
			return 0
		}
	}
	return v
}

func unwrap(v any) any {
	switch t := v.(type) {
	case []byte:
		return string(t)
	case sql.NullString:
		if !t.Valid {
			return nil
		}
		return t.String
	case sql.NullInt64:
		if !t.Valid {
			return nil
		}
		return t.Int64
	default:
		return v
	}
}

func stringify(vals []any) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = fmt.Sprint(v)
	}
	return out
}

func stringsToAny(keys []string) []any {
	out := make([]any, len(keys))
	for i, k := range keys {
		out[i] = k
	}
	return out
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
