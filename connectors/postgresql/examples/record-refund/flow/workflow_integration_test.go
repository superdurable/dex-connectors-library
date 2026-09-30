//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql/internal/scriptedpostgresql"
	"github.com/superdurable/dex/sdk-go/dex"
)

const scriptedPassword = "scripted-ledger-password"

var refundLedgerColumns = []scriptedpostgresql.Column{
	{Name: "refund_id", TypeOID: 20}, {Name: "order_id", TypeOID: 25}, {Name: "amount_usd", TypeOID: 1700},
	{Name: "external_reference", TypeOID: 25}, {Name: "idempotency_key", TypeOID: 2950}, {Name: "recorded_at", TypeOID: 1184},
}

// scriptedRefundLedger holds committed rows; like a unique index, an INSERT waits on a key another transaction holds.
type scriptedRefundLedger struct {
	mu              sync.Mutex
	keyResolved     *sync.Cond
	pendingKeys     map[string]bool
	rows            [][][]byte
	commitActions   []scriptedpostgresql.CommitAction
	insertFaults    []bool
	slowInsertDelay time.Duration
}

func newScriptedRefundLedger() *scriptedRefundLedger {
	ledger := &scriptedRefundLedger{pendingKeys: map[string]bool{}}
	ledger.keyResolved = sync.NewCond(&ledger.mu)
	return ledger
}

func (ledger *scriptedRefundLedger) script() scriptedpostgresql.Script {
	return scriptedpostgresql.Script{Describe: ledger.describe, Execute: ledger.execute, Commit: ledger.commit}
}

func (ledger *scriptedRefundLedger) describe(sql string) (scriptedpostgresql.Shape, *scriptedpostgresql.ServerError) {
	switch sql {
	case FindRecordedRefundStatement, FindRefundByIdempotencyKeyStatement:
		return scriptedpostgresql.Shape{ParameterCount: 1, Columns: refundLedgerColumns}, nil
	case InsertRefundStatement:
		return scriptedpostgresql.Shape{ParameterCount: 4, Columns: refundLedgerColumns}, nil
	}
	return scriptedpostgresql.Shape{}, &scriptedpostgresql.ServerError{Code: "42601", Message: "unexpected statement"}
}

func (ledger *scriptedRefundLedger) execute(statement scriptedpostgresql.Statement) scriptedpostgresql.Execution {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	switch statement.SQL {
	case FindRecordedRefundStatement:
		return scriptedpostgresql.Execution{Rows: ledger.rowsWhere(1, statement.Parameters[0]), CommandTag: "SELECT"}
	case FindRefundByIdempotencyKeyStatement:
		return scriptedpostgresql.Execution{Rows: ledger.rowsWhere(4, statement.Parameters[0]), CommandTag: "SELECT"}
	}
	if len(ledger.insertFaults) > 0 {
		isDropped := ledger.insertFaults[0]
		ledger.insertFaults = ledger.insertFaults[1:]
		if isDropped {
			return scriptedpostgresql.Execution{DropConnection: true}
		}
	}
	key := string(statement.Parameters[3])
	for ledger.pendingKeys[key] {
		ledger.keyResolved.Wait()
	}
	if len(ledger.rowsWhere(4, statement.Parameters[3])) > 0 {
		return scriptedpostgresql.Execution{CommandTag: "INSERT 0 0"}
	}
	if len(ledger.rowsWhere(1, statement.Parameters[0])) > 0 {
		return scriptedpostgresql.Execution{Error: &scriptedpostgresql.ServerError{Code: "23505", ConstraintName: "refund_ledger_order_id_key"}}
	}
	row := [][]byte{
		[]byte(strconv.Itoa(len(ledger.rows) + 1)), statement.Parameters[0], statement.Parameters[1], statement.Parameters[2],
		statement.Parameters[3], []byte("2026-09-30 12:00:00.25+00"),
	}
	ledger.pendingKeys[key] = true
	if delay := ledger.slowInsertDelay; delay > 0 {
		ledger.slowInsertDelay = 0
		ledger.mu.Unlock()
		// The delay is the behavior under test: the INSERT outlasts Dex's seven-second local phase.
		time.Sleep(delay)
		ledger.mu.Lock()
	}
	return scriptedpostgresql.Execution{
		Rows: [][][]byte{row}, CommandTag: "INSERT 0 1",
		OnCommit:   func() { ledger.resolveKey(key, row) },
		OnRollback: func() { ledger.resolveKey(key, nil) },
	}
}

