// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postgresql

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var noCredentials = sdkgo.StaticCredentialProvider[Credentials]{}

func TestNewAppliesManifestDefaultsWithoutContactingTheServer(t *testing.T) {
	client, err := New(Config{Host: "db.example.com", Database: "app", User: "dex_app"}, noCredentials)
	require.NoError(t, err)
	require.Equal(t, uint16(5432), client.port)
	require.Equal(t, SSLModeRequire, client.sslMode, "TLS is required by default")
	require.Equal(t, 5*time.Second, client.connectTimeout)
	require.Equal(t, 5*time.Second, client.statementTimeout)
	require.Equal(t, 1000, client.maxRows)
	require.Equal(t, 1<<20, client.maxResponseBytes)
}

func TestNewRejectsUnsafeOrAmbiguousConnectionSettings(t *testing.T) {
	valid := Config{Host: "db.example.com", Database: "app", User: "dex_app"}
	for name, mutate := range map[string]func(*Config){
		"missing host":              func(config *Config) { config.Host = "" },
		"missing database":          func(config *Config) { config.Database = "" },
		"missing user":              func(config *Config) { config.User = "" },
		"blank user":                func(config *Config) { config.User = "  " },
		"several hosts":             func(config *Config) { config.Host = "a.example.com,b.example.com" },
		"scheme":                    func(config *Config) { config.Host = "postgres://db.example.com" },
		"surrounding whitespace":    func(config *Config) { config.Host = " db.example.com" },
		"keyword injection":         func(config *Config) { config.Host = "db sslmode=disable" },
		"port too large":            func(config *Config) { config.Port = 65536 },
		"negative port":             func(config *Config) { config.Port = -1 },
		"plaintext to remote host":  func(config *Config) { config.SSLMode = SSLModeDisable },
		"prefer is not offered":     func(config *Config) { config.SSLMode = SSLMode("prefer") },
		"sub-millisecond timeout":   func(config *Config) { config.StatementTimeout = time.Microsecond },
		"unbounded rows":            func(config *Config) { config.MaxRows = maximumConfiguredRows + 1 },
		"unbounded response":        func(config *Config) { config.MaxResponseBytes = maximumConfiguredResponseBytes + 1 },
		"control character in user": func(config *Config) { config.User = "dex\napp" },
	} {
		t.Run(name, func(t *testing.T) {
			config := valid
			mutate(&config)
			_, err := New(config, noCredentials)
			require.Error(t, err)
		})
	}
	_, err := New(valid, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = New(valid, noCredentials, nil)
	require.ErrorContains(t, err, "option is nil")
	_, err = New(valid, noCredentials, WithClock(nil))
	require.ErrorContains(t, err, "clock is required")
}

func TestNewAllowsPlaintextOnlyForLocalServers(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1", "/var/run/postgresql"} {
		_, err := New(Config{Host: host, Database: "app", User: "dex_app", SSLMode: SSLModeDisable}, noCredentials)
		require.NoError(t, err, host)
	}
}

func TestConnectionConfigIgnoresLibpqEnvironmentOverrides(t *testing.T) {
	t.Setenv("PGHOST", "attacker.example.com")
	t.Setenv("PGPORT", "6543")
	t.Setenv("PGDATABASE", "other")
	t.Setenv("PGUSER", "postgres")
	t.Setenv("PGPASSWORD", "environment-password")
	t.Setenv("PGSSLMODE", "disable")
	t.Setenv("PGOPTIONS", "-c default_transaction_read_only=off")
	t.Setenv("PGTZ", "America/New_York")
	t.Setenv("PGTARGETSESSIONATTRS", "read-only")
	client, err := New(Config{Host: "db.example.com", Port: 5433, Database: "app", User: "dex_app"}, noCredentials)
	require.NoError(t, err)
	password := `pa'ss \word with spaces`
	config, err := client.newConnectionConfig(password)
	require.NoError(t, err)
	require.Equal(t, "db.example.com", config.Host)
	require.Equal(t, uint16(5433), config.Port)
	require.Equal(t, "app", config.Database)
	require.Equal(t, "dex_app", config.User)
	require.Equal(t, password, config.Password, "quoting preserves every byte")
	require.NotNil(t, config.TLSConfig, "PGSSLMODE cannot disable TLS")
	require.Empty(t, config.Fallbacks, "no plaintext fallback exists")
	require.Nil(t, config.ValidateConnect)
	require.Equal(t, map[string]string{"application_name": applicationName, "client_encoding": "UTF8"}, config.RuntimeParams)
	require.Equal(t, 5*time.Second, config.ConnectTimeout)
}

func TestConnectionConfigMapsSSLModes(t *testing.T) {
	for mode, wantsVerifiedHostName := range map[SSLMode]bool{SSLModeRequire: false, SSLModeVerifyCa: false, SSLModeVerifyFull: true} {
		client, err := New(Config{Host: "db.example.com", Database: "app", User: "dex_app", SSLMode: mode}, noCredentials)
		require.NoError(t, err)
		config, err := client.newConnectionConfig("password")
		require.NoError(t, err)
		require.NotNil(t, config.TLSConfig, mode)
		require.Equal(t, wantsVerifiedHostName, !config.TLSConfig.InsecureSkipVerify, mode)
		if mode == SSLModeVerifyCa {
			require.NotNil(t, config.TLSConfig.VerifyPeerCertificate, "verify-ca checks the chain without the host name")
		}
	}
	local, err := New(Config{Host: "127.0.0.1", Database: "app", User: "dex_app", SSLMode: SSLModeDisable}, noCredentials)
	require.NoError(t, err)
	config, err := local.newConnectionConfig("password")
	require.NoError(t, err)
	require.Nil(t, config.TLSConfig)
}

func TestMillisecondsRoundedUp(t *testing.T) {
	require.Equal(t, int64(1), millisecondsRoundedUp(time.Microsecond))
	require.Equal(t, int64(5000), millisecondsRoundedUp(5*time.Second))
	require.Equal(t, int64(1501), millisecondsRoundedUp(1500*time.Millisecond+time.Nanosecond))
}
