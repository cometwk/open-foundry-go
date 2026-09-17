package bootstrap

import (
	"database/sql"
	"fmt"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/spi"
)

// Bootstrap is dialect-neutral assembly input. mysql binds mysqlobda over a
// *sql.DB; memory binds the in-process provider with DB left nil.
type Bootstrap struct {
	Conf     *Conf
	Ontology *ir.Ontology
	Mappings []pack.Mapping
	TenantID string
	Schema   spi.OntologySchema
	Seeds    []SeedManifest

	SPI spi.StorageProvider
	DB  *sql.DB
}

func Open(c *Conf) (*Bootstrap, error) {
	return New(c)
}

// New 只处理Pack的加载，不处理 SPI 和 DB 的连接
func New(c *Conf) (*Bootstrap, error) {
	if c == nil {
		return nil, fmt.Errorf("bootstrap: conf required")
	}
	dir := packDir(c)
	onto, mappings, schema, err := LoadPack(dir)
	if err != nil {
		return nil, err
	}
	seeds, err := LoadPackSeeds(dir)
	if err != nil {
		return nil, err
	}
	if err := requireOneMapping(mappings); err != nil {
		return nil, err
	}

	// raw, err := mappingBytes(mappings)
	// if err != nil {
	// 	return nil, err
	// }

	// compiled, err := obda.Compile(schema, mappings[0].Doc)
	// if err != nil {
	// 	return nil, err
	// }

	return &Bootstrap{
		Conf: c,
		// DB:       db,
		Ontology: onto,
		Mappings: mappings,
		TenantID: c.TenantID,
		// SPI:      p,
		Schema: schema,
		Seeds:  seeds,
	}, nil
}

func (b *Bootstrap) ApplySchema() error {
	p, schema := b.SPI, b.Schema
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: b.Conf.TenantID}, schema); err != nil {
		return err
	}
	return nil
}

func (b *Bootstrap) ApplyStorageProvider() error {
	// 创建 StorageProvider
	raw, err := mappingBytes(b.Mappings)
	if err != nil {
		return err
	}

	db, sp, err := openBackend(b.Conf, raw)
	if err != nil {
		return err
	}
	b.DB = db
	b.SPI = sp

	if _, err := sp.ApplySchema(spi.RequestContext{TenantID: b.Conf.TenantID}, b.Schema); err != nil {
		return err
	}

	// // 创建 Engine
	// e, err := engine.NewWithCompiled(sp, b.Ontology, b.Schema)
	// if err != nil {
	// 	_ = b.Close()
	// 	slog.Error("engine.New failed", "error", err)
	// 	return nil, nil, err
	// }
	return nil
}
