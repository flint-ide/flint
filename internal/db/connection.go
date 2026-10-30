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

// Connection is a saved connection profile. The password lives in the OS keychain,
// never in this struct's JSON.
type Connection struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Driver   Driver `json:"driver"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	User     string `json:"user"`
	// TLS enables encrypted transport (Postgres sslmode=require, ClickHouse secure native port).
	TLS bool `json:"tls"`
	// ReadOnly makes Flint refuse statements that modify data.
	ReadOnly bool `json:"readOnly"`
}

// DefaultPort returns the conventional port for the driver.
func (c Connection) DefaultPort() int {
	switch c.Driver {
	case Postgres:
		return 5432
	case ClickHouse:
		if c.TLS {
			return 9440
		}
		return 9000
	}
	return 0
}

// Normalized trims fields and fills defaults.
func (c Connection) Normalized() Connection {
	c.Name = strings.TrimSpace(c.Name)
	c.Host = strings.TrimSpace(c.Host)
	c.Database = strings.TrimSpace(c.Database)
	c.User = strings.TrimSpace(c.User)
	if c.Port == 0 {
		c.Port = c.DefaultPort()
	}
	if c.Name == "" {
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
