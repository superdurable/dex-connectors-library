// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailtest

import (
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// SentinelText appears in every refusal the SMTP server sends, so tests can prove it never reaches a Result.
const SentinelText = "MAILTEST-SENTINEL"

// SMTPServerConfig configures StartSMTPServer.
type SMTPServerConfig struct {
	// Certificates presents the server certificate; required unless Security is PlaintextOnly.
	Certificates *Certificates
	// Security selects implicit TLS, STARTTLS, or plaintext only.
	Security Security
	// Username and Password are the only credentials AUTH PLAIN accepts.
	Username string
	Password string
}

// Submission is one message the server accepted.
type Submission struct {
	// From is the MAIL FROM address.
	From string
	// Recipients are the RCPT TO addresses in order.
	Recipients []string
	// Data is the message exactly as received, with CRLF line endings.
	Data []byte
}

// SMTPServer is a running recording SMTP server.
type SMTPServer struct {
	// Host is the loopback address the server listens on.
	Host string
	// Port is the server's TCP port.
	Port int

	config      SMTPServerConfig
	mutex       sync.Mutex
	submissions []Submission
	auths       int
	mails       int
	data        int
	// endOfDataHold delays the answer to the next final dot.
	endOfDataHold time.Duration
	// endOfDataRelease holds the answer to the next final dot until it is closed.
	endOfDataRelease chan struct{}
	// dropsNextEndOfDataReply accepts the next message and closes the connection instead of answering.
	dropsNextEndOfDataReply bool
	// temporaryMailFailures answers that many MAIL FROM commands with 451.
	temporaryMailFailures int
	rejectedRecipient     string
	heldReplies           sync.WaitGroup
}

// StartSMTPServer starts a server that the test's cleanup stops.
func StartSMTPServer(t testing.TB, config SMTPServerConfig) *SMTPServer {
	t.Helper()
	server := &SMTPServer{Host: "127.0.0.1", config: config}
	smtpServer := smtp.NewServer(smtp.BackendFunc(func(conn *smtp.Conn) (smtp.Session, error) {
		return &recordingSession{server: server, conn: conn}, nil
	}))
	smtpServer.Domain = "mailtest.example.com"
	smtpServer.ErrorLog = log.New(io.Discard, "", 0)
	smtpServer.ReadTimeout, smtpServer.WriteTimeout = time.Minute, time.Minute
	if config.Security == StartTLS {
		smtpServer.TLSConfig = config.Certificates.serverTLSConfig()
	}
	if config.Security == PlaintextOnly {
		// A plaintext server that would accept AUTH proves the connector, not the server, refuses it.
		smtpServer.AllowInsecureAuth = true
	}
	listener, port := listen(t, config.Certificates, config.Security)
	server.Port = port
	served := make(chan struct{})
	go func() {
		defer close(served)
		// Serve returns when Close closes the listener.
		_ = smtpServer.Serve(listener)
	}()
	t.Cleanup(func() {
		server.heldReplies.Wait()
		// Close also ends every open connection; its error only reports an already closed server.
		_ = smtpServer.Close()
		// Serve may not have registered the listener before Close ran, so close it directly too.
		_ = listener.Close()
		<-served
	})
	return server
}

// HoldNextEndOfDataReply delays the answer to the next final dot by delay; the message is accepted.
func (server *SMTPServer) HoldNextEndOfDataReply(delay time.Duration) {
	server.mutex.Lock()
	server.endOfDataHold = delay
	server.mutex.Unlock()
}

// HoldNextEndOfDataReplyUntil holds the answer to the next final dot until release is closed.
func (server *SMTPServer) HoldNextEndOfDataReplyUntil(release chan struct{}) {
	server.mutex.Lock()
	server.endOfDataRelease = release
	server.mutex.Unlock()
}

// DropNextEndOfDataReply accepts the next message and closes the connection instead of answering 250.
func (server *SMTPServer) DropNextEndOfDataReply() {
	server.mutex.Lock()
	server.dropsNextEndOfDataReply = true
	server.mutex.Unlock()
}

// FailNextMailCommands answers the next count MAIL FROM commands with 451 4.3.0.
func (server *SMTPServer) FailNextMailCommands(count int) {
	server.mutex.Lock()
	server.temporaryMailFailures = count
	server.mutex.Unlock()
}

// RejectRecipient answers RCPT TO for address with 550 5.1.1.
func (server *SMTPServer) RejectRecipient(address string) {
	server.mutex.Lock()
	server.rejectedRecipient = address
	server.mutex.Unlock()
}

// WaitForHeldReplies waits for every held end-of-data answer to be sent.
func (server *SMTPServer) WaitForHeldReplies() { server.heldReplies.Wait() }

// Submissions returns every accepted message in arrival order.
func (server *SMTPServer) Submissions() []Submission {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return append([]Submission(nil), server.submissions...)
}

// AuthCount is the number of AUTH attempts.
func (server *SMTPServer) AuthCount() int { return server.count(func() int { return server.auths }) }

// MailCount is the number of MAIL FROM commands.
func (server *SMTPServer) MailCount() int { return server.count(func() int { return server.mails }) }

// DataCount is the number of messages the server received after DATA, accepted or not.
func (server *SMTPServer) DataCount() int { return server.count(func() int { return server.data }) }

func (server *SMTPServer) count(read func() int) int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return read()
}

