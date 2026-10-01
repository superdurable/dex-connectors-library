//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerissue

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
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationSubdomain = "acme"
	integrationAgent     = "agent@acme.example.com"
	integrationAPIToken  = "zendeskIntegrationToken0123456789"
	integrationRequester = "jane@acme.example.com"
	integrationIssueTag  = "billing-double-charge"

	requesterUserID = int64(20978392)
	agentUserID     = int64(235323)

	defaultRequestTimeout = 5 * time.Second
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so Dex dispatches a backup attempt.
	slowResponseDelay  = 9 * time.Second
	slowRequestTimeout = 20 * time.Second
)

func integrationIssueInput() Input {
	return Input{
		RequesterEmail: integrationRequester, RequesterName: "Jane Smith", Subject: "Double charge on order 88213",
		Message: "I was charged twice for order 88213.", IssueTag: integrationIssueTag, GroupID: 98738,
	}
}

func TestNewIssueOpensOneTicketWithRealDex(t *testing.T) {
	provider := newFakeZendesk(t)
	flow, harness := newCustomerIssueHarness(t, provider, defaultRequestTimeout)
	ctx := integrationContext(t)

	outcome := runCustomerIssue(t, ctx, harness, flow, "new-issue", integrationIssueInput())
	require.Equal(t, IssueTicketOpened, outcome.Action)
	require.False(t, outcome.WasAlreadyApplied)
	require.Equal(t, requesterUserID, outcome.Ticket.RequesterID)
	require.Equal(t, "https://acme.zendesk.com/agent/tickets/"+strconv.FormatInt(outcome.Ticket.ID, 10), outcome.Ticket.AgentURL)
	require.Equal(t, 1, provider.ticketCount())
	require.Equal(t, 1, provider.count("create"))

	search := provider.lastRequest("search")
	require.Equal(t, "status:new status:open status:pending status:hold requester:jane@acme.example.com tags:billing-double-charge", search.query.Get("query"))
	require.Equal(t, "ticket", search.query.Get("filter[type]"))
	create := provider.lastRequest("create")
	require.NotEmpty(t, create.header.Get("Idempotency-Key"))
	require.JSONEq(t, `{"ticket":{"subject":"Double charge on order 88213","comment":{"body":"I was charged twice for order 88213.","public":true},
		"requester":{"email":"jane@acme.example.com","name":"Jane Smith"},"priority":"normal","tags":["billing-double-charge"],"group_id":98738,
		"metadata":{"dex_idempotency_key":"`+create.header.Get("Idempotency-Key")+`"}}}`, create.body)
	ticket := provider.ticket(outcome.Ticket.ID)
	require.Len(t, ticket.comments, 1)
	require.True(t, ticket.comments[0].public)
}

func TestRepeatContactAddsOneInternalNoteAndLeavesDecoysUntouchedWithRealDex(t *testing.T) {
	provider := newFakeZendesk(t)
	existing := provider.seedTicket(requesterUserID, "pending", []string{integrationIssueTag}, "2026-01-26T14:02:00Z", "I was charged twice.")
	solved := provider.seedTicket(requesterUserID, "solved", []string{integrationIssueTag}, "2026-01-12T09:00:00Z", "Refund already issued.")
	otherCustomer := provider.seedTicket(provider.seedUser("ben@meridian.example.com"), "open", []string{integrationIssueTag}, "2026-01-27T11:00:00Z", "Password reset.")
	flow, harness := newCustomerIssueHarness(t, provider, defaultRequestTimeout)
	ctx := integrationContext(t)

	outcome := runCustomerIssue(t, ctx, harness, flow, "repeat-contact", integrationIssueInput())
	require.Equal(t, IssueFollowUpAdded, outcome.Action)
	require.Equal(t, existing, outcome.Ticket.ID)
	require.Equal(t, support.TicketStatusOpen, outcome.Ticket.Status)
	require.Contains(t, outcome.Ticket.Tags, RepeatContactTag)
	require.False(t, outcome.WasAlreadyApplied)

	ticket := provider.ticket(existing)
	require.Len(t, ticket.comments, 2, "exactly one follow-up note")
	require.False(t, ticket.comments[1].public, "the follow-up is an internal note")
	require.Equal(t, FollowUpNote+"\n\nI was charged twice for order 88213.", ticket.comments[1].body)
	require.Equal(t, 1, provider.count("appliedUpdate"))
	update := provider.lastRequest("update")
	require.Contains(t, update.body, `"safe_update":true`)
	require.Contains(t, update.body, `"updated_stamp":"2026-01-26T14:02:00Z"`)
	for _, decoy := range []int64{solved, otherCustomer} {
		require.Len(t, provider.ticket(decoy).comments, 1, "ticket %d must not be touched", decoy)
	}
	require.Equal(t, 3, provider.ticketCount(), "no ticket was created")
}

