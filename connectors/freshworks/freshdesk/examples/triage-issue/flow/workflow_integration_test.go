//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageissue

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationDomain    = "acme"
	integrationAPIKey    = "freshdeskIntegrationKey0123456789"
	integrationRequester = "jane@acme.example.com"
	integrationIssueTag  = "billing-double-charge"
	agentUserID          = int64(6001263404)

	defaultRequestTimeout = 5 * time.Second
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowResponseDelay  = 9 * time.Second
	slowRequestTimeout = 20 * time.Second
)

var (
	statusTermPattern = regexp.MustCompile(`status:(\d+)`)
	tagTermPattern    = regexp.MustCompile(`tag:'([^']*)'`)
)

func integrationIssueInput() Input {
	return Input{
		RequesterEmail: integrationRequester, RequesterName: "Jane Smith", Subject: "Double charge on order 88213",
		Message: "I was charged twice for order 88213.", IssueTag: integrationIssueTag, Priority: freshdesk.TicketPriorityHigh, GroupID: 156,
	}
}

func TestNewIssueOpensOneTicketAndAddsOnePrivateNoteWithRealDex(t *testing.T) {
	provider := newFakeFreshdesk(t)
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "new-issue", integrationIssueInput())
	require.Equal(t, TriageTicketOpened, outcome.Action)
	require.False(t, outcome.NeedsReview)
	require.Equal(t, freshdesk.TicketPriorityHigh, outcome.Ticket.Priority)
	require.Equal(t, freshdesk.TicketStatusOpen, outcome.Ticket.Status)
	require.Equal(t, 1, provider.ticketCount())
	require.Equal(t, 1, provider.count("create"))

	require.Zero(t, provider.count("search"), "a customer without a contact needs no search")
	require.Equal(t, integrationRequester, provider.lastRequest("contacts").query.Get("email"))
	require.JSONEq(t, `{"subject":"Double charge on order 88213","description":"I was charged twice for order 88213.","email":"jane@acme.example.com",
		"name":"Jane Smith","status":2,"priority":3,"tags":["billing-double-charge"],"group_id":156}`, provider.lastRequest("create").body)
	ticket := provider.ticket(outcome.Ticket.ID)
	require.Len(t, ticket.conversations, 1, "exactly one triage note")
	require.True(t, ticket.conversations[0].private)
	require.Equal(t, outcome.NoteID, ticket.conversations[0].id)
	require.Equal(t, BuildOpenedTicketNote(mustCustomerIssue(t, integrationIssueInput())), ticket.conversations[0].bodyText)
}

func TestRepeatContactReprioritizesTheCustomersTicketAndLeavesDecoysUntouchedWithRealDex(t *testing.T) {
	provider := newFakeFreshdesk(t)
	jane := provider.seedContact(integrationRequester)
	existing := provider.seedTicket(jane, 3, 1, []string{integrationIssueTag, "vip"}, "2026-01-26T14:02:00Z")
	resolved := provider.seedTicket(jane, 4, 2, []string{integrationIssueTag}, "2026-01-12T09:00:00Z")
	provider.seedNote(resolved, "Refund already issued on Jan 13, see REF-771.")
	otherCustomer := provider.seedTicket(provider.seedContact("ben@meridian.example.com"), 2, 1, []string{integrationIssueTag}, "2026-01-27T11:00:00Z")
	lookalike := provider.seedTicket(provider.seedContact("jane@acme.example.com.au"), 2, 1, []string{integrationIssueTag}, "2026-01-28T11:00:00Z")
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "repeat-contact", integrationIssueInput())
	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	require.Equal(t, existing, outcome.Ticket.ID)
	require.Equal(t, freshdesk.TicketStatusOpen, outcome.Ticket.Status, "a pending ticket is reopened")
	require.Equal(t, freshdesk.TicketPriorityHigh, outcome.Ticket.Priority)
	require.Equal(t, []string{integrationIssueTag, "vip", RepeatContactTag}, outcome.Ticket.Tags)
	require.False(t, outcome.WasAlreadyApplied)
	require.Equal(t, `"(status:2 OR status:3 OR status:6 OR status:7) AND tag:'billing-double-charge'"`, provider.lastRequest("search").query.Get("query"))
	require.JSONEq(t, `{"status":2,"priority":3,"tags":["billing-double-charge","vip","dex-repeat-contact"]}`, provider.lastRequest("update").body)

	ticket := provider.ticket(existing)
	require.Len(t, ticket.conversations, 1, "exactly one triage note")
	require.True(t, ticket.conversations[0].private)
	require.Equal(t, BuildFollowUpNote(mustCustomerIssue(t, integrationIssueInput())), ticket.conversations[0].bodyText)
	for _, decoy := range []int64{resolved, otherCustomer, lookalike} {
		snapshot := provider.ticket(decoy)
		require.Equal(t, snapshot.seededAt, snapshot.updatedAt, "ticket %d must not be touched", decoy)
	}
	require.Len(t, provider.ticket(resolved).conversations, 1)
	require.Equal(t, 4, provider.ticketCount(), "no ticket was created")
	require.Equal(t, 1, provider.count("update"))
}

