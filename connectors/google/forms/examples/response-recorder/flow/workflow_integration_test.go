//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package responserecorder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/forms"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	intakeFormID = "1FAIpQLintake"
	// fakeIntegrationToken is the only bearer token the fake accepts.
	fakeIntegrationToken = "forms-integration-token"
)

var (
	morning = time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	// sharedInstant is one submission time that several responses share, the case the cursor's IDs cover.
	sharedInstant = time.Date(2026, time.September, 30, 9, 15, 0, 123456000, time.UTC)
)

func TestResponseRecorderRecordsEachNewResponseOnceAcrossRunsWithRealDex(t *testing.T) {
	provider := newFormsProvider(t, 2)
	provider.addResponse(formsResponse{id: "r_acme", created: morning, lastSubmitted: morning, email: "buyer@acme.example", answers: map[string]any{
		"q_name":    textAnswer("Acme GmbH"),
		"q_regions": textAnswer("EU", "US"),
		"q_grid_eu": textAnswer("ISO"),
		"q_contract": map[string]any{"questionId": "q_contract", "fileUploadAnswers": map[string]any{"answers": []map[string]string{
			{"fileId": "file_msa", "fileName": "MSA.pdf", "mimeType": "application/pdf"},
		}}},
	}})
	provider.addResponse(formsResponse{id: "r_shared_b", created: sharedInstant, lastSubmitted: sharedInstant, answers: map[string]any{"q_name": textAnswer("Beta")}})
	provider.addResponse(formsResponse{id: "r_early", created: morning.Add(time.Hour), lastSubmitted: morning.Add(time.Hour), answers: map[string]any{"q_name": textAnswer("Early")}})
	provider.addResponse(formsResponse{id: "r_shared_a", created: sharedInstant, lastSubmitted: sharedInstant, answers: map[string]any{"q_name": textAnswer("Alpha")}})
	flow, harness := newFormsIntegrationHarness(t, provider.URL, FormConfiguration{FormID: intakeFormID, FormTitle: "Vendor intake"})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)

	first := runRecorder(t, ctx, harness.client, flow, "first-"+runID, Input{})
	require.Equal(t, dex.FlowCompleted, first.status)
	require.Equal(t, StatusRecorded, first.outcome.Status)
	require.Equal(t, "Vendor intake", first.outcome.FormTitle)
	require.Equal(t, 2, first.outcome.PagesRead)
	require.ElementsMatch(t, []string{"r_acme", "r_shared_b", "r_early", "r_shared_a"}, recordedIDs(first.outcome))
	require.Equal(t, sharedInstant, first.outcome.NextCursor.LastSubmittedAt)
	require.ElementsMatch(t, []string{"r_shared_a", "r_shared_b"}, first.outcome.NextCursor.ResponseIDs)
	acme := recordedResponse(t, first.outcome, "r_acme")
	require.Equal(t, "buyer@acme.example", acme.RespondentEmail)
	require.False(t, acme.IsEdited)
	require.Equal(t, []RecordedAnswer{
		{QuestionID: "q_name", Question: "Company name", Values: []string{"Acme GmbH"}},
		{QuestionID: "q_regions", Question: "Regions", Values: []string{"EU", "US"}},
		{QuestionID: "q_contract", Question: "Contract", FileNames: []string{"MSA.pdf"}},
		{QuestionID: "q_grid_eu", Question: "Certifications / EU entity", Values: []string{"ISO"}},
	}, acme.Answers)
	require.Empty(t, provider.filtersSent())

	unchanged := runRecorder(t, ctx, harness.client, flow, "unchanged-"+runID, Input{Cursor: first.outcome.NextCursor})
	require.Equal(t, dex.FlowCompleted, unchanged.status)
	require.Equal(t, StatusRecorded, unchanged.outcome.Status)
	require.Empty(t, unchanged.outcome.Responses)
	require.Equal(t, first.outcome.NextCursor, unchanged.outcome.NextCursor)
	require.Equal(t, []string{"timestamp >= 2026-09-30T09:15:00.123456Z"}, provider.filtersSent())

	// A late response at the shared instant, a later one, and an edit must all be recorded once.
	provider.addResponse(formsResponse{id: "r_shared_late", created: sharedInstant, lastSubmitted: sharedInstant, answers: map[string]any{"q_name": textAnswer("Late")}})
	provider.addResponse(formsResponse{id: "r_next", created: sharedInstant.Add(time.Minute), lastSubmitted: sharedInstant.Add(time.Minute), answers: map[string]any{}})
	provider.editResponse("r_acme", sharedInstant.Add(2*time.Minute), map[string]any{"q_name": textAnswer("Acme AG")})
	second := runRecorder(t, ctx, harness.client, flow, "second-"+runID, Input{Cursor: unchanged.outcome.NextCursor})
	require.Equal(t, dex.FlowCompleted, second.status)
	require.ElementsMatch(t, []string{"r_shared_late", "r_next", "r_acme"}, recordedIDs(second.outcome))
	editedAcme := recordedResponse(t, second.outcome, "r_acme")
	require.True(t, editedAcme.IsEdited)
	require.Equal(t, []RecordedAnswer{{QuestionID: "q_name", Question: "Company name", Values: []string{"Acme AG"}}}, editedAcme.Answers)
	require.Equal(t, &ResponseCursor{LastSubmittedAt: sharedInstant.Add(2 * time.Minute), ResponseIDs: []string{"r_acme"}}, second.outcome.NextCursor)
	require.Equal(t, []int{25}, provider.pageSizesSent())
}

