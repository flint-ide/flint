package agenthost

import (
	"os/exec"
	"strings"
	"testing"
)

// The host runs without a GUI, in a process that must never load Wails: this
// keeps it true for the server and the client package.
func TestImportGraphHasNoWails(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool to list dependencies")
	}
	out, err := exec.Command(goTool, "list", "-deps", "-f", "{{.ImportPath}}", ".", "./client", "./hostmode").CombinedOutput()
	if err != nil {
		t.Skipf("go list failed: %v\n%s", err, out)
	}
	for _, pkg := range strings.Fields(string(out)) {
		if strings.Contains(pkg, "github.com/wailsapp") {
			t.Errorf("agenthost depends on %s", pkg)
		}
	}
}
