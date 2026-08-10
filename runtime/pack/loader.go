package pack

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/openfoundry/runtime/ir"
	"github.com/openfoundry/runtime/odl"
	"gopkg.in/yaml.v3"
)

// Manifest is the subset of pack.yaml needed for Phase 1 schema loading.
type Manifest struct {
	Name      string   `yaml:"name"`
	Namespace string   `yaml:"namespace"`
	Schema    []string `yaml:"schema"`
}

var namespaceExtendRe = regexp.MustCompile(`(?m)^extend\s+schema\s+@namespace\([^)]*\)\s*\n?`)

// LoadDir loads a domain pack directory by reading pack.yaml schema list,
// concatenating ODL with duplicate namespace stripping, then parse+lower+validate.
// It does not load dependency packs (e.g. core) or action YAML.
func LoadDir(packDir string) (*ir.Ontology, error) {
	manifestPath := filepath.Join(packDir, "pack.yaml")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("pack: read manifest: %w", err)
	}
	var m Manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("pack: parse manifest: %w", err)
	}
	if len(m.Schema) == 0 {
		return nil, fmt.Errorf("pack: %s has empty schema list", packDir)
	}

	sources := make([]odl.Source, 0, len(m.Schema))
	for i, rel := range m.Schema {
		path := filepath.Join(packDir, rel)
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("pack: read schema %s: %w", rel, err)
		}
		text := string(content)
		if i > 0 {
			text = namespaceExtendRe.ReplaceAllString(text, "")
		}
		sources = append(sources, odl.Source{Name: rel, Content: text})
	}

	onto, err := odl.ParseAndLower(sources...)
	if err != nil {
		return nil, err
	}
	if err := ir.Validate(onto); err != nil {
		return nil, fmt.Errorf("pack: validate: %w", err)
	}
	return onto, nil
}

// FindRepoRoot walks upward from start looking for a directory that contains domain-packs/.
func FindRepoRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, "domain-packs")); err == nil && st.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("pack: domain-packs not found from %s", start)
		}
		dir = parent
	}
}

// SupplyChainDir resolves domain-packs/supply-chain relative to the repo root.
func SupplyChainDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, err := FindRepoRoot(wd)
	if err != nil {
		// Also try from this file's module path convention: runtime/ -> repo root
		root, err = FindRepoRoot(filepath.Join(wd, ".."))
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(root, "domain-packs", "supply-chain"), nil
}

// StripNamespaceForTest exposes namespace stripping for tests.
func StripNamespaceForTest(s string) string {
	return strings.TrimSpace(namespaceExtendRe.ReplaceAllString(s, ""))
}
