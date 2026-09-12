package bootstrap_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openfoundry/runtime/bootstrap"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
	"github.com/openfoundry/runtime/storage/mysqlobda"
)

func TestOpen_MemoryUnactivated(t *testing.T) {
	c := widgetOpenConf(t, "memory")
	b, err := bootstrap.Open(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	if b.DB != nil {
		t.Fatalf("memory backend must not open a database, got %T", b.DB)
	}
	if _, ok := b.SPI.(*memory.Provider); !ok {
		t.Fatalf("SPI=%T", b.SPI)
	}
	if b.Conf == nil || b.Conf.TenantID != "t1" {
		t.Fatalf("Conf=%+v", b.Conf)
	}
	// The memory provider surfaces an un-activated mapping as not-found
	// (mysqlobda uses ErrMappingNotActive; both fail closed).
	_, err = b.SPI.GetObject(spi.RequestContext{TenantID: "t1"}, "Widget", "x")
	if !errors.Is(err, spi.ErrObjectNotFound) {
		t.Fatalf("err=%v want ErrObjectNotFound", err)
	}
}

func TestOpen_MySQLEmptyURL(t *testing.T) {
	c := widgetOpenConf(t, "mysql")
	c.DBURL = ""
	if _, err := bootstrap.Open(c); err == nil || !strings.Contains(err.Error(), "DB_URL") {
		t.Fatalf("err=%v", err)
	}
}

func TestOpen_UnknownDriver(t *testing.T) {
	// sqlite died with the sqliteobda provider; it must fail like any other
	// unknown driver instead of silently falling back.
	for _, driver := range []string{"sqlite", "sqlite3", "postgres"} {
		c := widgetOpenConf(t, driver)
		if _, err := bootstrap.Open(c); err == nil {
			t.Fatalf("driver %q: want unsupported driver", driver)
		}
	}
}

func TestOpen_TwoMappings(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "domain-packs", "fixture")
	for rel, content := range map[string]string{
		"pack.yaml":             "name: fixture\nnamespace: test.pack\nschema:\n  - schema/models.odl\nobda:\n  - obda/widget.obda.yaml\n  - obda/gadget.obda.yaml\n",
		"schema/models.odl":     widgetODL,
		"obda/widget.obda.yaml": modelMapping("Widget", "widget"),
		"obda/gadget.obda.yaml": modelMapping("Gadget", "gadget"),
	} {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &bootstrap.Conf{
		BaseDir:     base,
		DomainPacks: "fixture",
		TenantID:    "t1",
		DBDriver:    "memory",
	}
	if _, err := bootstrap.Open(c); err == nil || !strings.Contains(err.Error(), "mappings=2") {
		t.Fatalf("err=%v", err)
	}
}

func TestOpen_MySQL(t *testing.T) {
	dsn := os.Getenv("TEST_DB_URL")
	if dsn == "" {
		t.Skip("TEST_DB_URL unset")
	}
	c := widgetOpenConf(t, "mysql")
	c.DBURL = dsn
	b, err := bootstrap.Open(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	if _, ok := b.SPI.(*mysqlobda.Provider); !ok {
		t.Fatalf("SPI=%T", b.SPI)
	}
	_, err = b.SPI.GetObject(spi.RequestContext{TenantID: "t1"}, "Widget", "x")
	if !errors.Is(err, spi.ErrMappingNotActive) {
		t.Fatalf("err=%v want ErrMappingNotActive", err)
	}
}

func widgetOpenConf(t *testing.T, driver string) *bootstrap.Conf {
	t.Helper()
	base := t.TempDir()
	writePackInto(t, base, "fixture")
	return &bootstrap.Conf{
		BaseDir:     base,
		DomainPacks: "fixture",
		TenantID:    "t1",
		DBDriver:    driver,
		DBURL:       filepath.Join(t.TempDir(), "t.db"),
	}
}
