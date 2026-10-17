// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package createcheckoutsession

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMapToCreateCheckoutSessionInputPreservesPaymentFields(t *testing.T) {
	input := Input{
		ClientReferenceID: "registration-42", CustomerEmail: "person@example.com",
		SuccessURL: "https://example.com/success", CancelURL: "https://example.com/cancel",
		Currency: "usd", UnitAmount: 7500, ProductName: "Conference ticket",
	}
	providerInput := NewFlow(stripe.Connection{}).MapToCreateCheckoutSessionInput(input)
	require.Equal(t, stripe.CreateACHCheckoutSessionInput{
		ClientReferenceID: input.ClientReferenceID, CustomerEmail: input.CustomerEmail,
		SuccessURL: input.SuccessURL, CancelURL: input.CancelURL,
		Currency: input.Currency, UnitAmount: input.UnitAmount, ProductName: input.ProductName,
	}, providerInput)
}

func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(stripe.Connection{})))
	require.Equal(t, recordCheckoutRequestStepType, dex.GetFinalStepType[Input](recordCheckoutRequest{}))
	require.Equal(t, completeCheckoutStepType, dex.GetFinalStepType[stripe.CreateACHCheckoutSessionResult](completeCheckout{}))
	wait, err := recordCheckoutRequest{}.WaitFor(nil, Input{})
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
