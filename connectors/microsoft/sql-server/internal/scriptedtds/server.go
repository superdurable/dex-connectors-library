// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package scriptedtds is a minimal Tabular Data Stream (TDS) server for deterministic tests.
//
// It speaks enough of TDS 7.4 and TDS 8.0 for the connector and the go-mssqldb driver: PRELOGIN
// with every encryption answer, TLS inside PRELOGIN packets or before any TDS byte, LOGIN7 with
// SQL authentication, an Azure SQL style routing redirect, SQL batches, sp_executesql RPC
// requests with typed parameters, transaction manager requests, and attention signals. A Script
// supplies statement results, errors, delays, and commit faults, and every received request is
// recorded so tests can assert what reached the server.
package scriptedtds

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultProductVersion is the SERVERPROPERTY('ProductVersion') the server reports unless Options override it.
	DefaultProductVersion = "16.0.4135.4"
	// FirstProcessID is the @@SPID of the first accepted connection; later ones count up.
	FirstProcessID = 51
	// sessionSettingsPrefix identifies the connector's own session settings batch.
	sessionSettingsPrefix = "SET LANGUAGE us_english;"
)

// Encryption is the server's answer to the client's PRELOGIN encryption request.
type Encryption int

const (
	// EncryptionNotSupported answers ENCRYPT_NOT_SUP, so the session stays plaintext.
	EncryptionNotSupported Encryption = iota
	// EncryptionOn answers ENCRYPT_ON and runs TLS inside PRELOGIN packets, as TDS 7.4 does.
	EncryptionOn
	// EncryptionStrict runs TLS before any TDS byte with ALPN tds/8.0, as TDS 8.0 does.
	EncryptionStrict
	// EncryptionLoginOnly answers ENCRYPT_OFF, which would encrypt only the login packet.
	EncryptionLoginOnly
)

// ColumnType is a SQL Server column type the server can encode.
type ColumnType int

// Column types the scripted server can encode.
const (
	TypeTinyInt ColumnType = iota + 1
	TypeSmallInt
	TypeInt
	TypeBigInt
	TypeBit
	TypeReal
	TypeFloat
	TypeDecimal
	TypeMoney
	TypeSmallMoney
	TypeNVarChar
	TypeNVarCharMax
	TypeVarChar
	TypeVarBinary
	TypeVarBinaryMax
	TypeBinary
	TypeUniqueIdentifier
	TypeDate
	TypeTime
	TypeDateTime2
	TypeDateTimeOffset
	TypeDateTime
	TypeSmallDateTime
	TypeXML
	TypeSQLVariant
	TypeGeography
)

// CommitAction is what the server does when a transaction's COMMIT arrives.
type CommitAction int

const (
	// CommitAndReply applies the transaction and acknowledges COMMIT.
	CommitAndReply CommitAction = iota
	// CommitThenDropConnection applies the transaction and closes the connection without replying.
	CommitThenDropConnection
	// DropConnectionWithoutCommitting discards the transaction and closes the connection without replying.
	DropConnectionWithoutCommitting
	// RejectCommitWithNoTransaction discards the transaction and replies with error 3902.
	RejectCommitWithNoTransaction
	// RejectCommitWithServerError replies with error 3999 (severity 17), after which the outcome is unknown to the client.
	RejectCommitWithServerError
)

// Event kinds recorded by the server.
const (
	EventPrelogin         = "prelogin"
	EventLogin            = "login"
	EventSessionSettings  = "sessionSettings"
	EventBatch            = "batch"
	EventRPC              = "rpc"
	EventBeginTransaction = "begin"
	EventCommit           = "commit"
	EventRollback         = "rollback"
	EventAttention        = "attention"
)

// Column is one scripted result column.
type Column struct {
	// Name is the reported column name.
	Name string
	// Type is the column type.
	Type ColumnType
	// Precision is a DECIMAL column's precision.
	Precision byte
	// Scale is a DECIMAL column's scale, or the fractional-second digits of TIME, DATETIME2, and
	// DATETIMEOFFSET; zero uses 7 for those types.
	Scale byte
	// Length is a character or binary column's maximum length; zero uses 4000 characters or 8000 bytes.
	Length int
}

// ServerError is an ERROR token the server sends instead of a result.
type ServerError struct {
	// Number is the SQL Server error number, such as 2627.
	Number int32
	// State is the error state.
	State byte
	// Severity is the error class; 20 and above end the connection.
	Severity byte
	// Message is the server message text.
	Message string
}

