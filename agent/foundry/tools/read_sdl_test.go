package tools

import (
	"context"
	"testing"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/stretchr/testify/require"
)

func TestReadSdl(t *testing.T) {
	tool, err := CreateReadSdlTool(nil)
	require.NoError(t, err)
	out, err := tool.Execute(context.Background(), []byte("{}"), aisdk.ToolExecutionOptions{})
	require.NoError(t, err)
	t.Logf("out: %s", string(out))
}
