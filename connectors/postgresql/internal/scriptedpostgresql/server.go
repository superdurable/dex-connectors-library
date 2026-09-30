// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package scriptedpostgresql is a minimal PostgreSQL wire-protocol server for deterministic tests.
//
// It speaks enough of protocol 3.0 for the connector: cleartext password authentication, simple
// queries for transaction control, and one unnamed extended-protocol statement at a time. A Script
// supplies statement shapes, rows, errors, and commit faults, and every received message is
// recorded so tests can assert what reached the server.
package scriptedpostgresql

import (
	"errors"
	"net"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgproto3"
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
	// RejectCommitWithSerializationFailure discards the transaction and replies with SQLSTATE 40001.
	RejectCommitWithSerializationFailure
)

// Event kinds recorded by the server.
const (
	EventStartup   = "startup"
	EventQuery     = "query"
	EventParse     = "parse"
	EventExecute   = "execute"
	EventCommit    = "commit"
	EventTerminate = "terminate"
)

// ServerVersion is the server_version parameter the server reports.
const ServerVersion = "17.0 (scripted)"

// Column is one scripted result column.
type Column struct {
	// Name is the reported column name.
	Name string
	// TypeOID is the reported PostgreSQL type OID.
	TypeOID uint32
}

// Shape is the server's description of a parsed statement.
type Shape struct {
	// ParameterCount is the number of placeholders the server reports.
	ParameterCount int
	// Columns are the result columns; empty means the statement returns no rows.
	Columns []Column
}

// ServerError is an ErrorResponse the server sends instead of a result.
type ServerError struct {
	// Code is the SQLSTATE.
	Code string
	// Message is the primary server message.
	Message string
	// ConstraintName names the violated constraint, when any.
	ConstraintName string
}

// Statement is one executed statement with its bound text parameters.
type Statement struct {
	// SQL is the statement text received in Parse.
	SQL string
	// Parameters are the bound values; a nil entry is SQL NULL.
	Parameters [][]byte
}

// Execution scripts the result of one executed statement.
type Execution struct {
	// Rows are text-format rows; a nil value is SQL NULL.
	Rows [][][]byte
	// CommandTag is the reported command tag, such as "INSERT 0 1".
	CommandTag string
	// Error replaces the result with an ErrorResponse when set.
	Error *ServerError
	// DropConnection closes the connection instead of answering Execute.
	DropConnection bool
	// OnCommit runs when the enclosing transaction commits.
	OnCommit func()
	// OnRollback runs when the enclosing transaction ends any other way, including a lost connection.
	OnRollback func()
}

// Script supplies every scripted response. Its functions must be safe for concurrent connections.
type Script struct {
	// Describe returns a statement's shape, or an error such as a syntax error.
	Describe func(sql string) (Shape, *ServerError)
	// Execute returns a statement's result.
	Execute func(statement Statement) Execution
	// Commit decides how COMMIT is handled; nil commits normally.
	Commit func() CommitAction
}

// Event is one message the server received.
type Event struct {
	// Connection numbers connections from one in accept order.
	Connection int
	// Kind is one of the Event constants.
	Kind string
	// Text is the SQL of a query, parse, or execute event, or the user of a startup event.
	Text string
	// Parameters are the bound parameters of an execute event, or the password of a startup event.
	Parameters [][]byte
}

// Server accepts connections on a loopback port until Close.
type Server struct {
	listener         net.Listener
	password         string
	script           Script
	mu               sync.Mutex
	events           []Event
	connectionNumber int
	serving          sync.WaitGroup
}

