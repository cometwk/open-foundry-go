package ql

type Visibility string

const (
	VisibilityPrivate Visibility = "PRIVATE"
	VisibilityPublic  Visibility = "PUBLIC"
)

type MessageRole string

const (
	MessageRoleUser      MessageRole = "USER"
	MessageRoleAssistant MessageRole = "ASSISTANT"
	MessageRoleSystem    MessageRole = "SYSTEM"
)

type Account struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	PasswordHash  string `json:"passwordHash"`
	Name          string `json:"name"`
	EmailVerified bool   `json:"emailVerified"`
	Image         string `json:"image"`
	IsAnonymous   bool   `json:"isAnonymous"`
}

type Chat struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Visibility Visibility `json:"visibility"`
}

type Message struct {
	ID          string      `json:"id"`
	Role        MessageRole `json:"role"`
	Parts       string      `json:"parts"`
	Attachments string      `json:"attachments"`
}

type Vote struct {
	ID        string `json:"id"`
	IsUpvoted bool   `json:"isUpvoted"`
}
