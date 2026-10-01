// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package scriptedmysql is a minimal MySQL client/server-protocol server for deterministic tests.
//
// It speaks enough of protocol 41 for the connector: mysql_native_password authentication with
// optional TLS, the connector's own text queries for session setup and transaction control, and
// server-side prepared statements with binary result rows. A Script supplies statement shapes,
// rows, errors, and commit faults, and every received command is recorded so tests can assert what
// reached the server.
package scriptedmysql

import (
	"bytes"
	"crypto/sha1"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Column types the scripted server can encode, using the protocol's MYSQL_TYPE_* numbers.
const (
	TypeTiny       byte = 0x01
	TypeShort      byte = 0x02
	TypeLong       byte = 0x03
	TypeFloat      byte = 0x04
	TypeDouble     byte = 0x05
	TypeNull       byte = 0x06
	TypeTimestamp  byte = 0x07
	TypeLongLong   byte = 0x08
	TypeInt24      byte = 0x09
	TypeDate       byte = 0x0a
	TypeTime       byte = 0x0b
	TypeDateTime   byte = 0x0c
	TypeYear       byte = 0x0d
	TypeBit        byte = 0x10
	TypeJSON       byte = 0xf5
	TypeNewDecimal byte = 0xf6
	TypeBlob       byte = 0xfc
	TypeVarString  byte = 0xfd
	TypeString     byte = 0xfe
)

const (
	// FlagUnsigned marks an unsigned integer column or parameter.
	FlagUnsigned uint16 = 0x20
	// CharsetBinary marks a binary string column, such as BLOB or VARBINARY.
	CharsetBinary byte = 63
	// CharsetUTF8MB4 is utf8mb4_0900_ai_ci, the text charset the server reports by default.
	CharsetUTF8MB4 byte = 255
	// DefaultServerVersion is the MySQL-flavored version the server reports unless Options override it.
	DefaultServerVersion = "8.4.0-scripted"
	// FirstConnectionID is the CONNECTION_ID() of the first accepted connection; later ones count up.
	FirstConnectionID = 1001
)

// Capability flags from the protocol's CLIENT_* constants.
const (
	capabilityLongPassword             uint32 = 1
	capabilityLongFlag                 uint32 = 4
	capabilityConnectWithDatabase      uint32 = 8
	capabilityProtocol41               uint32 = 512
	capabilitySSL                      uint32 = 2048
	capabilityTransactions             uint32 = 8192
	capabilitySecureConnection         uint32 = 32768
	capabilityMultiResults             uint32 = 1 << 17
	capabilityPluginAuth               uint32 = 1 << 19
	capabilityPluginAuthLenencData     uint32 = 1 << 21
	statusAutocommit                   uint16 = 0x0002
	statusInTransaction                uint16 = 0x0001
	commandQuit                        byte   = 0x01
	commandQuery                       byte   = 0x03
	commandPing                        byte   = 0x0e
	commandStatementPrepare            byte   = 0x16
	commandStatementExecute            byte   = 0x17
	commandStatementClose              byte   = 0x19
	commandStatementReset              byte   = 0x1a
	nativePasswordPlugin                      = "mysql_native_password"
	connectorIdentityStatement                = "select connection_id(), version()"
	connectorStatementOutcomeStatement        = "select row_count(), last_insert_id(), @@warning_count"
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
	// RejectCommitWithDeadlock discards the transaction and replies with error 1213 (SQLSTATE 40001).
	RejectCommitWithDeadlock
	// RejectCommitWithUnknownError replies with error 1180, after which the outcome is unknown to the client.
	RejectCommitWithUnknownError
)

// Event kinds recorded by the server.
const (
	EventHandshake = "handshake"
	EventQuery     = "query"
	EventPrepare   = "prepare"
	EventExecute   = "execute"
	EventCommit    = "commit"
	EventQuit      = "quit"
)

// Column is one scripted result column.
type Column struct {
	// Name is the reported column name.
	Name string
	// Type is a MYSQL_TYPE_* number such as TypeLongLong.
	Type byte
	// Flags holds column flags such as FlagUnsigned.
	Flags uint16
	// Charset is the reported character set; zero reports CharsetBinary for numbers and CharsetUTF8MB4 otherwise.
	Charset byte
	// Decimals is the reported fractional precision of a temporal or DECIMAL column.
	Decimals byte
}

// Shape is the server's answer to COM_STMT_PREPARE.
type Shape struct {
	// ParameterCount is the number of ? placeholders the server reports.
	ParameterCount int
}

// ServerError is an ERR packet the server sends instead of a result.
type ServerError struct {
	// Number is the server error number, such as 1062.
	Number uint16
	// SQLState is the five-character SQLSTATE, such as 23000.
	SQLState string
	// Message is the server message text.
	Message string
}

// Parameter is one bound prepared-statement parameter.
type Parameter struct {
	// Type is the MYSQL_TYPE_* number the client declared.
	Type byte
	// IsUnsigned reports the client's unsigned flag.
	IsUnsigned bool
	// IsNull reports SQL NULL.
	IsNull bool
	// Text is the decoded value: decimal for integers, shortest text for doubles, and raw bytes for strings.
	Text string
}

// Statement is one executed prepared statement with its bound parameters.
type Statement struct {
	// SQL is the statement text received in COM_STMT_PREPARE.
	SQL string
	// Parameters are the bound values in placeholder order.
	Parameters []Parameter
}

// Execution scripts the result of one executed statement.
type Execution struct {
	// Columns are the result columns; empty means the statement answers with an OK packet.
	Columns []Column
	// Rows are result values: nil, int64, uint64, float32, float64, string, or []byte. Temporal
	// columns take MySQL text such as "2026-01-01 05:30:00.123456".
	Rows [][]any
	// AffectedRows is the OK packet's affected rows and the following ROW_COUNT().
	AffectedRows int64
	// LastInsertID is the following LAST_INSERT_ID().
	LastInsertID uint64
	// Warnings is the following @@warning_count.
	Warnings uint16
	// Error replaces the result with an ERR packet when set.
	Error *ServerError
	// DropConnection closes the connection instead of answering COM_STMT_EXECUTE.
	DropConnection bool
	// OnCommit runs when the enclosing transaction commits.
	OnCommit func()
	// OnRollback runs when the enclosing transaction ends any other way, including a lost connection.
	OnRollback func()
}

// Script supplies every scripted response. Its functions must be safe for concurrent connections.
type Script struct {
	// Prepare returns a statement's shape, or an error such as a syntax error.
	Prepare func(sql string) (Shape, *ServerError)
	// Execute returns a statement's result.
	Execute func(statement Statement) Execution
	// Commit decides how COMMIT is handled; nil commits normally.
	Commit func() CommitAction
}

// Options configure one scripted server.
type Options struct {
	// Password is the only password that authenticates.
	Password string
	// ServerVersion is reported in the handshake and by VERSION(); blank uses DefaultServerVersion.
	ServerVersion string
	// TLS, when set, lets clients upgrade to TLS; nil makes the server refuse TLS.
	TLS *tls.Config
	// ReturnsEmptyIdentityResult answers SELECT CONNECTION_ID(), VERSION() with columns but no row.
	ReturnsEmptyIdentityResult bool
}

// Event is one command the server received.
type Event struct {
	// Connection numbers connections from one in accept order.
	Connection int
	// Kind is one of the Event constants.
	Kind string
	// Text is the SQL of a query, prepare, or execute event, the user of a handshake, or the commit action.
	Text string
	// Database is the default database a handshake requested.
	Database string
	// IsTLS reports that the event arrived over TLS.
	IsTLS bool
	// Parameters are the bound parameters of an execute event.
	Parameters []Parameter
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

// Start listens on 127.0.0.1 and serves connections that authenticate with options.Password.
func Start(options Options, script Script) (*Server, error) {
	if script.Prepare == nil || script.Execute == nil {
		return nil, errors.New("scripted MySQL requires Prepare and Execute")
	}
	if options.ServerVersion == "" {
		options.ServerVersion = DefaultServerVersion
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
			protocol := &session{server: server, number: number, connection: connection, statements: map[uint32]preparedStatement{}}
			// Close the TLS connection when the session upgraded; its close error has no observer.
			defer func() { _ = protocol.connection.Close() }()
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

// preparedStatement is one statement a session prepared.
type preparedStatement struct {
	sql            string
	parameterCount int
	parameterTypes []byte
}

// session is one client connection's protocol state.
type session struct {
	server           *Server
	number           int
	connection       net.Conn
	isTLS            bool
	sequence         byte
	isInTransaction  bool
	statements       map[uint32]preparedStatement
	nextStatementID  uint32
	lastOutcome      Execution
	hasResultSet     bool
	pendingCommits   []func()
	pendingRollbacks []func()
}

var errConnectionDropped = errors.New("scripted connection dropped")

func (session *session) serve() error {
	defer session.endTransaction(false)
	if err := session.authenticate(); err != nil {
		return err
	}
	for {
		session.sequence = 0
		packet, err := session.readPacket()
		if err != nil {
			return err
		}
		if len(packet) == 0 {
			return errors.New("empty command packet")
		}
		if err := session.handleCommand(packet[0], packet[1:]); err != nil {
			return err
		}
	}
}

func (session *session) authenticate() error {
	scramble := []byte("abcdefghij0123456789")
	if err := session.writePacket(session.handshakePacket(scramble)); err != nil {
		return err
	}
	response, err := session.readPacket()
	if err != nil {
		return err
	}
	if len(response) == 32 && binary.LittleEndian.Uint32(response)&capabilitySSL != 0 {
		if session.server.options.TLS == nil {
			return errors.New("client requested TLS that the server did not offer")
		}
		tlsConnection := tls.Server(session.connection, session.server.options.TLS)
		if err := tlsConnection.Handshake(); err != nil {
			return err
		}
		session.connection, session.isTLS = tlsConnection, true
		if response, err = session.readPacket(); err != nil {
			return err
		}
	}
	user, database, authResponse, err := parseHandshakeResponse(response)
	if err != nil {
		return err
	}
	session.server.record(Event{Connection: session.number, Kind: EventHandshake, Text: user, Database: database, IsTLS: session.isTLS})
	if !bytes.Equal(authResponse, nativePasswordScramble(session.server.options.Password, scramble)) {
		message := fmt.Sprintf("Access denied for user '%s'@'localhost' (using password: YES)", user)
		return errors.Join(session.writeError(&ServerError{Number: 1045, SQLState: "28000", Message: message}), errConnectionDropped)
	}
	return session.writeOK(0, 0, 0)
}

func (session *session) handshakePacket(scramble []byte) []byte {
	capabilities := capabilityLongPassword | capabilityLongFlag | capabilityConnectWithDatabase | capabilityProtocol41 |
		capabilityTransactions | capabilitySecureConnection | capabilityMultiResults | capabilityPluginAuth | capabilityPluginAuthLenencData
	if session.server.options.TLS != nil {
		capabilities |= capabilitySSL
	}
	packet := []byte{0x0a}
	packet = append(packet, session.server.options.ServerVersion...)
	packet = append(packet, 0)
	packet = binary.LittleEndian.AppendUint32(packet, uint32(FirstConnectionID-1+session.number))
	packet = append(packet, scramble[:8]...)
	packet = append(packet, 0)
	packet = binary.LittleEndian.AppendUint16(packet, uint16(capabilities))
	packet = append(packet, CharsetUTF8MB4)
	packet = binary.LittleEndian.AppendUint16(packet, statusAutocommit)
	packet = binary.LittleEndian.AppendUint16(packet, uint16(capabilities>>16))
	packet = append(packet, byte(len(scramble)+1))
	packet = append(packet, make([]byte, 10)...)
	packet = append(packet, scramble[8:]...)
	packet = append(packet, 0)
	packet = append(packet, nativePasswordPlugin...)
	return append(packet, 0)
}

func (session *session) handleCommand(command byte, body []byte) error {
	switch command {
	case commandQuit:
		session.server.record(Event{Connection: session.number, Kind: EventQuit, IsTLS: session.isTLS})
		return errConnectionDropped
	case commandPing, commandStatementReset:
		return session.writeOK(0, 0, 0)
	case commandStatementClose:
		return nil
	case commandQuery:
		return session.handleTextQuery(string(body))
	case commandStatementPrepare:
		return session.handlePrepare(string(body))
	case commandStatementExecute:
		return session.handleExecute(body)
	default:
		return session.writeError(&ServerError{Number: 1047, SQLState: "08S01", Message: "Unknown command"})
	}
}

func (session *session) handleTextQuery(text string) error {
	session.server.record(Event{Connection: session.number, Kind: EventQuery, Text: text, IsTLS: session.isTLS})
	statement := strings.ToLower(strings.TrimSpace(text))
	switch {
	case statement == connectorIdentityStatement:
		columns := []Column{{Name: "CONNECTION_ID()", Type: TypeLongLong, Flags: FlagUnsigned}, {Name: "VERSION()", Type: TypeVarString}}
		rows := [][]string{{strconv.Itoa(FirstConnectionID - 1 + session.number), session.server.options.ServerVersion}}
		if session.server.options.ReturnsEmptyIdentityResult {
			rows = nil
		}
		return session.writeTextResult(columns, rows...)
	case statement == connectorStatementOutcomeStatement:
		rowCount := session.lastOutcome.AffectedRows
		if session.hasResultSet {
			rowCount = -1
		}
		columns := []Column{
			{Name: "ROW_COUNT()", Type: TypeLongLong}, {Name: "LAST_INSERT_ID()", Type: TypeLongLong, Flags: FlagUnsigned},
			{Name: "@@warning_count", Type: TypeLongLong, Flags: FlagUnsigned},
		}
		row := []string{strconv.FormatInt(rowCount, 10), strconv.FormatUint(session.lastOutcome.LastInsertID, 10), strconv.Itoa(int(session.lastOutcome.Warnings))}
		return session.writeTextResult(columns, row)
	case strings.HasPrefix(statement, "set "):
		return session.writeOK(0, 0, 0)
	case strings.HasPrefix(statement, "start transaction"):
		session.isInTransaction = true
		return session.writeOK(0, 0, 0)
	case statement == "commit":
		return session.handleCommit()
	case statement == "rollback":
		session.endTransaction(false)
		session.isInTransaction = false
		return session.writeOK(0, 0, 0)
	default:
		return session.writeError(&ServerError{Number: 1064, SQLState: "42000", Message: "scripted server does not run this text query"})
	}
}

func (session *session) handleCommit() error {
	action := CommitAndReply
	if session.server.script.Commit != nil {
		action = session.server.script.Commit()
	}
	session.server.record(Event{Connection: session.number, Kind: EventCommit, Text: commitActionNames[action], IsTLS: session.isTLS})
	session.isInTransaction = false
	switch action {
	case CommitThenDropConnection:
		session.endTransaction(true)
		return errConnectionDropped
	case DropConnectionWithoutCommitting:
		session.endTransaction(false)
		return errConnectionDropped
	case RejectCommitWithDeadlock:
		session.endTransaction(false)
		return session.writeError(&ServerError{Number: 1213, SQLState: "40001", Message: "Deadlock found when trying to get lock; try restarting transaction"})
	case RejectCommitWithUnknownError:
		session.endTransaction(true)
		return session.writeError(&ServerError{Number: 1180, SQLState: "HY000", Message: "Got error during COMMIT"})
	default:
		session.endTransaction(true)
		return session.writeOK(0, 0, 0)
	}
}

func (session *session) handlePrepare(sql string) error {
	session.server.record(Event{Connection: session.number, Kind: EventPrepare, Text: sql, IsTLS: session.isTLS})
	shape, serverError := session.server.script.Prepare(sql)
	if serverError != nil {
		return session.writeError(serverError)
	}
	session.nextStatementID++
	session.statements[session.nextStatementID] = preparedStatement{sql: sql, parameterCount: shape.ParameterCount}
	response := []byte{0x00}
	response = binary.LittleEndian.AppendUint32(response, session.nextStatementID)
	response = binary.LittleEndian.AppendUint16(response, 0)
	response = binary.LittleEndian.AppendUint16(response, uint16(shape.ParameterCount))
	response = append(response, 0x00, 0x00, 0x00)
	if err := session.writePacket(response); err != nil {
		return err
	}
	if shape.ParameterCount == 0 {
		return nil
	}
	for range shape.ParameterCount {
		if err := session.writePacket(columnDefinition(Column{Name: "?", Type: TypeVarString})); err != nil {
			return err
		}
	}
	return session.writeEOF()
}

func (session *session) handleExecute(body []byte) error {
	if len(body) < 9 {
		return errors.New("short COM_STMT_EXECUTE")
	}
	statementID := binary.LittleEndian.Uint32(body)
	prepared, isPrepared := session.statements[statementID]
	if !isPrepared {
		return session.writeError(&ServerError{Number: 1243, SQLState: "HY000", Message: "Unknown prepared statement handler"})
	}
	parameters, types, err := parseExecuteParameters(body[9:], prepared)
	if err != nil {
		return err
	}
	prepared.parameterTypes = types
	session.statements[statementID] = prepared
	statement := Statement{SQL: prepared.sql, Parameters: parameters}
	session.server.record(Event{Connection: session.number, Kind: EventExecute, Text: statement.SQL, Parameters: parameters, IsTLS: session.isTLS})
	execution := session.server.script.Execute(statement)
	if execution.DropConnection {
		return errConnectionDropped
	}
	if execution.Error != nil {
		return session.writeError(execution.Error)
	}
	if execution.OnCommit != nil {
		session.pendingCommits = append(session.pendingCommits, execution.OnCommit)
	}
	if execution.OnRollback != nil {
		session.pendingRollbacks = append(session.pendingRollbacks, execution.OnRollback)
	}
	session.lastOutcome, session.hasResultSet = execution, len(execution.Columns) > 0
	if len(execution.Columns) == 0 {
		return session.writeOK(uint64(execution.AffectedRows), execution.LastInsertID, execution.Warnings)
	}
	return session.writeBinaryResult(execution.Columns, execution.Rows)
}

// endTransaction runs the transaction's commit or rollback hooks exactly once.
func (session *session) endTransaction(isCommitted bool) {
	hooks := session.pendingRollbacks
	if isCommitted {
		hooks = session.pendingCommits
	}
	session.pendingCommits, session.pendingRollbacks = nil, nil
	for _, hook := range hooks {
		hook()
	}
}

func (session *session) writeTextResult(columns []Column, rows ...[]string) error {
	if err := session.writeColumns(columns); err != nil {
		return err
	}
	for _, row := range rows {
		var packet []byte
		for _, value := range row {
			packet = appendLengthEncodedString(packet, []byte(value))
		}
		if err := session.writePacket(packet); err != nil {
			return err
		}
	}
	return session.writeEOF()
}

func (session *session) writeBinaryResult(columns []Column, rows [][]any) error {
	if err := session.writeColumns(columns); err != nil {
		return err
	}
	for _, row := range rows {
		packet, err := encodeBinaryRow(columns, row)
		if err != nil {
			return err
		}
		if err := session.writePacket(packet); err != nil {
			return err
		}
	}
	return session.writeEOF()
}

func (session *session) writeColumns(columns []Column) error {
	if err := session.writePacket(appendLengthEncodedInteger(nil, uint64(len(columns)))); err != nil {
		return err
	}
	for _, column := range columns {
		if err := session.writePacket(columnDefinition(column)); err != nil {
			return err
		}
	}
	return session.writeEOF()
}

func (session *session) writeOK(affectedRows uint64, lastInsertID uint64, warnings uint16) error {
	packet := []byte{0x00}
	packet = appendLengthEncodedInteger(packet, affectedRows)
	packet = appendLengthEncodedInteger(packet, lastInsertID)
	packet = binary.LittleEndian.AppendUint16(packet, session.status())
	packet = binary.LittleEndian.AppendUint16(packet, warnings)
	return session.writePacket(packet)
}

func (session *session) writeEOF() error {
	packet := []byte{0xfe, 0x00, 0x00}
	packet = binary.LittleEndian.AppendUint16(packet, session.status())
	return session.writePacket(packet)
}

func (session *session) writeError(serverError *ServerError) error {
	packet := []byte{0xff}
	packet = binary.LittleEndian.AppendUint16(packet, serverError.Number)
	packet = append(packet, '#')
	packet = append(packet, (serverError.SQLState + "HY000")[:5]...)
	packet = append(packet, serverError.Message...)
	return session.writePacket(packet)
}

func (session *session) status() uint16 {
	if session.isInTransaction {
		return statusAutocommit | statusInTransaction
	}
	return statusAutocommit
}

func (session *session) readPacket() ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(session.connection, header); err != nil {
		return nil, err
	}
	length := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	session.sequence = header[3] + 1
	packet := make([]byte, length)
	_, err := io.ReadFull(session.connection, packet)
	return packet, err
}

// writePacket splits nothing: scripted packets stay below the protocol's 16 MiB packet limit.
func (session *session) writePacket(payload []byte) error {
	if len(payload) >= 1<<24-1 {
		return errors.New("scripted packet exceeds 16 MiB")
	}
	header := []byte{byte(len(payload)), byte(len(payload) >> 8), byte(len(payload) >> 16), session.sequence}
	session.sequence++
	_, err := session.connection.Write(append(header, payload...))
	return err
}

func parseHandshakeResponse(response []byte) (user string, database string, authResponse []byte, err error) {
	if len(response) < 33 {
		return "", "", nil, errors.New("short handshake response")
	}
	capabilities := binary.LittleEndian.Uint32(response)
	remaining := response[32:]
	end := bytes.IndexByte(remaining, 0)
	if end < 0 {
		return "", "", nil, errors.New("unterminated user")
	}
	user, remaining = string(remaining[:end]), remaining[end+1:]
	length, size := readLengthEncodedInteger(remaining)
	if size == 0 || uint64(len(remaining)-size) < length {
		return "", "", nil, errors.New("truncated auth response")
	}
	authResponse, remaining = remaining[size:size+int(length)], remaining[size+int(length):]
	if capabilities&capabilityConnectWithDatabase != 0 {
		if end = bytes.IndexByte(remaining, 0); end >= 0 {
			database = string(remaining[:end])
		}
	}
	return user, database, authResponse, nil
}

// nativePasswordScramble is SHA1(password) XOR SHA1(scramble + SHA1(SHA1(password))).
func nativePasswordScramble(password string, scramble []byte) []byte {
	passwordHash := sha1.Sum([]byte(password))
	doubleHash := sha1.Sum(passwordHash[:])
	mixed := sha1.Sum(append(append([]byte(nil), scramble...), doubleHash[:]...))
	for index := range mixed {
		mixed[index] ^= passwordHash[index]
	}
	return mixed[:]
}

func parseExecuteParameters(body []byte, prepared preparedStatement) ([]Parameter, []byte, error) {
	count := prepared.parameterCount
	if count == 0 {
		return nil, nil, nil
	}
	nullBitmapLength := (count + 7) / 8
	if len(body) < nullBitmapLength+1 {
		return nil, nil, errors.New("short parameter block")
	}
	nullBitmap, remaining := body[:nullBitmapLength], body[nullBitmapLength:]
	types := prepared.parameterTypes
	if remaining[0] == 1 {
		if len(remaining) < 1+2*count {
			return nil, nil, errors.New("short parameter types")
		}
		types = append([]byte(nil), remaining[1:1+2*count]...)
		remaining = remaining[1+2*count:]
	} else {
		remaining = remaining[1:]
	}
	parameters := make([]Parameter, count)
	for index := range parameters {
		parameter := Parameter{Type: types[2*index], IsUnsigned: types[2*index+1]&0x80 != 0}
		if nullBitmap[index/8]&(1<<(index%8)) != 0 {
			parameter.IsNull = true
			parameters[index] = parameter
			continue
		}
		var size int
		switch parameter.Type {
		case TypeLongLong:
			if len(remaining) < 8 {
				return nil, nil, errors.New("short integer parameter")
			}
			value := binary.LittleEndian.Uint64(remaining)
			parameter.Text, size = strconv.FormatInt(int64(value), 10), 8
			if parameter.IsUnsigned {
				parameter.Text = strconv.FormatUint(value, 10)
			}
		case TypeDouble:
			if len(remaining) < 8 {
				return nil, nil, errors.New("short double parameter")
			}
			parameter.Text, size = strconv.FormatFloat(math.Float64frombits(binary.LittleEndian.Uint64(remaining)), 'g', -1, 64), 8
		case TypeTiny:
			if len(remaining) < 1 {
				return nil, nil, errors.New("short tiny parameter")
			}
			parameter.Text, size = strconv.Itoa(int(remaining[0])), 1
		case TypeString, TypeVarString, TypeBlob:
			length, prefix := readLengthEncodedInteger(remaining)
			if prefix == 0 || uint64(len(remaining)-prefix) < length {
				return nil, nil, errors.New("short string parameter")
			}
			parameter.Text, size = string(remaining[prefix:prefix+int(length)]), prefix+int(length)
		default:
			return nil, nil, fmt.Errorf("scripted server cannot decode parameter type %d", parameter.Type)
		}
		parameters[index], remaining = parameter, remaining[size:]
	}
	return parameters, types, nil
}

func columnDefinition(column Column) []byte {
	charset := column.Charset
	if charset == 0 {
		charset = CharsetUTF8MB4
		if isNumericType(column.Type) {
			charset = CharsetBinary
		}
	}
	packet := appendLengthEncodedString(nil, []byte("def"))
	packet = appendLengthEncodedString(packet, []byte("scripted"))
	packet = appendLengthEncodedString(packet, []byte("scripted_table"))
	packet = appendLengthEncodedString(packet, []byte("scripted_table"))
	packet = appendLengthEncodedString(packet, []byte(column.Name))
	packet = appendLengthEncodedString(packet, []byte(column.Name))
	packet = append(packet, 0x0c)
	packet = binary.LittleEndian.AppendUint16(packet, uint16(charset))
	packet = binary.LittleEndian.AppendUint32(packet, 255)
	packet = append(packet, column.Type)
	packet = binary.LittleEndian.AppendUint16(packet, column.Flags)
	packet = append(packet, column.Decimals, 0x00, 0x00)
	return packet
}

func encodeBinaryRow(columns []Column, row []any) ([]byte, error) {
	if len(row) != len(columns) {
		return nil, errors.New("scripted row width does not match its columns")
	}
	nullBitmap := make([]byte, (len(columns)+7+2)/8)
	var values []byte
	for index, value := range row {
		if value == nil {
			nullBitmap[(index+2)/8] |= 1 << ((index + 2) % 8)
			continue
		}
		encoded, err := encodeBinaryValue(columns[index].Type, value)
		if err != nil {
			return nil, fmt.Errorf("column %q: %w", columns[index].Name, err)
		}
		values = append(values, encoded...)
	}
	packet := append([]byte{0x00}, nullBitmap...)
	return append(packet, values...), nil
}

func encodeBinaryValue(columnType byte, value any) ([]byte, error) {
	switch columnType {
	case TypeTiny:
		integer, err := integerBits(value)
		return []byte{byte(integer)}, err
	case TypeShort, TypeYear:
		integer, err := integerBits(value)
		return binary.LittleEndian.AppendUint16(nil, uint16(integer)), err
	case TypeLong, TypeInt24:
		integer, err := integerBits(value)
		return binary.LittleEndian.AppendUint32(nil, uint32(integer)), err
	case TypeLongLong:
		integer, err := integerBits(value)
		return binary.LittleEndian.AppendUint64(nil, integer), err
	case TypeFloat:
		number, isFloat := value.(float32)
		if !isFloat {
			return nil, errors.New("FLOAT takes a float32")
		}
		return binary.LittleEndian.AppendUint32(nil, math.Float32bits(number)), nil
	case TypeDouble:
		number, isFloat := value.(float64)
		if !isFloat {
			return nil, errors.New("DOUBLE takes a float64")
		}
		return binary.LittleEndian.AppendUint64(nil, math.Float64bits(number)), nil
	case TypeDate, TypeDateTime, TypeTimestamp:
		text, isText := value.(string)
		if !isText {
			return nil, errors.New("temporal columns take MySQL text")
		}
		return encodeBinaryDateTime(text)
	case TypeTime:
		text, isText := value.(string)
		if !isText {
			return nil, errors.New("TIME takes MySQL text")
		}
		return encodeBinaryTime(text)
	default:
		switch typed := value.(type) {
		case string:
			return appendLengthEncodedString(nil, []byte(typed)), nil
		case []byte:
			return appendLengthEncodedString(nil, typed), nil
		}
		return nil, fmt.Errorf("type %d takes a string or []byte", columnType)
	}
}

func integerBits(value any) (uint64, error) {
	switch typed := value.(type) {
	case int64:
		return uint64(typed), nil
	case uint64:
		return typed, nil
	case int:
		return uint64(typed), nil
	}
	return 0, errors.New("integer columns take int, int64, or uint64")
}

// encodeBinaryDateTime writes the length-prefixed binary DATE, DATETIME, or TIMESTAMP form.
func encodeBinaryDateTime(text string) ([]byte, error) {
	if strings.HasPrefix(text, "0000-00-00") {
		return []byte{0}, nil
	}
	layout := "2006-01-02 15:04:05.999999"
	if len(text) == len("2006-01-02") {
		layout = "2006-01-02"
	}
	parsed, err := time.Parse(layout, text)
	if err != nil {
		return nil, err
	}
	encoded := binary.LittleEndian.AppendUint16([]byte{11}, uint16(parsed.Year()))
	encoded = append(encoded, byte(parsed.Month()), byte(parsed.Day()), byte(parsed.Hour()), byte(parsed.Minute()), byte(parsed.Second()))
	return binary.LittleEndian.AppendUint32(encoded, uint32(parsed.Nanosecond()/1000)), nil
}

// encodeBinaryTime writes the length-prefixed binary TIME form for text such as -12:34:56.5.
func encodeBinaryTime(text string) ([]byte, error) {
	isNegative := strings.HasPrefix(text, "-")
	parts := strings.Split(strings.TrimPrefix(text, "-"), ":")
	if len(parts) != 3 {
		return nil, errors.New("TIME text must be [-]H:MM:SS[.ffffff]")
	}
	hours, hoursErr := strconv.Atoi(parts[0])
	minutes, minutesErr := strconv.Atoi(parts[1])
	seconds, secondsErr := strconv.ParseFloat(parts[2], 64)
	if err := errors.Join(hoursErr, minutesErr, secondsErr); err != nil {
		return nil, err
	}
	encoded := []byte{12, 0}
	if isNegative {
		encoded[1] = 1
	}
	encoded = binary.LittleEndian.AppendUint32(encoded, uint32(hours/24))
	whole := math.Floor(seconds)
	encoded = append(encoded, byte(hours%24), byte(minutes), byte(whole))
	return binary.LittleEndian.AppendUint32(encoded, uint32(math.Round((seconds-whole)*1e6))), nil
}

func isNumericType(columnType byte) bool {
	switch columnType {
	case TypeTiny, TypeShort, TypeLong, TypeFloat, TypeDouble, TypeLongLong, TypeInt24, TypeYear, TypeNewDecimal:
		return true
	}
	return false
}

func appendLengthEncodedInteger(buffer []byte, value uint64) []byte {
	switch {
	case value < 251:
		return append(buffer, byte(value))
	case value < 1<<16:
		return binary.LittleEndian.AppendUint16(append(buffer, 0xfc), uint16(value))
	case value < 1<<24:
		return append(buffer, 0xfd, byte(value), byte(value>>8), byte(value>>16))
	default:
		return binary.LittleEndian.AppendUint64(append(buffer, 0xfe), value)
	}
}

func appendLengthEncodedString(buffer []byte, value []byte) []byte {
	return append(appendLengthEncodedInteger(buffer, uint64(len(value))), value...)
}

// readLengthEncodedInteger returns the value and its encoded size, or size zero when the buffer is short.
func readLengthEncodedInteger(buffer []byte) (uint64, int) {
	if len(buffer) == 0 {
		return 0, 0
	}
	switch buffer[0] {
	case 0xfc:
		if len(buffer) < 3 {
			return 0, 0
		}
		return uint64(binary.LittleEndian.Uint16(buffer[1:])), 3
	case 0xfd:
		if len(buffer) < 4 {
			return 0, 0
		}
		return uint64(buffer[1]) | uint64(buffer[2])<<8 | uint64(buffer[3])<<16, 4
	case 0xfe:
		if len(buffer) < 9 {
			return 0, 0
		}
		return binary.LittleEndian.Uint64(buffer[1:]), 9
	default:
		return uint64(buffer[0]), 1
	}
}

var commitActionNames = map[CommitAction]string{
	CommitAndReply:                  "commit-and-reply",
	CommitThenDropConnection:        "commit-then-drop-connection",
	DropConnectionWithoutCommitting: "drop-connection-without-committing",
	RejectCommitWithDeadlock:        "reject-commit-with-deadlock",
	RejectCommitWithUnknownError:    "reject-commit-with-unknown-error",
}
