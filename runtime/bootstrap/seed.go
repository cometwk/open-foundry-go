package bootstrap

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/openfoundry/runtime/engine"
	"github.com/openfoundry/runtime/pack"
	"github.com/openfoundry/runtime/spi"
	"gopkg.in/yaml.v3"
)

const (
	// DefaultSeedTenant is used when SEED_TENANT is unset. It is isolated
	// from ordinary request tenants — API reads in another tenant will not
	// see seeded rows.
	DefaultSeedTenant = "system"
	// SeedActorID is the boot actor recorded on seed writes.
	SeedActorID = "boot"
)

// SeedObject is a single seed object to create at bootstrap.
type SeedObject struct {
	Type   string
	Ref    string
	Fields map[string]any
}

// SeedLink is a single seed link to create at bootstrap.
type SeedLink struct {
	Type   string
	From   string
	To     string
	Fields map[string]any
}

// SeedManifest is one pack.yaml seed: file after parse.
type SeedManifest struct {
	PackName string
	Objects  []SeedObject
	Links    []SeedLink
}

// SeedResult counts objects and links written or skipped during ApplySeeds.
type SeedResult struct {
	CreatedObjects int
	CreatedLinks   int
	SkippedObjects int
}

// SeedContext is the boot RequestContext for seed writes.
// Blank seedTenant is treated as unset (compose/Helm pass "").
func SeedContext(seedTenant string) spi.RequestContext {
	tenant := strings.TrimSpace(seedTenant)
	if tenant == "" {
		tenant = DefaultSeedTenant
	}
	return spi.RequestContext{TenantID: tenant, ActorID: SeedActorID}
}

// LoadPackSeeds reads pack.yaml's seed: list and parses each YAML file.
// Missing or invalid files are skipped with a warning, matching the
// TypeScript schema loader. An omitted seed: key returns (nil, nil).
func LoadPackSeeds(packDir string) ([]SeedManifest, error) {
	m, err := pack.ReadManifest(packDir)
	if err != nil {
		return nil, err
	}
	if len(m.Seed) == 0 {
		return nil, nil
	}
	var seeds []SeedManifest
	for _, rel := range m.Seed {
		path := filepath.Join(packDir, rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				slog.Warn("Schema loader: seed file not found, skipping", "file", rel, "packDir", packDir)
				continue
			}
			return nil, fmt.Errorf("bootstrap: read seed %s: %w", rel, err)
		}
		manifest, ok := parseSeedFile(m.Name, rel, raw)
		if !ok {
			continue
		}
		seeds = append(seeds, manifest)
	}
	return seeds, nil
}

func parseSeedFile(packName, rel string, raw []byte) (SeedManifest, bool) {
	var parsed any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		slog.Warn("Schema loader: seed file is not valid YAML, skipping", "file", rel, "error", err)
		return SeedManifest{}, false
	}
	root, ok := asObjectMap(parsed)
	if !ok {
		slog.Warn("Schema loader: seed file is not a valid YAML object, skipping", "file", rel)
		return SeedManifest{}, false
	}

	objects := parseSeedObjects(root["objects"])
	links := parseSeedLinks(root["links"])
	if len(objects) == 0 && len(links) == 0 {
		return SeedManifest{}, false
	}
	return SeedManifest{PackName: packName, Objects: objects, Links: links}, true
}

func parseSeedObjects(raw any) []SeedObject {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]SeedObject, 0, len(items))
	for _, item := range items {
		o, ok := asObjectMap(item)
		if !ok {
			continue
		}
		typ, ok := o["type"].(string)
		if !ok {
			continue
		}
		obj := SeedObject{Type: typ, Fields: map[string]any{}}
		if ref, ok := o["ref"].(string); ok {
			obj.Ref = ref
		}
		if fields, ok := asObjectMap(o["fields"]); ok {
			obj.Fields = fields
		}
		out = append(out, obj)
	}
	return out
}

func parseSeedLinks(raw any) []SeedLink {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]SeedLink, 0, len(items))
	for _, item := range items {
		l, ok := asObjectMap(item)
		if !ok {
			continue
		}
		typ, ok := l["type"].(string)
		if !ok {
			continue
		}
		from, ok := l["from"].(string)
		if !ok {
			continue
		}
		to, ok := l["to"].(string)
		if !ok {
			continue
		}
		lnk := SeedLink{Type: typ, From: from, To: to}
		if fields, ok := asObjectMap(l["fields"]); ok {
			lnk.Fields = fields
		}
		out = append(out, lnk)
	}
	return out
}

func asObjectMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				continue
			}
			out[ks] = val
		}
		return out, true
	default:
		return nil, false
	}
}

// ApplySeeds creates seed objects then links through the Engine.
// Existing objects (looked up by name, then title) are skipped and their
// IDs still resolve refs. Duplicate links are skipped. Per-row failures
// are logged and do not abort the rest of the batch.
func ApplySeeds(eng *engine.Engine, seeds []SeedManifest, ctx spi.RequestContext) (SeedResult, error) {
	if eng == nil {
		return SeedResult{}, fmt.Errorf("bootstrap: engine required")
	}
	var result SeedResult
	refMap := map[string]string{}
	for _, seed := range seeds {
		for _, obj := range seed.Objects {
			if id, ok := findExistingSeedObject(eng, ctx, obj); ok {
				if obj.Ref != "" {
					refMap[obj.Ref] = id
				}
				result.SkippedObjects++
				continue
			}
			created, err := eng.CreateObject(ctx, obj.Type, obj.Fields)
			if err != nil {
				slog.Warn("Seed: failed to create object", "type", obj.Type, "pack", seed.PackName, "error", err)
				continue
			}
			id, _ := created[spi.FieldID].(string)
			if obj.Ref != "" && id != "" {
				refMap[obj.Ref] = id
			}
			slog.Info("Seed: created object", "type", obj.Type, "id", id, "ref", obj.Ref, "pack", seed.PackName)
			result.CreatedObjects++
		}
		for _, lnk := range seed.Links {
			fromID := resolveSeedRef(refMap, lnk.From)
			toID := resolveSeedRef(refMap, lnk.To)
			_, err := eng.CreateLink(ctx, lnk.Type, fromID, toID, lnk.Fields)
			if err != nil {
				if isDuplicateLink(err) {
					slog.Info("Seed: link already exists, skipping", "type", lnk.Type, "from", fromID, "to", toID)
					continue
				}
				slog.Warn("Seed: failed to create link", "type", lnk.Type, "pack", seed.PackName, "error", err)
				continue
			}
			result.CreatedLinks++
		}
	}
	if result.CreatedObjects > 0 || result.CreatedLinks > 0 || result.SkippedObjects > 0 {
		slog.Info("Seed: applied",
			"objects", result.CreatedObjects,
			"links", result.CreatedLinks,
			"skipped", result.SkippedObjects,
			"tenant", ctx.TenantID,
		)
	}
	return result, nil
}

// ApplySeeds writes loaded pack seeds through a new Engine over b.SPI.
func (b *Bootstrap) ApplySeeds() (SeedResult, error) {
	if b == nil || b.SPI == nil || b.Ontology == nil {
		return SeedResult{}, fmt.Errorf("bootstrap: not open")
	}
	eng, err := engine.New(b.SPI, b.Ontology)
	if err != nil {
		return SeedResult{}, err
	}
	tenant := ""
	if b.Conf != nil {
		tenant = b.Conf.SeedTenant
	}
	return ApplySeeds(eng, b.Seeds, SeedContext(tenant))
}

func findExistingSeedObject(eng *engine.Engine, ctx spi.RequestContext, obj SeedObject) (string, bool) {
	field, value, ok := seedLookup(obj.Fields)
	if !ok {
		return "", false
	}
	page, err := eng.QueryObjects(ctx, obj.Type, spi.FilterExpression{
		Field: field, Operator: "eq", Value: value,
	}, &spi.QueryOptions{Limit: 1})
	if err != nil || len(page.Items) == 0 {
		return "", false
	}
	id, _ := page.Items[0][spi.FieldID].(string)
	if id == "" {
		return "", false
	}
	return id, true
}

func seedLookup(fields map[string]any) (field, value string, ok bool) {
	for _, name := range []string{"name", "title"} {
		v, ok := fields[name].(string)
		if ok && v != "" {
			return name, v, true
		}
	}
	return "", "", false
}

func resolveSeedRef(refMap map[string]string, ref string) string {
	if id, ok := refMap[ref]; ok {
		return id
	}
	return ref
}

func isDuplicateLink(err error) bool {
	if errors.Is(err, spi.ErrCardinalityViolation) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "duplicate")
}
