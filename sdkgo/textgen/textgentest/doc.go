// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package textgentest proves that a lab connector's generateText Query follows
// the shared text-generation contract.
//
// It provides three tools:
//
//   - FakeProvider, a scripted loopback HTTP provider that never records the
//     credential it checks;
//   - RunTextGenerationExchangeSuite, the provider-exchange conformance suite
//     every lab connector runs without Dex;
//   - RunTextGenerationDexScenarios, the real-Dex scenarios that run one-Step
//     Flows through a real Worker. It uses the Dex Server at
//     DEX_FLOW_SERVICE_ADDRESS, or DefaultDexFlowServiceAddress, and fails
//     when that server is unreachable, so call it from a test file with the
//     integration build tag.
//
// A connector describes its provider API once in a ProviderDialect: the
// credential slot, two model IDs, how to read the model from a recorded
// request, and closures that render each provider reply. Chat Completions
// connectors can use openaichattest.NewProviderDialect instead of writing the
// closures. The suites also take a closure that builds the connector's own
// Query or Step against the fake provider, because each connector generates
// its own Connection and Step config types. The fixture connector in
// sdkgo/integrationtest/fixturellm builds its Query like this:
//
//	func newFixtureQuery(t testing.TB, connection textgentest.FakeConnection) *textgen.TextGenerationQuery {
//		client := newFixtureClient(t, connection)
//		return client.GenerateText()
//	}
//
//	func newFixtureClient(t testing.TB, connection textgentest.FakeConnection) *fixturellm.Client {
//		t.Helper()
//		client, err := fixturellm.New(fixturellm.Config{
//			Model: connection.Model, Endpoint: connection.BaseURL, MaxResponseBytes: connection.MaxResponseBytes,
//		}, sdkgo.StaticCredentialProvider[fixturellm.Credentials]{connection.Reference: {APIKey: connection.APIKey}})
//		require.NoError(t, err)
//		return client
//	}
//
// and passes it as TextGenerationExchangeSuite.NewQuery. Both suites take
// their configuration by pointer and fail the test when it is nil.
//
// The model-ID cases the suite runs are also published as
// testdata/model_id_cases.json, which the TypeScript picker's validator must
// accept and reject identically.
package textgentest
