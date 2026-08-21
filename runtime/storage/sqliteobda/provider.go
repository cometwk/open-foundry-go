package sqliteobda

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/openfoundry/runtime/obda"
	sqlitedialect "github.com/openfoundry/runtime/obda/dialect/sqlite"
	"github.com/openfoundry/runtime/obda/sqlast"
	"github.com/openfoundry/runtime/spi"
)

var _ spi.StorageProvider = (*Provider)(nil)

// Options resolve mapping dsnRef names. Plaintext DSNs never live in YAML.
type Options struct {
	DSNRefs map[string]string
}

// DBTX is *sql.DB or *sql.Tx. Helpers must not fall back to p.db while a Tx is open.
type DBTX interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Provider is the SQLite OBDA StorageProvider.
type Provider struct {
	spi.UnimplementedStorageProvider
	db      *sql.DB
	doc     *obda.Document
	dialect *sqlitedialect.Dialect

	mu         sync.Mutex
	active     *activation
	failClosed bool
}

type activation struct {
	schema      spi.OntologySchema
	compiled    *obda.Compiled
	version     int
	fingerprint string
}

// Open parses mapping, binds the SQLite dialect, and prepares PRAGMAs.
func Open(db *sql.DB, mapping []byte, opts Options) (*Provider, error) {
	doc, err := obda.Parse(mapping)
	if err != nil {
		return nil, err
	}
	if err := obda.Validate(doc); err != nil {
		return nil, err
	}
	for name, src := range doc.Sources {
		if src.Dialect != "" && src.Dialect != "sqlite" {
			return nil, fmt.Errorf("%w: dialect %q on source %q", spi.ErrInvalidMapping, src.Dialect, name)
		}
		if src.Connection.DSNRef != "" && opts.DSNRefs != nil {
			if _, ok := opts.DSNRefs[src.Connection.DSNRef]; !ok {
				return nil, fmt.Errorf("%w: unresolved dsnRef %q", spi.ErrInvalidMapping, src.Connection.DSNRef)
			}
		}
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return nil, err
	}
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	d := sqlitedialect.New()
	if ok, err := sqlitedialect.ProbeFTS5(context.Background(), db); err == nil {
		d.SetFTS5(ok)
	}
	return &Provider{db: db, doc: doc, dialect: d}, nil
}