func (ledger *scriptedRefundLedger) resolveKey(key string, committedRow [][]byte) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if committedRow != nil {
		ledger.rows = append(ledger.rows, committedRow)
	}
	delete(ledger.pendingKeys, key)
	ledger.keyResolved.Broadcast()
}

func (ledger *scriptedRefundLedger) commit() scriptedpostgresql.CommitAction {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if len(ledger.commitActions) == 0 {
		return scriptedpostgresql.CommitAndReply
	}
	action := ledger.commitActions[0]
	ledger.commitActions = ledger.commitActions[1:]
	return action
}

func (ledger *scriptedRefundLedger) rowsWhere(column int, value []byte) [][][]byte {
	var matching [][][]byte
	for _, row := range ledger.rows {
		if string(row[column]) == string(value) {
			matching = append(matching, row)
		}
	}
	return matching
}

func (ledger *scriptedRefundLedger) committedRowCount() int {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return len(ledger.rows)
}

func startScriptedLedger(t *testing.T, ledger *scriptedRefundLedger) (*scriptedpostgresql.Server, *testWorker) {
	t.Helper()
	server, err := scriptedpostgresql.Start(scriptedPassword, ledger.script())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	config := postgresql.Config{
		Host: "127.0.0.1", Port: int64(server.Port()), Database: "ledger", User: "dex_app", SSLMode: postgresql.SSLModeDisable,
	}
	return server, startTestWorker(t, config, scriptedPassword)
}

func insertExecutions(server *scriptedpostgresql.Server) []scriptedpostgresql.Event {
	var inserts []scriptedpostgresql.Event
	for _, event := range server.EventsOfKind(scriptedpostgresql.EventExecute) {
		if event.Text == InsertRefundStatement {
			inserts = append(inserts, event)
		}
	}
	return inserts
}