func TestResponseRecorderReadsOneResponseByIDWithRealDex(t *testing.T) {
	provider := newFormsProvider(t, 2)
	provider.addResponse(formsResponse{id: "r_acme", created: morning, lastSubmitted: morning, answers: map[string]any{"q_name": textAnswer("Acme GmbH")}})
	provider.addResponse(formsResponse{id: "r_other", created: morning, lastSubmitted: morning, answers: map[string]any{"q_name": textAnswer("Other")}})
	flow, harness := newFormsIntegrationHarness(t, provider.URL, FormConfiguration{FormID: intakeFormID})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)

	named := runRecorder(t, ctx, harness.client, flow, "named-"+runID, Input{ResponseID: "r_acme"})
	require.Equal(t, dex.FlowCompleted, named.status)
	require.Equal(t, StatusRecorded, named.outcome.Status)
	require.Equal(t, []string{"r_acme"}, recordedIDs(named.outcome))
	require.Nil(t, named.outcome.NextCursor)
	require.Equal(t, 0, provider.listCalls())

	missing := runRecorder(t, ctx, harness.client, flow, "missing-"+runID, Input{ResponseID: "r_missing"})
	require.Equal(t, dex.FlowCompleted, missing.status)
	require.Equal(t, StatusResponseNotFound, missing.outcome.Status)
	require.Empty(t, missing.outcome.Responses)

	both := runRecorder(t, ctx, harness.client, flow, "both-"+runID, Input{ResponseID: "r_acme", Cursor: &ResponseCursor{LastSubmittedAt: morning}})
	require.Equal(t, dex.FlowFailed, both.status)
}

func TestResponseRecorderTruncatesAListingLongerThanItsPageBudgetWithRealDex(t *testing.T) {
	provider := newFormsProvider(t, 1)
	for index := range MaxResponsePages + 1 {
		submitted := morning.Add(time.Duration(index) * time.Minute)
		provider.addResponse(formsResponse{id: fmt.Sprintf("r_%02d", index), created: submitted, lastSubmitted: submitted, answers: map[string]any{}})
	}
	flow, harness := newFormsIntegrationHarness(t, provider.URL, FormConfiguration{FormID: intakeFormID})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cursor := &ResponseCursor{LastSubmittedAt: morning.Add(-time.Hour)}

	result := runRecorder(t, ctx, harness.client, flow, "truncated-"+strconv.FormatInt(time.Now().UnixNano(), 10), Input{Cursor: cursor})
	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusTruncated, result.outcome.Status)
	require.Equal(t, MaxResponsePages, result.outcome.PagesRead)
	require.Len(t, result.outcome.Responses, MaxResponsePages)
	require.Equal(t, cursor, result.outcome.NextCursor, "an unread page may hold an earlier response, so the cursor must not advance")
}

