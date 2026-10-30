// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	thisStepKey  = "5f0c7a1e-8f6b-4d8a-9f3c-2d1e0b9a8c7d"
	otherFlowKey = "0b6d3f43-2c55-4f1f-8a3e-0d8c3a9e1f20"
)

func refundLedgerRow(refundID string, key string) map[string]any {
	return map[string]any{
		"refund_id": refundID, "order_id": "88213", "amount_usd": "250.00", "external_reference": nil,
		"idempotency_key": key, "recorded_at": "2026-10-01T12:00:00.25",
	}
}

func TestMapToInsertRefundInputBindsTheIdempotencyKeyLastAndReturnsTheRow(t *testing.T) {
	flow := NewFlow(sqlserver.Connection{})
	input := flow.MapToInsertRefundInput(Input{OrderID: "88213", AmountUSD: "250.00", ExternalReference: "tkt_5488"})
	require.Equal(t, InsertRefundStatement, input.Statement)
	require.Equal(t, []any{"88213", "250.00", "tkt_5488"}, input.Parameters)
	require.Equal(t, len(input.Parameters)+1, input.IdempotencyKeyPlaceholder)
	require.True(t, input.ReturnsOutputRows, "the OUTPUT clause returns the inserted row")
	require.Contains(t, InsertRefundStatement, "WITH (UPDLOCK, HOLDLOCK)", "the existence check holds its range lock until COMMIT")
	require.Contains(t, InsertRefundStatement, "CAST(@p2 AS decimal(12, 2))", "the amount converts exactly from text")
	require.Equal(t, int64(1), *input.MaxRowsAffected)

	withoutReference := flow.MapToInsertRefundInput(Input{OrderID: "88213", AmountUSD: "250.00"})
	require.Nil(t, withoutReference.Parameters[2], "a blank reference is stored as SQL NULL")
}

func TestQueryMappersBindOnlyPositionalParameters(t *testing.T) {
	flow := NewFlow(sqlserver.Connection{})
	find := flow.MapToFindRecordedRefundInput(Input{OrderID: "88213"})
	require.Equal(t, sqlserver.QueryRowsInput{Statement: FindRecordedRefundStatement, Parameters: []any{"88213"}}, find)
	readBack := flow.MapToFindInsertedRefundInput(RefundReadBack{IdempotencyKey: thisStepKey, OrderID: "88213"})
	require.Equal(t, sqlserver.QueryRowsInput{Statement: FindInsertedRefundStatement, Parameters: []any{thisStepKey, "88213"}}, readBack)
}

func TestDecodeRefundReadsTheConnectorTypeMapping(t *testing.T) {
	refund, err := decodeRefund(refundLedgerRow("9007199254740993", thisStepKey))
	require.NoError(t, err)
	require.Equal(t, Refund{
		RefundID: "9007199254740993", OrderID: "88213", AmountUSD: "250.00", IdempotencyKey: thisStepKey, RecordedAt: "2026-10-01T12:00:00.25",
	}, refund)
	_, err = decodeRefund(map[string]any{"refund_id": 7})
	require.Error(t, err, "a refund_id that is not the bigint string mapping is rejected")
}

func TestResolveRefundOutcomeTellsThisFlowsRowFromAnotherFlowsRow(t *testing.T) {
	replayed := sqlserver.ExecuteStatementResult{Branch: sqlserver.ExecuteStatementBranchCompleted, Receipt: sdkgo.Receipt{IdempotencyKey: thisStepKey}}
	thisRow := sqlserver.QueryRowsResult{Branch: sqlserver.QueryRowsBranchCompleted, Value: sqlserver.RowSet{Rows: []map[string]any{refundLedgerRow("17", thisStepKey)}}}
	outcome, err := resolveRefundOutcome(replayed, thisRow)
	require.NoError(t, err)
	require.Equal(t, StatusRecorded, outcome.Status)
	require.True(t, outcome.WasReconciled, "a replay that inserted nothing is confirmed by the read-back")

	uncertain := sqlserver.ExecuteStatementResult{Branch: sqlserver.ExecuteStatementBranchUncertain, Receipt: replayed.Receipt}
	outcome, err = resolveRefundOutcome(uncertain, thisRow)
	require.NoError(t, err)
	require.Equal(t, StatusRecorded, outcome.Status)

	otherRow := sqlserver.QueryRowsResult{Branch: sqlserver.QueryRowsBranchCompleted, Value: sqlserver.RowSet{Rows: []map[string]any{refundLedgerRow("9", otherFlowKey)}}}
	outcome, err = resolveRefundOutcome(replayed, otherRow)
	require.NoError(t, err)
	require.Equal(t, StatusAlreadyRecorded, outcome.Status, "NOT EXISTS matched another Flow's order_id")
	require.Equal(t, otherFlowKey, outcome.Refund.IdempotencyKey)

	_, err = resolveRefundOutcome(uncertain, sqlserver.QueryRowsResult{Branch: sqlserver.QueryRowsBranchCompleted})
	require.ErrorIs(t, err, errRefundNotFound, "an uncertain insert without a row stops for review")
	_, err = resolveRefundOutcome(uncertain, otherRow)
	require.ErrorIs(t, err, errRefundNotFound, "an uncertain insert never claims another Flow's row")
}

func TestRecordRefundRequestRejectsInvalidInputBeforeTheDatabase(t *testing.T) {
	const orderMessage = "orderId is required and must be at most 100 bytes"
	const amountMessage = "amountUsd must be a positive decimal string with at most two fractional digits, such as 250.00"
	for name, testCase := range map[string]struct {
		input   Input
		message string
	}{
		"missing order":        {Input{AmountUSD: "1.00"}, orderMessage},
		"zero amount":          {Input{OrderID: "1", AmountUSD: "0.00"}, amountMessage},
		"three decimal places": {Input{OrderID: "1", AmountUSD: "1.005"}, amountMessage},
		"not a decimal":        {Input{OrderID: "1", AmountUSD: "1e3"}, amountMessage},
	} {
		t.Run(name, func(t *testing.T) {
			decision, err := recordRefundRequest{}.Execute(nil, testCase.input)
			require.NoError(t, err)
			require.Equal(t, dex.ForceFail(testCase.message), decision)
		})
	}
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(sqlserver.Connection{})))
	client, err := sqlserver.New(sqlserver.Config{Host: "db.example.com", Database: "app", User: "dex_app"}, sdkgo.StaticCredentialProvider[sqlserver.Credentials]{})
	require.NoError(t, err)
	connection, err := sqlserver.NewConnection(client, sdkgo.ConnectionRef{Provider: "microsoft-sql-server", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)
	otherConnection, err := sqlserver.NewConnection(client, sdkgo.ConnectionRef{Provider: "microsoft-sql-server", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection)}) })
}
