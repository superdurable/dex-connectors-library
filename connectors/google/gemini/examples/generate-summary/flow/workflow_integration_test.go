//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package generatesummary

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationAPIKey = "AIzaSENTINEL-integration-key"

func TestGenerateSummaryExampleRoutesGeminiOutcomesWithRealDex(t *testing.T) {
	provider := newSummaryProvider(t)
	flow, harness := newSummaryIntegrationHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)

	t.Run("generated summary completes the Flow", func(t *testing.T) {
		flowID := "gemini-summary-generated-" + testRunID
		result := harness.runFlow(t, ctx, flow, flowID, SummaryRequest{Title: "Launch", Text: "GENERATE " + testRunID})
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var outcome SummaryOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, StatusGenerated, outcome.Status)
		require.Equal(t, "Launch", outcome.Title)
		require.Equal(t, &Summary{Headline: "Connector ships", Summary: "It works.", KeyPoints: []string{"typed", "durable"}}, outcome.Summary)
		require.Equal(t, "STOP", outcome.FinishReason)
		require.Equal(t, 42, outcome.Usage.TotalTokens)
		requests := provider.requestsFor("GENERATE " + testRunID)
		require.Len(t, requests, 1)
		require.Equal(t, "/v1beta/models/gemini-3.8-flash:generateContent", requests[0].path, "the connection model applies")
		require.Empty(t, requests[0].query)
		require.Equal(t, integrationAPIKey, requests[0].apiKey)
		require.Equal(t, "application/json", requests[0].generationConfig["responseMimeType"])
		require.NotNil(t, requests[0].generationConfig["responseJsonSchema"])
		require.NotContains(t, requests[0].generationConfig, "temperature", "Gemini 3 models keep the default temperature")
		require.NotContains(t, requests[0].generationConfig, "thinkingConfig", "Gemini 3 models keep the default thinking level")

		var display map[string]any
		require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.GetDexDisplay, nil, &display))
		encodedDisplay, err := json.Marshal(display)
		require.NoError(t, err)
		require.Contains(t, string(encodedDisplay), "Connector ships")
		require.NotContains(t, string(encodedDisplay), integrationAPIKey)
	})

	t.Run("wired blocked branch completes without a summary", func(t *testing.T) {
		result := harness.runFlow(t, ctx, flow, "gemini-summary-blocked-"+testRunID, SummaryRequest{Title: "Blocked", Text: "BLOCK " + testRunID})
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var outcome SummaryOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, StatusBlocked, outcome.Status)
		require.Equal(t, "SAFETY", outcome.BlockReason)
		require.Nil(t, outcome.Summary)
		require.Len(t, provider.requestsFor("BLOCK "+testRunID), 1)
	})

	t.Run("wired truncated branch completes without a summary", func(t *testing.T) {
		result := harness.runFlow(t, ctx, flow, "gemini-summary-truncated-"+testRunID, SummaryRequest{Title: "Long", Text: "TRUNCATE " + testRunID})
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var outcome SummaryOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, StatusTruncated, outcome.Status)
		require.Equal(t, "MAX_TOKENS", outcome.FinishReason)
		require.Len(t, provider.requestsFor("TRUNCATE "+testRunID), 1)
	})

	t.Run("unwired providerRejected branch fails the Flow without a retry", func(t *testing.T) {
		result := harness.runFlow(t, ctx, flow, "gemini-summary-rejected-"+testRunID, SummaryRequest{Title: "Rejected", Text: "REJECT " + testRunID})
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Len(t, provider.requestsFor("REJECT "+testRunID), 1, "a conclusive rejection is not retried")
	})

	t.Run("invalid start input fails before calling Gemini", func(t *testing.T) {
		result := harness.runFlow(t, ctx, flow, "gemini-summary-invalid-"+testRunID, SummaryRequest{Title: "Empty", Text: "  "})
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Empty(t, provider.requestsFor("  "))
	})
}

type summaryProviderRequest struct {
	path             string
	query            string
	apiKey           string
	prompt           string
	generationConfig map[string]any
}

