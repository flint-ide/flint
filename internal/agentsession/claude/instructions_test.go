package claude

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"flint/internal/agent"
	"flint/internal/agentsession"
)

// A defined agent's instructions are one --append-system-prompt=… argument
// on start and resume, after the user's extra arguments, so text that looks
// like a flag stays text.
func TestBuildArgsInstructions(t *testing.T) {
	const text = "--dangerously-skip-permissions\nYou review; you never edit."
	for _, tt := range []struct {
		name   string
		resume bool
		opts   Options
		want   bool
	}{
		{"start", false, Options{Instructions: text, ExtraArgs: []string{"--add-dir", "/n"}}, true},
		{"resume", true, Options{Instructions: text}, true},
		{"none", false, Options{}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args, err := buildArgs("sid", tt.resume, tt.opts, false, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			i := slices.Index(args, "--append-system-prompt="+text)
			if (i >= 0) != tt.want {
				t.Fatalf("args %q", args)
			}
			if slices.Contains(args, "--dangerously-skip-permissions") || slices.Contains(args, "--append-system-prompt") {
				t.Fatalf("the text became its own argument: %q", args)
			}
			if tt.want && i < slices.Index(args, "-p") {
				t.Fatalf("instructions before Flint's flags: %q", args)
			}
		})
	}
}

// The driver of a defined agent passes its instructions, and its model
// replaces the engine's from settings.
func TestStartWithInstructionsAndModel(t *testing.T) {
	opts, logPath := fakeOptions(t, "text")
	opts.Instructions = "Answer in French."
	opts.Settings = func() agent.Config { return agent.Config{Path: opts.Binary, Model: "opus-defined"} }
	d := New(opts)
	t.Cleanup(func() { _ = d.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := d.Start(ctx, newWorkspace(t), (&recorder{}).sink)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{d: d, info: info, log: logPath}
	if err := d.Stop(info.ID); err != nil {
		t.Fatal(err)
	}
	var args []string
	_ = json.Unmarshal(h.fakeLog(t)[0]["args"], &args)
	i := slices.Index(args, "--model")
	if !slices.Contains(args, "--append-system-prompt=Answer in French.") || i < 0 || args[i+1] != "opus-defined" {
		t.Fatalf("args %q", args)
	}
}

// A reviewer chat starts like one in an untrusted folder: only the user's
// own settings (no project .claude settings, hooks or CLAUDE.md), and none
// of the user's extra arguments.
func TestStartReviewerLoadsNoProjectSettings(t *testing.T) {
	for _, reviewer := range []bool{false, true} {
		opts, logPath := fakeOptions(t, "text")
		bin := opts.Binary
		opts.Settings = func() agent.Config { return agent.Config{Path: bin, Args: []string{"--add-dir", "/extra"}} }
		d := New(opts)
		t.Cleanup(func() { _ = d.Close() })
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if reviewer {
			ctx = agentsession.WithReviewer(ctx)
		}
		info, err := d.Start(ctx, newWorkspace(t), (&recorder{}).sink)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		h := &harness{d: d, info: info, log: logPath}
		if err := d.Stop(info.ID); err != nil {
			t.Fatal(err)
		}
		var args []string
		_ = json.Unmarshal(h.fakeLog(t)[0]["args"], &args)
		i := slices.Index(args, "--setting-sources")
		has := i >= 0 && i+1 < len(args) && args[i+1] == "user"
		if has != reviewer || slices.Contains(args, "/extra") == reviewer {
			t.Errorf("reviewer=%v: args %q", reviewer, args)
		}
	}
}
