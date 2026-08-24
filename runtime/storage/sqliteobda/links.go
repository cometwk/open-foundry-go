package sqliteobda

import (
	"database/sql"
	"fmt"

	"github.com/openfoundry/runtime/internal/uuidv7"
	"github.com/openfoundry/runtime/obda"
	sqlitedialect "github.com/openfoundry/runtime/obda/dialect/sqlite"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

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
	return withTx(p, func(tx DBTX) (spi.OntologyLink, error) {
		return p.createLinkTx(tx, act, ctx, typ, fromID, toID, properties)
	})
}

func (p *Provider) createLinkTx(tx DBTX, act *activation, ctx spi.RequestContext, typ, fromID, toID string, properties map[string]any) (spi.OntologyLink, error) {
	l, err := act.link(typ)
	if err != nil {
		return nil, err
	}
	if !l.Writable() {
		return nil, spi.ErrReadOnlyMapping
	}
	fromMeta, err := p.requireLiveEndpoint(tx, act, ctx.TenantID, l.FromObject, fromID)
	if err != nil {
		return nil, err
	}
	toMeta, err := p.requireLiveEndpoint(tx, act, ctx.TenantID, l.ToObject, toID)
	if err != nil {
		return nil, err
	}
	k := uuidv7.New()
	if v, ok := properties[spi.LinkFieldEngineLinkID].(string); ok && v != "" {
		k = v
	}
	id := obda.EncodeDirect(l.Name, []string{k})
	now := nowRFC3339()
	if err := p.insertLinkRow(tx, l, ctx.TenantID, id, fromMeta.EngineID, toMeta.EngineID, copyLinkProps(properties), now); err != nil {
		return nil, err
	}
	return p.loadLink(tx, l, ctx.TenantID, id)
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
	if l.Omit.Version && expectedVersion != nil {
		return nil, spi.ErrUnsupportedCapability
	}
	tx, conn, err := p.begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	defer func() { _ = tx.Rollback() }()
	link, err := p.loadLink(tx, l, ctx.TenantID, linkID)
	if err != nil {
		return nil, err
	}
	if _, del := link[spi.FieldDeletedAt]; del {
		return nil, spi.ErrLinkNotFound
	}
	now := nowRFC3339()
	cols := make([]string, 0, 4)
	vals := make([]any, 0, 4)
	props := copyLinkProps(properties)
	idCols := map[string]struct{}{}
	for _, c := range l.IdentityColumns {
		idCols[c] = struct{}{}
	}
	for _, c := range l.FromColumns {
		idCols[c] = struct{}{}
	}
	for _, c := range l.ToColumns {
		idCols[c] = struct{}{}
	}
	if l.TenantColumn != "" {
		idCols[l.TenantColumn] = struct{}{}
	}
	for _, f := range l.Fields {
		if _, skip := idCols[f.Column]; skip {
			continue
		}
		v, ok := props[f.Logical]
		if !ok {
			continue
		}
		cols = append(cols, f.Column)
		vals = append(vals, writeValue(l.PropertyTypes[f.Logical], v))
	}
	if !l.Omit.UpdatedAt {
		cols = append(cols, "updated_at")
		vals = append(vals, now)
	}
	curVer := asInt(link[spi.FieldVersion])
	if !l.Omit.Version {
		cols = append(cols, "version")
		vals = append(vals, curVer+1)
	}
	if len(cols) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return link, nil
	}
	upd, args, err := obda.PlanUpdateObject(l.Binding(), ctx.TenantID, []any{linkID}, cols, vals)
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
	if !l.Omit.DeletedAt {
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
		cur, rerr := p.loadLink(tx, l, ctx.TenantID, linkID)
		if rerr != nil {
			return nil, spi.ErrLinkNotFound
		}
		if _, del := cur[spi.FieldDeletedAt]; del {
			return nil, spi.ErrLinkNotFound
		}
		return nil, spi.ErrVersionConflict
	}
	out, err := p.loadLink(tx, l, ctx.TenantID, linkID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
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
	link, err := p.loadLink(tx, l, ctx.TenantID, linkID)
	if err != nil {
		if err == spi.ErrLinkNotFound {
			return tx.Commit()
		}
		return err
	}
	if _, del := link[spi.FieldDeletedAt]; del {
		return tx.Commit()
	}
	if l.Omit.DeletedAt {
		return spi.ErrUnsupportedCapability
	}
	now := nowRFC3339()
	cols := []string{"deleted_at"}
	vals := []any{now}
	if !l.Omit.UpdatedAt {
		cols = append(cols, "updated_at")
		vals = append(vals, now)
	}
	if !l.Omit.Version {
		cols = append(cols, "version")
		vals = append(vals, asInt(link[spi.FieldVersion])+1)
	}
	upd, args, err := obda.PlanUpdateObject(l.Binding(), ctx.TenantID, []any{linkID}, cols, vals)
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
	peerName := l.ToObject
	endCol := firstCol(l.FromColumns)
	peerFK := firstCol(l.ToColumns)
	if direction == "inbound" {
		peerName = l.FromObject
		endCol = firstCol(l.ToColumns)
		peerFK = firstCol(l.FromColumns)
	}
	peer, err := act.model(peerName)
	if err != nil {
		return spi.LinkPage{}, spi.ErrObjectNotFound
	}
	includeDeleted := options != nil && options.IncludeDeleted
	sel, args, err := obda.PlanGetLinksJoin(obda.LinkJoinBinding{
		LinkTable:       l.Table,
		LinkTenant:      l.TenantColumn,
		EndpointCol:     endCol,
		PeerFKCol:       peerFK,
		PeerTable:       peer.Table,
		PeerIDCol:       firstCol(peer.IdentityColumns),
		PeerTenantCol:   peer.TenantColumn,
		SelectColumns:   l.Binding().SelectColumns,
		OmitLinkDeleted: l.Omit.DeletedAt || includeDeleted,
		OmitPeerDeleted: peer.Omit.DeletedAt,
	}, ctx.TenantID, objectID)
	if err != nil {
		return spi.LinkPage{}, err
	}
	sel.Order = []sqlast.Order{{Field: sqlast.Identifier{Qualifier: "l", Name: firstCol(l.IdentityColumns)}}}
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
	countSel.Limit = nil
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
	bizCols := l.Binding().SelectColumns
	var items []spi.OntologyLink
	for rows.Next() {
		dest := make([]any, len(bizCols))
		ptrs := make([]any, len(dest))
		for i := range dest {
			ptrs[i] = &dest[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return spi.LinkPage{}, err
		}
		biz := map[string]any{}
		for i, col := range bizCols {
			biz[col] = unwrap(dest[i])
		}
		link, err := p.assembleLink(l, ctx.TenantID, biz)
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
	obj, err := p.loadObject(tx, m, tenant, id)
	if err != nil {
		return nil, spi.ErrObjectNotFound
	}
	if _, del := obj[spi.FieldDeletedAt]; del {
		return nil, spi.ErrObjectNotFound
	}
	idStr, _ := obj[spi.FieldID].(string)
	return &metaRow{EngineID: idStr, TenantID: tenant, Type: typ}, nil
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

func (p *Provider) insertLinkRow(tx DBTX, l *obda.CompiledLink, tenant, id, fromID, toID string, props map[string]any, now string) error {
	b := l.Binding()
	cols := make([]string, 0, len(b.SelectColumns))
	vals := make([]any, 0, len(b.SelectColumns))
	seen := map[string]struct{}{}
	add := func(col string, v any) {
		if col == "" {
			return
		}
		if _, ok := seen[col]; ok {
			return
		}
		seen[col] = struct{}{}
		cols = append(cols, col)
		vals = append(vals, v)
	}
	for _, col := range l.IdentityColumns {
		add(col, id)
	}
	if l.TenantColumn != "" {
		add(l.TenantColumn, tenant)
	}
	for _, col := range l.FromColumns {
		add(col, fromID)
	}
	for _, col := range l.ToColumns {
		add(col, toID)
	}
	for _, f := range l.Fields {
		if _, ok := seen[f.Column]; ok {
			continue
		}
		v, ok := props[f.Logical]
		if !ok {
			continue
		}
		add(f.Column, writeValue(l.PropertyTypes[f.Logical], v))
	}
	if !l.Omit.Version {
		add("version", 1)
	}
	if !l.Omit.CreatedAt {
		add("created_at", now)
	}
	if !l.Omit.UpdatedAt {
		add("updated_at", now)
	}
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
	if err := matchDirectID(l.Name, id); err != nil {
		return nil, spi.ErrLinkNotFound
	}
	b := l.Binding()
	sel, args, err := obda.PlanGetObject(b, tenant, []any{id})
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
			return nil, spi.ErrLinkNotFound
		}
		return nil, sqlitedialect.Classify(err)
	}
	biz := map[string]any{}
	for i, col := range b.SelectColumns {
		biz[col] = unwrap(dest[i])
	}
	return p.assembleLink(l, tenant, biz)
}

func (p *Provider) assembleLink(l *obda.CompiledLink, tenant string, biz map[string]any) (spi.OntologyLink, error) {
	id := ""
	if len(l.IdentityColumns) > 0 {
		id = fmt.Sprint(biz[l.IdentityColumns[0]])
	}
	link := spi.OntologyLink{
		spi.FieldID:           id,
		spi.FieldType:         l.Name,
		spi.FieldTenantID:     tenant,
		spi.LinkFieldFromID:   fmt.Sprint(biz[firstCol(l.FromColumns)]),
		spi.LinkFieldToID:     fmt.Sprint(biz[firstCol(l.ToColumns)]),
		spi.LinkFieldFromType: l.FromObject,
		spi.LinkFieldToType:   l.ToObject,
	}
	if l.Omit.Version {
		link[spi.FieldVersion] = 0
	} else {
		link[spi.FieldVersion] = asInt(biz["version"])
	}
	if !l.Omit.CreatedAt {
		if v := biz["created_at"]; v != nil {
			link[spi.FieldCreatedAt] = fmt.Sprint(v)
		}
	}
	if !l.Omit.UpdatedAt {
		if v := biz["updated_at"]; v != nil {
			link[spi.FieldUpdatedAt] = fmt.Sprint(v)
		}
	}
	if !l.Omit.DeletedAt {
		if v := biz["deleted_at"]; v != nil && fmt.Sprint(v) != "" {
			link[spi.FieldDeletedAt] = fmt.Sprint(v)
		}
	}
	for _, f := range l.Fields {
		raw := biz[f.Column]
		v, err := p.dialect.NormalizeValue(l.PropertyTypes[f.Logical], raw)
		if err != nil {
			return nil, err
		}
		link[f.Logical] = v
	}
	return link, nil
}

func copyLinkProps(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		if spi.IsLinkSystemField(k) {
			continue
		}
		out[k] = v
	}
	return out
}

func firstCol(cols []string) string {
	if len(cols) == 0 {
		return ""
	}
	return cols[0]
}
