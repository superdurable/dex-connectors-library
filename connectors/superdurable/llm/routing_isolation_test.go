// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmrouter_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
)

// TestEachKeyReachesOnlyItsOwnProvider holds all three keys and routes one call to each provider.
func TestEachKeyReachesOnlyItsOwnProvider(t *testing.T) {
	for _, routed := range routedProviders {
		t.Run(string(routed.provider), func(t *testing.T) {
			fakes := newProviderFakes(t)
			fakes.providers[routed.provider].EnqueueReplies(routed.generatedReply("Isolated."))
			client := newRoutedClient(t, fakes, llmrouter.Config{Model: "openai"}, staticCredentials(allTestAPIKeys()))
			result, err := runGenerateText(t, client, userRequest(string(routed.provider)+"/"+routed.dialect.AlternateModel, "Hello"))
			require.NoError(t, err)
			require.Equal(t, llmrouter.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, "Isolated.", result.Value.Text)
			requests := fakes.providers[routed.provider].Requests()
			require.Len(t, requests, 1)
			require.True(t, requests[0].HasCredentialInSlot, "the key travels in the provider's own credential header")
			require.False(t, requests[0].HasCredentialOutsideSlot)
			for _, apiKey := range testAPIKeys {
				if apiKey == routed.apiKey {
					continue
				}
				// The fake removed its own credential header, so every remaining byte is checked.
				require.False(t, strings.Contains(requests[0].Path, apiKey), "a foreign key reached the request path")
				require.False(t, strings.Contains(string(requests[0].Body), apiKey), "a foreign key reached the request body")
				for name, values := range requests[0].Header {
					require.False(t, strings.Contains(name+strings.Join(values, ","), apiKey), "a foreign key reached a request header")
				}
			}
			for _, other := range routedProvidersExcept(routed.provider) {
				require.Empty(t, fakes.providers[other].Requests())
			}
		})
	}
}

