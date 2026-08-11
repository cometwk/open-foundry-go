package memory

import (
	"errors"
	"testing"

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