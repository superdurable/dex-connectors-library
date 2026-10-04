// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fixturellm_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/integrationtest/fixturellm"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/openaichat/openaichattest"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
)

// fixtureDialect matches chatProfile: streaming replies, a token-recognized 429 quota error, and a 400 content-policy error.
var fixtureDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
	ConnectionModel: "fixture-model-a", AlternateModel: "fixture-reasoner-b", IsStreaming: true,
	QuotaExhaustedStatusCode: http.StatusTooManyRequests, QuotaExhaustedErrorToken: "fixture_quota_exhausted",
	ContentPolicyErrorToken: "fixture_content_filter",
})

func TestGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveRange, temperature := 1.6, 0.2
	textgentest.RunTextGenerationExchangeSuite(t, &textgentest.TextGenerationExchangeSuite{
		Dialect:  fixtureDialect,
		NewQuery: newFixtureQuery,
		LocallyRejectedRequests: []textgentest.NamedTextGenerationRequest{
			{Name: "temperature above the model range", Request: textgen.TextGenerationRequest{Temperature: &temperatureAboveRange}},
			{Name: "temperature on a reasoning model", Request: textgen.TextGenerationRequest{
				Model: "fixture-reasoner-b", Temperature: &temperature,
			}},
			{Name: "reasoning effort on a model without effort", Request: textgen.TextGenerationRequest{
				ReasoningEffort: textgen.ReasoningEffortHigh,
			}},
			{Name: "unmapped reasoning effort", Request: textgen.TextGenerationRequest{
				Model: "fixture-reasoner-b", ReasoningEffort: textgen.ReasoningEffortMax,
			}},
		},
	})
}

func newFixtureQuery(t testing.TB, connection textgentest.FakeConnection) *textgen.TextGenerationQuery {
	client := newFixtureClient(t, connection)
	return client.GenerateText()
}

func newFixtureClient(t testing.TB, connection textgentest.FakeConnection) *fixturellm.Client {
	t.Helper()
	client, err := fixturellm.New(fixturellm.Config{
		Model: connection.Model, Endpoint: connection.BaseURL, MaxResponseBytes: connection.MaxResponseBytes,
	}, sdkgo.StaticCredentialProvider[fixturellm.Credentials]{connection.Reference: {APIKey: connection.APIKey}})
	require.NoError(t, err)
	return client
}
