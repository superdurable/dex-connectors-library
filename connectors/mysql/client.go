// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package mysql connects Dex applications to MySQL and MariaDB databases.
//
// The connector exposes two operations. QueryRows runs one parameterized
// statement inside a read-only transaction and returns bounded rows.
// ExecuteStatement runs one parameterized write statement in its own
// transaction and reports the rows it affected. Both bind values only through
// positional ? placeholders as server-side prepared-statement parameters, and
// map every result column to a JSON value without losing precision.
//
// Each operation opens one connection, runs one transaction, and closes the
// connection. The connector keeps no pool, so credential replacement takes
// effect on the next Step.
package mysql

import (
	"context"
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

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	applicationName = "dex-mysql-connector"
	// trustedRootsEnvironmentVariable names a PEM bundle that replaces the system roots for verify-ca and verify-identity.
	trustedRootsEnvironmentVariable = "MYSQL_SSL_CA"
	// commitAllowance bounds the COMMIT round trip after the statement's own timeout.
	commitAllowance = 5 * time.Second
	// closeTimeout bounds the COM_QUIT message sent when a connection closes.
	closeTimeout = time.Second
	// idleSessionTimeout lets the server end the session and roll back if a Worker stalls mid-transaction.
	idleSessionTimeout = 30 * time.Second
	// protocolOverheadBytes allows row packets to carry framing beyond maxResponseBytes.
	protocolOverheadBytes = 64 << 10
	// pinnedSQLMode is MySQL 8.0's default sql_mode, which MariaDB also accepts.
	pinnedSQLMode = "ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION"
	// identityStatement reads the receipt identity and the server flavor in one round trip.
	identityStatement = "SELECT CONNECTION_ID(), VERSION()"
	// statementOutcomeStatement reads what the previous statement on this session changed.
	statementOutcomeStatement = "SELECT ROW_COUNT(), LAST_INSERT_ID(), @@warning_count"
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

// Client runs MySQL or MariaDB statements for one configured account.
//
// A Client is safe for concurrent use. It stores only non-secret settings and
// resolves the password from its CredentialProvider before every operation.
type Client struct {
	host             string
	port             int
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

// databaseSession is one authenticated connection that serves exactly one operation call.
type databaseSession struct {
	connection    driver.Conn
	meter         *connectionMeter
	connectionID  string
	serverVersion string
}

// New validates configuration and constructs a MySQL client.
//
// Zero configuration fields take their manifest defaults. New returns an error
// for a missing host, database, or user, a host that includes a scheme, port,
// or several servers, a port outside 1 through 65535, disabled TLS for a host
// that is not a loopback address or Unix-socket path, TLS for a Unix-socket
// path, or a nil credential provider. It never contacts the server.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, fmt.Errorf("mysql credential provider is required")
	}
	if err := validateHost(config.Host); err != nil {
		return nil, err
	}
	if config.Port < 1 || config.Port > 65535 {
		return nil, fmt.Errorf("mysql port must be from 1 through 65535")
	}
	if err := validateConnectionName("database", config.Database); err != nil {
		return nil, err
	}
	if err := validateConnectionName("user", config.User); err != nil {
		return nil, err
	}
	if config.SSLMode == SSLModeDisabled && !isLocalHost(config.Host) {
		return nil, fmt.Errorf("mysql sslMode disabled is allowed only for localhost, 127.0.0.1, ::1, or a Unix-socket path")
	}
	if config.SSLMode != SSLModeDisabled && isSocketPath(config.Host) {
		return nil, fmt.Errorf("mysql Unix-socket path %q is local, so sslMode must be disabled", config.Host)
	}
	if config.StatementTimeout < time.Millisecond {
		return nil, fmt.Errorf("mysql statementTimeout must be at least 1ms")
	}
	if config.MaxRows > int64(maximumConfiguredRows) {
		return nil, fmt.Errorf("mysql maxRows cannot exceed %d", maximumConfiguredRows)
	}
	if config.MaxResponseBytes > int64(maximumConfiguredResponseBytes) {
		return nil, fmt.Errorf("mysql maxResponseBytes cannot exceed %d", maximumConfiguredResponseBytes)
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("mysql connector option is nil")
		}
		option.applyClientOption(&dependencies)
	}
	if dependencies.now == nil {
		return nil, fmt.Errorf("mysql connector clock is required")
	}
	return &Client{
		host: config.Host, port: int(config.Port), database: config.Database, user: config.User,
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

// openSession connects, reads the session identity, and pins the session settings the type mapping depends on.
//
// The returned session is never nil, so its meter can classify a failed connect.
func (client *Client) openSession(ctx context.Context, password string) (*databaseSession, sessionPhase, error) {
	deadline, _ := ctx.Deadline()
	session := &databaseSession{meter: newConnectionMeter(deadline)}
	driverConfig, err := client.newDriverConfig(password, session.meter)
	if err != nil {
		return session, phaseConnect, err
	}
	connector, err := mysqldriver.NewConnector(driverConfig)
	if err != nil {
		return session, phaseConnect, errConnectionSettingsInvalid
	}
	connectCtx, cancelConnect := context.WithTimeout(ctx, client.connectTimeout)
	defer cancelConnect()
	if session.connection, err = connector.Connect(connectCtx); err != nil {
		return session, phaseConnect, err
	}
	isMariaDB, err := session.readIdentity(ctx)
	if err != nil {
		return session, phaseBegin, err
	}
	if err := session.execText(ctx, client.sessionSettingsStatement(isMariaDB)); err != nil {
		return session, phaseBegin, err
	}
	return session, phaseBegin, nil
}

// newDriverConfig sets every security-relevant driver field explicitly instead of parsing a DSN.
func (client *Client) newDriverConfig(password string, meter *connectionMeter) (*mysqldriver.Config, error) {
	tlsConfig, err := client.newTLSConfig()
	if err != nil {
		return nil, err
	}
	config := mysqldriver.NewConfig()
	config.User = client.user
	config.Passwd = password
	config.DBName = client.database
	if isSocketPath(client.host) {
		config.Net, config.Addr = "unix", client.host
	} else {
		config.Net, config.Addr = "tcp", net.JoinHostPort(client.host, strconv.Itoa(client.port))
	}
	config.DialFunc = meter.dial
	config.TLS = tlsConfig
	config.Timeout = client.connectTimeout
	config.Loc = time.UTC
	config.Logger = &mysqldriver.NopLogger{}
	config.ConnectionAttributes = "program_name:" + applicationName
	config.AllowNativePasswords = true
	config.AllowCleartextPasswords = false
	config.AllowFallbackToPlaintext = false
	config.AllowAllFiles = false
	config.AllowOldPasswords = false
	config.CheckConnLiveness = false
	config.ClientFoundRows = false
	config.InterpolateParams = false
	config.MultiStatements = false
	config.ParseTime = false
	config.RejectReadOnly = false
	return config, nil
}

// newTLSConfig never falls back to plaintext: a server without TLS fails the connection.
func (client *Client) newTLSConfig() (*tls.Config, error) {
	if client.sslMode == SSLModeDisabled {
		return nil, nil
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if net.ParseIP(client.host) == nil {
		config.ServerName = client.host
	}
	switch client.sslMode {
	case SSLModeRequired:
		config.InsecureSkipVerify = true
	case SSLModeVerifyCa:
		roots, err := loadTrustedRoots()
		if err != nil {
			return nil, err
		}
		// Go's verifier always checks the host name, so verify-ca checks only the chain itself.
		config.InsecureSkipVerify = true
		config.VerifyConnection = func(state tls.ConnectionState) error { return verifyCertificateChain(state, roots) }
	case SSLModeVerifyIdentity:
		roots, err := loadTrustedRoots()
		if err != nil {
			return nil, err
		}
		config.RootCAs = roots
		config.ServerName = client.host
	default:
		return nil, errConnectionSettingsInvalid
	}
	return config, nil
}

// sessionSettingsStatement sets session variables; MySQL and MariaDB name the statement timeout differently.
func (client *Client) sessionSettingsStatement(isMariaDB bool) string {
	lockWaitSeconds := strconv.FormatInt(secondsRoundedUp(client.statementTimeout), 10)
	settings := []string{
		"time_zone = '+00:00'",
		"sql_mode = '" + pinnedSQLMode + "'",
		"wait_timeout = " + strconv.FormatInt(secondsRoundedUp(idleSessionTimeout), 10),
		"innodb_lock_wait_timeout = " + lockWaitSeconds,
		"lock_wait_timeout = " + lockWaitSeconds,
	}
	if isMariaDB {
		settings = append(settings, "max_statement_time = "+strconv.FormatFloat(client.statementTimeout.Seconds(), 'f', 6, 64))
	} else {
		settings = append(settings, "max_execution_time = "+strconv.FormatInt(millisecondsRoundedUp(client.statementTimeout), 10))
	}
	return "SET SESSION " + strings.Join(settings, ", ")
}

func (client *Client) receipt(call sdkgo.Call, session *databaseSession, failure classifiedFailure) sdkgo.Receipt {
	receipt := sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: ConnectorID, ObservedAt: client.now().UTC()}
	metadata := map[string]string{}
	if session != nil {
		if session.connectionID != "" {
			metadata["connectionId"] = session.connectionID
		}
		if isSafeReceiptValue(session.serverVersion) {
			metadata["serverVersion"] = session.serverVersion
		}
	}
	if failure.errorNumber != 0 {
		metadata["errorNumber"] = strconv.FormatUint(uint64(failure.errorNumber), 10)
	}
	if failure.sqlState != "" {
		metadata["sqlState"] = failure.sqlState
	}
	if len(metadata) > 0 {
		receipt.Metadata = metadata
	}
	return receipt
}

// classifyFailure attributes a read the meter refused to an oversized reply rather than a lost connection.
func (session *databaseSession) classifyFailure(err error, phase sessionPhase) classifiedFailure {
	if session.meter.hasExceededReadBudget() {
		err = errors.Join(errServerReplyTooLarge, err)
	}
	return classifyFailure(err, phase)
}

// readIdentity records the connection ID and server version and reports whether the server is MariaDB.
func (session *databaseSession) readIdentity(ctx context.Context) (bool, error) {
	values, err := session.queryTextRow(ctx, identityStatement, 2, errSessionIdentityInvalid)
	if err != nil {
		return false, err
	}
	connectionID, err := formatUnsignedInteger(values[0])
	if err != nil {
		return false, errSessionIdentityInvalid
	}
	version, isText := values[1].([]byte)
	if !isText {
		return false, errSessionIdentityInvalid
	}
	session.connectionID, session.serverVersion = connectionID, string(version)
	return strings.Contains(strings.ToLower(session.serverVersion), "mariadb"), nil
}

// readStatementOutcome works because the session is new: LAST_INSERT_ID() reflects only this statement.
func (session *databaseSession) readStatementOutcome(ctx context.Context) (statementOutcome, error) {
	values, err := session.queryTextRow(ctx, statementOutcomeStatement, 3, errStatementOutcomeInvalid)
	if err != nil {
		return statementOutcome{}, err
	}
	rowsAffected, isRowsAffectedInteger := values[0].(int64)
	lastInsertID, lastInsertIDErr := formatUnsignedInteger(values[1])
	warningCount, warningCountErr := formatUnsignedInteger(values[2])
	if !isRowsAffectedInteger || lastInsertIDErr != nil || warningCountErr != nil {
		return statementOutcome{}, errStatementOutcomeInvalid
	}
	warnings, err := strconv.ParseInt(warningCount, 10, 64)
	if err != nil {
		return statementOutcome{}, errStatementOutcomeInvalid
	}
	return statementOutcome{rowsAffected: rowsAffected, lastInsertID: lastInsertID, warningCount: warnings}, nil
}

// queryTextRow runs one connector-owned statement over the text protocol and returns its single row.
//
// A result without exactly one row of columnCount values returns errUnreadableResult.
func (session *databaseSession) queryTextRow(ctx context.Context, statement string, columnCount int, errUnreadableResult error) ([]driver.Value, error) {
	queryer, isQueryer := session.connection.(driver.QueryerContext)
	if !isQueryer {
		return nil, errDriverInterfaceMissing
	}
	rows, err := queryer.QueryContext(ctx, statement, nil)
	if err != nil {
		return nil, err
	}
	if len(rows.Columns()) != columnCount {
		return nil, errors.Join(errUnreadableResult, rows.Close())
	}
	values := make([]driver.Value, columnCount)
	if err := rows.Next(values); err != nil {
		if errors.Is(err, io.EOF) {
			err = errUnreadableResult
		}
		return nil, errors.Join(err, rows.Close())
	}
	row := make([]driver.Value, columnCount)
	for index, value := range values {
		if text, isText := value.([]byte); isText {
			value = append([]byte(nil), text...)
		}
		row[index] = value
	}
	return row, rows.Close()
}

func (session *databaseSession) execText(ctx context.Context, statement string) error {
	execer, isExecer := session.connection.(driver.ExecerContext)
	if !isExecer {
		return errDriverInterfaceMissing
	}
	_, err := execer.ExecContext(ctx, statement, nil)
	return err
}

func (session *databaseSession) beginTransaction(ctx context.Context, isReadOnly bool) (driver.Tx, error) {
	beginner, isBeginner := session.connection.(driver.ConnBeginTx)
	if !isBeginner {
		return nil, errDriverInterfaceMissing
	}
	return beginner.BeginTx(ctx, driver.TxOptions{ReadOnly: isReadOnly})
}

// prepareStatement asks the server to parse the statement; MySQL prepares exactly one statement.
func (session *databaseSession) prepareStatement(ctx context.Context, sql string) (driver.Stmt, error) {
	preparer, isPreparer := session.connection.(driver.ConnPrepareContext)
	if !isPreparer {
		return nil, errDriverInterfaceMissing
	}
	return preparer.PrepareContext(ctx, sql)
}

// commitTransaction succeeds once COMMIT is acknowledged; only proof that no byte was sent means nothing committed.
func (session *databaseSession) commitTransaction(ctx context.Context, transaction driver.Tx) error {
	if ctx.Err() != nil || session.meter.isClosed() {
		return errCommitNotSent
	}
	bytesWrittenBeforeCommit := session.meter.writtenBytes()
	err := transaction.Commit()
	if err != nil && session.meter.writtenBytes() == bytesWrittenBeforeCommit {
		return errCommitNotSent
	}
	return err
}

// close ends the session without COMMIT, so the server rolls back any open transaction.
func (session *databaseSession) close() {
	if session.connection == nil {
		return
	}
	session.meter.shortenDeadline(closeTimeout)
	// Close errors are ignored: the transaction outcome is already decided and the server discards the session.
	_ = session.connection.Close()
}

func verifyCertificateChain(state tls.ConnectionState, roots *x509.CertPool) error {
	if len(state.PeerCertificates) == 0 {
		return errors.New("the server presented no certificate")
	}
	intermediates := x509.NewCertPool()
	for _, certificate := range state.PeerCertificates[1:] {
		intermediates.AddCert(certificate)
	}
	if _, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates}); err != nil {
		// The platform verifier can return an untyped error, so wrap it the way crypto/tls does.
		return &tls.CertificateVerificationError{UnverifiedCertificates: state.PeerCertificates, Err: err}
	}
	return nil
}

