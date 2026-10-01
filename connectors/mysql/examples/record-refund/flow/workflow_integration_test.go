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
	"github.com/superdurable/dex-connectors-library/connectors/mysql"
	"github.com/superdurable/dex-connectors-library/connectors/mysql/internal/scriptedmysql"
	"github.com/superdurable/dex/sdk-go/dex"
)

const scriptedPassword = "scripted-ledger-password"

var refundLedgerColumns = []scriptedmysql.Column{
	{Name: "refund_id", Type: scriptedmysql.TypeLongLong, Flags: scriptedmysql.FlagUnsigned},
	{Name: "order_id", Type: scriptedmysql.TypeVarString}, {Name: "amount_usd", Type: scriptedmysql.TypeNewDecimal, Decimals: 2},
	{Name: "external_reference", Type: scriptedmysql.TypeVarString}, {Name: "idempotency_key", Type: scriptedmysql.TypeString},
	{Name: "recorded_at", Type: scriptedmysql.TypeTimestamp, Decimals: 6},
}

// ledgerRow is one committed scripted refund_ledger row.
type ledgerRow struct {
	refundID          uint64
	orderID           string
	amountUSD         string
	externalReference any
	idempotencyKey    string
}

func (row ledgerRow) values() []any {
	return []any{row.refundID, row.orderID, row.amountUSD, row.externalReference, row.idempotencyKey, "2026-09-30 12:00:00.250000"}
}

// scriptedRefundLedger holds committed rows; like a unique index, an INSERT waits on a key another transaction holds.
type scriptedRefundLedger struct {
	mu                      sync.Mutex
	keyResolved             *sync.Cond
	pendingKeys             map[string]bool
	pendingOrders           map[string]bool
	rows                    []ledgerRow
	nextRefundID            uint64
	commitActions           []scriptedmysql.CommitAction
	insertFaults            []bool
	slowInsertDelay         time.Duration
	racingOrderBeforeInsert string
}

func newScriptedRefundLedger() *scriptedRefundLedger {
	ledger := &scriptedRefundLedger{pendingKeys: map[string]bool{}, pendingOrders: map[string]bool{}, nextRefundID: 1}
	ledger.keyResolved = sync.NewCond(&ledger.mu)
	return ledger
}

func (ledger *scriptedRefundLedger) script() scriptedmysql.Script {
	return scriptedmysql.Script{Prepare: ledger.prepare, Execute: ledger.execute, Commit: ledger.commit}
}

func (ledger *scriptedRefundLedger) prepare(sql string) (scriptedmysql.Shape, *scriptedmysql.ServerError) {
	switch sql {
	case FindRecordedRefundStatement:
		return scriptedmysql.Shape{ParameterCount: 1}, nil
	case FindInsertedRefundStatement:
		return scriptedmysql.Shape{ParameterCount: 2}, nil
	case InsertRefundStatement:
		return scriptedmysql.Shape{ParameterCount: 4}, nil
	}
	return scriptedmysql.Shape{}, &scriptedmysql.ServerError{Number: 1064, SQLState: "42000", Message: "unexpected statement"}
}

func (ledger *scriptedRefundLedger) execute(statement scriptedmysql.Statement) scriptedmysql.Execution {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	switch statement.SQL {
	case FindRecordedRefundStatement:
		return ledger.selectRows(func(row ledgerRow) bool { return row.orderID == statement.Parameters[0].Text })
	case FindInsertedRefundStatement:
		refundID, _ := strconv.ParseUint(statement.Parameters[1].Text, 10, 64)
		return ledger.selectRows(func(row ledgerRow) bool {
			return row.idempotencyKey == statement.Parameters[0].Text || row.refundID == refundID
		})
	}
	if len(ledger.insertFaults) > 0 {
		isDropped := ledger.insertFaults[0]
		ledger.insertFaults = ledger.insertFaults[1:]
		if isDropped {
			return scriptedmysql.Execution{DropConnection: true}
		}
	}
	orderID, key := statement.Parameters[0].Text, statement.Parameters[3].Text
	if ledger.racingOrderBeforeInsert == orderID {
		ledger.racingOrderBeforeInsert = ""
		ledger.appendRow(ledgerRow{orderID: orderID, amountUSD: "1.00", idempotencyKey: "racing-flow-key"})
	}
	for ledger.pendingKeys[key] || ledger.pendingOrders[orderID] {
		ledger.keyResolved.Wait()
	}
	for _, row := range ledger.rows {
		if row.idempotencyKey == key || row.orderID == orderID {
			return scriptedmysql.Execution{AffectedRows: 0, LastInsertID: row.refundID}
		}
	}
	var externalReference any
	if !statement.Parameters[2].IsNull {
		externalReference = statement.Parameters[2].Text
	}
	row := ledgerRow{refundID: ledger.nextRefundID, orderID: orderID, amountUSD: statement.Parameters[1].Text, externalReference: externalReference, idempotencyKey: key}
	ledger.nextRefundID++
	ledger.pendingKeys[key], ledger.pendingOrders[orderID] = true, true
	if delay := ledger.slowInsertDelay; delay > 0 {
		ledger.slowInsertDelay = 0
		ledger.mu.Unlock()
		// The delay is the behavior under test: the INSERT outlasts Dex's seven-second local phase.
		time.Sleep(delay)
		ledger.mu.Lock()
	}
	return scriptedmysql.Execution{
		AffectedRows: 1, LastInsertID: row.refundID,
		OnCommit:   func() { ledger.resolveInsert(row, true) },
		OnRollback: func() { ledger.resolveInsert(row, false) },
	}
}

