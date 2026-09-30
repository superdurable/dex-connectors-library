// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package postgresql connects Dex applications to PostgreSQL databases.
//
// The connector exposes two operations. QueryRows runs one parameterized
// statement inside a read-only transaction and returns bounded rows.
// ExecuteStatement runs one parameterized write statement in its own
// transaction and reports the rows it affected. Both bind values only through
// positional $1..$n parameters, send every parameter as text that PostgreSQL
// parses for the parameter's inferred type, and read every result column as
// text, so numeric precision survives the round trip.
//
// Each operation opens one connection, runs one transaction, and closes the
// connection. The connector keeps no pool, so credential replacement takes
// effect on the next Step.
package postgresql

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	applicationName = "dex-postgresql-connector"
	// commitAllowance bounds the COMMIT round trip after the statement's own timeout.
	commitAllowance = 5 * time.Second
	// closeTimeout bounds the Terminate message sent when a connection closes.
	closeTimeout = time.Second
	// idleInTransactionTimeout lets the server release locks if a Worker stalls mid-transaction.
	idleInTransactionTimeout = 30 * time.Second
	// protocolOverheadBytes allows one wire message to carry framing beyond maxResponseBytes.
	protocolOverheadBytes = 64 << 10
)

// Option supplies a non-serializable Client dependency.
type Option interface{ applyClientOption(*clientOptions) }

type clientOptions struct {
	now func() time.Time
}

type clockOption struct{ now func() time.Time }

func (option clockOption) applyClientOption(options *clientOptions) { options.now = option.now }

// WithClock replaces the clock used for the ObservedAt time of provider receipts.
func WithClock(now func() time.Time) Option { return clockOption{now: now} }

// Client runs PostgreSQL statements for one configured database role.
//
// A Client is safe for concurrent use. It stores only non-secret settings and
// resolves the password from its CredentialProvider before every operation.
type Client struct {
	host             string
	port             uint16
	database         string
	user             string
	sslMode          SSLMode
	connectTimeout   time.Duration
	statementTimeout time.Duration
	maxRows          int
	maxResponseBytes int
	credentials      sdkgo.CredentialProvider[Credentials]
	now              func() time.Time
}

type transactionAccessMode int

const (
	transactionAccessDefault transactionAccessMode = iota
	transactionAccessReadOnly
)

// New validates configuration and constructs a PostgreSQL client.
//
// Zero configuration fields take their manifest defaults. New returns an error
// for a missing host, database, or user, a port outside 1 through 65535, a
// host that names several servers, disable TLS for a host that is not a
// loopback address or Unix-socket directory, or a nil credential provider. It
// never contacts the server.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, fmt.Errorf("postgresql credential provider is required")
	}
	if err := validateHost(config.Host); err != nil {
		return nil, err
	}
	if config.Port < 1 || config.Port > 65535 {
		return nil, fmt.Errorf("postgresql port must be from 1 through 65535")
	}
	if err := validateConnectionName("database", config.Database); err != nil {
		return nil, err
	}
	if err := validateConnectionName("user", config.User); err != nil {
		return nil, err
	}
	if config.SSLMode == SSLModeDisable && !isLocalHost(config.Host) {
		return nil, fmt.Errorf("postgresql sslMode disable is allowed only for localhost, 127.0.0.1, ::1, or a Unix-socket directory")
	}
	if config.StatementTimeout < time.Millisecond {
		return nil, fmt.Errorf("postgresql statementTimeout must be at least 1ms")
	}
	if config.MaxRows > int64(maximumConfiguredRows) {
		return nil, fmt.Errorf("postgresql maxRows cannot exceed %d", maximumConfiguredRows)
	}
	if config.MaxResponseBytes > int64(maximumConfiguredResponseBytes) {
		return nil, fmt.Errorf("postgresql maxResponseBytes cannot exceed %d", maximumConfiguredResponseBytes)
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("postgresql connector option is nil")
		}
		option.applyClientOption(&dependencies)
	}
	if dependencies.now == nil {
		return nil, fmt.Errorf("postgresql connector clock is required")
	}
	return &Client{
		host: config.Host, port: uint16(config.Port), database: config.Database, user: config.User,
		sslMode: config.SSLMode, connectTimeout: config.ConnectTimeout, statementTimeout: config.StatementTimeout,
		maxRows: int(config.MaxRows), maxResponseBytes: int(config.MaxResponseBytes),
		credentials: credentials, now: dependencies.now,
	}, nil
}

// QueryRows returns the read-only query operation bound to this client.
func (client *Client) QueryRows() QueryRowsOperation {
	return QueryRowsOperation{client: client}
}

// ExecuteStatement returns the transactional write operation bound to this client.
func (client *Client) ExecuteStatement() ExecuteStatementOperation {
	return ExecuteStatementOperation{client: client}
}

// newCallContext bounds the whole call so it returns a classified attempt before Dex's Execute timeout.
func (client *Client) newCallContext(call sdkgo.Call) (context.Context, context.CancelFunc) {
	return context.WithTimeout(call.Context, client.connectTimeout+client.statementTimeout+commitAllowance)
}

func (client *Client) resolvePassword(call sdkgo.Call) (string, bool) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Password.Reveal() == "" {
		return "", false
	}
	return credentials.Password.Reveal(), true
}

