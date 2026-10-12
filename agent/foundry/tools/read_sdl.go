package tools

import (
	"context"
	"path/filepath"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/lib/env"
	"github.com/openfoundry/runtime/pack"
	projgql "github.com/openfoundry/runtime/projection/graphql"
	"github.com/openfoundry/runtime/spi"
)

type readSdlInput struct{}

func readSdl(_ context.Context, input readSdlInput, _ aisdk.ToolExecutionOptions) (string, error) {
	baseDir := env.BaseDir()
	domainPacks := env.String("DOMAIN_PACKS", "domain-packs")
	onto, err := pack.LoadDir(filepath.Join(baseDir, "domain-packs", domainPacks))
	if err != nil {
		return "", err
	}
	return projgql.Generate(onto, spi.StorageCapabilities{
		SupportsTransactions:   true,
		SupportsFullTextSearch: false, // 不输出 searchXxx()
		SupportsGraphTraversal: true,
		MaxTraversalDepth:      8,
	}), nil
}

func CreateReadSdlTool(_ any) (aisdk.Tool, error) {
	return aisdk.TypedTool(aisdk.TypedToolDef[readSdlInput, string]{
		Name:        "read_sdl",
		Description: "Read the GraphQL SDL from the domain packs.",
		Execute:     readSdl,
	})
}