func TestStaleSearchMatchIsReadSkippedAndNeverWrittenWithRealDex(t *testing.T) {
	provider := newFakeFreshdesk(t)
	jane := provider.seedContact(integrationRequester)
	stale := provider.seedTicket(jane, 2, 1, []string{"password-reset"}, "2026-01-27T11:00:00Z")
	provider.setSearchIndexTags(stale, []string{integrationIssueTag})
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "stale-search", integrationIssueInput())
	require.Equal(t, TriageTicketOpened, outcome.Action)
	require.Equal(t, stale, outcome.SkippedTicketID, "the read shows the search index was stale")
	require.NotEqual(t, stale, outcome.Ticket.ID)
	require.Zero(t, provider.count("update"))
	require.Empty(t, provider.ticket(stale).conversations)
}

func TestSecondSearchPageIsReadWhenTheFirstHoldsNoMatchWithRealDex(t *testing.T) {
	provider := newFakeFreshdesk(t)
	for index := 0; index < freshdesk.SearchPageSize; index++ {
		provider.seedTicket(provider.seedContact("customer"+strconv.Itoa(index)+"@example.com"), 2, 1, []string{integrationIssueTag}, "2026-01-20T09:00:00Z")
	}
	existing := provider.seedTicket(provider.seedContact(integrationRequester), 2, 1, []string{integrationIssueTag}, "2026-01-26T14:02:00Z")
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "second-page", integrationIssueInput())
	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	require.Equal(t, existing, outcome.Ticket.ID)
	pages := provider.queryValues("search", "page")
	require.Equal(t, []string{"1", "2"}, pages)
}

// TestSlowCreateIsSentOnceWithRealDex is the duplicate-dispatch test: sync durability means
// Dex never dispatches a second attempt while the first create is still in flight.
func TestSlowCreateIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeFreshdesk(t)
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

func TestSlowNoteIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeFreshdesk(t)
	provider.delaysFirstNote = true
	harness := newTriageHarness(t, provider, slowRequestTimeout)

	outcome := harness.runTriage(t, "slow-note", integrationIssueInput())
	require.Equal(t, TriageTicketOpened, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("note"), "no second dispatch while the first was in flight")
	require.Len(t, provider.ticket(outcome.Ticket.ID).conversations, 1)
}

// TestSlowUpdateIsSafeToRepeatWithRealDex lets async Dex dispatch the update again; both attempts write the same values.
func TestSlowUpdateIsSafeToRepeatWithRealDex(t *testing.T) {
	provider := newFakeFreshdesk(t)
	existing := provider.seedTicket(provider.seedContact(integrationRequester), 3, 1, []string{integrationIssueTag}, "2026-01-26T14:02:00Z")
	provider.delaysFirstUpdate = true
	harness := newTriageHarness(t, provider, slowRequestTimeout)

	outcome := harness.runTriage(t, "slow-update", integrationIssueInput())
	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("update"), 2, "Dex dispatched the update again past its local phase")
	ticket := provider.ticket(existing)
	require.Equal(t, 2, ticket.status)
	require.Equal(t, 3, ticket.priority)
	require.Equal(t, []string{integrationIssueTag, RepeatContactTag}, ticket.tags, "the repeated write did not duplicate the tag")
	require.Len(t, ticket.conversations, 1, "exactly one triage note")
	t.Logf("slow update: updates=%d reads=%d", provider.count("update"), provider.count("read"))
}

