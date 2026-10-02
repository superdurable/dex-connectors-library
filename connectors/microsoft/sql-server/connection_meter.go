// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// controlReadBudgetBytes bounds each connector-owned reply, such as the login, the settings batch, or COMMIT.
const controlReadBudgetBytes = 4 << 20

// errReadBudgetExceeded stops the driver from buffering a reply larger than the configured bounds.
var errReadBudgetExceeded = errors.New("the server's reply exceeded the connector's read bound before it was buffered")

// connectionMeter bounds, counts, and closes every connection the driver dials, including Azure SQL redirects.
type connectionMeter struct {
	deadline time.Time
	host     string

	mu                        sync.Mutex
	connections               []*meteredConnection
	bytesWritten              int64
	readBudget                int64
	bytesReadSinceArmed       int64
	isReadBudgetExceeded      bool
	isServerCertificateDenied bool
}

// meteredConnection reports every read, write, and close of one raw connection to its meter.
type meteredConnection struct {
	net.Conn
	meter    *connectionMeter
	isClosed bool
}

func newConnectionMeter(deadline time.Time, host string) *connectionMeter {
	return &connectionMeter{deadline: deadline, host: host, readBudget: controlReadBudgetBytes}
}

// DialContext is the driver's Dialer; the deadline also bounds COMMIT, which the driver reads with the transaction's context.
func (meter *connectionMeter) DialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	dialer := net.Dialer{KeepAlive: keepAliveInterval}
	connection, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	if !meter.deadline.IsZero() {
		if err := connection.SetDeadline(meter.deadline); err != nil {
			return nil, errors.Join(err, connection.Close())
		}
	}
	metered := &meteredConnection{Conn: connection, meter: meter}
	meter.mu.Lock()
	defer meter.mu.Unlock()
	meter.connections = append(meter.connections, metered)
	return metered, nil
}

// HostName makes the meter a HostDialer, so the driver passes the host name to DialContext instead of resolving it.
func (meter *connectionMeter) HostName() string {
	return meter.host
}

// Read fails once the armed read budget is exhausted, so a huge reply is never fully buffered.
func (connection *meteredConnection) Read(buffer []byte) (int, error) {
	if connection.meter.hasExceededReadBudget() {
		return 0, errReadBudgetExceeded
	}
	count, err := connection.Conn.Read(buffer)
	if !connection.meter.recordRead(count) {
		return count, errReadBudgetExceeded
	}
	return count, err
}

// Write counts the bytes the kernel accepted, including a partial write that failed.
func (connection *meteredConnection) Write(buffer []byte) (int, error) {
	count, err := connection.Conn.Write(buffer)
	connection.meter.recordWrite(count)
	return count, err
}

// Close records that the connection can no longer carry a COMMIT.
func (connection *meteredConnection) Close() error {
	connection.meter.mu.Lock()
	wasClosed := connection.isClosed
	connection.isClosed = true
	connection.meter.mu.Unlock()
	if wasClosed {
		return nil
	}
	return connection.Conn.Close()
}

// armReadBudget restarts the count of bytes read with a new budget; reads beyond it fail the connection.
func (meter *connectionMeter) armReadBudget(budget int64) {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	meter.readBudget, meter.bytesReadSinceArmed = budget, 0
}

func (meter *connectionMeter) hasExceededReadBudget() bool {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	return meter.isReadBudgetExceeded
}

func (meter *connectionMeter) recordCertificateRejection() {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	meter.isServerCertificateDenied = true
}

func (meter *connectionMeter) hasRejectedCertificate() bool {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	return meter.isServerCertificateDenied
}

func (meter *connectionMeter) writtenBytes() int64 {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	return meter.bytesWritten
}

// isClosed reports whether the most recent connection is closed or none was opened.
func (meter *connectionMeter) isClosed() bool {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	return len(meter.connections) == 0 || meter.connections[len(meter.connections)-1].isClosed
}

// shortenDeadline moves the I/O deadline earlier so closing a session never waits on a slow server.
func (meter *connectionMeter) shortenDeadline(remaining time.Duration) {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	deadline := time.Now().Add(remaining)
	if !meter.deadline.IsZero() && meter.deadline.Before(deadline) {
		deadline = meter.deadline
	}
	for _, connection := range meter.connections {
		if !connection.isClosed {
			// A deadline error means the connection is already unusable, which closing handles.
			_ = connection.Conn.SetDeadline(deadline)
		}
	}
}

// closeConnections closes every connection the driver left open, such as one abandoned by a failed login.
func (meter *connectionMeter) closeConnections() {
	meter.mu.Lock()
	connections := append([]*meteredConnection(nil), meter.connections...)
	meter.mu.Unlock()
	for _, connection := range connections {
		// Close errors are ignored: the session is finished and the socket is released either way.
		_ = connection.Close()
	}
}

func (meter *connectionMeter) recordRead(count int) bool {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	meter.bytesReadSinceArmed += int64(count)
	if meter.bytesReadSinceArmed > meter.readBudget {
		meter.isReadBudgetExceeded = true
	}
	return !meter.isReadBudgetExceeded
}

func (meter *connectionMeter) recordWrite(count int) {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	meter.bytesWritten += int64(count)
}
