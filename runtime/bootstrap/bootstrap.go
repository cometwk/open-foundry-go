package bootstrap

import (
	"database/sql"
	"fmt"

	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/spi"
)

// Bootstrap is dialect-neutral assembly input. mysql binds mysqlobda over a
// *sql.DB; memory binds the in-process provider with DB left nil.
type Bootstrap struct {
	Conf *Conf
	// TenantID string
	// Ontology *ir.Ontology
	// Mappings []pack.Mapping
	// Schema   spi.OntologySchema
	Seeds []SeedManifest
	Pack  *pack.Pack

	SPI spi.StorageProvider
	DB  *sql.DB
	// OBDA *obda.Compiled
	Engine *engine.Engine
}

func Open(c *Conf) (*Bootstrap, error) {
	return New(c)
}

// New 只处理 Pack 文件相关的加载，不处理 SPI 和 DB 的连接
func New(c *Conf) (*Bootstrap, error) {
	if c == nil {
		return nil, fmt.Errorf("bootstrap: conf required")
	}
	dir := packDir(c)
	pack, err := LoadPack(dir)
	if err != nil {
		return nil, err
	}
	if err := requireCompiled(pack); err != nil {
		return nil, err
	}
	seeds, err := loadPackSeeds(dir, pack.Manifest)
	if err != nil {
		return nil, err
	}

	return &Bootstrap{
		Conf: c,
		Pack: pack,
		// Ontology: p.Ontology,
		// Mappings: p.Mappings,
		// TenantID: c.TenantID,
		// Schema:   p.Schema,
		Seeds: seeds,
		// OBDA:     p.Compiled,
	}, nil
}

// // @deprecated
// func (b *Bootstrap) ApplySchema() error {
// 	p, schema := b.SPI, b.Schema
// 	if _, err := p.ApplySchema(spi.RequestContext{TenantID: b.Conf.TenantID}, schema); err != nil {
// 		return err
// 	}
// 	return nil
// }

func (b *Bootstrap) OpenEngine() (*engine.Engine, error) {
	if b.Engine != nil {
		return b.Engine, nil
	}

	//  创建 SPI
	db, sp, err := b.Conf.openBackend(b.Pack.Compiled)
	if err != nil {
		return nil, err
	}
	b.DB = db
	b.SPI = sp

	if _, err := sp.ApplySchema(spi.RequestContext{TenantID: b.Conf.TenantID}, b.Pack.Schema); err != nil {
		return nil, err
	}

	// 创建 Engine
	e, err := engine.NewWithCompiled(sp, b.Pack.Ontology, b.Pack.Compiled)
	if err != nil {
		return nil, err
	}
	b.Engine = e

	return e, nil
}

// Close releases the underlying database. The memory backend has none, so
// Close is always safe to call.
func (b *Bootstrap) Close() error {
	if b == nil || b.DB == nil {
		return nil
	}
	return b.DB.Close()
}

// ApplySeeds writes loaded pack seeds through a new Engine over b.SPI.
func (b *Bootstrap) ApplySeeds() (SeedResult, error) {
	if b == nil || b.Engine == nil {
		return SeedResult{}, fmt.Errorf("bootstrap: not open")
	}
	tenant := ""
	if b.Conf != nil {
		tenant = b.Conf.SeedTenant
	}
	return ApplySeeds(b.Engine, b.Seeds, SeedContext(tenant))
}
