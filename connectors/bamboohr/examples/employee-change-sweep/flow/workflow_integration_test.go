//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package employeechangesweep

import (
	"context"
	"encoding/base64"
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
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// integrationAPIKey is split so secret scanners do not mistake the 40-hex fixture for a real key.
	integrationAPIKey = "0123456789abcdef" + "0123456789abcdef01234567"
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowResponseDelay = 9 * time.Second
)

var sweepStart = time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)

// seedOnboardingChanges places two inserts at one instant so a page boundary falls between them.
func seedOnboardingChanges(provider *fakeChangeHistory) {
	provider.seed("101", "Inserted", sweepStart.Add(time.Hour))
	provider.seed("102", "Inserted", sweepStart.Add(2*time.Hour))
	provider.seed("103", "Inserted", sweepStart.Add(2*time.Hour))
	provider.seed("104", "Inserted", sweepStart.Add(3*time.Hour))
	provider.seed("105", "Inserted", sweepStart.Add(4*time.Hour))
	provider.seed("90", "Updated", sweepStart.Add(90*time.Minute))
	provider.seed("80", "Deleted", sweepStart.Add(150*time.Minute))
	provider.seed("70", "Inserted", sweepStart.Add(-time.Hour))
}

func TestSweepReadsEveryPageOfNewHiresWithRealDex(t *testing.T) {
	provider := newFakeChangeHistory(t)
	seedOnboardingChanges(provider)
	harness := newSweepHarness(t, provider)

	sweep := harness.runSweep(t, "every-page", Input{Since: sweepStart.Format(time.RFC3339), ChangeType: bamboohr.EmployeeChangeTypeInserted, PageSize: 2})
	require.Equal(t, []string{"101", "102", "103", "104", "105"}, changedEmployeeIDs(sweep.Changes), "employee 70 changed before the sweep window")
	require.Equal(t, 3, sweep.PagesRead)
	require.True(t, sweep.IsCaughtUp)
	require.Equal(t, bamboohr.EmployeeChangeCursor{Since: sweepStart.Add(4 * time.Hour), AfterEmployeeID: "105"}, sweep.NextCursor)
	for _, query := range provider.queries() {
		require.Equal(t, "inserted", query.Get("type"))
	}
}

func TestSweepStoppedByMaxPagesContinuesWithoutGapsOrDuplicatesWithRealDex(t *testing.T) {
	provider := newFakeChangeHistory(t)
	seedOnboardingChanges(provider)
	harness := newSweepHarness(t, provider)

	first := harness.runSweep(t, "first", Input{Since: sweepStart.Format(time.RFC3339), PageSize: 2, MaxPages: 1})
	require.Equal(t, []string{"101", "90"}, changedEmployeeIDs(first.Changes))
	require.False(t, first.IsCaughtUp, "maxPages stopped the sweep")

	second := harness.runSweep(t, "second", Input{
		Since: first.NextCursor.Since.Format(time.RFC3339), AfterEmployeeID: first.NextCursor.AfterEmployeeID, PageSize: 3, MaxPages: 1,
	})
	require.Equal(t, []string{"102", "103", "80"}, changedEmployeeIDs(second.Changes))
	require.Equal(t, bamboohr.EmployeeChangeCursor{Since: sweepStart.Add(150 * time.Minute), AfterEmployeeID: "80"}, second.NextCursor)

	third := harness.runSweep(t, "third", Input{
		Since: second.NextCursor.Since.Format(time.RFC3339), AfterEmployeeID: second.NextCursor.AfterEmployeeID, PageSize: 1, MaxPages: 1,
	})
	require.Equal(t, []string{"104"}, changedEmployeeIDs(third.Changes))

	fourth := harness.runSweep(t, "fourth", Input{
		Since: third.NextCursor.Since.Format(time.RFC3339), AfterEmployeeID: third.NextCursor.AfterEmployeeID,
	})
	require.Equal(t, []string{"105"}, changedEmployeeIDs(fourth.Changes))
	require.True(t, fourth.IsCaughtUp)
	since := provider.queries()[1].Get("since")
	require.Equal(t, first.NextCursor.Since.Add(-time.Second).Format(time.RFC3339), since,
		"the connector asks one second early, because this fake treats since as exclusive")
}

func TestSlowChangeListIsSafeToRepeatWithRealDex(t *testing.T) {
	provider := newFakeChangeHistory(t)
	seedOnboardingChanges(provider)
	provider.delaysFirstList = true
	harness := newSweepHarness(t, provider)

	sweep := harness.runSweep(t, "slow", Input{Since: sweepStart.Format(time.RFC3339), ChangeType: bamboohr.EmployeeChangeTypeInserted})
	require.Equal(t, []string{"101", "102", "103", "104", "105"}, changedEmployeeIDs(sweep.Changes), "a repeated read keeps each change once")
	require.Equal(t, 1, sweep.PagesRead)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.requestCount(), 2, "Dex dispatched the read again past its local phase")
}

func TestRateLimitedChangeListWaitsAndCompletesWithRealDex(t *testing.T) {
	provider := newFakeChangeHistory(t)
	seedOnboardingChanges(provider)
	provider.rateLimitsFirstList = true
	harness := newSweepHarness(t, provider)

	sweep := harness.runSweep(t, "rate-limited", Input{Since: sweepStart.Format(time.RFC3339), ChangeType: bamboohr.EmployeeChangeTypeDeleted})
	require.Equal(t, []string{"80"}, changedEmployeeIDs(sweep.Changes))
	require.Equal(t, 2, provider.requestCount())
}

