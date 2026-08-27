// Package action loads action YAML manifests and evaluates their
// CEL preconditions. Effects, side-effects, and rollback are parsed
// so unknown operational fields do not fail Unmarshal; they are not
// executed in this phase.
package action

import (
	"fmt"

	"github.com/openfoundry/runtime/ir"
	"gopkg.in/yaml.v3"
)

// Precondition is one CEL gate on an action manifest.
type Precondition struct {
	Expr  string `yaml:"expr"`
	Error string `yaml:"error"`
}

// Manifest is the evaluable subset of an action YAML file. Operational
// fields (effects, sideEffects, rollback, undo, reversible) decode so
// they cannot break load; evaluation ignores them.
type Manifest struct {
	Action        string         `yaml:"action"`
	Version       int            `yaml:"version"`
	Reversible    bool           `yaml:"reversible"`
	Preconditions []Precondition `yaml:"preconditions"`
	Effects       any            `yaml:"effects"`
	SideEffects   any            `yaml:"sideEffects"`
	Rollback      any            `yaml:"rollback"`
	Undo          any            `yaml:"undo"`
}

// Parse decodes an action YAML document. Unknown fields are ignored
// (yaml.v3 default) so sideEffects/rollback/reversible/undo never fail load.
func Parse(data []byte) (Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("action: parse yaml: %w", err)
	}
	if m.Action == "" {
		return Manifest{}, fmt.Errorf("action: missing action name")
	}
	return m, nil
}

// Bind checks that m.Action names an ActionType on onto. Binding
// failure is a hard error — the YAML is not an evaluable action.
func (m Manifest) Bind(onto *ir.Ontology) error {
	if onto == nil {
		return fmt.Errorf("action: ontology is nil")
	}
	if onto.ActionByName(m.Action) == nil {
		return fmt.Errorf("action: %q is not an ActionType in the ontology", m.Action)
	}
	return nil
}

// Lookup returns the manifest whose Action name matches, or nil.
func Lookup(manifests []Manifest, name string) *Manifest {
	for i := range manifests {
		if manifests[i].Action == name {
			return &manifests[i]
		}
	}
	return nil
}
