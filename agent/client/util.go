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
		case *aisdk.TextPart:
			textParts = append(textParts, part.Text)
		default:
			continue
		}
	}
	return strings.Join(textParts, "")
}

func ConvertToUIMessage(message Message) aisdk.UIMessage {
	var m aisdk.UIMessage
	err := json.Unmarshal([]byte(message.Parts), &m)
	if err != nil {
		return aisdk.UIMessage{}
	}
	return m
}

func ConvertToDBMessage(message aisdk.UIMessage) Message {
	parts, err := json.Marshal(message.Parts)
	if err != nil {
		return Message{}
	}
	return Message{
		ID:    message.ID,
		Role:  message.Role,
		Parts: parts,
	}
}
