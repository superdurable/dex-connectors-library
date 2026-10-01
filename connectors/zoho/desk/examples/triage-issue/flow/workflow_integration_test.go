//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageissue

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationOrganizationID = "2389290"
	integrationAccessToken    = "1000.zohoDeskIntegrationAccess0123456789"
	integrationRefreshedToken = "1000.zohoDeskIntegrationRefreshed0123456789"
	integrationRefreshToken   = "1000.zohoDeskIntegrationRefresh0123456789"
	integrationContactEmail   = "jane@acme.example.com"
	integrationDepartmentID   = "1892000000006907"
	otherDepartmentID         = "1892000000082069"
	agentID                   = "1892000000056007"

	defaultRequestTimeout = 5 * time.Second
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowResponseDelay  = 9 * time.Second
	slowRequestTimeout = 20 * time.Second
)

func integrationIssueInput() Input {
	return Input{
		ContactEmail: integrationContactEmail, ContactFirstName: "Jane", ContactLastName: "Smith", Subject: "Double charge on order 88213",
		Message: "I was charged twice for order 88213.", DepartmentID: integrationDepartmentID, Priority: desk.TicketPriorityHigh,
	}
}

func TestNewIssueOpensOneTicketAndAddsOnePrivateCommentWithRealDex(t *testing.T) {
	provider := newFakeZohoDesk(t)
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "new-issue", integrationIssueInput())
	require.Equal(t, TriageTicketOpened, outcome.Action)
	require.False(t, outcome.NeedsReview)
	require.Equal(t, desk.TicketPriorityHigh, outcome.Ticket.Priority)
	require.Equal(t, desk.TicketStatusOpen, outcome.Ticket.Status)
	require.Equal(t, 1, provider.ticketCount())
	require.Equal(t, 1, provider.count("create"))
	require.Equal(t, "${OPEN},${ONHOLD}", provider.lastRequest("search").query.Get("status"))
	require.Equal(t, integrationContactEmail, provider.lastRequest("search").query.Get("email"))
	require.Equal(t, integrationDepartmentID, provider.lastRequest("search").query.Get("departmentId"))
	require.JSONEq(t, `{"subject":"Double charge on order 88213","description":"I was charged twice for order 88213.","departmentId":"`+integrationDepartmentID+`",
		"contact":{"email":"jane@acme.example.com","firstName":"Jane","lastName":"Smith"},"email":"jane@acme.example.com","status":"Open","priority":"High"}`,
		provider.lastRequest("create").body)
	ticket := provider.ticket(outcome.Ticket.ID)
	require.Len(t, ticket.comments, 1, "exactly one triage comment")
	require.False(t, ticket.comments[0].isPublic)
	require.Equal(t, outcome.CommentID, ticket.comments[0].id)
	require.Equal(t, BuildOpenedTicketComment(mustCustomerIssue(t, integrationIssueInput())), ticket.comments[0].content)
}

func TestRepeatContactReprioritizesTheContactsTicketAndLeavesDecoysUntouchedWithRealDex(t *testing.T) {
	provider := newFakeZohoDesk(t)
	existing := provider.seedTicket(integrationContactEmail, integrationDepartmentID, "On Hold", "Low", "2026-01-26T14:02:00Z")
	closed := provider.seedTicket(integrationContactEmail, integrationDepartmentID, "Closed", "Medium", "2026-01-27T09:00:00Z")
	provider.seedComment(closed, "Refund already issued on Jan 13, see REF-771.")
	otherContact := provider.seedTicket("ben@meridian.example.com", integrationDepartmentID, "Open", "Low", "2026-01-27T11:00:00Z")
	lookalike := provider.seedTicket("jane@acme.example.com.au", integrationDepartmentID, "Open", "Low", "2026-01-28T11:00:00Z")
	otherDepartment := provider.seedTicket(integrationContactEmail, otherDepartmentID, "Open", "Low", "2026-01-28T12:00:00Z")
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "repeat-contact", integrationIssueInput())
	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	require.Equal(t, existing, outcome.Ticket.ID, "the look-alike address's newer ticket was dropped from the page")
	require.Equal(t, desk.TicketStatusOpen, outcome.Ticket.Status, "an on-hold ticket is reopened")
	require.Equal(t, desk.TicketPriorityHigh, outcome.Ticket.Priority)
	require.False(t, outcome.WasAlreadyApplied)
	require.JSONEq(t, `{"status":"Open","priority":"High"}`, provider.lastRequest("update").body)

	ticket := provider.ticket(existing)
	require.Len(t, ticket.comments, 1, "exactly one triage comment")
	require.False(t, ticket.comments[0].isPublic)
	require.Equal(t, BuildFollowUpComment(mustCustomerIssue(t, integrationIssueInput())), ticket.comments[0].content)
	for _, decoy := range []string{closed, otherContact, lookalike, otherDepartment} {
		snapshot := provider.ticket(decoy)
		require.Equal(t, snapshot.seededAt, snapshot.modifiedAt, "ticket %s must not be touched", decoy)
	}
	require.Len(t, provider.ticket(closed).comments, 1)
	require.Equal(t, 5, provider.ticketCount(), "no ticket was created")
	require.Equal(t, 1, provider.count("update"))
}

