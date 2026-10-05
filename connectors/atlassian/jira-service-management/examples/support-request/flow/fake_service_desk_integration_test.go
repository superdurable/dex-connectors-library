//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package supportrequest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	integrationCloudID     = "11223344-a1b2-4b33-8c44-def123456789"
	integrationAccessToken = "integration-access-token"
	janeAccountID          = "qm:a713c8ea-1075-4e30-9d96-891a7d181739:5ad6d69abfa3980ce712caae"
	otherCustomerAccountID = "qm:a713c8ea-1075-4e30-9d96-891a7d181739:5b10ac8d82e05b22cc7d4ef5"
	nearDuplicateKey       = "ITH-1"
	openDuplicateKey       = "ITH-2"
	openDuplicateSummary   = "VPN drops every hour"

	markerReject         = "[reject]"
	markerRateLimit      = "[rate-limit]"
	markerServerError    = "[server-error]"
	markerCreateTimeout  = "[create-timeout]"
	markerSlowCreate     = "[slow-create]"
	markerSlowReply      = "[slow-reply]"
	markerSlowLabels     = "[slow-labels]"
	markerSlowTransition = "[slow-transition]"
	markerNoteTimeout    = "[note-timeout]"
)

// fakeServiceDesk is a credential-safe, stateful Jira Service Management fake. A marker in the summary selects a failure.
type fakeServiceDesk struct {
	*httptest.Server
	t                 *testing.T
	mutex             sync.Mutex
	nextIssueNumber   int
	nextCommentNumber int
	requests          map[string]*fakeRequest
	createTimes       map[string][]time.Time
	updates           map[string]int
	labelReads        map[string]int
	transitionPosts   map[string]int
	searches          []string
	resourcesRequests int
}

// fakeRequest is one customer request; isIndexed false keeps a just-created request out of search.
type fakeRequest struct {
	number            int
	summary           string
	reporterAccountID string
	statusID          string
	labels            []string
	priorityName      string
	comments          []bool
	isIndexed         bool
}

var (
	fakeStatuses = map[string][2]string{"1": {"Waiting for support", "new"}, "3": {"In progress", "indeterminate"}, "5": {"Resolved", "done"}}
	// fakeWorkflow maps a source status to its transitions: ID, name, destination.
	fakeWorkflow = map[string][][3]string{
		"1": {{"11", "Start work", "3"}, {"31", "Resolve this issue", "5"}},
		"3": {{"21", "Pending", "1"}, {"31", "Resolve this issue", "5"}},
		"5": {{"51", "Reopen", "1"}},
	}
	fakeCustomers = []struct{ accountID, displayName, email string }{
		{janeAccountID, "Jane Smith", "jane@acme.example.com"},
		{otherCustomerAccountID, "Jane Smithers", "jane.smithers@acme.example.com"},
	}
	summaryPhrasePattern = regexp.MustCompile(`summary ~ "\\"(.*)\\""`)
	reporterPattern      = regexp.MustCompile(`reporter in \("([^"]+)"\)`)
	sitePathPrefix       = "/ex/jira/" + integrationCloudID
	issuePathPattern     = regexp.MustCompile(`^` + sitePathPrefix + `/rest/api/3/issue/([A-Za-z0-9-]+)(/transitions)?$`)
	requestPathPattern   = regexp.MustCompile(`^` + sitePathPrefix + `/rest/servicedeskapi/request/([A-Za-z0-9-]+)/(comment|sla)$`)
)