func TestLostUpdateResponseIsRetriedAndWritesNothingTwiceWithRealDex(t *testing.T) {
	provider := newFakeFreshdesk(t)
	existing := provider.seedTicket(provider.seedContact(integrationRequester), 2, 1, []string{integrationIssueTag}, "2026-01-26T14:02:00Z")
	provider.losesFirstUpdateResponse = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "lost-update", integrationIssueInput())
	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	require.True(t, outcome.WasAlreadyApplied, "the retried attempt found the values applied")
	require.Equal(t, 1, provider.count("update"), "the retry wrote nothing")
	require.Len(t, provider.ticket(existing).conversations, 1)
}

func TestLostCreateResponseSelectsUncertainAndIsNeverResentWithRealDex(t *testing.T) {
	provider := newFakeFreshdesk(t)
	provider.losesFirstCreateResponse = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "lost-create", integrationIssueInput())
	require.Equal(t, TriageTicketCreationUncertain, outcome.Action)
	require.True(t, outcome.NeedsReview)
	require.Equal(t, "createTicket", outcome.ReviewReason)
	require.Equal(t, "Freshdesk request failed before a response arrived", outcome.ReviewDetail)
	require.Equal(t, 1, provider.count("create"), "an unconfirmed create is never resent")
	require.Equal(t, 1, provider.ticketCount(), "Freshdesk did create the ticket, which a person must now find")
	require.Zero(t, provider.count("note"))
}