func TestStaleSearchMatchIsReadSkippedAndNeverWrittenWithRealDex(t *testing.T) {
	provider := newFakeZohoDesk(t)
	stale := provider.seedTicket(integrationContactEmail, integrationDepartmentID, "Closed", "Low", "2026-01-27T11:00:00Z")
	provider.setSearchIndexStatusType(stale, desk.TicketStatusTypeOpen)
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "stale-search", integrationIssueInput())
	require.Equal(t, TriageTicketOpened, outcome.Action)
	require.Equal(t, stale, outcome.SkippedTicketID, "the read shows the search index was stale")
	require.NotEqual(t, stale, outcome.Ticket.ID)
	require.Zero(t, provider.count("update"))
	require.Empty(t, provider.ticket(stale).comments)
}

func TestSecondSearchPageIsReadWhenTheFirstHoldsNoMatchWithRealDex(t *testing.T) {
	provider := newFakeZohoDesk(t)
	for index := 0; index < SearchPageLimit; index++ {
		provider.seedTicket("jane@acme.example.com.au", integrationDepartmentID, "Open", "Low", "2026-01-28T09:"+twoDigits(index)+":00Z")
	}
	existing := provider.seedTicket(integrationContactEmail, integrationDepartmentID, "Open", "Low", "2026-01-26T14:02:00Z")
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "second-page", integrationIssueInput())
	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	require.Equal(t, existing, outcome.Ticket.ID)
	require.Equal(t, []string{"0", "25"}, provider.queryValues("search", "from"))
}

// TestSlowCreateIsSentOnceWithRealDex is the duplicate-dispatch test: sync durability means
// Dex never dispatches a second attempt while the first create is still in flight.
func TestSlowCreateIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeZohoDesk(t)
	provider.delaysFirstCreate = true
	harness := newTriageHarness(t, provider, slowRequestTimeout)

	startedAt := time.Now()
	outcome := harness.runTriage(t, "slow-create", integrationIssueInput())
	require.GreaterOrEqual(t, time.Since(startedAt), slowResponseDelay)
	require.Equal(t, TriageTicketOpened, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("create"), "no second dispatch while the first was in flight")
	require.Equal(t, 1, provider.ticketCount())
}

func TestSlowCommentIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeZohoDesk(t)
	provider.delaysFirstComment = true
	harness := newTriageHarness(t, provider, slowRequestTimeout)

	outcome := harness.runTriage(t, "slow-comment", integrationIssueInput())
	require.Equal(t, TriageTicketOpened, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("comment"), "no second dispatch while the first was in flight")
	require.Len(t, provider.ticket(outcome.Ticket.ID).comments, 1)
}

// TestSlowUpdateIsSafeToRepeatWithRealDex lets async Dex dispatch the update again; both attempts write the same values.
func TestSlowUpdateIsSafeToRepeatWithRealDex(t *testing.T) {
	provider := newFakeZohoDesk(t)
	existing := provider.seedTicket(integrationContactEmail, integrationDepartmentID, "On Hold", "Low", "2026-01-26T14:02:00Z")
	provider.delaysFirstUpdate = true
	harness := newTriageHarness(t, provider, slowRequestTimeout)

	outcome := harness.runTriage(t, "slow-update", integrationIssueInput())
	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("update"), 2, "Dex dispatched the update again past its local phase")
	ticket := provider.ticket(existing)
	require.Equal(t, "Open", ticket.status)
	require.Equal(t, "High", ticket.priority)
	require.Len(t, ticket.comments, 1, "exactly one triage comment")
	for _, request := range provider.requestsNamed("update") {
		require.JSONEq(t, `{"status":"Open","priority":"High"}`, request.body, "every dispatch writes the same absolute values")
	}
	t.Logf("slow update: updates=%d reads=%d", provider.count("update"), provider.count("read"))
}

func TestLostUpdateResponseIsRetriedAndWritesNothingTwiceWithRealDex(t *testing.T) {
	provider := newFakeZohoDesk(t)
	existing := provider.seedTicket(integrationContactEmail, integrationDepartmentID, "Open", "Low", "2026-01-26T14:02:00Z")
	provider.losesFirstUpdateResponse = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "lost-update", integrationIssueInput())
	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	require.True(t, outcome.WasAlreadyApplied, "the retried attempt found the values applied")
	require.Equal(t, 1, provider.count("update"), "the retry wrote nothing")
	require.Len(t, provider.ticket(existing).comments, 1)
}