// Parameter is one RPC parameter the client sent.
type Parameter struct {
	// Name is the parameter name, such as @p1; sp_executesql's statement and declaration have none.
	Name string
	// TypeID is the TDS type the client declared, such as 0xe7 for nvarchar.
	TypeID byte
	// IsNull reports SQL NULL.
	IsNull bool
	// Text is the decoded value: decimal for integers, shortest text for floats, the string for
	// text, RFC 3339 for datetimeoffset, and hexadecimal for binary data.
	Text string
}

// Statement is one statement the client sent as a SQL batch or through sp_executesql.
type Statement struct {
	// SQL is the statement text.
	SQL string
	// Declarations is sp_executesql's @params text, such as "@p1 nvarchar(5),@p2 bigint".
	Declarations string
	// Parameters are the bound @pN values in order.
	Parameters []Parameter
	// IsBatch reports a SQL batch rather than an sp_executesql RPC.
	IsBatch bool
	// TransactionDescriptor is the transaction the request ran in, or zero.
	TransactionDescriptor uint64
}

// Execution scripts the result of one statement.
type Execution struct {
	// Columns are the result columns; empty means the statement returns no result set.
	Columns []Column
	// Rows are result values: nil, int64, bool, float32 for REAL, float64, decimal text, string,
	// []byte, or ISO 8601 text for temporal types.
	Rows [][]any
	// RowsAffected is the row count reported for a statement without columns.
	RowsAffected int64
	// TriggerRowsAffected adds a second row count, as an AFTER trigger without SET NOCOUNT ON reports.
	TriggerRowsAffected int64
	// Error replaces the result with an ERROR token when set; with Rows it follows them.
	Error *ServerError
	// ExtraResultSet sends a second one-column result set after the first.
	ExtraResultSet bool
	// DropConnection closes the connection instead of answering.
	DropConnection bool
	// Delay holds the reply; an attention that arrives meanwhile cancels the statement.
	Delay time.Duration
	// OnCommit runs when the enclosing transaction commits.
	OnCommit func()
	// OnRollback runs when the statement is canceled or its transaction ends any other way.
	OnRollback func()
}

// Script supplies every scripted response. Its functions must be safe for concurrent connections.
type Script struct {
	// Execute returns a statement's result.
	Execute func(statement Statement) Execution
	// Commit decides how COMMIT is handled; nil commits normally.
	Commit func() CommitAction
}

// Route redirects a client after login, as an Azure SQL gateway does.
type Route struct {
	// Host is the server name the client reconnects to.
	Host string
	// Port is the TCP port the client reconnects to.
	Port uint16
}

// Options configure one scripted server.
type Options struct {
	// Password is the only password that logs in.
	Password string
	// Encryption is the server's PRELOGIN encryption answer.
	Encryption Encryption
	// TLS serves EncryptionOn and EncryptionStrict.
	TLS *tls.Config
	// ProductVersion is reported by SERVERPROPERTY('ProductVersion'); blank uses DefaultProductVersion.
	ProductVersion string
	// LoginError, when set, rejects every login with this error.
	LoginError *ServerError
	// RouteTo, when set, redirects every client after a successful login.
	RouteTo *Route
	// SendsMalformedPrelogin answers PRELOGIN with an ENCRYPTION option that has no value.
	SendsMalformedPrelogin bool
}

// Event is one request the server received.
type Event struct {
	// Connection numbers connections from one in accept order.
	Connection int
	// Kind is one of the Event constants.
	Kind string
	// Text is the SQL of a batch or RPC, the user of a login, or the encryption of a prelogin.
	Text string
	// Database is the database a login requested.
	Database string
	// ApplicationName is the application name a login sent.
	ApplicationName string
	// IsEncrypted reports that the event arrived over TLS.
	IsEncrypted bool
	// Statement is the decoded statement of a batch or RPC event.
	Statement Statement
}

// Server accepts connections on a loopback port until Close.
type Server struct {
	listener         net.Listener
	options          Options
	script           Script
	mu               sync.Mutex
	events           []Event
	connectionNumber int
	serving          sync.WaitGroup
}

