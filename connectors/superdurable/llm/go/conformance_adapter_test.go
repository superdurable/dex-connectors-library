// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

var conformanceConnection = sdkgo.ConnectionRef{Provider: "llm", Name: "conformance"}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	provider := Provider(strings.SplitN(scenario.Name, "/", 2)[0])
	config := Config{Provider: provider, Model: "mock-model"}
	options := []Option{WithHTTPClient(transport), WithBaseURLForTest(endpoint)}
	if provider == ProviderOpenaiCompatible {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			request.URL.Scheme = "http"
			request.URL.Host = request.Host
			request.RequestURI = ""
			request.Header.Del("Accept-Encoding")
			request.Header.Del("User-Agent")
			response, requestErr := transport.Transport.RoundTrip(request)
			if requestErr != nil {
				if hijacker, isSupported := writer.(http.Hijacker); isSupported {
					connection, _, hijackErr := hijacker.Hijack()
					if hijackErr == nil {
						_ = connection.Close() // The scripted transport failure closes the stand-in exchange.
					}
				}
				return
			}
			defer func() {
				if closeErr := response.Body.Close(); closeErr != nil {
					t.Errorf("close conformance response: %v", closeErr)
				}
			}()
			for name, values := range response.Header {
				for _, value := range values {
					writer.Header().Add(name, value)
				}
			}
			writer.WriteHeader(response.StatusCode)
			if _, copyErr := io.Copy(writer, response.Body); copyErr != nil {
				t.Errorf("copy conformance response: %v", copyErr)
			}
		}))
		t.Cleanup(server.Close)
		config.BaseURL = server.URL + "/v1"
		options = []Option{WithBaseURLForTest(server.URL)}
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{
		conformanceConnection: {APIKey: sdkgo.NewSecretString("sk-conformance-inert")},
	}, options...)
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"generateText": newConformanceQueryInvocation(t, scenario.Input,
			testsupport.NewDexContext("llm-conformance", scenario.Name), client.GenerateText(), conformanceConnection),
	}}
}
