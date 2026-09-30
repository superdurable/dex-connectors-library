// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMapToInsertRefundInputBindsTheIdempotencyKeyLast(t *testing.T) {
	flow := NewFlow(postgresql.Connection{})
	input := flow.MapToInsertRefundInput(Input{OrderID: "88213", AmountUSD: "250.00", ExternalReference: "tkt_5488"})
	require.Equal(t, InsertRefundStatement, input.Statement)
	require.Equal(t, []any{"88213", "250.00", "tkt_5488"}, input.Parameters)
	require.Equal(t, len(input.Parameters)+1, input.IdempotencyKeyPlaceholder)
	require.Contains(t, InsertRefundStatement, "$4")
	require.Contains(t, InsertRefundStatement, "ON CONFLICT (idempotency_key) DO NOTHING")
	require.Equal(t, int64(1), *input.MaxRowsAffected)

	withoutReference := flow.MapToInsertRefundInput(Input{OrderID: "88213", AmountUSD: "250.00"})
	require.Nil(t, withoutReference.Parameters[2], "a blank reference is stored as SQL NULL")
}

func TestQueryMappersBindOnlyPositionalParameters(t *testing.T) {
	flow := NewFlow(postgresql.Connection{})
	find := flow.MapToFindRecordedRefundInput(Input{OrderID: "88213"})
	require.Equal(t, postgresql.QueryRowsInput{Statement: FindRecordedRefundStatement, Parameters: []any{"88213"}}, find)

	reconcile := flow.MapToFindRefundByIdempotencyKeyInput(postgresql.ExecuteStatementResult{
		Branch: postgresql.ExecuteStatementBranchUncertain, Receipt: sdkgo.Receipt{IdempotencyKey: "5f0c7a1e-8f6b-4d8a-9f3c-2d1e0b9a8c7d"},
	})
	require.Equal(t, FindRefundByIdempotencyKeyStatement, reconcile.Statement)
	require.Equal(t, []any{"5f0c7a1e-8f6b-4d8a-9f3c-2d1e0b9a8c7d"}, reconcile.Parameters)
}

func TestDecodeRefundReadsTheConnectorTypeMapping(t *testing.T) {
	refund, err := decodeRefund(map[string]any{
		"refund_id": "9007199254740993", "order_id": "88213", "amount_usd": "250.00", "external_reference": nil,
		"idempotency_key": "5f0c7a1e-8f6b-4d8a-9f3c-2d1e0b9a8c7d", "recorded_at": "2026-09-30T12:00:00Z",
	})
	require.NoError(t, err)
	require.Equal(t, Refund{
		RefundID: "9007199254740993", OrderID: "88213", AmountUSD: "250.00",
		IdempotencyKey: "5f0c7a1e-8f6b-4d8a-9f3c-2d1e0b9a8c7d", RecordedAt: "2026-09-30T12:00:00Z",
	}, refund)

	_, err = decodeRefund(map[string]any{"refund_id": 7})
	require.Error(t, err, "a refund_id that is not the int8 string mapping is rejected")
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

func TestReplayedInsertWithoutARowReadsTheRowBackByItsKey(t *testing.T) {
	replay := postgresql.ExecuteStatementResult{
		Branch: postgresql.ExecuteStatementBranchCompleted, Value: postgresql.StatementExecution{Command: "INSERT", Rows: []map[string]any{}},
		Receipt: sdkgo.Receipt{IdempotencyKey: "5f0c7a1e-8f6b-4d8a-9f3c-2d1e0b9a8c7d"},
	}
	decision, err := completeRefundRecording{}.Execute(nil, replay)
	require.NoError(t, err)
	require.Equal(t, dex.GoTo(sdkgo.StepRef[postgresql.ExecuteStatementResult](findRefundByIdempotencyKeyStepType), replay), decision)

	decision, err = completeReconciledRefund{}.Execute(nil, postgresql.QueryRowsResult{Branch: postgresql.QueryRowsBranchCompleted})
	require.NoError(t, err)
	require.Equal(t, dex.ForceFail("no refund_ledger row carries this Flow's idempotency key; inspect the ledger before recording the refund again"), decision)
}

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(postgresql.Connection{})))
	require.Equal(t, recordRefundRequestStepType, dex.GetFinalStepType[Input](recordRefundRequest{}))
	require.Equal(t, decideRefundRecordingStepType, dex.GetFinalStepType[postgresql.QueryRowsResult](decideRefundRecording{}))
	require.Equal(t, completeRefundRecordingStepType, dex.GetFinalStepType[postgresql.ExecuteStatementResult](completeRefundRecording{}))
	require.Equal(t, completeReconciledRefundStepType, dex.GetFinalStepType[postgresql.QueryRowsResult](completeReconciledRefund{}))
	wait, err := recordRefundRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := postgresql.New(postgresql.Config{Host: "db.example.com", Database: "app", User: "dex_app"}, sdkgo.StaticCredentialProvider[postgresql.Credentials]{})
	require.NoError(t, err)
	connection, err := postgresql.NewConnection(client, sdkgo.ConnectionRef{Provider: "postgresql", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)

	otherConnection, err := postgresql.NewConnection(client, sdkgo.ConnectionRef{Provider: "postgresql", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection)}) })
}
