// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package forms_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	forms "github.com/superdurable/dex-connectors-library/connectors/google/forms/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath, conformanceFixturePathPrefix = "", "/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := forms.New(forms.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[forms.Credentials]{
		formsConnection: {AccessToken: sdkgo.NewSecretString(formsTestToken)},
	}, forms.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getForm":       newConformanceQueryInvocation(t, scenario.Input, context, client.GetForm(), formsConnection),
		"listResponses": newConformanceQueryInvocation(t, scenario.Input, context, client.ListResponses(), formsConnection),
		"getResponse":   newConformanceQueryInvocation(t, scenario.Input, context, client.GetResponse(), formsConnection),
	}}
}
