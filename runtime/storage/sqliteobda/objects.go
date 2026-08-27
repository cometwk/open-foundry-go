package sqliteobda

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openfoundry/runtime/internal/uuidv7"
	"github.com/openfoundry/runtime/obda"
	sqlitedialect "github.com/openfoundry/runtime/obda/dialect/sqlite"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

type metaRow struct {
	EngineID  string
	TenantID  string
	Type      string
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
	id, err := objectIdentity(m, props)
	if err != nil {
		return nil, err
	}
	now := nowRFC3339()
	if err := p.insertBusiness(tx, m, ctx.TenantID, props, id, now); err != nil {
		if errors.Is(err, spi.ErrCardinalityViolation) {
			return nil, fmt.Errorf("%w: identity exists", spi.ErrInvalidMapping)
		}
		return nil, err
	}
	return p.loadObject(tx, m, ctx.TenantID, id)
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
	if m.Omit.Version && expectedVersion != nil {
		return nil, spi.ErrUnsupportedCapability
	}
	obj, err := p.loadObject(tx, m, ctx.TenantID, id)
	if err != nil {
		return nil, err
	}
	if _, del := obj[spi.FieldDeletedAt]; del {
		return nil, spi.ErrObjectNotFound
	}
	props := copyUserProps(properties)
	now := nowRFC3339()
	cols := make([]string, 0, len(m.Fields)+2)
	vals := make([]any, 0, len(m.Fields)+2)
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
	if !m.Omit.UpdatedAt {
		cols = append(cols, "updated_at")
		vals = append(vals, now)
	}
	curVer := asInt(obj[spi.FieldVersion])
	if !m.Omit.Version {
		cols = append(cols, "version")
		vals = append(vals, curVer+1)
	}
	if len(cols) == 0 {
		return obj, nil
	}
	upd, args, err := obda.PlanUpdateObject(m.Binding(), ctx.TenantID, []any{id}, cols, vals)
	if err != nil {
		return nil, err
	}
	if expectedVersion != nil {
		upd.Where = andPred(upd.Where, &sqlast.Predicate{
			Op:    "eq",
			Field: &sqlast.Identifier{Name: "version"},
		})
		args = append(args, *expectedVersion)
	}
	if !m.Omit.DeletedAt {
		upd.Where = andPred(upd.Where, &sqlast.Predicate{
			Op:    "is_null",
			Field: &sqlast.Identifier{Name: "deleted_at"},
		})
	}
	stmt, err := p.dialect.Render(upd)
	if err != nil {
		return nil, err
	}
	res, err := tx.Exec(stmt.SQL, args...)
	if err != nil {
		return nil, sqlitedialect.Classify(err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		cur, rerr := p.loadObject(tx, m, ctx.TenantID, id)
		if rerr != nil {
			return nil, spi.ErrObjectNotFound
		}
		if _, del := cur[spi.FieldDeletedAt]; del {
			return nil, spi.ErrObjectNotFound
		}
		return nil, spi.ErrVersionConflict
	}
	return p.loadObject(tx, m, ctx.TenantID, id)
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
	obj, err := p.loadObject(tx, m, ctx.TenantID, id)
	if err != nil {
		if errors.Is(err, spi.ErrObjectNotFound) {
			return nil
		}
		return err
	}
	if mode != "hard" {
		if m.Omit.DeletedAt {
			return spi.ErrUnsupportedCapability
		}
		if _, del := obj[spi.FieldDeletedAt]; del {
			return nil
		}
		now := nowRFC3339()
		cols := []string{"deleted_at"}
		vals := []any{now}
		if !m.Omit.UpdatedAt {
			cols = append(cols, "updated_at")
			vals = append(vals, now)
		}
		if !m.Omit.Version {
			cols = append(cols, "version")
			vals = append(vals, asInt(obj[spi.FieldVersion])+1)
		}
		upd, args, err := obda.PlanUpdateObject(m.Binding(), ctx.TenantID, []any{id}, cols, vals)
		if err != nil {
			return err
		}
		upd.Where = andPred(upd.Where, &sqlast.Predicate{
			Op:    "is_null",
			Field: &sqlast.Identifier{Name: "deleted_at"},
		})
		stmt, err := p.dialect.Render(upd)
		if err != nil {
			return err
		}
		_, err = tx.Exec(stmt.SQL, args...)
		return sqlitedialect.Classify(err)
	}
	if err := p.deleteLinksForObject(tx, act, ctx.TenantID, id); err != nil {
		return err
	}
	del, args, err := obda.PlanDeleteObject(m.Binding(), ctx.TenantID, []any{id})
	if err != nil {
		return err
	}
	stmt, err := p.dialect.Render(del)
	if err != nil {
		return err
	}
	_, err = tx.Exec(stmt.SQL, args...)
	return sqlitedialect.Classify(err)
}

func (p *Provider) deleteLinksForObject(tx DBTX, act *activation, tenant, objectID string) error {
	names := make([]string, 0, len(act.compiled.Links))
	for name := range act.compiled.Links {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		l := act.compiled.Links[name]
		tbl, err := p.dialect.QuoteIdentifier(sqlast.Identifier{Name: l.Table})
		if err != nil {
			return err
		}
		tenantCol, err := p.dialect.QuoteIdentifier(sqlast.Identifier{Name: l.TenantColumn})
		if err != nil {
			return err
		}
		fromCol, err := p.dialect.QuoteIdentifier(sqlast.Identifier{Name: firstCol(l.FromColumns)})
		if err != nil {
			return err
		}
		toCol, err := p.dialect.QuoteIdentifier(sqlast.Identifier{Name: firstCol(l.ToColumns)})
		if err != nil {
			return err
		}
		q := "DELETE FROM " + tbl + " WHERE " + tenantCol + " = ? AND (" + fromCol + " = ? OR " + toCol + " = ?)"
		if _, err := tx.Exec(q, tenant, objectID, objectID); err != nil {
			return sqlitedialect.Classify(err)
		}
	}
	return nil
}

func (p *Provider) insertBusiness(tx DBTX, m *obda.CompiledModel, tenant string, props map[string]any, id, now string) error {
	cols := make([]string, 0, len(m.Fields)+6)
	vals := make([]any, 0, len(m.Fields)+6)
	seen := map[string]struct{}{}
	for _, col := range m.IdentityColumns {
		cols = append(cols, col)
		vals = append(vals, id)
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
	if !m.Omit.Version {
		cols = append(cols, "version")
		vals = append(vals, 1)
	}
	if !m.Omit.CreatedAt {
		cols = append(cols, "created_at")
		vals = append(vals, now)
	}
	if !m.Omit.UpdatedAt {
		cols = append(cols, "updated_at")
		vals = append(vals, now)
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

func (p *Provider) loadObject(tx DBTX, m *obda.CompiledModel, tenant, id string) (spi.OntologyObject, error) {
	if err := matchDirectID(m.Name, id); err != nil {
		return nil, err
	}
	biz, err := p.loadBusiness(tx, m, tenant, []any{id})
	if err != nil {
		return nil, err
	}
	if biz == nil {
		return nil, spi.ErrObjectNotFound
	}
	return p.assemble(m, tenant, biz)
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

func (p *Provider) assemble(m *obda.CompiledModel, tenant string, biz map[string]any) (spi.OntologyObject, error) {
	id := ""
	if len(m.IdentityColumns) > 0 {
		id = fmt.Sprint(biz[m.IdentityColumns[0]])
	}
	obj := spi.OntologyObject{
		spi.FieldID:       id,
		spi.FieldType:     m.Name,
		spi.FieldTenantID: tenant,
	}
	if m.Omit.Version {
		obj[spi.FieldVersion] = 0
	} else {
		obj[spi.FieldVersion] = asInt(biz["version"])
	}
	if !m.Omit.CreatedAt {
		if v := biz["created_at"]; v != nil {
			obj[spi.FieldCreatedAt] = fmt.Sprint(v)
		}
	}
	if !m.Omit.UpdatedAt {
		if v := biz["updated_at"]; v != nil {
			obj[spi.FieldUpdatedAt] = fmt.Sprint(v)
		}
	}
	if !m.Omit.DeletedAt {
		if v := biz["deleted_at"]; v != nil && fmt.Sprint(v) != "" {
			obj[spi.FieldDeletedAt] = fmt.Sprint(v)
		}
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

func objectIdentity(m *obda.CompiledModel, props map[string]any) (string, error) {
	if m.IdentityInsert == "generated" {
		for _, col := range m.IdentityColumns {
			if f, ok := m.FieldByColumn[col]; ok {
				if _, exists := props[f.Logical]; exists {
					return "", fmt.Errorf("%w: generated identity cannot be supplied", spi.ErrInvalidMapping)
				}
			}
		}
		return obda.EncodeDirect(m.Name, []string{uuidv7.New()}), nil
	}
	keys := make([]string, len(m.IdentityColumns))
	for i, col := range m.IdentityColumns {
		f, ok := m.FieldByColumn[col]
		if !ok {
			return "", spi.ErrInvalidMapping
		}
		v, ok := props[f.Logical]
		if !ok || v == nil {
			return "", fmt.Errorf("%w: missing identity field", spi.ErrInvalidMapping)
		}
		keys[i] = fmt.Sprint(v)
	}
	return obda.EncodeDirect(m.Name, keys), nil
}

func matchDirectID(typ, id string) error {
	got, _, err := obda.DecodeDirect(id)
	if err != nil || got != typ {
		return spi.ErrObjectNotFound
	}
	return nil
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

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
