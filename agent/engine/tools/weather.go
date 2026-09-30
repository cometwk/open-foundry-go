package tools

import (
	"context"

	aisdk "github.com/grafana/ai-sdk"
)

type weatherInput struct {
	City string `json:"city" jsonschema:"description=City to look up the weather for"`
}

type weatherOutput struct {
	City       string `json:"city"`
	Celsius    int    `json:"celsius"`
	Conditions string `json:"conditions"`
}

func getWeather(_ context.Context, input weatherInput, _ aisdk.ToolExecutionOptions) (weatherOutput, error) {
	return weatherOutput{
		City:       input.City,
		Celsius:    18,
		Conditions: "partly cloudy",
	}, nil
}

func CreateWeatherTool(tctx ToolContext) (aisdk.Tool, error) {
	return aisdk.TypedTool(aisdk.TypedToolDef[weatherInput, weatherOutput]{
		Name:        "get_weather",
		Description: "Return deterministic sample weather data for a city.",
		Execute:     getWeather,
	})
}
