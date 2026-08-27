package memory

import (
	"errors"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/spi"
)

// Covers AE10 / R6: MemoryTransaction commit visibility, rollback undo,
// versionHistory pop, assertOpen, and cross-tenant isolation.

func TestTransaction_CreateObject_CommitVisible(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	tx, err := p.BeginTransaction(a)
	if err != nil {
		t.Fatalf("BeginTransaction err = %v (U7)", err)
	}
	obj, err := tx.CreateObject("Supplier", map[string]any{"name": "Acme"})
	if err != nil {
		t.Fatalf("tx.CreateObject err = %v", err)
	}
	id := obj["_id"].(string)
	// Eager-apply: visible on provider before commit.
	if _, err := p.GetObject(a, "Supplier", id); err != nil {
		t.Fatalf("eager GetObject before commit err = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit err = %v", err)
	}
	got, err := p.GetObject(a, "Supplier", id)
	if err != nil {
		t.Fatalf("GetObject after commit err = %v (AE10)", err)
	}
	if got["name"] != "Acme" {
		t.Errorf("name = %v, want Acme", got["name"])
	}
}

func TestTransaction_UpdateObject_RollbackRestores(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	created, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme", "tier": "Bronze"})
	id := created["_id"].(string)

	tx, _ := p.BeginTransaction(a)
	if _, err := tx.UpdateObject("Supplier", id, map[string]any{"tier": "Gold"}, nil); err != nil {
		t.Fatalf("tx.UpdateObject err = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback err = %v", err)
	}
	got, err := p.GetObject(a, "Supplier", id)
	if err != nil {
		t.Fatalf("GetObject after rollback err = %v", err)
	}
	if got["tier"] != "Bronze" {
		t.Errorf("tier after rollback = %v, want Bronze (AE10)", got["tier"])
	}
	if objectVersionValue(got) != 1 {
		t.Errorf("_version after rollback = %d, want 1 (AE10)", objectVersionValue(got))
	}
}

func TestTransaction_SoftDelete_RollbackRestores(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	created, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme"})
	id := created["_id"].(string)

	tx, _ := p.BeginTransaction(a)
	if err := tx.DeleteObject("Supplier", id, "soft"); err != nil {
		t.Fatalf("tx.DeleteObject soft err = %v", err)
	}
	if _, err := p.GetObject(a, "Supplier", id); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("eager soft-delete should mask GetObject, err = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback err = %v", err)
	}
	got, err := p.GetObject(a, "Supplier", id)
	if err != nil {
		t.Fatalf("GetObject after soft-delete rollback err = %v (AE10)", err)
	}
	if got["_deletedAt"] != nil {
		t.Errorf("_deletedAt after rollback = %v, want unset", got["_deletedAt"])
	}
}

func TestTransaction_HardDelete_RollbackRestores(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	created, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme"})
	id := created["_id"].(string)

	tx, _ := p.BeginTransaction(a)
	if err := tx.DeleteObject("Supplier", id, "hard"); err != nil {
		t.Fatalf("tx.DeleteObject hard err = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback err = %v", err)
	}
	if _, err := p.GetObject(a, "Supplier", id); err != nil {
		t.Errorf("GetObject after hard-delete rollback err = %v, want nil (AE10)", err)
	}
}

func TestTransaction_CreateLink_RollbackRemoves(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	applySupplyLinkSchema(t, p, a)
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")

	tx, _ := p.BeginTransaction(a)
	link, err := tx.CreateLink("Supplies", s["_id"].(string), pt["_id"].(string), nil)
	if err != nil {
		t.Fatalf("tx.CreateLink err = %v", err)
	}
	id := link["_id"].(string)
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback err = %v", err)
	}
	if _, err := p.GetLink(a, "Supplies", id); !errors.Is(err, spi.ErrLinkNotFound) {
		t.Errorf("GetLink after createLink rollback err = %v, want ErrLinkNotFound (AE10)", err)
	}
}

