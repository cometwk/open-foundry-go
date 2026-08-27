package memory

import (
	"fmt"

	"github.com/openfoundry/runtime/spi"
)

// Compile-time assertion: *memoryTransaction satisfies spi.Transaction.
var _ spi.Transaction = (*memoryTransaction)(nil)

// memoryTransaction is the Phase 3 first Go implementation of spi.Transaction.
// Eager-apply under p.mu + reverse journal rollback mirrors TS
// MemoryTransaction (memory-storage-provider.ts:125-239). Commit is a
// no-op state flip; only Rollback restores Begin-time state. Covers R6,
// AE10.
type memoryTransaction struct {
	p          *Provider
	ctx        spi.RequestContext
	journal    []txEntry
	committed  bool
	rolledBack bool
}

// txEntry is one journaled mutation. Discriminated by Op; Prev is set for
// update/softDelete/hardDelete/updateLink/deleteLink. History is set for
// hardDeleteObject so rollback can restore versionHistory cleared by the
// hard-delete apply path.
type txEntry struct {
	Op      string
	Key     string
	Prev    map[string]any // OntologyObject or OntologyLink snapshot
	History []spi.OntologyObject
}

// BeginTransaction returns a new memoryTransaction bound to ctx. The
// RequestContext is fixed at Begin time (tenant isolation for all verbs).
// Covers R6.
func (p *Provider) BeginTransaction(ctx spi.RequestContext) (spi.Transaction, error) {
	return &memoryTransaction{p: p, ctx: ctx}, nil
}

func (tx *memoryTransaction) assertOpen() error {
	if tx.committed {
		return fmt.Errorf("transaction already committed")
	}
	if tx.rolledBack {
		return fmt.Errorf("transaction already rolled back")
	}
	return nil
}

func (tx *memoryTransaction) CreateObject(typ string, properties map[string]any) (spi.OntologyObject, error) {
	if err := tx.assertOpen(); err != nil {
		return nil, err
	}
	tx.p.mu.Lock()
	defer tx.p.mu.Unlock()
	obj, err := tx.p.doCreateObjectUnlocked(tx.ctx, typ, properties)
	if err != nil {
		return nil, err
	}
	id, _ := obj[spi.FieldID].(string)
	key := objectKey(typ, id)
	tx.journal = append(tx.journal, txEntry{Op: "createObject", Key: key})
	return obj, nil
}

func (tx *memoryTransaction) UpdateObject(typ, id string, properties map[string]any, expectedVersion *int) (spi.OntologyObject, error) {
	if err := tx.assertOpen(); err != nil {
		return nil, err
	}
	tx.p.mu.Lock()
	defer tx.p.mu.Unlock()
	key := objectKey(typ, id)
	prev, err := tx.cloneObjectInternal(key)
	if err != nil {
		return nil, err
	}
	updated, err := tx.p.doUpdateObjectUnlocked(tx.ctx, typ, id, properties, expectedVersion)
	if err != nil {
		return nil, err
	}
	tx.journal = append(tx.journal, txEntry{Op: "updateObject", Key: key, Prev: prev})
	return updated, nil
}

func (tx *memoryTransaction) DeleteObject(typ, id, mode string) error {
	if err := tx.assertOpen(); err != nil {
		return err
	}
	tx.p.mu.Lock()
	defer tx.p.mu.Unlock()
	key := objectKey(typ, id)
	prev, err := tx.cloneObjectInternal(key)
	if err != nil {
		return err
	}
	var hist []spi.OntologyObject
	if mode == "hard" {
		hist = cloneHistorySlice(tx.p.versionHistory[key])
	}
	if err := tx.p.doDeleteObjectUnlocked(tx.ctx, typ, id, mode); err != nil {
		return err
	}
	op := "softDeleteObject"
	if mode == "hard" {
		op = "hardDeleteObject"
	}
	tx.journal = append(tx.journal, txEntry{Op: op, Key: key, Prev: prev, History: hist})
	return nil
}

func (tx *memoryTransaction) CreateLink(typ, fromID, toID string, properties map[string]any) (spi.OntologyLink, error) {
	if err := tx.assertOpen(); err != nil {
		return nil, err
	}
	tx.p.mu.Lock()
	defer tx.p.mu.Unlock()
	link, err := tx.p.doCreateLinkUnlocked(tx.ctx, typ, fromID, toID, properties)
	if err != nil {
		return nil, err
	}
	id, _ := link[spi.FieldID].(string)
	key := linkKey(typ, id)
	tx.journal = append(tx.journal, txEntry{Op: "createLink", Key: key})
	return link, nil
}

