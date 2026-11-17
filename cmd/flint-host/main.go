// Command flint-host is the headless Flint for a machine you reach over ssh
// (docs/design/agent-runtime.md, "Phase 4"): the background session host and
// its ssh relay, with no window, no Wails and no cgo, so one static binary
// builds for linux and darwin on amd64 and arm64.
//
//	flint --version                 the version and the host protocol version
//	flint --agent-host --stdio      what Flint runs over ssh: relay to this machine's host
//	flint --agent-host              the host itself (the relay starts it when needed)
//
// It hosts terminals only. Agent drivers live in Flint's main package, so
// agent calls fail with "agents are not available on this host".
//
// The release archive names the binary "flint", which is what a machine's
// "command" setting defaults to.
package main

import (
	"fmt"
	"os"

	"flint/internal/agenthost/hostmode"
	"flint/internal/diag"
)

// version is set by the build: -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	hostmode.Main(hostmode.Options{Version: diag.Version(version), NoAgents: true}) // exits for its flags
	fmt.Fprintln(os.Stderr, "flint (headless host): run it with --version, --agent-host or --agent-host --stdio")
	os.Exit(2)
}
