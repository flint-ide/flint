// Package db manages database connections, query execution and schema introspection.
package db

import (
	"errors"
	"fmt"
	"strings"
)

// Driver identifies a supported database engine.
type Driver string

const (
	Postgres   Driver = "postgres"
	ClickHouse Driver = "clickhouse"
)

// Protocol is the ClickHouse wire protocol: the native TCP one or HTTP(S).
// It's a plain string in JSON; "" means native so older profiles keep working.
const (
	ProtocolNative = "native"
	ProtocolHTTP   = "http"
)

// Connection is a saved connection profile. The password lives in the OS keychain,
// never in this struct's JSON.
type Connection struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Driver Driver `json:"driver"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	// Database is the default database; empty means "postgres" (Postgres) or the user's
	// server-side default database (ClickHouse).
	// Other databases of the same cluster are reachable with WithDatabase.
	Database string `json:"database"`
	User     string `json:"user"`
	// Protocol selects the ClickHouse transport (ProtocolNative or ProtocolHTTP); unused for Postgres.
	Protocol string `json:"protocol"`
	// TLS enables encrypted transport with the server's certificate and host
	// name verified (Postgres sslmode=verify-full, ClickHouse secure ports).
	TLS bool `json:"tls"`
	// TLSInsecure skips that verification (self-signed or private CAs): the
	// traffic is encrypted but a man in the middle can read it.
	TLSInsecure bool `json:"tlsInsecure"`
	// ReadOnly makes Flint refuse statements that modify data.
	ReadOnly bool `json:"readOnly"`
}

// DefaultPort returns the conventional port for the driver.
func (c Connection) DefaultPort() int {
	switch c.Driver {
	case Postgres:
		return 5432
	case ClickHouse:
		switch {
		case c.Protocol == ProtocolHTTP && c.TLS:
			return 8443
		case c.Protocol == ProtocolHTTP:
			return 8123
		case c.TLS:
			return 9440
		}
		return 9000
	}
	return 0
}

// DefaultDatabase is the database Flint opens when none is chosen: the profile's, or for
// Postgres the always-present "postgres" (enough to list the cluster). ClickHouse returns ""
// so the server applies the user's own default_database, which may not be "default".
func (c Connection) DefaultDatabase() string {
	if c.Database == "" && c.Driver == Postgres {
		return "postgres"
	}
	return c.Database
}

// WithDatabase returns the profile pointed at another database of the same server
// (same host, user and password). An empty name keeps the profile's default.
func (c Connection) WithDatabase(name string) Connection {
	if name != "" {
		c.Database = name
	}
	return c
}

// Normalized trims fields and fills defaults.
func (c Connection) Normalized() Connection {
	c.Name = strings.TrimSpace(c.Name)
	c.Host = strings.TrimSpace(c.Host)
	c.Database = strings.TrimSpace(c.Database)
	c.User = strings.TrimSpace(c.User)
	if c.Driver == ClickHouse && c.Protocol == "" {
		c.Protocol = ProtocolNative
	}
	if c.Driver != ClickHouse {
		c.Protocol = ""
	}
	if c.Port == 0 {
		c.Port = c.DefaultPort()
	}
	if c.Name == "" && c.Database == "" {
		c.Name = c.Host
	} else if c.Name == "" {
		c.Name = fmt.Sprintf("%s@%s", c.Database, c.Host)
	}
	return c
}

// Validate reports the first problem with the profile.
func (c Connection) Validate() error {
	switch c.Driver {
	case Postgres, ClickHouse:
	default:
		return fmt.Errorf("unsupported driver %q", c.Driver)
	}
	if c.Driver == ClickHouse && c.Protocol != "" && c.Protocol != ProtocolNative && c.Protocol != ProtocolHTTP {
		return fmt.Errorf("unsupported clickhouse protocol %q", c.Protocol)
	}
	if c.Host == "" {
		return errors.New("host is required")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("invalid port %d", c.Port)
	}
	if c.User == "" {
		return errors.New("user is required")
	}
	return nil
}
