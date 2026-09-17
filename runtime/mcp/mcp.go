package mcp

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/server"
	"github.com/ThinkInAIXYZ/go-mcp/transport"
)

type MCP struct {
	server  *server.Server
	handler *transport.StreamableHTTPHandler
}

func (m *MCP) Handler() http.Handler {
	return m.handler.HandleMCP()
}

func NewMCP(ctx context.Context) (*MCP, error) {
	transport, handler, err := transport.NewStreamableHTTPServerTransportAndHandler()
	if err != nil {
		return nil, fmt.Errorf("create mcp transport and hander with error: %v", err)
	}

	mcpServer, err := server.NewServer(transport)
	if err != nil {
		return nil, fmt.Errorf("create mcp server with error: %v", err)
	}

	// Optional: Global middleware (variadic - multiple middleware supported)
	// mcpServer.Use(
	// 	LoggingMiddleware,
	// 	AuthMiddleware,
	// 	MetricsMiddleware,
	// )

	// 工具
	tool, err := protocol.NewTool("current_time", "Get current time for specified timezone", TimeRequest{})
	if err != nil {
		log.Fatalf("Failed to create tool: %v", err)
		return nil, err
	}
	mcpServer.RegisterTool(tool, handleTimeRequest)

	// 提示词
	testPrompt := &protocol.Prompt{
		Name:        "test_prompt",
		Description: "test_prompt_description",
		Arguments: []*protocol.PromptArgument{
			{
				Name:        "params1",
				Description: "params1's description",
				Required:    true,
			},
		},
	}

	mcpServer.RegisterPrompt(testPrompt, func(ctx context.Context, req *protocol.GetPromptRequest) (*protocol.GetPromptResult, error) {
		return &protocol.GetPromptResult{
			Description: "test_prompt_description",
			Messages: []*protocol.PromptMessage{
				{
					Role: protocol.RoleUser,
					Content: &protocol.TextContent{
						Type: "text",
						Text: "Hello, world: arg1=" + req.Arguments["params1"],
					},
				},
			},
		}, nil
	})
	// 注册提示词2
	testPrompt2 := &protocol.Prompt{
		Name:        "test_prompt2",
		Description: "test_prompt2_description",
		Arguments: []*protocol.PromptArgument{
			{
				Name:        "params2",
				Description: "params2's description",
				Required:    true,
			},
		},
	}
	testPrompt2GetResponse := &protocol.GetPromptResult{
		Description: "test_prompt2_description",
	}
	mcpServer.RegisterPrompt(testPrompt2, func(context.Context, *protocol.GetPromptRequest) (*protocol.GetPromptResult, error) {
		return testPrompt2GetResponse, nil
	})

	// defer mcpServer.Shutdown(ctx)

	return &MCP{
		server:  mcpServer,
		handler: handler,
	}, nil
}

func (m *MCP) Shutdown(ctx context.Context) error {
	return m.server.Shutdown(ctx)
}
