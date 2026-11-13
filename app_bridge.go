package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"easytty/internal/mcpserver"
	"easytty/internal/store"
)

// isBridgeMode reports "easyTTY.exe --mcp": no window, MCP over stdin/stdout,
// forwarded to the running easyTTY (for clients that start programs, like
// Claude Desktop).
func isBridgeMode() bool { return len(os.Args) > 1 && os.Args[1] == "--mcp" }

// runBridge returns the process exit code. URL and token come from
// EASYTTY_MCP_URL / EASYTTY_MCP_TOKEN, otherwise from easyTTY's settings.
func runBridge() int {
	url, token := os.Getenv("EASYTTY_MCP_URL"), os.Getenv("EASYTTY_MCP_TOKEN")
	if url == "" || token == "" {
		s, err := bridgeSettings()
		if err != nil {
			fmt.Fprintln(os.Stderr, "easyTTY --mcp:", err)
			return 1
		}
		if url == "" {
			url = fmt.Sprintf("http://127.0.0.1:%d%s", s.McpPort, mcpserver.Endpoint)
		}
		if token == "" {
			token = s.McpToken
		}
	}
	if token == "" {
		fmt.Fprintln(os.Stderr, "easyTTY --mcp: kein Token – in easyTTY unter Einstellungen → KI-Zugriff (MCP) einschalten")
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := mcpserver.Bridge(ctx, url, token, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "easyTTY --mcp:", err)
		return 1
	}
	return 0
}

// bridgeSettings reads settings.json from the same place the app uses.
func bridgeSettings() (store.Settings, error) {
	dir, _, err := dataDir()
	if err != nil {
		return store.Settings{}, err
	}
	return store.LoadSettings(filepath.Join(dir, "settings.json"))
}
