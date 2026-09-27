// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fixturellm_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/integrationtest/fixturellm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat/openaichattest"
)

// fixtureDialect matches chatProfile: streaming replies, a token-recognized 429 quota error, and a 400 content-policy error.
var fixtureDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
	ConnectionModel: "fixture-model-a", AlternateModel: "fixture-reasoner-b", IsStreaming: true,
	QuotaExhaustedStatusCode: http.StatusTooManyRequests, QuotaExhaustedErrorToken: "fixture_quota_exhausted",
	ContentPolicyErrorToken: "fixture_content_filter",
})

func TestGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveRange, temperature := 1.6, 0.2
	llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{
		Dialect:  fixtureDialect,
		NewQuery: newFixtureQuery,
		LocallyRejectedRequests: []llmtest.NamedTextGenerationRequest{
			{Name: "temperature above the model range", Request: llm.TextGenerationRequest{Temperature: &temperatureAboveRange}},
			{Name: "temperature on a reasoning model", Request: llm.TextGenerationRequest{
				Model: "fixture-reasoner-b", Temperature: &temperature,
			}},
			{Name: "reasoning effort on a model without effort", Request: llm.TextGenerationRequest{
				ReasoningEffort: llm.ReasoningEffortHigh,
			}},
			{Name: "unmapped reasoning effort", Request: llm.TextGenerationRequest{
				Model: "fixture-reasoner-b", ReasoningEffort: llm.ReasoningEffortMax,
			}},
		},
	})
}

func newFixtureQuery(t testing.TB, connection llmtest.FakeConnection) *llm.TextGenerationQuery {
	client := newFixtureClient(t, connection)
	return client.GenerateText()
}

func newFixtureClient(t testing.TB, connection llmtest.FakeConnection) *fixturellm.Client {
	t.Helper()
	client, err := fixturellm.New(fixturellm.Config{
		Model: connection.Model, Endpoint: connection.BaseURL, MaxResponseBytes: connection.MaxResponseBytes,
	}, sdkgo.StaticCredentialProvider[fixturellm.Credentials]{connection.Reference: {APIKey: connection.APIKey}})
	require.NoError(t, err)
	return client
}