func TestSearchMatchForAnotherRequesterOpensANewTicketWithRealDex(t *testing.T) {
	provider := newFakeZendesk(t)
	lookalike := provider.seedTicket(provider.seedUser("jane@acme.example.com.au"), "open", []string{integrationIssueTag}, "2026-01-27T11:00:00Z", "Different Jane.")
	flow, harness := newCustomerIssueHarness(t, provider, defaultRequestTimeout)
	ctx := integrationContext(t)

	outcome := runCustomerIssue(t, ctx, harness, flow, "lookalike-requester", integrationIssueInput())
	require.Equal(t, IssueTicketOpened, outcome.Action)
	require.Equal(t, lookalike, outcome.SkippedTicketID)
	require.NotEqual(t, lookalike, outcome.Ticket.ID)
	require.Len(t, provider.ticket(lookalike).comments, 1, "the other requester's ticket is read but never written")
	require.Zero(t, provider.count("update"))
}

func TestLostCreateResponseIsReplayedUnderTheSameKeyWithRealDex(t *testing.T) {
	provider := newFakeZendesk(t)
	provider.losesFirstCreateResponse = true
	flow, harness := newCustomerIssueHarness(t, provider, defaultRequestTimeout)
	ctx := integrationContext(t)

	outcome := runCustomerIssue(t, ctx, harness, flow, "lost-create", integrationIssueInput())
	require.Equal(t, IssueTicketOpened, outcome.Action)
	require.True(t, outcome.WasAlreadyApplied, "the retried request received Zendesk's cached response")
	require.Equal(t, 1, provider.ticketCount())
	require.Equal(t, 2, provider.count("create"))
	keys := provider.createKeys()
	require.Equal(t, keys[0], keys[1], "the retry reused the Step's Idempotency-Key")
}

// TestSlowCreateBackupDispatchCreatesOneTicketWithRealDex covers Dex's async backup attempt while the first create is in flight.
func TestSlowCreateBackupDispatchCreatesOneTicketWithRealDex(t *testing.T) {
	provider := newFakeZendesk(t)
	provider.delaysFirstCreate = true
	flow, harness := newCustomerIssueHarness(t, provider, slowRequestTimeout)
	ctx := integrationContext(t)

	outcome := runCustomerIssue(t, ctx, harness, flow, "slow-create", integrationIssueInput())
	require.Equal(t, IssueTicketOpened, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.ticketCount(), "every dispatch carried one key, so Zendesk created one ticket")
	require.GreaterOrEqual(t, provider.count("create"), 2, "Dex dispatched a backup attempt past its local phase")
	for _, key := range provider.createKeys() {
		require.Equal(t, provider.createKeys()[0], key)
	}
	t.Logf("slow create: creates=%d inFlightConflicts=%d replays=%d", provider.count("create"), provider.count("createInFlight"), provider.count("createReplay"))
}

