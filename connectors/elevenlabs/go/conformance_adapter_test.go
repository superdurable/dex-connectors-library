// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elevenlabs

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	}
	if scenario.Name == "listModels-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listModels":          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListModels(), testConnection),
		"getVoice":            newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetVoice(), testConnection),
		"updateVoiceSettings": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateVoiceSettings(), testConnection, scenario.CallID),
		"listVoices":          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListVoices(), testConnection),
		"createSpeech":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.CreateSpeech(), testConnection),
	}}
}
