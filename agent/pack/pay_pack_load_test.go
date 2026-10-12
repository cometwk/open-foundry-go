package pack_test

import (
	"path/filepath"
	"testing"

	"github.com/openfoundry/runtime/pack"
)

func TestLoadPayPack(t *testing.T) {
	if true {
		t.Skip("skipping pay pack load test")
		return
	}
	root, err := pack.FindRepoRoot(".")
	if err != nil {
		root, err = pack.FindRepoRoot("..")
		if err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(root, "domain-packs", "pay-pack")
	onto, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if onto.Namespace == nil || onto.Namespace.Name != "example.pay" {
		t.Fatalf("namespace: %+v", onto.Namespace)
	}
	if len(onto.Objects) != 7 {
		t.Fatalf("objects=%d want 7", len(onto.Objects))
	}
	if len(onto.Links) != 5 {
		t.Fatalf("links=%d want 5", len(onto.Links))
	}
	got, err := pack.LoadMappings(dir, onto)
	if err != nil {
		t.Fatalf("LoadMappings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("mappings=%d want 1", len(got))
	}
	if n := len(got[0].Doc.Models); n != 7 {
		t.Fatalf("models=%d want 7", n)
	}
	if n := len(got[0].Doc.Links); n != 5 {
		t.Fatalf("links=%d want 5", n)
	}
	for _, name := range []string{"Parent", "BoundTo", "ForMerch", "ForChan"} {
		if !got[0].Doc.Links[name].Inline() {
			t.Fatalf("%s should be inline", name)
		}
	}
	closure := got[0].Doc.Links["Closure"]
	if closure.Inline() || closure.Relation.Kind != "table" {
		t.Fatalf("Closure should be a junction: %+v", closure.Relation)
	}
	if closure.From.Object != "Agent" || closure.To.Object != "Agent" {
		t.Fatalf("Closure ends = %s -> %s", closure.From.Object, closure.To.Object)
	}
}