func (ledger *scriptedRefundLedger) selectRows(matches func(ledgerRow) bool) scriptedmysql.Execution {
	execution := scriptedmysql.Execution{Columns: refundLedgerColumns, Rows: [][]any{}}
	for _, row := range ledger.rows {
		if matches(row) {
			execution.Rows = append(execution.Rows, row.values())
		}
	}
	return execution
}

func (ledger *scriptedRefundLedger) resolveInsert(row ledgerRow, isCommitted bool) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if isCommitted {
		ledger.rows = append(ledger.rows, row)
	}
	delete(ledger.pendingKeys, row.idempotencyKey)
	delete(ledger.pendingOrders, row.orderID)
	ledger.keyResolved.Broadcast()
}

func (ledger *scriptedRefundLedger) appendRow(row ledgerRow) {
	row.refundID = ledger.nextRefundID
	ledger.nextRefundID++
	ledger.rows = append(ledger.rows, row)
}

func (ledger *scriptedRefundLedger) commit() scriptedmysql.CommitAction {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if len(ledger.commitActions) == 0 {
		return scriptedmysql.CommitAndReply
	}
	action := ledger.commitActions[0]
	ledger.commitActions = ledger.commitActions[1:]
	return action
}

func (ledger *scriptedRefundLedger) committedRowCount() int {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return len(ledger.rows)
}

func startScriptedLedger(t *testing.T, ledger *scriptedRefundLedger) (*scriptedmysql.Server, *testWorker) {
	t.Helper()
	server, err := scriptedmysql.Start(scriptedmysql.Options{Password: scriptedPassword}, ledger.script())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	config := mysql.Config{
		Host: "127.0.0.1", Port: int64(server.Port()), Database: "ledger", User: "dex_app", SSLMode: mysql.SSLModeDisabled,
	}
	return server, startTestWorker(t, config, scriptedPassword)
}

