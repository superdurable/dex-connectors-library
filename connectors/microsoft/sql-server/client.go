// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sqlserver connects Dex applications to Microsoft SQL Server, Azure SQL Database, and
// Azure SQL Managed Instance.
//
// The connector exposes two operations. QueryRows runs one parameterized SELECT inside a
// transaction that it always rolls back and returns bounded rows. ExecuteStatement runs one
// parameterized INSERT, UPDATE, DELETE, or MERGE in its own transaction, commits it, and reports
// the rows it affected and any OUTPUT rows. Both bind values only through the positional
// parameters @p1 through @pN, which SQL Server receives as typed sp_executesql parameters, and map
// every result column to a JSON value without losing precision.
//
// Each operation opens one connection, runs one transaction, and closes the connection. The
// connector keeps no pool, so a rotated password takes effect on the next Step.
package sqlserver

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	applicationName = "dex-sql-server-connector"
	// trustedRootsEnvironmentVariable names a PEM bundle that replaces the system roots for mandatory and strict.
	trustedRootsEnvironmentVariable = "SQLSERVER_SSL_CA"
	// commitAllowance bounds the COMMIT round trip after the statement's own timeout.
	commitAllowance = 5 * time.Second
	// closeTimeout bounds draining a canceled result and closing the connection.
	closeTimeout = time.Second
	// protocolOverheadBytes allows row packets and TLS records to carry framing beyond maxResponseBytes.
	protocolOverheadBytes = 64 << 10
	// keepAliveInterval detects a silently dropped TCP connection during a long statement.
	keepAliveInterval = 15 * time.Second
	// maximumLockTimeoutMilliseconds is the largest value SET LOCK_TIMEOUT accepts.
	maximumLockTimeoutMilliseconds = 1<<31 - 1
)

// sessionSettingsStatements pin the options the type mapping, row counts, and error classification depend on.
var sessionSettingsStatements = []string{
	"SET LANGUAGE us_english",
	"SET DATEFORMAT ymd",
	"SET XACT_ABORT ON",
	"SET NOCOUNT OFF",
	"SET IMPLICIT_TRANSACTIONS OFF",
	"SET ANSI_NULLS ON",
	"SET ANSI_PADDING ON",
	"SET ANSI_WARNINGS ON",
	"SET ARITHABORT ON",
	"SET CONCAT_NULL_YIELDS_NULL ON",
	"SET QUOTED_IDENTIFIER ON",
	"SET NUMERIC_ROUNDABORT OFF",
	"SET TEXTSIZE 2147483647",
	"SET TRANSACTION ISOLATION LEVEL READ COMMITTED",
}

// sessionIdentityStatement reads the receipt identity in the same round trip as the session settings.
const sessionIdentityStatement = "SELECT @@SPID, CAST(SERVERPROPERTY('ProductVersion') AS nvarchar(128))"

// Option supplies a non-serializable Client dependency.
type Option interface{ applyClientOption(*clientOptions) }

type clientOptions struct {
	now func() time.Time
}

type clockOption struct{ now func() time.Time }

func (option clockOption) applyClientOption(options *clientOptions) { options.now = option.now }

// WithClock replaces the clock used for the ObservedAt time of provider receipts.
func WithClock(now func() time.Time) Option { return clockOption{now: now} }

// Client runs SQL Server statements for one configured SQL authentication user.
//
// A Client is safe for concurrent use. It stores only non-secret settings and resolves the
// password from its CredentialProvider before every operation.
type Client struct {
	host             string
	port             int
	database         string
	user             string
	encrypt          Encrypt
	connectTimeout   time.Duration
	statementTimeout time.Duration
	maxRows          int
	maxResponseBytes int
	credentials      CredentialSource
	now              func() time.Time
}

// databaseSession is one authenticated connection that serves exactly one operation call.
type databaseSession struct {
	connection    driver.Conn
	meter         *connectionMeter
	openRows      driver.Rows
	processID     string
	serverVersion string
}

