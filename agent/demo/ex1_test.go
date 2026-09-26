package demo

import (
	"context"
	"testing"

	aisdk "github.com/grafana/ai-sdk"
	"github.com/grafana/ai-sdk/output"
	"github.com/grafana/ai-sdk/provider"
	"github.com/grafana/ai-sdk/schema"
	"github.com/openfoundry/agent/internal/llm"
	"github.com/openfoundry/lib/env"
	"github.com/openfoundry/lib/testutil"
	"github.com/stretchr/testify/require"
)

func initEnv(t *testing.T) {
	err := env.LoadEnv("")
	require.NoError(t, err)
}

type AlertTriage struct {
	Severity  string `json:"severity" jsonschema:"enum=critical,enum=warning,enum=info"`
	RootCause string `json:"rootCause" jsonschema:"description=Likely root cause in one sentence"`
}

func TestEx1_Object(t *testing.T) {
	initEnv(t)
	ctx := context.Background()
	model := llm.NewDefaultModel()

	t.Run("tool", func(t *testing.T) {
		type WeatherInput struct {
			City string `json:"city" jsonschema:"description=City to look up"`
		}

		type WeatherOutput struct {
			TemperatureC int `json:"temperatureC"`
		}

		weather, err := aisdk.TypedTool(aisdk.TypedToolDef[WeatherInput, WeatherOutput]{
			Name:        "get_weather",
			Description: "Get the current weather for a city.",
			Execute: func(ctx context.Context, input WeatherInput, _ aisdk.ToolExecutionOptions) (WeatherOutput, error) {
				return WeatherOutput{
					TemperatureC: 18,
				}, nil
			},
		})
		require.NoError(t, err)

		result, err := aisdk.GenerateText(ctx, model,
			aisdk.WithModelMessages(provider.UserText("What is the weather in Paris?")),
			aisdk.WithTools(aisdk.ToolSet{"get_weather": weather}),
			aisdk.WithStopWhen(aisdk.StepCountIs(5)),
		)
		require.NoError(t, err)
		testutil.PrintPretty(result)
	})

	t.Run("json", func(t *testing.T) {
		schemaValue, err := schema.SchemaFor[AlertTriage]()
		require.NoError(t, err)

		objectOutput, err := output.Object[AlertTriage](schemaValue)
		require.NoError(t, err)
		testutil.PrintPretty(objectOutput)
		testutil.PrintPretty(string(schemaValue.JSON()))
	})

	t.Run("Object", func(t *testing.T) {

		schemaValue, err := schema.SchemaFor[AlertTriage]()
		require.NoError(t, err)

		objectOutput, err := output.Object[AlertTriage](schemaValue)
		require.NoError(t, err)

		testutil.PrintPretty(objectOutput)

		ctx := context.Background()
		model := llm.NewObjectModel()
		const alertText = `FIRING: HighErrorRate on payments-api
Error rate exceeded 5% threshold (current: 12.3%).
Labels: service=payments-api, env=production, region=us-east-1
Recent deploy: payments-api v2.4.1 rolled out 8 minutes ago.
Downstream: checkout-web reporting elevated 502s.`
		result, err := output.GenerateObject[AlertTriage](ctx, model, objectOutput,
			// aisdk.WithSystem("Triage the alert into the required structure."),
			aisdk.WithSystem("Triage the alert into the required JSON structure."),
			aisdk.WithModelMessages(provider.UserText(alertText)),
		)

		require.NoError(t, err)

		triage, err := result.Object()
		require.NoError(t, err)

		testutil.PrintPretty(triage)
	})
}
