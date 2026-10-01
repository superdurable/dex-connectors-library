// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql

import (
	"context"
	"crypto/tls"
	"database/sql/driver"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var noCredentials = sdkgo.StaticCredentialProvider[Credentials]{}

func TestNewAppliesManifestDefaultsWithoutContactingTheServer(t *testing.T) {
	client, err := New(Config{Host: "db.example.com", Database: "app", User: "dex_app"}, noCredentials)
	require.NoError(t, err)
	require.Equal(t, 3306, client.port)
	require.Equal(t, SSLModeRequired, client.sslMode, "TLS is required by default")
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
		"scheme":                    func(config *Config) { config.Host = "mysql://db.example.com" },
		"port in host":              func(config *Config) { config.Host = "db.example.com:3307" },
		"bracketed IPv6":            func(config *Config) { config.Host = "[::1]" },
		"surrounding whitespace":    func(config *Config) { config.Host = " db.example.com" },
		"quote":                     func(config *Config) { config.Host = "db'.example.com" },
		"port too large":            func(config *Config) { config.Port = 65536 },
		"negative port":             func(config *Config) { config.Port = -1 },
		"plaintext to remote host":  func(config *Config) { config.SSLMode = SSLModeDisabled },
		"preferred is not offered":  func(config *Config) { config.SSLMode = SSLMode("preferred") },
		"TLS over a Unix socket":    func(config *Config) { config.Host = "/tmp/mysql.sock" },
		"sub-millisecond timeout":   func(config *Config) { config.StatementTimeout = time.Microsecond },
		"unbounded rows":            func(config *Config) { config.MaxRows = maximumConfiguredRows + 1 },
		"unbounded response":        func(config *Config) { config.MaxResponseBytes = maximumConfiguredResponseBytes + 1 },
		"control character in user": func(config *Config) { config.User = "dex\x00app" },
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
	for _, host := range []string{"localhost", "127.0.0.1", "::1", "/tmp/mysql.sock"} {
		_, err := New(Config{Host: host, Database: "app", User: "dex_app", SSLMode: SSLModeDisabled}, noCredentials)
		require.NoError(t, err, host)
	}
	_, err := New(Config{Host: "2001:db8::1", Database: "app", User: "dex_app"}, noCredentials)
	require.NoError(t, err, "a bare IPv6 literal is one host")
}

func TestDriverConfigSetsEverySecurityRelevantFieldExplicitly(t *testing.T) {
	t.Setenv("MYSQL_HOST", "attacker.example.com")
	t.Setenv("MYSQL_TCP_PORT", "6543")
	t.Setenv("MYSQL_PWD", "environment-password")
	client, err := New(Config{Host: "db.example.com", Port: 3307, Database: "app", User: "dex_app"}, noCredentials)
	require.NoError(t, err)
	password := `pa'ss \word with spaces`
	meter := newConnectionMeter(time.Time{})
	config, err := client.newDriverConfig(password, meter)
	require.NoError(t, err)
	require.Equal(t, "tcp", config.Net)
	require.Equal(t, "db.example.com:3307", config.Addr)
	require.Equal(t, "app", config.DBName)
	require.Equal(t, "dex_app", config.User)
	require.Equal(t, password, config.Passwd, "no DSN is parsed, so every byte is literal")
	require.NotNil(t, config.TLS)
	require.False(t, config.AllowFallbackToPlaintext, "no plaintext fallback exists")
	require.False(t, config.AllowCleartextPasswords)
	require.False(t, config.AllowAllFiles, "LOAD DATA LOCAL INFILE cannot read Worker files")
	require.False(t, config.MultiStatements)
	require.False(t, config.InterpolateParams, "values are always server-side parameters")
	require.False(t, config.ClientFoundRows)
	require.False(t, config.ParseTime)
	require.Equal(t, time.UTC, config.Loc)
	require.Equal(t, 5*time.Second, config.Timeout)
	require.Empty(t, config.Params)
	require.Equal(t, "program_name:dex-mysql-connector", config.ConnectionAttributes)
	require.NotNil(t, config.DialFunc)

	local, err := New(Config{Host: "/tmp/mysql.sock", Database: "app", User: "dex_app", SSLMode: SSLModeDisabled}, noCredentials)
	require.NoError(t, err)
	socketConfig, err := local.newDriverConfig(password, meter)
	require.NoError(t, err)
	require.Equal(t, "unix", socketConfig.Net)
	require.Equal(t, "/tmp/mysql.sock", socketConfig.Addr)
	require.Nil(t, socketConfig.TLS)
}

func TestTLSConfigMapsSSLModes(t *testing.T) {
	t.Setenv("MYSQL_SSL_CA", "")
	for mode, check := range map[SSLMode]func(*tls.Config){
		SSLModeRequired: func(config *tls.Config) {
			require.True(t, config.InsecureSkipVerify)
			require.Nil(t, config.VerifyConnection)
		},
		SSLModeVerifyCa: func(config *tls.Config) {
			require.True(t, config.InsecureSkipVerify, "Go's verifier always checks the name, so verify-ca verifies the chain itself")
			require.NotNil(t, config.VerifyConnection)
		},
		SSLModeVerifyIdentity: func(config *tls.Config) {
			require.False(t, config.InsecureSkipVerify)
			require.Equal(t, "db.example.com", config.ServerName)
		},
	} {
		client, err := New(Config{Host: "db.example.com", Database: "app", User: "dex_app", SSLMode: mode}, noCredentials)
		require.NoError(t, err)
		config, err := client.newTLSConfig()
		require.NoError(t, err)
		require.Equal(t, uint16(tls.VersionTLS12), config.MinVersion)
		require.Equal(t, "db.example.com", config.ServerName, "SNI names the host for proxies that route by it")
		check(config)
	}
	address, err := New(Config{Host: "10.0.0.5", Database: "app", User: "dex_app", SSLMode: SSLModeRequired}, noCredentials)
	require.NoError(t, err)
	config, err := address.newTLSConfig()
	require.NoError(t, err)
	require.Empty(t, config.ServerName, "an IP address is not sent as SNI")

	t.Setenv("MYSQL_SSL_CA", "/nonexistent/ca.pem")
	verified, err := New(Config{Host: "db.example.com", Database: "app", User: "dex_app", SSLMode: SSLModeVerifyIdentity}, noCredentials)
	require.NoError(t, err)
	_, err = verified.newTLSConfig()
	require.ErrorIs(t, err, errTrustedRootsUnreadable)
}

func TestSessionSettingsPinTheTypeMappingAndBoundEveryStatement(t *testing.T) {
	client, err := New(Config{Host: "db.example.com", Database: "app", User: "dex_app", StatementTimeout: 2500 * time.Millisecond}, noCredentials)
	require.NoError(t, err)
	require.Equal(t, "SET SESSION time_zone = '+00:00', sql_mode = '"+pinnedSQLMode+"', wait_timeout = 30, "+
		"innodb_lock_wait_timeout = 3, lock_wait_timeout = 3, max_execution_time = 2500", client.sessionSettingsStatement(false))
	require.Equal(t, "SET SESSION time_zone = '+00:00', sql_mode = '"+pinnedSQLMode+"', wait_timeout = 30, "+
		"innodb_lock_wait_timeout = 3, lock_wait_timeout = 3, max_statement_time = 2.500000", client.sessionSettingsStatement(true))
}

// recordingTransaction is a driver.Tx whose COMMIT writes through a metered connection.
type recordingTransaction struct {
	connection   *meteredConnection
	commitBytes  []byte
	commitErr    error
	commitCalled bool
}

func (transaction *recordingTransaction) Commit() error {
	transaction.commitCalled = true
	if len(transaction.commitBytes) > 0 {
		// The pipe's peer discards bytes; only the meter's count matters here.
		_, _ = transaction.connection.Write(transaction.commitBytes)
	}
	return transaction.commitErr
}

func (*recordingTransaction) Rollback() error { return nil }

var _ driver.Tx = (*recordingTransaction)(nil)

func newMeteredPipe(t *testing.T) (*connectionMeter, *meteredConnection) {
	t.Helper()
	clientSide, serverSide := net.Pipe()
	go func() {
		buffer := make([]byte, 64)
		for {
			if _, err := serverSide.Read(buffer); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { require.NoError(t, serverSide.Close()) })
	meter := newConnectionMeter(time.Time{})
	return meter, &meteredConnection{Conn: clientSide, meter: meter}
}

func TestCommitTransactionRetriesOnlyWhenNoCommitByteLeftTheClient(t *testing.T) {
	ctx := context.Background()
	meter, connection := newMeteredPipe(t)
	session := &databaseSession{meter: meter}

	failedBeforeWriting := &recordingTransaction{connection: connection, commitErr: driver.ErrBadConn}
	require.ErrorIs(t, session.commitTransaction(ctx, failedBeforeWriting), errCommitNotSent)

	failedAfterWriting := &recordingTransaction{connection: connection, commitBytes: []byte("COMMIT"), commitErr: errors.New("connection reset")}
	err := session.commitTransaction(ctx, failedAfterWriting)
	require.Error(t, err)
	require.NotErrorIs(t, err, errCommitNotSent, "a COMMIT that may have arrived is uncertain")

	acknowledged := &recordingTransaction{connection: connection, commitBytes: []byte("COMMIT")}
	require.NoError(t, session.commitTransaction(ctx, acknowledged))

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	neverSent := &recordingTransaction{connection: connection}
	require.ErrorIs(t, session.commitTransaction(canceled, neverSent), errCommitNotSent)
	require.False(t, neverSent.commitCalled, "a passed deadline never sends COMMIT")

	require.NoError(t, connection.Close())
	closed := &recordingTransaction{connection: connection}
	require.ErrorIs(t, session.commitTransaction(ctx, closed), errCommitNotSent)
	require.False(t, closed.commitCalled, "a closed connection never sends COMMIT")
}

func TestTimeoutRounding(t *testing.T) {
	require.Equal(t, int64(1), millisecondsRoundedUp(time.Microsecond))
	require.Equal(t, int64(5000), millisecondsRoundedUp(5*time.Second))
	require.Equal(t, int64(1501), millisecondsRoundedUp(1500*time.Millisecond+time.Nanosecond))
	require.Equal(t, int64(1), secondsRoundedUp(time.Millisecond))
	require.Equal(t, int64(5), secondsRoundedUp(5*time.Second))
	require.Equal(t, int64(6), secondsRoundedUp(5*time.Second+time.Nanosecond))
}