// Start listens on 127.0.0.1 and serves connections that log in with options.Password.
func Start(options Options, script Script) (*Server, error) {
	if script.Execute == nil {
		return nil, errors.New("scripted TDS requires Execute")
	}
	if (options.Encryption == EncryptionOn || options.Encryption == EncryptionStrict) && options.TLS == nil {
		return nil, errors.New("scripted TDS encryption requires a TLS configuration")
	}
	if options.ProductVersion == "" {
		options.ProductVersion = DefaultProductVersion
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	server := &Server{listener: listener, options: options, script: script}
	server.serving.Add(1)
	go server.acceptConnections()
	return server, nil
}

// Port returns the TCP port the server listens on.
func (server *Server) Port() int { return server.listener.Addr().(*net.TCPAddr).Port }

// Events returns a copy of every recorded event.
func (server *Server) Events() []Event {
	server.mu.Lock()
	defer server.mu.Unlock()
	return append([]Event(nil), server.events...)
}

// EventsOfKind returns the recorded events of one kind in order.
func (server *Server) EventsOfKind(kind string) []Event {
	var matching []Event
	for _, event := range server.Events() {
		if event.Kind == kind {
			matching = append(matching, event)
		}
	}
	return matching
}

// Close stops accepting connections and waits for open connections to finish.
func (server *Server) Close() error {
	err := server.listener.Close()
	server.serving.Wait()
	return err
}

func (server *Server) acceptConnections() {
	defer server.serving.Done()
	for {
		connection, err := server.listener.Accept()
		if err != nil {
			return
		}
		server.mu.Lock()
		server.connectionNumber++
		number := server.connectionNumber
		server.mu.Unlock()
		server.serving.Add(1)
		go func() {
			defer server.serving.Done()
			protocol := &session{server: server, number: number, raw: connection, transport: connection, done: make(chan struct{})}
			// Close the connection, and any TLS layer over it; the close error has no observer.
			defer func() { _ = protocol.raw.Close() }()
			// A session error ends the connection, which is the behavior the client observes.
			_ = protocol.serve()
		}()
	}
}

func (server *Server) record(event Event) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.events = append(server.events, event)
}

// clientMessage is one complete client message: the first packet's type and the joined payloads.
type clientMessage struct {
	packetType byte
	payload    []byte
}

// session is one client connection's protocol state.
type session struct {
	server      *Server
	number      int
	raw         net.Conn
	transport   io.ReadWriter
	isEncrypted bool
	requests    chan clientMessage
	done        chan struct{}
	writeMu     sync.Mutex

	transactionDescriptor uint64
	pendingCommits        []func()
	pendingRollbacks      []func()
}

var errConnectionDropped = errors.New("scripted connection dropped")

func (session *session) serve() error {
	defer session.endTransaction(false)
	defer close(session.done)
	if err := session.negotiate(); err != nil {
		return err
	}
	session.requests = make(chan clientMessage, 4)
	go session.readRequests()
	for message := range session.requests {
		if err := session.handle(message); err != nil {
			return err
		}
	}
	return nil
}

// negotiate runs PRELOGIN, any TLS handshake, and LOGIN7.
func (session *session) negotiate() error {
	options := session.server.options
	if options.Encryption == EncryptionStrict {
		config := options.TLS.Clone()
		config.NextProtos = []string{"tds/8.0"}
		tlsConnection := tls.Server(session.raw, config)
		if err := tlsConnection.Handshake(); err != nil {
			return err
		}
		session.transport, session.isEncrypted = tlsConnection, true
	}
	message, err := session.readMessage()
	if err != nil {
		return err
	}
	if message.packetType != packetPrelogin {
		return fmt.Errorf("expected PRELOGIN, got packet type %d", message.packetType)
	}
	requested, err := requestedEncryption(message.payload)
	if err != nil {
		return err
	}
	session.server.record(Event{Connection: session.number, Kind: EventPrelogin, Text: describeEncryption(requested), IsEncrypted: session.isEncrypted})
	answer := map[Encryption]byte{EncryptionNotSupported: 2, EncryptionOn: 1, EncryptionStrict: requested, EncryptionLoginOnly: 0}[options.Encryption]
	if err := session.writeMessage(preloginReply(answer, options.SendsMalformedPrelogin)); err != nil {
		return err
	}
	if options.Encryption == EncryptionOn && requested != 2 {
		if err := session.startTLSInsidePrelogin(); err != nil {
			return err
		}
	}
	if options.Encryption == EncryptionLoginOnly || (options.Encryption == EncryptionNotSupported && requested != 2) {
		// A client that requires encryption disconnects here; one that does not would send LOGIN7 in plaintext.
		if _, err := session.readMessage(); err != nil {
			return errConnectionDropped
		}
		return errors.New("scripted TDS does not serve unencrypted logins to a client that asked for encryption")
	}
	return session.login()
}