func TestTransaction_UpdateLink_RollbackRestores(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	applySupplyLinkSchema(t, p, a)
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	created, _ := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), map[string]any{"qty": 1})
	id := created["_id"].(string)

	tx, _ := p.BeginTransaction(a)
	if _, err := tx.UpdateLink("Supplies", id, map[string]any{"qty": 9}, nil); err != nil {
		t.Fatalf("tx.UpdateLink err = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback err = %v", err)
	}
	got, err := p.GetLink(a, "Supplies", id)
	if err != nil {
		t.Fatalf("GetLink after updateLink rollback err = %v", err)
	}
	if asInt(got["qty"]) != 1 {
		t.Errorf("qty after rollback = %v, want 1 (AE10)", got["qty"])
	}
}

func TestTransaction_DeleteLink_RollbackRestores(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	applySupplyLinkSchema(t, p, a)
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	created, _ := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	id := created["_id"].(string)

	tx, _ := p.BeginTransaction(a)
	if err := tx.DeleteLink("Supplies", id); err != nil {
		t.Fatalf("tx.DeleteLink err = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback err = %v", err)
	}
	if _, err := p.GetLink(a, "Supplies", id); err != nil {
		t.Errorf("GetLink after deleteLink rollback err = %v, want nil (AE10)", err)
	}
}

func TestTransaction_Update_RollbackPopsVersionHistory(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	created, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme", "tier": "Bronze"})
	id := created["_id"].(string)

	tx, _ := p.BeginTransaction(a)
	if _, err := tx.UpdateObject("Supplier", id, map[string]any{"tier": "Gold"}, nil); err != nil {
		t.Fatalf("tx.UpdateObject err = %v", err)
	}
	// During tx, version 2 snapshot exists.
	if _, err := p.GetObjectAtVersion(a, "Supplier", id, 2); err != nil {
		t.Fatalf("GetObjectAtVersion(2) during tx err = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback err = %v", err)
	}
	if _, err := p.GetObjectAtVersion(a, "Supplier", id, 2); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("GetObjectAtVersion(2) after rollback err = %v, want ErrObjectNotFound (history popped) (AE10)", err)
	}
	v1, err := p.GetObjectAtVersion(a, "Supplier", id, 1)
	if err != nil {
		t.Fatalf("GetObjectAtVersion(1) after rollback err = %v", err)
	}
	if v1["tier"] != "Bronze" {
		t.Errorf("v1 tier = %v, want Bronze", v1["tier"])
	}
}

func TestTransaction_AssertOpen_AfterCommitAndRollback(t *testing.T) {
	p := New()
	a, _ := tenancyA()

	tx1, _ := p.BeginTransaction(a)
	_ = tx1.Commit()
	if _, err := tx1.CreateObject("Supplier", map[string]any{"name": "x"}); err == nil || !strings.Contains(err.Error(), "committed") {
		t.Errorf("post-commit CreateObject err = %v, want committed error (AE10)", err)
	}

	tx2, _ := p.BeginTransaction(a)
	_ = tx2.Rollback()
	if err := tx2.DeleteObject("Supplier", "x", "hard"); err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("post-rollback DeleteObject err = %v, want rolled back error (AE10)", err)
	}
}

func TestTransaction_CrossTenantIsolation(t *testing.T) {
	p := New()
	a, b := tenancyA()
	created, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme"})
	id := created["_id"].(string)

	txB, _ := p.BeginTransaction(b)
	_, err := txB.UpdateObject("Supplier", id, map[string]any{"name": "Evil"}, nil)
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("tenantB tx.UpdateObject err = %v, want ErrObjectNotFound (AE10)", err)
	}
	got, err := p.GetObject(a, "Supplier", id)
	if err != nil {
		t.Fatalf("tenantA GetObject err = %v", err)
	}
	if got["name"] != "Acme" {
		t.Errorf("tenantA name = %v, want Acme (no cross-tenant leak)", got["name"])
	}
}

func TestCapabilities_TransactionsEnabled(t *testing.T) {
	if !New().Capabilities().SupportsTransactions {
		t.Error("SupportsTransactions = false, want true (U7)")
	}
}
