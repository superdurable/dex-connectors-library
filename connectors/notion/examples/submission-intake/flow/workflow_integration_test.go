//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package submissionintake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationToken    = "ntn_integration_token"
	shortRequestTimeout = 500 * time.Millisecond
	// slowRequestTimeout outlasts the fake's nine-second responses and Dex's roughly seven-second async local phase.
	slowRequestTimeout = 20 * time.Second
	slowProviderDelay  = 9 * time.Second

	submissionsTitle      = "Form submissions"
	submissionsSourceID   = "2a4d6f80-1b3c-4d5e-8f70-112233445566"
	archivedSourceID      = "2a4d6f80-1b3c-4d5e-8f70-000000000001"
	decoySourceID         = "2a4d6f80-1b3c-4d5e-8f70-000000000002"
	opsLogTitle           = "Ops log"
	opsLogFirstSourceID   = "2a4d6f80-1b3c-4d5e-8f70-000000000003"
	opsLogSecondSourceID  = "2a4d6f80-1b3c-4d5e-8f70-000000000004"
	brokenTitle           = "Broken submissions"
	brokenSourceID        = "2a4d6f80-1b3c-4d5e-8f70-000000000005"
	existingSubmissionID  = "sub-existing"
	duplicateSubmissionID = "sub-duplicate"

	markerReject       = "[reject]"
	markerRateLimit    = "[rate-limit]"
	markerTimeoutSaved = "[timeout-saved]"
	markerTimeoutLost  = "[timeout-lost]"
	markerSaved503     = "[saved-503]"
	markerSlowCreate   = "[slow-create]"
	markerSlowUpdate   = "[slow-update]"
)

func TestNewSubmissionIsCreatedOnceAndReadBackWithRealDex(t *testing.T) {
	provider, flow, harness := newNotionIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	record := runSubmissionFlow(t, ctx, harness, flow, "created", submissionInput("sub-new-1"))
	require.Equal(t, PhaseCreated, record.Phase)
	require.False(t, record.IsExistingRow)
	require.Equal(t, submissionsSourceID, record.DataSourceID, "the archived twin and the (2025) decoy are not the submissions database")
	require.NotEmpty(t, record.PageID)
	require.Equal(t, map[string]string{
		"Name": "Ada Lovelace", SubmissionIDProperty: "sub-new-1", EmailProperty: "ada@example.com", SubmittedAtProperty: record.SubmittedAt,
	}, record.StoredProperties)
	require.Equal(t, "Hello from the form.\nSecond paragraph.", record.StoredBodyText)
	require.Equal(t, 1, provider.createCount("sub-new-1"))
	require.Equal(t, 1, provider.rowCount("sub-new-1"))
	require.Equal(t, []string{submissionsTitle}, provider.searchQueries())
}

func TestResubmissionUpdatesTheExistingRowInsteadOfCreatingWithRealDex(t *testing.T) {
	provider, flow, harness := newNotionIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)
	existingPageID := provider.seedRow(submissionsSourceID, existingSubmissionID, "Old Name", "old@example.com")

	input := submissionInput(existingSubmissionID)
	input.Email = "new@example.com"
	record := runSubmissionFlow(t, ctx, harness, flow, "updated", input)
	require.Equal(t, PhaseUpdated, record.Phase)
	require.True(t, record.IsExistingRow)
	require.Equal(t, existingPageID, record.PageID)
	require.Equal(t, "new@example.com", record.StoredProperties[EmailProperty])
	require.Equal(t, "Ada Lovelace", record.StoredProperties["Name"])
	require.Zero(t, provider.createCount(existingSubmissionID))
	require.Equal(t, 1, provider.rowCount(existingSubmissionID))
	require.Len(t, provider.patchBodies(existingPageID), 1)
}