func TestResponseRecorderRetriesARateLimitedFormReadWithRealDex(t *testing.T) {
	provider := newFormsProvider(t, 2)
	provider.rateLimitFormReads(1)
	flow, harness := newFormsIntegrationHarness(t, provider.URL, FormConfiguration{FormID: intakeFormID})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result := runRecorder(t, ctx, harness.client, flow, "rate-limited-"+strconv.FormatInt(time.Now().UnixNano(), 10), Input{})
	require.Equal(t, dex.FlowCompleted, result.status)
	require.Equal(t, StatusRecorded, result.outcome.Status)
	require.Equal(t, 2, provider.formReads())
}

func TestResponseRecorderFailsWithoutAReadableFormWithRealDex(t *testing.T) {
	provider := newFormsProvider(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)

	unpicked, unpickedHarness := newFormsIntegrationHarness(t, provider.URL, FormConfiguration{})
	unpickedHarness.startWorker(t)
	require.Equal(t, dex.FlowFailed, runRecorder(t, ctx, unpickedHarness.client, unpicked, "unpicked-"+runID, Input{}).status)
	require.Equal(t, 0, provider.formReads())
	unpickedHarness.stopWorker(t)

	deleted, deletedHarness := newFormsIntegrationHarness(t, provider.URL, FormConfiguration{FormID: "1FAIpQLdeleted"})
	deletedHarness.startWorker(t)
	require.Equal(t, dex.FlowFailed, runRecorder(t, ctx, deletedHarness.client, deleted, "deleted-"+runID, Input{}).status)
	require.Equal(t, 1, provider.formReads())
}

// Dex re-dispatches an async attempt past about seven seconds; recording reads only the committed Result.
func TestResponseRecorderRecordsEachResponseOnceWhenAListingIsRedispatchedWithRealDex(t *testing.T) {
	provider := newFormsProvider(t, 25)
	provider.addResponse(formsResponse{id: "r_one", created: morning, lastSubmitted: morning, answers: map[string]any{}})
	provider.addResponse(formsResponse{id: "r_two", created: sharedInstant, lastSubmitted: sharedInstant, answers: map[string]any{}})
	provider.delayFirstListing(9 * time.Second)
	flow, harness := newFormsIntegrationHarness(t, provider.URL, FormConfiguration{FormID: intakeFormID})
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result := runRecorder(t, ctx, harness.client, flow, "redispatched-"+strconv.FormatInt(time.Now().UnixNano(), 10), Input{})
	require.Equal(t, dex.FlowCompleted, result.status)
	require.Len(t, result.outcome.Responses, 2)
	require.ElementsMatch(t, []string{"r_one", "r_two"}, recordedIDs(result.outcome))
	require.Equal(t, 1, result.outcome.PagesRead)
	require.GreaterOrEqual(t, provider.listCalls(), 2, "Dex should have dispatched the slow listing again")
}

type recorderRun struct {
	status  dex.FlowStatus
	outcome Outcome
}

func runRecorder(t *testing.T, ctx context.Context, client *dex.Client, flow *Flow, flowID string, input Input) recorderRun {
	t.Helper()
	_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	run := recorderRun{status: result.Status}
	if result.Status == dex.FlowCompleted {
		require.NoError(t, result.DecodeSingleOutput(&run.outcome))
	}
	return run
}

func recordedIDs(outcome Outcome) []string {
	ids := make([]string, 0, len(outcome.Responses))
	for _, response := range outcome.Responses {
		ids = append(ids, response.ResponseID)
	}
	return ids
}

func recordedResponse(t *testing.T, outcome Outcome, responseID string) RecordedResponse {
	t.Helper()
	index := slices.IndexFunc(outcome.Responses, func(response RecordedResponse) bool { return response.ResponseID == responseID })
	require.GreaterOrEqual(t, index, 0, responseID)
	return outcome.Responses[index]
}

func textAnswer(values ...string) map[string]any {
	answers := make([]map[string]string, 0, len(values))
	for _, value := range values {
		answers = append(answers, map[string]string{"value": value})
	}
	return map[string]any{"textAnswers": map[string]any{"answers": answers}}
}

type formsResponse struct {
	id            string
	created       time.Time
	lastSubmitted time.Time
	email         string
	answers       map[string]any
}

