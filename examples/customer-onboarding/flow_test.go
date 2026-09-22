// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customeronboarding_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	httpconnector "github.com/superdurable/dex-connectors-library/connectors/http"
	customeronboarding "github.com/superdurable/dex-connectors-library/examples/customer-onboarding"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	mockprovider "github.com/superdurable/dex-connectors-library/test/mock-provider"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFlowDefinitionRegistersDurableAttribute(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	connection := connector.ConnectionRef{Provider: "mock", Name: "default"}
	client, err := httpconnector.New(httpconnector.Config{
		BaseURL: provider.URL(), CredentialHeaders: map[string]string{"api_key": "X-Mock-Api-Key"},
	}, connector.StaticCredentialProvider{connection: connector.NewCredential(map[string]string{"api_key": "test-key"})})
	require.NoError(t, err)
	flow := customeronboarding.NewCustomerOnboardingConnectorFlow(client)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.NotNil(t, registry)
	require.Len(t, flow.GetPersistenceSchema().Attributes, 1)
}