// Start listens on 127.0.0.1 and serves connections that authenticate with password.
func Start(password string, script Script) (*Server, error) {
	if script.Describe == nil || script.Execute == nil {
		return nil, errors.New("scripted PostgreSQL requires Describe and Execute")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	server := &Server{listener: listener, password: password, script: script}
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
			defer connection.Close()
			protocol := &session{server: server, number: number, connection: connection, backend: pgproto3.NewBackend(connection, connection)}
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

// session is one client connection's protocol state.
type session struct {
	server           *Server
	number           int
	connection       net.Conn
	backend          *pgproto3.Backend
	transactionState byte
	statementSQL     string
	statementShape   Shape
	parameters       [][]byte
	isSkippingToSync bool
	pendingCommits   []func()
	pendingRollbacks []func()
}

var errConnectionDropped = errors.New("scripted connection dropped")

func (session *session) serve() error {
	defer session.endTransaction(false)
	if err := session.authenticate(); err != nil {
		return err
	}
	session.transactionState = 'I'
	for {
		message, err := session.backend.Receive()
		if err != nil {
			return err
		}
		if err := session.handle(message); err != nil {
			return err
		}
	}
}

func (session *session) authenticate() error {
	startup, err := session.backend.ReceiveStartupMessage()
	if err != nil {
		return err
	}
	if _, isSSLRequest := startup.(*pgproto3.SSLRequest); isSSLRequest {
		if _, err := session.connection.Write([]byte{'N'}); err != nil {
			return err
		}
		if startup, err = session.backend.ReceiveStartupMessage(); err != nil {
			return err
		}
	}
	startupMessage, isStartup := startup.(*pgproto3.StartupMessage)
	if !isStartup {
		return errors.New("scripted PostgreSQL expected a startup message")
	}
	user := startupMessage.Parameters["user"]
	session.backend.Send(&pgproto3.AuthenticationCleartextPassword{})
	if err := session.backend.Flush(); err != nil {
		return err
	}
	if err := session.backend.SetAuthType(pgproto3.AuthTypeCleartextPassword); err != nil {
		return err
	}
	response, err := session.backend.Receive()
	if err != nil {
		return err
	}
	password, isPassword := response.(*pgproto3.PasswordMessage)
	if !isPassword {
		return errors.New("scripted PostgreSQL expected a password message")
	}
	session.server.record(Event{Connection: session.number, Kind: EventStartup, Text: user, Parameters: [][]byte{[]byte(password.Password)}})
	if password.Password != session.server.password {
		session.backend.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: "28P01", Message: "password authentication failed"})
		return errors.Join(session.backend.Flush(), errConnectionDropped)
	}
	session.backend.Send(&pgproto3.AuthenticationOk{})
	session.backend.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: ServerVersion})
	session.backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
	session.backend.Send(&pgproto3.BackendKeyData{ProcessID: uint32(1000 + session.number), SecretKey: 1234})
	session.backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	return session.backend.Flush()
}

func (session *session) handle(message pgproto3.FrontendMessage) error {
	switch message := message.(type) {
	case *pgproto3.Query:
		return session.handleSimpleQuery(message.String)
	case *pgproto3.Parse:
		session.handleParse(message.Query)
	case *pgproto3.Describe:
		session.handleDescribe(message.ObjectType)
	case *pgproto3.Bind:
		session.handleBind(message.Parameters)
	case *pgproto3.Execute:
		return session.handleExecute()
	case *pgproto3.Sync:
		session.isSkippingToSync = false
		session.backend.Send(&pgproto3.ReadyForQuery{TxStatus: session.transactionState})
		return session.backend.Flush()
	case *pgproto3.Terminate:
		session.server.record(Event{Connection: session.number, Kind: EventTerminate})
		return errConnectionDropped
	}
	return nil
}

func (session *session) handleSimpleQuery(text string) error {
	session.server.record(Event{Connection: session.number, Kind: EventQuery, Text: text})
	for _, statement := range strings.Split(text, ";") {
		statement = strings.ToLower(strings.TrimSpace(statement))
		switch {
		case statement == "":
			continue
		case strings.HasPrefix(statement, "begin"):
			session.transactionState = 'T'
			session.backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("BEGIN")})
		case statement == "commit":
			return session.handleCommit()
		case statement == "rollback":
			session.endTransaction(false)
			session.transactionState = 'I'
			session.backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("ROLLBACK")})
		default:
			session.backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SET")})
		}
	}
	session.backend.Send(&pgproto3.ReadyForQuery{TxStatus: session.transactionState})
	return session.backend.Flush()
}

