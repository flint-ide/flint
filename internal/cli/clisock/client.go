package clisock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"

	"flint/internal/agenthost/hostmode"
	"flint/internal/cli"
	"flint/internal/config"
	"flint/internal/jsonrpc"
)

// NotRunning is what the client says when no Flint answers.
const NotRunning = "flint: Flint is not running: start it first (flint <folder>)"

// dialTimeout bounds connecting; the command itself may take as long as it
// takes (a native question, run wait).
const dialTimeout = 5 * time.Second

// Main runs the command line and exits when the arguments ask for a command
// (cli.IsCommand); otherwise it returns at once and Flint starts as usual.
// main calls it before anything of the window exists, so a command opens no
// window and shows no Dock icon.
func Main() {
	args := os.Args[1:]
	if msg := joined(args); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(cli.ExitUsage)
	}
	if !cli.IsCommand(args) {
		return
	}
	os.Exit(Run(context.Background(), args, os.Stdin, os.Stdout, os.Stderr))
}

// joined is cli.JoinedCommand unless a folder of that name exists (it
// opens as usual).
func joined(args []string) string {
	msg := cli.JoinedCommand(args)
	if msg == "" {
		return ""
	}
	if fi, err := os.Stat(args[0]); err == nil && fi.IsDir() {
		return ""
	}
	return msg
}

// Run sends one command line to the running Flint of this configuration
// directory and prints its answer; it returns the exit status.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintf(stderr, "flint: %v\n", err)
		return cli.ExitFailed
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "flint: the current directory: %v\n", err)
		return cli.ExitFailed
	}
	return RunIn(ctx, PathsFor(dir), args, cwd, stdin, stdout, stderr)
}

// RunIn is Run with the files and the working directory given (tests).
func RunIn(ctx context.Context, p Paths, args []string, cwd string, stdin io.Reader, stdout, stderr io.Writer) int {
	args, files, err := readFiles(args, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "flint: %v\n", err)
		if cli.IsUsage(err) {
			return cli.ExitUsage
		}
		return cli.ExitFailed
	}
	resp, err := call(ctx, p, cli.Request{Args: args, Cwd: cwd, Files: files})
	if err != nil {
		fmt.Fprintln(stderr, clientError(err))
		return cli.ExitFailed
	}
	_, _ = io.WriteString(stdout, resp.Stdout)
	_, _ = io.WriteString(stderr, resp.Stderr)
	return resp.Exit
}

// errNotRunning: no socket, no token, nobody listening.
var errNotRunning = errors.New("not running")

func clientError(err error) string {
	if errors.Is(err, errNotRunning) {
		return NotRunning
	}
	return "flint: " + err.Error()
}

func call(ctx context.Context, p Paths, req cli.Request) (cli.Response, error) {
	token, err := hostmode.ReadToken(p.Token)
	if errors.Is(err, hostmode.ErrNotRunning) {
		return cli.Response{}, errNotRunning
	}
	if err != nil {
		return cli.Response{}, err
	}
	req.Token = token
	d := net.Dialer{Timeout: dialTimeout}
	nc, err := d.DialContext(ctx, "unix", p.Sock)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT) {
			return cli.Response{}, errNotRunning
		}
		return cli.Response{}, err
	}
	defer nc.Close()
	conn := jsonrpc.New(nc, jsonrpc.Options{MaxMessage: cli.MaxMessage})
	go func() { _ = conn.Serve(nc) }()
	defer conn.Close(nil)
	var resp cli.Response
	if err := conn.Call(ctx, cli.Method, req, &resp); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, jsonrpc.ErrClosed) {
			return cli.Response{}, errors.New("Flint closed the connection (it may be quitting)")
		}
		return cli.Response{}, err
	}
	return resp, nil
}

var fileFlag = regexp.MustCompile(`^--([a-z][a-z0-9-]{0,23})-file(=.*)?$`)

// readFiles takes the --<flag>-file options out of args and reads them: a
// regular file (followed through links, as any program of the user would)
// or "-" for standard input, at most cli.MaxText bytes each. Flint gets the
// text, never the path. Options after "--" are left alone.
func readFiles(args []string, stdin io.Reader) ([]string, map[string]string, error) {
	var out []string
	files := map[string]string{}
	stdinUsed := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			out = append(out, args[i:]...)
			break
		}
		m := fileFlag.FindStringSubmatch(a)
		if m == nil {
			out = append(out, a)
			continue
		}
		name, path := m[1], strings.TrimPrefix(m[2], "=")
		if m[2] == "" {
			if i+1 >= len(args) {
				return nil, nil, cli.Usagef("%s needs a file (or - for standard input)", a)
			}
			i++
			path = args[i]
		}
		if _, dup := files[name]; dup {
			return nil, nil, cli.Usagef("--%s-file is given twice", name)
		}
		if len(files) == cli.MaxFiles {
			return nil, nil, cli.Usagef("at most %d --…-file options", cli.MaxFiles)
		}
		var text string
		var err error
		if path == "-" {
			if stdinUsed {
				return nil, nil, cli.Usagef("standard input can be read once")
			}
			stdinUsed = true
			text, err = readBounded(stdin, "standard input")
		} else {
			text, err = readFile(path)
		}
		if err != nil {
			return nil, nil, err
		}
		files[name] = text
	}
	if len(files) == 0 {
		files = nil
	}
	return out, files, nil
}

func readFile(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	if fi.Size() > cli.MaxText {
		return "", fmt.Errorf("%s is larger than %d bytes", path, cli.MaxText)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return readBounded(f, path)
}

func readBounded(r io.Reader, what string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("%s is not available", what)
	}
	data, err := io.ReadAll(io.LimitReader(r, cli.MaxText+1))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", what, err)
	}
	if len(data) > cli.MaxText {
		return "", fmt.Errorf("%s is larger than %d bytes", what, cli.MaxText)
	}
	return string(data), nil
}
