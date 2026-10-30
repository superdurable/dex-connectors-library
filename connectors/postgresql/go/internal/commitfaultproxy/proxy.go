// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package commitfaultproxy is a loopback TCP proxy that drops a plaintext PostgreSQL connection at a
// chosen protocol point, so live tests can prove what a lost connection means around COMMIT.
package commitfaultproxy

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
)

// Fault is where the proxy drops one connection.
type Fault int

const (
	// PassThrough forwards every message.
	PassThrough Fault = iota
	// DropAtStatementExecute closes both sides when the client sends an extended-protocol Execute.
	DropAtStatementExecute
	// DropBeforeCommitReachesServer closes both sides instead of forwarding COMMIT.
	DropBeforeCommitReachesServer
	// DropAfterCommitReachesServer forwards COMMIT, waits for the server's reply, discards it, and closes both sides.
	DropAfterCommitReachesServer
)

// cancelRequestCode identifies the out-of-band cancel packet a client sends on a separate connection.
const cancelRequestCode = 80877102

// Proxy forwards loopback connections to one PostgreSQL address.
type Proxy struct {
	listener       net.Listener
	target         string
	faultFor       func(sessionNumber int) Fault
	mu             sync.Mutex
	sessionNumber  int
	commitMessages int
	serving        sync.WaitGroup
}

// Start listens on 127.0.0.1 and applies faultFor to each client session, numbered from one.
// Cancel-request connections are forwarded unchanged and are not numbered.
func Start(target string, faultFor func(sessionNumber int) Fault) (*Proxy, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	proxy := &Proxy{listener: listener, target: target, faultFor: faultFor}
	proxy.serving.Add(1)
	go proxy.acceptConnections()
	return proxy, nil
}

// Port returns the proxy's TCP port.
func (proxy *Proxy) Port() int { return proxy.listener.Addr().(*net.TCPAddr).Port }

// Sessions returns how many client sessions, excluding cancel requests, the proxy has accepted.
func (proxy *Proxy) Sessions() int {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	return proxy.sessionNumber
}

// CommitMessages returns how many COMMIT queries clients have sent through the proxy.
func (proxy *Proxy) CommitMessages() int {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	return proxy.commitMessages
}

// Close stops accepting connections and waits for forwarded connections to end.
func (proxy *Proxy) Close() error {
	err := proxy.listener.Close()
	proxy.serving.Wait()
	return err
}

func (proxy *Proxy) acceptConnections() {
	defer proxy.serving.Done()
	for {
		client, err := proxy.listener.Accept()
		if err != nil {
			return
		}
		proxy.serving.Add(1)
		go func() {
			defer proxy.serving.Done()
			// Forwarding errors end the connection, which is the fault under test.
			_ = proxy.forward(client)
		}()
	}
}

func (proxy *Proxy) nextSessionFault() Fault {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	proxy.sessionNumber++
	return proxy.faultFor(proxy.sessionNumber)
}

func (proxy *Proxy) forward(client net.Conn) error {
	defer client.Close()
	startup, err := readStartupPacket(client)
	if err != nil {
		return err
	}
	fault := PassThrough
	if binary.BigEndian.Uint32(startup[4:8]) != cancelRequestCode {
		fault = proxy.nextSessionFault()
	}
	server, err := net.Dial("tcp", proxy.target)
	if err != nil {
		return err
	}
	defer server.Close()
	relay := &serverRelay{client: client, server: server, serverReplied: make(chan struct{})}
	go relay.copyServerToClient()
	if _, err := server.Write(startup); err != nil {
		return err
	}
	for {
		messageType, message, err := readFrontendMessage(client)
		if err != nil {
			return err
		}
		if messageType == 'Q' && isCommit(message) {
			proxy.mu.Lock()
			proxy.commitMessages++
			proxy.mu.Unlock()
		}
		switch {
		case fault == DropAtStatementExecute && messageType == 'E':
			return errors.New("dropped at Execute")
		case messageType == 'Q' && isCommit(message) && fault == DropBeforeCommitReachesServer:
			return errors.New("dropped before COMMIT")
		case messageType == 'Q' && isCommit(message) && fault == DropAfterCommitReachesServer:
			relay.startDiscardingServerOutput()
			if _, err := server.Write(message); err != nil {
				return err
			}
			<-relay.serverReplied
			return errors.New("dropped after COMMIT")
		}
		if _, err := server.Write(message); err != nil {
			return err
		}
	}
}

// serverRelay copies server output to the client until told to discard it.
type serverRelay struct {
	client        net.Conn
	server        net.Conn
	mu            sync.Mutex
	isDiscarding  bool
	serverReplied chan struct{}
	replyOnce     sync.Once
}

func (relay *serverRelay) startDiscardingServerOutput() {
	relay.mu.Lock()
	defer relay.mu.Unlock()
	relay.isDiscarding = true
}

// copyServerToClient closes the client when the server closes, as a direct connection would.
func (relay *serverRelay) copyServerToClient() {
	defer relay.client.Close()
	defer relay.replyOnce.Do(func() { close(relay.serverReplied) })
	buffer := make([]byte, 32<<10)
	for {
		count, err := relay.server.Read(buffer)
		if count > 0 {
			relay.mu.Lock()
			isDiscarding := relay.isDiscarding
			relay.mu.Unlock()
			if isDiscarding {
				relay.replyOnce.Do(func() { close(relay.serverReplied) })
			} else if _, writeErr := relay.client.Write(buffer[:count]); writeErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func readStartupPacket(client net.Conn) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(client, header); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint32(header))
	if length < 8 || length > 10000 {
		return nil, errors.New("invalid startup packet length")
	}
	packet := make([]byte, length)
	copy(packet, header)
	_, err := io.ReadFull(client, packet[4:])
	return packet, err
}

func readFrontendMessage(client net.Conn) (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(client, header); err != nil {
		return 0, nil, err
	}
	length := int(binary.BigEndian.Uint32(header[1:]))
	if length < 4 || length > 64<<20 {
		return 0, nil, errors.New("invalid message length")
	}
	message := make([]byte, 1+length)
	copy(message, header)
	_, err := io.ReadFull(client, message[5:])
	return header[0], message, err
}

func isCommit(message []byte) bool {
	query := strings.TrimRight(string(message[5:]), "\x00")
	return strings.EqualFold(strings.TrimSpace(query), "commit")
}
