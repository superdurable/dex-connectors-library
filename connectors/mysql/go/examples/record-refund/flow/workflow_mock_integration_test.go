//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mysql/go"
	"github.com/superdurable/dex-connectors-library/connectors/mysql/go/mysqlmock"
)

func TestRecordRefundUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := mysqlmock.New(t, ConnectionName)
	mock.QueryRows().Respond(mysqlmock.QueryRowsCompleted(mysql.RowSet{Columns: []mysql.Column{
		{Name: "refund_id", TypeName: "UNSIGNED BIGINT"}, {Name: "order_id", TypeName: "VARCHAR"},
		{Name: "amount_usd", TypeName: "DECIMAL"}, {Name: "external_reference", TypeName: "VARCHAR"},
		{Name: "idempotency_key", TypeName: "CHAR"}, {Name: "recorded_at", TypeName: "TIMESTAMP"},
	}, Rows: []map[string]any{}}, nil))
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
	const findRecordedStatement = "SELECT refund_id, order_id, amount_usd, external_reference, idempotency_key, recorded_at FROM refund_ledger WHERE order_id = ?"
	const findInsertedStatement = "SELECT refund_id, order_id, amount_usd, external_reference, idempotency_key, recorded_at FROM refund_ledger WHERE idempotency_key = ? OR refund_id = CAST(? AS UNSIGNED)"
	const insertStatement = `INSERT INTO refund_ledger (order_id, amount_usd, external_reference, idempotency_key)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE refund_id = LAST_INSERT_ID(refund_id)`
	require.Equal(t, mysql.QueryRowsInput{Statement: findRecordedStatement, Parameters: []any{"88213"}}, queries[0].Input)
	require.Equal(t, findInsertedStatement, queries[1].Input.Statement)
	require.Equal(t, flowID, queries[0].FlowID)
	require.Equal(t, flowID, queries[1].FlowID)
	executions := mock.ExecuteStatement().Calls()
	require.Len(t, executions, 1)
	require.Equal(t, flowID, executions[0].FlowID)
	require.Equal(t, mysql.QueryRowsInput{
		Statement:  findInsertedStatement,
		Parameters: []any{string(executions[0].IdempotencyKey), "1"},
	}, queries[1].Input)
	maxRowsAffected := int64(1)
	require.Equal(t, mysql.ExecuteStatementInput{
		Statement: insertStatement, Parameters: []any{"88213", "250.00", "tkt_5488"},
		IdempotencyKeyPlaceholder: 4, MaxRowsAffected: &maxRowsAffected,
	}, executions[0].Input)
	require.Equal(t, mysql.ExecuteStatementBranchCompleted, executions[0].Branch)
}
