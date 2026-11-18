package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// CloneOptions are a clone of a repository whose URL came from elsewhere (a
// backlog in someone else's repository).
type CloneOptions struct {
	// URL is passed after "--", never as an option.
	URL string
	// Dir is the new checkout: an empty folder or one that does not exist,
	// in an existing parent.
	Dir string
	// Protocols are the transports git may use ("https", "ssh", "file");
	// every other one (ext, fd, git, http …) is switched off.
	Protocols []string
	// Progress gets git's progress lines (may be nil).
	Progress func(line string)
}

// cloneProtocols are the transports a clone may allow.
var cloneProtocols = []string{"https", "ssh", "file"}

// maxCloneMessage bounds the end of git's output kept for an error.
const maxCloneMessage = 2000

// Clone runs git clone for a URL Flint does not control: only the given
// transports (protocol.allow=never otherwise, ext and fd always off), no
// submodules, no hooks (core.hooksPath=/dev/null, so neither the user's
// template hooks nor anything else runs), no fsmonitor, and nothing asked on
// a terminal (GIT_TERMINAL_PROMPT=0; credentials come from the user's helper
// or SSH agent). ctx cancels it (git is killed). What git leaves behind on
// failure is the caller's to remove.
func Clone(ctx context.Context, o CloneOptions) error {
	if len(o.Protocols) == 0 {
		return errors.New("no transport allowed")
	}
	if o.URL == "" || strings.HasPrefix(o.URL, "-") {
		return fmt.Errorf("invalid URL %q", o.URL)
	}
	if !filepath.IsAbs(o.Dir) {
		return errors.New("the destination must be an absolute path")
	}
	args := slices.Clone(safeConfig)
	args = append(args, "-c", "protocol.allow=never")
	for _, p := range o.Protocols {
		if !slices.Contains(cloneProtocols, p) {
			return fmt.Errorf("transport %q is not allowed", p)
		}
		args = append(args, "-c", "protocol."+p+".allow=always")
	}
	args = append(args,
		"-c", "protocol.ext.allow=never",
		"-c", "protocol.fd.allow=never",
		"-c", "core.hooksPath=/dev/null",
		"-c", "submodule.recurse=false",
		"clone", "--progress", "--no-recurse-submodules", "--", o.URL, o.Dir)
	cmd := exec.CommandContext(ctx, gitBinary(), args...)
	cmd.Dir = filepath.Dir(o.Dir)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",     // no hidden password prompts: fail instead
		"SSH_ASKPASS_REQUIRE=never", // nor from ssh
		"GIT_PROTOCOL_FROM_USER=0",
		"GIT_EDITOR=true",
		"LC_ALL=C",
	)
	cmd.WaitDelay = 5 * time.Second // ssh may keep stderr open after git was killed
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	tail := readProgress(stderr, o.Progress)
	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		msg := strings.TrimSpace(tail)
		if msg == "" {
			msg = err.Error()
		}
		return &Error{Args: []string{"clone"}, Msg: msg, Err: err}
	}
	return nil
}

// readProgress passes git's stderr on line by line (progress lines end in
// \r) and returns the last lines that are not progress, for an error.
func readProgress(r io.Reader, progress func(string)) string {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), 64<<10)
	sc.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	var tail string
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if progress != nil {
			progress(line)
		}
		if isProgress(line) {
			continue
		}
		tail += line + "\n"
		if len(tail) > maxCloneMessage {
			tail = tail[len(tail)-maxCloneMessage:]
		}
	}
	_, _ = io.Copy(io.Discard, r)
	return strings.ToValidUTF8(tail, "")
}

// isProgress reports one of git's progress lines.
func isProgress(line string) bool {
	for _, p := range []string{"Cloning into", "remote: Enumerating", "remote: Counting", "remote: Compressing", "remote: Total", "Receiving objects", "Resolving deltas", "Updating files", "Checking out files", "Unpacking objects"} {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}
