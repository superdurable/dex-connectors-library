// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailtest

import (
	"bytes"
	"crypto/tls"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/stretchr/testify/require"
)

const (
	inspectorUsername = "mailtest-inspector"
	inspectorPassword = "mailtest-inspector-password"
)

// IMAPServerConfig configures StartIMAPServer.
type IMAPServerConfig struct {
	// Certificates presents the server certificate; required unless Security is PlaintextOnly.
	Certificates *Certificates
	// Security selects implicit TLS, STARTTLS, or plaintext only.
	Security Security
	// Username and Password are the only credentials the server accepts.
	Username string
	Password string
	// Mailboxes are created in addition to INBOX.
	Mailboxes []string
	// Capabilities overrides the default IMAP4rev1, MOVE, and UIDPLUS set.
	Capabilities imap.CapSet
}

// IMAPServer is a running in-memory IMAP server.
type IMAPServer struct {
	// Host is the loopback address the server listens on.
	Host string
	// Port is the server's TCP port.
	Port int

	user     *imapmemserver.User
	config   IMAPServerConfig
	mutex    sync.Mutex
	logins   int
	moves    int
	stores   int
	moveHold time.Duration
	// dropsNextMoveReply closes the connection after the next MOVE is applied, before its OK is sent.
	dropsNextMoveReply bool
	delayedMoves       sync.WaitGroup
}

// StoredMessage is one message as the server holds it.
type StoredMessage struct {
	// UID is the message's UID in its mailbox.
	UID imap.UID
	// Flags are the message's flags.
	Flags []imap.Flag
	// MessageID is the Message-ID without angle brackets.
	MessageID string
	// Subject is the decoded subject.
	Subject string
}

// StartIMAPServer starts a server that the test's cleanup stops.
func StartIMAPServer(t testing.TB, config IMAPServerConfig) *IMAPServer {
	t.Helper()
	server := &IMAPServer{Host: "127.0.0.1", config: config, user: imapmemserver.NewUser(config.Username, config.Password)}
	for _, mailbox := range append([]string{"INBOX"}, config.Mailboxes...) {
		require.NoError(t, server.user.Create(mailbox, nil))
	}
	capabilities := config.Capabilities
	if capabilities == nil {
		capabilities = imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapMove: {}, imap.CapUIDPlus: {}}
	}
	options := &imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &faultInjectingSession{server: server, conn: conn}, nil, nil
		},
		Caps:   capabilities,
		Logger: log.New(io.Discard, "", 0),
	}
	if config.Security == StartTLS {
		options.TLSConfig = config.Certificates.serverTLSConfig()
	}
	listener, port := listen(t, config.Certificates, config.Security)
	server.Port = port
	imapServer := imapserver.New(options)
	served := make(chan struct{})
	go func() {
		defer close(served)
		// Serve returns when Close closes the listener.
		_ = imapServer.Serve(listener)
	}()
	t.Cleanup(func() {
		server.delayedMoves.Wait()
		// Close also ends every open connection; its error only reports an already closed listener.
		_ = imapServer.Close()
		// Serve may not have registered the listener before Close ran, so close it directly too.
		_ = listener.Close()
		<-served
	})
	return server
}

// AppendMessage stores raw in mailbox with flags and an internal date, and returns its UID.
func (server *IMAPServer) AppendMessage(t testing.TB, mailbox string, raw []byte, flags []imap.Flag, receivedAt time.Time) imap.UID {
	t.Helper()
	appended, err := server.user.Append(mailbox, literal{Reader: bytes.NewReader(raw), size: int64(len(raw))},
		&imap.AppendOptions{Flags: flags, Time: receivedAt})
	require.NoError(t, err)
	return appended.UID
}

// UIDValidity returns the mailbox's UIDVALIDITY.
func (server *IMAPServer) UIDValidity(t testing.TB, mailbox string) uint32 {
	t.Helper()
	status, err := server.user.Status(mailbox, &imap.StatusOptions{UIDValidity: true})
	require.NoError(t, err)
	return status.UIDValidity
}

