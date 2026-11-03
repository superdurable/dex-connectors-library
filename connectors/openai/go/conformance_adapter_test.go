// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	openai "github.com/superdurable/dex-connectors-library/connectors/openai/go"
	"github.com/superdurable/dex-connectors-library/connectors/openai/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v1"
const conformanceFixturePathPrefix = "/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := openai.New(openai.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[openai.Credentials]{
		openAIConnection: {APIKey: sdkgo.NewSecretString("sk-conformance")},
	}, openai.WithHTTPClient(transport))
	require.NoError(t, err)
	context := testsupport.NewDexContext("conformance", "openai-"+scenario.Name)
	previousAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) {
		require.NoError(t, context.RecordHeartbeat(json.RawMessage(value)))
	})
	return &conformanceMappedInvoker{
		t: t, scenario: scenario,
		before: func(attempt int) { context.AttemptNo = previousAttempt + int32(attempt) },
		actions: map[string]conformanceOperationInvocation{
			"createResponse":   newConformanceMutationInvocation(t, scenario.Input, context, client.CreateResponse(), openAIConnection, scenario.CallID),
			"retrieveResponse": newConformanceQueryInvocation(t, scenario.Input, context, client.RetrieveResponse(), openAIConnection),
		},
	}
}
