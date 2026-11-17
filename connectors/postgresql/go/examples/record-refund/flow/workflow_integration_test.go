//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql/go"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql/go/internal/scriptedpostgresql"
	"github.com/superdurable/dex/sdk-go/dex"
)

const scriptedPassword = "scripted-ledger-password"

var refundLedgerColumns = []scriptedpostgresql.Column{
	{Name: "refund_id", TypeOID: 20}, {Name: "order_id", TypeOID: 25}, {Name: "amount_usd", TypeOID: 1700},
	{Name: "external_reference", TypeOID: 25}, {Name: "idempotency_key", TypeOID: 2950}, {Name: "recorded_at", TypeOID: 1184},
}

var dailyTotalColumns = []scriptedpostgresql.Column{{Name: "refund_date", TypeOID: 1082}, {Name: "total_usd", TypeOID: 1700}}

// scriptedRefundLedger holds committed rows; like a unique index, an INSERT waits on a key another transaction holds.
type scriptedRefundLedger struct {
	mu                      sync.Mutex
	keyResolved             *sync.Cond
	pendingKeys             map[string]bool
	rows                    [][][]byte
	dailyTotalCents         int64
	commitActions           []scriptedpostgresql.CommitAction
	insertFaults            []bool
	waitForCommitDisconnect bool
}

func newScriptedRefundLedger() *scriptedRefundLedger {
	ledger := &scriptedRefundLedger{pendingKeys: map[string]bool{}}
	ledger.keyResolved = sync.NewCond(&ledger.mu)
	return ledger
}

func (ledger *scriptedRefundLedger) script() scriptedpostgresql.Script {
	return scriptedpostgresql.Script{Describe: ledger.describe, Execute: ledger.execute, Commit: ledger.commit,
		WaitForCommitDisconnect: ledger.takeCommitDisconnect}
}