func TestRateLimitedCreateWaitsAndCreatesOneTicketWithRealDex(t *testing.T) {
	provider := newFakeFreshdesk(t)
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
// Freshdesk holds the create; the next attempt finds the dispatch marker and sends nothing.
func TestLostWorkerDuringCreateSelectsUncertainWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeFreshdesk(t)
	provider.holdsFirstCreate = make(chan struct{})
	harness := newTriageHarness(t, provider, slowRequestTimeout)
	flowID := harness.startTriage(t, "lost-worker", integrationIssueInput())
	require.Eventually(t, func() bool { return provider.count("create") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach Freshdesk")
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

func TestLostNoteResponseSelectsUncertainAndIsNeverResentWithRealDex(t *testing.T) {
	provider := newFakeFreshdesk(t)
	provider.losesFirstNoteResponse = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "lost-note", integrationIssueInput())
	require.Equal(t, TriageTicketOpened, outcome.Action)
	require.True(t, outcome.NeedsReview)
	require.Equal(t, "addNote", outcome.ReviewReason)
	require.Zero(t, outcome.NoteID)
	require.Equal(t, 1, provider.count("note"), "an unconfirmed note is never resent")
	require.Len(t, provider.ticket(outcome.Ticket.ID).conversations, 1)
}

func TestRejectedTicketFailsTheFlowWithoutFreshdeskTextWithRealDex(t *testing.T) {
	provider := newFakeFreshdesk(t)
	provider.rejectsCreate = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)
	flowID := harness.startTriage(t, "rejected", integrationIssueInput())

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	require.NotContains(t, result.ErrorMessage, integrationAPIKey)
	require.Equal(t, 1, provider.count("create"), "a conclusive rejection is not retried")
	require.Zero(t, provider.ticketCount())
	t.Logf("rejected create failure: %s", result.ErrorMessage)
}

func TestInvalidIssueFailsBeforeCallingFreshdeskWithRealDex(t *testing.T) {
	provider := newFakeFreshdesk(t)
	harness := newTriageHarness(t, provider, defaultRequestTimeout)
	input := integrationIssueInput()
	input.Priority = 5
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

// fakeFreshdesk is a stateful Freshdesk API v2 fake without idempotency keys or tags in search results.
type fakeFreshdesk struct {
	*httptest.Server
	t     *testing.T
	mutex sync.Mutex
	// delayedRequests tracks handlers still sleeping, so a test can assert their final effect.
	delayedRequests sync.WaitGroup

	clock              time.Time
	nextTicketID       int64
	nextContactID      int64
	nextConversationID int64
	contacts           map[int64]string
	tickets            map[int64]*fakeTicket
	counts             map[string]int
	requests           map[string][]fakeRecordedRequest

	delaysFirstCreate        bool
	delaysFirstNote          bool
	delaysFirstUpdate        bool
	losesFirstCreateResponse bool
	losesFirstUpdateResponse bool
	losesFirstNoteResponse   bool
	rateLimitsFirstCreate    bool
	rejectsCreate            bool
	holdsFirstCreate         chan struct{}
}

type fakeTicket struct {
	id              int64
	subject         string
	descriptionText string
	status          int
	priority        int
	requesterID     int64
	responderID     int64
	groupID         int64
	tags            []string
	searchIndexTags []string
	createdAt       time.Time
	seededAt        time.Time
	updatedAt       time.Time
	conversations   []fakeConversation
}

type fakeConversation struct {
	id        int64
	userID    int64
	private   bool
	source    int
	bodyText  string
	createdAt time.Time
}

type fakeRecordedRequest struct {
	at    time.Time
	query url.Values
	body  string
}

func newFakeFreshdesk(t *testing.T) *fakeFreshdesk {
	t.Helper()
	provider := &fakeFreshdesk{
		t: t, clock: time.Date(2026, 1, 28, 9, 0, 0, 0, time.UTC), nextTicketID: 1200, nextContactID: 6007738000, nextConversationID: 1,
		contacts: map[int64]string{}, tickets: map[int64]*fakeTicket{}, counts: map[string]int{}, requests: map[string][]fakeRecordedRequest{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeFreshdesk) serveHTTP(response http.ResponseWriter, request *http.Request) {
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(integrationAPIKey+":X"))
	if request.Header.Get("Authorization") != expected {
		provider.writeJSON(response, http.StatusUnauthorized, `{"code":"invalid_credentials","message":"You have to be logged in to perform this action."}`)
		return
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		provider.writeJSON(response, http.StatusBadRequest, `{"code":"invalid_json"}`)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/api/v2")
	switch {
	case request.Method == http.MethodGet && path == "/contacts":
		provider.listContacts(response, request, body)
	case request.Method == http.MethodGet && path == "/search/tickets":
		provider.filterTickets(response, request, body)
	case request.Method == http.MethodPost && path == "/tickets":
		provider.createTicket(response, request, body)
	case strings.HasPrefix(path, "/tickets/"):
		segments := strings.Split(strings.TrimPrefix(path, "/tickets/"), "/")
		ticketID, err := strconv.ParseInt(segments[0], 10, 64)
		if err != nil {
			provider.writeJSON(response, http.StatusNotFound, ``)
			return
		}
		switch {
		case request.Method == http.MethodGet && len(segments) == 1:
			provider.readTicket(response, request, body, ticketID)
		case request.Method == http.MethodGet && len(segments) == 2 && segments[1] == "conversations":
			provider.listConversations(response, request, body, ticketID)
		case request.Method == http.MethodPut && len(segments) == 1:
			provider.updateTicket(response, request, body, ticketID)
		case request.Method == http.MethodPost && len(segments) == 2 && segments[1] == "notes":
			provider.addNote(response, request, body, ticketID)
		default:
			provider.writeJSON(response, http.StatusNotFound, ``)
		}
	default:
		provider.writeJSON(response, http.StatusNotFound, ``)
	}
}

func (provider *fakeFreshdesk) listContacts(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("contacts", request, body)
	email := request.URL.Query().Get("email")
	provider.mutex.Lock()
	contacts := []any{}
	for id, known := range provider.contacts {
		if strings.EqualFold(known, email) {
			contacts = append(contacts, map[string]any{"id": id, "name": "Contact", "email": known})
		}
	}
	provider.mutex.Unlock()
	provider.writeValue(response, http.StatusOK, contacts)
}

func (provider *fakeFreshdesk) filterTickets(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("search", request, body)
	query := request.URL.Query().Get("query")
	page, err := strconv.Atoi(request.URL.Query().Get("page"))
	if !strings.HasPrefix(query, `"`) || !strings.HasSuffix(query, `"`) || len(query) > 512 || err != nil || page < 1 || page > 10 {
		provider.writeJSON(response, http.StatusBadRequest, `{"description":"Validation failed","errors":[{"field":"query","message":"SENTINEL","code":"invalid_value"}]}`)
		return
	}
	var statuses []int
	for _, match := range statusTermPattern.FindAllStringSubmatch(query, -1) {
		status, _ := strconv.Atoi(match[1])
		statuses = append(statuses, status)
	}
	var tags []string
	for _, match := range tagTermPattern.FindAllStringSubmatch(query, -1) {
		tags = append(tags, match[1])
	}
	provider.mutex.Lock()
	var matches []any
	for _, ticket := range provider.sortedTickets() {
		indexedTags := ticket.tags
		if ticket.searchIndexTags != nil {
			indexedTags = ticket.searchIndexTags
		}
		if (len(statuses) != 0 && !containsInt(statuses, ticket.status)) || (len(tags) != 0 && !overlaps(tags, indexedTags)) {
			continue
		}
		result := provider.ticketJSON(ticket)
		delete(result, "tags")
		matches = append(matches, result)
	}
	provider.mutex.Unlock()
	start, end := min((page-1)*30, len(matches)), min(page*30, len(matches))
	results := append([]any{}, matches[start:end]...)
	provider.writeValue(response, http.StatusOK, map[string]any{"total": len(matches), "results": results})
}

func (provider *fakeFreshdesk) readTicket(response http.ResponseWriter, request *http.Request, body []byte, ticketID int64) {
	provider.record("read", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.writeJSON(response, http.StatusNotFound, ``)
		return
	}
	value := provider.ticketJSON(ticket)
	if request.URL.Query().Get("include") == "requester" {
		value["requester"] = map[string]any{"id": ticket.requesterID, "name": "Requester", "email": provider.contacts[ticket.requesterID], "mobile": nil, "phone": nil}
	}
	provider.writeValue(response, http.StatusOK, value)
}

func (provider *fakeFreshdesk) listConversations(response http.ResponseWriter, request *http.Request, body []byte, ticketID int64) {
	provider.record("conversations", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.writeJSON(response, http.StatusNotFound, ``)
		return
	}
	perPage, err := strconv.Atoi(request.URL.Query().Get("per_page"))
	if err != nil || perPage < 1 || perPage > 100 || request.URL.Query().Get("page") != "1" {
		provider.writeJSON(response, http.StatusBadRequest, `{"errors":[{"field":"per_page","code":"invalid_value"}]}`)
		return
	}
	conversations := []any{}
	for _, conversation := range ticket.conversations[:min(perPage, len(ticket.conversations))] {
		conversations = append(conversations, map[string]any{
			"id": conversation.id, "body": "<div>" + conversation.bodyText + "</div>", "body_text": conversation.bodyText,
			"private": conversation.private, "incoming": false, "source": conversation.source, "user_id": conversation.userID,
			"ticket_id": ticketID, "created_at": conversation.createdAt.Format(time.RFC3339),
		})
	}
	if len(ticket.conversations) > perPage {
		response.Header().Set("Link", `<`+provider.URL+request.URL.Path+`?page=2>; rel="next"`)
	}
	provider.writeValue(response, http.StatusOK, conversations)
}

func (provider *fakeFreshdesk) createTicket(response http.ResponseWriter, request *http.Request, body []byte) {
	attempt := provider.record("create", request, body)
	if provider.rejectsCreate {
		provider.writeJSON(response, http.StatusBadRequest, `{"description":"SENTINEL Validation failed","errors":[{"field":"group_id","message":"SENTINEL There is no group matching the given group_id","code":"invalid_value"}]}`)
		return
	}
	if provider.rateLimitsFirstCreate && attempt == 1 {
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"code":"too_many_requests","message":"SENTINEL"}`)
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
		Subject     string   `json:"subject"`
		Description string   `json:"description"`
		Email       string   `json:"email"`
		Status      int      `json:"status"`
		Priority    int      `json:"priority"`
		Tags        []string `json:"tags"`
		GroupID     int64    `json:"group_id"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	provider.mutex.Lock()
	requesterID := provider.contactIDForEmail(payload.Email)
	ticket := provider.storeTicket(requesterID, max(payload.Status, 2), max(payload.Priority, 1), payload.Tags, provider.advanceClock())
	ticket.subject, ticket.descriptionText, ticket.groupID = payload.Subject, payload.Description, payload.GroupID
	value := provider.ticketJSON(ticket)
	provider.mutex.Unlock()
	if provider.losesFirstCreateResponse && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusCreated, value)
}

func (provider *fakeFreshdesk) updateTicket(response http.ResponseWriter, request *http.Request, body []byte, ticketID int64) {
	attempt := provider.record("update", request, body)
	if provider.delaysFirstUpdate && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	var payload struct {
		Status      int       `json:"status"`
		Priority    int       `json:"priority"`
		GroupID     int64     `json:"group_id"`
		ResponderID int64     `json:"responder_id"`
		Tags        *[]string `json:"tags"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	provider.mutex.Lock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusNotFound, ``)
		return
	}
	if payload.Status != 0 {
		ticket.status = payload.Status
	}
	if payload.Priority != 0 {
		ticket.priority = payload.Priority
	}
	if payload.GroupID != 0 {
		ticket.groupID = payload.GroupID
	}
	if payload.ResponderID != 0 {
		ticket.responderID = payload.ResponderID
	}
	if payload.Tags != nil {
		ticket.tags = append([]string(nil), (*payload.Tags)...)
	}
	ticket.updatedAt = provider.advanceClock()
	provider.counts["appliedUpdate"]++
	value := provider.ticketJSON(ticket)
	provider.mutex.Unlock()
	if provider.losesFirstUpdateResponse && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusOK, value)
}

func (provider *fakeFreshdesk) addNote(response http.ResponseWriter, request *http.Request, body []byte, ticketID int64) {
	attempt := provider.record("note", request, body)
	if provider.delaysFirstNote && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	var payload struct {
		Body    string `json:"body"`
		Private *bool  `json:"private"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	provider.mutex.Lock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusNotFound, ``)
		return
	}
	isPrivate := payload.Private == nil || *payload.Private
	createdAt := provider.advanceClock()
	conversation := fakeConversation{
		id: provider.nextConversationID, userID: agentUserID, private: isPrivate, source: 2,
		bodyText: strings.ReplaceAll(unescapeHTML(payload.Body), "<br>", "\n"), createdAt: createdAt,
	}
	provider.nextConversationID++
	ticket.conversations = append(ticket.conversations, conversation)
	ticket.updatedAt = createdAt
	provider.mutex.Unlock()
	if provider.losesFirstNoteResponse && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusCreated, map[string]any{
		"id": conversation.id, "body": "<div>" + payload.Body + "</div>", "body_text": conversation.bodyText, "incoming": false,
		"private": isPrivate, "user_id": agentUserID, "support_email": nil, "ticket_id": ticketID, "notified_to": []string{},
		"created_at": createdAt.Format(time.RFC3339), "updated_at": createdAt.Format(time.RFC3339),
	})
}

func unescapeHTML(value string) string {
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&#34;", `"`, "&#39;", "'", "&amp;", "&").Replace(value)
}

// dropConnection closes the connection after Freshdesk applied the request, as a lost response would.
func (provider *fakeFreshdesk) dropConnection(response http.ResponseWriter) {
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(provider.t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(provider.t, err)
	require.NoError(provider.t, connection.Close())
}

func (provider *fakeFreshdesk) seedContact(email string) int64 {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.contactIDForEmail(email)
}

func (provider *fakeFreshdesk) seedTicket(requesterID int64, status int, priority int, tags []string, updatedAt string) int64 {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	instant, err := time.Parse(time.RFC3339, updatedAt)
	require.NoError(provider.t, err)
	ticket := provider.storeTicket(requesterID, status, priority, tags, instant)
	ticket.subject, ticket.descriptionText, ticket.seededAt = "Seeded ticket", "Seeded description.", instant
	return ticket.id
}

func (provider *fakeFreshdesk) seedNote(ticketID int64, bodyText string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket := provider.tickets[ticketID]
	ticket.conversations = append(ticket.conversations, fakeConversation{
		id: provider.nextConversationID, userID: agentUserID, private: true, source: 2, bodyText: bodyText, createdAt: ticket.updatedAt,
	})
	provider.nextConversationID++
}

// setSearchIndexTags makes the search index disagree with the ticket, as Freshdesk's indexing lag can.
func (provider *fakeFreshdesk) setSearchIndexTags(ticketID int64, tags []string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.tickets[ticketID].searchIndexTags = tags
}

// storeTicket requires provider.mutex.
func (provider *fakeFreshdesk) storeTicket(requesterID int64, status int, priority int, tags []string, createdAt time.Time) *fakeTicket {
	provider.nextTicketID++
	ticket := &fakeTicket{
		id: provider.nextTicketID, status: status, priority: priority, requesterID: requesterID,
		tags: append([]string{}, tags...), createdAt: createdAt, updatedAt: createdAt,
	}
	provider.tickets[ticket.id] = ticket
	return ticket
}

// contactIDForEmail requires provider.mutex; like Freshdesk, an unknown email adds a contact.
func (provider *fakeFreshdesk) contactIDForEmail(email string) int64 {
	for id, known := range provider.contacts {
		if strings.EqualFold(known, email) {
			return id
		}
	}
	provider.nextContactID++
	provider.contacts[provider.nextContactID] = email
	return provider.nextContactID
}

// advanceClock requires provider.mutex; every write moves updated_at forward by one minute.
func (provider *fakeFreshdesk) advanceClock() time.Time {
	provider.clock = provider.clock.Add(time.Minute)
	return provider.clock
}

// sortedTickets requires provider.mutex.
func (provider *fakeFreshdesk) sortedTickets() []*fakeTicket {
	tickets := make([]*fakeTicket, 0, len(provider.tickets))
	for _, ticket := range provider.tickets {
		tickets = append(tickets, ticket)
	}
	sort.Slice(tickets, func(left, right int) bool { return tickets[left].id < tickets[right].id })
	return tickets
}

// ticketJSON requires provider.mutex.
func (provider *fakeFreshdesk) ticketJSON(ticket *fakeTicket) map[string]any {
	return map[string]any{
		"id": ticket.id, "subject": ticket.subject, "description": "<div>" + ticket.descriptionText + "</div>",
		"description_text": ticket.descriptionText, "status": ticket.status, "priority": ticket.priority, "source": 2, "type": nil,
		"requester_id": ticket.requesterID, "responder_id": nullableID(ticket.responderID), "group_id": nullableID(ticket.groupID),
		"company_id": nil, "product_id": nil, "tags": append([]string{}, ticket.tags...), "spam": false, "is_escalated": false,
		"cc_emails": []string{}, "custom_fields": map[string]any{}, "created_at": ticket.createdAt.Format(time.RFC3339),
		"updated_at": ticket.updatedAt.Format(time.RFC3339), "due_by": nil, "fr_due_by": nil,
	}
}

func (provider *fakeFreshdesk) record(name string, request *http.Request, body []byte) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[name]++
	provider.requests[name] = append(provider.requests[name], fakeRecordedRequest{at: time.Now(), query: request.URL.Query(), body: string(body)})
	return provider.counts[name]
}

func (provider *fakeFreshdesk) writeValue(response http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	require.NoError(provider.t, err)
	provider.writeJSON(response, status, string(encoded))
}

func (provider *fakeFreshdesk) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("X-Request-Id", "fake-request")
	response.WriteHeader(status)
	if _, err := io.WriteString(response, body); err != nil {
		provider.t.Logf("fake Freshdesk response write failed: %v", err)
	}
}

