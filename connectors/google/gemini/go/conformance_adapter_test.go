// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gemini_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/gemini/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v1beta"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := gemini.New(gemini.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[gemini.Credentials]{
		geminiConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	}, gemini.WithHTTPClient(transport))
	require.NoError(t, err)
	context := testsupport.NewDexContext("conformance", "gemini-generate")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"generateContent": newConformanceQueryInvocation(t, scenario.Input, context, client.GenerateContent(), geminiConnection),
	}}
}