func newFakeServiceDesk(t *testing.T) *fakeServiceDesk {
	t.Helper()
	provider := &fakeServiceDesk{
		t: t, nextIssueNumber: 5, nextCommentNumber: 10500,
		requests: map[string]*fakeRequest{
			nearDuplicateKey: {number: 1, summary: "Laptop will not boot (again)", reporterAccountID: janeAccountID, statusID: "1", isIndexed: true},
			openDuplicateKey: {number: 2, summary: openDuplicateSummary, reporterAccountID: janeAccountID, statusID: "1", isIndexed: true},
			"ITH-3":          {number: 3, summary: openDuplicateSummary, reporterAccountID: janeAccountID, statusID: "5", isIndexed: true},
			"ITH-4":          {number: 4, summary: "Laptop will not boot", reporterAccountID: otherCustomerAccountID, statusID: "1", isIndexed: true},
		},
		createTimes: map[string][]time.Time{}, updates: map[string]int{}, labelReads: map[string]int{}, transitionPosts: map[string]int{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeServiceDesk) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != "Bearer "+integrationAccessToken {
		provider.writeJSON(response, http.StatusUnauthorized, `{"code":401,"message":"SENTINEL Unauthorized"}`)
		return
	}
	path := request.URL.Path
	switch {
	case request.Method == http.MethodGet && path == "/oauth/token/accessible-resources":
		provider.mutex.Lock()
		provider.resourcesRequests++
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusOK, `[{"id":"`+integrationCloudID+`","name":"Help","url":"https://help.atlassian.net",`+
			`"scopes":["read:servicedesk-request","write:servicedesk-request","read:jira-work","write:jira-work"]}]`)
	case request.Method == http.MethodGet && path == sitePathPrefix+"/rest/servicedeskapi/servicedesk/10/customer":
		provider.listCustomers(response, request)
	case request.Method == http.MethodPost && path == sitePathPrefix+"/rest/api/3/search/jql":
		provider.searchRequests(response, request)
	case request.Method == http.MethodPost && path == sitePathPrefix+"/rest/servicedeskapi/request":
		provider.createRequest(response, request)
	case issuePathPattern.MatchString(path):
		match := issuePathPattern.FindStringSubmatch(path)
		provider.serveIssue(response, request, strings.ToUpper(match[1]), match[2])
	case requestPathPattern.MatchString(path):
		match := requestPathPattern.FindStringSubmatch(path)
		provider.serveRequestResource(response, request, strings.ToUpper(match[1]), match[2])
	default:
		provider.writeJSON(response, http.StatusNotFound, `{"errorMessage":"SENTINEL no route"}`)
	}
}

func (provider *fakeServiceDesk) listCustomers(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("X-ExperimentalApi") != "opt-in" {
		provider.writeJSON(response, http.StatusPreconditionFailed, `{"errorMessage":"SENTINEL experimental API not opted in"}`)
		return
	}
	query := strings.ToLower(request.URL.Query().Get("query"))
	var values []string
	for _, customer := range fakeCustomers {
		if strings.Contains(customer.email, query) {
			values = append(values, fmt.Sprintf(`{"accountId":%q,"displayName":%q,"emailAddress":%q,"active":true}`, customer.accountID, customer.displayName, customer.email))
		}
	}
	provider.writeJSON(response, http.StatusOK, `{"start":0,"limit":50,"size":`+strconv.Itoa(len(values))+`,"isLastPage":true,"values":[`+strings.Join(values, ",")+`]}`)
}

func (provider *fakeServiceDesk) searchRequests(response http.ResponseWriter, request *http.Request) {
	var body struct {
		JQL string `json:"jql"`
	}
	require.NoError(provider.t, json.NewDecoder(request.Body).Decode(&body))
	phrase, reporter := "", ""
	if match := summaryPhrasePattern.FindStringSubmatch(body.JQL); match != nil {
		phrase = strings.ToLower(match[1])
	}
	if match := reporterPattern.FindStringSubmatch(body.JQL); match != nil {
		reporter = match[1]
	}
	provider.mutex.Lock()
	provider.searches = append(provider.searches, body.JQL)
	var issues []string
	for key, fake := range provider.requests {
		// Search is eventually consistent: a just-created request is not indexed yet.
		if fake.isIndexed && fakeStatuses[fake.statusID][1] != "done" && fake.reporterAccountID == reporter && strings.Contains(strings.ToLower(fake.summary), phrase) {
			issues = append(issues, provider.issueJSON(key, fake, false))
		}
	}
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, `{"issues":[`+strings.Join(issues, ",")+`]}`)
}