func TestDuplicateRowsCompleteWithoutWritingWithRealDex(t *testing.T) {
	provider, flow, harness := newNotionIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)
	first := provider.seedRow(submissionsSourceID, duplicateSubmissionID, "Ada", "ada@example.com")
	second := provider.seedRow(submissionsSourceID, duplicateSubmissionID, "Ada", "ada@example.com")

	record := runSubmissionFlow(t, ctx, harness, flow, "duplicates", submissionInput(duplicateSubmissionID))
	require.Equal(t, PhaseDuplicateRows, record.Phase)
	require.Equal(t, []string{first, second}, record.DuplicatePageIDs)
	require.Zero(t, provider.createCount(duplicateSubmissionID))
	require.Empty(t, provider.patchBodies(first))
}

func TestMissingAndAmbiguousDatabasesCompleteWithoutWritingWithRealDex(t *testing.T) {
	provider, flow, harness := newNotionIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	missingInput := submissionInput("sub-missing")
	missingInput.DatabaseTitle = "Acount Hierarchy"
	missing := runSubmissionFlow(t, ctx, harness, flow, "missing-database", missingInput)
	require.Equal(t, PhaseDataSourceNotFound, missing.Phase, "search's noMatch branch is routed")

	ambiguousInput := submissionInput("sub-ambiguous")
	ambiguousInput.DatabaseTitle = opsLogTitle
	ambiguous := runSubmissionFlow(t, ctx, harness, flow, "ambiguous-database", ambiguousInput)
	require.Equal(t, PhaseDataSourceAmbiguous, ambiguous.Phase)
	require.ElementsMatch(t, []string{opsLogFirstSourceID, opsLogSecondSourceID}, ambiguous.CandidateDataSourceIDs)
	require.Zero(t, provider.totalCreates())
}

func TestRejectedCreateCompletesWithNotionsCodeWithRealDex(t *testing.T) {
	provider, flow, harness := newNotionIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	submissionID := markerReject + " sub-1"
	record := runSubmissionFlow(t, ctx, harness, flow, "rejected", submissionInput(submissionID))
	require.Equal(t, PhaseRejected, record.Phase)
	require.NotNil(t, record.Rejection)
	require.Equal(t, sdkgo.FailureValidation, record.Rejection.Kind)
	require.Contains(t, record.Rejection.Message, "validation_error")
	require.NotContains(t, record.Rejection.Message, "SENTINEL")
	require.Equal(t, 1, provider.createCount(submissionID))
	require.Zero(t, provider.rowCount(submissionID))
}

func TestRateLimitedCreateIsRetriedAfterRetryAfterWithRealDex(t *testing.T) {
	provider, flow, harness := newNotionIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	submissionID := markerRateLimit + " sub-1"
	record := runSubmissionFlow(t, ctx, harness, flow, "rate-limited", submissionInput(submissionID))
	require.Equal(t, PhaseCreated, record.Phase)
	attempts := provider.createTimes(submissionID)
	require.Len(t, attempts, 2, "a 429 wrote nothing, so Dex retries the same Step execution")
	require.GreaterOrEqual(t, attempts[1].Sub(attempts[0]), time.Second, "the retry waits for Retry-After")
	require.Equal(t, 1, provider.rowCount(submissionID))
}

func TestUncertainCreateIsReconciledByQueryWithoutCreatingAgainWithRealDex(t *testing.T) {
	provider, flow, harness := newNotionIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	submissionID := markerTimeoutSaved + " sub-1"
	record := runSubmissionFlow(t, ctx, harness, flow, "uncertain-saved", submissionInput(submissionID))
	require.Equal(t, PhaseCreated, record.Phase, "the reconciliation query adopted the saved row")
	require.NotNil(t, record.UncertainCreate)
	require.Equal(t, sdkgo.FailureTransport, record.UncertainCreate.Kind)
	require.Equal(t, 1, provider.createCount(submissionID), "a timeout after dispatch is never retried")
	require.Equal(t, 1, provider.rowCount(submissionID))
	require.Equal(t, submissionID, record.StoredProperties[SubmissionIDProperty])
}

