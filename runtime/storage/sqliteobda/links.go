package sqliteobda

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/openfoundry/runtime/internal/uuidv7"
	"github.com/openfoundry/runtime/obda"
	sqlitedialect "github.com/openfoundry/runtime/obda/dialect/sqlite"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

type linkMeta struct {
	EngineID  string
	TenantID  string
	Type      string
	FromID    string
	ToID      string
	Version   int
	CreatedAt string
	UpdatedAt string
	DeletedAt sql.NullString
}

func (a *activation) link(typ string) (*obda.CompiledLink, error) {
	l, ok := a.compiled.Links[typ]
	if !ok {
		return nil, spi.ErrLinkNotFound
	}
	return l, nil
}

func (p *Provider) CreateLink(ctx spi.RequestContext, typ, fromID, toID string, properties map[string]any) (spi.OntologyLink, error) {
	act, err := p.pin(ctx)
	if err != nil {
		return nil, err
	}
	l, err := act.link(typ)
	if err != nil {
		return nil, err
	}
	if !l.Writable() {
		return nil, spi.ErrReadOnlyMapping
	}
	tx, conn, err := p.begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	defer func() { _ = tx.Rollback() }()
	fromMeta, err := p.requireLiveEndpoint(tx, act, ctx.TenantID, l.FromObject, fromID)
	if err != nil {
		return nil, err
	}
	toMeta, err := p.requireLiveEndpoint(tx, act, ctx.TenantID, l.ToObject, toID)
	if err != nil {
		return nil, err
	}
	props := copyUserProps(properties)
	admID := uuidv7.New()
	if len(l.IdentityColumns) > 0 {
		if f, ok := l.FieldByColumn[l.IdentityColumns[0]]; ok {
			if v, ok := props[f.Logical]; ok && v != nil {
				admID = fmt.Sprint(v)
			} else {
				props[f.Logical] = admID
			}
		}
	}
	engineID := uuidv7.New()
	if l.IdentityStrategy == "sidecar" {
		if v, ok := properties[spi.LinkFieldEngineLinkID].(string); ok && v != "" {
			engineID = v
		}
	} else {
		engineID = obda.EncodeDirect(l.Name, []string{admID})
	}
	now := nowRFC3339()
	if err := p.insertLinkBusiness(tx, l, ctx.TenantID, admID, fromMeta.Key, toMeta.Key); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(
		`INSERT INTO "of_link_meta" (engine_id, tenant_id, link_type, from_id, to_id, version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		engineID, ctx.TenantID, l.Name, fromMeta.EngineID, toMeta.EngineID, now, now,
	); err != nil {
		return nil, sqlitedialect.Classify(err)
	}
	link, err := p.loadLink(tx, l, ctx.TenantID, engineID)
	if err != nil {
		return nil, err
	}
	if err := writeLinkHistory(tx, engineID, ctx.TenantID, 1, link, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return link, nil
}

func (p *Provider) GetLink(ctx spi.RequestContext, typ, linkID string) (spi.OntologyLink, error) {
	act, err := p.pin(ctx)
	if err != nil {
		return nil, err
	}
	l, err := act.link(typ)
	if err != nil {
		return nil, spi.ErrLinkNotFound
	}
	return p.loadLink(p.db, l, ctx.TenantID, linkID)
}

func (p *Provider) UpdateLink(ctx spi.RequestContext, typ, linkID string, properties map[string]any, expectedVersion *int) (spi.OntologyLink, error) {
	act, err := p.pin(ctx)
	if err != nil {
		return nil, err
	}
	l, err := act.link(typ)
	if err != nil {
		return nil, spi.ErrLinkNotFound
	}
	if !l.Writable() {
		return nil, spi.ErrReadOnlyMapping
	}
	tx, conn, err := p.begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	defer func() { _ = tx.Rollback() }()
	meta, err := p.loadLinkMeta(tx, l, ctx.TenantID, linkID)
	if err != nil {
		return nil, err
	}
	if meta == nil || meta.DeletedAt.Valid {
		return nil, spi.ErrLinkNotFound
	}
	now := nowRFC3339()
	var res sql.Result
	if expectedVersion != nil {
		res, err = tx.Exec(
			`UPDATE "of_link_meta" SET version = version + 1, updated_at = ? WHERE engine_id = ? AND tenant_id = ? AND version = ? AND deleted_at IS NULL`,
			now, meta.EngineID, ctx.TenantID, *expectedVersion,
		)
	} else {
		res, err = tx.Exec(
			`UPDATE "of_link_meta" SET version = version + 1, updated_at = ? WHERE engine_id = ? AND tenant_id = ? AND deleted_at IS NULL`,
			now, meta.EngineID, ctx.TenantID,
		)
	}
	if err != nil {
		return nil, sqlitedialect.Classify(err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		cur, rerr := p.loadLinkMeta(tx, l, ctx.TenantID, meta.EngineID)
		if rerr != nil {
			return nil, rerr
		}
		if cur == nil || cur.DeletedAt.Valid {
			return nil, spi.ErrLinkNotFound
		}
		return nil, spi.ErrVersionConflict
	}
	link, err := p.loadLink(tx, l, ctx.TenantID, meta.EngineID)
	if err != nil {
		return nil, err
	}
	ver, _ := link[spi.FieldVersion].(int)
	if err := writeLinkHistory(tx, meta.EngineID, ctx.TenantID, ver, link, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return link, nil
}

func (p *Provider) DeleteLink(ctx spi.RequestContext, typ, linkID string) error {
	act, err := p.pin(ctx)
	if err != nil {
		return err
	}
	l, err := act.link(typ)
	if err != nil {
		return nil
	}
	if !l.Writable() {
		return spi.ErrReadOnlyMapping
	}
	tx, conn, err := p.begin()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	defer func() { _ = tx.Rollback() }()
	meta, err := p.loadLinkMeta(tx, l, ctx.TenantID, linkID)
	if err != nil {
		return err
	}
	if meta == nil || meta.DeletedAt.Valid {
		return tx.Commit()
	}
	now := nowRFC3339()
	if _, err := tx.Exec(
		`UPDATE "of_link_meta" SET deleted_at = ?, updated_at = ?, version = version + 1 WHERE engine_id = ? AND tenant_id = ? AND deleted_at IS NULL`,
		now, now, meta.EngineID, ctx.TenantID,
	); err != nil {
		return sqlitedialect.Classify(err)
	}
	return tx.Commit()
}

func (p *Provider) GetLinks(ctx spi.RequestContext, objectID, linkType, direction string, options *spi.QueryOptions) (spi.LinkPage, error) {
	act, err := p.pin(ctx)
	if err != nil {
		return spi.LinkPage{}, err
	}
	l, err := act.link(linkType)
	if err != nil {
		return spi.LinkPage{}, spi.ErrLinkNotFound
	}
	endCol := "from_id"
	if direction == "inbound" {
		endCol = "to_id"
	}
	sel, args, err := obda.PlanGetLinks("of_link_meta", "tenant_id", endCol, ctx.TenantID, objectID)
	if err != nil {
		return spi.LinkPage{}, err
	}
	sel.Columns = []sqlast.Expr{
		sqlast.Identifier{Name: "engine_id"},
		sqlast.Identifier{Name: "tenant_id"},
		sqlast.Identifier{Name: "link_type"},
		sqlast.Identifier{Name: "from_id"},
		sqlast.Identifier{Name: "to_id"},
		sqlast.Identifier{Name: "version"},
		sqlast.Identifier{Name: "created_at"},
		sqlast.Identifier{Name: "updated_at"},
		sqlast.Identifier{Name: "deleted_at"},
	}
	sel.Where = andPred(sel.Where, &sqlast.Predicate{
		Op:    "eq",
		Field: &sqlast.Identifier{Name: "link_type"},
		Value: sqlast.Param{Position: 3},
	})
	args = append(args, linkType)
	if options == nil || !options.IncludeDeleted {
		sel.Where = andPred(sel.Where, &sqlast.Predicate{
			Op:    "is_null",
			Field: &sqlast.Identifier{Name: "deleted_at"},
		})
	}
	sel.Order = []sqlast.Order{{Field: sqlast.Identifier{Name: "engine_id"}}}
	limit := 100
	offset := 0
	if options != nil {
		if options.Limit > 0 {
			limit = options.Limit
		}
		if limit > 1000 {
			limit = 1000
		}
		if options.Offset > 0 {
			offset = options.Offset
		}
	}
	countSel := *sel
	countStmt, err := p.dialect.Render(&countSel)
	if err != nil {
		return spi.LinkPage{}, err
	}
	var total int
	if err := p.db.QueryRow("SELECT COUNT(*) FROM ("+countStmt.SQL+") AS q", args...).Scan(&total); err != nil {
		return spi.LinkPage{}, sqlitedialect.Classify(err)
	}
	sel.Limit = &sqlast.LimitOffset{}
	stmt, err := p.dialect.Render(sel)
	if err != nil {
		return spi.LinkPage{}, err
	}
	rows, err := p.db.Query(stmt.SQL, append(append([]any{}, args...), limit+1, offset)...)
	if err != nil {
		return spi.LinkPage{}, sqlitedialect.Classify(err)
	}
	defer rows.Close()
	var items []spi.OntologyLink
	for rows.Next() {
		meta, err := scanLinkMetaRow(rows)
		if err != nil {
			return spi.LinkPage{}, err
		}
		link, err := p.assembleLink(l, meta)
		if err != nil {
			return spi.LinkPage{}, err
		}
		items = append(items, link)
	}
	if err := rows.Err(); err != nil {
		return spi.LinkPage{}, err
	}
	hasNext := len(items) > limit
	if hasNext {
		items = items[:limit]
	}
	return spi.LinkPage{Items: items, TotalCount: total, HasNextPage: hasNext}, nil
}

func (p *Provider) Traverse(ctx spi.RequestContext, startID string, path spi.TraversalPath, options *spi.TraversalOptions) (spi.TraversalResult, error) {
	act, err := p.pin(ctx)
	if err != nil {
		return spi.TraversalResult{}, err
	}
	if len(path.Steps) == 0 {
		return spi.TraversalResult{}, nil
	}
	if len(path.Steps) > 8 {
		return spi.TraversalResult{}, spi.ErrUnsupportedCapability
	}
	if _, err := p.lookupAnyObject(p.db, act, ctx.TenantID, startID); err != nil {
		return spi.TraversalResult{}, spi.ErrObjectNotFound
	}
	frontier := []string{startID}
	seen := map[string]struct{}{startID: {}}
	var edges []spi.OntologyLink
	var visited []spi.OntologyObject
	var nodes []spi.OntologyObject
	for i, step := range path.Steps {
		dir := step.Direction
		if dir == "" {
			dir = "outbound"
		}
		var next []string
		for _, id := range frontier {
			page, err := p.GetLinks(ctx, id, step.LinkType, dir, &spi.QueryOptions{Limit: 1000})
			if err != nil {
				return spi.TraversalResult{}, err
			}
			for _, e := range page.Items {
				edges = append(edges, e)
				other, _ := e[spi.LinkFieldToID].(string)
				if dir == "inbound" {
					other, _ = e[spi.LinkFieldFromID].(string)
				}
				if other == "" {
					continue
				}
				if _, ok := seen[other]; ok {
					continue
				}
				seen[other] = struct{}{}
				obj, err := p.lookupAnyObject(p.db, act, ctx.TenantID, other)
				if err != nil {
					continue
				}
				next = append(next, other)
				if i == len(path.Steps)-1 {
					nodes = append(nodes, obj)
				} else {
					visited = append(visited, obj)
				}
			}
		}
		frontier = next
	}
	_ = options
	return spi.TraversalResult{Nodes: nodes, Edges: edges, Visited: visited, TotalCount: len(nodes)}, nil
}

func (p *Provider) requireLiveEndpoint(tx DBTX, act *activation, tenant, typ, id string) (*metaRow, error) {
	m, err := act.model(typ)
	if err != nil {
		return nil, spi.ErrObjectNotFound
	}
	meta, err := p.loadMeta(tx, m, tenant, id)
	if err != nil {
		return nil, err
	}
	if meta == nil || meta.DeletedAt.Valid {
		return nil, spi.ErrObjectNotFound
	}
	keys, err := decodePhysicalKey(m, meta.Key)
	if err != nil {
		return nil, spi.ErrObjectNotFound
	}
	biz, err := p.loadBusiness(tx, m, tenant, keys)
	if err != nil || biz == nil {
		return nil, spi.ErrObjectNotFound
	}
	return meta, nil
}

func (p *Provider) lookupAnyObject(tx DBTX, act *activation, tenant, id string) (spi.OntologyObject, error) {
	for _, m := range act.compiled.Models {
		obj, err := p.loadObject(tx, m, tenant, id)
		if err == nil {
			return obj, nil
		}
		if err != spi.ErrObjectNotFound {
			return nil, err
		}
	}
	return nil, spi.ErrObjectNotFound
}

func (p *Provider) insertLinkBusiness(tx DBTX, l *obda.CompiledLink, tenant, ident, fromKey, toKey string) error {
	cols := append([]string(nil), l.IdentityColumns...)
	vals := []any{ident}
	cols = append(cols, l.FromColumns...)
	vals = append(vals, fromKey)
	cols = append(cols, l.ToColumns...)
	vals = append(vals, toKey)
	if l.TenantColumn != "" {
		cols = append(cols, l.TenantColumn)
		vals = append(vals, tenant)
	}
	b := obda.ObjectBinding{Table: l.Table, TenantColumn: l.TenantColumn, IdentityColumns: l.IdentityColumns, Writable: true}
	ins, args, err := obda.PlanCreateObject(b, cols, vals)
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

func (p *Provider) loadLink(tx DBTX, l *obda.CompiledLink, tenant, id string) (spi.OntologyLink, error) {
	meta, err := p.loadLinkMeta(tx, l, tenant, id)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, spi.ErrLinkNotFound
	}
	return p.assembleLink(l, meta)
}

func (p *Provider) loadLinkMeta(tx DBTX, l *obda.CompiledLink, tenant, id string) (*linkMeta, error) {
	row := &linkMeta{}
	var ver int64
	err := tx.QueryRow(
		`SELECT engine_id, tenant_id, link_type, from_id, to_id, version, created_at, updated_at, deleted_at FROM "of_link_meta" WHERE engine_id = ? AND tenant_id = ? AND link_type = ?`,
		id, tenant, l.Name,
	).Scan(&row.EngineID, &row.TenantID, &row.Type, &row.FromID, &row.ToID, &ver, &row.CreatedAt, &row.UpdatedAt, &row.DeletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row.Version = int(ver)
	return row, nil
}

func scanLinkMetaRow(rows *sql.Rows) (*linkMeta, error) {
	row := &linkMeta{}
	var ver int64
	if err := rows.Scan(&row.EngineID, &row.TenantID, &row.Type, &row.FromID, &row.ToID, &ver, &row.CreatedAt, &row.UpdatedAt, &row.DeletedAt); err != nil {
		return nil, err
	}
	row.Version = int(ver)
	return row, nil
}

func (p *Provider) assembleLink(l *obda.CompiledLink, meta *linkMeta) (spi.OntologyLink, error) {
	link := spi.OntologyLink{
		spi.FieldID:           meta.EngineID,
		spi.FieldType:         l.Name,
		spi.FieldTenantID:     meta.TenantID,
		spi.FieldVersion:      meta.Version,
		spi.FieldCreatedAt:    meta.CreatedAt,
		spi.FieldUpdatedAt:    meta.UpdatedAt,
		spi.LinkFieldFromID:   meta.FromID,
		spi.LinkFieldToID:     meta.ToID,
		spi.LinkFieldFromType: l.FromObject,
		spi.LinkFieldToType:   l.ToObject,
	}
	if meta.DeletedAt.Valid {
		link[spi.FieldDeletedAt] = meta.DeletedAt.String
	}
	return link, nil
}

func writeLinkHistory(tx DBTX, engineID, tenant string, version int, link spi.OntologyLink, at string) error {
	snap, err := json.Marshal(link)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`INSERT INTO "of_link_history" (engine_id, version, tenant_id, snapshot, updated_at) VALUES (?, ?, ?, ?, ?)`,
		engineID, version, tenant, string(snap), at,
	)
	return err
}
