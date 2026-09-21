package main

// import (
// 	"context"
// 	"fmt"
// 	"log"
// 	"testing"

// 	aisdk "github.com/grafana/ai-sdk"
// 	"github.com/grafana/ai-sdk/provider"
// 	openaiCompatible "github.com/grafana/ai-sdk/providers/openai-compatible"
// )

// func TestMain(t *testing.T) {
// 	ctx := context.Background()

// 	model := openaiCompatible.New(
// 		modelName,
// 		openaiCompatible.WithAPIKey(apiKey),
// 		openaiCompatible.WithBaseURL(baseURL),
// 	)
// 	result, err := aisdk.GenerateText(ctx, model,
// 		aisdk.WithModelMessages(provider.UserText("Explain goroutines in one sentence.")),
// 	)
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	fmt.Println(result.Text)
// }

// func TestMain2(t *testing.T) {

// }