func insertExecutions(server *scriptedmysql.Server) []scriptedmysql.Event {
	var inserts []scriptedmysql.Event
	for _, event := range server.EventsOfKind(scriptedmysql.EventExecute) {
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
		outcome := harness.requireCompletedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		require.False(t, outcome.WasReconciled)
		reference := "tkt_5488"
		require.Equal(t, Refund{
			RefundID: "1", OrderID: "88213", AmountUSD: "250.00", ExternalReference: &reference,
			IdempotencyKey: outcome.Refund.IdempotencyKey, RecordedAt: "2026-09-30T12:00:00.25Z",
		}, outcome.Refund)
		inserts := insertExecutions(server)
		require.Len(t, inserts, 1)
		parameters := make([]string, len(inserts[0].Parameters))
		for index, parameter := range inserts[0].Parameters {
			parameters[index] = parameter.Text
		}
		require.Equal(t, []string{"88213", "250.00", "tkt_5488", outcome.Refund.IdempotencyKey}, parameters)
		require.Len(t, outcome.Refund.IdempotencyKey, 36, "the key is the Step's UUID Call ID")
		require.Equal(t, 1, ledger.committedRowCount())

		var transactions []string
		for _, event := range server.EventsOfKind(scriptedmysql.EventQuery) {
			if event.Text == "START TRANSACTION" || event.Text == "START TRANSACTION READ ONLY" {
				transactions = append(transactions, event.Text)
			}
		}
		require.Equal(t, []string{"START TRANSACTION READ ONLY", "START TRANSACTION", "START TRANSACTION READ ONLY"}, transactions,
			"both reads run read-only and only the insert can write")
		require.Len(t, server.EventsOfKind(scriptedmysql.EventCommit), 1, "only the write commits")
		for _, handshake := range server.EventsOfKind(scriptedmysql.EventHandshake) {
			require.Equal(t, "dex_app", handshake.Text)
		}
	})

	t.Run("completes without writing when the order already has a refund", func(t *testing.T) {
		before := len(insertExecutions(server))
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "88213", AmountUSD: "250.00"})
		outcome := harness.requireCompletedOutcome(t, flowID, result)
		require.Equal(t, StatusAlreadyRecorded, outcome.Status)
		require.Equal(t, "1", outcome.Refund.RefundID)
		require.Len(t, insertExecutions(server), before, "no INSERT was sent")
	})

	t.Run("reports another Flow's refund that won the race between the check and the insert", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.racingOrderBeforeInsert = "90006"
		ledger.mu.Unlock()
		committedBefore := ledger.committedRowCount()
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "90006", AmountUSD: "6.00"})
		outcome := harness.requireCompletedOutcome(t, flowID, result)
		require.Equal(t, StatusAlreadyRecorded, outcome.Status, "ON DUPLICATE KEY UPDATE matched order_id and changed nothing")
		require.Equal(t, "racing-flow-key", outcome.Refund.IdempotencyKey)
		require.Equal(t, committedBefore+1, ledger.committedRowCount(), "only the racing Flow's row exists")
	})

	t.Run("reconciles a COMMIT whose reply was lost after it applied", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.commitActions = []scriptedmysql.CommitAction{scriptedmysql.CommitThenDropConnection}
		ledger.mu.Unlock()
		before := len(insertExecutions(server))
		committedBefore := ledger.committedRowCount()
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "90001", AmountUSD: "41000.00"})
		outcome := harness.requireCompletedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		require.True(t, outcome.WasReconciled, "the uncertain branch read the row back by its key")
		require.Equal(t, "90001", outcome.Refund.OrderID)
		require.Len(t, insertExecutions(server), before+1, "an uncertain write is never retried")
		require.Equal(t, committedBefore+1, ledger.committedRowCount())
	})

	t.Run("fails for review when an uncertain COMMIT left no row", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.commitActions = []scriptedmysql.CommitAction{scriptedmysql.DropConnectionWithoutCommitting}
		ledger.mu.Unlock()
		before := len(insertExecutions(server))
		committedBefore := ledger.committedRowCount()
		_, result := harness.runRefundFlow(t, Input{OrderID: "90002", AmountUSD: "10.00"})
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Contains(t, result.ErrorMessage, "no refund_ledger row carries this Flow's idempotency key")
		require.Len(t, insertExecutions(server), before+1)
		require.Equal(t, committedBefore, ledger.committedRowCount())
	})

	t.Run("retries a write lost before COMMIT with the same idempotency key", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.insertFaults = []bool{true}
		ledger.mu.Unlock()
		before := len(insertExecutions(server))
		committedBefore := ledger.committedRowCount()
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "90003", AmountUSD: "99.00"})
		outcome := harness.requireCompletedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		inserts := insertExecutions(server)[before:]
		require.Len(t, inserts, 2, "Dex retried the Step after the connection was lost")
		require.Equal(t, inserts[0].Parameters[3], inserts[1].Parameters[3], "the retry kept the idempotency key")
		require.Equal(t, committedBefore+1, ledger.committedRowCount())
	})

	t.Run("an INSERT that outlasts the async local phase writes one row when Dex dispatches it again", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.slowInsertDelay = 9 * time.Second
		ledger.mu.Unlock()
		before := len(insertExecutions(server))
		committedBefore := ledger.committedRowCount()
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "90005", AmountUSD: "8.00"})
		outcome := harness.requireCompletedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		inserts := insertExecutions(server)[before:]
		require.GreaterOrEqual(t, len(inserts), 2, "Dex dispatched InsertRefund again after the local phase")
		for _, insert := range inserts {
			require.Equal(t, outcome.Refund.IdempotencyKey, insert.Parameters[3].Text, "every dispatch carried the Step's key")
		}
		require.Equal(t, committedBefore+1, ledger.committedRowCount(), "the duplicate dispatch wrote nothing")
	})

	t.Run("rejects invalid input before connecting", func(t *testing.T) {
		before := len(server.EventsOfKind(scriptedmysql.EventHandshake))
		_, result := harness.runRefundFlow(t, Input{OrderID: "90004", AmountUSD: "-5"})
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Len(t, server.EventsOfKind(scriptedmysql.EventHandshake), before)
	})
}