func (provider *fakeServiceDesk) createRequest(response http.ResponseWriter, request *http.Request) {
	var body struct {
		ServiceDeskID      string `json:"serviceDeskId"`
		RequestTypeID      string `json:"requestTypeId"`
		RaiseOnBehalfOf    string `json:"raiseOnBehalfOf"`
		RequestFieldValues struct {
			Summary string `json:"summary"`
		} `json:"requestFieldValues"`
	}
	require.NoError(provider.t, json.NewDecoder(request.Body).Decode(&body))
	summary := body.RequestFieldValues.Summary
	if body.ServiceDeskID != "10" || body.RequestTypeID != "25" {
		provider.writeJSON(response, http.StatusBadRequest, `{"errorMessage":"SENTINEL unknown request type","i18nErrorMessage":{"i18nKey":"sd.request.type.unknown"}}`)
		return
	}
	provider.mutex.Lock()
	provider.createTimes[summary] = append(provider.createTimes[summary], time.Now())
	attempt := len(provider.createTimes[summary])
	provider.mutex.Unlock()
	switch {
	case strings.Contains(summary, markerReject):
		provider.writeJSON(response, http.StatusBadRequest, `{"errorMessage":"SENTINEL Category is required",`+
			`"i18nErrorMessage":{"i18nKey":"sd.request.create.field.required","parameters":["SENTINEL"]},"errors":{"customfield_10050":"SENTINEL required"}}`)
	case strings.Contains(summary, markerRateLimit) && attempt == 1:
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"errorMessage":"SENTINEL rate limited"}`)
	case strings.Contains(summary, markerServerError) && attempt == 1:
		provider.writeJSON(response, http.StatusInternalServerError, `{"errorMessage":"SENTINEL internal error"}`)
	case strings.Contains(summary, markerCreateTimeout):
		provider.storeRequest(summary, body.RaiseOnBehalfOf)
		// The request was raised, but the response never arrives before the connector gives up.
		<-request.Context().Done()
	case strings.Contains(summary, markerSlowCreate):
		key := provider.storeRequest(summary, body.RaiseOnBehalfOf)
		time.Sleep(slowProviderDelay)
		provider.writeJSON(response, http.StatusCreated, provider.createdJSON(key))
	default:
		provider.writeJSON(response, http.StatusCreated, provider.createdJSON(provider.storeRequest(summary, body.RaiseOnBehalfOf)))
	}
}

func (provider *fakeServiceDesk) serveIssue(response http.ResponseWriter, request *http.Request, key string, subresource string) {
	provider.mutex.Lock()
	fake, exists := provider.requests[key]
	provider.mutex.Unlock()
	if !exists {
		provider.writeJSON(response, http.StatusNotFound, `{"errorMessages":["SENTINEL Issue does not exist or you do not have permission to see it."],"errors":{}}`)
		return
	}
	switch {
	case request.Method == http.MethodGet && subresource == "":
		provider.mutex.Lock()
		if request.URL.Query().Get("fields") == "labels,priority" {
			provider.labelReads[key]++
		}
		body := provider.issueJSON(key, fake, request.URL.Query().Get("expand") == "transitions")
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusOK, body)
	case request.Method == http.MethodPut && subresource == "":
		provider.editIssue(response, request, key, fake)
	case request.Method == http.MethodPost && subresource == "/transitions":
		provider.transitionIssue(response, request, key, fake)
	default:
		provider.writeJSON(response, http.StatusMethodNotAllowed, `{}`)
	}
}

func (provider *fakeServiceDesk) editIssue(response http.ResponseWriter, request *http.Request, key string, fake *fakeRequest) {
	var body struct {
		Update struct {
			Labels []struct {
				Add    string `json:"add"`
				Remove string `json:"remove"`
			} `json:"labels"`
		} `json:"update"`
		Fields struct {
			Priority *struct {
				Name string `json:"name"`
			} `json:"priority"`
		} `json:"fields"`
	}
	require.NoError(provider.t, json.NewDecoder(request.Body).Decode(&body))
	provider.mutex.Lock()
	provider.updates[key]++
	for _, operation := range body.Update.Labels {
		if operation.Add != "" && !containsString(fake.labels, operation.Add) {
			fake.labels = append(fake.labels, operation.Add)
		}
		if operation.Remove != "" {
			fake.labels = removeString(fake.labels, operation.Remove)
		}
	}
	if body.Fields.Priority != nil {
		fake.priorityName = body.Fields.Priority.Name
	}
	provider.mutex.Unlock()
	if strings.Contains(fake.summary, markerSlowLabels) {
		// Jira applied the change, but the response arrives after Dex's async local phase ends.
		time.Sleep(slowProviderDelay)
	}
	response.WriteHeader(http.StatusNoContent)
}

func (provider *fakeServiceDesk) transitionIssue(response http.ResponseWriter, request *http.Request, key string, fake *fakeRequest) {
	var body struct {
		Transition struct {
			ID string `json:"id"`
		} `json:"transition"`
	}
	require.NoError(provider.t, json.NewDecoder(request.Body).Decode(&body))
	provider.mutex.Lock()
	provider.transitionPosts[key]++
	destination := ""
	for _, transition := range fakeWorkflow[fake.statusID] {
		if transition[0] == body.Transition.ID {
			destination = transition[2]
		}
	}
	if destination != "" {
		fake.statusID = destination
	}
	provider.mutex.Unlock()
	if destination == "" {
		provider.writeJSON(response, http.StatusBadRequest, `{"errorMessages":["SENTINEL Transition is not valid for this issue."],"errors":{}}`)
		return
	}
	if strings.Contains(fake.summary, markerSlowTransition) {
		time.Sleep(slowProviderDelay)
	}
	response.WriteHeader(http.StatusNoContent)
}

func (provider *fakeServiceDesk) serveRequestResource(response http.ResponseWriter, request *http.Request, key string, resource string) {
	provider.mutex.Lock()
	fake, exists := provider.requests[key]
	provider.mutex.Unlock()
	if !exists {
		provider.writeJSON(response, http.StatusNotFound, `{"errorMessage":"SENTINEL Request does not exist"}`)
		return
	}
	switch {
	case request.Method == http.MethodGet && resource == "sla":
		provider.writeJSON(response, http.StatusOK, `{"start":0,"limit":50,"size":2,"isLastPage":true,"values":[`+
			`{"id":"1","name":"Time to first response","ongoingCycle":{"startTime":{"epochMillis":1790000000000},"breachTime":{"epochMillis":1790003600000},`+
			`"breached":false,"paused":false,"withinCalendarHours":true,"goalDuration":{"millis":3600000},"elapsedTime":{"millis":60000},"remainingTime":{"millis":3540000}},"completedCycles":[]},`+
			`{"id":"2","name":"Time to resolution","ongoingCycle":{"startTime":{"epochMillis":1790000000000},"breachTime":{"epochMillis":1790028800000},`+
			`"breached":false,"paused":false,"withinCalendarHours":true,"goalDuration":{"millis":28800000},"elapsedTime":{"millis":60000},"remainingTime":{"millis":28740000}},"completedCycles":[]}]}`)
	case request.Method == http.MethodGet:
		provider.mutex.Lock()
		var values []string
		for index, isPublic := range fake.comments {
			values = append(values, fmt.Sprintf(`{"id":"%d","body":"comment","public":%t,"created":{"epochMillis":1790000000000}}`, 10000+index, isPublic))
		}
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusOK, `{"start":0,"limit":10,"size":`+strconv.Itoa(len(values))+`,"isLastPage":true,"values":[`+strings.Join(values, ",")+`]}`)
	case request.Method == http.MethodPost:
		provider.addComment(response, request, fake)
	default:
		provider.writeJSON(response, http.StatusMethodNotAllowed, `{}`)
	}
}

func (provider *fakeServiceDesk) addComment(response http.ResponseWriter, request *http.Request, fake *fakeRequest) {
	var body struct {
		Body   string `json:"body"`
		Public *bool  `json:"public"`
	}
	require.NoError(provider.t, json.NewDecoder(request.Body).Decode(&body))
	if body.Public == nil {
		provider.writeJSON(response, http.StatusBadRequest, `{"errorMessage":"SENTINEL public is required"}`)
		return
	}
	provider.mutex.Lock()
	fake.comments = append(fake.comments, *body.Public)
	provider.nextCommentNumber++
	commentID := provider.nextCommentNumber
	provider.mutex.Unlock()
	switch {
	case strings.Contains(fake.summary, markerNoteTimeout) && !*body.Public:
		// The note was stored, but the response never arrives before the connector gives up.
		_, err := io.Copy(io.Discard, request.Body)
		require.NoError(provider.t, err)
		<-request.Context().Done()
		return
	case strings.Contains(fake.summary, markerSlowReply) && *body.Public:
		time.Sleep(slowProviderDelay)
	}
	provider.writeJSON(response, http.StatusCreated, fmt.Sprintf(`{"id":"%d","body":"SENTINEL echo","public":%t,"created":{"epochMillis":1790000600000}}`, commentID, *body.Public))
}

func (provider *fakeServiceDesk) storeRequest(summary string, reporterAccountID string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	number := provider.nextIssueNumber
	provider.nextIssueNumber++
	key := "ITH-" + strconv.Itoa(number)
	provider.requests[key] = &fakeRequest{number: number, summary: summary, reporterAccountID: reporterAccountID, statusID: "1"}
	return key
}

// issueJSON renders a request as its Jira issue; the caller holds the mutex.
func (provider *fakeServiceDesk) issueJSON(key string, fake *fakeRequest, includesTransitions bool) string {
	status := fakeStatuses[fake.statusID]
	labels, err := json.Marshal(append([]string{}, fake.labels...))
	require.NoError(provider.t, err)
	priority := "null"
	if fake.priorityName != "" {
		priority = fmt.Sprintf(`{"id":"2","name":%q}`, fake.priorityName)
	}
	fields := fmt.Sprintf(`"summary":%q,"status":{"id":%q,"name":%q,"statusCategory":{"key":%q}},"project":{"id":"10000","key":"ITH"},`+
		`"reporter":{"accountId":%q,"displayName":"Customer","emailAddress":"SENTINEL@acme.example.com"},"priority":%s,"labels":%s,`+
		`"created":"2026-10-04T09:15:00.000-0700","updated":"2026-10-04T09:15:00.000-0700",`+
		`"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[`+
		`{"type":"text","text":"My laptop will not boot."},{"type":"hardBreak"},{"type":"text","text":"Error 0x7B."}]}]}`,
		fake.summary, fake.statusID, status[0], status[1], fake.reporterAccountID, priority, labels)
	transitions := ""
	if includesTransitions {
		var rendered []string
		for _, transition := range fakeWorkflow[fake.statusID] {
			destination := fakeStatuses[transition[2]]
			rendered = append(rendered, fmt.Sprintf(`{"id":%q,"name":%q,"to":{"id":%q,"name":%q,"statusCategory":{"key":%q}}}`,
				transition[0], transition[1], transition[2], destination[0], destination[1]))
		}
		transitions = `,"transitions":[` + strings.Join(rendered, ",") + `]`
	}
	return fmt.Sprintf(`{"id":"%d","key":%q,"fields":{%s}%s}`, 10000+fake.number, key, fields, transitions)
}

