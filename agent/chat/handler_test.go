package chat

import (
	"context"
	"testing"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/openfoundry/agent/internal/llm"
	"github.com/openfoundry/lib/testutil"
	"github.com/stretchr/testify/require"
)

func Test1(t *testing.T) {
	ctx := context.Background()
	model := llm.NewFlashModel()

	agent, err := NewAgent(model)
	require.NoError(t, err)

	result, err := agent.Generate(ctx,
		aisdk.WithAgentPrompt("成都天气怎么样"),
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	t.Logf("result: %s", testutil.Pretty(result))
}

func Test2(t *testing.T) {
	ctx := context.Background()
	model := llm.NewFlashModel()

	agent, err := NewAgent(model)
	require.NoError(t, err)

	messages := []aisdk.UIMessage{
		sampleUIMessage("msg-1", "成都天气怎么样"),
	}
	stream, err := aisdk.CreateAgentUIStream(ctx, agent, messages)
	require.NoError(t, err)
	aisdk.PipeUIMessageStreamToResponse(nil, stream)

	// if err := aisdk.WriteAgentUIStream(
	// 	w,
	// 	r.Context(),
	// 	agent,
	// 	body.Messages,
	// ); err != nil {
	// 	log.Printf("streaming agent response: %v", err)
	// }

	// result, err := agent.Generate(ctx,
	// 	aisdk.WithAgentPrompt("成都天气怎么样"),
	// }
}

func sampleUIMessage(id, text string) aisdk.UIMessage {
	return aisdk.UIMessage{
		ID:   id,
		Role: aisdk.RoleUser,
		Parts: []aisdk.Part{
			aisdk.TextPart{Text: text},
		},
	}
}
