package memory

import (
	"errors"
	"testing"
	"time"

	"github.com/openfoundry/runtime/spi"
)

// TestSentinelErrors_IdentifiableViaErrorsIs verifies the two Phase 3
// sentinels added to runtime/spi/errors.go are repeatedly identifiable by
// errors.Is, including when wrapped via fmt.Errorf("%w: …"). This is the
// stable contract KTD-2 commits to (mirroring the Phase 1/2 sentinels).
// Covers U1 (sentinel scaffolding) — the verbs that return these sentinels
// land in U2 (ErrVersionConflict) and U6 (ErrCardinalityViolation).
func TestSentinelErrors_IdentifiableViaErrorsIs(t *testing.T) {
	cases := []struct {
		name    string
		sentiel error
	}{
		{"ErrVersionConflict", spi.ErrVersionConflict},
		{"ErrCardinalityViolation", spi.ErrCardinalityViolation},
	}
	for _, c := range cases {
		// Direct equality.
		if !errors.Is(c.sentiel, c.sentiel) {
			t.Errorf("%s: errors.Is(self) = false, want true", c.name)
		}
		// After fmt.Errorf("%w: …") wrapping, mirroring the provider's
		// (U2/U6) wrapping style: the sentinel must still match.
		wrapped := wrapForTest(c.sentiel, "Supplier/s1")
		if !errors.Is(wrapped, c.sentiel) {
			t.Errorf("%s: errors.Is(wrapped) = false, want true (wrapped=%v)", c.name, wrapped)
		}
	}
}

// wrapForTest reproduces the fmt.Errorf("%w: detail") shape the memory
// provider uses (see existing ErrObjectNotFound wraps at provider.go:156,
// 170); kept inline so this test does not import "fmt" just for one call.
func wrapForTest(sentinel error, detail string) error {
	return sentinelWrap{sentinel: sentinel, detail: detail}
}

type sentinelWrap struct {
	sentinel error
	detail   string
}

func (w sentinelWrap) Error() string { return w.sentinel.Error() + ": " + w.detail }
func (w sentinelWrap) Is(target error) bool {
	return errors.Is(w.sentinel, target)
}

// TestCreateObject_PushesVersionHistory asserts the Phase 3 U1 scaffolding:
// CreateObject appends exactly one snapshot to the (type:id) history slice,
// and that snapshot carries the authoritative _version:1 the stored object
// also carries. Read-path coverage (GetObjectAtVersion/AtTime) lands in U2;
// here we only assert the write-path invariant the scaffolding introduced.
func TestCreateObject_PushesVersionHistory(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, err := p.CreateObject(a, "Supplier", map[string]any{"name": "Acme"})
	if err != nil {
		t.Fatalf("CreateObject err: %v", err)
	}
	key := objectKey("Supplier", obj["_id"].(string))

	p.mu.Lock()
	defer p.mu.Unlock()
	hist, ok := p.versionHistory[key]
	if !ok {
		t.Fatalf("versionHistory has no entry for %s after CreateObject (U1)", key)
	}
	if len(hist) != 1 {
		t.Fatalf("versionHistory[%s] len = %d, want 1 (U1 one snapshot on create)", key, len(hist))
	}
	snap := hist[0]
	if snap["_id"] != obj["_id"] {
		t.Errorf("snapshot _id = %v, want %v (U1)", snap["_id"], obj["_id"])
	}
	if snap["_type"] != "Supplier" {
		t.Errorf("snapshot _type = %v, want Supplier (U1)", snap["_type"])
	}
	// Snapshot must carry the same _version value the caller-visible object
	// carries. Both are JSON clones of the stored map (int authoritative),
	// so _version is float64 in both clones. Compare by value, not type.
	snapVer, snapOk := snap["_version"]
	objVer, objOk := obj["_version"]
	if !snapOk || !objOk {
		t.Errorf("snapshot or object missing _version: snap=%v obj=%v (U1)", snapVer, objVer)
	} else if snapVer != objVer {
		t.Errorf("snapshot _version = %v, want %v (U1 authoritative copy)", snapVer, objVer)
	}
}