// Messages lists the mailbox through a separate inspector login that the counters ignore.
func (server *IMAPServer) Messages(t testing.TB, mailbox string) []StoredMessage {
	t.Helper()
	require.Equal(t, ImplicitTLS, server.config.Security, "mailtest: Messages needs an implicit TLS server")
	conn, err := tls.Dial("tcp", net.JoinHostPort(server.Host, strconv.Itoa(server.Port)), &tls.Config{
		RootCAs: server.config.Certificates.RootCAs, ServerName: server.Host, MinVersion: tls.VersionTLS12,
	})
	require.NoError(t, err)
	client := imapclient.New(conn, nil)
	defer client.Close()
	require.NoError(t, client.Login(inspectorUsername, inspectorPassword).Wait())
	selected, err := client.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait()
	require.NoError(t, err)
	if selected.NumMessages == 0 {
		return []StoredMessage{}
	}
	fetched, err := client.Fetch(imap.SeqSetNum(sequenceNumbers(selected.NumMessages)...), &imap.FetchOptions{UID: true, Flags: true, Envelope: true}).Collect()
	require.NoError(t, err)
	messages := make([]StoredMessage, 0, len(fetched))
	for _, message := range fetched {
		stored := StoredMessage{UID: message.UID, Flags: message.Flags}
		if message.Envelope != nil {
			stored.MessageID, stored.Subject = message.Envelope.MessageID, message.Envelope.Subject
		}
		messages = append(messages, stored)
	}
	return messages
}

// HoldNextMove delays the next MOVE by delay before the server applies it.
func (server *IMAPServer) HoldNextMove(delay time.Duration) {
	server.mutex.Lock()
	server.moveHold = delay
	server.mutex.Unlock()
}

// DropNextMoveReply applies the next MOVE and closes the connection before the OK is sent.
func (server *IMAPServer) DropNextMoveReply() {
	server.mutex.Lock()
	server.dropsNextMoveReply = true
	server.mutex.Unlock()
}

// WaitForDelayedMoves waits for every held MOVE to finish.
func (server *IMAPServer) WaitForDelayedMoves() { server.delayedMoves.Wait() }

// LoginCount is the number of LOGIN or AUTHENTICATE attempts, excluding the inspector's.
func (server *IMAPServer) LoginCount() int { return server.count(func() int { return server.logins }) }

// MoveCount is the number of MOVE commands the server received.
func (server *IMAPServer) MoveCount() int { return server.count(func() int { return server.moves }) }

// StoreCount is the number of STORE commands the server received.
func (server *IMAPServer) StoreCount() int { return server.count(func() int { return server.stores }) }

func (server *IMAPServer) count(read func() int) int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return read()
}

// faultInjectingSession wraps the in-memory session to count commands and inject delays and lost replies.
type faultInjectingSession struct {
	*imapmemserver.UserSession
	server *IMAPServer
	conn   *imapserver.Conn
}

// Login accepts the configured credentials and the inspector's.
func (session *faultInjectingSession) Login(username string, password string) error {
	if username == inspectorUsername && password == inspectorPassword {
		session.UserSession = imapmemserver.NewUserSession(session.server.user)
		return nil
	}
	session.server.mutex.Lock()
	session.server.logins++
	session.server.mutex.Unlock()
	if err := session.server.user.Login(username, password); err != nil {
		return err
	}
	session.UserSession = imapmemserver.NewUserSession(session.server.user)
	return nil
}

// Close releases the in-memory session, which exists only after a login.
func (session *faultInjectingSession) Close() error {
	if session.UserSession == nil {
		return nil
	}
	return session.UserSession.Close()
}

// Store counts STORE commands.
func (session *faultInjectingSession) Store(writer *imapserver.FetchWriter, numbers imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	session.server.mutex.Lock()
	session.server.stores++
	session.server.mutex.Unlock()
	return session.UserSession.Store(writer, numbers, flags, options)
}

// Move counts MOVE commands and applies a pending hold or lost reply.
func (session *faultInjectingSession) Move(writer *imapserver.MoveWriter, numbers imap.NumSet, destination string) error {
	server := session.server
	server.mutex.Lock()
	server.moves++
	hold, dropsReply := server.moveHold, server.dropsNextMoveReply
	server.moveHold, server.dropsNextMoveReply = 0, false
	if hold > 0 {
		server.delayedMoves.Add(1)
	}
	server.mutex.Unlock()
	if hold > 0 {
		defer server.delayedMoves.Done()
		time.Sleep(hold)
	}
	err := session.UserSession.Move(writer, numbers, destination)
	if dropsReply {
		// Losing the reply is the fault being injected.
		_ = session.conn.NetConn().Close()
	}
	return err
}

func sequenceNumbers(count uint32) []uint32 {
	numbers := make([]uint32, count)
	for index := range numbers {
		numbers[index] = uint32(index) + 1
	}
	return numbers
}

// literal adapts a byte reader to imap.LiteralReader.
type literal struct {
	*bytes.Reader
	size int64
}

// Size returns the literal's length.
func (value literal) Size() int64 { return value.size }

var _ imapserver.SessionMove = (*faultInjectingSession)(nil)
