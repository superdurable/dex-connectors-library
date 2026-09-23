// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customeronboarding_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	httpconnector "github.com/superdurable/dex-connectors-library/connectors/http"
	customeronboarding "github.com/superdurable/dex-connectors-library/examples/customer-onboarding"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFlowDefinitionRegistersDurableAttribute(t *testing.T) {
	connection := connector.ConnectionRef{Provider: "mock", Name: "default"}
	client, err := httpconnector.New(httpconnector.Config{
		BaseURL: "http://127.0.0.1:1", CredentialHeaders: map[string]string{"api_key": "X-Mock-Api-Key"},
	}, connector.StaticCredentialProvider[httpconnector.Credentials]{
		connection: {APIKey: connector.NewSecretString("test-key")},
	})
	require.NoError(t, err)
	typedConnection, err := httpconnector.NewConnection(client, connection)
	require.NoError(t, err)
	flow := customeronboarding.NewCustomerOnboardingConnectorFlow(typedConnection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.NotNil(t, registry)
	require.Len(t, flow.GetPersistenceSchema().Attributes, 1)
	require.Len(t, flow.GetRPCs(), 2)
}

func TestFlowRejectsZeroTypedConnection(t *testing.T) {
	flow := customeronboarding.NewCustomerOnboardingConnectorFlow(httpconnector.Connection{})
	require.Panics(t, func() {
		flow.GetSteps()
	})
}

func TestStartInputDoesNotExposeConnectionSelection(t *testing.T) {
	encoded, err := json.Marshal(customeronboarding.Input{CustomerID: "customer-1", Credits: 100})
	require.NoError(t, err)
	require.JSONEq(t, `{"customerId":"customer-1","credits":100}`, string(encoded))
}
