package manifest

import (
	"fmt"
	"regexp"
	"strings"
)

// run.env: variables of the user's environment an extension needs to work
// like the tools it drives (KUBECONFIG, DOCKER_HOST, AWS_CONFIG_FILE, …).
// Flint passes their values from its environment (which has the login
// shell's) when it starts the program; the install dialog lists them.

const maxRunEnv = 30

var runEnvName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// notPassed are variables Flint sets itself (or keeps minimal) and never
// copies on request.
var notPassed = map[string]bool{"PATH": true, "HOME": true, "SHELL": true, "USER": true, "LOGNAME": true, "TMPDIR": true, "LD_PRELOAD": true, "LD_LIBRARY_PATH": true, "DYLD_INSERT_LIBRARIES": true, "DYLD_LIBRARY_PATH": true}

func checkRunEnv(names []string) error {
	if len(names) > maxRunEnv {
		return fmt.Errorf("run.env: at most %d variables", maxRunEnv)
	}
	seen := map[string]bool{}
	for _, n := range names {
		switch {
		case !runEnvName.MatchString(n):
			return fmt.Errorf("run.env: %q is not a variable name (upper-case letters, digits, underscores)", truncate(n, 40))
		case notPassed[n] || strings.HasPrefix(n, "FLINT_"):
			return fmt.Errorf("run.env: %s is set by Flint and can't be requested", n)
		case seen[n]:
			return fmt.Errorf("run.env: %s is listed twice", n)
		}
		seen[n] = true
	}
	return nil
}