// connect opens one authenticated connection; PG* environment variables cannot replace configured fields.
func (client *Client) connect(ctx context.Context, password string) (*pgconn.PgConn, error) {
	config, err := client.newConnectionConfig(password)
	if err != nil {
		return nil, err
	}
	return pgconn.ConnectConfig(ctx, config)
}

func (client *Client) newConnectionConfig(password string) (*pgconn.Config, error) {
	settings := []string{
		"host=" + quoteConnectionSetting(client.host),
		"port=" + strconv.Itoa(int(client.port)),
		"dbname=" + quoteConnectionSetting(client.database),
		"user=" + quoteConnectionSetting(client.user),
		"password=" + quoteConnectionSetting(password),
		"sslmode=" + string(client.sslMode),
		"target_session_attrs=any",
	}
	config, err := pgconn.ParseConfig(strings.Join(settings, " "))
	if err != nil {
		return nil, errConnectionSettingsInvalid
	}
	config.ConnectTimeout = client.connectTimeout
	config.RuntimeParams = map[string]string{"application_name": applicationName, "client_encoding": "UTF8"}
	config.Fallbacks = nil
	config.ValidateConnect = nil
	config.AfterConnect = nil
	buildFrontend := config.BuildFrontend
	maximumMessageBytes := client.maxResponseBytes + protocolOverheadBytes
	config.BuildFrontend = func(reader io.Reader, writer io.Writer) *pgproto3.Frontend {
		frontend := buildFrontend(reader, writer)
		frontend.SetMaxBodyLen(maximumMessageBytes)
		return frontend
	}
	return config, nil
}

// beginTransaction opens the transaction and pins the session settings the type mapping depends on.
func (client *Client) beginTransaction(ctx context.Context, connection *pgconn.PgConn, accessMode transactionAccessMode) error {
	begin := "begin"
	if accessMode == transactionAccessReadOnly {
		begin = "begin transaction read only"
	}
	statements := []string{
		begin,
		"set local statement_timeout = " + strconv.FormatInt(millisecondsRoundedUp(client.statementTimeout), 10),
		"set local idle_in_transaction_session_timeout = " + strconv.FormatInt(millisecondsRoundedUp(idleInTransactionTimeout), 10),
		"set local timezone = 'UTC'",
		"set local datestyle = 'ISO, MDY'",
		"set local intervalstyle = 'iso_8601'",
		"set local bytea_output = 'hex'",
		"set local extra_float_digits = 3",
	}
	results, err := connection.Exec(ctx, strings.Join(statements, "; ")).ReadAll()
	for _, result := range results {
		if result.Err != nil {
			return result.Err
		}
	}
	return err
}

// commitTransaction succeeds once COMMIT is acknowledged; only a pre-send check proves nothing committed.
func (client *Client) commitTransaction(ctx context.Context, connection *pgconn.PgConn) error {
	if ctx.Err() != nil || connection.IsClosed() {
		return errCommitNotSent
	}
	results, err := connection.Exec(ctx, "commit").ReadAll()
	if len(results) > 0 {
		if results[0].Err != nil {
			return results[0].Err
		}
		if results[0].CommandTag.String() == "COMMIT" {
			return nil
		}
		if err == nil {
			return errCommitRolledBack
		}
	}
	return err
}

func (client *Client) closeConnection(connection *pgconn.PgConn) {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	// Close errors are ignored: the transaction outcome is already decided and the server discards the session.
	_ = connection.Close(ctx)
}

func (client *Client) receipt(call sdkgo.Call, connection *pgconn.PgConn, failure classifiedFailure) sdkgo.Receipt {
	receipt := sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: ConnectorID, ObservedAt: client.now().UTC()}
	metadata := map[string]string{}
	if connection != nil {
		metadata["backendPid"] = strconv.FormatUint(uint64(connection.PID()), 10)
		if version := connection.ParameterStatus("server_version"); isSafeReceiptValue(version) {
			metadata["serverVersion"] = version
		}
	}
	if failure.sqlState != "" {
		metadata["sqlState"] = failure.sqlState
	}
	if len(metadata) > 0 {
		receipt.Metadata = metadata
	}
	return receipt
}

func validateHost(host string) error {
	if strings.TrimSpace(host) != host || host == "" {
		return fmt.Errorf("postgresql host must not be blank or contain surrounding whitespace")
	}
	if strings.ContainsAny(host, ",=' \\") || strings.ContainsFunc(host, unicode.IsControl) {
		return fmt.Errorf("postgresql host must name one server without commas, quotes, spaces, or control characters")
	}
	if strings.Contains(host, "://") {
		return fmt.Errorf("postgresql host must not include a scheme")
	}
	return nil
}

func validateConnectionName(field string, value string) error {
	if strings.TrimSpace(value) == "" || strings.ContainsFunc(value, unicode.IsControl) {
		return fmt.Errorf("postgresql %s must be non-blank and contain no control characters", field)
	}
	return nil
}

func isLocalHost(host string) bool {
	if strings.HasPrefix(host, "/") || host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// quoteConnectionSetting escapes a libpq keyword/value setting so any byte sequence is taken literally.
func quoteConnectionSetting(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `\'`)
	return "'" + escaped + "'"
}

func millisecondsRoundedUp(duration time.Duration) int64 {
	return int64((duration + time.Millisecond - 1) / time.Millisecond)
}

func isSafeReceiptValue(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}
