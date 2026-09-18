package bootstrap

import (
	"database/sql"
	"fmt"

	"github.com/kelseyhightower/envconfig"
	"github.com/openfoundry/lib/env"
	"github.com/openfoundry/runtime/internal/sqlopen"
	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/spi"
	"github.com/openfoundry/runtime/storage/memory"
	"github.com/openfoundry/runtime/storage/mysqlobda"

	_ "github.com/go-sql-driver/mysql"
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
	if err := env.LoadEnv(configPath); err != nil {
		fmt.Println("加载 env 文件失败: ", err)
		return nil, err
	}

	var cfg Conf
	if err := envconfig.Process("", &cfg); err != nil {
		fmt.Println("解析 config 文件失败: ", err)
		return nil, err
	}

	cfg.BaseDir = env.ExpandHome(cfg.BaseDir)

	return &cfg, nil
}

// openBackend resolves DB_DRIVER to a provider. mysql opens a *sql.DB via
// DB_URL; memory constructs the in-process provider with no database.
func (c *Conf) openBackend(compiled *obda.Compiled) (*sql.DB, spi.StorageProvider, error) {
	switch c.DBDriver {
	case SQLMySQL:
		if c.DBURL == "" {
			return nil, nil, fmt.Errorf("bootstrap: DB_URL required")
		}
		db, err := sqlopen.Open(SQLMySQL, c.DBURL)
		if err != nil {
			return nil, nil, err
		}
		p, err := mysqlobda.Open(db, compiled, mysqlobda.Options{})
		if err != nil {
			_ = db.Close()
			return nil, nil, err
		}
		if c.DBDebug {
			sqlopen.LogSQL = true
		}
		return db, p, nil
	case BackendMemory:
		return nil, memory.New(), nil
	default:
		return nil, nil, fmt.Errorf("bootstrap: unsupported driver %q", c.DBDriver)
	}
}