// loadTrustedRoots rereads MYSQL_SSL_CA for every connection so a rotated bundle takes effect without a restart.
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

func validateHost(host string) error {
	if strings.TrimSpace(host) != host || host == "" {
		return fmt.Errorf("mysql host must not be blank or contain surrounding whitespace")
	}
	if strings.ContainsAny(host, ", '\"\\") || strings.ContainsFunc(host, unicode.IsControl) {
		return fmt.Errorf("mysql host must name one server without commas, quotes, spaces, or control characters")
	}
	if strings.Contains(host, "://") {
		return fmt.Errorf("mysql host must not include a scheme")
	}
	if isSocketPath(host) {
		return nil
	}
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return fmt.Errorf("mysql host must not include a port; use the port field")
	}
	return nil
}

func validateConnectionName(field string, value string) error {
	if strings.TrimSpace(value) == "" || strings.ContainsFunc(value, unicode.IsControl) {
		return fmt.Errorf("mysql %s must be non-blank and contain no control characters", field)
	}
	return nil
}

func isSocketPath(host string) bool {
	return strings.HasPrefix(host, "/")
}

func isLocalHost(host string) bool {
	if isSocketPath(host) || host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// formatUnsignedInteger returns the decimal text of an unsigned integer the driver decoded as int64, uint64, or text.
func formatUnsignedInteger(value driver.Value) (string, error) {
	switch typed := value.(type) {
	case int64:
		if typed < 0 {
			return "", errors.New("negative unsigned integer")
		}
		return strconv.FormatInt(typed, 10), nil
	case uint64:
		return strconv.FormatUint(typed, 10), nil
	case []byte:
		if _, err := strconv.ParseUint(string(typed), 10, 64); err != nil {
			return "", err
		}
		return string(typed), nil
	default:
		return "", errors.New("not an unsigned integer")
	}
}

func millisecondsRoundedUp(duration time.Duration) int64 {
	return int64((duration + time.Millisecond - 1) / time.Millisecond)
}

func secondsRoundedUp(duration time.Duration) int64 {
	return int64((duration + time.Second - 1) / time.Second)
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