// formsProvider fakes the Forms API subset the example uses, listing newest insertion first, not by time.
type formsProvider struct {
	*httptest.Server
	t                *testing.T
	mutex            sync.Mutex
	pageLimit        int
	responses        []formsResponse
	filters          []string
	pageSizes        []int
	formReadCount    int
	listCallCount    int
	rateLimitedReads int
	firstListDelay   time.Duration
}

func newFormsProvider(t *testing.T, pageLimit int) *formsProvider {
	t.Helper()
	provider := &formsProvider{t: t, pageLimit: pageLimit}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *formsProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+fakeIntegrationToken {
		provider.writeJSON(response, http.StatusUnauthorized, googleErrorBody(401, "UNAUTHENTICATED"))
		return
	}
	formPath := strings.TrimPrefix(request.URL.Path, "/v1/forms/")
	formID, formResourcePath, _ := strings.Cut(formPath, "/")
	switch {
	case request.Method != http.MethodGet:
		http.NotFound(response, request)
	case formID != intakeFormID && formResourcePath == "":
		provider.countFormRead()
		provider.writeJSON(response, http.StatusNotFound, googleErrorBody(404, "NOT_FOUND"))
	case formResourcePath == "":
		provider.readForm(response)
	case formResourcePath == "responses":
		provider.listResponses(response, request)
	case strings.HasPrefix(formResourcePath, "responses/"):
		provider.getResponse(response, strings.TrimPrefix(formResourcePath, "responses/"))
	default:
		http.NotFound(response, request)
	}
}

func (provider *formsProvider) readForm(response http.ResponseWriter) {
	provider.mutex.Lock()
	provider.formReadCount++
	isRateLimited := provider.rateLimitedReads > 0
	if isRateLimited {
		provider.rateLimitedReads--
	}
	provider.mutex.Unlock()
	if isRateLimited {
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, googleErrorBody(429, "RESOURCE_EXHAUSTED"))
		return
	}
	provider.writeJSON(response, http.StatusOK, map[string]any{
		"formId": intakeFormID, "info": map[string]any{"title": "Vendor intake"},
		"items": []any{
			map[string]any{"itemId": "it_name", "title": "Company name", "questionItem": map[string]any{"question": map[string]any{"questionId": "q_name", "textQuestion": map[string]any{}}}},
			map[string]any{"itemId": "it_regions", "title": "Regions", "questionItem": map[string]any{"question": map[string]any{"questionId": "q_regions",
				"choiceQuestion": map[string]any{"type": "CHECKBOX", "options": []any{map[string]any{"value": "EU"}, map[string]any{"value": "US"}}}}}},
			map[string]any{"itemId": "it_contract", "title": "Contract", "questionItem": map[string]any{"question": map[string]any{"questionId": "q_contract", "fileUploadQuestion": map[string]any{}}}},
			map[string]any{"itemId": "it_grid", "title": "Certifications", "questionGroupItem": map[string]any{
				"grid":      map[string]any{"columns": map[string]any{"type": "CHECKBOX", "options": []any{map[string]any{"value": "ISO"}}}},
				"questions": []any{map[string]any{"questionId": "q_grid_eu", "rowQuestion": map[string]any{"title": "EU entity"}}},
			}},
		},
	})
}

func (provider *formsProvider) listResponses(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	provider.mutex.Lock()
	provider.listCallCount++
	delay := provider.firstListDelay
	provider.firstListDelay = 0
	pageSize, err := strconv.Atoi(query.Get("pageSize"))
	require.NoError(provider.t, err)
	if query.Get("pageToken") == "" {
		provider.pageSizes = append(provider.pageSizes, pageSize)
		if filter := query.Get("filter"); filter != "" {
			provider.filters = append(provider.filters, filter)
		}
	}
	var matches []formsResponse
	for index := len(provider.responses) - 1; index >= 0; index-- {
		if provider.matchesFilter(provider.responses[index], query.Get("filter")) {
			matches = append(matches, provider.responses[index])
		}
	}
	provider.mutex.Unlock()
	time.Sleep(delay)
	offset := 0
	if token := query.Get("pageToken"); token != "" {
		offset, err = strconv.Atoi(strings.TrimPrefix(token, "offset-"))
		require.NoError(provider.t, err)
	}
	end := min(offset+min(pageSize, provider.pageLimit), len(matches))
	page := []map[string]any{}
	for _, match := range matches[offset:end] {
		page = append(page, match.resource(false))
	}
	body := map[string]any{"responses": page}
	if end < len(matches) {
		body["nextPageToken"] = "offset-" + strconv.Itoa(end)
	}
	provider.writeJSON(response, http.StatusOK, body)
}