func TestUncertainCreateWithNoVisibleRowCompletesAsUncertainWithRealDex(t *testing.T) {
	provider, flow, harness := newNotionIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	submissionID := markerTimeoutLost + " sub-1"
	record := runSubmissionFlow(t, ctx, harness, flow, "uncertain-lost", submissionInput(submissionID))
	require.Equal(t, PhaseCreateUncertain, record.Phase)
	require.NotNil(t, record.UncertainCreate)
	require.Empty(t, record.PageID)
	require.Equal(t, 1, provider.createCount(submissionID))
	require.Zero(t, provider.rowCount(submissionID))
}

func TestCreateSavedBeforeA503IsConfirmedWithRealDex(t *testing.T) {
	provider, flow, harness := newNotionIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	submissionID := markerSaved503 + " sub-1"
	record := runSubmissionFlow(t, ctx, harness, flow, "saved-503", submissionInput(submissionID))
	require.Equal(t, PhaseCreated, record.Phase)
	require.Nil(t, record.UncertainCreate, "Notion named the saved page, so the create was never uncertain")
	require.Equal(t, 1, provider.createCount(submissionID))
	require.Equal(t, 1, provider.rowCount(submissionID))
}

// TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex guards sync durability: under async, Dex
// dispatches a Step again when its local attempt passes about seven seconds, and Notion has no idempotency key.
func TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex(t *testing.T) {
	provider, flow, harness := newNotionIntegrationHarness(t, slowRequestTimeout)
	ctx := integrationContext(t)

	submissionID := markerSlowCreate + " sub-1"
	record := runSubmissionFlow(t, ctx, harness, flow, "slow-create", submissionInput(submissionID))
	require.Equal(t, PhaseCreated, record.Phase)
	require.Equal(t, 1, provider.createCount(submissionID), "one Step execution dispatches one create request")
	require.Equal(t, 1, provider.rowCount(submissionID))
}

// TestSlowUpdateConvergesUnderAsyncDurabilityWithRealDex proves updatePageProperties is safe under
// async durability: a duplicate dispatch sends the same absolute values, so the row ends in one state.
func TestSlowUpdateConvergesUnderAsyncDurabilityWithRealDex(t *testing.T) {
	provider, flow, harness := newNotionIntegrationHarness(t, slowRequestTimeout)
	ctx := integrationContext(t)
	submissionID := markerSlowUpdate + " sub-1"
	pageID := provider.seedRow(submissionsSourceID, submissionID, "Old Name", "old@example.com")

	input := submissionInput(submissionID)
	input.Email = "new@example.com"
	record := runSubmissionFlow(t, ctx, harness, flow, "slow-update", input)
	require.Equal(t, PhaseUpdated, record.Phase)
	require.Equal(t, "new@example.com", record.StoredProperties[EmailProperty])
	bodies := provider.patchBodies(pageID)
	require.NotEmpty(t, bodies)
	for _, body := range bodies {
		require.JSONEq(t, bodies[0], body, "every dispatch sends the same absolute values")
	}
	require.Zero(t, provider.createCount(submissionID))
	require.Equal(t, 1, provider.rowCount(submissionID))
	t.Logf("slow update: Notion saw %d PATCH requests for one Step execution", len(bodies))
}

func TestUnwiredOptionalBranchFailsTheFlowWithRealDex(t *testing.T) {
	provider, flow, harness := newNotionIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	input := submissionInput("sub-broken")
	input.DatabaseTitle = brokenTitle
	flowID := startSubmissionFlow(t, ctx, harness, flow, "unwired", input)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, result.Status, "queryDatabase providerRejected is optional and not wired")
	require.Zero(t, provider.totalCreates())
}

func TestInvalidStartInputFailsBeforeAnyNotionCallWithRealDex(t *testing.T) {
	provider, flow, harness := newNotionIntegrationHarness(t, shortRequestTimeout)
	ctx := integrationContext(t)

	input := submissionInput("sub-invalid")
	input.Email = "not an email"
	flowID := startSubmissionFlow(t, ctx, harness, flow, "invalid", input)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.requestCount())
}