func TestLostCreateResponseSelectsUncertainAndIsNeverResentWithRealDex(t *testing.T) {
	provider := newFakeZohoDesk(t)
	provider.losesFirstCreateResponse = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "lost-create", integrationIssueInput())
	require.Equal(t, TriageTicketCreationUncertain, outcome.Action)
	require.True(t, outcome.NeedsReview)
	require.Equal(t, "createTicket", outcome.ReviewReason)
	require.Equal(t, "Zoho Desk request failed before a response arrived", outcome.ReviewDetail)
	require.Equal(t, 1, provider.count("create"), "an unconfirmed create is never resent")
	require.Equal(t, 1, provider.ticketCount(), "Zoho Desk did create the ticket, which a person must now find")
	require.Zero(t, provider.count("comment"))
}

func TestRateLimitedCreateWaitsAndCreatesOneTicketWithRealDex(t *testing.T) {
	provider := newFakeZohoDesk(t)
	provider.rateLimitsFirstCreate = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "rate-limited", integrationIssueInput())
	require.Equal(t, TriageTicketOpened, outcome.Action, "a 429 clears the dispatch marker, so the retry may send")
	require.False(t, outcome.NeedsReview)
	times := provider.requestTimes("create")
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), time.Second, "the retry waited for Retry-After")
	require.Equal(t, 1, provider.ticketCount())
}

// TestLostWorkerDuringCreateSelectsUncertainWithoutResendingWithRealDex replaces the Worker while
// Zoho Desk holds the create; the next attempt finds the dispatch marker and sends nothing.
func TestLostWorkerDuringCreateSelectsUncertainWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeZohoDesk(t)
	provider.holdsFirstCreate = make(chan struct{})
	harness := newTriageHarness(t, provider, slowRequestTimeout)
	flowID := harness.startTriage(t, "lost-worker", integrationIssueInput())
	require.Eventually(t, func() bool { return provider.count("create") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach Zoho Desk")
	harness.replaceWorker(t)

	result := harness.waitForFlow(t, flowID)
	close(provider.holdsFirstCreate)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome TriageOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, TriageTicketCreationUncertain, outcome.Action)
	require.True(t, outcome.NeedsReview)
	require.Equal(t, "an earlier attempt of this Step may have sent the request, so it is not sent again", outcome.ReviewDetail,
		"the new Worker's attempt found the dispatch marker the lost attempt recorded")
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("create"), "the attempt on the new Worker did not resend the create")
}

func TestLostCommentResponseSelectsUncertainAndIsNeverResentWithRealDex(t *testing.T) {
	provider := newFakeZohoDesk(t)
	provider.losesFirstCommentResponse = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "lost-comment", integrationIssueInput())
	require.Equal(t, TriageTicketOpened, outcome.Action)
	require.True(t, outcome.NeedsReview)
	require.Equal(t, "addComment", outcome.ReviewReason)
	require.Empty(t, outcome.CommentID)
	require.Equal(t, 1, provider.count("comment"), "an unconfirmed comment is never resent")
	require.Len(t, provider.ticket(outcome.Ticket.ID).comments, 1)
}