// matchesFilter evaluates Google's two documented submission time filters against lastSubmittedTime.
func (provider *formsProvider) matchesFilter(response formsResponse, filter string) bool {
	if filter == "" {
		return true
	}
	operator, value, ok := strings.Cut(strings.TrimPrefix(filter, "timestamp "), " ")
	require.True(provider.t, ok, filter)
	threshold, err := time.Parse(time.RFC3339Nano, value)
	require.NoError(provider.t, err)
	switch operator {
	case ">":
		return response.lastSubmitted.After(threshold)
	case ">=":
		return !response.lastSubmitted.Before(threshold)
	default:
		provider.t.Errorf("unexpected filter %q", filter)
		return false
	}
}

func (provider *formsProvider) getResponse(response http.ResponseWriter, responseID string) {
	provider.mutex.Lock()
	index := slices.IndexFunc(provider.responses, func(candidate formsResponse) bool { return candidate.id == responseID })
	var found formsResponse
	if index >= 0 {
		found = provider.responses[index]
	}
	provider.mutex.Unlock()
	if index < 0 {
		provider.writeJSON(response, http.StatusNotFound, googleErrorBody(404, "NOT_FOUND"))
		return
	}
	provider.writeJSON(response, http.StatusOK, found.resource(true))
}

func (provider *formsProvider) addResponse(response formsResponse) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.responses = append(provider.responses, response)
}

func (provider *formsProvider) editResponse(responseID string, lastSubmitted time.Time, answers map[string]any) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	index := slices.IndexFunc(provider.responses, func(candidate formsResponse) bool { return candidate.id == responseID })
	require.GreaterOrEqual(provider.t, index, 0)
	provider.responses[index].lastSubmitted, provider.responses[index].answers = lastSubmitted, answers
}

func (provider *formsProvider) rateLimitFormReads(count int) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.rateLimitedReads = count
}

func (provider *formsProvider) delayFirstListing(delay time.Duration) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.firstListDelay = delay
}

func (provider *formsProvider) countFormRead() {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.formReadCount++
}

func (provider *formsProvider) formReads() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.formReadCount
}

func (provider *formsProvider) listCalls() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.listCallCount
}

func (provider *formsProvider) filtersSent() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return slices.Clone(provider.filters)
}

func (provider *formsProvider) pageSizesSent() []int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return slices.Compact(slices.Clone(provider.pageSizes))
}

func (provider *formsProvider) writeJSON(response http.ResponseWriter, status int, body any) {
	contents, err := json.Marshal(body)
	require.NoError(provider.t, err)
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err = response.Write(contents)
	require.NoError(provider.t, err)
}

// resource renders the response as Google does; a listed response omits formId.
func (response formsResponse) resource(includesFormID bool) map[string]any {
	answers := map[string]any{}
	for questionID, answer := range response.answers {
		answers[questionID] = answer
	}
	resource := map[string]any{
		"responseId": response.id, "createTime": response.created.Format(time.RFC3339Nano),
		"lastSubmittedTime": response.lastSubmitted.Format(time.RFC3339Nano), "answers": answers,
	}
	if response.email != "" {
		resource["respondentEmail"] = response.email
	}
	if includesFormID {
		resource["formId"] = intakeFormID
	}
	return resource
}

func googleErrorBody(code int, status string) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "message": "fake provider detail", "status": status}}
}

type formsIntegrationHarness struct {
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newFormsIntegrationHarness(t *testing.T, endpoint string, form FormConfiguration) (*Flow, *formsIntegrationHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName}
	providerClient, err := forms.New(forms.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[forms.Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString(fakeIntegrationToken)},
	})
	require.NoError(t, err)
	connection, err := forms.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection, sdkgo.ConnectorLoadedConfiguration[FormConfiguration]{Reference: FormConfigurationRef(), Value: form})
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &formsIntegrationHarness{
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

func (harness *formsIntegrationHarness) startWorker(t *testing.T) {
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

func (harness *formsIntegrationHarness) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult))
	harness.worker = nil
	harness.workerResult = nil
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