func submissionInput(submissionID string) Input {
	return Input{
		DatabaseTitle: submissionsTitle, SubmissionID: submissionID, Name: "Ada Lovelace", Email: "ada@example.com",
		Message: "Hello from the form.\n\nSecond paragraph.",
	}
}

func runSubmissionFlow(t *testing.T, ctx context.Context, harness *notionIntegrationHarness, flow *Flow, scenario string, input Input) SubmissionRecord {
	t.Helper()
	flowID := startSubmissionFlow(t, ctx, harness, flow, scenario, input)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s", flowID)
	var record SubmissionRecord
	require.NoError(t, result.DecodeSingleOutput(&record))
	return record
}

func startSubmissionFlow(t *testing.T, ctx context.Context, harness *notionIntegrationHarness, flow *Flow, scenario string, input Input) string {
	t.Helper()
	flowID := "notion-submission-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

type notionIntegrationHarness struct {
	registry     *dex.Registry
	cache        *blobcache.Cache
	client       *dex.Client
	worker       *dex.Worker
	workerResult chan error
}

func newNotionIntegrationHarness(t *testing.T, requestTimeout time.Duration) (*fakeNotion, *Flow, *notionIntegrationHarness) {
	t.Helper()
	provider := newFakeNotion(t)
	reference := sdkgo.ConnectionRef{Provider: "notion", Name: ConnectionName}
	providerClient, err := notion.New(
		notion.Config{Endpoint: provider.URL},
		sdkgo.StaticCredentialProvider[notion.Credentials]{reference: {APIToken: sdkgo.NewSecretString(integrationToken)}},
		notion.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := notion.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	harness := &notionIntegrationHarness{registry: registry, cache: cache}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- harness.worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(stopCtx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return provider, flow, harness
}

func availableIntegrationPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { require.NoError(t, listener.Close()) }()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// fakeNotion is a stateful Notion API; markers in a submission ID select its create or update behavior.
type fakeNotion struct {
	*httptest.Server
	t           *testing.T
	mutex       sync.Mutex
	dataSources []fakeDataSource
	rows        []*fakeRow
	creates     []fakeWrite
	patches     []fakeWrite
	queries     []string
	requests    int
	nextRow     int
	rateLimited map[string]bool
}

type fakeDataSource struct {
	id        string
	title     string
	isInTrash bool
	isBroken  bool
}

type fakeRow struct {
	id           string
	dataSourceID string
	title        string
	submissionID string
	email        string
	submittedAt  string
	paragraphs   []string
}

type fakeWrite struct {
	key  string
	body string
	at   time.Time
}

func newFakeNotion(t *testing.T) *fakeNotion {
	provider := &fakeNotion{t: t, rateLimited: map[string]bool{}, dataSources: []fakeDataSource{
		{id: submissionsSourceID, title: submissionsTitle},
		{id: archivedSourceID, title: submissionsTitle, isInTrash: true},
		{id: decoySourceID, title: submissionsTitle + " (2025)"},
		{id: opsLogFirstSourceID, title: opsLogTitle},
		{id: opsLogSecondSourceID, title: opsLogTitle},
		{id: brokenSourceID, title: brokenTitle, isBroken: true},
	}}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeNotion) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+integrationToken || request.Header.Get("Notion-Version") != notion.NotionVersion {
		provider.writeJSON(response, http.StatusUnauthorized, `{"object":"error","status":401,"code":"unauthorized","message":"SENTINEL"}`)
		return
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		provider.writeJSON(response, http.StatusBadRequest, `{"object":"error","status":400,"code":"invalid_json","message":"SENTINEL"}`)
		return
	}
	provider.mutex.Lock()
	provider.requests++
	provider.mutex.Unlock()
	path := strings.TrimPrefix(request.URL.Path, "/v1")
	switch {
	case request.Method == http.MethodPost && path == "/search":
		provider.search(response, body)
	case request.Method == http.MethodPost && strings.HasPrefix(path, "/data_sources/") && strings.HasSuffix(path, "/query"):
		provider.query(response, strings.TrimSuffix(strings.TrimPrefix(path, "/data_sources/"), "/query"), body)
	case request.Method == http.MethodPost && path == "/pages":
		provider.createPage(response, body)
	case request.Method == http.MethodPatch && strings.HasPrefix(path, "/pages/"):
		provider.updatePage(response, strings.TrimPrefix(path, "/pages/"), body)
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/pages/"):
		provider.getPage(response, strings.TrimPrefix(path, "/pages/"))
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/blocks/") && strings.HasSuffix(path, "/children"):
		provider.getBlocks(response, strings.TrimSuffix(strings.TrimPrefix(path, "/blocks/"), "/children"))
	default:
		provider.writeJSON(response, http.StatusBadRequest, `{"object":"error","status":400,"code":"invalid_request_url","message":"SENTINEL"}`)
	}
}

func (provider *fakeNotion) search(response http.ResponseWriter, body []byte) {
	var request struct {
		Query  string `json:"query"`
		Filter struct {
			Value string `json:"value"`
		} `json:"filter"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &request))
	require.Equal(provider.t, "data_source", request.Filter.Value)
	provider.mutex.Lock()
	provider.queries = append(provider.queries, request.Query)
	var results []string
	for _, source := range provider.dataSources {
		if strings.Contains(strings.ToLower(source.title), strings.ToLower(request.Query)) {
			results = append(results, fmt.Sprintf(`{"object":"data_source","id":%q,"title":[{"plain_text":%q}],"in_trash":%t,`+
				`"parent":{"type":"database_id","database_id":"3b5e7091-2c4d-4e6f-9081-223344556677"}}`, source.id, source.title, source.isInTrash))
		}
	}
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, `{"object":"list","results":[`+strings.Join(results, ",")+`],"next_cursor":null,"has_more":false}`)
}

func (provider *fakeNotion) query(response http.ResponseWriter, dataSourceID string, body []byte) {
	if provider.isBroken(dataSourceID) {
		provider.writeJSON(response, http.StatusBadRequest, `{"object":"error","status":400,"code":"validation_error","message":"SENTINEL Submission ID is not a property"}`)
		return
	}
	var request struct {
		Filter struct {
			Property string `json:"property"`
			RichText struct {
				Equals string `json:"equals"`
			} `json:"rich_text"`
		} `json:"filter"`
		PageSize   int    `json:"page_size"`
		ResultType string `json:"result_type"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &request))
	require.Equal(provider.t, SubmissionIDProperty, request.Filter.Property)
	require.Equal(provider.t, "page", request.ResultType)
	provider.mutex.Lock()
	var results []string
	for _, row := range provider.rows {
		if row.dataSourceID == dataSourceID && row.submissionID == request.Filter.RichText.Equals && len(results) < request.PageSize {
			results = append(results, provider.pageJSON(row))
		}
	}
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, `{"object":"list","results":[`+strings.Join(results, ",")+`],"next_cursor":null,"has_more":false}`)
}

