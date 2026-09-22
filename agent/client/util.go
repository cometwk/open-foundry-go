package client

import (
	"encoding/json"
	"strings"

	aisdk "github.com/grafana/ai-sdk"
)

func GetTextFromMessage(message aisdk.UIMessage) string {
	var textParts []string
	for _, part := range message.Parts {
		switch part := part.(type) {
		case aisdk.TextPart:
			textParts = append(textParts, part.Text)
		case *aisdk.TextPart:
			textParts = append(textParts, part.Text)
		default:
			continue
		}
	}
	return strings.Join(textParts, "")
}

// ConvertToUIMessage maps a persisted Message (parts stored as JSON string) to aisdk.UIMessage.
func ConvertToUIMessage(message Message) aisdk.UIMessage {
	parts := message.Parts
	if parts == "" {
		parts = "[]"
	}
	payload, err := json.Marshal(struct {
		ID    string          `json:"id"`
		Role  MessageRole     `json:"role"`
		Parts json.RawMessage `json:"parts"`
	}{
		ID:    message.ID,
		Role:  message.Role,
		Parts: json.RawMessage(parts),
	})
	if err != nil {
		return aisdk.UIMessage{}
	}
	var ui aisdk.UIMessage
	if err := json.Unmarshal(payload, &ui); err != nil {
		return aisdk.UIMessage{}
	}
	return ui
}

// ConvertToDBMessage maps an aisdk.UIMessage to a persisted Message.
// Parts are encoded with the AI SDK wire format (including "type").
func ConvertToDBMessage(message aisdk.UIMessage) Message {
	normalized := message
	normalized.Parts = make([]aisdk.Part, len(message.Parts))
	for i, p := range message.Parts {
		normalized.Parts[i] = normalizePart(p)
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return Message{}
	}
	var extracted struct {
		ID    string          `json:"id"`
		Role  MessageRole     `json:"role"`
		Parts json.RawMessage `json:"parts"`
	}
	if err := json.Unmarshal(raw, &extracted); err != nil {
		return Message{}
	}
	return Message{
		ID:    extracted.ID,
		Role:  extracted.Role,
		Parts: string(extracted.Parts),
	}
}

// normalizePart converts pointer part values to the value types aisdk.MarshalJSON expects.
func normalizePart(p aisdk.Part) aisdk.Part {
	switch v := p.(type) {
	case *aisdk.TextPart:
		return *v
	case *aisdk.ReasoningPart:
		return *v
	case *aisdk.ToolInvocationPart:
		return *v
	case *aisdk.DynamicToolUIPart:
		return *v
	case *aisdk.FilePart:
		return *v
	case *aisdk.ReasoningFilePart:
		return *v
	case *aisdk.SourceURLPart:
		return *v
	case *aisdk.SourceDocumentPart:
		return *v
	case *aisdk.DataPart:
		return *v
	case *aisdk.StepStartPart:
		return *v
	default:
		return p
	}
}