func TestRejectedCredentialsFailTheSweepWithRealDex(t *testing.T) {
	provider := newFakeChangeHistory(t)
	provider.rejectsCredentials = true
	harness := newSweepHarness(t, provider)
	flowID := harness.startSweep(t, "rejected", Input{Since: sweepStart.Format(time.RFC3339)})

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.NotContains(t, result.ErrorMessage, integrationAPIKey)
	require.Equal(t, 1, provider.requestCount())
}

func changedEmployeeIDs(changes []bamboohr.EmployeeChange) []string {
	employeeIDs := []string{}
	for _, change := range changes {
		employeeIDs = append(employeeIDs, change.EmployeeID)
	}
	return employeeIDs
}

// fakeChangeHistory serves BambooHR's Get Changed Employee IDs and treats since as exclusive.
type fakeChangeHistory struct {
	*httptest.Server
	t               *testing.T
	mutex           sync.Mutex
	delayedRequests sync.WaitGroup
	changes         map[string]fakeChange
	requests        []url.Values

	delaysFirstList     bool
	rateLimitsFirstList bool
	rejectsCredentials  bool
}

type fakeChange struct {
	action      string
	lastChanged time.Time
}

func newFakeChangeHistory(t *testing.T) *fakeChangeHistory {
	t.Helper()
	provider := &fakeChangeHistory{t: t, changes: map[string]fakeChange{}}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeChangeHistory) seed(employeeID string, action string, lastChanged time.Time) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.changes[employeeID] = fakeChange{action: action, lastChanged: lastChanged}
}

func (provider *fakeChangeHistory) serveHTTP(response http.ResponseWriter, request *http.Request) {
	provider.mutex.Lock()
	provider.requests = append(provider.requests, request.URL.Query())
	attempt := len(provider.requests)
	provider.mutex.Unlock()
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(integrationAPIKey+":x"))
	switch {
	case provider.rejectsCredentials || request.Header.Get("Authorization") != expected:
		response.WriteHeader(http.StatusUnauthorized)
		return
	case request.Method != http.MethodGet || request.URL.Path != "/api/v1/employees/changed":
		response.WriteHeader(http.StatusNotFound)
		return
	case provider.rateLimitsFirstList && attempt == 1:
		response.Header().Set("Retry-After", "1")
		response.WriteHeader(http.StatusTooManyRequests)
		return
	case provider.delaysFirstList && attempt == 1:
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	since, err := time.Parse(time.RFC3339, request.URL.Query().Get("since"))
	if err != nil {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	changeType := request.URL.Query().Get("type")
	employees := map[string]any{}
	latest := ""
	provider.mutex.Lock()
	for employeeID, change := range provider.changes {
		if !change.lastChanged.After(since) || (changeType != "" && !strings.EqualFold(change.action, changeType)) {
			continue
		}
		stamp := change.lastChanged.Format("2006-01-02T15:04:05+00:00")
		employees[employeeID] = map[string]string{"id": employeeID, "action": change.action, "lastChanged": stamp}
		if stamp > latest {
			latest = stamp
		}
	}
	provider.mutex.Unlock()
	body := map[string]any{"latest": latest, "employees": employees}
	if len(employees) == 0 {
		body["employees"] = []any{}
	}
	encoded, err := json.Marshal(body)
	require.NoError(provider.t, err)
	response.Header().Set("Content-Type", "application/json")
	_, err = response.Write(encoded)
	require.NoError(provider.t, err)
}

func (provider *fakeChangeHistory) queries() []url.Values {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]url.Values(nil), provider.requests...)
}

func (provider *fakeChangeHistory) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *fakeChangeHistory) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatal("a delayed BambooHR request did not finish")
	}
}

type sweepHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newSweepHarness(t *testing.T, provider *fakeChangeHistory) *sweepHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "bamboohr", Name: ConnectionName}
	providerClient, err := bamboohr.New(bamboohr.Config{CompanyDomain: "acme"},
		sdkgo.StaticCredentialProvider[bamboohr.Credentials]{reference: {APIKey: sdkgo.NewSecretString(integrationAPIKey)}},
		bamboohr.WithAPIBaseURL(provider.URL+"/api/v1"), bamboohr.WithHTTPClient(&http.Client{Timeout: 20 * time.Second}),
	)
	require.NoError(t, err)
	connection, err := bamboohr.NewConnection(providerClient, reference)
	require.NoError(t, err)
	harness := &sweepHarness{flow: NewFlow(connection), serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
	harness.registry, err = dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.workerAddress = net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress, WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.worker, harness.workerResult = worker, make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return harness
}

func (harness *sweepHarness) runSweep(t *testing.T, scenario string, input Input) EmployeeChangeSweep {
	t.Helper()
	flowID := harness.startSweep(t, scenario, input)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var sweep EmployeeChangeSweep
	require.NoError(t, result.DecodeSingleOutput(&sweep))
	return sweep
}

func (harness *sweepHarness) startSweep(t *testing.T, scenario string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := fmt.Sprintf("bamboohr-sweep-%s-%d", scenario, time.Now().UnixNano())
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *sweepHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for {
		result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err, "Flow %s did not close", flowID)
		return result
	}
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
