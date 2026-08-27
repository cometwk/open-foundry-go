package pack

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/openfoundry/runtime/action"
	"github.com/openfoundry/runtime/ir"
)

// LoadActions reads pack.yaml's actions: list (paths as declared, no glob),
// parses each YAML into an action.Manifest, and binds YAML action names to
// IR ActionType signatures. LoadDir is unchanged: it still returns only IR.
func LoadActions(packDir string, onto *ir.Ontology) ([]action.Manifest, error) {
	m, err := readManifest(packDir)
	if err != nil {
		return nil, err
	}
	if len(m.Actions) == 0 {
		return nil, fmt.Errorf("pack: %s has empty actions list", packDir)
	}
	out := make([]action.Manifest, 0, len(m.Actions))
	for _, rel := range m.Actions {
		path := filepath.Join(packDir, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("pack: read action %s: %w", rel, err)
		}
		am, err := action.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("pack: parse action %s: %w", rel, err)
		}
		if err := am.Bind(onto); err != nil {
			return nil, fmt.Errorf("pack: bind %s: %w", rel, err)
		}
		out = append(out, am)
	}
	return out, nil
}