// startTLSInsidePrelogin runs the TLS handshake with its records wrapped in TDS packets, as TDS 7.4 does.
func (session *session) startTLSInsidePrelogin() error {
	config := session.server.options.TLS.Clone()
	config.MaxVersion = tls.VersionTLS12
	adapter := &handshakeAdapter{session: session, connection: session.raw}
	tlsConnection := tls.Server(adapter, config)
	if err := tlsConnection.Handshake(); err != nil {
		return err
	}
	adapter.isHandshakeComplete = true
	session.transport, session.isEncrypted = tlsConnection, true
	return nil
}

func (session *session) login() error {
	message, err := session.readMessage()
	if err != nil {
		return err
	}
	if message.packetType != packetLogin7 {
		return fmt.Errorf("expected LOGIN7, got packet type %d", message.packetType)
	}
	login, err := parseLogin7(message.payload)
	if err != nil {
		return err
	}
	session.server.record(Event{
		Connection: session.number, Kind: EventLogin, Text: login.user, Database: login.database,
		ApplicationName: login.applicationName, IsEncrypted: session.isEncrypted,
	})
	options := session.server.options
	reply := tokenWriter{}
	switch {
	case options.LoginError != nil:
		reply.message(tokenError, *options.LoginError)
		reply.done(tokenDone, doneError, 0, 0)
		return errors.Join(session.writeMessage(reply.buffer), errConnectionDropped)
	case login.password != options.Password:
		reply.message(tokenError, ServerError{Number: 18456, State: 1, Severity: 14, Message: fmt.Sprintf("Login failed for user '%s'.", login.user)})
		reply.done(tokenDone, doneError, 0, 0)
		return errors.Join(session.writeMessage(reply.buffer), errConnectionDropped)
	}
	reply.textEnvironmentChange(environmentDatabase, login.database, "master")
	reply.textEnvironmentChange(environmentPacketSize, "4096", "4096")
	reply.environmentChange(environmentCollation, []byte(collationLatin1General), nil)
	if err := reply.loginAcknowledgement(options.ProductVersion); err != nil {
		return err
	}
	if options.RouteTo != nil {
		route := tokenWriter{}
		route.byte(0)
		route.uint16(options.RouteTo.Port)
		route.shortText(options.RouteTo.Host)
		reply.byte(tokenEnvChange)
		reply.uint16(uint16(1 + 2 + len(route.buffer) + 2))
		reply.byte(environmentRouting)
		reply.uint16(uint16(len(route.buffer)))
		reply.bytes(route.buffer)
		reply.uint16(0)
	}
	reply.done(tokenDone, 0, 0, 0)
	return session.writeMessage(reply.buffer)
}

// readRequests forwards every client message, including attentions that arrive while a statement waits.
func (session *session) readRequests() {
	defer close(session.requests)
	for {
		message, err := session.readMessage()
		if err != nil {
			return
		}
		select {
		case session.requests <- message:
		case <-session.done:
			return
		}
	}
}

func (session *session) handle(message clientMessage) error {
	switch message.packetType {
	case packetSQLBatch:
		statement, err := parseBatch(message.payload)
		if err != nil {
			return err
		}
		if strings.HasPrefix(statement.SQL, sessionSettingsPrefix) {
			session.server.record(Event{Connection: session.number, Kind: EventSessionSettings, Text: statement.SQL, IsEncrypted: session.isEncrypted, Statement: statement})
			return session.writeMessage(session.sessionSettingsReply(statement.SQL))
		}
		session.server.record(Event{Connection: session.number, Kind: EventBatch, Text: statement.SQL, IsEncrypted: session.isEncrypted, Statement: statement})
		return session.execute(statement)
	case packetRPC:
		statement, err := parseExecuteSQL(message.payload)
		if err != nil {
			return err
		}
		session.server.record(Event{Connection: session.number, Kind: EventRPC, Text: statement.SQL, IsEncrypted: session.isEncrypted, Statement: statement})
		return session.execute(statement)
	case packetTransactionManager:
		return session.handleTransactionRequest(message.payload)
	case packetAttention:
		session.server.record(Event{Connection: session.number, Kind: EventAttention, IsEncrypted: session.isEncrypted})
		reply := tokenWriter{}
		reply.done(tokenDone, doneAttention, 0, 0)
		return session.writeMessage(reply.buffer)
	default:
		return fmt.Errorf("unexpected packet type %d", message.packetType)
	}
}

