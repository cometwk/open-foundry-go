package mysqlobda

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"time"

	// Registers github.com/go-sql-driver/mysql with database/sql. The
	// provider itself only speaks database/sql and the driver's error text.
	_ "github.com/go-sql-driver/mysql"

	"github.com/openfoundry/runtime/obda"
	mysqldialect "github.com/openfoundry/runtime/obda/dialect/mysql"
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

// Provider is the MySQL OBDA StorageProvider. The DSN handed to sql.Open
// must select a database; introspection resolves tables within DATABASE().
type Provider struct {
	spi.UnimplementedStorageProvider
	db      *sql.DB
	doc     *obda.Document
	dialect *mysqldialect.Dialect

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

// Open parses mapping, binds the MySQL dialect, and verifies connectivity.
func Open(db *sql.DB, mapping []byte, opts Options) (*Provider, error) {
	doc, err := obda.Parse(mapping)
	if err != nil {
		return nil, err
	}
	if err := obda.Validate(doc); err != nil {
		return nil, err
	}
	for name, src := range doc.Sources {
		if src.Dialect != "" && src.Dialect != "mysql" {
			return nil, fmt.Errorf("%w: dialect %q on source %q", spi.ErrInvalidMapping, src.Dialect, name)
		}
		if src.Connection.DSNRef != "" && opts.DSNRefs != nil {
			if _, ok := opts.DSNRefs[src.Connection.DSNRef]; !ok {
				return nil, fmt.Errorf("%w: unresolved dsnRef %q", spi.ErrInvalidMapping, src.Connection.DSNRef)
			}
		}
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &Provider{db: db, doc: doc, dialect: mysqldialect.New()}, nil
}

func (p *Provider) ApplySchema(ctx spi.RequestContext, schema spi.OntologySchema) (spi.MigrationResult, error) {
	if ctx.TenantID == "" {
		return spi.MigrationResult{}, spi.ErrTenantRequired
	}
	compiled, err := obda.Compile(schema, p.doc)
	if err != nil {
		return spi.MigrationResult{}, err
	}
	if err := p.verifyMappedSchema(compiled); err != nil {
		return spi.MigrationResult{}, err
	}
	fp, err := p.fingerprint()
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
		return spi.OntologySchema{}, spi.ErrMappingNotActive
	}
	return act.schema, nil
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
		Provider:  "mysqlobda",
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
		SupportsTemporalQueries: false,
		SupportsFullTextSearch:  caps.FullTextSearch,
		SupportsGraphTraversal:  true,
		SupportsBulkMutations:   false,
		MaxTraversalDepth:       8,
		ReplicationSupport:      spi.ReplicationNone,
	}
}

func (p *Provider) GetObjectAtVersion(spi.RequestContext, string, string, int) (spi.OntologyObject, error) {
	return nil, spi.ErrUnsupportedCapability
}

func (p *Provider) GetObjectAtTime(spi.RequestContext, string, string, time.Time) (spi.OntologyObject, error) {
	return nil, spi.ErrUnsupportedCapability
}

func (p *Provider) BulkMutate(spi.RequestContext, spi.BulkMutationRequest) (spi.BulkMutationResult, error) {
	return spi.BulkMutationResult{}, spi.ErrUnsupportedCapability
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
	names := make([]string, 0, len(p.doc.Models)+len(p.doc.Links))
	tables := map[string]string{}
	for name, m := range p.doc.Models {
		names = append(names, "m:"+name)
		tables["m:"+name] = m.Relation.Name
	}
	for name, l := range p.doc.Links {
		names = append(names, "l:"+name)
		tables["l:"+name] = l.Relation.Name
	}
	sort.Strings(names)
	h := ""
	for _, name := range names {
		snap, err := mysqldialect.InspectTable(context.Background(), p.db, sqlast.Identifier{Name: tables[name]})
		if err != nil {
			return "", err
		}
		h += snap.Hash
	}
	return h, nil
}
