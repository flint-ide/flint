package db

import (
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestConnectionNormalized(t *testing.T) {
	tests := []struct {
		name string
		in   Connection
		want Connection
	}{
		{
			name: "postgres default port and name fallback",
			in:   Connection{Driver: Postgres, Host: " db.local ", Database: " app ", User: " bob "},
			want: Connection{Driver: Postgres, Host: "db.local", Database: "app", User: "bob", Port: 5432, Name: "app@db.local"},
		},
		{
			name: "postgres without database falls back to host for the name",
			in:   Connection{Driver: Postgres, Host: "db.local", User: "bob"},
			want: Connection{Driver: Postgres, Host: "db.local", User: "bob", Port: 5432, Name: "db.local"},
		},
		{
			name: "clickhouse plain port",
			in:   Connection{Driver: ClickHouse, Host: "ch", Name: "  Prod CH "},
			want: Connection{Driver: ClickHouse, Host: "ch", Port: 8123, Name: "Prod CH"},
		},
		{
			name: "clickhouse tls port",
			in:   Connection{Driver: ClickHouse, Host: "ch", TLS: true, Name: "x"},
			want: Connection{Driver: ClickHouse, Host: "ch", TLS: true, Port: 8443, Name: "x"},
		},
		{
			name: "clickhouse custom http port kept",
			in:   Connection{Driver: ClickHouse, Host: "ch", Port: 18123, Name: "x"},
			want: Connection{Driver: ClickHouse, Host: "ch", Port: 18123, Name: "x"},
		},
		{
			name: "explicit port kept",
			in:   Connection{Driver: Postgres, Host: "h", Port: 6543, Name: "x"},
			want: Connection{Driver: Postgres, Host: "h", Port: 6543, Name: "x"},
		},
		{
			name: "unknown driver has no default port",
			in:   Connection{Driver: "mysql", Host: "h", Name: "x"},
			want: Connection{Driver: "mysql", Host: "h", Port: 0, Name: "x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.Normalized(); got != tt.want {
				t.Errorf("Normalized() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestConnectionValidate(t *testing.T) {
	valid := Connection{Driver: Postgres, Host: "h", Port: 5432, User: "u"}
	tests := []struct {
		name    string
		mutate  func(c Connection) Connection
		wantSub string
	}{
		{"valid", func(c Connection) Connection { return c }, ""},
		{"valid clickhouse", func(c Connection) Connection { c.Driver = ClickHouse; return c }, ""},
		{"bad driver", func(c Connection) Connection { c.Driver = "oracle"; return c }, `unsupported driver "oracle"`},
		{"empty driver", func(c Connection) Connection { c.Driver = ""; return c }, "unsupported driver"},
		{"no host", func(c Connection) Connection { c.Host = ""; return c }, "host is required"},
		{"port zero", func(c Connection) Connection { c.Port = 0; return c }, "invalid port 0"},
		{"port negative", func(c Connection) Connection { c.Port = -1; return c }, "invalid port"},
		{"port too big", func(c Connection) Connection { c.Port = 65536; return c }, "invalid port 65536"},
		{"no user", func(c Connection) Connection { c.User = ""; return c }, "user is required"},
		{"no database is fine", func(c Connection) Connection { c.Database = ""; return c }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mutate(valid).Validate()
			if tt.wantSub == "" {
				if err != nil {
					t.Errorf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("Validate() = %v, want containing %q", err, tt.wantSub)
			}
		})
	}
}

func TestConnectionDefaultDatabase(t *testing.T) {
	tests := []struct {
		name string
		conn Connection
		want string
	}{
		{"postgres profile database", Connection{Driver: Postgres, Database: "app"}, "app"},
		{"postgres without database uses maintenance db", Connection{Driver: Postgres}, "postgres"},
		{"clickhouse without database leaves it to the server", Connection{Driver: ClickHouse}, ""},
		{"clickhouse profile database", Connection{Driver: ClickHouse, Database: "logs"}, "logs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.conn.DefaultDatabase(); got != tt.want {
				t.Errorf("DefaultDatabase() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConnectionWithDatabase(t *testing.T) {
	base := Connection{ID: "c1", Driver: Postgres, Host: "h", User: "u", Database: "app", ReadOnly: true}
	other := base.WithDatabase("billing")
	if other.Database != "billing" || other.ID != "c1" || other.Host != "h" || other.User != "u" || !other.ReadOnly {
		t.Errorf("WithDatabase(billing) = %+v", other)
	}
	if base.Database != "app" {
		t.Errorf("WithDatabase mutated the receiver: %+v", base)
	}
	if got := base.WithDatabase(""); got != base {
		t.Errorf("WithDatabase(\"\") = %+v, want the profile unchanged", got)
	}
}

func TestPostgresDSN(t *testing.T) {
	tests := []struct {
		name     string
		conn     Connection
		password string
		wantSSL  string
	}{
		{
			name:     "special chars in password, prefer ssl",
			conn:     Connection{Driver: Postgres, Host: "db.example.com", Port: 5432, Database: "app", User: "user@corp"},
			password: `p@ss:w/rd?#%& zażółć"'`,
			wantSSL:  "prefer",
		},
		{
			name:     "tls verifies the certificate, ipv6 host, db with space",
			conn:     Connection{Driver: Postgres, Host: "::1", Port: 6543, Database: "my db", User: "bob", TLS: true},
			password: "",
			wantSSL:  "verify-full",
		},
		{
			name:     "tls without verification only when asked",
			conn:     Connection{Driver: Postgres, Host: "db", Port: 5432, Database: "app", User: "bob", TLS: true, TLSInsecure: true},
			password: "",
			wantSSL:  "require",
		},
		{
			name:     "no database connects to postgres",
			conn:     Connection{Driver: Postgres, Host: "h", Port: 5432, User: "bob"},
			password: "pw",
			wantSSL:  "prefer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := postgresDSN(tt.conn, tt.password)

			u, err := url.Parse(dsn)
			if err != nil {
				t.Fatalf("DSN %q is not a valid URL: %v", dsn, err)
			}
			q := u.Query()
			if got := q.Get("sslmode"); got != tt.wantSSL {
				t.Errorf("sslmode = %q, want %q", got, tt.wantSSL)
			}
			if q.Get("application_name") != "flint" || q.Get("connect_timeout") != "10" {
				t.Errorf("query = %v", q)
			}

			cfg, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("pgconn.ParseConfig(%q) error = %v", dsn, err)
			}
			if cfg.Password != tt.password {
				t.Errorf("password = %q, want %q", cfg.Password, tt.password)
			}
			if cfg.User != tt.conn.User || cfg.Database != tt.conn.DefaultDatabase() {
				t.Errorf("user/db = %q/%q, want %q/%q", cfg.User, cfg.Database, tt.conn.User, tt.conn.DefaultDatabase())
			}
			if cfg.Host != tt.conn.Host || int(cfg.Port) != tt.conn.Port {
				t.Errorf("host:port = %s:%d, want %s:%d", cfg.Host, cfg.Port, tt.conn.Host, tt.conn.Port)
			}
			if tt.wantSSL == "require" && cfg.TLSConfig == nil {
				t.Error("sslmode=require produced no TLS config")
			}
		})
	}
}
