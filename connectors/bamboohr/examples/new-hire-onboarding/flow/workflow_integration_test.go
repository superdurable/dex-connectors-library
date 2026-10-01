//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newhireonboarding

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	integrationCompanyDomain = "acme"
	// integrationAPIKey is split so secret scanners do not mistake the 40-hex fixture for a real key.
	integrationAPIKey        = "0123456789abcdef" + "0123456789abcdef01234567"
	integrationPersonalEmail = "ava.nguyen@personal.example.com"
	integrationHandoffField  = "customITProvisioning"

	defaultRequestTimeout = 5 * time.Second
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowResponseDelay  = 9 * time.Second
	slowRequestTimeout = 20 * time.Second
)

var (
	filterStartPattern    = regexp.MustCompile(`startDate le '(\d{4}-\d{2}-\d{2})'`)
	filterEndPattern      = regexp.MustCompile(`endDate ge '(\d{4}-\d{2}-\d{2})'`)
	filterEmployeePattern = regexp.MustCompile(`employeeId eq (\d+)`)
	filterStatusesPattern = regexp.MustCompile(`status (?:in \(([^)]*)\)|eq '([a-z]+)')`)
)

func integrationInput() Input {
	return Input{FirstName: "Ava", LastName: "Nguyen", PersonalEmail: integrationPersonalEmail, HireDate: "2026-10-13", HandoffFieldName: integrationHandoffField}
}

// completeRecord is what HR fills in before IT can provision accounts.
func completeRecord() map[string]string {
	return map[string]string{
		"firstName": "Ava", "lastName": "Nguyen", "homeEmail": integrationPersonalEmail, "hireDate": "2026-10-13",
		"department": "Engineering", "jobTitle": "Platform Engineer", "location": "Salt Lake City", "supervisorEId": "101", "status": "Active",
	}
}

func TestRerunAfterHRCompletesTheRecordFindsTheHireAndHandsOffWithRealDex(t *testing.T) {
	provider := newFakeBambooHR(t)
	provider.seedEmployee(map[string]string{"firstName": "Ava", "lastName": "Nguyen-Smith", "homeEmail": "ava.nguyen@personal.example.com.au", "status": "Active"})
	harness := newOnboardingHarness(t, provider, defaultRequestTimeout)

	first := harness.runOnboarding(t, "new-hire", integrationInput())
	require.Equal(t, OnboardingHeld, first.Action, "a record with only name, email, and hire date is not ready for IT")
	require.True(t, first.WasEmployeeAdded)
	require.Equal(t, []string{"department", "jobTitle", "location", "supervisorEId"}, first.MissingFields)
	require.Equal(t, 1, provider.count("add"))
	require.JSONEq(t, `{"firstName":"Ava","lastName":"Nguyen","homeEmail":"ava.nguyen@personal.example.com","hireDate":"2026-10-13"}`, provider.lastRequest("add").body)
	require.Equal(t, []string{integrationPersonalEmail}, provider.lastRequest("find").query["filter[homeEmail]"])
	require.Zero(t, provider.count("timeOff"), "nothing is handed off, so nothing more is read")
	require.Zero(t, provider.count("update"))

	provider.setFields(first.EmployeeID, map[string]string{"department": "Engineering", "jobTitle": "Platform Engineer", "location": "Salt Lake City", "supervisorEId": "101"})
	second := harness.runOnboarding(t, "rerun", integrationInput())
	require.Equal(t, OnboardingHandedOff, second.Action)
	require.Equal(t, first.EmployeeID, second.EmployeeID, "the re-run found the hire by personal email")
	require.False(t, second.WasEmployeeAdded)
	require.Equal(t, 1, provider.count("add"), "the re-run added nobody")
	require.Equal(t, 2, provider.employeeCount(), "the look-alike personal email is another employee")
	require.Equal(t, "IT provisioning requested by Dex for a 2026-10-13 start: Platform Engineer, Engineering, Salt Lake City; manager employee 101; first-two-weeks time off: none",
		provider.field(first.EmployeeID, integrationHandoffField))
}

