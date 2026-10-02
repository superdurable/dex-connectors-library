//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server/internal/scriptedtds"
	"github.com/superdurable/dex/sdk-go/dex"
)

const scriptedPassword = "scripted-ledger-password"

// startScriptedLedger allows 15-second statements so the nine-second insert below outlasts only Dex's local phase.
func startScriptedLedger(t *testing.T, ledger *scriptedRefundLedger) (*scriptedtds.Server, *testWorker) {
	t.Helper()
	server, err := scriptedtds.Start(scriptedtds.Options{Password: scriptedPassword}, ledger.script())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	config := sqlserver.Config{
		Host: "127.0.0.1", Port: int64(server.Port()), Database: "ledger", User: "dex_app", Encrypt: sqlserver.EncryptDisable,
		ConnectTimeout: 5 * time.Second, StatementTimeout: 15 * time.Second,
	}
	return server, startTestWorker(t, config, scriptedPassword)
}

func TestRecordRefundExampleWithRealDex(t *testing.T) {
	ledger := newScriptedRefundLedger()
	server, harness := startScriptedLedger(t, ledger)

	t.Run("records a new refund with the Step's idempotency key", func(t *testing.T) {
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "88213", AmountUSD: "250.00", ExternalReference: "tkt_5488"})
		outcome := harness.requireCompletedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		require.False(t, outcome.WasReconciled, "the OUTPUT row needed no read-back")
		reference := "tkt_5488"
		require.Equal(t, Refund{
			RefundID: "1", OrderID: "88213", AmountUSD: "250.00", ExternalReference: &reference,
			IdempotencyKey: outcome.Refund.IdempotencyKey, RecordedAt: "2026-10-01T12:00:00.25",
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
		require.Len(t, server.EventsOfKind(scriptedtds.EventBeginTransaction), 2, "the check and the insert each run in a transaction")
		require.Len(t, server.EventsOfKind(scriptedtds.EventCommit), 1, "only the write commits")
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
		require.Equal(t, StatusAlreadyRecorded, outcome.Status, "NOT EXISTS matched order_id and the INSERT returned no row")
		require.Equal(t, racingFlowKey, outcome.Refund.IdempotencyKey)
		require.Equal(t, committedBefore+1, ledger.committedRowCount(), "only the racing Flow's row exists")
	})

	t.Run("reconciles a COMMIT whose reply was lost after it applied", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.commitActions = []scriptedtds.CommitAction{scriptedtds.CommitThenDropConnection}
		ledger.mu.Unlock()
		before, committedBefore := len(insertExecutions(server)), ledger.committedRowCount()
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "90001", AmountUSD: "41000.00"})
		outcome := harness.requireCompletedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		require.True(t, outcome.WasReconciled, "the uncertain branch read the row back by its key")
		require.Len(t, insertExecutions(server), before+1, "an uncertain write is never retried")
		require.Equal(t, committedBefore+1, ledger.committedRowCount())
	})

	t.Run("fails for review when an uncertain COMMIT left no row", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.commitActions = []scriptedtds.CommitAction{scriptedtds.DropConnectionWithoutCommitting}
		ledger.mu.Unlock()
		before, committedBefore := len(insertExecutions(server)), ledger.committedRowCount()
		_, result := harness.runRefundFlow(t, Input{OrderID: "90002", AmountUSD: "10.00"})
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Contains(t, result.ErrorMessage, "no refund_ledger row carries this Flow's idempotency key")
		require.Len(t, insertExecutions(server), before+1)
		require.Equal(t, committedBefore, ledger.committedRowCount())
	})
}