// TestRevokedAccessTokenIsRefreshedOnceAndTheCreateIsSentOnceWithRealDex runs the local refreshing
// credential provider: Zoho Desk rejects the stored token at the create, the EU Accounts server
// issues a new one, and the rejected create, which Zoho Desk never applied, is sent again with it.
func TestRevokedAccessTokenIsRefreshedOnceAndTheCreateIsSentOnceWithRealDex(t *testing.T) {
	const storedAccessToken = "1000.zohoDeskIntegrationStored0123456789"
	provider := newFakeZohoDesk(t)
	provider.acceptedAccessToken, provider.revokesStoredTokenAtFirstCreate = storedAccessToken, true
	connectionsPath := writeLocalConnection(t, storedAccessToken, time.Now().Add(30*time.Minute))
	store, err := localconfig.LoadFile(connectionsPath)
	require.NoError(t, err)
	connection, err := desk.NewLocalConnection(store, ConnectionName,
		desk.WithAPIBaseURL(provider.URL+"/api/v1"), desk.WithHTTPClient(provider.accountsRoutingClient(t, defaultRequestTimeout)))
	require.NoError(t, err)
	harness := newTriageHarnessForConnection(t, connection)

	outcome := harness.runTriage(t, "refresh", integrationIssueInput())
	require.Equal(t, TriageTicketOpened, outcome.Action)
	require.Equal(t, 1, provider.count("token"), "one forced refresh at https://accounts.zoho.eu")
	require.Equal(t, 2, provider.count("create"), "the 401 create applied nothing, so it is sent once more")
	require.Equal(t, 1, provider.ticketCount())
	require.Equal(t, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {integrationRefreshToken},
		"client_id": {"1000.ZOHODESKINTEGRATIONCLIENT"}, "client_secret": {"zoho-integration-secret"},
	}, provider.lastRequest("token").form)

	contents, err := os.ReadFile(connectionsPath)
	require.NoError(t, err)
	var file struct {
		Connections []struct {
			AuthMethodID string            `json:"authMethodId"`
			Credentials  map[string]string `json:"credentials"`
		} `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(contents, &file))
	require.Equal(t, desk.EUDataCenterAuthMethodID, file.Connections[0].AuthMethodID, "Dex Web's record member survives the refresh")
	require.Equal(t, integrationRefreshedToken, file.Connections[0].Credentials["access_token"])
	require.Equal(t, integrationRefreshToken, file.Connections[0].Credentials["refresh_token"], "Zoho does not rotate refresh tokens")
}

func TestRejectedTicketFailsTheFlowWithoutZohoDeskTextWithRealDex(t *testing.T) {
	provider := newFakeZohoDesk(t)
	provider.rejectsCreate = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)
	flowID := harness.startTriage(t, "rejected", integrationIssueInput())

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	require.NotContains(t, result.ErrorMessage, integrationAccessToken)
	require.Equal(t, 1, provider.count("create"), "a conclusive rejection is not retried")
	require.Zero(t, provider.ticketCount())
	t.Logf("rejected create failure: %s", result.ErrorMessage)
}

func TestInvalidIssueFailsBeforeCallingZohoDeskWithRealDex(t *testing.T) {
	provider := newFakeZohoDesk(t)
	harness := newTriageHarness(t, provider, defaultRequestTimeout)
	input := integrationIssueInput()
	input.DepartmentID = "Billing"
	flowID := harness.startTriage(t, "invalid", input)

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.totalRequests())
}

func mustCustomerIssue(t *testing.T, input Input) CustomerIssue {
	t.Helper()
	issue, err := BuildCustomerIssue(input)
	require.NoError(t, err)
	return issue
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}

// writeLocalConnection writes the record Dex Web saves for an EU data center connection.
func writeLocalConnection(t *testing.T, accessToken string, expiresAt time.Time) string {
	t.Helper()
	record := map[string]any{
		"schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
		"connections": []any{map[string]any{
			"connectorId": desk.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/zoho/desk",
			"moduleVersion": "v0.1.0", "provider": "zoho", "connectionName": ConnectionName, "authMethodId": desk.EUDataCenterAuthMethodID,
			"configuration": map[string]any{"orgId": integrationOrganizationID},
			"credentials": map[string]any{
				"auth_method": desk.EUDataCenterAuthMethodID, "oauth_client_id": "1000.ZOHODESKINTEGRATIONCLIENT",
				"oauth_client_secret": "zoho-integration-secret", "access_token": accessToken, "refresh_token": integrationRefreshToken,
			},
			"credentialExpiresAt": expiresAt.UTC().Format(time.RFC3339),
		}},
	}
	encoded, err := json.Marshal(record)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(path, encoded, 0o600))
	return path
}

// fakeZohoDesk is a stateful Zoho Desk API fake without idempotency keys, whose search index can lag.
type fakeZohoDesk struct {
	*httptest.Server
	t     *testing.T
	mutex sync.Mutex
	// delayedRequests tracks handlers still sleeping, so a test can assert their final effect.
	delayedRequests sync.WaitGroup

	acceptedAccessToken string
	// revokesStoredTokenAtFirstCreate makes Zoho Desk reject the stored token from the first create on.
	revokesStoredTokenAtFirstCreate bool
	clock                           time.Time
	nextID                          int64
	tickets                         map[string]*fakeTicket
	counts                          map[string]int
	requests                        map[string][]fakeRecordedRequest

	delaysFirstCreate         bool
	delaysFirstComment        bool
	delaysFirstUpdate         bool
	losesFirstCreateResponse  bool
	losesFirstUpdateResponse  bool
	losesFirstCommentResponse bool
	rateLimitsFirstCreate     bool
	rejectsCreate             bool
	holdsFirstCreate          chan struct{}
}

type fakeTicket struct {
	id                    string
	subject               string
	descriptionHTML       string
	status                string
	priority              string
	departmentID          string
	contactID             string
	email                 string
	assigneeID            string
	searchIndexStatusType desk.TicketStatusType
	createdAt             time.Time
	seededAt              time.Time
	modifiedAt            time.Time
	comments              []fakeComment
}

type fakeComment struct {
	id          string
	isPublic    bool
	content     string
	commentedAt time.Time
}

type fakeRecordedRequest struct {
	at    time.Time
	query url.Values
	form  url.Values
	body  string
}

func newFakeZohoDesk(t *testing.T) *fakeZohoDesk {
	t.Helper()
	provider := &fakeZohoDesk{
		t: t, acceptedAccessToken: integrationAccessToken, clock: time.Date(2026, 1, 28, 13, 0, 0, 0, time.UTC), nextID: 1892000000100000,
		tickets: map[string]*fakeTicket{}, counts: map[string]int{}, requests: map[string][]fakeRecordedRequest{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeZohoDesk) serveHTTP(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		provider.writeJSON(response, http.StatusBadRequest, `{"errorCode":"INVALID_DATA"}`)
		return
	}
	if request.URL.Path == "/oauth/v2/token" {
		provider.refreshToken(response, request, body)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/api/v1")
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	provider.mutex.Lock()
	if provider.revokesStoredTokenAtFirstCreate && request.Method == http.MethodPost && path == "/tickets" {
		provider.acceptedAccessToken, provider.revokesStoredTokenAtFirstCreate = integrationRefreshedToken, false
	}
	acceptedAccessToken := provider.acceptedAccessToken
	provider.mutex.Unlock()
	if request.Header.Get("Authorization") != "Zoho-oauthtoken "+acceptedAccessToken {
		if request.Method == http.MethodPost && path == "/tickets" {
			provider.record("create", request, body)
		}
		provider.writeJSON(response, http.StatusUnauthorized, `{"errorCode":"INVALID_OAUTH","message":"SENTINEL The OAuth Token you provided is invalid."}`)
		return
	}
	if request.Header.Get("orgId") != integrationOrganizationID {
		provider.writeJSON(response, http.StatusForbidden, `{"errorCode":"OAUTH_ORG_MISMATCH","message":"SENTINEL"}`)
		return
	}
	switch {
	case request.Method == http.MethodGet && path == "/tickets/search":
		provider.searchTickets(response, request, body)
	case request.Method == http.MethodPost && path == "/tickets":
		provider.createTicket(response, request, body)
	case len(segments) >= 2 && segments[0] == "tickets":
		ticketID := segments[1]
		switch {
		case request.Method == http.MethodGet && len(segments) == 2:
			provider.readTicket(response, request, body, ticketID)
		case request.Method == http.MethodGet && len(segments) == 3 && segments[2] == "threads":
			provider.listThreads(response, request, body, ticketID)
		case request.Method == http.MethodGet && len(segments) == 3 && segments[2] == "comments":
			provider.listComments(response, request, body, ticketID)
		case request.Method == http.MethodPatch && len(segments) == 2:
			provider.updateTicket(response, request, body, ticketID)
		case request.Method == http.MethodPost && len(segments) == 3 && segments[2] == "comments":
			provider.addComment(response, request, body, ticketID)
		default:
			provider.writeJSON(response, http.StatusNotFound, `{"errorCode":"URL_NOT_FOUND"}`)
		}
	default:
		provider.writeJSON(response, http.StatusNotFound, `{"errorCode":"URL_NOT_FOUND"}`)
	}
}

// refreshToken answers like Zoho Accounts: HTTP 200 with a one-hour token and no refresh token.
func (provider *fakeZohoDesk) refreshToken(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("token", request, body)
	form, err := url.ParseQuery(string(body))
	if err != nil || form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != integrationRefreshToken {
		provider.writeJSON(response, http.StatusOK, `{"error":"invalid_code"}`)
		return
	}
	provider.writeValue(response, http.StatusOK, map[string]any{
		"access_token": integrationRefreshedToken, "api_domain": "https://www.zohoapis.eu", "token_type": "Bearer", "expires_in": 3600,
	})
}

// searchTickets matches email by substring, as Zoho Desk's email search also matches look-alike addresses.
func (provider *fakeZohoDesk) searchTickets(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("search", request, body)
	query := request.URL.Query()
	from, fromErr := strconv.Atoi(query.Get("from"))
	limit, limitErr := strconv.Atoi(query.Get("limit"))
	if fromErr != nil || limitErr != nil || from < 0 || limit < 1 || limit > 100 || query.Get("sortBy") != "-modifiedTime" {
		provider.writeJSON(response, http.StatusUnprocessableEntity, `{"errorCode":"INVALID_DATA","message":"SENTINEL"}`)
		return
	}
	statusTypes := map[desk.TicketStatusType]bool{}
	for _, token := range strings.Split(query.Get("status"), ",") {
		switch token {
		case "${OPEN}":
			statusTypes[desk.TicketStatusTypeOpen] = true
		case "${ONHOLD}":
			statusTypes[desk.TicketStatusTypeOnHold] = true
		case "${CLOSED}":
			statusTypes[desk.TicketStatusTypeClosed] = true
		}
	}
	email := strings.ToLower(query.Get("email"))
	provider.mutex.Lock()
	var matches []any
	for _, ticket := range provider.ticketsNewestFirst() {
		statusType := statusTypeOf(ticket.status)
		if ticket.searchIndexStatusType != "" {
			statusType = ticket.searchIndexStatusType
		}
		if (len(statusTypes) != 0 && !statusTypes[statusType]) || (email != "" && !strings.Contains(strings.ToLower(ticket.email), email)) ||
			(query.Get("departmentId") != "" && ticket.departmentID != query.Get("departmentId")) {
			continue
		}
		value := provider.ticketJSON(ticket)
		value["statusType"] = string(statusType)
		matches = append(matches, value)
	}
	provider.mutex.Unlock()
	if from >= len(matches) {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	page := matches[from:min(from+limit, len(matches))]
	provider.writeValue(response, http.StatusOK, map[string]any{"data": page, "count": strconv.Itoa(len(matches))})
}

func (provider *fakeZohoDesk) readTicket(response http.ResponseWriter, request *http.Request, body []byte, ticketID string) {
	provider.record("read", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.writeJSON(response, http.StatusNotFound, `{"errorCode":"URL_NOT_FOUND"}`)
		return
	}
	value := provider.ticketJSON(ticket)
	if request.URL.Query().Get("include") == "contacts" {
		value["contact"] = map[string]any{"id": ticket.contactID, "email": ticket.email, "lastName": "Contact", "phone": "SENTINEL"}
	}
	provider.writeValue(response, http.StatusOK, value)
}

func (provider *fakeZohoDesk) listThreads(response http.ResponseWriter, request *http.Request, body []byte, ticketID string) {
	provider.record("threads", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.writeJSON(response, http.StatusNotFound, `{"errorCode":"URL_NOT_FOUND"}`)
		return
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"data": []any{map[string]any{
		"id": ticket.id + "1", "channel": "EMAIL", "direction": "in", "visibility": "public", "status": "SUCCESS",
		"summary": ticket.subject, "isDescriptionThread": true, "createdTime": formatFakeTime(ticket.createdAt),
		"author": map[string]any{"name": "Contact", "type": "END_USER"},
	}}})
}

func (provider *fakeZohoDesk) listComments(response http.ResponseWriter, request *http.Request, body []byte, ticketID string) {
	provider.record("comments", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.writeJSON(response, http.StatusNotFound, `{"errorCode":"URL_NOT_FOUND"}`)
		return
	}
	limit, err := strconv.Atoi(request.URL.Query().Get("limit"))
	if err != nil || limit < 1 || limit > 100 || request.URL.Query().Get("sortBy") != "-commentedTime" {
		provider.writeJSON(response, http.StatusUnprocessableEntity, `{"errorCode":"INVALID_DATA"}`)
		return
	}
	comments := []any{}
	for index := len(ticket.comments) - 1; index >= 0 && len(comments) < limit; index-- {
		comments = append(comments, commentJSON(ticket.comments[index]))
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"data": comments})
}

func (provider *fakeZohoDesk) createTicket(response http.ResponseWriter, request *http.Request, body []byte) {
	attempt := provider.record("create", request, body)
	if provider.rejectsCreate {
		provider.writeJSON(response, http.StatusUnprocessableEntity,
			`{"errorCode":"INVALID_DATA","message":"SENTINEL The data does not comply","errors":[{"fieldName":"/departmentId","errorType":"invalid","errorMessage":"SENTINEL"}]}`)
		return
	}
	if provider.rateLimitsFirstCreate && provider.count("createAttempt") == 0 {
		provider.increment("createAttempt")
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"errorCode":"THRESHOLD_EXCEEDED","message":"SENTINEL"}`)
		return
	}
	if attempt == 1 && (provider.delaysFirstCreate || provider.holdsFirstCreate != nil) {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		if provider.holdsFirstCreate != nil {
			<-provider.holdsFirstCreate
		} else {
			time.Sleep(slowResponseDelay)
		}
	}
	var payload struct {
		Subject      string `json:"subject"`
		Description  string `json:"description"`
		DepartmentID string `json:"departmentId"`
		Email        string `json:"email"`
		Status       string `json:"status"`
		Priority     string `json:"priority"`
		Contact      struct {
			Email string `json:"email"`
		} `json:"contact"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	provider.mutex.Lock()
	ticket := provider.storeTicket(payload.Contact.Email, payload.DepartmentID, cmpOr(payload.Status, "Open"), payload.Priority, provider.advanceClock())
	ticket.subject, ticket.descriptionHTML = payload.Subject, payload.Description
	value := provider.ticketJSON(ticket)
	provider.mutex.Unlock()
	if provider.losesFirstCreateResponse && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusOK, value)
}

func (provider *fakeZohoDesk) updateTicket(response http.ResponseWriter, request *http.Request, body []byte, ticketID string) {
	attempt := provider.record("update", request, body)
	if provider.delaysFirstUpdate && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	var payload struct {
		Status       string `json:"status"`
		Priority     string `json:"priority"`
		AssigneeID   string `json:"assigneeId"`
		DepartmentID string `json:"departmentId"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	provider.mutex.Lock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusNotFound, `{"errorCode":"URL_NOT_FOUND"}`)
		return
	}
	ticket.status = cmpOr(payload.Status, ticket.status)
	ticket.priority = cmpOr(payload.Priority, ticket.priority)
	ticket.assigneeID = cmpOr(payload.AssigneeID, ticket.assigneeID)
	ticket.departmentID = cmpOr(payload.DepartmentID, ticket.departmentID)
	ticket.modifiedAt = provider.advanceClock()
	value := provider.ticketJSON(ticket)
	provider.mutex.Unlock()
	if provider.losesFirstUpdateResponse && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusOK, value)
}

