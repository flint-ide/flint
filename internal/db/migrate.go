package db

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"flint/internal/config"
)

// Earlier versions reached ClickHouse over its native TCP protocol (ports 9000 / 9440) and
// stored a "protocol" field in connections.json. Flint now speaks HTTP only, so profiles
// saved with the native protocol move to the HTTP port the first time they are loaded.

const (
	nativeBackupFile = "connections.json.native-backup"
	noticeFile       = "connections-migration-notice.txt"
)

// storedConnection is a profile as it sits in connections.json: the current fields plus
// the "protocol" older versions wrote.
type storedConnection struct {
	Connection
	Protocol string `json:"protocol,omitempty"`
}

// migrateNative returns the profile moved to ClickHouse's HTTP interface and a one-line
// description of what changed ("" when nothing did).
func migrateNative(sc storedConnection) (Connection, string) {
	c := sc.Connection
	if c.Driver != ClickHouse || sc.Protocol == "http" {
		return c, ""
	}
	// An explicit HTTP port saved with the native protocol is already the right one.
	if c.Port == 8123 || c.Port == 8443 {
		return c, ""
	}
	old := c.Port
	c.Port = c.DefaultPort()
	switch old {
	case 0, 9000, 9440:
		return c, fmt.Sprintf("%s: port %d → %d", c.Name, orDefault(old, 9000), c.Port)
	}
	return c, fmt.Sprintf("%s: custom native port %d → %d (set the HTTP port in the connection if it differs)", c.Name, old, c.Port)
}

func orDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// loadConnections reads connections.json and migrates native ClickHouse profiles. When
// anything changed it keeps the original file as connections.json.native-backup, saves the
// migrated list and leaves a notice for the UI to show once.
func loadConnections(dir, path string) ([]Connection, error) {
	var stored []storedConnection
	if err := config.LoadJSON(path, &stored); err != nil {
		return nil, err
	}
	conns := make([]Connection, len(stored))
	var changes []string
	for i, sc := range stored {
		c, change := migrateNative(sc)
		conns[i] = c
		if change != "" {
			changes = append(changes, change)
		}
	}
	if len(changes) == 0 { // profiles already on HTTP keep their stale protocol field until next saved
		return conns, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	backup := filepath.Join(dir, nativeBackupFile)
	if _, err := os.Stat(backup); errors.Is(err, fs.ErrNotExist) {
		if err := config.WriteFileAtomic(backup, raw, 0o600); err != nil {
			return nil, fmt.Errorf("back up connections: %w", err)
		}
	}
	if len(changes) > 0 {
		notice := "ClickHouse connections now use the HTTP interface instead of the native protocol. Moved:\n" +
			strings.Join(changes, "\n") + "\nThe previous file was kept as " + nativeBackupFile + "."
		if err := config.WriteFileAtomic(filepath.Join(dir, noticeFile), []byte(notice), 0o600); err != nil {
			return nil, fmt.Errorf("write migration notice: %w", err)
		}
	}
	if err := config.SaveJSON(path, conns); err != nil {
		return nil, err
	}
	return conns, nil
}

// TakeMigrationNotice returns the one-time message about migrated connections ("" when
// there is none) and forgets it.
func (s *Store) TakeMigrationNotice() string {
	path := filepath.Join(filepath.Dir(s.path), noticeFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	_ = os.Remove(path)
	return string(data)
}