func TestExistingHireIsHandedOffWithStartWindowTimeOffWithRealDex(t *testing.T) {
	provider := newFakeBambooHR(t)
	hire := provider.seedEmployee(completeRecord())
	other := provider.seedEmployee(map[string]string{"firstName": "Sam", "lastName": "Park", "homeEmail": "sam@personal.example.com", "status": "Active"})
	provider.seedTimeOff(hire, "2026-10-20", "2026-10-21", "APPROVED")
	provider.seedTimeOff(hire, "2026-10-26", "2026-10-26", "REQUESTED")
	provider.seedTimeOff(hire, "2026-10-15", "2026-10-15", "DENIED")
	provider.seedTimeOff(hire, "2026-11-20", "2026-11-21", "APPROVED")
	provider.seedTimeOff(other, "2026-10-14", "2026-10-14", "APPROVED")
	harness := newOnboardingHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runOnboarding(t, "existing", integrationInput())
	require.Equal(t, OnboardingHandedOff, outcome.Action)
	require.Equal(t, hire, outcome.EmployeeID)
	require.False(t, outcome.WasEmployeeAdded)
	require.Zero(t, provider.count("add"))
	require.Equal(t, []StartWindowTimeOff{
		{RequestID: 1, StartDate: "2026-10-20", EndDate: "2026-10-21", Status: bamboohr.TimeOffRequestStatusApproved},
		{RequestID: 2, StartDate: "2026-10-26", EndDate: "2026-10-26", Status: bamboohr.TimeOffRequestStatusRequested},
	}, outcome.StartWindowTimeOff, "denied, later, and other employees' time off is excluded")
	require.Equal(t, []string{"startDate le '2026-10-26' and endDate ge '2026-10-13' and employeeId eq " + hire + " and status in ('approved', 'requested')"},
		provider.lastRequest("timeOff").query["filter"])
	expected := "IT provisioning requested by Dex for a 2026-10-13 start: Platform Engineer, Engineering, Salt Lake City; manager employee 101; " +
		"first-two-weeks time off: 2026-10-20 to 2026-10-21 (APPROVED), 2026-10-26 to 2026-10-26 (REQUESTED)"
	require.Equal(t, expected, outcome.HandoffValue)
	require.Equal(t, expected, provider.field(hire, integrationHandoffField))
	require.Equal(t, 1, provider.count("update"))
	require.JSONEq(t, `{"customITProvisioning":`+strconv.Quote(expected)+`}`, provider.lastRequest("update").body, "only the hand-off field is written")
	require.Empty(t, provider.field(other, integrationHandoffField))

	again := harness.runOnboarding(t, "already-handed-off", integrationInput())
	require.Equal(t, OnboardingAlreadyHandedOff, again.Action)
	require.Equal(t, expected, again.HandoffValue)
	require.Equal(t, 1, provider.count("update"), "a recorded hand-off is never overwritten")
}

// TestSlowAddIsSentOnceWithRealDex is the duplicate-dispatch test: sync durability means Dex never
// dispatches a second attempt while the first add is still in flight.
func TestSlowAddIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeBambooHR(t)
	provider.delaysFirstAdd = true
	harness := newOnboardingHarness(t, provider, slowRequestTimeout)

	startedAt := time.Now()
	outcome := harness.runOnboarding(t, "slow-add", integrationInput())
	require.GreaterOrEqual(t, time.Since(startedAt), slowResponseDelay)
	require.Equal(t, OnboardingHeld, outcome.Action)
	require.True(t, outcome.WasEmployeeAdded)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("add"), "no second dispatch while the first was in flight")
	require.Equal(t, 1, provider.employeeCount())
}