// sessionSettingsReply answers each SET with a DONE and the identity SELECT with one row.
func (session *session) sessionSettingsReply(batch string) []byte {
	reply := tokenWriter{}
	for range strings.Count(batch, ";") {
		reply.done(tokenDone, doneMore, 0, 0)
	}
	reply.textEnvironmentChange(environmentLanguage, "us_english", "")
	reply.message(tokenInfo, ServerError{Number: 5703, State: 1, Severity: 0, Message: "Changed language setting to us_english."})
	columns := []Column{{Name: "", Type: TypeSmallInt}, {Name: "", Type: TypeNVarChar, Length: 128}}
	// The fixed scripted columns always encode.
	_ = reply.columnMetadata(columns)
	_ = reply.row(columns, []any{int64(FirstProcessID - 1 + session.number), session.server.options.ProductVersion})
	reply.done(tokenDone, doneCount, commandSelect, 1)
	return reply.buffer
}

// execute runs one scripted statement inside the session's transaction, if any.
func (session *session) execute(statement Statement) error {
	statement.TransactionDescriptor = session.transactionDescriptor
	execution := session.server.script.Execute(statement)
	if execution.DropConnection {
		return errConnectionDropped
	}
	if execution.Delay > 0 {
		isCanceled, err := session.waitUnlessCanceled(execution.Delay)
		if err != nil {
			runIfSet(execution.OnRollback)
			return err
		}
		if isCanceled {
			runIfSet(execution.OnRollback)
			reply := tokenWriter{}
			reply.done(tokenDone, doneAttention, 0, 0)
			return session.writeMessage(reply.buffer)
		}
	}
	reply, err := statementReply(statement, execution)
	if err != nil {
		return err
	}
	if execution.Error == nil {
		session.track(execution)
	} else {
		runIfSet(execution.OnRollback)
	}
	if err := session.writeMessage(reply); err != nil {
		return err
	}
	if execution.Error != nil && execution.Error.Severity >= 20 {
		return errConnectionDropped
	}
	return nil
}

// waitUnlessCanceled waits for delay and reports whether an attention arrived first.
func (session *session) waitUnlessCanceled(delay time.Duration) (bool, error) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return false, nil
	case message, isOpen := <-session.requests:
		if !isOpen {
			return false, errConnectionDropped
		}
		if message.packetType != packetAttention {
			return false, fmt.Errorf("unexpected packet type %d during a statement", message.packetType)
		}
		session.server.record(Event{Connection: session.number, Kind: EventAttention, IsEncrypted: session.isEncrypted})
		return true, nil
	}
}

func (session *session) track(execution Execution) {
	if session.transactionDescriptor == 0 {
		runIfSet(execution.OnCommit)
		return
	}
	if execution.OnCommit != nil {
		session.pendingCommits = append(session.pendingCommits, execution.OnCommit)
	}
	if execution.OnRollback != nil {
		session.pendingRollbacks = append(session.pendingRollbacks, execution.OnRollback)
	}
}

