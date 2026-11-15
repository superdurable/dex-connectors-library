//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package integrationtest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex/sdk-go/dex"
)

type pollItem struct {
	ItemID   string `json:"itemId"`
	Sequence int    `json:"sequence"`
}

// pollItemFlow is started once per polled change and completes with its input.
type pollItemFlow struct {
	dex.FlowDefaults
}

func (*pollItemFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(pollItemStartStep{})}
}

func (*pollItemFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

type pollItemStartStep struct {
	dex.StepDefaultsNoWaitFor[pollItem]
}

func (pollItemStartStep) Execute(_ dex.Context, input pollItem) (*dex.StepDecision, error) {
	return dex.GracefulComplete(input), nil
}

// changeLogProvider is an in-process provider with sequence-numbered changes and scripted failures.
type changeLogProvider struct {
	server    *httptest.Server
	mutex     sync.Mutex
	changes   []pollItem
	responses []changeLogResponse
	requests  []changeLogRequest
}

// changeLogResponse is one scripted failure: a status and an optional Retry-After header.
type changeLogResponse struct {
	status     int
	retryAfter string
}

type changeLogRequest struct {
	path  string
	after string
	at    time.Time
}

func newChangeLogProvider(t *testing.T) *changeLogProvider {
	t.Helper()
	provider := &changeLogProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.serve))
	t.Cleanup(provider.server.Close)
	return provider
}

func (provider *changeLogProvider) serve(writer http.ResponseWriter, request *http.Request) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.requests = append(provider.requests, changeLogRequest{path: request.URL.Path, after: request.URL.Query().Get("after"), at: time.Now()})
	if len(provider.responses) > 0 && request.URL.Path == "/changes" {
		scripted := provider.responses[0]
		provider.responses = provider.responses[1:]
		if scripted.retryAfter != "" {
			writer.Header().Set("Retry-After", scripted.retryAfter)
		}
		writer.WriteHeader(scripted.status)
		return
	}
	switch request.URL.Path {
	case "/latest":
		writeJSON(writer, map[string]int{"latest": len(provider.changes)})
	case "/changes":
		after, err := strconv.Atoi(request.URL.Query().Get("after"))
		limit, limitErr := strconv.Atoi(request.URL.Query().Get("limit"))
		if err != nil || limitErr != nil || after > len(provider.changes) {
			writer.WriteHeader(http.StatusGone)
			return
		}
		end := min(after+limit, len(provider.changes))
		writeJSON(writer, map[string]any{"changes": provider.changes[after:end], "hasMore": end < len(provider.changes)})
	default:
		writer.WriteHeader(http.StatusNotFound)
	}
}

func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	// The test reads every response; an encoding failure shows up as a decode error there.
	_ = json.NewEncoder(writer).Encode(value)
}

// appendItems adds changes for new item IDs and returns them.
func (provider *changeLogProvider) appendItems(prefix string, count int) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	itemIDs := []string{}
	for index := 0; index < count; index++ {
		itemID := fmt.Sprintf("%s-%d", prefix, index)
		provider.changes = append(provider.changes, pollItem{ItemID: itemID, Sequence: len(provider.changes) + 1})
		itemIDs = append(itemIDs, itemID)
	}
	return itemIDs
}

func (provider *changeLogProvider) script(responses ...changeLogResponse) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.responses = append(provider.responses, responses...)
}

func (provider *changeLogProvider) changeRequests() []changeLogRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	requests := []changeLogRequest{}
	for _, request := range provider.requests {
		if request.path == "/changes" {
			requests = append(requests, request)
		}
	}
	return requests
}

// truncate drops every change, so any stored cursor past zero is answered with 410 Gone.
func (provider *changeLogProvider) truncate() {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.changes = nil
}

// changeLogFetcher is the connector-side fetcher for the change-log provider.
type changeLogFetcher struct {
	baseURL  string
	pageSize int
}

func (fetcher changeLogFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	var latest struct {
		Latest int `json:"latest"`
	}
	if err := fetcher.get(ctx, "/latest", &latest); err != nil {
		return pollingtrigger.Cursor{}, err
	}
	return pollingtrigger.Cursor{Token: strconv.Itoa(latest.Latest)}, nil
}

func (fetcher changeLogFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[pollItem]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[pollItem]]
	var listed struct {
		Changes []pollItem `json:"changes"`
		HasMore bool       `json:"hasMore"`
	}
	if err := fetcher.get(ctx, "/changes?after="+cursor.Token+"&limit="+strconv.Itoa(fetcher.pageSize), &listed); err != nil {
		return page, err
	}
	next := cursor.Token
	for _, change := range listed.Changes {
		page.Changes = append(page.Changes, sdkgo.TriggerEvent[pollItem]{ID: change.ItemID + "@" + strconv.Itoa(change.Sequence), OccurredAt: time.Now().UTC(), Payload: change})
		next = strconv.Itoa(change.Sequence)
	}
	page.Next, page.HasMorePages = pollingtrigger.Cursor{Token: next}, listed.HasMore
	return page, nil
}

func (fetcher changeLogFetcher) get(ctx context.Context, path string, destination any) error {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fetcher.baseURL+path, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusGone:
		return pollingtrigger.MarkCursorInvalid(fmt.Errorf("change log cursor expired: %s", response.Status))
	case response.StatusCode == http.StatusTooManyRequests:
		return pollingtrigger.MarkRateLimited(fmt.Errorf("change log rate limited: %s", response.Status),
			providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode != http.StatusOK:
		return fmt.Errorf("change log request failed: %s", response.Status)
	}
	return json.NewDecoder(response.Body).Decode(destination)
}

func waitForPollItemFlow(ctx context.Context, t *testing.T, client *dex.Client, flowID string) pollItem {
	t.Helper()
	// The Trigger starts the Flow asynchronously, so wait until Dex knows it before waiting for its result.
	require.Eventually(t, func() bool {
		_, err := client.DescribeFlow(ctx, flowID)
		return err == nil
	}, 60*time.Second, 50*time.Millisecond, flowID)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, flowID)
	var output pollItem
	require.NoError(t, result.DecodeSingleOutput(&output))
	return output
}
