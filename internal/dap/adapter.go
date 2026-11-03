package dap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// Adapter is a debug adapter program that speaks DAP on its standard input
// and output (an extension's debugger).
type Adapter struct {
	// Argv is the program, already resolved, and its arguments.
	Argv []string
	Env  []string
	Dir  string
	// ID is sent as the adapterID in initialize.
	ID string
}

// StartAdapter starts a, launches with args (the DAP launch request's
// arguments) and sets the breakpoints before the program runs.
func StartAdapter(ctx context.Context, a Adapter, args json.RawMessage, breakpoints map[string][]SourceBreakpoint, onEvent func(Event)) (*Session, error) {
	if len(a.Argv) == 0 {
		return nil, fmt.Errorf("no debug adapter program")
	}
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	// Plain pipes rather than cmd.StdinPipe/StdoutPipe: Wait must not close
	// the read end while the read loop still uses it.
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_, _ = inR.Close(), inW.Close()
		return nil, err
	}
	s := newSession()
	cmd := exec.Command(a.Argv[0], a.Argv[1:]...)
	cmd.Dir, cmd.Env = a.Dir, a.Env
	cmd.Stdin, cmd.Stdout = inR, outW
	err = s.startProcess(cmd)
	_, _ = inR.Close(), outW.Close() // the adapter has its own copies
	if err != nil {
		_, _ = inW.Close(), outR.Close()
		return nil, err
	}
	s.closeIO = func() { _, _ = inW.Close(), outR.Close() }
	s.connect(outR, inW, onEvent)

	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	if err := s.launch(ctx, a.ID, args, breakpoints); err != nil {
		s.kill()
		return nil, fmt.Errorf("%w%s", err, s.stderrTail())
	}
	return s, nil
}
