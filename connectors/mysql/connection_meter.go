// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// controlReadBudgetBytes bounds each connector-owned reply, such as the handshake, column definitions, or COMMIT.
const controlReadBudgetBytes = 4 << 20

// errReadBudgetExceeded stops the driver from buffering a reply larger than the configured bounds.
var errReadBudgetExceeded = errors.New("the server's reply exceeded the connector's read bound before it was buffered")

// connectionMeter bounds one call's raw connection by its deadline, counts written bytes, and enforces a read budget.
type connectionMeter struct {
	deadline time.Time

	mu                   sync.Mutex
	connection           net.Conn
	bytesWritten         int64
	readBudget           int64
	bytesReadSinceArmed  int64
	isReadBudgetExceeded bool
	isConnectionClosed   bool
}

// meteredConnection reports every read, write, and close of the raw connection to its meter.
type meteredConnection struct {
	net.Conn
	meter *connectionMeter
}

func newConnectionMeter(deadline time.Time) *connectionMeter {
	return &connectionMeter{deadline: deadline, readBudget: controlReadBudgetBytes}
}

// dial is the driver's DialFunc; the deadline also bounds COMMIT, which the driver sends without a context.
func (meter *connectionMeter) dial(ctx context.Context, network string, address string) (net.Conn, error) {
	dialer := net.Dialer{KeepAlive: 15 * time.Second}
	connection, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	if !meter.deadline.IsZero() {
		if err := connection.SetDeadline(meter.deadline); err != nil {
			return nil, errors.Join(err, connection.Close())
		}
	}
	meter.mu.Lock()
	defer meter.mu.Unlock()
	meter.connection = connection
	return &meteredConnection{Conn: connection, meter: meter}, nil
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
	connection.meter.recordClose()
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

func (meter *connectionMeter) writtenBytes() int64 {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	return meter.bytesWritten
}

func (meter *connectionMeter) isClosed() bool {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	return meter.isConnectionClosed
}

// shortenDeadline moves the I/O deadline earlier so closing a session never waits on a slow server.
func (meter *connectionMeter) shortenDeadline(remaining time.Duration) {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	if meter.connection == nil || meter.isConnectionClosed {
		return
	}
	deadline := time.Now().Add(remaining)
	if !meter.deadline.IsZero() && meter.deadline.Before(deadline) {
		deadline = meter.deadline
	}
	// A deadline error means the connection is already unusable, which closing handles.
	_ = meter.connection.SetDeadline(deadline)
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

func (meter *connectionMeter) recordClose() {
	meter.mu.Lock()
	defer meter.mu.Unlock()
	meter.isConnectionClosed = true
}