func TestLostUpdateResponseDoesNotRepeatTheNoteWithRealDex(t *testing.T) {
	provider := newFakeZendesk(t)
	existing := provider.seedTicket(requesterUserID, "open", []string{integrationIssueTag}, "2026-01-26T14:02:00Z", "I was charged twice.")
	provider.losesFirstUpdateResponse = true
	flow, harness := newCustomerIssueHarness(t, provider, defaultRequestTimeout)
	ctx := integrationContext(t)

	outcome := runCustomerIssue(t, ctx, harness, flow, "lost-update", integrationIssueInput())
	require.Equal(t, IssueFollowUpAdded, outcome.Action)
	require.True(t, outcome.WasAlreadyApplied, "the retried Step found its own audit marker")
	require.Len(t, provider.ticket(existing).comments, 2)
	require.Equal(t, 1, provider.count("update"), "the retry wrote nothing")
	require.GreaterOrEqual(t, provider.count("audits"), 2)
}

// TestSlowUpdateBackupDispatchAddsOneNoteWithRealDex covers two concurrent update attempts of one Step.
func TestSlowUpdateBackupDispatchAddsOneNoteWithRealDex(t *testing.T) {
	provider := newFakeZendesk(t)
	existing := provider.seedTicket(requesterUserID, "open", []string{integrationIssueTag}, "2026-01-26T14:02:00Z", "I was charged twice.")
	provider.delaysFirstUpdate = true
	flow, harness := newCustomerIssueHarness(t, provider, slowRequestTimeout)
	ctx := integrationContext(t)

	outcome := runCustomerIssue(t, ctx, harness, flow, "slow-update", integrationIssueInput())
	require.Equal(t, IssueFollowUpAdded, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.Len(t, provider.ticket(existing).comments, 2, "the safe update rejected the stale concurrent write")
	require.Equal(t, 1, provider.count("appliedUpdate"))
	require.GreaterOrEqual(t, provider.count("update"), 2, "Dex dispatched a backup attempt past its local phase")
	require.Equal(t, 1, provider.count("updateConflict"), "the delayed first write carried a stale updated_stamp")
	t.Logf("slow update: updates=%d applied=%d safeUpdateConflicts=%d", provider.count("update"), provider.count("appliedUpdate"), provider.count("updateConflict"))
}

func TestRateLimitedSearchWaitsForRetryAfterWithRealDex(t *testing.T) {
	provider := newFakeZendesk(t)
	provider.rateLimitsFirstSearch = true
	flow, harness := newCustomerIssueHarness(t, provider, defaultRequestTimeout)
	ctx := integrationContext(t)

	outcome := runCustomerIssue(t, ctx, harness, flow, "rate-limited", integrationIssueInput())
	require.Equal(t, IssueTicketOpened, outcome.Action)
	times := provider.requestTimes("search")
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), time.Second, "the retry waited for Retry-After")
}

func TestRejectedTicketFailsTheFlowWithoutZendeskTextWithRealDex(t *testing.T) {
	provider := newFakeZendesk(t)
	provider.rejectsCreate = true
	flow, harness := newCustomerIssueHarness(t, provider, defaultRequestTimeout)
	ctx := integrationContext(t)
	flowID := startCustomerIssue(t, ctx, harness, flow, "rejected", integrationIssueInput())

	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	require.NotContains(t, result.ErrorMessage, integrationAPIToken)
	require.Equal(t, 1, provider.count("create"), "a conclusive rejection is not retried")
	require.Zero(t, provider.ticketCount())
	t.Logf("rejected create failure: %s", result.ErrorMessage)
}

func TestInvalidIssueFailsBeforeCallingZendeskWithRealDex(t *testing.T) {
	provider := newFakeZendesk(t)
	flow, harness := newCustomerIssueHarness(t, provider, defaultRequestTimeout)
	ctx := integrationContext(t)
	input := integrationIssueInput()
	input.RequesterEmail = "Jane <jane@acme.example.com>"
	flowID := startCustomerIssue(t, ctx, harness, flow, "invalid", input)

	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, result.Status)
	require.Zero(t, provider.totalRequests())
}