func (tx *memoryTransaction) UpdateLink(typ, linkID string, properties map[string]any, expectedVersion *int) (spi.OntologyLink, error) {
	if err := tx.assertOpen(); err != nil {
		return nil, err
	}
	tx.p.mu.Lock()
	defer tx.p.mu.Unlock()
	key := linkKey(typ, linkID)
	prev, err := tx.cloneLinkInternal(key)
	if err != nil {
		return nil, err
	}
	updated, err := tx.p.doUpdateLinkUnlocked(tx.ctx, typ, linkID, properties, expectedVersion)
	if err != nil {
		return nil, err
	}
	tx.journal = append(tx.journal, txEntry{Op: "updateLink", Key: key, Prev: prev})
	return updated, nil
}

func (tx *memoryTransaction) DeleteLink(typ, linkID string) error {
	if err := tx.assertOpen(); err != nil {
		return err
	}
	tx.p.mu.Lock()
	defer tx.p.mu.Unlock()
	key := linkKey(typ, linkID)
	prev, err := tx.cloneLinkInternal(key)
	if err != nil {
		return err
	}
	if err := tx.p.doDeleteLinkUnlocked(tx.ctx, typ, linkID); err != nil {
		return err
	}
	tx.journal = append(tx.journal, txEntry{Op: "deleteLink", Key: key, Prev: prev})
	return nil
}

// Commit flips state; data was already applied eagerly. Covers R6.
func (tx *memoryTransaction) Commit() error {
	if err := tx.assertOpen(); err != nil {
		return err
	}
	tx.committed = true
	return nil
}

// Rollback reverses the journal under p.mu and restores Begin-time state
// including versionHistory pops for update/softDelete and full history
// restore for hardDelete. Covers R6, AE10.
func (tx *memoryTransaction) Rollback() error {
	if err := tx.assertOpen(); err != nil {
		return err
	}
	tx.rolledBack = true
	tx.p.mu.Lock()
	defer tx.p.mu.Unlock()
	for i := len(tx.journal) - 1; i >= 0; i-- {
		e := tx.journal[i]
		switch e.Op {
		case "createObject":
			delete(tx.p.objects, e.Key)
			delete(tx.p.versionHistory, e.Key)
		case "updateObject", "softDeleteObject":
			tx.p.objects[e.Key] = e.Prev
			tx.p.popVersionHistoryUnlocked(e.Key)
		case "hardDeleteObject":
			tx.p.objects[e.Key] = e.Prev
			if e.History == nil {
				delete(tx.p.versionHistory, e.Key)
			} else {
				tx.p.versionHistory[e.Key] = e.History
			}
		case "createLink":
			delete(tx.p.links, e.Key)
		case "updateLink", "deleteLink":
			tx.p.links[e.Key] = e.Prev
		}
	}
	return nil
}

// cloneObjectInternal snapshots the live map entry for journaling. Soft-
// deleted objects are still journalable (mirrors TS _getObjectInternal,
// which does not mask _deletedAt). Missing / cross-tenant → ErrObjectNotFound.
// Caller MUST hold p.mu.
func (tx *memoryTransaction) cloneObjectInternal(key string) (spi.OntologyObject, error) {
	obj, ok := tx.p.objects[key]
	if !ok || obj[spi.FieldTenantID] != tx.ctx.TenantID {
		return nil, fmt.Errorf("%w: %s", spi.ErrObjectNotFound, key)
	}
	return cloneObject(obj)
}

// cloneLinkInternal snapshots the live link map entry. Caller MUST hold p.mu.
func (tx *memoryTransaction) cloneLinkInternal(key string) (spi.OntologyLink, error) {
	link, ok := tx.p.links[key]
	if !ok || link[spi.FieldTenantID] != tx.ctx.TenantID {
		return nil, fmt.Errorf("%w: %s", spi.ErrLinkNotFound, key)
	}
	return cloneLink(link)
}

// cloneHistorySlice deep-copies each snapshot so rollback cannot share
// mutable map references with live history.
func cloneHistorySlice(in []spi.OntologyObject) []spi.OntologyObject {
	if in == nil {
		return nil
	}
	out := make([]spi.OntologyObject, 0, len(in))
	for _, snap := range in {
		c, err := cloneObject(snap)
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	return out
}