func (provider *fakeNotion) createPage(response http.ResponseWriter, body []byte) {
	row, err := decodeFakeRow(body)
	require.NoError(provider.t, err)
	provider.mutex.Lock()
	provider.creates = append(provider.creates, fakeWrite{key: row.submissionID, body: string(body), at: time.Now()})
	isFirstRateLimit := strings.Contains(row.submissionID, markerRateLimit) && !provider.rateLimited[row.submissionID]
	if isFirstRateLimit {
		provider.rateLimited[row.submissionID] = true
	}
	provider.mutex.Unlock()
	switch {
	case strings.Contains(row.submissionID, markerReject):
		provider.writeJSON(response, http.StatusBadRequest, `{"object":"error","status":400,"code":"validation_error","message":"SENTINEL Email is expected"}`)
	case isFirstRateLimit:
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"object":"error","status":429,"code":"rate_limited","message":"SENTINEL",`+
			`"additional_data":{"rate_limit_reason":"public_api_request_rate_limit","retry_after":"1"}}`)
	case strings.Contains(row.submissionID, markerTimeoutSaved):
		provider.storeRow(row)
		time.Sleep(2 * time.Second)
	case strings.Contains(row.submissionID, markerTimeoutLost):
		time.Sleep(2 * time.Second)
	case strings.Contains(row.submissionID, markerSaved503):
		saved := provider.storeRow(row)
		provider.writeJSON(response, http.StatusServiceUnavailable, `{"object":"error","status":503,"code":"service_unavailable","message":"SENTINEL saved",`+
			`"additional_data":{"retry_guidance":["Do not repeat the write."],"committed_resource_id":"`+saved.id+`"}}`)
	default:
		if strings.Contains(row.submissionID, markerSlowCreate) {
			time.Sleep(slowProviderDelay)
		}
		saved := provider.storeRow(row)
		provider.mutex.Lock()
		page := provider.pageJSON(saved)
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusOK, page)
	}
}

func (provider *fakeNotion) updatePage(response http.ResponseWriter, pageID string, body []byte) {
	provider.mutex.Lock()
	row := provider.rowByID(pageID)
	provider.patches = append(provider.patches, fakeWrite{key: pageID, body: string(body), at: time.Now()})
	provider.mutex.Unlock()
	if row == nil {
		provider.writeJSON(response, http.StatusNotFound, `{"object":"error","status":404,"code":"object_not_found","message":"SENTINEL"}`)
		return
	}
	if strings.Contains(row.submissionID, markerSlowUpdate) {
		time.Sleep(slowProviderDelay)
	}
	update, err := decodeFakeRow(body)
	require.NoError(provider.t, err)
	provider.mutex.Lock()
	row.title, row.email, row.submittedAt = update.title, update.email, update.submittedAt
	page := provider.pageJSON(row)
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, page)
}

func (provider *fakeNotion) getPage(response http.ResponseWriter, pageID string) {
	provider.mutex.Lock()
	row := provider.rowByID(pageID)
	var page string
	if row != nil {
		page = provider.pageJSON(row)
	}
	provider.mutex.Unlock()
	if row == nil {
		provider.writeJSON(response, http.StatusNotFound, `{"object":"error","status":404,"code":"object_not_found","message":"SENTINEL"}`)
		return
	}
	provider.writeJSON(response, http.StatusOK, page)
}

func (provider *fakeNotion) getBlocks(response http.ResponseWriter, pageID string) {
	provider.mutex.Lock()
	row := provider.rowByID(pageID)
	var blocks []string
	if row != nil {
		for index, paragraph := range row.paragraphs {
			blocks = append(blocks, fmt.Sprintf(`{"object":"block","id":"block-%d","type":"paragraph","has_children":false,"paragraph":{"rich_text":[{"plain_text":%q}]}}`,
				index, paragraph))
		}
	}
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, `{"object":"list","results":[`+strings.Join(blocks, ",")+`],"next_cursor":null,"has_more":false}`)
}

func (provider *fakeNotion) seedRow(dataSourceID string, submissionID string, title string, email string) string {
	return provider.storeRow(&fakeRow{dataSourceID: dataSourceID, submissionID: submissionID, title: title, email: email, submittedAt: "2026-01-01T00:00:00Z"}).id
}

func (provider *fakeNotion) storeRow(row *fakeRow) *fakeRow {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.nextRow++
	row.id = fmt.Sprintf("6e8a0324-5f70-4192-a3b4-%012d", provider.nextRow)
	provider.rows = append(provider.rows, row)
	return row
}

func (provider *fakeNotion) rowByID(pageID string) *fakeRow {
	for _, row := range provider.rows {
		if row.id == pageID {
			return row
		}
	}
	return nil
}

func (provider *fakeNotion) isBroken(dataSourceID string) bool {
	for _, source := range provider.dataSources {
		if source.id == dataSourceID {
			return source.isBroken
		}
	}
	return false
}

// pageJSON renders a row as a Notion page object; the caller holds the mutex.
func (provider *fakeNotion) pageJSON(row *fakeRow) string {
	return fmt.Sprintf(`{"object":"page","id":%q,"created_time":"2026-09-30T16:00:00.000Z","last_edited_time":"2026-09-30T16:00:00.000Z",`+
		`"in_trash":false,"is_archived":false,"url":"https://app.notion.com/p/%s",`+
		`"parent":{"type":"data_source_id","data_source_id":%q,"database_id":"3b5e7091-2c4d-4e6f-9081-223344556677"},"properties":{`+
		`"Name":{"id":"title","type":"title","title":[{"plain_text":%q}]},`+
		`"Submission ID":{"id":"sub","type":"rich_text","rich_text":[{"plain_text":%q}]},`+
		`"Email":{"id":"em","type":"email","email":%q},`+
		`"Submitted at":{"id":"at","type":"date","date":{"start":%q,"end":null,"time_zone":null}}}}`,
		row.id, strings.ReplaceAll(row.id, "-", ""), row.dataSourceID, row.title, row.submissionID, row.email, row.submittedAt)
}

func (provider *fakeNotion) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Notion-Request-Id", "fake-request")
	response.WriteHeader(status)
	_, err := response.Write([]byte(body))
	require.NoError(provider.t, err)
}

func (provider *fakeNotion) createCount(submissionID string) int {
	return len(provider.createTimes(submissionID))
}

func (provider *fakeNotion) createTimes(submissionID string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var times []time.Time
	for _, write := range provider.creates {
		if write.key == submissionID {
			times = append(times, write.at)
		}
	}
	return times
}

func (provider *fakeNotion) totalCreates() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.creates)
}

func (provider *fakeNotion) patchBodies(pageID string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var bodies []string
	for _, write := range provider.patches {
		if write.key == pageID {
			bodies = append(bodies, write.body)
		}
	}
	return bodies
}

func (provider *fakeNotion) rowCount(submissionID string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	count := 0
	for _, row := range provider.rows {
		if row.submissionID == submissionID {
			count++
		}
	}
	return count
}

func (provider *fakeNotion) searchQueries() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.queries...)
}

func (provider *fakeNotion) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests
}

// decodeFakeRow reads the title, submission ID, email, date, and paragraphs from a create or update body.
func decodeFakeRow(body []byte) (*fakeRow, error) {
	var request struct {
		Parent struct {
			DataSourceID string `json:"data_source_id"`
		} `json:"parent"`
		Properties map[string]json.RawMessage `json:"properties"`
		Children   []struct {
			Paragraph struct {
				RichText []struct {
					Text struct {
						Content string `json:"content"`
					} `json:"text"`
				} `json:"rich_text"`
			} `json:"paragraph"`
		} `json:"children"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	row := &fakeRow{dataSourceID: request.Parent.DataSourceID}
	keys := make([]string, 0, len(request.Properties))
	for key := range request.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var value struct {
			Title    []struct{ Text struct{ Content string } } `json:"title"`
			RichText []struct{ Text struct{ Content string } } `json:"rich_text"`
			Email    string                                    `json:"email"`
			Date     struct {
				Start string `json:"start"`
			} `json:"date"`
		}
		if err := json.Unmarshal(request.Properties[key], &value); err != nil {
			return nil, err
		}
		switch key {
		case notion.TitlePropertyID:
			row.title = joinFakeText(value.Title)
		case SubmissionIDProperty:
			row.submissionID = joinFakeText(value.RichText)
		case EmailProperty:
			row.email = value.Email
		case SubmittedAtProperty:
			row.submittedAt = value.Date.Start
		default:
			return nil, fmt.Errorf("unexpected property %q", key)
		}
	}
	for _, child := range request.Children {
		var text strings.Builder
		for _, segment := range child.Paragraph.RichText {
			text.WriteString(segment.Text.Content)
		}
		row.paragraphs = append(row.paragraphs, text.String())
	}
	return row, nil
}

func joinFakeText(segments []struct{ Text struct{ Content string } }) string {
	var text strings.Builder
	for _, segment := range segments {
		text.WriteString(segment.Text.Content)
	}
	return text.String()
}
