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

// Parse unmarshals a *.obda.yaml document and rejects plaintext credential keys.
func Parse(data []byte) (*Document, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%w: yaml: %v", spi.ErrInvalidMapping, err)
	}
	if err := rejectSecretKeys(&root); err != nil {
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
