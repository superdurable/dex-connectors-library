//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package companynews

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/newsapi"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationAPIKey = "5e471ae15e471ae15e471ae15e471ae1"

func TestCompanyNewsExampleRoutesNewsAPIOutcomesWithRealDex(t *testing.T) {
	provider := newNewsProvider(t)
	flow, harness := newCompanyNewsIntegrationHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)

	t.Run("found articles complete the Flow", func(t *testing.T) {
		company := "Found " + testRunID
		flowID := "newsapi-company-news-found-" + testRunID
		result := harness.runFlow(t, ctx, flow, flowID, CompanyNewsRequest{Company: company, Days: 3, MaxArticles: 5})
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var outcome CompanyNewsOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, StatusFound, outcome.Status)
		require.Equal(t, company, outcome.Company)
		require.Equal(t, time.Now().UTC().AddDate(0, 0, -3).Format(time.DateOnly), outcome.From)
		require.Equal(t, 2, outcome.TotalResults)
		require.Len(t, outcome.Articles, 2)
		require.Equal(t, company+" opens a lab", outcome.Articles[0].Title)
		require.Nil(t, outcome.Articles[1].Description, "a null description survives the Dex round trip")

		requests := provider.requestsFor(company)
		require.Len(t, requests, 1)
		require.Equal(t, integrationAPIKey, requests[0].apiKey)
		require.Equal(t, url.Values{
			"q": {company}, "searchIn": {"title"}, "from": {outcome.From}, "language": {"en"},
			"sortBy": {"publishedAt"}, "pageSize": {"5"},
		}, requests[0].query)

		var display map[string]any
		require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.GetDexDisplay, nil, &display))
		encodedDisplay, err := json.Marshal(display)
		require.NoError(t, err)
		require.Contains(t, string(encodedDisplay), company+" opens a lab")
		require.NotContains(t, string(encodedDisplay), integrationAPIKey)
	})

	t.Run("a transient outage retries in a later attempt", func(t *testing.T) {
		company := "Flaky " + testRunID
		result := harness.runFlow(t, ctx, flow, "newsapi-company-news-flaky-"+testRunID, CompanyNewsRequest{Company: company})
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var outcome CompanyNewsOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, StatusFound, outcome.Status)
		require.Len(t, provider.requestsFor(company), 2, "the first attempt saw HTTP 503")
	})

	t.Run("a wired rejection completes without articles and without a retry", func(t *testing.T) {
		company := "Exhausted " + testRunID
		result := harness.runFlow(t, ctx, flow, "newsapi-company-news-exhausted-"+testRunID, CompanyNewsRequest{Company: company})
		require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
		var outcome CompanyNewsOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, StatusRejected, outcome.Status)
		require.Equal(t, string(sdkgo.FailureQuotaExhausted), outcome.FailureKind)
		require.NotContains(t, outcome.FailureMessage, "SENTINEL")
		require.Empty(t, outcome.Articles)
		require.Len(t, provider.requestsFor(company), 1)
	})

	t.Run("an unwired invalidResponse fails the Flow without a retry", func(t *testing.T) {
		company := "Garbled " + testRunID
		result := harness.runFlow(t, ctx, flow, "newsapi-company-news-garbled-"+testRunID, CompanyNewsRequest{Company: company})
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Len(t, provider.requestsFor(company), 1)
	})

	t.Run("invalid start input fails before calling NewsAPI", func(t *testing.T) {
		before := provider.requestCount()
		result := harness.runFlow(t, ctx, flow, "newsapi-company-news-invalid-"+testRunID, CompanyNewsRequest{Company: "Acme", Days: 31})
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Equal(t, before, provider.requestCount())
	})
}

type newsProviderRequest struct {
	apiKey string
	query  url.Values
}

// newsProvider is a local NewsAPI. The query's first word selects the outcome.
type newsProvider struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []newsProviderRequest
}

func newNewsProvider(t *testing.T) *newsProvider {
	t.Helper()
	provider := &newsProvider{}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *newsProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	company := request.URL.Query().Get("q")
	provider.mutex.Lock()
	provider.requests = append(provider.requests, newsProviderRequest{apiKey: request.Header.Get("X-Api-Key"), query: request.URL.Query()})
	attempts := 0
	for _, previous := range provider.requests {
		if previous.query.Get("q") == company {
			attempts++
		}
	}
	provider.mutex.Unlock()
	if request.URL.Path != "/v2/everything" || request.Header.Get("X-Api-Key") != integrationAPIKey {
		writeProviderResponse(response, http.StatusUnauthorized, `{"status":"error","code":"apiKeyInvalid","message":"SENTINEL"}`)
		return
	}
	switch {
	case strings.HasPrefix(company, "Found "), strings.HasPrefix(company, "Flaky ") && attempts > 1:
		writeProviderResponse(response, http.StatusOK, fmt.Sprintf(`{"status":"ok","totalResults":2,"articles":[
			{"source":{"id":null,"name":"Example Wire"},"author":"Reporter","title":%q,"description":"Summary.",
			 "url":"https://news.example/a","urlToImage":null,"publishedAt":"2026-10-04T12:30:00Z","content":"Body"},
			{"source":{"id":"bbc-news","name":"BBC News"},"author":null,"title":"Second story","description":null,
			 "url":"https://news.example/b","urlToImage":null,"publishedAt":"2026-10-03T08:00:00Z","content":null}]}`, company+" opens a lab"))
	case strings.HasPrefix(company, "Flaky "):
		writeProviderResponse(response, http.StatusServiceUnavailable, `{"status":"error","code":"unexpectedError","message":"SENTINEL"}`)
	case strings.HasPrefix(company, "Exhausted "):
		writeProviderResponse(response, http.StatusTooManyRequests, `{"status":"error","code":"apiKeyExhausted","message":"SENTINEL"}`)
	case strings.HasPrefix(company, "Garbled "):
		writeProviderResponse(response, http.StatusOK, `{`)
	default:
		writeProviderResponse(response, http.StatusInternalServerError, `{"status":"error","code":"unexpectedError"}`)
	}
}

func writeProviderResponse(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		panic(err)
	}
}

func (provider *newsProvider) requestsFor(company string) []newsProviderRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var matching []newsProviderRequest
	for _, request := range provider.requests {
		if request.query.Get("q") == company {
			matching = append(matching, request)
		}
	}
	return matching
}

func (provider *newsProvider) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

type companyNewsIntegrationHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newCompanyNewsIntegrationHarness(t *testing.T, providerURL string) (*Flow, *companyNewsIntegrationHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "newsapi", Name: ConnectionName}
	providerClient, err := newsapi.New(newsapi.Config{BaseURL: providerURL + "/v2"}, sdkgo.StaticCredentialProvider[newsapi.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(integrationAPIKey)},
	})
	require.NoError(t, err)
	connection, err := newsapi.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &companyNewsIntegrationHarness{
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

func (harness *companyNewsIntegrationHarness) startWorker(t *testing.T) {
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

func (harness *companyNewsIntegrationHarness) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	harness.worker = nil
	harness.workerResult = nil
}

func (harness *companyNewsIntegrationHarness) runFlow(t *testing.T, ctx context.Context, flow *Flow, flowID string, input CompanyNewsRequest) dex.FlowResult {
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