func (session *session) handleTransactionRequest(payload []byte) error {
	reader := requestReader{buffer: payload}
	if _, err := reader.skipAllHeaders(); err != nil {
		return err
	}
	requestType, err := reader.uint16()
	if err != nil {
		return err
	}
	reply := tokenWriter{}
	switch requestType {
	case transactionBegin:
		session.endTransaction(false)
		session.transactionDescriptor = uint64(0x1000 + session.number)
		session.server.record(Event{Connection: session.number, Kind: EventBeginTransaction, IsEncrypted: session.isEncrypted})
		reply.environmentChange(environmentBegin, binary.LittleEndian.AppendUint64(nil, session.transactionDescriptor), nil)
	case transactionCommit:
		action := CommitAndReply
		if session.server.script.Commit != nil {
			action = session.server.script.Commit()
		}
		session.server.record(Event{Connection: session.number, Kind: EventCommit, Text: describeCommitAction(action), IsEncrypted: session.isEncrypted})
		switch action {
		case CommitThenDropConnection:
			session.endTransaction(true)
			return errConnectionDropped
		case DropConnectionWithoutCommitting:
			session.endTransaction(false)
			return errConnectionDropped
		case RejectCommitWithNoTransaction:
			session.endTransaction(false)
			reply.message(tokenError, ServerError{Number: 3902, State: 1, Severity: 16, Message: "The COMMIT TRANSACTION request has no corresponding BEGIN TRANSACTION."})
			reply.done(tokenDone, doneError, 0, 0)
			return session.writeMessage(reply.buffer)
		case RejectCommitWithServerError:
			session.endTransaction(false)
			reply.message(tokenError, ServerError{Number: 3999, State: 1, Severity: 17, Message: "Failed to flush the commit table to disk in dbid 5 due to error 1105."})
			reply.done(tokenDone, doneError, 0, 0)
			return session.writeMessage(reply.buffer)
		}
		descriptor := session.transactionDescriptor
		session.endTransaction(true)
		reply.environmentChange(environmentCommit, nil, binary.LittleEndian.AppendUint64(nil, descriptor))
	case transactionRollback:
		descriptor := session.transactionDescriptor
		session.server.record(Event{Connection: session.number, Kind: EventRollback, IsEncrypted: session.isEncrypted})
		session.endTransaction(false)
		reply.environmentChange(environmentRollback, nil, binary.LittleEndian.AppendUint64(nil, descriptor))
	default:
		return fmt.Errorf("unsupported transaction manager request %d", requestType)
	}
	reply.done(tokenDone, 0, 0, 0)
	return session.writeMessage(reply.buffer)
}

// endTransaction runs the commit or rollback hooks of every statement in the open transaction.
func (session *session) endTransaction(isCommitted bool) {
	hooks := session.pendingRollbacks
	if isCommitted {
		hooks = session.pendingCommits
	}
	session.pendingCommits, session.pendingRollbacks, session.transactionDescriptor = nil, nil, 0
	for _, hook := range hooks {
		hook()
	}
}

// statementReply encodes the result tokens of one statement.
func statementReply(statement Statement, execution Execution) ([]byte, error) {
	reply := tokenWriter{}
	statementDone, finalDone := tokenDoneInProc, tokenDoneProc
	if statement.IsBatch {
		statementDone, finalDone = tokenDone, tokenDone
	}
	if len(execution.Columns) > 0 {
		if err := reply.columnMetadata(execution.Columns); err != nil {
			return nil, err
		}
		for _, row := range execution.Rows {
			if err := reply.row(execution.Columns, row); err != nil {
				return nil, err
			}
		}
	}
	if execution.Error != nil {
		reply.message(tokenError, *execution.Error)
		reply.done(statementDone, doneMore|doneError, commandInsert, 0)
		if !statement.IsBatch {
			reply.byte(tokenReturnStatus)
			reply.uint32(0)
		}
		reply.done(finalDone, doneError, 0, 0)
		return reply.buffer, nil
	}
	if execution.TriggerRowsAffected > 0 {
		reply.done(tokenDoneInProc, doneMore|doneCount, commandInsert, uint64(execution.TriggerRowsAffected))
	}
	command, rowCount := commandInsert, uint64(execution.RowsAffected)
	if len(execution.Columns) > 0 {
		rowCount = uint64(len(execution.Rows))
	}
	reply.done(statementDone, doneMore|doneCount, command, rowCount)
	if execution.ExtraResultSet {
		extra := []Column{{Name: "extra", Type: TypeInt}}
		if err := reply.columnMetadata(extra); err != nil {
			return nil, err
		}
		if err := reply.row(extra, []any{int64(1)}); err != nil {
			return nil, err
		}
		reply.done(statementDone, doneMore|doneCount, commandSelect, 1)
	}
	if !statement.IsBatch {
		reply.byte(tokenReturnStatus)
		reply.uint32(0)
	}
	reply.done(finalDone, 0, 0, 0)
	return reply.buffer, nil
}

// readMessage reads packets until one carries the end-of-message status.
func (session *session) readMessage() (clientMessage, error) {
	var message clientMessage
	for {
		header := make([]byte, 8)
		if _, err := io.ReadFull(session.transport, header); err != nil {
			return clientMessage{}, err
		}
		size := int(binary.BigEndian.Uint16(header[2:]))
		if size < 8 {
			return clientMessage{}, errors.New("packet shorter than its header")
		}
		body := make([]byte, size-8)
		if _, err := io.ReadFull(session.transport, body); err != nil {
			return clientMessage{}, err
		}
		if message.payload == nil {
			message.packetType = header[0]
		}
		message.payload = append(message.payload, body...)
		if header[1]&statusEndOfMessage != 0 {
			return message, nil
		}
	}
}

