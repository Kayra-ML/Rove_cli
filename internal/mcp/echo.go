package mcp

import (
	"bufio"
	"encoding/json"
	"os"
)

// echo server used by tests: reads JSON-RPC lines and replies.
func runEcho() {
	in := bufio.NewScanner(os.Stdin)
	w := bufio.NewWriter(os.Stdout)
	for in.Scan() {
		var req map[string]any
		if err := json.Unmarshal(in.Bytes(), &req); err != nil {
			continue
		}
		method, _ := req["method"].(string)
		id := req["id"]
		var result any
		switch method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "echo"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "ping", "description": "pong", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "pong"}}}
		default:
			result = map[string]any{}
		}
		if id == nil {
			continue
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
		b, _ := json.Marshal(resp)
		w.Write(b)
		w.WriteByte('\n')
		w.Flush()
	}
}
