package bootstrap

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	"github.com/kelseyhightower/envconfig"
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/mysqlobda"
	"github.com/openfoundry/runtime/storage/sqliteobda"
	_ "modernc.org/sqlite"
)

type Conf struct {
	// 基础目录
	BaseDir     string `envconfig:"BASE_DIR" required:"true"`
	DomainPacks string `envconfig:"DOMAIN_PACKS" required:"true"`
	// Ctx
	TenantID   string `envconfig:"TENANT_ID" required:"true"`
	SeedTenant string `envconfig:"SEED_TENANT"`
	// 数据库
	DBDriver         string `envconfig:"DB_DRIVER" default:"mysql"`
	DBURL            string `envconfig:"DB_URL"`
	DBMinConnections int    `envconfig:"DB_MIN_CONNECTIONS" default:"1"`
	DBMaxConnections int    `envconfig:"DB_MAX_CONNECTIONS" default:"10"`
	DBDebug          bool   `envconfig:"DB_DEBUG" default:"false"`
	DBMigrate        string `envconfig:"DB_MIGRATE" default:"./migrations"`
}

type Dialect string

func (s *Dialect) Decode(value string) error {
	*s = Dialect(value)
	return nil
}

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

// Bootstrap is dialect-neutral assembly input. OpenSQLite binds SQLite;
// a future MySQL entry point can share this shape without changing SPI.
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
	driver, err := SQLName(c.DBDriver)
	if err != nil {
		return nil, err
	}
	if c.DBURL == "" {
		return nil, fmt.Errorf("bootstrap: DB_URL required")
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
	db, err := sql.Open(driver, c.DBURL)
	if err != nil {
		return nil, err
	}
	p, err := openProvider(driver, db, raw)
	if err != nil {
		_ = db.Close()
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

func openProvider(driver string, db *sql.DB, raw []byte) (spi.StorageProvider, error) {
	switch driver {
	case SQLMySQL:
		return mysqlobda.Open(db, raw, mysqlobda.Options{})
	default:
		return sqliteobda.Open(db, raw, sqliteobda.Options{})
	}
}

func (b *Bootstrap) ApplySchema() error {
	p, schema := b.SPI, b.Schema
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: b.Conf.TenantID}, schema); err != nil {
		return err
	}
	return nil
}
