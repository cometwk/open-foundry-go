package obda

import "testing"

func TestEncodeDirectRoundTrip(t *testing.T) {
	id := EncodeDirect("Patient", []string{"p1", "x"})
	typ, keys, err := DecodeDirect(id)
	if err != nil {
		t.Fatal(err)
	}
	if typ != "Patient" || len(keys) != 2 || keys[0] != "p1" {
		t.Fatalf("typ=%s keys=%v", typ, keys)
	}
	if _, _, err := DecodeDirect("not-base64"); err == nil {
		t.Fatal("expected decode failure")
	}
}

func TestEncodePhysicalKey(t *testing.T) {
	if got := EncodePhysicalKey([]any{"p1"}); got != "p1" {
		t.Fatalf("got=%q", got)
	}
	if got := EncodePhysicalKey([]any{"a", "b"}); got == "a" {
		t.Fatalf("composite should not collapse: %q", got)
	}
}