// TestSlowUpdateIsSafeToRepeatWithRealDex lets async Dex dispatch the update again; both attempts write the same value.
func TestSlowUpdateIsSafeToRepeatWithRealDex(t *testing.T) {
	provider := newFakeBambooHR(t)
	hire := provider.seedEmployee(completeRecord())
	provider.delaysFirstUpdate = true
	harness := newOnboardingHarness(t, provider, slowRequestTimeout)

	outcome := harness.runOnboarding(t, "slow-update", integrationInput())
	require.Equal(t, OnboardingHandedOff, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("update"), 2, "Dex dispatched the update again past its local phase")
	require.Equal(t, outcome.HandoffValue, provider.field(hire, integrationHandoffField), "the repeated write stored the same text")
	t.Logf("slow update: updates=%d reads=%d", provider.count("update"), provider.count("read"))
}

// TestLostWorkerDuringAddIsReconciledWithoutResendingWithRealDex replaces the Worker while BambooHR
// holds the add; the next attempt finds the dispatch marker, sends nothing, and finds the hire.
func TestLostWorkerDuringAddIsReconciledWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeBambooHR(t)
	provider.holdsFirstAdd = make(chan struct{})
	harness := newOnboardingHarness(t, provider, slowRequestTimeout)
	flowID := harness.startOnboarding(t, "lost-worker", integrationInput())
	require.Eventually(t, func() bool { return provider.count("add") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach BambooHR")
	harness.replaceWorker(t)

	result := harness.waitForFlow(t, flowID)
	close(provider.holdsFirstAdd)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome OnboardingOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, OnboardingHeld, outcome.Action)
	require.True(t, outcome.WasAdditionReconciled, "the new Worker's attempt found the dispatch marker and looked the hire up")
	require.Equal(t, "an earlier attempt of this Step may have sent the request, so it is not sent again", outcome.ReviewDetail)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("add"), "the attempt on the new Worker did not resend the add")
	require.Equal(t, 1, provider.employeeCount())
}

func TestLostAddResponseIsReconciledByPersonalEmailWithRealDex(t *testing.T) {
	provider := newFakeBambooHR(t)
	provider.losesFirstAddResponse = true
	harness := newOnboardingHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runOnboarding(t, "lost-add", integrationInput())
	require.Equal(t, OnboardingHeld, outcome.Action)
	require.True(t, outcome.WasAdditionReconciled)
	require.Equal(t, "BambooHR request failed before a response arrived", outcome.ReviewDetail)
	require.Equal(t, 1, provider.count("add"), "an unconfirmed add is never resent")
	require.Equal(t, 1, provider.employeeCount())
	require.Equal(t, 2, provider.count("find"), "the hire was looked up before and after the add")
}

func TestUnappliedAddWithUnknownOutcomeNeedsReviewWithRealDex(t *testing.T) {
	provider := newFakeBambooHR(t)
	provider.failsFirstAddWithoutApplying = true
	harness := newOnboardingHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runOnboarding(t, "unknown-add", integrationInput())
	require.Equal(t, OnboardingNeedsReview, outcome.Action)
	require.True(t, outcome.NeedsReview)
	require.Equal(t, "additionUncertain", outcome.ReviewReason)
	require.Equal(t, "BambooHR could not complete the request (HTTP 500)", outcome.ReviewDetail)
	require.Equal(t, 1, provider.count("add"), "a 500 might have been applied, so the add is never resent")
	require.Zero(t, provider.employeeCount())
}

func TestRateLimitedAddWaitsAndAddsOneEmployeeWithRealDex(t *testing.T) {
	provider := newFakeBambooHR(t)
	provider.rateLimitsFirstAdd = true
	harness := newOnboardingHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runOnboarding(t, "rate-limited", integrationInput())
	require.Equal(t, OnboardingHeld, outcome.Action, "a rate-limited 429 clears the dispatch marker, so the retry may send")
	require.False(t, outcome.WasAdditionReconciled)
	times := provider.requestTimes("add")
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), time.Second, "the retry waited for Retry-After")
	require.Equal(t, 1, provider.employeeCount())
}

