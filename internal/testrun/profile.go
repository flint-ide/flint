package testrun

import (
	"errors"
	"path/filepath"
	"slices"

	"flint/internal/runconfig"
)

// Profiles are the files a profiled test run writes.
type Profiles struct {
	CPU string `json:"cpu"`
	// Mem is "" when no memory profile was asked for.
	Mem string `json:"mem,omitempty"`
}

// PlanProfile plans a Go test run that writes a CPU profile (and, with mem, a
// memory profile) into dir. go test profiles one package at a time. The test
// binary goes to dir as well; profiling flags otherwise leave it in the
// package directory.
func PlanProfile(req Request, dir string, mem bool) (Plan, Profiles, error) {
	if req.Lang != LangGo {
		return Plan{}, Profiles{}, errors.New("profiling is supported for Go tests")
	}
	if len(req.Dirs) != 1 {
		return Plan{}, Profiles{}, errors.New("go test profiles one package at a time")
	}
	if !filepath.IsAbs(dir) {
		return Plan{}, Profiles{}, errors.New("profile directory must be absolute")
	}
	plan, err := PlanRun(req)
	if err != nil {
		return Plan{}, Profiles{}, err
	}
	out := Profiles{CPU: filepath.Join(dir, "cpu.pprof")}
	flags := []string{"-cpuprofile", out.CPU}
	if mem {
		out.Mem = filepath.Join(dir, "mem.pprof")
		flags = append(flags, "-memprofile", out.Mem)
	}
	flags = append(flags, "-o", filepath.Join(dir, "test.bin"))

	step := plan.Steps[0]
	at := slices.Index(step.Args, "-json") + 1 // planGo always passes "test", "-json"
	args := slices.Concat(step.Args[:at], flags, step.Args[at:])
	plan.Steps = []runconfig.Command{runconfig.NewCommand(step.Program, args...)}
	plan.Title = "Profile " + plan.Title
	return plan, out, nil
}