func (provider *fakeFreshdesk) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * slowResponseDelay):
		t.Fatal("a delayed fake Freshdesk request did not finish")
	}
}

func (provider *fakeFreshdesk) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakeFreshdesk) totalRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, requests := range provider.requests {
		total += len(requests)
	}
	return total
}

func (provider *fakeFreshdesk) ticketCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.tickets)
}

func (provider *fakeFreshdesk) ticket(ticketID int64) fakeTicket {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket := *provider.tickets[ticketID]
	ticket.conversations = append([]fakeConversation(nil), ticket.conversations...)
	ticket.tags = append([]string(nil), ticket.tags...)
	return ticket
}

func (provider *fakeFreshdesk) lastRequest(name string) fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	requests := provider.requests[name]
	require.NotEmpty(provider.t, requests, name)
	return requests[len(requests)-1]
}

func (provider *fakeFreshdesk) queryValues(name string, key string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var values []string
	for _, request := range provider.requests[name] {
		values = append(values, request.query.Get(key))
	}
	return values
}

func (provider *fakeFreshdesk) requestTimes(name string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var times []time.Time
	for _, request := range provider.requests[name] {
		times = append(times, request.at)
	}
	return times
}

func containsInt(values []int, value int) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func overlaps(wanted []string, actual []string) bool {
	for _, value := range wanted {
		for _, candidate := range actual {
			if strings.EqualFold(candidate, value) {
				return true
			}
		}
	}
	return false
}

func nullableID(value int64) any {
	if value == 0 {
		return nil
	}
	return value
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

func newTriageHarness(t *testing.T, provider *fakeFreshdesk, requestTimeout time.Duration) *triageHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "freshdesk", Name: ConnectionName}
	providerClient, err := freshdesk.New(freshdesk.Config{Domain: integrationDomain},
		sdkgo.StaticCredentialProvider[freshdesk.Credentials]{reference: {APIKey: sdkgo.NewSecretString(integrationAPIKey)}},
		freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"), freshdesk.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := freshdesk.NewConnection(providerClient, reference)
	require.NoError(t, err)
	harness := &triageHarness{flow: NewFlow(connection), serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
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
	flowID := "freshdesk-triage-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
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