func TestRejectedAddFailsTheFlowWithoutBambooHRTextWithRealDex(t *testing.T) {
	provider := newFakeBambooHR(t)
	provider.rejectsAdd = true
	harness := newOnboardingHarness(t, provider, defaultRequestTimeout)
	flowID := harness.startOnboarding(t, "rejected", integrationInput())

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	require.NotContains(t, result.ErrorMessage, integrationAPIKey)
	require.Equal(t, 1, provider.count("add"), "a conclusive rejection is not retried")
	require.Zero(t, provider.employeeCount())
	t.Logf("rejected add failure: %s", result.ErrorMessage)
}

func TestSharedPersonalEmailNeedsReviewAndWritesNothingWithRealDex(t *testing.T) {
	provider := newFakeBambooHR(t)
	provider.seedEmployee(completeRecord())
	provider.seedEmployee(map[string]string{"firstName": "Ava", "lastName": "Ng", "homeEmail": "AVA.NGUYEN@personal.example.com", "status": "Inactive"})
	harness := newOnboardingHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runOnboarding(t, "ambiguous", integrationInput())
	require.Equal(t, OnboardingNeedsReview, outcome.Action)
	require.Equal(t, "ambiguousHire", outcome.ReviewReason)
	require.Equal(t, "2 employees contain this personal email; exact matches: [1, 2]", outcome.ReviewDetail)
	require.Zero(t, provider.count("add"))
	require.Zero(t, provider.count("update"))
}

func TestUnknownHandoffFieldNeedsReviewWithRealDex(t *testing.T) {
	provider := newFakeBambooHR(t)
	provider.seedEmployee(completeRecord())
	harness := newOnboardingHarness(t, provider, defaultRequestTimeout)
	input := integrationInput()
	input.HandoffFieldName = "customMissingField"

	outcome := harness.runOnboarding(t, "unknown-field", input)
	require.Equal(t, OnboardingNeedsReview, outcome.Action)
	require.Equal(t, "handoffFieldUnavailable", outcome.ReviewReason)
	require.Zero(t, provider.count("update"))
}

func TestInvalidHireFailsBeforeCallingBambooHRWithRealDex(t *testing.T) {
	provider := newFakeBambooHR(t)
	harness := newOnboardingHarness(t, provider, defaultRequestTimeout)
	input := integrationInput()
	input.HireDate = "10/13/2026"
	flowID := harness.startOnboarding(t, "invalid", input)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.totalRequests())
}

// accountFields are the fields this fake account defines; others are omitted like an unknown field.
var accountFields = []string{
	"firstName", "lastName", "preferredName", "homeEmail", "workEmail", "hireDate", "department", "jobTitle", "location",
	"supervisorEId", "status", integrationHandoffField,
}

// fakeBambooHR is a stateful BambooHR API v1 fake without idempotency keys.
type fakeBambooHR struct {
	*httptest.Server
	t     *testing.T
	mutex sync.Mutex
	// delayedRequests tracks handlers still sleeping, so a test can assert their final effect.
	delayedRequests sync.WaitGroup

	nextEmployeeID int
	employees      map[string]map[string]string
	timeOff        []fakeTimeOff
	counts         map[string]int
	requests       map[string][]fakeRecordedRequest

	delaysFirstAdd               bool
	losesFirstAddResponse        bool
	failsFirstAddWithoutApplying bool
	rateLimitsFirstAdd           bool
	rejectsAdd                   bool
	delaysFirstUpdate            bool
	holdsFirstAdd                chan struct{}
}

type fakeTimeOff struct {
	id         int64
	employeeID string
	startDate  string
	endDate    string
	status     string
}

type fakeRecordedRequest struct {
	at    time.Time
	query map[string][]string
	body  string
}