// TestProductionHostsReceiveOnlyTheirOwnKey proves each route's public host without a network call.
func TestProductionHostsReceiveOnlyTheirOwnKey(t *testing.T) {
	for _, testCase := range []struct {
		provider                        llmrouter.Provider
		host, credentialHeader, sentKey string
	}{
		{llmrouter.ProviderOpenAI, "api.openai.com", "Authorization", "Bearer " + openAITestKey},
		{llmrouter.ProviderAnthropic, "api.anthropic.com", "Authorization", "Bearer " + anthropicTestKey},
		{llmrouter.ProviderGemini, "generativelanguage.googleapis.com", "X-Goog-Api-Key", geminiTestKey},
	} {
		t.Run(string(testCase.provider), func(t *testing.T) {
			transport := &recordingTransport{}
			client, err := llmrouter.New(llmrouter.Config{Model: string(testCase.provider)}, staticCredentials(allTestAPIKeys()),
				llmrouter.WithHTTPClient(&http.Client{Transport: transport}))
			require.NoError(t, err)
			result, err := runGenerateText(t, client, userRequest("", "Hello"))
			require.NoError(t, err)
			require.Equal(t, llmrouter.GenerateTextBranchProviderRejected, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			requests := transport.recordedRequests()
			require.Len(t, requests, 1)
			require.Equal(t, "https", requests[0].scheme)
			require.Equal(t, testCase.host, requests[0].host)
			require.Equal(t, []string{testCase.sentKey}, requests[0].header.Values(testCase.credentialHeader))
			requests[0].header.Del(testCase.credentialHeader)
			for _, apiKey := range testAPIKeys {
				require.False(t, strings.Contains(requests[0].everythingButTheCredentialHeader(), apiKey),
					"a key reached the URL, the body, or a header other than the credential header")
			}
		})
	}
}

func TestMissingKeySelectsDefectWithoutARequest(t *testing.T) {
	for _, routed := range routedProviders {
		for name, missingKey := range map[string]string{"empty": "", "whitespace only": " \t\n"} {
			t.Run(string(routed.provider)+" "+name, func(t *testing.T) {
				fakes := newProviderFakes(t)
				credentials := routed.withAPIKey(allTestAPIKeys(), missingKey)
				client := newRoutedClient(t, fakes, llmrouter.Config{Model: "openai"}, staticCredentials(credentials))
				result, err := runGenerateText(t, client, userRequest(string(routed.provider)+"/"+routed.dialect.AlternateModel, "Hello"))
				require.NoError(t, err, "a missing key selects defect instead of Retry")
				require.Equal(t, llmrouter.GenerateTextBranchDefect, result.Branch)
				require.Equal(t, &sdkgo.Failure{
					Kind: sdkgo.FailureAuthentication, Provider: "llm", Operation: "generateText",
					Message: "the model selects " + string(routed.provider) + ", but the connection has no " + routed.credentialField,
				}, result.Failure)
				require.Equal(t, routed.dialect.AlternateModel, result.Value.RequestedModel, "the model was valid, so it is reported")
				require.Equal(t, map[llmrouter.Provider]int{
					llmrouter.ProviderOpenAI: 0, llmrouter.ProviderAnthropic: 0, llmrouter.ProviderGemini: 0,
				}, fakes.requestCounts(), "the other keys never select another provider")
			})
		}
	}
	t.Run("a provider alone reports the provider connector's default model", func(t *testing.T) {
		fakes := newProviderFakes(t)
		credentials := routedProviderFor(t, llmrouter.ProviderAnthropic).withAPIKey(llmrouter.Credentials{}, "")
		client := newRoutedClient(t, fakes, llmrouter.Config{Model: "anthropic"}, staticCredentials(credentials))
		result, err := runGenerateText(t, client, userRequest("", "Hello"))
		require.NoError(t, err)
		require.Equal(t, llmrouter.GenerateTextBranchDefect, result.Branch)
		require.Equal(t, "claude-sonnet-5", result.Value.RequestedModel)
	})
}

func TestForeignKeyFormatsSelectDefectWithoutARequest(t *testing.T) {
	// Split so secret scanning does not match the legacy OpenAI key shape.
	const (
		legacyOpenAIKey         = "sk-LlmrouterLegacyKey0" + "T3Blbk" + "FJLlmrouterLegacyKey0"
		unlistedOpenAIPrefixKey = "sk-futureprefix-llmrouter-foreign-canary"
	)
	for _, testCase := range []struct {
		provider   llmrouter.Provider
		foreignKey string
	}{
		{llmrouter.ProviderOpenAI, "sk-ant-api03-llmrouter-foreign-canary"},
		{llmrouter.ProviderOpenAI, "AIzaLlmrouterForeignCanary"},
		{llmrouter.ProviderAnthropic, "sk-proj-llmrouter-foreign-canary"},
		{llmrouter.ProviderAnthropic, "sk-svcacct-llmrouter-foreign-canary"},
		{llmrouter.ProviderAnthropic, "sk-admin-llmrouter-foreign-canary"},
		{llmrouter.ProviderAnthropic, legacyOpenAIKey},
		{llmrouter.ProviderAnthropic, unlistedOpenAIPrefixKey},
		{llmrouter.ProviderAnthropic, "AIzaLlmrouterForeignCanary"},
		{llmrouter.ProviderGemini, "sk-proj-llmrouter-foreign-canary"},
		{llmrouter.ProviderGemini, legacyOpenAIKey},
		{llmrouter.ProviderGemini, unlistedOpenAIPrefixKey},
		{llmrouter.ProviderGemini, "sk-ant-api03-llmrouter-foreign-canary"},
	} {
		t.Run(string(testCase.provider)+" "+testCase.foreignKey[:7], func(t *testing.T) {
			routed := routedProviderFor(t, testCase.provider)
			fakes := newProviderFakes(t)
			client := newRoutedClient(t, fakes, llmrouter.Config{Model: "openai"},
				staticCredentials(routed.withAPIKey(allTestAPIKeys(), testCase.foreignKey)))
			result, err := runGenerateText(t, client, userRequest(string(routed.provider), "Hello"))
			require.NoError(t, err)
			require.Equal(t, llmrouter.GenerateTextBranchDefect, result.Branch)
			require.Equal(t, &sdkgo.Failure{
				Kind: sdkgo.FailureAuthentication, Provider: "llm", Operation: "generateText",
				Message: routed.credentialField + " holds a key in another provider's format",
			}, result.Failure)
			require.False(t, strings.Contains(result.Failure.Message, testCase.foreignKey), "the message never repeats the key")
			require.Equal(t, map[llmrouter.Provider]int{
				llmrouter.ProviderOpenAI: 0, llmrouter.ProviderAnthropic: 0, llmrouter.ProviderGemini: 0,
			}, fakes.requestCounts())
		})
	}
}

// TestUnknownKeyFormatsReachTheirProvider keeps the guard from rejecting a format no routed provider issues.
func TestUnknownKeyFormatsReachTheirProvider(t *testing.T) {
	for _, routed := range routedProviders {
		t.Run(string(routed.provider), func(t *testing.T) {
			const unknownFormatKey = "llmrouter-unknown-format-key"
			fake := llmtest.NewFakeProvider(t, routed.dialect.CredentialHeader, unknownFormatKey)
			fake.EnqueueReplies(routed.generatedReply("Accepted."))
			client, err := llmrouter.New(llmrouter.Config{Model: string(routed.provider)},
				staticCredentials(routed.withAPIKey(llmrouter.Credentials{}, unknownFormatKey)),
				llmrouter.WithProviderBaseURLForTest(routed.provider, fake.BaseURL()))
			require.NoError(t, err)
			result, err := runGenerateText(t, client, userRequest("", "Hello"))
			require.NoError(t, err)
			require.Equal(t, llmrouter.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.True(t, fake.Requests()[0].HasCredentialInSlot)
		})
	}
}

// TestKeyChangedAfterThePreCheckIsCheckedAgainBeforeSending covers a key replaced between the router's read and the pipeline's.
func TestKeyChangedAfterThePreCheckIsCheckedAgainBeforeSending(t *testing.T) {
	claudeRoute := routedProviderFor(t, llmrouter.ProviderAnthropic)
	providerRemoved := allTestAPIKeys()
	providerRemoved.AuthMethodIDs = []string{"openai", "gemini"}
	for name, later := range map[string]llmrouter.Credentials{
		"foreign": claudeRoute.withAPIKey(allTestAPIKeys(), "sk-proj-llmrouter-replacement-canary"),
		"removed": claudeRoute.withAPIKey(allTestAPIKeys(), ""),
		// The key stays behind to prove that the provider list, not the key, gates the request.
		"provider removed with its key left behind": providerRemoved,
	} {
		t.Run(name, func(t *testing.T) {
			fakes := newProviderFakes(t)
			credentials := &changingCredentialProvider{first: allTestAPIKeys(), later: later}
			client := newRoutedClient(t, fakes, llmrouter.Config{Model: "anthropic"}, credentials)
			result, err := runGenerateText(t, client, userRequest("", "Hello"))
			require.NoError(t, err)
			require.Equal(t, int32(2), credentials.resolutions.Load(), "the router and the pipeline each read the key once")
			require.Equal(t, llmrouter.GenerateTextBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			require.Equal(t, "connection credentials are unavailable", result.Failure.Message)
			require.Equal(t, "claude", result.Receipt.Provider, "the Claude pipeline rejected the second read")
			require.False(t, strings.Contains(result.Failure.Message, "canary"))
			require.Equal(t, map[llmrouter.Provider]int{
				llmrouter.ProviderOpenAI: 0, llmrouter.ProviderAnthropic: 0, llmrouter.ProviderGemini: 0,
			}, fakes.requestCounts())
		})
	}
}

func TestUnavailableCredentialsSelectDefectWithoutARequest(t *testing.T) {
	fakes := newProviderFakes(t)
	client := newRoutedClient(t, fakes, llmrouter.Config{Model: "gemini"}, failingCredentialProvider{})
	result, err := runGenerateText(t, client, userRequest("", "Hello"))
	require.NoError(t, err)
	require.Equal(t, llmrouter.GenerateTextBranchDefect, result.Branch)
	require.Equal(t, &sdkgo.Failure{
		Kind: sdkgo.FailureAuthentication, Provider: "llm", Operation: "generateText", Message: "connection credentials are unavailable",
	}, result.Failure)
	require.Equal(t, map[llmrouter.Provider]int{
		llmrouter.ProviderOpenAI: 0, llmrouter.ProviderAnthropic: 0, llmrouter.ProviderGemini: 0,
	}, fakes.requestCounts())
}

// changingCredentialProvider returns first on the first read and later on every read after it.
type changingCredentialProvider struct {
	first, later llmrouter.Credentials
	resolutions  atomic.Int32
}

func (provider *changingCredentialProvider) Resolve(sdkgo.Call) (llmrouter.Credentials, error) {
	if provider.resolutions.Add(1) == 1 {
		return provider.first, nil
	}
	return provider.later, nil
}

type failingCredentialProvider struct{}

func (failingCredentialProvider) Resolve(sdkgo.Call) (llmrouter.Credentials, error) {
	return llmrouter.Credentials{}, errors.New("the connection file is unreadable")
}

// recordingTransport answers every request with a 401 and records it, so no request leaves the process.
type recordingTransport struct {
	mu       sync.Mutex
	requests []recordedTransportRequest
}

type recordedTransportRequest struct {
	scheme, host, pathAndQuery string
	header                     http.Header
	body                       string
}

func (request recordedTransportRequest) everythingButTheCredentialHeader() string {
	var rendered strings.Builder
	rendered.WriteString(request.pathAndQuery + "\n" + request.body + "\n")
	for name, values := range request.header {
		rendered.WriteString(name + ": " + strings.Join(values, ",") + "\n")
	}
	return rendered.String()
}

func (transport *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body := ""
	if request.Body != nil {
		encoded, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		body = string(encoded)
	}
	transport.mu.Lock()
	transport.requests = append(transport.requests, recordedTransportRequest{
		scheme: request.URL.Scheme, host: request.URL.Host, pathAndQuery: request.URL.RequestURI(),
		header: request.Header.Clone(), body: body,
	})
	transport.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusUnauthorized, Header: http.Header{"Content-Type": {"application/json"}},
		Body:    io.NopCloser(strings.NewReader(`{"error":{"type":"authentication_error","message":"invalid key"}}`)),
		Request: request,
	}, nil
}

func (transport *recordingTransport) recordedRequests() []recordedTransportRequest {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return append([]recordedTransportRequest(nil), transport.requests...)
}
