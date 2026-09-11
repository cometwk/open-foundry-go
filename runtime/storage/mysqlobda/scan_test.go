package mysqlobda

import (
	"database/sql"
	"fmt"
	"testing"
)

type stubScanner struct {
	vals []any
	err  error
}

func (s stubScanner) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	if len(dest) != len(s.vals) {
		return fmt.Errorf("scan dest=%d vals=%d", len(dest), len(s.vals))
	}
	for i, v := range s.vals {
		ptr, ok := dest[i].(*any)
		if !ok {
			return fmt.Errorf("dest[%d] type %T want *any", i, dest[i])
		}
		*ptr = v
	}
	return nil
}

func TestScanUnwrapsBytesAndNulls(t *testing.T) {
	dest, err := scan(stubScanner{vals: []any{[]byte("r1"), sql.NullString{Valid: false}, int64(1)}}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(dest) != 3 {
		t.Fatalf("len=%d", len(dest))
	}
	if dest[0] != "r1" {
		t.Fatalf("bytes unwrap got %#v", dest[0])
	}
	if dest[1] != nil {
		t.Fatalf("null unwrap got %#v", dest[1])
	}
	if dest[2] != int64(1) {
		t.Fatalf("int64 got %#v", dest[2])
	}
}

func TestScanPreservesErrNoRowsIdentity(t *testing.T) {
	_, err := scan(stubScanner{err: sql.ErrNoRows}, 1)
	if err != sql.ErrNoRows {
		t.Fatalf("err=%v want sql.ErrNoRows identity", err)
	}
}

func TestBizMapOmitsExtraSlots(t *testing.T) {
	vals := []any{"id-1", "t1", 0.9}
	got := bizMap(vals, []string{"id", "tenant_id"})
	if len(got) != 2 {
		t.Fatalf("map=%v", got)
	}
	if got["id"] != "id-1" || got["tenant_id"] != "t1" {
		t.Fatalf("map=%v", got)
	}
	if _, ok := got["of_score"]; ok {
		t.Fatalf("extra key leaked: %v", got)
	}
}
