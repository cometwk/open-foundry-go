package obda

import (
	"fmt"
	"strings"

	"github.com/openfoundry/runtime/spi"
	"gopkg.in/yaml.v3"
)

var secretKeys = map[string]struct{}{
	"dsn":      {},
	"password": {},
	"uri":      {},
	"url":      {},
	"token":    {},
	"secret":   {},
	"user":     {},
}

// Parse unmarshals a *.obda.yaml document, rejects plaintext credential keys,
// and rejects removed mapping keys (top-level sources, binding sourceRef).
func Parse(data []byte) (*Document, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%w: yaml: %v", spi.ErrInvalidMapping, err)
	}
	if err := rejectSecretKeys(&root); err != nil {
		return nil, err
	}
	if err := rejectRemovedKeys(&root); err != nil {
		return nil, err
	}
	var doc Document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%w: yaml: %v", spi.ErrInvalidMapping, err)
	}
	return &doc, nil
}

func rejectSecretKeys(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			if key.Kind == yaml.ScalarNode {
				if _, banned := secretKeys[strings.ToLower(key.Value)]; banned {
					return fmt.Errorf("%w: plaintext credential field %q", spi.ErrInvalidMapping, key.Value)
				}
			}
			if err := rejectSecretKeys(n.Content[i+1]); err != nil {
				return err
			}
		}
		return nil
	}
	for _, c := range n.Content {
		if err := rejectSecretKeys(c); err != nil {
			return err
		}
	}
	return nil
}

func rejectRemovedKeys(n *yaml.Node) error {
	doc := documentMapping(n)
	if doc == nil {
		return nil
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		key, val := doc.Content[i], doc.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			continue
		}
		switch key.Value {
		case "sources":
			return fmt.Errorf("%w: sources has been removed; DSN is injected at provider.Open", spi.ErrInvalidMapping)
		case "models", "links":
			if err := rejectSourceRefInBindings(val); err != nil {
				return err
			}
		}
	}
	return nil
}

func documentMapping(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		n = n.Content[0]
	}
	if n.Kind == yaml.MappingNode {
		return n
	}
	return nil
}

func rejectSourceRefInBindings(n *yaml.Node) error {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		binding := n.Content[i+1]
		if binding.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(binding.Content); j += 2 {
			k := binding.Content[j]
			if k.Kind == yaml.ScalarNode && k.Value == "sourceRef" {
				return fmt.Errorf("%w: sourceRef has been removed", spi.ErrInvalidMapping)
			}
		}
	}
	return nil
}