// writeMessage splits a reply into packets no larger than the negotiated packet size.
func (session *session) writeMessage(payload []byte) error {
	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	return writePackets(session.transport, packetReply, payload)
}

func writePackets(writer io.Writer, packetType byte, payload []byte) error {
	var framed bytes.Buffer
	sequence := byte(1)
	for {
		chunk := payload
		if len(chunk) > packetSize-8 {
			chunk = chunk[:packetSize-8]
		}
		payload = payload[len(chunk):]
		status := byte(0)
		if len(payload) == 0 {
			status = statusEndOfMessage
		}
		header := []byte{packetType, status, 0, 0, 0, 0, sequence, 0}
		binary.BigEndian.PutUint16(header[2:], uint16(8+len(chunk)))
		framed.Write(header)
		framed.Write(chunk)
		sequence++
		if len(payload) == 0 {
			_, err := writer.Write(framed.Bytes())
			return err
		}
	}
}

// handshakeAdapter carries TLS handshake records inside TDS packets until the handshake completes.
type handshakeAdapter struct {
	session             *session
	connection          net.Conn
	pending             []byte
	isHandshakeComplete bool
}

// Read returns handshake records from PRELOGIN packets, then raw bytes once the handshake completes.
func (adapter *handshakeAdapter) Read(buffer []byte) (int, error) {
	if adapter.isHandshakeComplete {
		return adapter.connection.Read(buffer)
	}
	if len(adapter.pending) == 0 {
		message, err := adapter.session.readRawMessage(adapter.connection)
		if err != nil {
			return 0, err
		}
		adapter.pending = message.payload
	}
	count := copy(buffer, adapter.pending)
	adapter.pending = adapter.pending[count:]
	return count, nil
}

// Write wraps each handshake flight in one TDS reply message, then writes raw bytes once the handshake completes.
func (adapter *handshakeAdapter) Write(buffer []byte) (int, error) {
	if adapter.isHandshakeComplete {
		return adapter.connection.Write(buffer)
	}
	if err := writePackets(adapter.connection, packetReply, buffer); err != nil {
		return 0, err
	}
	return len(buffer), nil
}

// Close closes the raw connection.
func (adapter *handshakeAdapter) Close() error { return adapter.connection.Close() }

// LocalAddr returns the raw connection's local address.
func (adapter *handshakeAdapter) LocalAddr() net.Addr { return adapter.connection.LocalAddr() }

// RemoteAddr returns the raw connection's remote address.
func (adapter *handshakeAdapter) RemoteAddr() net.Addr { return adapter.connection.RemoteAddr() }

// SetDeadline sets the raw connection's deadline.
func (adapter *handshakeAdapter) SetDeadline(deadline time.Time) error {
	return adapter.connection.SetDeadline(deadline)
}

// SetReadDeadline sets the raw connection's read deadline.
func (adapter *handshakeAdapter) SetReadDeadline(deadline time.Time) error {
	return adapter.connection.SetReadDeadline(deadline)
}

// SetWriteDeadline sets the raw connection's write deadline.
func (adapter *handshakeAdapter) SetWriteDeadline(deadline time.Time) error {
	return adapter.connection.SetWriteDeadline(deadline)
}

// readRawMessage reads one PRELOGIN-wrapped handshake message directly from the raw connection.
func (session *session) readRawMessage(connection net.Conn) (clientMessage, error) {
	original := session.transport
	session.transport = connection
	defer func() { session.transport = original }()
	return session.readMessage()
}

type loginRecord struct {
	user            string
	password        string
	database        string
	applicationName string
}

// parseLogin7 decodes the LOGIN7 fields the scripted server checks and records.
func parseLogin7(payload []byte) (loginRecord, error) {
	if len(payload) < 94 {
		return loginRecord{}, errors.New("LOGIN7 is shorter than its fixed header")
	}
	field := func(offset int, isPassword bool) (string, error) {
		start := int(binary.LittleEndian.Uint16(payload[offset:]))
		length := 2 * int(binary.LittleEndian.Uint16(payload[offset+2:]))
		if start+length > len(payload) {
			return "", errors.New("LOGIN7 field exceeds the message")
		}
		encoded := append([]byte(nil), payload[start:start+length]...)
		if isPassword {
			for index, mangled := range encoded {
				unmasked := mangled ^ 0xa5
				encoded[index] = unmasked<<4 | unmasked>>4
			}
		}
		return decodeUCS2(encoded)
	}
	var record loginRecord
	var err error
	if record.user, err = field(40, false); err != nil {
		return loginRecord{}, err
	}
	if record.password, err = field(44, true); err != nil {
		return loginRecord{}, err
	}
	if record.applicationName, err = field(48, false); err != nil {
		return loginRecord{}, err
	}
	if record.database, err = field(68, false); err != nil {
		return loginRecord{}, err
	}
	return record, nil
}

