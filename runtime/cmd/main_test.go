package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openfoundry/runtime/bootstrap"
)

func TestWriteDDL_File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.sql")
	if err := writeDDL([]string{"CREATE TABLE book (id TEXT);", "CREATE TABLE member (id TEXT);"}, path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "CREATE TABLE book (id TEXT);\nCREATE TABLE member (id TEXT);\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWriteDDL_EmptyStmts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.sql")
	if err := writeDDL(nil, path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %q, want empty file", got)
	}
}

func TestWriteDDL_CreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out", "nested", "schema.sql")
	if err := writeDDL([]string{"CREATE TABLE book (id TEXT);"}, path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "CREATE TABLE book (id TEXT);\n" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveOutputPath_UsesCWD(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	got, err := resolveOutputPath("1.sql")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "1.sql")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveOutputPath_FallsBackToBaseDir(t *testing.T) {
	root := t.TempDir()
	gone := filepath.Join(root, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	prev := conf
	conf = &bootstrap.Conf{BaseDir: root}
	t.Cleanup(func() { conf = prev })

	got, err := resolveOutputPath("1.sql")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "1.sql")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
