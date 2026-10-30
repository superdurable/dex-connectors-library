//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server/go/internal/scriptedtds"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestRecordRefundExampleSurvivesRetriesAndDuplicateDispatch(t *testing.T) {
	ledger := newScriptedRefundLedger()
	server, harness := startScriptedLedger(t, ledger)

	t.Run("retries a write lost before COMMIT with the same idempotency key", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.insertFaults = []bool{true}
		ledger.mu.Unlock()
		flowID, result := harness.runRefundFlow(t, Input{OrderID: "90003", AmountUSD: "99.00"})
		outcome := harness.requireCompletedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		inserts := insertExecutions(server)
		require.Len(t, inserts, 2, "Dex retried the Step after the connection was lost")
		require.Equal(t, inserts[0].Parameters[3], inserts[1].Parameters[3], "the retry kept the idempotency key")
		require.Equal(t, 1, ledger.committedRowCount())
	})

	t.Run("an INSERT committed before Worker loss is sent again without a second row", func(t *testing.T) {
		ledger.mu.Lock()
		ledger.waitForCommitDisconnect = true
		ledger.mu.Unlock()
		before, committedBefore := len(insertExecutions(server)), ledger.committedRowCount()
		started := time.Now()
		flowID := harness.startRefundFlow(t, Input{OrderID: "90005", AmountUSD: "8.00"})
		require.Eventually(t, func() bool {
			return len(insertExecutions(server)) == before+1 && ledger.committedRowCount() == committedBefore+1
		}, 30*time.Second, 10*time.Millisecond)
		harness.replaceWorker(t)
		result := harness.waitForRefundFlow(t, flowID)
		outcome := harness.requireCompletedOutcome(t, flowID, result)
		require.Equal(t, StatusRecorded, outcome.Status)
		inserts := insertExecutions(server)[before:]
		require.Len(t, inserts, 2, "the replacement Worker repeats InsertRefund once")
		for _, insert := range inserts {
			require.Equal(t, outcome.Refund.IdempotencyKey, insert.Parameters[3].Text, "every dispatch carried the Step's key")
		}
		require.Equal(t, committedBefore+1, ledger.committedRowCount(), "the duplicate dispatch wrote nothing")
		t.Logf("InsertRefund dispatches: %d; attentions: %d; reconciled: %t; elapsed: %s",
			len(inserts), len(server.EventsOfKind(scriptedtds.EventAttention)), outcome.WasReconciled, time.Since(started).Round(time.Millisecond))
	})

	t.Run("rejects invalid input before connecting", func(t *testing.T) {
		before := len(server.EventsOfKind(scriptedtds.EventLogin))
		_, result := harness.runRefundFlow(t, Input{OrderID: "90004", AmountUSD: "-5"})
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Len(t, server.EventsOfKind(scriptedtds.EventLogin), before)
	})
}
