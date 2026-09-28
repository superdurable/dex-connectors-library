// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmrouter_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
)

// TestEveryRouteFollowsTheExchangeContract runs the shared exchange suite on each provider Query exactly as New built it.
func TestEveryRouteFollowsTheExchangeContract(t *testing.T) {
	for _, routed := range routedProviders {
		t.Run(string(routed.provider), func(t *testing.T) {
			llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{
				Dialect: routed.exchangeSuiteDialect(),
				NewQuery: func(t testing.TB, connection llmtest.FakeConnection) *llm.TextGenerationQuery {
					query := llmrouter.RouteGenerateTextQueryForTest(newFakeConnectionClient(t, routed, connection), routed.provider)
					require.NotNil(t, query)
					return query
				},
			})
		})
	}
}

// newFakeConnectionClient qualifies the suite's connection model and puts its canary in this provider's key field only.
func newFakeConnectionClient(t testing.TB, routed routedProvider, connection llmtest.FakeConnection) *llmrouter.Client {
	t.Helper()
	credentials := routed.withAPIKey(llmrouter.Credentials{}, routed.sharedSuiteKeyPrefix+connection.APIKey.Reveal())
	client, err := llmrouter.New(llmrouter.Config{
		Model: string(routed.provider) + "/" + connection.Model, MaxResponseBytes: connection.MaxResponseBytes,
	}, sdkgo.StaticCredentialProvider[llmrouter.Credentials]{connection.Reference: credentials},
		llmrouter.WithProviderBaseURLForTest(routed.provider, connection.BaseURL))
	require.NoError(t, err)
	return client
}