func (p *Provider) ApplySchema(ctx spi.RequestContext, schema spi.OntologySchema) (spi.MigrationResult, error) {
	if ctx.TenantID == "" {
		return spi.MigrationResult{}, spi.ErrTenantRequired
	}
	compiled, err := obda.Compile(schema, p.doc)
	if err != nil {
		return spi.MigrationResult{}, err
	}
	fp, err := p.fingerprint()
	if err != nil {
		return spi.MigrationResult{}, err
	}
	stmts, err := sqlitedialect.SidecarStatements("of_")
	if err != nil {
		return spi.MigrationResult{}, err
	}
	for _, s := range stmts {
		if _, err := p.db.Exec(s); err != nil {
			return spi.MigrationResult{}, err
		}
	}
	if err := p.backfill(p.db, compiled); err != nil {
		return spi.MigrationResult{}, err
	}
	snap, err := json.Marshal(schema)
	if err != nil {
		return spi.MigrationResult{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	from := 0
	if p.active != nil {
		from = p.active.version
	}
	to := schema.Version
	if to == 0 {
		to = from + 1
	}
	if _, err := p.db.Exec(`INSERT OR REPLACE INTO of_schema_versions (version, snapshot) VALUES (?, ?)`, to, string(snap)); err != nil {
		return spi.MigrationResult{}, err
	}
	mapBytes, _ := json.Marshal(p.doc.Metadata)
	if _, err := p.db.Exec(`INSERT OR REPLACE INTO of_mapping_versions (version, document, dsn_ref) VALUES (?, ?, ?)`, to, string(mapBytes), dsnRefName(p.doc)); err != nil {
		return spi.MigrationResult{}, err
	}
	res, err := p.db.Exec(`INSERT INTO of_mapping_activation (id, mapping_version, schema_version, fingerprint) VALUES (1, ?, ?, ?)`, to, to, fp)
	if err != nil {
		res, err = p.db.Exec(`UPDATE of_mapping_activation SET mapping_version = ?, schema_version = ?, fingerprint = ? WHERE id = 1 AND mapping_version = ?`, to, to, fp, from)
		if err != nil {
			return spi.MigrationResult{}, err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return spi.MigrationResult{}, fmt.Errorf("%w: activation cas lost", spi.ErrMappingNotActive)
		}
	}
	p.active = &activation{schema: schema, compiled: compiled, version: to, fingerprint: fp}
	p.failClosed = false
	return spi.MigrationResult{Success: true, FromVersion: from, ToVersion: to, AppliedAt: time.Now().UTC()}, nil
}

func (p *Provider) GetSchema(ctx spi.RequestContext, version *int) (spi.OntologySchema, error) {
	act, err := p.pin(ctx)
	if err != nil {
		return spi.OntologySchema{}, err
	}
	if version != nil && *version != act.version {
		var snap string
		err := p.db.QueryRow(`SELECT snapshot FROM of_schema_versions WHERE version = ?`, *version).Scan(&snap)
		if err != nil {
			return spi.OntologySchema{}, spi.ErrMappingNotActive
		}
		var s spi.OntologySchema
		if err := json.Unmarshal([]byte(snap), &s); err != nil {
			return spi.OntologySchema{}, err
		}
		return s, nil
	}
	return act.schema, nil
}

func (p *Provider) CreateObject(ctx spi.RequestContext, typ string, properties map[string]any) (spi.OntologyObject, error) {
	if _, err := p.pin(ctx); err != nil {
		return nil, err
	}
	return p.UnimplementedStorageProvider.CreateObject(ctx, typ, properties)
}

func (p *Provider) GetObject(ctx spi.RequestContext, typ, id string) (spi.OntologyObject, error) {
	if _, err := p.pin(ctx); err != nil {
		return nil, err
	}
	return p.UnimplementedStorageProvider.GetObject(ctx, typ, id)
}

func (p *Provider) HealthCheck() (spi.HealthStatus, error) {
	start := time.Now()
	err := p.db.Ping()
	p.mu.Lock()
	active := p.active != nil
	fp := ""
	if p.active != nil {
		fp = p.active.fingerprint
	}
	p.mu.Unlock()
	st := spi.HealthStatus{
		Healthy:   err == nil,
		Provider:  "sqliteobda",
		LatencyMs: int(time.Since(start).Milliseconds()),
		Details:   map[string]any{"active": active},
	}
	if err != nil {
		st.Healthy = false
		st.Details["error"] = "ping failed"
		return st, nil
	}
	if active {
		live, ferr := p.fingerprint()
		if ferr != nil || live != fp {
			p.mu.Lock()
			p.failClosed = true
			p.mu.Unlock()
			st.Healthy = false
			st.Details["drift"] = true
		}
	}
	return st, nil
}

func (p *Provider) Capabilities() spi.StorageCapabilities {
	caps := p.dialect.Capabilities()
	return spi.StorageCapabilities{
		SupportsTransactions:    caps.Transactions,
		SupportsTemporalQueries: true,
		SupportsFullTextSearch:  caps.FullTextSearch,
		SupportsGraphTraversal:  true,
		SupportsBulkMutations:   true,
		MaxTraversalDepth:       8,
		ReplicationSupport:      spi.ReplicationNone,
	}
}

func (p *Provider) pin(ctx spi.RequestContext) (*activation, error) {
	if ctx.TenantID == "" {
		return nil, spi.ErrTenantRequired
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failClosed {
		return nil, spi.ErrSourceSchemaDrift
	}
	if p.active == nil {
		return nil, spi.ErrMappingNotActive
	}
	return p.active, nil
}

func (p *Provider) fingerprint() (string, error) {
	h := ""
	for _, m := range p.doc.Models {
		snap, err := sqlitedialect.InspectTable(context.Background(), p.db, sqlast.Identifier{Name: m.Relation.Name})
		if err != nil {
			return "", err
		}
		h += snap.Hash
	}
	return h, nil
}

func dsnRefName(doc *obda.Document) string {
	for _, s := range doc.Sources {
		return s.Connection.DSNRef
	}
	return ""
}
