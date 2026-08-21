package sqliteobda

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/openfoundry/runtime/spi"
)

var _ spi.Transaction = (*sqlTxn)(nil)

type sqlTxn struct {
	p    *Provider
	ctx  spi.RequestContext
	act  *activation
	tx   *sql.Tx
	conn *sql.Conn

	mu     sync.Mutex
	closed bool
}

func (p *Provider) BeginTransaction(ctx spi.RequestContext) (spi.Transaction, error) {
	act, err := p.pin(ctx)
	if err != nil {
		return nil, err
	}
	tx, conn, err := p.begin()
	if err != nil {
		return nil, err
	}
	return &sqlTxn{p: p, ctx: ctx, act: act, tx: tx, conn: conn}, nil
}

func (t *sqlTxn) assertOpen() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return fmt.Errorf("transaction already closed")
	}
	return nil
}

func (t *sqlTxn) CreateObject(typ string, properties map[string]any) (spi.OntologyObject, error) {
	if err := t.assertOpen(); err != nil {
		return nil, err
	}
	return t.p.createObjectTx(t.tx, t.act, t.ctx, typ, properties)
}

func (t *sqlTxn) UpdateObject(typ, id string, properties map[string]any, expectedVersion *int) (spi.OntologyObject, error) {
	if err := t.assertOpen(); err != nil {
		return nil, err
	}
	return t.p.updateObjectTx(t.tx, t.act, t.ctx, typ, id, properties, expectedVersion)
}

func (t *sqlTxn) DeleteObject(typ, id, mode string) error {
	if err := t.assertOpen(); err != nil {
		return err
	}
	return t.p.deleteObjectTx(t.tx, t.act, t.ctx, typ, id, mode)
}

func (t *sqlTxn) CreateLink(typ, fromID, toID string, properties map[string]any) (spi.OntologyLink, error) {
	if err := t.assertOpen(); err != nil {
		return nil, err
	}
	return t.p.createLinkTx(t.tx, t.act, t.ctx, typ, fromID, toID, properties)
}

func (t *sqlTxn) UpdateLink(typ, linkID string, properties map[string]any, expectedVersion *int) (spi.OntologyLink, error) {
	if err := t.assertOpen(); err != nil {
		return nil, err
	}
	return t.p.UpdateLink(t.ctx, typ, linkID, properties, expectedVersion)
}

func (t *sqlTxn) DeleteLink(typ, linkID string) error {
	if err := t.assertOpen(); err != nil {
		return err
	}
	return t.p.DeleteLink(t.ctx, typ, linkID)
}

func (t *sqlTxn) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return fmt.Errorf("transaction already closed")
	}
	t.closed = true
	err := t.tx.Commit()
	_ = t.conn.Close()
	return err
}

func (t *sqlTxn) Rollback() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return fmt.Errorf("transaction already closed")
	}
	t.closed = true
	err := t.tx.Rollback()
	_ = t.conn.Close()
	return err
}