func newFakeBambooHR(t *testing.T) *fakeBambooHR {
	t.Helper()
	provider := &fakeBambooHR{
		t: t, nextEmployeeID: 1, employees: map[string]map[string]string{}, counts: map[string]int{}, requests: map[string][]fakeRecordedRequest{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeBambooHR) serveHTTP(response http.ResponseWriter, request *http.Request) {
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(integrationAPIKey+":x"))
	if request.Header.Get("Authorization") != expected {
		response.Header().Set("WWW-Authenticate", `Basic realm="BambooHR"`)
		response.WriteHeader(http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/api/v1")
	switch {
	case request.Method == http.MethodGet && path == "/employees":
		provider.listEmployees(response, request, body)
	case request.Method == http.MethodPost && path == "/employees":
		provider.addEmployee(response, request, body)
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/employees/"):
		provider.getEmployee(response, request, strings.TrimPrefix(path, "/employees/"), body)
	case request.Method == http.MethodPost && strings.HasPrefix(path, "/employees/"):
		provider.updateEmployee(response, request, strings.TrimPrefix(path, "/employees/"), body)
	case request.Method == http.MethodGet && path == "/time-off/requests":
		provider.listTimeOff(response, request, body)
	default:
		response.WriteHeader(http.StatusNotFound)
	}
}

func (provider *fakeBambooHR) record(kind string, request *http.Request, body []byte) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[kind]++
	provider.requests[kind] = append(provider.requests[kind], fakeRecordedRequest{at: time.Now(), query: request.URL.Query(), body: string(body)})
	return provider.counts[kind]
}

func (provider *fakeBambooHR) listEmployees(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("find", request, body)
	query := request.URL.Query()
	emailField, email := "workEmail", query.Get("filter[workEmail]")
	if value := query.Get("filter[homeEmail]"); value != "" {
		emailField, email = "homeEmail", value
	}
	requested := strings.Split(query.Get("fields"), ",")
	provider.mutex.Lock()
	var data []map[string]any
	for _, employeeID := range provider.sortedEmployeeIDs() {
		fields := provider.employees[employeeID]
		if !strings.Contains(strings.ToLower(fields[emailField]), strings.ToLower(email)) {
			continue
		}
		entry := map[string]any{
			"employeeId": employeeID, "firstName": fields["firstName"], "lastName": fields["lastName"], "preferredName": nil,
			"photoUrl": nil, "jobTitleName": fields["jobTitle"], "status": fields["status"], "_restrictedFields": []string{},
		}
		for _, field := range requested {
			entry[field] = fields[field]
		}
		data = append(data, entry)
	}
	provider.mutex.Unlock()
	if data == nil {
		data = []map[string]any{}
	}
	provider.writeValue(response, http.StatusOK, map[string]any{
		"data": data, "meta": map[string]any{"total": len(data), "page": map[string]any{"limit": 100, "nextCursor": nil, "prevCursor": nil}},
		"_links": map[string]any{"self": map[string]any{"href": provider.URL + request.URL.RequestURI()}},
	})
}

func (provider *fakeBambooHR) addEmployee(response http.ResponseWriter, request *http.Request, body []byte) {
	attempt := provider.record("add", request, body)
	var fields map[string]string
	if json.Unmarshal(body, &fields) != nil || fields["firstName"] == "" || fields["lastName"] == "" {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	switch {
	case provider.rejectsAdd:
		response.Header().Set("X-BambooHR-Error-Message", "SENTINEL duplicate email")
		response.WriteHeader(http.StatusConflict)
		return
	case provider.rateLimitsFirstAdd && attempt == 1:
		response.Header().Set("Retry-After", "1")
		response.WriteHeader(http.StatusTooManyRequests)
		return
	case provider.failsFirstAddWithoutApplying && attempt == 1:
		response.Header().Set("X-BambooHR-Error-Message", "SENTINEL internal error")
		response.WriteHeader(http.StatusInternalServerError)
		return
	}
	employeeID := provider.applyAdd(fields)
	switch {
	case provider.holdsFirstAdd != nil && attempt == 1:
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		<-provider.holdsFirstAdd
	case provider.delaysFirstAdd && attempt == 1:
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	case provider.losesFirstAddResponse && attempt == 1:
		hijacker, isHijacker := response.(http.Hijacker)
		require.True(provider.t, isHijacker)
		connection, _, err := hijacker.Hijack()
		require.NoError(provider.t, err)
		require.NoError(provider.t, connection.Close())
		return
	}
	response.Header().Set("Location", provider.URL+"/employees/employee.php?id="+employeeID)
	provider.writeValue(response, http.StatusCreated, map[string]any{"id": employeeID, "firstName": fields["firstName"], "lastName": fields["lastName"]})
}

func (provider *fakeBambooHR) applyAdd(fields map[string]string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	employeeID := strconv.Itoa(provider.nextEmployeeID)
	provider.nextEmployeeID++
	stored := map[string]string{"status": "Active"}
	for field, value := range fields {
		stored[field] = value
	}
	provider.employees[employeeID] = stored
	return employeeID
}

func (provider *fakeBambooHR) getEmployee(response http.ResponseWriter, request *http.Request, employeeID string, body []byte) {
	provider.record("read", request, body)
	provider.mutex.Lock()
	fields, isFound := provider.employees[employeeID]
	value := map[string]any{"id": employeeID}
	for _, field := range strings.Split(request.URL.Query().Get("fields"), ",") {
		if isFound && containsString(accountFields, field) {
			value[field] = fields[field]
		}
	}
	provider.mutex.Unlock()
	if !isFound {
		response.WriteHeader(http.StatusNotFound)
		return
	}
	provider.writeValue(response, http.StatusOK, value)
}

func (provider *fakeBambooHR) updateEmployee(response http.ResponseWriter, request *http.Request, employeeID string, body []byte) {
	attempt := provider.record("update", request, body)
	var fields map[string]string
	if json.Unmarshal(body, &fields) != nil {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	if provider.delaysFirstUpdate && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	provider.mutex.Lock()
	stored, isFound := provider.employees[employeeID]
	for field, value := range fields {
		if isFound {
			stored[field] = value
		}
	}
	provider.mutex.Unlock()
	if !isFound {
		response.WriteHeader(http.StatusNotFound)
		return
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"id": employeeID})
}

func (provider *fakeBambooHR) listTimeOff(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("timeOff", request, body)
	filter := request.URL.Query().Get("filter")
	windowEnd, windowStart := filterStartPattern.FindStringSubmatch(filter), filterEndPattern.FindStringSubmatch(filter)
	if windowEnd == nil || windowStart == nil {
		provider.writeValue(response, http.StatusUnprocessableEntity, map[string]any{"code": "invalid_filter"})
		return
	}
	employee := filterEmployeePattern.FindStringSubmatch(filter)
	var statuses []string
	if match := filterStatusesPattern.FindStringSubmatch(filter); match != nil {
		for _, status := range strings.Split(match[1]+match[2], ",") {
			statuses = append(statuses, strings.ToUpper(strings.Trim(strings.TrimSpace(status), "'")))
		}
	}
	provider.mutex.Lock()
	data := []map[string]any{}
	for _, entry := range provider.timeOff {
		switch {
		case entry.startDate > windowEnd[1] || entry.endDate < windowStart[1]:
		case employee != nil && entry.employeeID != employee[1]:
		case statuses != nil && !containsString(statuses, entry.status):
		default:
			employeeID, _ := strconv.ParseInt(entry.employeeID, 10, 64)
			data = append(data, map[string]any{
				"id": entry.id, "employeeId": employeeID, "categoryId": 78, "startDate": entry.startDate, "endDate": entry.endDate,
				"amount": 1, "unit": "DAYS", "status": entry.status, "requestedAt": "2026-09-02T15:04:05Z",
				"employeeNote": "SENTINEL note", "managerNote": nil, "statusUpdatedAt": nil, "statusUpdatedBy": nil,
			})
		}
	}
	provider.mutex.Unlock()
	provider.writeValue(response, http.StatusOK, map[string]any{
		"data": data, "meta": map[string]any{"page": 1, "pageSize": 50, "totalPages": 1, "totalItems": len(data)}, "_links": map[string]any{},
	})
}

func (provider *fakeBambooHR) writeValue(response http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	require.NoError(provider.t, err)
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, err = response.Write(encoded)
	require.NoError(provider.t, err)
}

func (provider *fakeBambooHR) seedEmployee(fields map[string]string) string {
	return provider.applyAdd(fields)
}

func (provider *fakeBambooHR) seedTimeOff(employeeID string, startDate string, endDate string, status string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.timeOff = append(provider.timeOff, fakeTimeOff{
		id: int64(len(provider.timeOff) + 1), employeeID: employeeID, startDate: startDate, endDate: endDate, status: status,
	})
}

func (provider *fakeBambooHR) setFields(employeeID string, fields map[string]string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	for field, value := range fields {
		provider.employees[employeeID][field] = value
	}
}

func (provider *fakeBambooHR) field(employeeID string, field string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.employees[employeeID][field]
}

func (provider *fakeBambooHR) employeeCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.employees)
}

func (provider *fakeBambooHR) sortedEmployeeIDs() []string {
	employeeIDs := make([]string, 0, len(provider.employees))
	for employeeID := range provider.employees {
		employeeIDs = append(employeeIDs, employeeID)
	}
	sort.Slice(employeeIDs, func(left int, right int) bool {
		leftID, _ := strconv.Atoi(employeeIDs[left])
		rightID, _ := strconv.Atoi(employeeIDs[right])
		return leftID < rightID
	})
	return employeeIDs
}

func (provider *fakeBambooHR) count(kind string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[kind]
}

func (provider *fakeBambooHR) totalRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, count := range provider.counts {
		total += count
	}
	return total
}

