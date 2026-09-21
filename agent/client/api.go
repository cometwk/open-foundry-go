package client

// Entity types, field fragments, and API resources mirror agent/client/api.ts.

type MessageRole string

const (
	MessageRoleUser      MessageRole = "USER"
	MessageRoleAssistant MessageRole = "ASSISTANT"
	MessageRoleSystem    MessageRole = "SYSTEM"
)

type Visibility string

const (
	VisibilityPrivate Visibility = "PRIVATE"
	VisibilityPublic  Visibility = "PUBLIC"
)

type Account struct {
	ID            string  `json:"id"`
	Email         string  `json:"email"`
	PasswordHash  *string `json:"passwordHash,omitempty"`
	Name          *string `json:"name,omitempty"`
	EmailVerified bool    `json:"emailVerified"`
	Image         *string `json:"image,omitempty"`
	IsAnonymous   bool    `json:"isAnonymous"`
	Chats         []Chat  `json:"chats,omitempty"`
}

type Chat struct {
	ID            string     `json:"id"`
	Title         string     `json:"title"`
	Visibility    Visibility `json:"visibility"`
	Owner         *Account   `json:"owner,omitempty"`
	Messages      []Message  `json:"messages,omitempty"`
	VotedMessages []Message  `json:"votedMessages,omitempty"`
}

type Message struct {
	ID          string      `json:"id"`
	Role        MessageRole `json:"role"`
	Parts       string      `json:"parts"`
	Attachments string      `json:"attachments"`
	Chat        *Chat       `json:"chat,omitempty"`
	VotedIn     []Chat      `json:"votedIn,omitempty"`
}

type MessageRoleFilter struct {
	Eq *MessageRole  `json:"eq,omitempty"`
	Ne *MessageRole  `json:"ne,omitempty"`
	In []MessageRole `json:"in,omitempty"`
}

type VisibilityFilter struct {
	Eq *Visibility  `json:"eq,omitempty"`
	Ne *Visibility  `json:"ne,omitempty"`
	In []Visibility `json:"in,omitempty"`
}

type AccountFilter struct {
	ID            *IDFilter       `json:"id,omitempty"`
	Email         *StringFilter   `json:"email,omitempty"`
	PasswordHash  *StringFilter   `json:"passwordHash,omitempty"`
	Name          *StringFilter   `json:"name,omitempty"`
	EmailVerified *BooleanFilter  `json:"emailVerified,omitempty"`
	Image         *StringFilter   `json:"image,omitempty"`
	IsAnonymous   *BooleanFilter  `json:"isAnonymous,omitempty"`
	AND           []AccountFilter `json:"AND,omitempty"`
	OR            []AccountFilter `json:"OR,omitempty"`
	NOT           *AccountFilter  `json:"NOT,omitempty"`
}

type AccountOrderBy struct {
	ID           *SortDirection `json:"id,omitempty"`
	Email        *SortDirection `json:"email,omitempty"`
	PasswordHash *SortDirection `json:"passwordHash,omitempty"`
	Name         *SortDirection `json:"name,omitempty"`
	Image        *SortDirection `json:"image,omitempty"`
}

type ChatFilter struct {
	ID         *IDFilter         `json:"id,omitempty"`
	Title      *StringFilter     `json:"title,omitempty"`
	Visibility *VisibilityFilter `json:"visibility,omitempty"`
	AND        []ChatFilter      `json:"AND,omitempty"`
	OR         []ChatFilter      `json:"OR,omitempty"`
	NOT        *ChatFilter       `json:"NOT,omitempty"`
}

type ChatOrderBy struct {
	ID         *SortDirection `json:"id,omitempty"`
	Title      *SortDirection `json:"title,omitempty"`
	Visibility *SortDirection `json:"visibility,omitempty"`
}

type MessageFilter struct {
	ID          *IDFilter          `json:"id,omitempty"`
	Role        *MessageRoleFilter `json:"role,omitempty"`
	Parts       *StringFilter      `json:"parts,omitempty"`
	Attachments *StringFilter      `json:"attachments,omitempty"`
	AND         []MessageFilter    `json:"AND,omitempty"`
	OR          []MessageFilter    `json:"OR,omitempty"`
	NOT         *MessageFilter     `json:"NOT,omitempty"`
}

type MessageOrderBy struct {
	ID          *SortDirection `json:"id,omitempty"`
	Role        *SortDirection `json:"role,omitempty"`
	Parts       *SortDirection `json:"parts,omitempty"`
	Attachments *SortDirection `json:"attachments,omitempty"`
}

const accountFields = `
  id
  email
  passwordHash
  name
  emailVerified
  image
  isAnonymous
`

const accountWithChatsFields = accountFields + `
  chats {
    id
    title
    visibility
  }
`

const chatFields = `
  id
  title
  visibility
`

const chatWithRelationsFields = chatFields + `
  owner {
    id
    email
    name
  }
  messages {
    id
    role
    parts
    attachments
  }
  votedMessages {
    id
    role
    parts
  }
`

const messageFields = `
  id
  role
  parts
  attachments
`

const messageWithRelationsFields = messageFields + `
  chat {
    id
    title
    visibility
  }
  votedIn {
    id
    title
  }
`

// APIs holds the three domain GraphQLResource instances (api.ts accountApi/chatApi/messageApi).
type APIs struct {
	Client  *Client
	Account *GraphQLResource[Account, AccountFilter, AccountOrderBy]
	Chat    *GraphQLResource[Chat, ChatFilter, ChatOrderBy]
	Message *GraphQLResource[Message, MessageFilter, MessageOrderBy]
}

// NewAPIs wires Account / Chat / Message resources on c (templ.ts createClient + GraphQLResource).
func NewAPIs(c *Client) *APIs {
	return &APIs{
		Client: c,
		Account: NewGraphQLResource[Account, AccountFilter, AccountOrderBy](c, ResourceNames{
			Single:    "account",
			List:      "accounts",
			Aggregate: "accountAggregate",
			Search:    "searchAccounts",
		}, accountWithChatsFields),
		Chat: NewGraphQLResource[Chat, ChatFilter, ChatOrderBy](c, ResourceNames{
			Single:    "chat",
			List:      "chats",
			Aggregate: "chatAggregate",
			Search:    "searchChats",
		}, chatWithRelationsFields),
		Message: NewGraphQLResource[Message, MessageFilter, MessageOrderBy](c, ResourceNames{
			Single:    "message",
			List:      "messages",
			Aggregate: "messageAggregate",
			Search:    "searchMessages",
		}, messageWithRelationsFields),
	}
}