// TestCreateLink_DoesNotPushVersionHistory asserts the asymmetry mirrored
// from the TS memory provider: links never push version history, even
// though CreateObject does (KTD-5 + R2). The link endpoints must exist.
func TestCreateLink_DoesNotPushVersionHistory(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	s := createObjectForTest(t, p, a, "Supplier")
	pt := createObjectForTest(t, p, a, "Part")
	link, err := p.CreateLink(a, "Supplies", s["_id"].(string), pt["_id"].(string), nil)
	if err != nil {
		t.Fatalf("CreateLink err: %v", err)
	}
	linkK := linkKey("Supplies", link["_id"].(string))

	p.mu.Lock()
	defer p.mu.Unlock()
	if hist, ok := p.versionHistory[linkK]; ok {
		t.Errorf("link key %s has versionHistory len %d, want no entry (U1 links skip history)", linkK, len(hist))
	}
	// Sanity: the objects on either end DID get history entries, so the link
	// skip is an explicit asymmetry, not a wiring bug in the key scheme.
	for _, endKey := range []string{
		objectKey("Supplier", s["_id"].(string)),
		objectKey("Part", pt["_id"].(string)),
	} {
		if _, ok := p.versionHistory[endKey]; !ok {
			t.Errorf("end object %s missing versionHistory entry (U1 sanity)", endKey)
		}
	}
}

// TestGetObjectAtVersion_ReturnsSnapshotAtVersion exercises the temporal
// read path landed in U2. Create pushes version 1; each accepted update
// pushes version N+1; GetObjectAtVersion returns the snapshot at that
// version. Covers R2, AE1 (temporal read half).
func TestGetObjectAtVersion_ReturnsSnapshotAtVersion(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "v1"})
	id := obj["_id"].(string)

	// version 1, 2, 3 — drive through UpdateObject with nil expectedVersion
	// (accept any) to advance _version and push snapshots.
	if _, err := p.UpdateObject(a, "Supplier", id, map[string]any{"name": "v2"}, nil); err != nil {
		t.Fatalf("UpdateObject v1->v2 err: %v", err)
	}
	if _, err := p.UpdateObject(a, "Supplier", id, map[string]any{"name": "v3"}, nil); err != nil {
		t.Fatalf("UpdateObject v2->v3 err: %v", err)
	}

	cases := []struct {
		version    int
		wantName any
	}{
		{1, "v1"},
		{2, "v2"},
		{3, "v3"},
	}
	for _, c := range cases {
		got, err := p.GetObjectAtVersion(a, "Supplier", id, c.version)
		if err != nil {
			t.Fatalf("GetObjectAtVersion(%d) err = %v, want nil (U2 AE1)", c.version, err)
		}
		if got["name"] != c.wantName {
			t.Errorf("GetObjectAtVersion(%d) name = %v, want %v (U2 AE1)", c.version, got["name"], c.wantName)
		}
		if v := objectVersionValue(got); v != c.version {
			t.Errorf("GetObjectAtVersion(%d) _version = %d, want %d (U2 AE1)", c.version, v, c.version)
		}
	}

	// Missing version surfaces typed not-found; no leak.
	if _, err := p.GetObjectAtVersion(a, "Supplier", id, 999); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("GetObjectAtVersion(missing) err = %v, want ErrObjectNotFound (U2 AE1)", err)
	}
	// Missing object+version surfaces typed not-found.
	if _, err := p.GetObjectAtVersion(a, "Supplier", "never-existed", 1); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("GetObjectAtVersion(missing obj) err = %v, want ErrObjectNotFound (U2 AE1)", err)
	}
}

// TestGetObjectAtVersion_CrossTenant_NoLeak asserts the temporal read
// path honors tenant isolation: tenant B cannot retrieve tenant A's
// snapshots. Covers R2 + the cross-tenant mask KTD-1/Tenancy invariant.
func TestGetObjectAtVersion_CrossTenant_NoLeak(t *testing.T) {
	p := New()
	a, b := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "v1"})
	id := obj["_id"].(string)
	if _, err := p.UpdateObject(a, "Supplier", id, map[string]any{"name": "v2"}, nil); err != nil {
		t.Fatalf("UpdateObject err: %v", err)
	}
	if _, err := p.GetObjectAtVersion(b, "Supplier", id, 1); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("GetObjectAtVersion cross-tenant err = %v, want ErrObjectNotFound (U2 no leak)", err)
	}
}

