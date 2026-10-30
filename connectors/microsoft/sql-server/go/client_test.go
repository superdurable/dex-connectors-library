// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver

import (
	"crypto/tls"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var noCredentials = sdkgo.StaticCredentialProvider[Credentials]{}

func TestNewAppliesManifestDefaultsWithoutContactingTheServer(t *testing.T) {
	client, err := New(Config{Host: "myserver.database.windows.net", Database: "app", User: "dex_app"}, noCredentials)
	require.NoError(t, err)
	require.Equal(t, 1433, client.port)
	require.Equal(t, EncryptMandatory, client.encrypt, "verified TLS is the default")
	require.Equal(t, 10*time.Second, client.connectTimeout)
	require.Equal(t, 5*time.Second, client.statementTimeout)
	require.Equal(t, 1000, client.maxRows)
	require.Equal(t, 1<<20, client.maxResponseBytes)
}

func TestNewRejectsUnsafeOrAmbiguousConnectionSettings(t *testing.T) {
	valid := Config{Host: "db.example.com", Database: "app", User: "dex_app"}
	for name, mutate := range map[string]func(*Config){
		"missing host":                func(config *Config) { config.Host = "" },
		"missing database":            func(config *Config) { config.Database = "" },
		"missing user":                func(config *Config) { config.User = "" },
		"blank user":                  func(config *Config) { config.User = "  " },
		"Windows account":             func(config *Config) { config.User = `CONTOSO\dex_app` },
		"several hosts":               func(config *Config) { config.Host = "a.example.com,b.example.com" },
		"port after a comma":          func(config *Config) { config.Host = "db.example.com,1433" },
		"protocol prefix":             func(config *Config) { config.Host = "tcp:db.example.com" },
		"scheme":                      func(config *Config) { config.Host = "sqlserver://db.example.com" },
		"named instance":              func(config *Config) { config.Host = `db.example.com\SQLEXPRESS` },
		"port in host":                func(config *Config) { config.Host = "db.example.com:1433" },
		"connection string keyword":   func(config *Config) { config.Host = "db.example.com;encrypt=false" },
		"surrounding whitespace":      func(config *Config) { config.Host = " db.example.com" },
		"port too large":              func(config *Config) { config.Port = 65536 },
		"negative port":               func(config *Config) { config.Port = -1 },
		"plaintext to remote host":    func(config *Config) { config.Encrypt = EncryptDisable },
		"optional is not offered":     func(config *Config) { config.Encrypt = Encrypt("optional") },
		"sub-millisecond timeout":     func(config *Config) { config.StatementTimeout = time.Microsecond },
		"timeout beyond LOCK_TIMEOUT": func(config *Config) { config.StatementTimeout = 600 * time.Hour },
		"unbounded rows":              func(config *Config) { config.MaxRows = maximumConfiguredRows + 1 },
		"unbounded response":          func(config *Config) { config.MaxResponseBytes = maximumConfiguredResponseBytes + 1 },
		"control character in user":   func(config *Config) { config.User = "dex\x00app" },
		"database name too long":      func(config *Config) { config.Database = strings.Repeat("d", 129) },
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
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		_, err := New(Config{Host: host, Database: "app", User: "dex_app", Encrypt: EncryptDisable}, noCredentials)
		require.NoError(t, err, host)
	}
	_, err := New(Config{Host: "2001:db8::1", Database: "app", User: "dex_app"}, noCredentials)
	require.NoError(t, err, "a bare IPv6 literal is one host")
}

func TestDriverConfigSetsEverySecurityRelevantFieldExplicitly(t *testing.T) {
	t.Setenv("MSSQL_USE_EPA", "true")
	client, err := New(Config{Host: "db.example.com", Port: 14330, Database: "app", User: "dex_app"}, noCredentials)
	require.NoError(t, err)
	password := `pa;ss'word="with" spaces;encrypt=disable`
	meter := newConnectionMeter(time.Time{}, client.host)
	config, err := client.newDriverConfig(password, meter)
	require.NoError(t, err)
	require.Equal(t, "db.example.com", config.Host)
	require.Equal(t, uint64(14330), config.Port)
	require.Empty(t, config.Instance, "no SQL Server Browser lookup")
	require.Equal(t, "app", config.Database)
	require.Equal(t, "dex_app", config.User)
	require.Equal(t, password, config.Password, "no connection string is parsed, so every byte is literal")
	require.Equal(t, msdsn.Encryption(msdsn.EncryptionRequired), config.Encryption)
	require.NotNil(t, config.TLSConfig)
	require.Equal(t, "dex-sql-server-connector", config.AppName)
	require.True(t, config.DisableRetry)
	require.False(t, config.MultiSubnetFailover, "one connection at a time, so the meter sees every byte")
	require.False(t, config.EpaEnabled, "the driver's environment variable is ignored")
	require.False(t, config.ReadOnlyIntent)
	require.Empty(t, config.FailOverPartner)
	require.Empty(t, config.ChangePassword)
	require.Zero(t, config.LogFlags, "the driver logs nothing")
	require.Equal(t, []string{"tcp"}, config.Protocols)
	require.Empty(t, config.Parameters, "no authenticator parameter selects integrated authentication")
	require.Equal(t, time.UTC, config.Encoding.Timezone)
	require.False(t, config.Encoding.GuidConversion, "the connector reads GUID wire bytes itself")
	require.Len(t, config.ActivityID, 16)
	require.Equal(t, 10*time.Second, config.DialTimeout)
}

func TestTLSConfigMapsEncryptModes(t *testing.T) {
	t.Setenv("SQLSERVER_SSL_CA", "")
	for mode, check := range map[Encrypt]func(*tls.Config, msdsn.Encryption){
		EncryptMandatory: func(config *tls.Config, encryption msdsn.Encryption) {
			require.Equal(t, msdsn.Encryption(msdsn.EncryptionRequired), encryption)
			require.True(t, config.InsecureSkipVerify, "the connector verifies the chain and name itself")
			require.NotNil(t, config.VerifyConnection)
		},
		EncryptStrict: func(config *tls.Config, encryption msdsn.Encryption) {
			require.Equal(t, msdsn.Encryption(msdsn.EncryptionStrict), encryption)
			require.NotNil(t, config.VerifyConnection)
		},
		EncryptTrustServerCertificate: func(config *tls.Config, encryption msdsn.Encryption) {
			require.Equal(t, msdsn.Encryption(msdsn.EncryptionRequired), encryption)
			require.True(t, config.InsecureSkipVerify)
			require.Nil(t, config.VerifyConnection, "the explicit choice skips verification")
		},
	} {
		client, err := New(Config{Host: "db.example.com", Database: "app", User: "dex_app", Encrypt: mode}, noCredentials)
		require.NoError(t, err)
		config, encryption, err := client.newTLSConfig(newConnectionMeter(time.Time{}, client.host))
		require.NoError(t, err)
		require.Equal(t, uint16(tls.VersionTLS12), config.MinVersion)
		require.Equal(t, "db.example.com", config.ServerName)
		require.True(t, config.DynamicRecordSizingDisabled, "SQL Server expects one TLS record per TDS packet")
		check(config, encryption)
	}
	local, err := New(Config{Host: "localhost", Database: "app", User: "dex_app", Encrypt: EncryptDisable}, noCredentials)
	require.NoError(t, err)
	config, encryption, err := local.newTLSConfig(newConnectionMeter(time.Time{}, local.host))
	require.NoError(t, err)
	require.Nil(t, config)
	require.Equal(t, msdsn.Encryption(msdsn.EncryptionDisabled), encryption)

	t.Setenv("SQLSERVER_SSL_CA", "/nonexistent/ca.pem")
	verified, err := New(Config{Host: "db.example.com", Database: "app", User: "dex_app"}, noCredentials)
	require.NoError(t, err)
	_, _, err = verified.newTLSConfig(newConnectionMeter(time.Time{}, verified.host))
	require.ErrorIs(t, err, errTrustedRootsUnreadable)
}

func TestSessionSettingsBatchPinsTheLockTimeoutInMilliseconds(t *testing.T) {
	client, err := New(Config{Host: "db.example.com", Database: "app", User: "dex_app", StatementTimeout: 1500*time.Millisecond + time.Microsecond}, noCredentials)
	require.NoError(t, err)
	batch := client.sessionSettingsBatch()
	require.Contains(t, batch, "SET LOCK_TIMEOUT 1501; SELECT @@SPID")
	require.Contains(t, batch, "SET NOCOUNT OFF", "row counts are always reported")
	require.Contains(t, batch, "SET LANGUAGE us_english", "error messages are parsed only in English")
}

func TestCallDriverTurnsAPanicIntoAnError(t *testing.T) {
	err := callDriver(func() error { panic("a value that may quote server data") })
	var panicFailure *driverPanicError
	require.ErrorAs(t, err, &panicFailure)
	require.NotContains(t, err.Error(), "server data")
}
