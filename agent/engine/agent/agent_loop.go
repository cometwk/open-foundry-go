package agent

import (
	aisdk "github.com/grafana/ai-sdk"
)

func NewAgentLoop(config AgentConfig) (*aisdk.ToolLoopAgent, error) {

	// model, err := llm.New(config.Model)
	// if err != nil {
	// 	return nil, err
	// }
	// agent := aisdk.NewToolLoopAgent(model,
	// 	aisdk.WithToolLoopAgentID("incident-assistant"),
	// 	aisdk.WithToolLoopAgentOptions(
	// 		aisdk.WithInstructions("Help operators investigate incidents."),
	// 		aisdk.WithTools(tools),
	// 		aisdk.WithStopWhen(aisdk.StepCountIs(8)),
	// 	),
	// )

	// result, err := agent.Generate(ctx,
	// 	aisdk.WithAgentPrompt("Investigate the payment API alert."),
	// )

	return nil, nil
}