func parseBatch(payload []byte) (Statement, error) {
	reader := requestReader{buffer: payload}
	descriptor, err := reader.skipAllHeaders()
	if err != nil {
		return Statement{}, err
	}
	text, err := decodeUCS2(payload[reader.offset:])
	return Statement{SQL: text, IsBatch: true, TransactionDescriptor: descriptor}, err
}

// parseExecuteSQL decodes an sp_executesql RPC request into its statement, declarations, and parameters.
func parseExecuteSQL(payload []byte) (Statement, error) {
	reader := requestReader{buffer: payload}
	descriptor, err := reader.skipAllHeaders()
	if err != nil {
		return Statement{}, err
	}
	procedureSwitch, err := reader.uint16()
	if err != nil {
		return Statement{}, err
	}
	procedureID, err := reader.uint16()
	if err != nil {
		return Statement{}, err
	}
	if procedureSwitch != lengthMax || procedureID != executeSQLProcedureID {
		return Statement{}, errors.New("scripted TDS serves only sp_executesql RPC requests")
	}
	if _, err := reader.uint16(); err != nil {
		return Statement{}, err
	}
	var parameters []Parameter
	for reader.offset < len(reader.buffer) {
		parameter, err := reader.parameter()
		if err != nil {
			return Statement{}, err
		}
		parameters = append(parameters, parameter)
	}
	if len(parameters) < 2 {
		return Statement{}, errors.New("sp_executesql needs a statement and declarations")
	}
	return Statement{
		SQL: parameters[0].Text, Declarations: parameters[1].Text, Parameters: parameters[2:], TransactionDescriptor: descriptor,
	}, nil
}

// requestedEncryption returns the ENCRYPTION option the client sent.
func requestedEncryption(payload []byte) (byte, error) {
	for offset := 0; offset+5 <= len(payload) && payload[offset] != preloginTerminator; offset += 5 {
		if payload[offset] != preloginEncryption {
			continue
		}
		position := int(binary.BigEndian.Uint16(payload[offset+1:]))
		if position >= len(payload) {
			return 0, errors.New("PRELOGIN encryption option exceeds the message")
		}
		return payload[position], nil
	}
	return 0, errors.New("PRELOGIN has no encryption option")
}

// preloginReply answers with VERSION, ENCRYPTION, INSTOPT, and MARS.
func preloginReply(encryption byte, isMalformed bool) []byte {
	options := []struct {
		token byte
		value []byte
	}{
		{0, []byte{16, 0, 0x10, 0x27, 0, 0}},
		{preloginEncryption, []byte{encryption}},
		{2, []byte{0}},
		{4, []byte{0}},
	}
	if isMalformed {
		options[1].value = nil
	}
	headerSize := 5*len(options) + 1
	var header, data []byte
	for _, option := range options {
		header = append(header, option.token)
		header = binary.BigEndian.AppendUint16(header, uint16(headerSize+len(data)))
		header = binary.BigEndian.AppendUint16(header, uint16(len(option.value)))
		data = append(data, option.value...)
	}
	return append(append(header, preloginTerminator), data...)
}

func describeEncryption(value byte) string {
	switch value {
	case 0:
		return "off"
	case 1:
		return "on"
	case 2:
		return "notSupported"
	case 3:
		return "required"
	case 4:
		return "strict"
	default:
		return fmt.Sprintf("unknown(%d)", value)
	}
}

func describeCommitAction(action CommitAction) string {
	switch action {
	case CommitThenDropConnection:
		return "commitThenDropConnection"
	case DropConnectionWithoutCommitting:
		return "dropConnectionWithoutCommitting"
	case RejectCommitWithNoTransaction:
		return "rejectCommitWithNoTransaction"
	case RejectCommitWithServerError:
		return "rejectCommitWithServerError"
	default:
		return "commitAndReply"
	}
}

func runIfSet(hook func()) {
	if hook != nil {
		hook()
	}
}
