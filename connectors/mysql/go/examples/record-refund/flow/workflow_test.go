// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mysql"
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
		"idempotency_key": key, "recorded_at": "2026-09-30T12:00:00.25Z",
	}
}

func TestMapToInsertRefundInputBindsTheIdempotencyKeyLast(t *testing.T) {
	flow := NewFlow(mysql.Connection{})
	input := flow.MapToInsertRefundInput(Input{OrderID: "88213", AmountUSD: "250.00", ExternalReference: "tkt_5488"})
	require.Equal(t, InsertRefundStatement, input.Statement)
	require.Equal(t, []any{"88213", "250.00", "tkt_5488"}, input.Parameters)
	require.Equal(t, len(input.Parameters)+1, input.IdempotencyKeyPlaceholder)
	require.Contains(t, InsertRefundStatement, "VALUES (?, ?, ?, ?)")
	require.Contains(t, InsertRefundStatement, "ON DUPLICATE KEY UPDATE refund_id = LAST_INSERT_ID(refund_id)")
	require.Equal(t, int64(1), *input.MaxRowsAffected)

	withoutReference := flow.MapToInsertRefundInput(Input{OrderID: "88213", AmountUSD: "250.00"})
	require.Nil(t, withoutReference.Parameters[2], "a blank reference is stored as SQL NULL")
}

func TestQueryMappersBindOnlyPositionalParameters(t *testing.T) {
	flow := NewFlow(mysql.Connection{})
	find := flow.MapToFindRecordedRefundInput(Input{OrderID: "88213"})
	require.Equal(t, mysql.QueryRowsInput{Statement: FindRecordedRefundStatement, Parameters: []any{"88213"}}, find)

	readBack := flow.MapToFindInsertedRefundInput(mysql.ExecuteStatementResult{
		Branch: mysql.ExecuteStatementBranchCompleted, Value: mysql.StatementExecution{LastInsertID: "17"},
		Receipt: sdkgo.Receipt{IdempotencyKey: thisStepKey},
	})
	require.Equal(t, FindInsertedRefundStatement, readBack.Statement)
	require.Equal(t, []any{thisStepKey, "17"}, readBack.Parameters)
	require.Contains(t, FindInsertedRefundStatement, "CAST(? AS UNSIGNED)", "the ID is compared as an integer, not as a double")
}

func TestDecodeRefundReadsTheConnectorTypeMapping(t *testing.T) {
	refund, err := decodeRefund(refundLedgerRow("9007199254740993", thisStepKey))
	require.NoError(t, err)
	require.Equal(t, Refund{
		RefundID: "9007199254740993", OrderID: "88213", AmountUSD: "250.00", IdempotencyKey: thisStepKey, RecordedAt: "2026-09-30T12:00:00.25Z",
	}, refund)

	_, err = decodeRefund(map[string]any{"refund_id": 7})
	require.Error(t, err, "a refund_id that is not the BIGINT UNSIGNED string mapping is rejected")
}

func TestResolveRefundOutcomeTellsThisFlowsRowFromAnotherFlowsRow(t *testing.T) {
	inserted := mysql.ExecuteStatementResult{
		Branch: mysql.ExecuteStatementBranchCompleted, Value: mysql.StatementExecution{RowsAffected: 1, LastInsertID: "17"},
		Receipt: sdkgo.Receipt{IdempotencyKey: thisStepKey},
	}
	readBack := mysql.QueryRowsResult{Branch: mysql.QueryRowsBranchCompleted, Value: mysql.RowSet{Rows: []map[string]any{refundLedgerRow("17", thisStepKey)}}}
	outcome, err := resolveRefundOutcome(inserted, readBack)
	require.NoError(t, err)
	require.Equal(t, StatusRecorded, outcome.Status)
	require.False(t, outcome.WasReconciled)

	replayed := inserted
	replayed.Value.RowsAffected = 0
	outcome, err = resolveRefundOutcome(replayed, readBack)
	require.NoError(t, err)
	require.True(t, outcome.WasReconciled, "a replay that changed nothing is confirmed by the read-back")

	uncertain := mysql.ExecuteStatementResult{Branch: mysql.ExecuteStatementBranchUncertain, Value: mysql.StatementExecution{LastInsertID: "0"}, Receipt: inserted.Receipt}
	outcome, err = resolveRefundOutcome(uncertain, readBack)
	require.NoError(t, err)
	require.Equal(t, StatusRecorded, outcome.Status)
	require.True(t, outcome.WasReconciled)

	otherFlow := mysql.QueryRowsResult{Branch: mysql.QueryRowsBranchCompleted, Value: mysql.RowSet{Rows: []map[string]any{refundLedgerRow("9", otherFlowKey)}}}
	outcome, err = resolveRefundOutcome(replayed, otherFlow)
	require.NoError(t, err)
	require.Equal(t, StatusAlreadyRecorded, outcome.Status, "ON DUPLICATE KEY UPDATE matched another Flow's order_id")
	require.Equal(t, otherFlowKey, outcome.Refund.IdempotencyKey)

	_, err = resolveRefundOutcome(uncertain, mysql.QueryRowsResult{Branch: mysql.QueryRowsBranchCompleted})
	require.ErrorIs(t, err, errRefundNotFound, "an uncertain insert without a row stops for review")
	_, err = resolveRefundOutcome(uncertain, otherFlow)
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

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(mysql.Connection{})))
	require.Equal(t, recordRefundRequestStepType, dex.GetFinalStepType[Input](recordRefundRequest{}))
	require.Equal(t, decideRefundRecordingStepType, dex.GetFinalStepType[mysql.QueryRowsResult](decideRefundRecording{}))
	require.Equal(t, completeRefundRecordingStepType, dex.GetFinalStepType[mysql.QueryRowsResult](completeRefundRecording{}))
	wait, err := recordRefundRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := mysql.New(mysql.Config{Host: "db.example.com", Database: "app", User: "dex_app"}, sdkgo.StaticCredentialProvider[mysql.Credentials]{})
	require.NoError(t, err)
	connection, err := mysql.NewConnection(client, sdkgo.ConnectionRef{Provider: "mysql", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)

	otherConnection, err := mysql.NewConnection(client, sdkgo.ConnectionRef{Provider: "mysql", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection)}) })
}
