// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package refundpayment

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestRefundableAmountIsTheUnrefundedBalanceOfASucceededPayment(t *testing.T) {
	require.Equal(t, int64(5000), RefundableAmount(stripe.PaymentIntent{Status: "succeeded", Amount: 7500, AmountRefunded: 2500}))
	require.Zero(t, RefundableAmount(stripe.PaymentIntent{Status: "succeeded", Amount: 7500, AmountRefunded: 7500}))
	require.Zero(t, RefundableAmount(stripe.PaymentIntent{Status: "requires_payment_method", Amount: 7500}))
}

func TestMappersCarryPaymentFields(t *testing.T) {
	require.Equal(t, stripe.GetPaymentIntentInput{PaymentIntentID: "pi_1"}, MapToGetPaymentIntentInput(Input{PaymentIntentID: "pi_1"}))
	request := RefundRequest{PaymentIntent: stripe.PaymentIntent{ID: "pi_1", Status: "succeeded", Amount: 7500, AmountRefunded: 2500}, Reason: "duplicate"}
	require.Equal(t, stripe.CreateRefundInput{PaymentIntentID: "pi_1", Amount: 5000, Reason: "duplicate"}, MapToCreateRefundInput(request))
}

func TestFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(stripe.Connection{})))
	require.Equal(t, recordRefundRequestStepType, dex.GetFinalStepType[Input](recordRefundRequest{}))
	require.Equal(t, planRefundStepType, dex.GetFinalStepType[stripe.GetPaymentIntentResult](planRefund{}))
	require.Equal(t, completeRefundStepType, dex.GetFinalStepType[stripe.CreateRefundResult](completeRefund{}))
	wait, err := recordRefundRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := stripe.New(stripe.Config{}, sdkgo.StaticCredentialProvider[stripe.Credentials]{})
	require.NoError(t, err)
	connection, err := stripe.NewConnection(client, sdkgo.ConnectionRef{Provider: "stripe", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)

	otherConnection, err := stripe.NewConnection(client, sdkgo.ConnectionRef{Provider: "stripe", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection)}) })
}
