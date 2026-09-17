package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/openfoundry/runtime/pack"
)

func packDir(c *Conf) string {
	return filepath.Join(c.BaseDir, "domain-packs", c.DomainPacks)
}

// LoadPack loads ODL, OBDA mappings, and a compiled mapping from a pack directory.
func LoadPack(dir string) (*pack.Pack, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("domain-packs directory not found: %s", dir)
	}
	return pack.Load(dir)
}

func requireCompiled(p *pack.Pack) error {
	if p == nil || p.Compiled == nil || len(p.Mappings) == 0 {
		return fmt.Errorf("mappings=0")
	}
	return nil
}