// recordingSession accepts AUTH PLAIN with the configured credentials and records accepted messages.
type recordingSession struct {
	server     *SMTPServer
	conn       *smtp.Conn
	from       string
	recipients []string
	isAuthed   bool
}

// AuthMechanisms offers PLAIN only.
func (session *recordingSession) AuthMechanisms() []string { return []string{sasl.Plain} }

// Auth checks the configured credentials.
func (session *recordingSession) Auth(mechanism string) (sasl.Server, error) {
	if mechanism != sasl.Plain {
		return nil, smtp.ErrAuthUnknownMechanism
	}
	return sasl.NewPlainServer(func(identity string, username string, password string) error {
		session.server.mutex.Lock()
		session.server.auths++
		session.server.mutex.Unlock()
		if identity != "" && identity != username || username != session.server.config.Username || password != session.server.config.Password {
			return &smtp.SMTPError{Code: 535, EnhancedCode: smtp.EnhancedCode{5, 7, 8}, Message: SentinelText + " authentication failed"}
		}
		session.isAuthed = true
		return nil
	}), nil
}

// Mail starts a transaction, or answers 451 while a temporary failure is pending.
func (session *recordingSession) Mail(from string, _ *smtp.MailOptions) error {
	server := session.server
	server.mutex.Lock()
	defer server.mutex.Unlock()
	server.mails++
	if !session.isAuthed {
		return smtp.ErrAuthRequired
	}
	if server.temporaryMailFailures > 0 {
		server.temporaryMailFailures--
		return &smtp.SMTPError{Code: 451, EnhancedCode: smtp.EnhancedCode{4, 3, 0}, Message: SentinelText + " try again later"}
	}
	session.from = from
	return nil
}

// Rcpt adds a recipient, or answers 550 for the rejected address.
func (session *recordingSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	session.server.mutex.Lock()
	rejected := session.server.rejectedRecipient
	session.server.mutex.Unlock()
	if rejected != "" && strings.EqualFold(to, rejected) {
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: SentinelText + " no such user " + to}
	}
	session.recipients = append(session.recipients, to)
	return nil
}

// Data records the message and applies a pending hold or lost reply before answering.
func (session *recordingSession) Data(reader io.Reader) error {
	contents, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	server := session.server
	server.mutex.Lock()
	server.data++
	server.submissions = append(server.submissions, Submission{From: session.from, Recipients: append([]string(nil), session.recipients...), Data: contents})
	hold, release, dropsReply := server.endOfDataHold, server.endOfDataRelease, server.dropsNextEndOfDataReply
	server.endOfDataHold, server.endOfDataRelease, server.dropsNextEndOfDataReply = 0, nil, false
	if hold > 0 || release != nil {
		server.heldReplies.Add(1)
	}
	server.mutex.Unlock()
	if hold > 0 || release != nil {
		defer server.heldReplies.Done()
	}
	if hold > 0 {
		time.Sleep(hold)
	}
	if release != nil {
		<-release
	}
	if dropsReply {
		// Losing the answer to the final dot is the fault being injected.
		_ = session.conn.Conn().Close()
		return errors.New("mailtest: reply dropped")
	}
	return nil
}

// Reset discards the current transaction.
func (session *recordingSession) Reset() { session.from, session.recipients = "", nil }

// Logout ends the session.
func (session *recordingSession) Logout() error { return nil }

var _ smtp.AuthSession = (*recordingSession)(nil)
