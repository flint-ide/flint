package codex

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Codex reads its whole configuration at start; a version that doesn't know
// Streamable HTTP MCP servers (an mcp_servers entry with a url) could refuse
// to start, breaking every chat. So Flint's server is offered only to a codex
// whose `codex mcp add --help` lists both --url and --bearer-token-env-var
// (the two settings Flint passes). The answer is cached per binary (path,
// size and modification time), so it is asked once per installed version.

const mcpProbeTimeout = 5 * time.Second

var mcpSupport = struct {
	sync.Mutex
	byBinary map[string]bool
}{byBinary: map[string]bool{}}

// supportsHTTPMCP is a variable for tests.
var supportsHTTPMCP = probeHTTPMCP

func probeHTTPMCP(path string, env []string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	key := fmt.Sprintf("%s\x00%d\x00%d", path, info.Size(), info.ModTime().UnixNano())
	mcpSupport.Lock()
	defer mcpSupport.Unlock()
	if ok, known := mcpSupport.byBinary[key]; known {
		return ok
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "mcp", "add", "--help")
	cmd.Env = env
	cmd.Dir = os.TempDir() // no project configuration
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	ok := err == nil && helpSupportsHTTPMCP(string(out))
	if len(mcpSupport.byBinary) >= 16 {
		clear(mcpSupport.byBinary) // a handful of binaries at most; stay bounded
	}
	mcpSupport.byBinary[key] = ok
	return ok
}

// helpSupportsHTTPMCP reads `codex mcp add --help`.
func helpSupportsHTTPMCP(help string) bool {
	return strings.Contains(help, "--url") && strings.Contains(help, "--bearer-token-env-var")
}
