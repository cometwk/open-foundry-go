package pack_test

import (
	"path/filepath"
	"testing"

	"github.com/openfoundry/runtime/pack"
)

func TestLoadAgentPack(t *testing.T) {
	root, err := pack.FindRepoRoot(".")
	if err != nil {
		root, err = pack.FindRepoRoot("..")
		if err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(root, "domain-packs", "agent")
	onto, err := pack.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if onto.Namespace == nil || onto.Namespace.Name != "example.agent" {
		t.Fatalf("namespace: %+v", onto.Namespace)
	}
	if len(onto.Objects) != 3 {
		t.Fatalf("objects=%d want 3", len(onto.Objects))
	}
	if len(onto.Links) != 3 {
		t.Fatalf("links=%d want 3", len(onto.Links))
	}
	got, err := pack.LoadMappings(dir, onto)
	if err != nil {
		t.Fatalf("LoadMappings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("mappings=%d want 1", len(got))
	}
	if n := len(got[0].Doc.Models); n != 3 {
		t.Fatalf("models=%d want 3", n)
	}
	if n := len(got[0].Doc.Links); n != 3 {
		t.Fatalf("links=%d want 3", n)
	}
	if !got[0].Doc.Links["OwnedBy"].Inline() {
		t.Fatal("OwnedBy should be inline")
	}
	if !got[0].Doc.Links["InChat"].Inline() {
		t.Fatal("InChat should be inline")
	}
	vote := got[0].Doc.Links["Vote"]
	if vote.Inline() || vote.Relation.Kind != "table" {
		t.Fatalf("Vote should be a junction: %+v", vote.Relation)
	}
	if vote.From.Object != "Chat" || vote.To.Object != "Message" {
		t.Fatalf("Vote ends = %s -> %s", vote.From.Object, vote.To.Object)
	}
}