func runCustomerIssue(t *testing.T, ctx context.Context, harness *customerIssueHarness, flow *Flow, scenario string, input Input) CustomerIssueOutcome {
	t.Helper()
	flowID := startCustomerIssue(t, ctx, harness, flow, scenario, input)
	result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome CustomerIssueOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

func startCustomerIssue(t *testing.T, ctx context.Context, harness *customerIssueHarness, flow *Flow, scenario string, input Input) string {
	t.Helper()
	flowID := "zendesk-customer-issue-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	return flowID
}

// fakeZendesk is a stateful, credential-safe Zendesk Support fake. It honors the documented
// Idempotency-Key replay for ticket creation, safe updates with updated_stamp, and audit metadata.
type fakeZendesk struct {
	*httptest.Server
	t     *testing.T
	mutex sync.Mutex
	// delayedRequests tracks handlers still sleeping, so a test can assert their final effect.
	delayedRequests sync.WaitGroup

	clock         time.Time
	nextTicketID  int64
	nextCommentID int64
	nextAuditID   int64
	userEmails    map[int64]string
	tickets       map[int64]*fakeTicket
	createsByKey  map[string]*fakeIdempotentCreate
	counts        map[string]int
	requests      map[string][]fakeRecordedRequest

	losesFirstCreateResponse bool
	delaysFirstCreate        bool
	losesFirstUpdateResponse bool
	delaysFirstUpdate        bool
	rateLimitsFirstSearch    bool
	rejectsCreate            bool
}

type fakeTicket struct {
	id          int64
	subject     string
	status      string
	priority    string
	requesterID int64
	assigneeID  int64
	groupID     int64
	tags        []string
	createdAt   time.Time
	updatedAt   time.Time
	comments    []fakeComment
	auditKeys   []string
}

type fakeComment struct {
	id        int64
	authorID  int64
	public    bool
	body      string
	createdAt time.Time
}

type fakeIdempotentCreate struct {
	body       string
	isInFlight bool
	ticketID   int64
}

type fakeRecordedRequest struct {
	at     time.Time
	query  url.Values
	header http.Header
	body   string
}

func newFakeZendesk(t *testing.T) *fakeZendesk {
	t.Helper()
	provider := &fakeZendesk{
		t: t, clock: time.Date(2026, 1, 28, 9, 0, 0, 0, time.UTC), nextTicketID: 35000, nextCommentID: 1, nextAuditID: 1,
		userEmails:   map[int64]string{requesterUserID: integrationRequester, agentUserID: integrationAgent},
		tickets:      map[int64]*fakeTicket{},
		createsByKey: map[string]*fakeIdempotentCreate{}, counts: map[string]int{}, requests: map[string][]fakeRecordedRequest{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeZendesk) serveHTTP(response http.ResponseWriter, request *http.Request) {
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(integrationAgent+"/token:"+integrationAPIToken))
	if request.Header.Get("Authorization") != expected {
		provider.writeJSON(response, http.StatusUnauthorized, `{"error":"Couldn't authenticate you"}`)
		return
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		provider.writeJSON(response, http.StatusBadRequest, `{"error":"InvalidBody"}`)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/api/v2")
	switch {
	case request.Method == http.MethodGet && path == "/search/export":
		provider.searchTickets(response, request, body)
	case request.Method == http.MethodPost && path == "/tickets":
		provider.createTicket(response, request, body)
	case strings.HasPrefix(path, "/tickets/"):
		segments := strings.Split(strings.TrimPrefix(path, "/tickets/"), "/")
		ticketID, err := strconv.ParseInt(segments[0], 10, 64)
		if err != nil {
			provider.writeJSON(response, http.StatusNotFound, `{"error":"InvalidEndpoint"}`)
			return
		}
		switch {
		case request.Method == http.MethodGet && len(segments) == 1:
			provider.readTicket(response, request, body, ticketID)
		case request.Method == http.MethodGet && len(segments) == 2 && segments[1] == "comments":
			provider.listComments(response, request, body, ticketID)
		case request.Method == http.MethodGet && len(segments) == 2 && segments[1] == "audits":
			provider.listAudits(response, request, body, ticketID)
		case request.Method == http.MethodPut && len(segments) == 1:
			provider.updateTicket(response, request, body, ticketID)
		default:
			provider.writeJSON(response, http.StatusNotFound, `{"error":"InvalidEndpoint"}`)
		}
	default:
		provider.writeJSON(response, http.StatusNotFound, `{"error":"InvalidEndpoint"}`)
	}
}

func (provider *fakeZendesk) searchTickets(response http.ResponseWriter, request *http.Request, body []byte) {
	isFirst := provider.record("search", request, body) == 1
	if provider.rateLimitsFirstSearch && isFirst {
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"error":"TooManyRequests","description":"SENTINEL slow down"}`)
		return
	}
	query := request.URL.Query().Get("query")
	if request.URL.Query().Get("filter[type]") != "ticket" || strings.Contains(query, "type:") {
		provider.writeJSON(response, http.StatusBadRequest, `{"error":"InvalidQuery"}`)
		return
	}
	var statuses, tags []string
	requesterPrefix := ""
	for _, term := range strings.Fields(query) {
		switch {
		case strings.HasPrefix(term, "status:"):
			statuses = append(statuses, strings.TrimPrefix(term, "status:"))
		case strings.HasPrefix(term, "tags:"):
			tags = append(tags, strings.TrimPrefix(term, "tags:"))
		case strings.HasPrefix(term, "requester:"):
			requesterPrefix = strings.TrimPrefix(term, "requester:")
		}
	}
	provider.mutex.Lock()
	var results []any
	for _, ticket := range provider.sortedTickets() {
		// Like Zendesk's user matching, the requester filter is not an exact address comparison.
		if (len(statuses) != 0 && !contains(statuses, ticket.status)) || (len(tags) != 0 && !overlaps(tags, ticket.tags)) ||
			(requesterPrefix != "" && !strings.HasPrefix(provider.userEmails[ticket.requesterID], requesterPrefix)) {
			continue
		}
		result := provider.ticketJSON(ticket)
		result["result_type"] = "ticket"
		results = append(results, result)
	}
	provider.mutex.Unlock()
	if results == nil {
		results = []any{}
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"results": results, "facets": nil, "meta": map[string]any{"has_more": false, "after_cursor": nil}})
}

func (provider *fakeZendesk) readTicket(response http.ResponseWriter, request *http.Request, body []byte, ticketID int64) {
	provider.record("read", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.writeJSON(response, http.StatusNotFound, `{"error":"RecordNotFound","description":"Not found"}`)
		return
	}
	value := map[string]any{"ticket": provider.ticketJSON(ticket)}
	if request.URL.Query().Get("include") == "users" {
		value["users"] = []any{map[string]any{"id": ticket.requesterID, "name": "Requester", "email": provider.userEmails[ticket.requesterID], "role": "end-user"}}
	}
	provider.writeValue(response, http.StatusOK, value)
}

func (provider *fakeZendesk) listComments(response http.ResponseWriter, request *http.Request, body []byte, ticketID int64) {
	provider.record("comments", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.writeJSON(response, http.StatusNotFound, `{"error":"RecordNotFound"}`)
		return
	}
	size, err := strconv.Atoi(request.URL.Query().Get("page[size]"))
	if err != nil || size < 1 || request.URL.Query().Get("sort") != "-created_at" {
		provider.writeJSON(response, http.StatusBadRequest, `{"error":"InvalidPaginationParameter"}`)
		return
	}
	var comments []any
	for index := len(ticket.comments) - 1; index >= 0 && len(comments) < size; index-- {
		comment := ticket.comments[index]
		comments = append(comments, map[string]any{
			"id": comment.id, "author_id": comment.authorID, "public": comment.public, "body": comment.body, "plain_body": comment.body,
			"created_at": comment.createdAt.Format(time.RFC3339),
		})
	}
	if comments == nil {
		comments = []any{}
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"comments": comments, "meta": map[string]any{"has_more": len(ticket.comments) > size}})
}

func (provider *fakeZendesk) listAudits(response http.ResponseWriter, request *http.Request, body []byte, ticketID int64) {
	provider.record("audits", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.writeJSON(response, http.StatusNotFound, `{"error":"RecordNotFound"}`)
		return
	}
	audits := []any{}
	for index := len(ticket.auditKeys) - 1; index >= 0; index-- {
		custom := map[string]any{}
		if ticket.auditKeys[index] != "" {
			custom["dex_idempotency_key"] = ticket.auditKeys[index]
		}
		audits = append(audits, map[string]any{"id": index + 1, "ticket_id": ticketID, "metadata": map[string]any{"custom": custom, "system": map[string]any{"ip_address": "SENTINEL"}}})
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"audits": audits})
}

func (provider *fakeZendesk) createTicket(response http.ResponseWriter, request *http.Request, body []byte) {
	attempt := provider.record("create", request, body)
	if provider.rejectsCreate {
		provider.writeJSON(response, http.StatusUnprocessableEntity, `{"error":"RecordInvalid","description":"SENTINEL Requester is suspended","details":{"requester":[{"type":"invalid","description":"SENTINEL suspended"}]}}`)
		return
	}
	key := request.Header.Get("Idempotency-Key")
	if key == "" {
		provider.writeJSON(response, http.StatusBadRequest, `{"error":"MissingIdempotencyKey"}`)
		return
	}
	provider.mutex.Lock()
	if earlier, found := provider.createsByKey[key]; found {
		defer provider.mutex.Unlock()
		switch {
		case earlier.isInFlight:
			provider.counts["createInFlight"]++
			provider.writeJSON(response, http.StatusConflict, `{"error":"IdempotentRequestInProgress","description":"SENTINEL in progress"}`)
		case earlier.body != string(body):
			provider.writeJSON(response, http.StatusBadRequest, `{"error":"IdempotentRequestError","description":"Request parameters don't match the given idempotency key"}`)
		default:
			provider.counts["createReplay"]++
			response.Header().Set("X-Idempotency-Lookup", "hit")
			provider.writeValue(response, http.StatusCreated, map[string]any{"ticket": provider.ticketJSON(provider.tickets[earlier.ticketID])})
		}
		return
	}
	entry := &fakeIdempotentCreate{body: string(body), isInFlight: true}
	provider.createsByKey[key] = entry
	provider.mutex.Unlock()
	if provider.delaysFirstCreate && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	var payload struct {
		Ticket struct {
			Subject string `json:"subject"`
			Comment struct {
				Body   string `json:"body"`
				Public bool   `json:"public"`
			} `json:"comment"`
			Requester struct {
				Email string `json:"email"`
			} `json:"requester"`
			Priority string            `json:"priority"`
			Tags     []string          `json:"tags"`
			GroupID  int64             `json:"group_id"`
			Metadata map[string]string `json:"metadata"`
		} `json:"ticket"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	provider.mutex.Lock()
	requesterID := provider.userIDForEmail(payload.Ticket.Requester.Email)
	ticket := provider.storeTicket(requesterID, "new", payload.Ticket.Tags, provider.advanceClock(), payload.Ticket.Comment.Body, payload.Ticket.Comment.Public)
	ticket.subject, ticket.priority, ticket.groupID = payload.Ticket.Subject, payload.Ticket.Priority, payload.Ticket.GroupID
	ticket.auditKeys = []string{payload.Ticket.Metadata["dex_idempotency_key"]}
	entry.isInFlight, entry.ticketID = false, ticket.id
	value := map[string]any{"ticket": provider.ticketJSON(ticket)}
	provider.mutex.Unlock()
	if provider.losesFirstCreateResponse && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	response.Header().Set("X-Idempotency-Lookup", "miss")
	provider.writeValue(response, http.StatusCreated, value)
}

func (provider *fakeZendesk) updateTicket(response http.ResponseWriter, request *http.Request, body []byte, ticketID int64) {
	attempt := provider.record("update", request, body)
	if provider.delaysFirstUpdate && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	var payload struct {
		Ticket struct {
			Status       string            `json:"status"`
			Priority     string            `json:"priority"`
			AssigneeID   int64             `json:"assignee_id"`
			GroupID      int64             `json:"group_id"`
			Tags         *[]string         `json:"tags"`
			Comment      *fakeCommentInput `json:"comment"`
			Metadata     map[string]string `json:"metadata"`
			SafeUpdate   bool              `json:"safe_update"`
			UpdatedStamp string            `json:"updated_stamp"`
		} `json:"ticket"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	provider.mutex.Lock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusNotFound, `{"error":"RecordNotFound"}`)
		return
	}
	if payload.Ticket.SafeUpdate && payload.Ticket.UpdatedStamp != ticket.updatedAt.Format(time.RFC3339) {
		provider.counts["updateConflict"]++
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusConflict, `{"error":"UpdateConflict","description":"SENTINEL Safe Update prevented the update due to outdated ticket data."}`)
		return
	}
	if payload.Ticket.Status != "" {
		ticket.status = payload.Ticket.Status
	}
	if payload.Ticket.Priority != "" {
		ticket.priority = payload.Ticket.Priority
	}
	if payload.Ticket.AssigneeID != 0 {
		ticket.assigneeID = payload.Ticket.AssigneeID
	}
	if payload.Ticket.GroupID != 0 {
		ticket.groupID = payload.Ticket.GroupID
	}
	if payload.Ticket.Tags != nil {
		ticket.tags = append([]string(nil), (*payload.Ticket.Tags)...)
	}
	updatedAt := provider.advanceClock()
	if payload.Ticket.Comment != nil {
		ticket.comments = append(ticket.comments, fakeComment{
			id: provider.nextCommentID, authorID: agentUserID, public: payload.Ticket.Comment.Public, body: payload.Ticket.Comment.Body, createdAt: updatedAt,
		})
		provider.nextCommentID++
	}
	ticket.updatedAt = updatedAt
	ticket.auditKeys = append(ticket.auditKeys, payload.Ticket.Metadata["dex_idempotency_key"])
	provider.counts["appliedUpdate"]++
	value := map[string]any{"ticket": provider.ticketJSON(ticket), "audit": map[string]any{"id": len(ticket.auditKeys)}}
	provider.mutex.Unlock()
	if provider.losesFirstUpdateResponse && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusOK, value)
}

type fakeCommentInput struct {
	Body   string `json:"body"`
	Public bool   `json:"public"`
}

// dropConnection closes the connection after Zendesk applied the request, as a lost response would.
func (provider *fakeZendesk) dropConnection(response http.ResponseWriter) {
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(provider.t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(provider.t, err)
	require.NoError(provider.t, connection.Close())
}

func (provider *fakeZendesk) seedUser(email string) int64 {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.userIDForEmail(email)
}

func (provider *fakeZendesk) seedTicket(requesterID int64, status string, tags []string, updatedAt string, firstComment string) int64 {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	instant, err := time.Parse(time.RFC3339, updatedAt)
	require.NoError(provider.t, err)
	ticket := provider.storeTicket(requesterID, status, tags, instant, firstComment, true)
	ticket.subject, ticket.priority = "Seeded ticket", "normal"
	ticket.auditKeys = []string{""}
	return ticket.id
}

// storeTicket requires provider.mutex.
func (provider *fakeZendesk) storeTicket(requesterID int64, status string, tags []string, createdAt time.Time, firstComment string, isPublic bool) *fakeTicket {
	provider.nextTicketID++
	ticket := &fakeTicket{
		id: provider.nextTicketID, status: status, requesterID: requesterID, tags: append([]string(nil), tags...),
		createdAt: createdAt, updatedAt: createdAt,
		comments: []fakeComment{{id: provider.nextCommentID, authorID: requesterID, public: isPublic, body: firstComment, createdAt: createdAt}},
	}
	provider.nextCommentID++
	provider.tickets[ticket.id] = ticket
	return ticket
}

// userIDForEmail requires provider.mutex.
func (provider *fakeZendesk) userIDForEmail(email string) int64 {
	for id, known := range provider.userEmails {
		if known == email {
			return id
		}
	}
	id := int64(90000 + len(provider.userEmails))
	provider.userEmails[id] = email
	return id
}

// advanceClock requires provider.mutex; every write moves updated_at forward by one minute.
func (provider *fakeZendesk) advanceClock() time.Time {
	provider.clock = provider.clock.Add(time.Minute)
	return provider.clock
}

// sortedTickets requires provider.mutex and orders tickets by creation, as export search does.
func (provider *fakeZendesk) sortedTickets() []*fakeTicket {
	tickets := make([]*fakeTicket, 0, len(provider.tickets))
	for _, ticket := range provider.tickets {
		tickets = append(tickets, ticket)
	}
	sort.Slice(tickets, func(left, right int) bool { return tickets[left].id < tickets[right].id })
	return tickets
}

// ticketJSON requires provider.mutex.
func (provider *fakeZendesk) ticketJSON(ticket *fakeTicket) map[string]any {
	return map[string]any{
		"id": ticket.id, "url": fmt.Sprintf("https://acme.zendesk.com/api/v2/tickets/%d.json", ticket.id), "subject": ticket.subject,
		"description": ticket.comments[0].body, "status": ticket.status, "priority": nullable(ticket.priority), "type": nil,
		"requester_id": ticket.requesterID, "submitter_id": ticket.requesterID, "assignee_id": nullableID(ticket.assigneeID),
		"group_id": nullableID(ticket.groupID), "tags": append([]string{}, ticket.tags...), "via": map[string]any{"channel": "api"},
		"created_at": ticket.createdAt.Format(time.RFC3339), "updated_at": ticket.updatedAt.Format(time.RFC3339),
	}
}

func (provider *fakeZendesk) record(name string, request *http.Request, body []byte) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[name]++
	provider.requests[name] = append(provider.requests[name], fakeRecordedRequest{
		at: time.Now(), query: request.URL.Query(), header: request.Header.Clone(), body: string(body),
	})
	return provider.counts[name]
}

func (provider *fakeZendesk) writeValue(response http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	require.NoError(provider.t, err)
	provider.writeJSON(response, status, string(encoded))
}

func (provider *fakeZendesk) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Zendesk-Request-Id", "fake-request")
	response.WriteHeader(status)
	if _, err := io.WriteString(response, body); err != nil {
		provider.t.Logf("fake Zendesk response write failed: %v", err)
	}
}

func (provider *fakeZendesk) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * slowResponseDelay):
		t.Fatal("a delayed fake Zendesk request did not finish")
	}
}

func (provider *fakeZendesk) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakeZendesk) totalRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, requests := range provider.requests {
		total += len(requests)
	}
	return total
}

func (provider *fakeZendesk) ticketCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.tickets)
}

func (provider *fakeZendesk) ticket(ticketID int64) fakeTicket {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket := *provider.tickets[ticketID]
	ticket.comments = append([]fakeComment(nil), ticket.comments...)
	return ticket
}

func (provider *fakeZendesk) lastRequest(name string) fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	requests := provider.requests[name]
	require.NotEmpty(provider.t, requests, name)
	return requests[len(requests)-1]
}

func (provider *fakeZendesk) requestTimes(name string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var times []time.Time
	for _, request := range provider.requests[name] {
		times = append(times, request.at)
	}
	return times
}

func (provider *fakeZendesk) createKeys() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var keys []string
	for _, request := range provider.requests["create"] {
		keys = append(keys, request.header.Get("Idempotency-Key"))
	}
	return keys
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func overlaps(wanted []string, actual []string) bool {
	for _, value := range wanted {
		if contains(actual, value) {
			return true
		}
	}
	return false
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableID(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

type customerIssueHarness struct {
	cache        *blobcache.Cache
	worker       *dex.Worker
	workerResult chan error
	client       *dex.Client
}

func newCustomerIssueHarness(t *testing.T, provider *fakeZendesk, requestTimeout time.Duration) (*Flow, *customerIssueHarness) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "zendesk", Name: ConnectionName}
	providerClient, err := support.New(support.Config{Subdomain: integrationSubdomain},
		sdkgo.StaticCredentialProvider[support.Credentials]{reference: {Email: integrationAgent, APIToken: sdkgo.NewSecretString(integrationAPIToken)}},
		support.WithAPIBaseURL(provider.URL+"/api/v2"), support.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := support.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &customerIssueHarness{cache: cache}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.worker = worker
	harness.workerResult = make(chan error, 1)
	go func() { harness.workerResult <- worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return flow, harness
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
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