// New validates configuration and constructs a SQL Server client.
//
// Zero configuration fields take their manifest defaults. New returns an error for a missing
// host, database, or user, a host that includes a scheme, port, instance name, or several
// servers, a port outside 1 through 65535, a Windows account name, disabled encryption for a host
// that is not a loopback address, a statementTimeout that SET LOCK_TIMEOUT cannot express, or a
// nil credential provider. It never contacts the server.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, fmt.Errorf("sql server credential provider is required")
	}
	if err := validateHost(config.Host); err != nil {
		return nil, err
	}
	if config.Port < 1 || config.Port > 65535 {
		return nil, fmt.Errorf("sql server port must be from 1 through 65535")
	}
	if err := validateConnectionName("database", config.Database); err != nil {
		return nil, err
	}
	if err := validateConnectionName("user", config.User); err != nil {
		return nil, err
	}
	if strings.ContainsRune(config.User, '\\') {
		return nil, fmt.Errorf("sql server user must be a SQL authentication user; Windows accounts such as DOMAIN\\user are not supported")
	}
	if config.Encrypt == EncryptDisable && !isLocalHost(config.Host) {
		return nil, fmt.Errorf("sql server encrypt disable is allowed only for localhost, 127.0.0.1, or ::1")
	}
	if config.StatementTimeout < time.Millisecond {
		return nil, fmt.Errorf("sql server statementTimeout must be at least 1ms")
	}
	if millisecondsRoundedUp(config.StatementTimeout) > maximumLockTimeoutMilliseconds {
		return nil, fmt.Errorf("sql server statementTimeout cannot exceed %d milliseconds", maximumLockTimeoutMilliseconds)
	}
	if config.MaxRows > int64(maximumConfiguredRows) {
		return nil, fmt.Errorf("sql server maxRows cannot exceed %d", maximumConfiguredRows)
	}
	if config.MaxResponseBytes > int64(maximumConfiguredResponseBytes) {
		return nil, fmt.Errorf("sql server maxResponseBytes cannot exceed %d", maximumConfiguredResponseBytes)
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("sql server connector option is nil")
		}
		option.applyClientOption(&dependencies)
	}
	if dependencies.now == nil {
		return nil, fmt.Errorf("sql server connector clock is required")
	}
	return &Client{
		host: config.Host, port: int(config.Port), database: config.Database, user: config.User,
		encrypt: config.Encrypt, connectTimeout: config.ConnectTimeout, statementTimeout: config.StatementTimeout,
		maxRows: int(config.MaxRows), maxResponseBytes: int(config.MaxResponseBytes),
		credentials: credentials, now: dependencies.now,
	}, nil
}

// QueryRows returns the read query operation bound to this client.
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

// newStatementContext bounds one statement; when it expires the driver cancels the statement with a TDS attention.
func (client *Client) newStatementContext(callCtx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(callCtx, client.statementTimeout)
}

func (client *Client) resolvePassword(call sdkgo.Call) (string, bool) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Password.Reveal() == "" {
		return "", false
	}
	return credentials.Password.Reveal(), true
}

// openSession returns a non-nil session even on failure, so its meter can classify and release it.
func (client *Client) openSession(ctx context.Context, password string) (*databaseSession, sessionPhase, error) {
	deadline, _ := ctx.Deadline()
	session := &databaseSession{meter: newConnectionMeter(deadline, client.host)}
	driverConfig, err := client.newDriverConfig(password, session.meter)
	if err != nil {
		return session, phaseConnect, err
	}
	connector := mssql.NewConnectorConfig(driverConfig)
	connector.Dialer = session.meter
	connectCtx, cancelConnect := context.WithTimeout(ctx, client.connectTimeout)
	defer cancelConnect()
	err = callDriver(func() error {
		var connectErr error
		session.connection, connectErr = connector.Connect(connectCtx)
		return connectErr
	})
	if err != nil {
		session.connection = nil
		return session, phaseConnect, err
	}
	if err := session.pinSettingsAndReadIdentity(ctx, client.sessionSettingsBatch()); err != nil {
		return session, phaseBegin, err
	}
	return session, phaseBegin, nil
}

