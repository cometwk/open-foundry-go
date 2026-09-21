package chat

import (
	"github.com/grafana/ai-sdk/provider"
)

// const (
// 	apiKey    = "sk-sp-H.DIIPMY.jhYy.MEUCIQDLWjxYMPRBMLSw8Q-6qdrmCqTKctSiEQaq9ULDhlHtcAIgYKsuzpfcK60VRpt8C2q3yFv0I1nBjEeuqnG7bSe4W6w"
// 	baseURL   = "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"
// 	modelName = "deepseek-v4.1-flash"
// )

func NewModel() provider.LanguageModel {
	// return openaiCompatible.New(
	// 	modelName,
	// 	openaiCompatible.WithAPIKey(apiKey),
	// 	openaiCompatible.WithBaseURL(baseURL),
	// )
	return NewDefaultModel()
}
