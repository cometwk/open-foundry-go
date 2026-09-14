package query

import (
	"fmt"
	"testing"
)

func TestIntermediateProject(t *testing.T) {
	ont := navOntology()
	paths := [][]string{{"b", "c"}}
	got := IntermediateProject(ont, "A", paths, []string{"name", "c", "c.name"})
	if fmt.Sprint(got["B"]) != "[name]" {
		t.Fatalf("selected B = %v, want [name]", got["B"])
	}
	if _, ok := got["C"]; ok {
		t.Fatalf("terminal C must not be in Project: %v", got)
	}
	skel := IntermediateProject(ont, "A", paths, nil)
	if skel == nil {
		t.Fatal("REST/nil names still records intermediates")
	}
	if fields, ok := skel["B"]; !ok || len(fields) != 0 {
		t.Fatalf("skeleton B = %v", skel)
	}
	if IntermediateProject(ont, "A", [][]string{{"leaf"}}, []string{"name"}) != nil {
		t.Fatal("1-hop path has no intermediate")
	}
}
