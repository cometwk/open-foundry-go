package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/openfoundry/runtime/pack"
	projgql "github.com/openfoundry/runtime/projection/graphql"
	"github.com/openfoundry/runtime/spi"
)

func sdl(output string) error {
	body, err := generateSDL()
	if err != nil {
		slog.Error("print sdl failed", "error", err)
		return err
	}
	if err := writeSDL(body, output); err != nil {
		slog.Error("write sdl failed", "error", err, "output", output)
		return err
	}
	return nil
}

func generateSDL() (string, error) {
	if conf == nil {
		return "", fmt.Errorf("config required")
	}
	onto, err := pack.LoadDir(filepath.Join(conf.BaseDir, "domain-packs", conf.DomainPacks))
	if err != nil {
		return "", err
	}
	return projgql.Generate(onto, spi.StorageCapabilities{
		SupportsTransactions:   true,
		SupportsFullTextSearch: true,
		SupportsGraphTraversal: true,
		MaxTraversalDepth:      8,
	}), nil
}

func writeSDL(body, output string) error {
	if !strings.HasSuffix(body, "\n") && body != "" {
		body += "\n"
	}
	if output == "" {
		fmt.Print(body)
		return nil
	}
	path, err := resolveOutputPath(output)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}
