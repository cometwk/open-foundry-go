package bootstrap

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/go-sql-driver/mysql"
	"github.com/kelseyhightower/envconfig"
	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/projection"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/sqliteobda"
	_ "modernc.org/sqlite"
)

type Conf struct {
	BaseDir     string `envconfig:"BASE_DIR" required:"true"`
	DomainPacks string `envconfig:"DOMAIN_PACKS" required:"true"`
	// 数据库
	DBDriver         string `envconfig:"DB_DRIVER" default:"mysql"`
	DBURL            string `envconfig:"DB_URL" required:"true"`
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
	DB       *sql.DB
	Ontology *ir.Ontology
	Mappings []pack.Mapping
	DSNRefs  map[string]string
	TenantID string
	SPI      spi.StorageProvider
	Schema   spi.OntologySchema
}

func Open(c *Conf) (*Bootstrap, error) {
	db, err := sql.Open(c.DBDriver, c.DBURL)
	if err != nil {
		return nil, err
	}

	dir := filepath.Join(c.BaseDir, "domain-packs", c.DomainPacks)
	st, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("domain-packs directory not found: %s", dir)
	}
	onto, err := pack.LoadDir(dir)
	if err != nil {
		return nil, err
	}
	mappings, err := pack.LoadMappings(dir, onto)
	if err != nil {
		return nil, err
	}
	if len(mappings) != 1 {
		return nil, fmt.Errorf("mappings=%d", len(mappings))
	}
	schema := projection.ProjectStorage(onto)
	// for _, m := range mappings {
	// 	compiled, err := obda.Compile(schema, m.Doc)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	if err := sqliteobda.InitMappedSchema(db, compiled); err != nil {
	// 		return nil, err
	// 	}
	// }

	raw, err := mappingBytes(mappings)
	if err != nil {
		return nil, err
	}
	refs := map[string]string{}
	p, err := sqliteobda.Open(db, raw, sqliteobda.Options{DSNRefs: refs})
	if err != nil {
		return nil, err
	}
	if _, err := p.ApplySchema(spi.RequestContext{TenantID: ""}, schema); err != nil {
		return nil, err
	}
	bootstrap := &Bootstrap{
		DB:       db,
		Ontology: onto,
		Mappings: mappings,
		DSNRefs:  refs,
		TenantID: "",
		SPI:      p,
	}
	return bootstrap, nil
}