func (ledger *scriptedRefundLedger) describe(sql string) (scriptedpostgresql.Shape, *scriptedpostgresql.ServerError) {
	switch sql {
	case FindRecordedRefundStatement, FindRefundByIdempotencyKeyStatement:
		return scriptedpostgresql.Shape{ParameterCount: 1, Columns: refundLedgerColumns}, nil
	case InsertRefundStatement:
		return scriptedpostgresql.Shape{ParameterCount: 4, Columns: refundLedgerColumns}, nil
	case AddRefundToDailyTotalStatement:
		return scriptedpostgresql.Shape{ParameterCount: 1, Columns: dailyTotalColumns}, nil
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
	case AddRefundToDailyTotalStatement:
		cents := parseCents(string(statement.Parameters[0]))
		total := ledger.dailyTotalCents + cents
		return scriptedpostgresql.Execution{
			Rows:       [][][]byte{{[]byte("2026-09-30"), []byte(formatCents(total))}},
			CommandTag: "INSERT 0 1",
			OnCommit:   func() { ledger.addToDailyTotal(cents) },
		}
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

func (ledger *scriptedRefundLedger) addToDailyTotal(cents int64) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	ledger.dailyTotalCents += cents
}

func (ledger *scriptedRefundLedger) committedDailyTotalCents() int64 {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return ledger.dailyTotalCents
}

// parseCents reads a validated decimal amount such as "250.00" or "8" as cents.
func parseCents(amount string) int64 {
	whole, fraction, _ := strings.Cut(amount, ".")
	fraction = (fraction + "00")[:2]
	cents, err := strconv.ParseInt(whole+fraction, 10, 64)
	if err != nil {
		panic(err)
	}
	return cents
}

func formatCents(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
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

func (ledger *scriptedRefundLedger) takeCommitDisconnect() bool {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	shouldWait := ledger.waitForCommitDisconnect
	ledger.waitForCommitDisconnect = false
	return shouldWait
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
	return executionsOf(server, InsertRefundStatement)
}

func executionsOf(server *scriptedpostgresql.Server, statement string) []scriptedpostgresql.Event {
	var executions []scriptedpostgresql.Event
	for _, event := range server.EventsOfKind(scriptedpostgresql.EventExecute) {
		if event.Text == statement {
			executions = append(executions, event)
		}
	}
	return executions
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
		require.Len(t, server.EventsOfKind(scriptedpostgresql.EventCommit), 2, "only the two writes commit")
		require.Equal(t, "250.00", outcome.DailyTotalUSD)
		dailyTotals := executionsOf(server, AddRefundToDailyTotalStatement)
		require.Len(t, dailyTotals, 1)
		require.Equal(t, [][]byte{[]byte("250.00")}, dailyTotals[0].Parameters, "executeNonIdempotent binds no idempotency key")
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

	t.Run("retries a COMMIT whose reply was lost after it applied and reads the row back", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.commitActions = []scriptedpostgresql.CommitAction{scriptedpostgresql.CommitThenDropConnection}
		ledger.mu.Unlock()
		before := len(insertExecutions(server))
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "90001", AmountUSD: "41000.00"})
		outcome := harness.requireRecordedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		require.True(t, outcome.WasReconciled, "the replayed INSERT inserted nothing, so the row was read back")
		require.Equal(t, "90001", outcome.Refund.OrderID)
		require.Len(t, insertExecutions(server), before+2, "execute repeats an unknown COMMIT with the same key")
		require.Equal(t, 2, ledger.committedRowCount())
	})

	t.Run("retries a COMMIT lost before it applied and records the refund", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.commitActions = []scriptedpostgresql.CommitAction{scriptedpostgresql.DropConnectionWithoutCommitting}
		ledger.mu.Unlock()
		before := len(insertExecutions(server))
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "90002", AmountUSD: "10.00"})
		outcome := harness.requireRecordedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		require.False(t, outcome.WasReconciled, "the retry inserted the row")
		require.Len(t, insertExecutions(server), before+2)
		require.Equal(t, 3, ledger.committedRowCount())
	})

	t.Run("stops for review when the daily total COMMIT reply was lost", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.commitActions = []scriptedpostgresql.CommitAction{scriptedpostgresql.CommitAndReply, scriptedpostgresql.CommitThenDropConnection}
		ledger.mu.Unlock()
		before := len(executionsOf(server, AddRefundToDailyTotalStatement))
		totalBefore := ledger.committedDailyTotalCents()
		_, result := harness.runRefundFlow(t, Input{OrderID: "90006", AmountUSD: "3.00"})
		require.Equal(t, dex.FlowFailed, result.Status, "the unwired uncertain branch fails the Flow")
		require.Contains(t, result.ErrorMessage, "uncertain")
		require.Len(t, executionsOf(server, AddRefundToDailyTotalStatement), before+1, "executeNonIdempotent never runs the statement again")
		require.Equal(t, totalBefore+300, ledger.committedDailyTotalCents(), "the refund was counted exactly once")
		require.Equal(t, 4, ledger.committedRowCount())
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
		require.Equal(t, 5, ledger.committedRowCount())
	})

	t.Run("an INSERT committed before Worker loss is sent again without a second row", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.waitForCommitDisconnect = true
		ledger.mu.Unlock()
		before := len(insertExecutions(server))
		committedBefore := ledger.committedRowCount()
		flowID := harness.startRefundFlow(t, Input{OrderID: "90005", AmountUSD: "8.00"})
		require.Eventually(t, func() bool {
			return len(insertExecutions(server)) == before+1 && ledger.committedRowCount() == committedBefore+1
		}, 30*time.Second, 10*time.Millisecond)
		harness.replaceWorker(t)
		result := harness.waitForRefundFlow(t, flowID)
		outcome := harness.requireRecordedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		inserts := insertExecutions(server)[before:]
		require.Len(t, inserts, 2, "the replacement Worker repeats InsertRefund once")
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
