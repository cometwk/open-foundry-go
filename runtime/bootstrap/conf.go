package bootstrap

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	"github.com/kelseyhightower/envconfig"
	"github.com/openfoundry/runtime/internal/sqlopen"
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
	"github.com/openfoundry/runtime/storage/mysqlobda"
)

type Conf struct {
	// 基础目录
	BaseDir     string `envconfig:"BASE_DIR" required:"true"`
	DomainPacks string `envconfig:"DOMAIN_PACKS" required:"true"`
	// Ctx
	TenantID   string `envconfig:"TENANT_ID" required:"true"`
	SeedTenant string `envconfig:"SEED_TENANT"`
	// 数据库：mysql（SQL，需 DB_URL）或 memory（进程内，无 DB_URL）
	DBDriver         string `envconfig:"DB_DRIVER" default:"mysql"`
	DBURL            string `envconfig:"DB_URL"`
	DBMinConnections int    `envconfig:"DB_MIN_CONNECTIONS" default:"1"`
	DBMaxConnections int    `envconfig:"DB_MAX_CONNECTIONS" default:"10"`
	DBDebug          bool   `envconfig:"DB_DEBUG" default:"false"`
	DBMigrate        string `envconfig:"DB_MIGRATE" default:"./migrations"`
}

// BackendMemory is the in-process DB_DRIVER value. It has no SQL dialect,
// no DDL, and no DB_URL.
const BackendMemory = "memory"

func LoadConfig(configPath string) (*Conf, error) {
	if err := LoadEnv(configPath); err != nil {
		fmt.Println("加载 env 文件失败: ", err)
		return nil, err
	}

	var cfg Conf
	if err := envconfig.Process("", &cfg); err != nil {
		fmt.Println("解析 config 文件失败: ", err)
		return nil, err
	}

	cfg.BaseDir = expandHome(cfg.BaseDir)

	return &cfg, nil
}

///

// Bootstrap is dialect-neutral assembly input. mysql binds mysqlobda over a
// *sql.DB; memory binds the in-process provider with DB left nil.
type Bootstrap struct {
	Conf     *Conf
	DB       *sql.DB
	Ontology *ir.Ontology
	Mappings []pack.Mapping
	TenantID string
	SPI      spi.StorageProvider
	Schema   spi.OntologySchema
	Seeds    []SeedManifest
}

func Open(c *Conf) (*Bootstrap, error) {
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
	raw, err := mappingBytes(mappings)
	if err != nil {
		return nil, err
	}
	db, p, err := openBackend(c, raw)
	if err != nil {
		return nil, err
	}
	return &Bootstrap{
		Conf:     c,
		DB:       db,
		Ontology: onto,
		Mappings: mappings,
		TenantID: c.TenantID,
		SPI:      p,
		Schema:   schema,
		Seeds:    seeds,
	}, nil
}

// openBackend resolves DB_DRIVER to a provider. mysql opens a *sql.DB via
// DB_URL; memory constructs the in-process provider with no database.
func openBackend(c *Conf, raw []byte) (*sql.DB, spi.StorageProvider, error) {
	switch c.DBDriver {
	case SQLMySQL:
		if c.DBURL == "" {
			return nil, nil, fmt.Errorf("bootstrap: DB_URL required")
		}
		db, err := sqlopen.Open(SQLMySQL, c.DBURL)
		if err != nil {
			return nil, nil, err
		}
		p, err := mysqlobda.Open(db, raw, mysqlobda.Options{})
		if err != nil {
			_ = db.Close()
			return nil, nil, err
		}
		return db, p, nil
	case BackendMemory:
		return nil, memory.New(), nil
	default:
		return nil, nil, fmt.Errorf("bootstrap: unsupported driver %q", c.DBDriver)
	}
}

// Close releases the underlying database. The memory backend has none, so
// Close is always safe to call.
func (b *Bootstrap) Close() error {
	if b == nil || b.DB == nil {
		return nil
	}
	return b.DB.Close()
}

func (b *Bootstrap) ApplySchema() error {
	p, schema := b.SPI, b.Schema
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: b.Conf.TenantID}, schema); err != nil {
		return err
	}
	return nil
}
