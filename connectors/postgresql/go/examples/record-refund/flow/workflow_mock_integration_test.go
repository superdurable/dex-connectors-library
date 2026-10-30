//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql/go"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql/go/postgresqlmock"
)

func TestRecordRefundUsesMockWithRealDex(t *testing.T) {
	mock := postgresqlmock.New(t, ConnectionName)
	harness := startTestWorkerForConnection(t, mock.Connection())
	input := Input{OrderID: "order-88213", AmountUSD: "250.00", ExternalReference: "SUP-88213"}
	flowID, result := harness.runRefundFlow(t, input)
	outcome := harness.requireRecordedOutcome(t, flowID, result)
	require.Equal(t, StatusRecorded, outcome.Status)

	queryCalls := mock.QueryRows().Calls()
	require.Len(t, queryCalls, 1)
	require.Equal(t, postgresql.QueryRowsInput{
		Statement: FindRecordedRefundStatement, Parameters: []any{"order-88213"},
	}, queryCalls[0].Input)
	require.Equal(t, postgresql.QueryRowsBranchCompleted, queryCalls[0].Branch)

	executeCalls := mock.ExecuteStatement().Calls()
	require.Len(t, executeCalls, 1)
	maximumRowsAffected := int64(1)
	require.Equal(t, postgresql.ExecuteStatementInput{
		Statement: InsertRefundStatement, Parameters: []any{"order-88213", "250.00", "SUP-88213"},
		IdempotencyKeyPlaceholder: 4, MaxRowsAffected: &maximumRowsAffected,
	}, executeCalls[0].Input)
	require.Equal(t, postgresql.ExecuteStatementBranchCompleted, executeCalls[0].Branch)
}