func TestRecordRefundExampleWithRealDex(t *testing.T) {
	ledger := newScriptedRefundLedger()
	server, harness := startScriptedLedger(t, ledger)

	t.Run("records a new refund with the Step's idempotency key", func(t *testing.T) {
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "88213", AmountUSD: "250.00", ExternalReference: "tkt_5488"})
		outcome := harness.requireRecordedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		require.False(t, outcome.WasReconciled)
		reference := "tkt_5488"
		require.Equal(t, Refund{
			RefundID: "1", OrderID: "88213", AmountUSD: "250.00", ExternalReference: &reference,
			IdempotencyKey: outcome.Refund.IdempotencyKey, RecordedAt: "2026-09-30T12:00:00.25Z",
		}, outcome.Refund)
		inserts := insertExecutions(server)
		require.Len(t, inserts, 1)
		require.Equal(t, [][]byte{[]byte("88213"), []byte("250.00"), []byte("tkt_5488"), []byte(outcome.Refund.IdempotencyKey)}, inserts[0].Parameters)
		require.Len(t, outcome.Refund.IdempotencyKey, 36, "the key is the Step's UUID Call ID")
		require.Equal(t, 1, ledger.committedRowCount())

		queries := server.EventsOfKind(scriptedpostgresql.EventQuery)
		require.Contains(t, queries[0].Text, "begin transaction read only", "the duplicate check runs read-only")
		require.Contains(t, queries[1].Text, "begin; set local statement_timeout = 5000")
		require.Len(t, server.EventsOfKind(scriptedpostgresql.EventCommit), 1, "only the write commits")
		for _, startup := range server.EventsOfKind(scriptedpostgresql.EventStartup) {
			require.Equal(t, "dex_app", startup.Text)
		}
	})

	t.Run("completes without writing when the order already has a refund", func(t *testing.T) {
		before := len(insertExecutions(server))
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "88213", AmountUSD: "250.00"})
		outcome := harness.requireRecordedOutcome(t, flowID, result)
		require.Equal(t, StatusAlreadyRecorded, outcome.Status)
		require.Equal(t, "1", outcome.Refund.RefundID)
		require.Len(t, insertExecutions(server), before, "no INSERT was sent")
	})

	t.Run("reconciles a COMMIT whose reply was lost after it applied", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.commitActions = []scriptedpostgresql.CommitAction{scriptedpostgresql.CommitThenDropConnection}
		ledger.mu.Unlock()
		before := len(insertExecutions(server))
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "90001", AmountUSD: "41000.00"})
		outcome := harness.requireRecordedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		require.True(t, outcome.WasReconciled, "the uncertain branch read the row back")
		require.Equal(t, "90001", outcome.Refund.OrderID)
		require.Len(t, insertExecutions(server), before+1, "an uncertain write is never retried")
		require.Equal(t, 2, ledger.committedRowCount())
	})

	t.Run("fails for review when an uncertain COMMIT left no row", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.commitActions = []scriptedpostgresql.CommitAction{scriptedpostgresql.DropConnectionWithoutCommitting}
		ledger.mu.Unlock()
		before := len(insertExecutions(server))
		_, result := harness.runRefundFlow(t, Input{OrderID: "90002", AmountUSD: "10.00"})
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Contains(t, result.ErrorMessage, "no refund_ledger row carries this Flow's idempotency key")
		require.Len(t, insertExecutions(server), before+1)
		require.Equal(t, 2, ledger.committedRowCount())
	})

	t.Run("retries a write lost before COMMIT with the same idempotency key", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.insertFaults = []bool{true}
		ledger.mu.Unlock()
		before := len(insertExecutions(server))
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "90003", AmountUSD: "99.00"})
		outcome := harness.requireRecordedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		inserts := insertExecutions(server)[before:]
		require.Len(t, inserts, 2, "Dex retried the Step after the connection was lost")
		require.Equal(t, inserts[0].Parameters[3], inserts[1].Parameters[3], "the retry kept the idempotency key")
		require.Equal(t, 3, ledger.committedRowCount())
	})

	t.Run("an INSERT that outlasts the async local phase writes one row when Dex dispatches it again", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.slowInsertDelay = 8 * time.Second
		ledger.mu.Unlock()
		before := len(insertExecutions(server))
		committedBefore := ledger.committedRowCount()
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "90005", AmountUSD: "8.00"})
		outcome := harness.requireRecordedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		inserts := insertExecutions(server)[before:]
		require.GreaterOrEqual(t, len(inserts), 2, "Dex dispatched InsertRefund again after the local phase")
		for _, insert := range inserts {
			require.Equal(t, outcome.Refund.IdempotencyKey, string(insert.Parameters[3]), "every dispatch carried the Step's key")
		}
		require.Equal(t, committedBefore+1, ledger.committedRowCount(), "the duplicate dispatch wrote nothing")
	})

	t.Run("rejects invalid input before connecting", func(t *testing.T) {
		before := len(server.EventsOfKind(scriptedpostgresql.EventStartup))
		_, result := harness.runRefundFlow(t, Input{OrderID: "90004", AmountUSD: "-5"})
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Len(t, server.EventsOfKind(scriptedpostgresql.EventStartup), before)
	})

	for _, startup := range server.EventsOfKind(scriptedpostgresql.EventStartup) {
		require.Equal(t, scriptedPassword, string(startup.Parameters[0]), "every connection resolved the password from the provider")
	}
}
