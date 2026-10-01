// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package commitfaultproxy is a loopback TCP proxy that drops a plaintext MySQL connection at a
// chosen protocol point, so live tests can prove what a lost connection means around COMMIT.
package commitfaultproxy

import (
	"errors"
	"io"
	"net"
	"strings"
	"sync"
)

// Fault is where the proxy drops one connection.
type Fault int

const (
	// PassThrough forwards every packet.
	PassThrough Fault = iota
	// DropAtStatementExecute closes both sides when the client sends COM_STMT_EXECUTE.
	DropAtStatementExecute
	// DropBeforeCommitReachesServer closes both sides instead of forwarding COMMIT.
	DropBeforeCommitReachesServer
	// DropAfterCommitReachesServer forwards COMMIT, waits for the server's reply, discards it, and closes both sides.
	DropAfterCommitReachesServer
)

// MySQL command bytes; a command packet is the only client packet with sequence number zero.
const (
	commandQuery            = 0x03
	commandStatementExecute = 0x17
)

// Proxy forwards loopback connections to one MySQL address.
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

// Sessions returns how many client sessions the proxy has accepted.
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
	fault := proxy.nextSessionFault()
	server, err := net.Dial("tcp", proxy.target)
	if err != nil {
		return err
	}
	defer server.Close()
	relay := &serverRelay{client: client, server: server, serverReplied: make(chan struct{})}
	go relay.copyServerToClient()
	for {
		sequence, packet, err := readPacket(client)
		if err != nil {
			return err
		}
		isCommand := sequence == 0 && len(packet) > 4
		isCommit := isCommand && packet[4] == commandQuery && strings.EqualFold(strings.TrimSpace(string(packet[5:])), "commit")
		if isCommit {
			proxy.mu.Lock()
			proxy.commitMessages++
			proxy.mu.Unlock()
		}
		switch {
		case fault == DropAtStatementExecute && isCommand && packet[4] == commandStatementExecute:
			return errors.New("dropped at COM_STMT_EXECUTE")
		case fault == DropBeforeCommitReachesServer && isCommit:
			return errors.New("dropped before COMMIT")
		case fault == DropAfterCommitReachesServer && isCommit:
			relay.startDiscardingServerOutput()
			if _, err := server.Write(packet); err != nil {
				return err
			}
			<-relay.serverReplied
			return errors.New("dropped after COMMIT")
		}
		if _, err := server.Write(packet); err != nil {
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

// readPacket returns one client packet with its four-byte header and its sequence number.
func readPacket(client net.Conn) (byte, []byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(client, header); err != nil {
		return 0, nil, err
	}
	length := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	packet := make([]byte, 4+length)
	copy(packet, header)
	_, err := io.ReadFull(client, packet[4:])
	return header[3], packet, err
}