// TestGetObjectAtTime_ReturnsNewestAtOrBefore exercises the temporal
// read path for time-based lookup. Create pushes v1 at t0; update pushes
// v2 at t1; GetObjectAtTime(t介于) returns the newest snapshot whose
// _updatedAt ≤ ts. Covers R2, AE1 (temporal read half).
func TestGetObjectAtTime_ReturnsNewestAtOrBefore(t *testing.T) {
	p := New()
	a, _ := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "v1"})
	id := obj["_id"].(string)

	// Capture t0 after the create so GetObjectAtTime(t0) sees v1.
	p.mu.Lock()
	t0Snap, _ := cloneObject(p.objects[objectKey("Supplier", id)])
	p.mu.Unlock()
	t0Str, _ := t0Snap["_updatedAt"].(string)
	t0, err := time.Parse(time.RFC3339Nano, t0Str)
	if err != nil {
		t.Fatalf("parse t0 _updatedAt %q: %v", t0Str, err)
	}

	// Advance time so the next snapshot's _updatedAt is strictly after t0.
	time.Sleep(2 * time.Millisecond)
	if _, err := p.UpdateObject(a, "Supplier", id, map[string]any{"name": "v2"}, nil); err != nil {
		t.Fatalf("UpdateObject err: %v", err)
	}
	p.mu.Lock()
	t1Snap, _ := cloneObject(p.objects[objectKey("Supplier", id)])
	p.mu.Unlock()
	t1Str, _ := t1Snap["_updatedAt"].(string)
	t1, err := time.Parse(time.RFC3339Nano, t1Str)
	if err != nil {
		t.Fatalf("parse t1 _updatedAt %q: %v", t1Str, err)
	}

	// At t0: only v1 is admissible.
	got0, err := p.GetObjectAtTime(a, "Supplier", id, t0)
	if err != nil {
		t.Fatalf("GetObjectAtTime(t0) err = %v, want nil (U2 AE1)", err)
	}
	if got0["name"] != "v1" {
		t.Errorf("GetObjectAtTime(t0) name = %v, want v1 (U2 AE1)", got0["name"])
	}

	// At t1: v2 is newest at-or-before.
	got1, err := p.GetObjectAtTime(a, "Supplier", id, t1)
	if err != nil {
		t.Fatalf("GetObjectAtTime(t1) err = %v, want nil (U2 AE1)", err)
	}
	if got1["name"] != "v2" {
		t.Errorf("GetObjectAtTime(t1) name = %v, want v2 (U2 AE1)", got1["name"])
	}

	// Before any snapshot: not-found (no leak).
	pre := t0.Add(-1 * time.Second)
	if _, err := p.GetObjectAtTime(a, "Supplier", id, pre); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("GetObjectAtTime(pre-t0) err = %v, want ErrObjectNotFound (U2 AE1)", err)
	}
	// Missing object: not-found (no leak).
	if _, err := p.GetObjectAtTime(a, "Supplier", "never-existed", t1); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("GetObjectAtTime(missing obj) err = %v, want ErrObjectNotFound (U2 AE1 no leak)", err)
	}
}

// TestGetObjectAtTime_CrossTenant_NoLeak asserts time-based lookup also
// honors tenant isolation. Covers R2 + cross-tenant mask.
func TestGetObjectAtTime_CrossTenant_NoLeak(t *testing.T) {
	p := New()
	a, b := tenancyA()
	obj, _ := p.CreateObject(a, "Supplier", map[string]any{"name": "v1"})
	id := obj["_id"].(string)
	p.mu.Lock()
	snap, _ := cloneObject(p.objects[objectKey("Supplier", id)])
	p.mu.Unlock()
	ts, _ := time.Parse(time.RFC3339Nano, snap["_updatedAt"].(string))
	if _, err := p.GetObjectAtTime(b, "Supplier", id, ts); !errors.Is(err, spi.ErrObjectNotFound) {
		t.Errorf("GetObjectAtTime cross-tenant err = %v, want ErrObjectNotFound (U2 no leak)", err)
	}
}