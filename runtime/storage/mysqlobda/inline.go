package mysqlobda

import (
	"fmt"

	"github.com/openfoundry/runtime/obda"
	mysqldialect "github.com/openfoundry/runtime/obda/dialect/mysql"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

func hostPeerIDs(l *obda.CompiledLink, fromID, toID string) (hostID, peerID string) {
	if l.HostModel == l.ToObject {
		return toID, fromID
	}
	return fromID, toID
}

func peerType(l *obda.CompiledLink) string {
	if l.HostModel == l.FromObject {
		return l.ToObject
	}
	return l.FromObject
}

func inlineLinksOnHost(act *activation, hostType string) []*obda.CompiledLink {
	var out []*obda.CompiledLink
	for _, l := range act.compiled.Links {
		if l.Inline && l.HostModel == hostType {
			out = append(out, l)
		}
	}
	return out
}

func hasNonEmptyProps(props map[string]any) bool {
	for _, v := range props {
		if v != nil && fmt.Sprint(v) != "" {
			return true
		}
	}
	return false
}

func (p *Provider) createInlineLink(tx DBTX, act *activation, ctx spi.RequestContext, l *obda.CompiledLink, fromID, toID string, properties map[string]any) (spi.OntologyLink, error) {
	if !l.FKNullable {
		return nil, fmt.Errorf("%w: required inline link %q uses object APIs", spi.ErrUnsupportedCapability, l.Name)
	}
	if hasNonEmptyProps(copyLinkProps(properties)) {
		return nil, fmt.Errorf("%w: inline link %q does not accept properties", spi.ErrInvalidMapping, l.Name)
	}
	if _, err := p.requireLiveEndpoint(tx, act, ctx.TenantID, l.FromObject, fromID); err != nil {
		return nil, err
	}
	if _, err := p.requireLiveEndpoint(tx, act, ctx.TenantID, l.ToObject, toID); err != nil {
		return nil, err
	}
	hostID, peerID := hostPeerIDs(l, fromID, toID)
	host, err := act.model(l.HostModel)
	if err != nil {
		return nil, err
	}
	obj, err := p.loadObject(tx, host, ctx.TenantID, hostID)
	if err != nil {
		return nil, err
	}
	now := nowRFC3339()
	cols := []string{l.FKColumn}
	vals := []any{peerID}
	if !host.Omit.UpdatedAt {
		cols = append(cols, "updated_at")
		vals = append(vals, now)
	}
	if !host.Omit.Version {
		cols = append(cols, "version")
		vals = append(vals, asInt(obj[spi.FieldVersion])+1)
	}
	upd, args, err := obda.PlanUpdateObject(host.Binding(), ctx.TenantID, []any{hostID}, cols, vals)
	if err != nil {
		return nil, err
	}
	upd.Where = andPred(upd.Where, &sqlast.Predicate{
		Op:    "is_null",
		Field: &sqlast.Identifier{Name: l.FKColumn},
	})
	if !host.Omit.DeletedAt {
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
		return nil, mysqldialect.Classify(err)
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil, spi.ErrCardinalityViolation
	}
	return p.loadInlineLink(tx, act, l, ctx.TenantID, obda.EncodeDirect(l.Name, []string{hostID}))
}

func (p *Provider) deleteInlineLink(tx DBTX, act *activation, ctx spi.RequestContext, l *obda.CompiledLink, linkID string) error {
	if !l.FKNullable {
		return fmt.Errorf("%w: required inline link %q uses object APIs", spi.ErrUnsupportedCapability, l.Name)
	}
	hostID, err := inlineHostID(l, linkID)
	if err != nil {
		return nil
	}
	host, err := act.model(l.HostModel)
	if err != nil {
		return err
	}
	obj, err := p.loadObject(tx, host, ctx.TenantID, hostID)
	if err != nil {
		if err == spi.ErrObjectNotFound {
			return nil
		}
		return err
	}
	if _, del := obj[spi.FieldDeletedAt]; del {
		return nil
	}
	now := nowRFC3339()
	cols := []string{l.FKColumn}
	vals := []any{nil}
	if !host.Omit.UpdatedAt {
		cols = append(cols, "updated_at")
		vals = append(vals, now)
	}
	if !host.Omit.Version {
		cols = append(cols, "version")
		vals = append(vals, asInt(obj[spi.FieldVersion])+1)
	}
	upd, args, err := obda.PlanUpdateObject(host.Binding(), ctx.TenantID, []any{hostID}, cols, vals)
	if err != nil {
		return err
	}
	stmt, err := p.dialect.Render(upd)
	if err != nil {
		return err
	}
	_, err = tx.Exec(stmt.SQL, args...)
	return mysqldialect.Classify(err)
}

func inlineHostID(l *obda.CompiledLink, linkID string) (string, error) {
	typ, keys, err := obda.DecodeDirect(linkID)
	if err != nil || typ != l.Name || len(keys) != 1 || keys[0] == "" {
		return "", spi.ErrLinkNotFound
	}
	return keys[0], nil
}

func (p *Provider) loadInlineLink(tx DBTX, act *activation, l *obda.CompiledLink, tenant, linkID string) (spi.OntologyLink, error) {
	hostID, err := inlineHostID(l, linkID)
	if err != nil {
		return nil, spi.ErrLinkNotFound
	}
	host, err := act.model(l.HostModel)
	if err != nil {
		return nil, spi.ErrLinkNotFound
	}
	biz, err := p.loadBusiness(tx, host, tenant, []any{hostID})
	if err != nil {
		return nil, err
	}
	if biz == nil {
		return nil, spi.ErrLinkNotFound
	}
	fk := unwrap(biz[l.FKColumn])
	if fk == nil || fmt.Sprint(fk) == "" {
		return nil, spi.ErrLinkNotFound
	}
	return assembleInlineLink(l, tenant, biz)
}

func assembleInlineLink(l *obda.CompiledLink, tenant string, biz map[string]any) (spi.OntologyLink, error) {
	hostID := ""
	if len(l.IdentityColumns) > 0 {
		hostID = fmt.Sprint(biz[l.IdentityColumns[0]])
	}
	peerID := fmt.Sprint(unwrap(biz[l.FKColumn]))
	fromID, toID := hostID, peerID
	if l.HostModel == l.ToObject {
		fromID, toID = peerID, hostID
	}
	link := spi.OntologyLink{
		spi.FieldID:           obda.EncodeDirect(l.Name, []string{hostID}),
		spi.FieldType:         l.Name,
		spi.FieldTenantID:     tenant,
		spi.LinkFieldFromID:   fromID,
		spi.LinkFieldToID:     toID,
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
	return link, nil
}

func joinBindingForInline(l *obda.CompiledLink, host, peer *obda.CompiledModel, direction string, includeDeleted bool) obda.LinkJoinBinding {
	endCol := l.FKColumn
	fromHost := (direction != "inbound" && l.HostModel == l.FromObject) || (direction == "inbound" && l.HostModel == l.ToObject)
	if fromHost {
		endCol = firstCol(host.IdentityColumns)
	}
	return obda.LinkJoinBinding{
		LinkTable:       l.Table,
		LinkTenant:      l.TenantColumn,
		EndpointCol:     endCol,
		PeerTable:       peer.Table,
		PeerIDCol:       firstCol(peer.IdentityColumns),
		PeerTenantCol:   peer.TenantColumn,
		SelectColumns:   host.Binding().SelectColumns,
		OmitLinkDeleted: l.Omit.DeletedAt || includeDeleted,
		OmitPeerDeleted: peer.Omit.DeletedAt,
		Inline:          true,
		FKColumn:        l.FKColumn,
		HostPKCol:       firstCol(host.IdentityColumns),
	}
}

func (p *Provider) applyInlineFKs(tx DBTX, act *activation, m *obda.CompiledModel, tenant string, props map[string]any, cols *[]string, vals *[]any, seen map[string]struct{}) error {
	for _, l := range inlineLinksOnHost(act, m.Name) {
		v, ok := props[l.HostNavField]
		if !ok {
			continue
		}
		if v == nil || fmt.Sprint(v) == "" {
			if !l.FKNullable {
				return fmt.Errorf("%w: missing required navigation %q", spi.ErrInvalidMapping, l.HostNavField)
			}
			if _, skip := seen[l.FKColumn]; skip {
				continue
			}
			*cols = append(*cols, l.FKColumn)
			*vals = append(*vals, nil)
			seen[l.FKColumn] = struct{}{}
			continue
		}
		id, ok := v.(string)
		if !ok || id == "" {
			return fmt.Errorf("%w: navigation %q must be an object id", spi.ErrInvalidMapping, l.HostNavField)
		}
		if _, err := p.requireLiveEndpoint(tx, act, tenant, peerType(l), id); err != nil {
			return err
		}
		if _, skip := seen[l.FKColumn]; skip {
			continue
		}
		*cols = append(*cols, l.FKColumn)
		*vals = append(*vals, id)
		seen[l.FKColumn] = struct{}{}
	}
	return nil
}

func (p *Provider) requireInlineOnCreate(act *activation, typ string, props map[string]any) error {
	for _, l := range inlineLinksOnHost(act, typ) {
		if l.FKNullable {
			continue
		}
		v, ok := props[l.HostNavField]
		if !ok || v == nil || fmt.Sprint(v) == "" {
			return fmt.Errorf("%w: missing required navigation %q", spi.ErrInvalidMapping, l.HostNavField)
		}
	}
	return nil
}

func (p *Provider) clearInlineRefs(tx DBTX, act *activation, tenant, objectType, objectID string) error {
	for _, l := range act.compiled.Links {
		if !l.Inline {
			continue
		}
		if l.HostModel == objectType {
			continue
		}
		if peerType(l) != objectType {
			continue
		}
		host, err := act.model(l.HostModel)
		if err != nil {
			return err
		}
		tbl, err := p.dialect.QuoteIdentifier(sqlast.Identifier{Name: host.Table})
		if err != nil {
			return err
		}
		tenantCol, err := p.dialect.QuoteIdentifier(sqlast.Identifier{Name: host.TenantColumn})
		if err != nil {
			return err
		}
		fk, err := p.dialect.QuoteIdentifier(sqlast.Identifier{Name: l.FKColumn})
		if err != nil {
			return err
		}
		if !l.FKNullable {
			q := "SELECT COUNT(*) FROM " + tbl + " WHERE " + tenantCol + " = ? AND " + fk + " = ?"
			var n int
			if err := tx.QueryRow(q, tenant, objectID).Scan(&n); err != nil {
				return mysqldialect.Classify(err)
			}
			if n > 0 {
				return fmt.Errorf("%w: required inline %q still references this object", spi.ErrCardinalityViolation, l.Name)
			}
			continue
		}
		q := "UPDATE " + tbl + " SET " + fk + " = NULL"
		if !host.Omit.Version {
			ver, err := p.dialect.QuoteIdentifier(sqlast.Identifier{Name: "version"})
			if err != nil {
				return err
			}
			q += ", " + ver + " = " + ver + " + 1"
		}
		if !host.Omit.UpdatedAt {
			upd, err := p.dialect.QuoteIdentifier(sqlast.Identifier{Name: "updated_at"})
			if err != nil {
				return err
			}
			q += ", " + upd + " = ?"
			q += " WHERE " + tenantCol + " = ? AND " + fk + " = ?"
			if _, err := tx.Exec(q, nowRFC3339(), tenant, objectID); err != nil {
				return mysqldialect.Classify(err)
			}
			continue
		}
		q += " WHERE " + tenantCol + " = ? AND " + fk + " = ?"
		if _, err := tx.Exec(q, tenant, objectID); err != nil {
			return mysqldialect.Classify(err)
		}
	}
	return nil
}