func (session *session) handleCommit() error {
	action := CommitAndReply
	if session.server.script.Commit != nil {
		action = session.server.script.Commit()
	}
	session.server.record(Event{Connection: session.number, Kind: EventCommit, Text: commitActionNames[action]})
	isFailedTransaction := session.transactionState == 'E'
	session.transactionState = 'I'
	switch action {
	case CommitThenDropConnection:
		session.endTransaction(!isFailedTransaction)
		return errConnectionDropped
	case DropConnectionWithoutCommitting:
		session.endTransaction(false)
		return errConnectionDropped
	case RejectCommitWithSerializationFailure:
		session.endTransaction(false)
		session.sendError(&ServerError{Code: "40001", Message: "could not serialize access due to concurrent update"})
	default:
		session.endTransaction(!isFailedTransaction)
		tag := "COMMIT"
		if isFailedTransaction {
			tag = "ROLLBACK"
		}
		session.backend.Send(&pgproto3.CommandComplete{CommandTag: []byte(tag)})
	}
	session.backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	return session.backend.Flush()
}

func (session *session) handleParse(sql string) {
	if session.isSkippingToSync {
		return
	}
	session.server.record(Event{Connection: session.number, Kind: EventParse, Text: sql})
	shape, serverError := session.server.script.Describe(sql)
	if serverError != nil {
		session.fail(serverError)
		return
	}
	session.statementSQL, session.statementShape = sql, shape
	session.backend.Send(&pgproto3.ParseComplete{})
}

func (session *session) handleDescribe(objectType byte) {
	if session.isSkippingToSync {
		return
	}
	if objectType == 'S' {
		parameterOIDs := make([]uint32, session.statementShape.ParameterCount)
		session.backend.Send(&pgproto3.ParameterDescription{ParameterOIDs: parameterOIDs})
	}
	session.sendRowDescription()
}

func (session *session) handleBind(parameters [][]byte) {
	if session.isSkippingToSync {
		return
	}
	session.parameters = make([][]byte, len(parameters))
	for index, parameter := range parameters {
		if parameter != nil {
			session.parameters[index] = append([]byte{}, parameter...)
		}
	}
	session.backend.Send(&pgproto3.BindComplete{})
}

func (session *session) handleExecute() error {
	if session.isSkippingToSync {
		return nil
	}
	statement := Statement{SQL: session.statementSQL, Parameters: session.parameters}
	session.server.record(Event{Connection: session.number, Kind: EventExecute, Text: statement.SQL, Parameters: statement.Parameters})
	execution := session.server.script.Execute(statement)
	if execution.DropConnection {
		return errConnectionDropped
	}
	if execution.Error != nil {
		session.fail(execution.Error)
		return nil
	}
	for _, row := range execution.Rows {
		session.backend.Send(&pgproto3.DataRow{Values: row})
	}
	session.backend.Send(&pgproto3.CommandComplete{CommandTag: []byte(execution.CommandTag)})
	if execution.OnCommit != nil {
		session.pendingCommits = append(session.pendingCommits, execution.OnCommit)
	}
	if execution.OnRollback != nil {
		session.pendingRollbacks = append(session.pendingRollbacks, execution.OnRollback)
	}
	return nil
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

func (session *session) sendRowDescription() {
	if len(session.statementShape.Columns) == 0 {
		session.backend.Send(&pgproto3.NoData{})
		return
	}
	fields := make([]pgproto3.FieldDescription, len(session.statementShape.Columns))
	for index, column := range session.statementShape.Columns {
		fields[index] = pgproto3.FieldDescription{Name: []byte(column.Name), DataTypeOID: column.TypeOID, DataTypeSize: -1, TypeModifier: -1}
	}
	session.backend.Send(&pgproto3.RowDescription{Fields: fields})
}

func (session *session) fail(serverError *ServerError) {
	session.sendError(serverError)
	session.isSkippingToSync = true
	if session.transactionState == 'T' {
		session.transactionState = 'E'
	}
}

func (session *session) sendError(serverError *ServerError) {
	session.backend.Send(&pgproto3.ErrorResponse{
		Severity: "ERROR", SeverityUnlocalized: "ERROR", Code: serverError.Code, Message: serverError.Message,
		ConstraintName: serverError.ConstraintName,
	})
}

var commitActionNames = map[CommitAction]string{
	CommitAndReply:                       "commit-and-reply",
	CommitThenDropConnection:             "commit-then-drop-connection",
	DropConnectionWithoutCommitting:      "drop-connection-without-committing",
	RejectCommitWithSerializationFailure: "reject-commit-with-serialization-failure",
}