func (provider *fakeServiceDesk) createdJSON(key string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return fmt.Sprintf(`{"issueId":"%d","issueKey":%q,"requestTypeId":"25","serviceDeskId":"10","createdDate":{"epochMillis":1790000000000},`+
		`"currentStatus":{"status":"Waiting for support","statusCategory":"NEW"}}`, 10000+provider.requests[key].number, key)
}

func (provider *fakeServiceDesk) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		provider.t.Logf("fake Jira Service Management response write failed: %v", err)
	}
}

// request returns a snapshot of one stored request.
func (provider *fakeServiceDesk) request(key string) fakeRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	fake, exists := provider.requests[key]
	require.True(provider.t, exists, "fake Jira Service Management has no request %s", key)
	snapshot := *fake
	snapshot.labels = append([]string(nil), fake.labels...)
	snapshot.comments = append([]bool(nil), fake.comments...)
	return snapshot
}

func (fake fakeRequest) commentVisibilities() []bool { return fake.comments }

func (provider *fakeServiceDesk) createCount(summary string) int {
	return len(provider.createTimesFor(summary))
}

func (provider *fakeServiceDesk) createTimesFor(summary string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]time.Time(nil), provider.createTimes[summary]...)
}

func (provider *fakeServiceDesk) updateCount(key string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.updates[key]
}

func (provider *fakeServiceDesk) labelReadCount(key string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.labelReads[key]
}

func (provider *fakeServiceDesk) transitionPostCount(key string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.transitionPosts[key]
}

func (provider *fakeServiceDesk) searchQueries() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.searches...)
}

func (provider *fakeServiceDesk) accessibleResourcesCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.resourcesRequests
}

func (provider *fakeServiceDesk) keyForSummary(summary string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	for key, fake := range provider.requests {
		if fake.summary == summary {
			return key
		}
	}
	provider.t.Fatalf("fake Jira Service Management has no request with summary %q", summary)
	return ""
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func removeString(values []string, unwanted string) []string {
	kept := values[:0]
	for _, value := range values {
		if value != unwanted {
			kept = append(kept, value)
		}
	}
	return kept
}
