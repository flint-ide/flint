package diag

import (
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"flint/internal/redact"
)

// maxLogBytes: past this the log is moved to flint.log.1 (one generation kept).
const maxLogBytes = 5 << 20

// LogDir is where the system expects an app's logs: ~/Library/Logs/Flint on
// macOS (Console.app shows them), $XDG_STATE_HOME/flint (~/.local/state/flint)
// elsewhere.
func LogDir() string {
	return logDir(runtime.GOOS, os.Getenv("XDG_STATE_HOME"))
}

func logDir(goos, stateHome string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	if goos == "darwin" {
		return filepath.Join(home, "Library", "Logs", "Flint")
	}
	if !filepath.IsAbs(stateHome) { // the XDG spec ignores relative paths
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "flint")
}

// LogPath is the current log file.
func LogPath() string { return filepath.Join(LogDir(), "flint.log") }

// rotatingFile appends to path and starts over (keeping one old file) when it grows too big.
type rotatingFile struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

func openRotating(path string) (*rotatingFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	r := &rotatingFile{path: path}
	return r, r.open()
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, info.Size()
	return nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size+int64(len(p)) > maxLogBytes {
		r.f.Close()
		_ = os.Rename(r.path, r.path+".1")
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// Setup makes slog and the log package write to the log file (and stderr),
// redacting secrets, and returns a logger for Wails that keeps only warnings
// (it logs every asset request at info). A log that can't be opened leaves
// logging on stderr.
func Setup() (wails *slog.Logger) {
	var out io.Writer = os.Stderr
	if f, err := openRotating(LogPath()); err == nil {
		out = io.MultiWriter(os.Stderr, redactingWriter{f})
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelInfo}))) // also routes the log package
	log.SetFlags(0)
	return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// redactingWriter strips secrets before they reach the file.
type redactingWriter struct{ w io.Writer }

func (r redactingWriter) Write(p []byte) (int, error) {
	if _, err := r.w.Write([]byte(redact.String(string(p)))); err != nil {
		return 0, err
	}
	return len(p), nil
}