func (provider *fakeZohoDesk) addComment(response http.ResponseWriter, request *http.Request, body []byte, ticketID string) {
	attempt := provider.record("comment", request, body)
	if provider.delaysFirstComment && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	var payload struct {
		Content     string `json:"content"`
		IsPublic    bool   `json:"isPublic"`
		ContentType string `json:"contentType"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	require.Equal(provider.t, "plainText", payload.ContentType)
	provider.mutex.Lock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusNotFound, `{"errorCode":"URL_NOT_FOUND"}`)
		return
	}
	provider.nextID++
	comment := fakeComment{id: strconv.FormatInt(provider.nextID, 10), isPublic: payload.IsPublic, content: payload.Content, commentedAt: provider.advanceClock()}
	ticket.comments = append(ticket.comments, comment)
	provider.mutex.Unlock()
	if provider.losesFirstCommentResponse && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusOK, commentJSON(comment))
}

func commentJSON(comment fakeComment) map[string]any {
	return map[string]any{
		"id": comment.id, "isPublic": comment.isPublic, "contentType": "plainText", "content": comment.content, "commenterId": agentID,
		"commentedTime": formatFakeTime(comment.commentedAt), "modifiedTime": nil,
		"commenter": map[string]any{"name": "Dex Integration", "type": "AGENT", "email": "SENTINEL@example.com"},
	}
}

// dropConnection closes the connection after Zoho Desk applied the request, as a lost response would.
func (provider *fakeZohoDesk) dropConnection(response http.ResponseWriter) {
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(provider.t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(provider.t, err)
	require.NoError(provider.t, connection.Close())
}

// accountsRoutingClient sends https://accounts.zoho.eu requests to the fake and refuses every other host.
func (provider *fakeZohoDesk) accountsRoutingClient(t *testing.T, timeout time.Duration) *http.Client {
	target, err := url.Parse(provider.URL)
	require.NoError(t, err)
	return &http.Client{Timeout: timeout, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		routed := request.Clone(request.Context())
		switch request.URL.Host {
		case "accounts.zoho.eu":
			routed.URL.Scheme, routed.URL.Host = target.Scheme, target.Host
		case target.Host:
		default:
			return nil, errors.New("request to an unexpected host")
		}
		return http.DefaultTransport.RoundTrip(routed)
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func (provider *fakeZohoDesk) seedTicket(email string, departmentID string, status string, priority string, modifiedAt string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	instant, err := time.Parse(time.RFC3339, modifiedAt)
	require.NoError(provider.t, err)
	ticket := provider.storeTicket(email, departmentID, status, priority, instant)
	ticket.subject, ticket.descriptionHTML, ticket.seededAt = "Seeded ticket", "<div>Seeded description.</div>", instant
	return ticket.id
}

func (provider *fakeZohoDesk) seedComment(ticketID string, content string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket := provider.tickets[ticketID]
	provider.nextID++
	ticket.comments = append(ticket.comments, fakeComment{id: strconv.FormatInt(provider.nextID, 10), content: content, commentedAt: ticket.modifiedAt})
}

// setSearchIndexStatusType makes the search index disagree with the ticket, as Zoho Desk's indexing delay can.
func (provider *fakeZohoDesk) setSearchIndexStatusType(ticketID string, statusType desk.TicketStatusType) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.tickets[ticketID].searchIndexStatusType = statusType
}

// storeTicket requires provider.mutex.
func (provider *fakeZohoDesk) storeTicket(email string, departmentID string, status string, priority string, createdAt time.Time) *fakeTicket {
	provider.nextID++
	ticket := &fakeTicket{
		id: strconv.FormatInt(provider.nextID, 10), status: status, priority: priority, departmentID: departmentID,
		contactID: strconv.FormatInt(provider.nextID+500000, 10), email: email, createdAt: createdAt, modifiedAt: createdAt,
	}
	provider.tickets[ticket.id] = ticket
	return ticket
}

// advanceClock requires provider.mutex; every write moves modifiedTime forward by one minute.
func (provider *fakeZohoDesk) advanceClock() time.Time {
	provider.clock = provider.clock.Add(time.Minute)
	return provider.clock
}

// ticketsNewestFirst requires provider.mutex.
func (provider *fakeZohoDesk) ticketsNewestFirst() []*fakeTicket {
	tickets := make([]*fakeTicket, 0, len(provider.tickets))
	for _, ticket := range provider.tickets {
		tickets = append(tickets, ticket)
	}
	sort.Slice(tickets, func(left, right int) bool {
		if !tickets[left].modifiedAt.Equal(tickets[right].modifiedAt) {
			return tickets[left].modifiedAt.After(tickets[right].modifiedAt)
		}
		return tickets[left].id < tickets[right].id
	})
	return tickets
}

// ticketJSON requires provider.mutex; IDs and counts are strings, as Zoho Desk sends them.
func (provider *fakeZohoDesk) ticketJSON(ticket *fakeTicket) map[string]any {
	return map[string]any{
		"id": ticket.id, "ticketNumber": strings.TrimPrefix(ticket.id, "18920000001"), "subject": ticket.subject,
		"description": ticket.descriptionHTML, "status": ticket.status, "statusType": string(statusTypeOf(ticket.status)),
		"priority": nullableString(ticket.priority), "channel": "Web", "departmentId": ticket.departmentID, "contactId": ticket.contactID,
		"assigneeId": nullableString(ticket.assigneeID), "email": ticket.email, "phone": "SENTINEL phone", "isSpam": false,
		"threadCount": "1", "commentCount": strconv.Itoa(len(ticket.comments)), "createdTime": formatFakeTime(ticket.createdAt),
		"modifiedTime": formatFakeTime(ticket.modifiedAt), "dueDate": nil, "closedTime": nil, "cf": map[string]any{"cf_note": "SENTINEL"},
	}
}

func statusTypeOf(status string) desk.TicketStatusType {
	switch status {
	case "On Hold":
		return desk.TicketStatusTypeOnHold
	case "Closed":
		return desk.TicketStatusTypeClosed
	default:
		return desk.TicketStatusTypeOpen
	}
}

func formatFakeTime(instant time.Time) string {
	return instant.UTC().Format("2006-01-02T15:04:05.000Z")
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func cmpOr(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (provider *fakeZohoDesk) record(name string, request *http.Request, body []byte) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[name]++
	recorded := fakeRecordedRequest{at: time.Now(), query: request.URL.Query(), body: string(body)}
	if form, err := url.ParseQuery(string(body)); err == nil && name == "token" {
		recorded.form = form
	}
	provider.requests[name] = append(provider.requests[name], recorded)
	return provider.counts[name]
}

func (provider *fakeZohoDesk) increment(name string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[name]++
}

func (provider *fakeZohoDesk) writeValue(response http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	require.NoError(provider.t, err)
	provider.writeJSON(response, status, string(encoded))
}

func (provider *fakeZohoDesk) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json;charset=UTF-8")
	response.Header().Set("X-Rate-Limit-Request-Weight-v3", "1")
	response.Header().Set("X-Rate-Limit-Remaining-v3", "49990")
	response.WriteHeader(status)
	if _, err := io.WriteString(response, body); err != nil {
		provider.t.Logf("fake Zoho Desk response write failed: %v", err)
	}
}

func (provider *fakeZohoDesk) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * slowResponseDelay):
		t.Fatal("a delayed fake Zoho Desk request did not finish")
	}
}

func (provider *fakeZohoDesk) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakeZohoDesk) totalRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, requests := range provider.requests {
		total += len(requests)
	}
	return total
}

func (provider *fakeZohoDesk) ticketCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.tickets)
}

func (provider *fakeZohoDesk) ticket(ticketID string) fakeTicket {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket := *provider.tickets[ticketID]
	ticket.comments = append([]fakeComment(nil), ticket.comments...)
	return ticket
}

func (provider *fakeZohoDesk) lastRequest(name string) fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	requests := provider.requests[name]
	require.NotEmpty(provider.t, requests, name)
	return requests[len(requests)-1]
}

func (provider *fakeZohoDesk) requestsNamed(name string) []fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]fakeRecordedRequest(nil), provider.requests[name]...)
}

func (provider *fakeZohoDesk) queryValues(name string, key string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var values []string
	for _, request := range provider.requests[name] {
		values = append(values, request.query.Get(key))
	}
	return values
}

func (provider *fakeZohoDesk) requestTimes(name string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var times []time.Time
	for _, request := range provider.requests[name] {
		times = append(times, request.at)
	}
	return times
}

// triageHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type triageHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newTriageHarness(t *testing.T, provider *fakeZohoDesk, requestTimeout time.Duration) *triageHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "zoho", Name: ConnectionName}
	credentials := desk.Credentials{
		AuthMethodID: desk.USDataCenterAuthMethodID, OAuthClientID: "1000.ZOHODESKINTEGRATIONCLIENT",
		OAuthClientSecret: sdkgo.NewSecretString("zoho-integration-secret"), AccessToken: sdkgo.NewSecretString(integrationAccessToken),
		RefreshToken: sdkgo.NewSecretString(integrationRefreshToken),
	}
	providerClient, err := desk.New(desk.Config{OrgID: integrationOrganizationID},
		sdkgo.StaticCredentialProvider[desk.Credentials]{reference: credentials},
		desk.WithAPIBaseURL(provider.URL+"/api/v1"), desk.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := desk.NewConnection(providerClient, reference)
	require.NoError(t, err)
	return newTriageHarnessForConnection(t, connection)
}

func newTriageHarnessForConnection(t *testing.T, connection desk.Connection) *triageHarness {
	t.Helper()
	harness := &triageHarness{flow: NewFlow(connection), serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
	var err error
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

func (harness *triageHarness) startWorker(t *testing.T) {
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
func (harness *triageHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *triageHarness) runTriage(t *testing.T, scenario string, input Input) TriageOutcome {
	t.Helper()
	flowID := harness.startTriage(t, scenario, input)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome TriageOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

func (harness *triageHarness) startTriage(t *testing.T, scenario string, input Input) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	flowID := "zoho-desk-triage-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlow waits, across server long-poll caps, until the Flow closes.
func (harness *triageHarness) waitForFlow(t *testing.T, flowID string) dex.FlowResult {
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