func (provider *fakeBambooHR) lastRequest(kind string) fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	requests := provider.requests[kind]
	require.NotEmpty(provider.t, requests, "no %s request", kind)
	return requests[len(requests)-1]
}

func (provider *fakeBambooHR) requestTimes(kind string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var times []time.Time
	for _, request := range provider.requests[kind] {
		times = append(times, request.at)
	}
	return times
}

func (provider *fakeBambooHR) waitForDelayedRequests(t *testing.T) {
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

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

type onboardingHarness struct {
	flow          *Flow
	serverAddress string
	workerAddress string
	registry      *dex.Registry
	cache         *blobcache.Cache
	client        *dex.Client
	worker        *dex.Worker
	workerResult  chan error
}

func newOnboardingHarness(t *testing.T, provider *fakeBambooHR, requestTimeout time.Duration) *onboardingHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "bamboohr", Name: ConnectionName}
	providerClient, err := bamboohr.New(bamboohr.Config{CompanyDomain: integrationCompanyDomain},
		sdkgo.StaticCredentialProvider[bamboohr.Credentials]{reference: {APIKey: sdkgo.NewSecretString(integrationAPIKey)}},
		bamboohr.WithAPIBaseURL(provider.URL+"/api/v1"), bamboohr.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := bamboohr.NewConnection(providerClient, reference)
	require.NoError(t, err)
	harness := &onboardingHarness{flow: NewFlow(connection), serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
	harness.registry, err = dex.NewRegistry([]dex.Flow{harness.flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.workerAddress = net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.startWorker(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return harness
}

func (harness *onboardingHarness) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(harness.registry, harness.cache, dex.WorkerOptions{
		BindAddress: harness.workerAddress, FlowServiceAddress: harness.serverAddress, WorkerTarget: dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	harness.worker, harness.workerResult = worker, workerResult
}

// replaceWorker force-stops the Worker without draining its handlers, like a crash, then starts a new one.
func (harness *onboardingHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *onboardingHarness) runOnboarding(t *testing.T, scenario string, input Input) OnboardingOutcome {
	t.Helper()
	flowID := harness.startOnboarding(t, scenario, input)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome OnboardingOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	encoded, err := json.Marshal(outcome)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL", "time off notes and BambooHR message text never reach Flow state")
	return outcome
}

func (harness *onboardingHarness) startOnboarding(t *testing.T, scenario string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := fmt.Sprintf("bamboohr-onboarding-%s-%d", scenario, time.Now().UnixNano())
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *onboardingHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
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
