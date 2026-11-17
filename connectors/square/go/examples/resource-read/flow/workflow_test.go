// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/square/go"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestRequestMappingAndFlowIdentity(t *testing.T) {
	request := Request{Cursor: ""}
	prepared, err := PrepareRequest(request)
	require.NoError(t, err)
	require.Equal(t, square.ListPaymentsInput{Cursor: ""}, MapToOperationInput(prepared))
	require.Equal(t, square.CreatePaymentInput{SourceID: "CASH", AmountMoney: square.Money{Amount: 2900, Currency: "USD"}}, MapToCreatePaymentInput(Request{RecordCashPayment: true, AmountMoney: square.Money{Amount: 2900, Currency: "USD"}}))
	require.Equal(t, square.RefundPaymentInput{PaymentID: "pay_101", AmountMoney: square.Money{Amount: 1000, Currency: "USD"}}, MapToRefundPaymentInput(Request{RefundPaymentID: "pay_101", AmountMoney: square.Money{Amount: 1000, Currency: "USD"}}))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(square.Connection{})))
	require.Equal(t, "PrepareResourceRead", dex.GetFinalStepType[Request](prepareRequest{}))
}
