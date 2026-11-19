// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/go"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v1"
const conformanceFixturePathPrefix = "/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "llm", Name: "llm-conformance"}
	client, err := llm.New(llm.Config{
		Provider: llm.ProviderOpenai, Model: "gpt-6-sol",
	}, sdkgo.StaticCredentialProvider[llm.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sk-llm-conformance-key")},
	}, llm.WithHTTPClient(transport), llm.WithBaseURLForTest(endpoint))
	require.NoError(t, err)
	context := testsupport.NewDexContext("llm-conformance-flow", "llm-conformance-step")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"generateText": newConformanceQueryInvocation(t, scenario.Input, context, client.GenerateText(), reference),
	}}
}
