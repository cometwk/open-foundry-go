package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/bootstrap"
)

func TestSDL_NilConf(t *testing.T) {
	prev := conf
	conf = nil
	t.Cleanup(func() { conf = prev })
	if err := sdl(""); err == nil || !strings.Contains(err.Error(), "config required") {
		t.Fatalf("err=%v, want config required", err)
	}
}

func TestSDL_WritesFile(t *testing.T) {
	base, _ := writeSeedFixture(t)
	prev := conf
	conf = &bootstrap.Conf{
		BaseDir:     base,
		DomainPacks: "fixture",
		DBDriver:    "sqlite",
	}
	t.Cleanup(func() { conf = prev })

	out := filepath.Join(t.TempDir(), "schema.graphql")
	if err := sdl(out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	for _, want := range []string{
		"type Widget {",
		"  widget(id: ID!): Widget",
		"  widgets(",
		"  widgetAggregate(",
		"  searchWidgets(",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("SDL missing %q\n%s", want, body)
		}
	}
}

func TestSDL_CreatesParentDir(t *testing.T) {
	base, _ := writeSeedFixture(t)
	prev := conf
	conf = &bootstrap.Conf{
		BaseDir:     base,
		DomainPacks: "fixture",
	}
	t.Cleanup(func() { conf = prev })

	out := filepath.Join(t.TempDir(), "out", "nested", "schema.graphql")
	if err := sdl(out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}
