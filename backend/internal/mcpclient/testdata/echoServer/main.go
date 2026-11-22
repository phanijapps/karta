// Command echo-mcp-server is a test-only MCP stdio server: it exposes
// `count` tools (tool_0000…) over stdin/stdout using the official SDK,
// giving the probe tests a real spawned binary to handshake with.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	count := 2
	if len(os.Args) > 1 {
		if n, err := strconv.Atoi(os.Args[1]); err == nil {
			count = n
		}
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "echo-mcp-server", Version: "0"}, nil)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("tool_%04d", i)
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: "test tool " + name},
			func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{}, nil, nil
			})
	}
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