// newDriverConfig sets typed fields instead of parsing a connection string, so no value injects a keyword.
func (client *Client) newDriverConfig(password string, meter *connectionMeter) (msdsn.Config, error) {
	tlsConfig, encryption, err := client.newTLSConfig(meter)
	if err != nil {
		return msdsn.Config{}, err
	}
	activityID := make([]byte, 16)
	if _, err := rand.Read(activityID); err != nil {
		return msdsn.Config{}, errConnectionSettingsInvalid
	}
	return msdsn.Config{
		Host:                client.host,
		Port:                uint64(client.port),
		Database:            client.database,
		User:                client.user,
		Password:            password,
		Encryption:          encryption,
		TLSConfig:           tlsConfig,
		AppName:             applicationName,
		DisableRetry:        true,
		DialTimeout:         client.connectTimeout,
		KeepAlive:           keepAliveInterval,
		Protocols:           []string{"tcp"},
		Parameters:          map[string]string{},
		ProtocolParameters:  map[string]interface{}{},
		MultiSubnetFailover: false,
		ActivityID:          activityID,
		Encoding:            msdsn.EncodeParameters{Timezone: time.UTC},
		EpaEnabled:          false,
	}, nil
}

// newTLSConfig never offers Microsoft's optional mode, which can leave the session unencrypted.
func (client *Client) newTLSConfig(meter *connectionMeter) (*tls.Config, msdsn.Encryption, error) {
	if client.encrypt == EncryptDisable {
		return nil, msdsn.EncryptionDisabled, nil
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: client.host, DynamicRecordSizingDisabled: true}
	switch client.encrypt {
	case EncryptTrustServerCertificate:
		config.InsecureSkipVerify = true
		return config, msdsn.EncryptionRequired, nil
	case EncryptMandatory, EncryptStrict:
		roots, err := loadTrustedRoots()
		if err != nil {
			return nil, 0, err
		}
		// The connector verifies the chain and name itself so a rejection is classified however the driver wraps it.
		config.InsecureSkipVerify = true
		verifier := certificateVerifier{roots: roots, configuredHost: client.host, meter: meter}
		config.VerifyConnection = verifier.verify
		if client.encrypt == EncryptStrict {
			return config, msdsn.EncryptionStrict, nil
		}
		return config, msdsn.EncryptionRequired, nil
	default:
		return nil, 0, errConnectionSettingsInvalid
	}
}

// sessionSettingsBatch is one SQL batch: the pinned settings, LOCK_TIMEOUT, and the identity query.
func (client *Client) sessionSettingsBatch() string {
	statements := append(append([]string(nil), sessionSettingsStatements...),
		"SET LOCK_TIMEOUT "+strconv.FormatInt(millisecondsRoundedUp(client.statementTimeout), 10),
		sessionIdentityStatement,
	)
	return strings.Join(statements, "; ")
}

func (client *Client) receipt(call sdkgo.Call, session *databaseSession, failure classifiedFailure) sdkgo.Receipt {
	receipt := sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: ConnectorID, ObservedAt: client.now().UTC()}
	metadata := map[string]string{}
	if session != nil {
		if session.processID != "" {
			metadata["processId"] = session.processID
		}
		if isSafeReceiptValue(session.serverVersion) {
			metadata["serverVersion"] = session.serverVersion
		}
	}
	if failure.errorNumber != 0 {
		metadata["errorNumber"] = strconv.FormatInt(int64(failure.errorNumber), 10)
		metadata["errorState"] = strconv.FormatUint(uint64(failure.errorState), 10)
		metadata["errorSeverity"] = strconv.FormatUint(uint64(failure.errorSeverity), 10)
	}
	if len(metadata) > 0 {
		receipt.Metadata = metadata
	}
	return receipt
}

// classifyFailure attributes a read the meter refused to an oversized reply rather than a lost connection.
func (session *databaseSession) classifyFailure(err error, phase sessionPhase) classifiedFailure {
	switch {
	case session.meter.hasRejectedCertificate():
		err = errors.Join(errCertificateRejected, err)
	case session.meter.hasExceededReadBudget():
		err = errors.Join(errServerReplyTooLarge, err)
	}
	return classifyFailure(err, phase)
}

