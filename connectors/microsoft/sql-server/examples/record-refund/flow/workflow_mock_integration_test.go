//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server/sqlservermock"
)

func TestRecordRefundHandlesMockRaceLostWithRealDex(t *testing.T) {
	mock := sqlservermock.New(t, ConnectionName)
	mock.QueryRows().Respond(sqlservermock.QueryRowsCompleted(sqlserver.RowSet{Columns: []sqlserver.Column{
		{Name: "refund_id", TypeName: "BIGINT"}, {Name: "order_id", TypeName: "NVARCHAR"},
		{Name: "amount_usd", TypeName: "DECIMAL"}, {Name: "external_reference", TypeName: "NVARCHAR"},
		{Name: "idempotency_key", TypeName: "UNIQUEIDENTIFIER"}, {Name: "recorded_at", TypeName: "DATETIME2"},
	}, Rows: []map[string]any{}}, nil))
	mock.ExecuteStatement().Respond(sqlservermock.ExecuteStatementRaceLost())
	harness := startTestWorkerWithConnection(t, mock.Connection())
	input := Input{OrderID: "88213", AmountUSD: "250.00", ExternalReference: "tkt_5488"}
	flowID, result := harness.runRefundFlow(t, input)
	outcome := harness.requireCompletedOutcome(t, flowID, result)
	require.Equal(t, StatusAlreadyRecorded, outcome.Status)
	require.Equal(t, input.OrderID, outcome.Refund.OrderID)
	require.Equal(t, input.AmountUSD, outcome.Refund.AmountUSD)
	require.Equal(t, input.ExternalReference, *outcome.Refund.ExternalReference)
	require.False(t, outcome.WasReconciled)

	queries := mock.QueryRows().Calls()
	require.Len(t, queries, 2)
	const findRecordedStatement = "SELECT refund_id, order_id, amount_usd, external_reference, idempotency_key, recorded_at FROM dbo.refund_ledger WHERE order_id = @p1"
	const findInsertedStatement = "SELECT refund_id, order_id, amount_usd, external_reference, idempotency_key, recorded_at FROM dbo.refund_ledger WHERE idempotency_key = CAST(@p1 AS uniqueidentifier) OR order_id = @p2"
	const insertStatement = "INSERT INTO dbo.refund_ledger (order_id, amount_usd, external_reference, idempotency_key)\n" +
		"OUTPUT INSERTED.refund_id, INSERTED.order_id, INSERTED.amount_usd, INSERTED.external_reference, INSERTED.idempotency_key, INSERTED.recorded_at\n" +
		"SELECT @p1, CAST(@p2 AS decimal(12, 2)), @p3, CAST(@p4 AS uniqueidentifier)\n" +
		"WHERE NOT EXISTS (SELECT 1 FROM dbo.refund_ledger WITH (UPDLOCK, HOLDLOCK)\n" +
		"WHERE idempotency_key = CAST(@p4 AS uniqueidentifier) OR order_id = @p1)"
	require.Equal(t, sqlserver.QueryRowsInput{Statement: findRecordedStatement, Parameters: []any{"88213"}}, queries[0].Input)
	require.Equal(t, findInsertedStatement, queries[1].Input.Statement)
	require.Equal(t, flowID, queries[0].FlowID)
	require.Equal(t, flowID, queries[1].FlowID)
	executions := mock.ExecuteStatement().Calls()
	require.Len(t, executions, 1)
	require.Equal(t, flowID, executions[0].FlowID)
	require.Equal(t, sqlserver.QueryRowsInput{
		Statement:  findInsertedStatement,
		Parameters: []any{string(executions[0].IdempotencyKey), "88213"},
	}, queries[1].Input)
	maxRowsAffected := int64(1)
	require.Equal(t, sqlserver.ExecuteStatementInput{
		Statement: insertStatement, Parameters: []any{"88213", "250.00", "tkt_5488"},
		IdempotencyKeyPlaceholder: 4, MaxRowsAffected: &maxRowsAffected, ReturnsOutputRows: true,
	}, executions[0].Input)
	require.Equal(t, sqlserver.ExecuteStatementBranchCompleted, executions[0].Branch)
}

func TestRecordRefundUsesMockInsertDefaultWithRealDex(t *testing.T) {
	mock := sqlservermock.New(t, ConnectionName)
	mock.QueryRows().Respond(sqlservermock.QueryRowsCompleted(sqlserver.RowSet{Columns: []sqlserver.Column{
		{Name: "refund_id", TypeName: "BIGINT"}, {Name: "order_id", TypeName: "NVARCHAR"},
		{Name: "amount_usd", TypeName: "DECIMAL"}, {Name: "external_reference", TypeName: "NVARCHAR"},
		{Name: "idempotency_key", TypeName: "UNIQUEIDENTIFIER"}, {Name: "recorded_at", TypeName: "DATETIME2"},
	}, Rows: []map[string]any{}}, nil))
	harness := startTestWorkerWithConnection(t, mock.Connection())
	flowID, result := harness.runRefundFlow(t, Input{OrderID: "88213", AmountUSD: "250.00", ExternalReference: "tkt_5488"})
	outcome := harness.requireCompletedOutcome(t, flowID, result)
	require.Equal(t, StatusRecorded, outcome.Status)
	require.Equal(t, "1", outcome.Refund.RefundID)
	require.Equal(t, "88213", outcome.Refund.OrderID)
	require.False(t, outcome.WasReconciled)
	require.Len(t, mock.QueryRows().Calls(), 1)
	require.Len(t, mock.ExecuteStatement().Calls(), 1)
}