// summaryProvider is a local Gemini API. The prompt's first word selects the outcome.
type summaryProvider struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []summaryProviderRequest
}

func newSummaryProvider(t *testing.T) *summaryProvider {
	t.Helper()
	provider := &summaryProvider{}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *summaryProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	contents, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(response, "unreadable", http.StatusBadRequest)
		return
	}
	var body struct {
		Contents []struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
		GenerationConfig map[string]any `json:"generationConfig"`
	}
	if err := json.Unmarshal(contents, &body); err != nil || len(body.Contents) == 0 || len(body.Contents[0].Parts) == 0 {
		http.Error(response, "invalid", http.StatusBadRequest)
		return
	}
	prompt := body.Contents[0].Parts[0].Text
	provider.mutex.Lock()
	provider.requests = append(provider.requests, summaryProviderRequest{
		path: request.URL.Path, query: request.URL.RawQuery, apiKey: request.Header.Get("x-goog-api-key"),
		prompt: prompt, generationConfig: body.GenerationConfig,
	})
	provider.mutex.Unlock()
	text := prompt[strings.Index(prompt, "\n\n")+2:]
	response.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasPrefix(text, "GENERATE "):
		summary := `{"headline":"Connector ships","summary":"It works.","keyPoints":["typed","durable"]}`
		writeProviderResponse(response, http.StatusOK, `{"candidates":[{"content":{"role":"model","parts":[{"text":`+strconv.Quote(summary)+`}]},"finishReason":"STOP"}],`+
			`"usageMetadata":{"promptTokenCount":30,"candidatesTokenCount":12,"totalTokenCount":42},"modelVersion":"gemini-3.5-flash-lite","responseId":"resp-summary"}`)
	case strings.HasPrefix(text, "BLOCK "):
		writeProviderResponse(response, http.StatusOK, `{"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{"promptTokenCount":30,"totalTokenCount":30}}`)
	case strings.HasPrefix(text, "TRUNCATE "):
		writeProviderResponse(response, http.StatusOK, `{"candidates":[{"content":{"role":"model","parts":[{"text":"{\"headline\":\"Conn"}]},"finishReason":"MAX_TOKENS"}]}`)
	case strings.HasPrefix(text, "REJECT "):
		writeProviderResponse(response, http.StatusBadRequest, `{"error":{"code":400,"message":"SENTINEL invalid argument","status":"INVALID_ARGUMENT"}}`)
	default:
		writeProviderResponse(response, http.StatusInternalServerError, `{"error":{"code":500,"status":"INTERNAL"}}`)
	}
}

func writeProviderResponse(response http.ResponseWriter, status int, body string) {
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		panic(err)
	}
}

func (provider *summaryProvider) requestsFor(text string) []summaryProviderRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var matching []summaryProviderRequest
	for _, request := range provider.requests {
		if strings.HasSuffix(request.prompt, "\n\n"+text) {
			matching = append(matching, request)
		}
	}
	return matching
}

type summaryIntegrationHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newSummaryIntegrationHarness(t *testing.T, providerURL string) (*Flow, *summaryIntegrationHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName}
	providerClient, err := gemini.New(gemini.Config{Endpoint: providerURL + "/v1beta", Model: "gemini-3.8-flash"}, sdkgo.StaticCredentialProvider[gemini.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(integrationAPIKey)},
	})
	require.NoError(t, err)
	connection, err := gemini.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &summaryIntegrationHarness{
		registry: registry, cache: cache, serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"), workerAddress: workerAddress,
	}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if harness.worker != nil {
			harness.stopWorker(t)
		}
		require.NoError(t, errors.Join(harness.client.Close(), harness.cache.Close()))
	})
	return flow, harness
}

func (harness *summaryIntegrationHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
}

func (harness *summaryIntegrationHarness) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	harness.worker = nil
	harness.workerResult = nil
}

func (harness *summaryIntegrationHarness) runFlow(t *testing.T, ctx context.Context, flow *Flow, flowID string, input SummaryRequest) dex.FlowResult {
	t.Helper()
	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	return result
}

func availableIntegrationPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