// classifyStatementFailure attributes a canceled statement to statementTimeout when only the statement's own deadline passed.
func (session *databaseSession) classifyStatementFailure(err error, statementCtx context.Context, callCtx context.Context) classifiedFailure {
	if errors.Is(statementCtx.Err(), context.DeadlineExceeded) && callCtx.Err() == nil && !session.meter.hasExceededReadBudget() {
		err = errors.Join(errStatementTimedOut, err)
	}
	return session.classifyFailure(err, phaseStatement)
}

// pinSettingsAndReadIdentity runs the connector-owned settings batch and records @@SPID and the product version.
func (session *databaseSession) pinSettingsAndReadIdentity(ctx context.Context, batch string) error {
	values, err := session.queryConnectorRow(ctx, batch, 2)
	if err != nil {
		return err
	}
	processID, isInteger := values[0].(int64)
	version, isText := values[1].(string)
	if !isInteger || processID < 0 || !isText {
		return errSessionIdentityInvalid
	}
	session.processID, session.serverVersion = strconv.FormatInt(processID, 10), version
	return nil
}

// queryConnectorRow runs one connector-owned statement and returns its single row of columnCount values.
func (session *databaseSession) queryConnectorRow(ctx context.Context, statement string, columnCount int) ([]driver.Value, error) {
	prepared, err := session.prepareStatement(ctx, statement)
	if err != nil {
		return nil, err
	}
	rows, err := session.queryStatement(ctx, prepared, nil)
	if err != nil {
		return nil, err
	}
	values := make([]driver.Value, columnCount)
	err = callDriver(func() error {
		if len(rows.Columns()) != columnCount {
			return errSessionIdentityInvalid
		}
		if nextErr := rows.Next(values); nextErr != nil {
			if errors.Is(nextErr, io.EOF) {
				return errSessionIdentityInvalid
			}
			return nextErr
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := session.closeRows(); err != nil {
		return nil, err
	}
	return values, nil
}

func (session *databaseSession) beginTransaction(ctx context.Context) (driver.Tx, error) {
	beginner, isBeginner := session.connection.(driver.ConnBeginTx)
	if !isBeginner {
		return nil, errDriverInterfaceMissing
	}
	var transaction driver.Tx
	err := callDriver(func() error {
		var beginErr error
		transaction, beginErr = beginner.BeginTx(ctx, driver.TxOptions{})
		return beginErr
	})
	return transaction, err
}

// prepareStatement creates the driver statement; the driver sends nothing until it runs.
func (session *databaseSession) prepareStatement(ctx context.Context, sql string) (driver.Stmt, error) {
	preparer, isPreparer := session.connection.(driver.ConnPrepareContext)
	if !isPreparer {
		return nil, errDriverInterfaceMissing
	}
	var prepared driver.Stmt
	err := callDriver(func() error {
		var prepareErr error
		prepared, prepareErr = preparer.PrepareContext(ctx, sql)
		return prepareErr
	})
	return prepared, err
}

// queryStatement runs the statement and keeps its rows so close can drain them before closing the connection.
func (session *databaseSession) queryStatement(ctx context.Context, prepared driver.Stmt, parameters []driver.NamedValue) (driver.Rows, error) {
	queryer, isQueryer := prepared.(driver.StmtQueryContext)
	if !isQueryer {
		return nil, errDriverInterfaceMissing
	}
	var rows driver.Rows
	err := callDriver(func() error {
		var queryErr error
		rows, queryErr = queryer.QueryContext(ctx, parameters)
		return queryErr
	})
	if err != nil {
		return nil, err
	}
	session.openRows = rows
	return rows, nil
}

// execStatement runs the statement and returns the rows affected the server reported in its DONE tokens.
func (session *databaseSession) execStatement(ctx context.Context, prepared driver.Stmt, parameters []driver.NamedValue) (int64, error) {
	execer, isExecer := prepared.(driver.StmtExecContext)
	if !isExecer {
		return 0, errDriverInterfaceMissing
	}
	var rowsAffected int64
	err := callDriver(func() error {
		result, execErr := execer.ExecContext(ctx, parameters)
		if execErr != nil {
			return execErr
		}
		var countErr error
		rowsAffected, countErr = result.RowsAffected()
		return countErr
	})
	return rowsAffected, err
}

// closeRows drains a finished result so the driver's reader goroutine ends.
func (session *databaseSession) closeRows() error {
	rows := session.openRows
	if rows == nil {
		return nil
	}
	session.openRows = nil
	return callDriver(rows.Close)
}

// commitTransaction succeeds once COMMIT is acknowledged; only proof that no byte was sent means nothing committed.
func (session *databaseSession) commitTransaction(ctx context.Context, transaction driver.Tx) error {
	if ctx.Err() != nil || session.meter.isClosed() {
		return errCommitNotSent
	}
	bytesWrittenBeforeCommit := session.meter.writtenBytes()
	err := callDriver(transaction.Commit)
	if err != nil && session.meter.writtenBytes() == bytesWrittenBeforeCommit {
		return errCommitNotSent
	}
	return err
}

// close closes sockets, not the driver, whose Close pools a buffer its reader may still use.
func (session *databaseSession) close() {
	session.meter.shortenDeadline(closeTimeout)
	if session.openRows != nil {
		// A drain error is ignored: the outcome is already decided and the connection is closed next.
		_ = session.closeRows()
	}
	session.meter.closeConnections()
}

// certificateVerifier checks the chain and the host name, recording a rejection on the session's meter.
type certificateVerifier struct {
	roots          *x509.CertPool
	configuredHost string
	meter          *connectionMeter
}

// verify checks the client's configured name, which is the routed server after an Azure SQL redirect.
func (verifier certificateVerifier) verify(state tls.ConnectionState) error {
	name := state.ServerName
	if name == "" {
		name = verifier.configuredHost
	}
	if len(state.PeerCertificates) == 0 {
		verifier.meter.recordCertificateRejection()
		return errors.New("the server presented no certificate")
	}
	intermediates := x509.NewCertPool()
	for _, certificate := range state.PeerCertificates[1:] {
		intermediates.AddCert(certificate)
	}
	options := x509.VerifyOptions{DNSName: name, Roots: verifier.roots, Intermediates: intermediates}
	if _, err := state.PeerCertificates[0].Verify(options); err != nil {
		verifier.meter.recordCertificateRejection()
		return &tls.CertificateVerificationError{UnverifiedCertificates: state.PeerCertificates, Err: err}
	}
	return nil
}

// loadTrustedRoots rereads SQLSERVER_SSL_CA for every connection so a rotated bundle takes effect without a restart.
func loadTrustedRoots() (*x509.CertPool, error) {
	path := os.Getenv(trustedRootsEnvironmentVariable)
	if path == "" {
		return nil, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, errTrustedRootsUnreadable
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(contents) {
		return nil, errTrustedRootsUnreadable
	}
	return roots, nil
}

// callDriver converts a panic in the driver, such as one caused by a malformed server reply, into an error.
func callDriver(call func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = &driverPanicError{}
		}
	}()
	return call()
}

func validateHost(host string) error {
	if strings.TrimSpace(host) != host || host == "" {
		return fmt.Errorf("sql server host must not be blank or contain surrounding whitespace")
	}
	if strings.ContainsAny(host, ", ;'\"=") || strings.ContainsFunc(host, unicode.IsControl) {
		return fmt.Errorf("sql server host must name one server without commas, semicolons, quotes, spaces, or control characters; enter the port in the port field")
	}
	if strings.Contains(host, "://") || strings.HasPrefix(strings.ToLower(host), "tcp:") {
		return fmt.Errorf("sql server host must not include a scheme or protocol prefix such as tcp:")
	}
	if strings.ContainsRune(host, '\\') {
		return fmt.Errorf("sql server host must not name an instance; enter the instance's TCP port in the port field")
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return fmt.Errorf("sql server host must not include a port; use the port field")
	}
	return nil
}

func validateConnectionName(field string, value string) error {
	if strings.TrimSpace(value) == "" || strings.ContainsFunc(value, unicode.IsControl) {
		return fmt.Errorf("sql server %s must be non-blank and contain no control characters", field)
	}
	if utf8.RuneCountInString(value) > maximumIdentifierCharacters {
		return fmt.Errorf("sql server %s cannot exceed %d characters", field, maximumIdentifierCharacters)
	}
	return nil
}

func isLocalHost(host string) bool {
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
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
