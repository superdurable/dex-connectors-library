//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageissue

import (
	"cmp"
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
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationDomain    = "acme"
	integrationEmail     = "agent@acme-shop.example.com"
	integrationAPIKey    = "gorgiasIntegrationKey0123456789"
	integrationRequester = "jane@acme.example.com"
	integrationIssueTag  = "billing-double-charge"
	supportAddress       = "support@acme-shop.example.com"
	acknowledgement      = "Thanks Jane, we are refunding the duplicate charge."

	defaultRequestTimeout = 5 * time.Second
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowResponseDelay  = 9 * time.Second
	slowRequestTimeout = 20 * time.Second
	fakeTimestamp      = "2006-01-02T15:04:05.000000"
)

func integrationIssueInput() Input {
	return Input{
		RequesterEmail: integrationRequester, RequesterName: "Jane Smith", Subject: "Double charge on order 88213",
		Message: "I was charged twice for order 88213.", IssueTag: integrationIssueTag, Priority: gorgias.TicketPriorityHigh,
	}
}

func TestNewCustomerOpensOneTicketAndAddsOneInternalNoteWithRealDex(t *testing.T) {
	provider := newFakeGorgias(t)
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "new-customer", integrationIssueInput())
	require.Equal(t, TriageTicketOpened, outcome.Action)
	require.False(t, outcome.NeedsReview)
	require.False(t, outcome.WasTicketCreatedByEarlierAttempt)
	require.Equal(t, gorgias.TicketPriorityHigh, outcome.Ticket.Priority)
	require.Equal(t, gorgias.TicketStatusOpen, outcome.Ticket.Status)
	require.Equal(t, 1, provider.ticketCount())
	require.Equal(t, 1, provider.count("create"))
	require.Zero(t, provider.count("list"), "a customer Gorgias does not know needs no ticket list")
	require.Equal(t, integrationRequester, provider.lastRequest("customers").query.Get("email"))

	create := provider.lastRequest("create").body
	externalID := outcome.Ticket.ExternalID
	require.True(t, strings.HasPrefix(externalID, "dex-"))
	require.JSONEq(t, `{"customer":{"email":"jane@acme.example.com","name":"Jane Smith"},"subject":"Double charge on order 88213","channel":"api",
		"via":"api","from_agent":false,"status":"open","priority":"high","tags":[{"name":"billing-double-charge"}],"external_id":"`+externalID+`",
		"messages":[{"channel":"api","via":"api","from_agent":false,"sender":{"email":"jane@acme.example.com","name":"Jane Smith"},
		"subject":"Double charge on order 88213","body_text":"I was charged twice for order 88213.","body_html":"I was charged twice for order 88213.",
		"stripped_text":"I was charged twice for order 88213."}]}`, create)
	ticket := provider.ticket(outcome.Ticket.ID)
	notes := ticket.messagesOn(gorgias.MessageChannelInternalNote)
	require.Len(t, notes, 1, "exactly one triage note")
	require.Equal(t, outcome.NoteID, notes[0].id)
	require.Equal(t, integrationEmail, notes[0].senderEmail)
	require.Equal(t, BuildOpenedTicketNote(mustCustomerIssue(t, integrationIssueInput())), notes[0].bodyText)
	require.Zero(t, outcome.AcknowledgementID, "a new ticket gets no acknowledgement")
}

func TestRepeatContactReprioritizesRepliesAndLeavesDecoysUntouchedWithRealDex(t *testing.T) {
	provider := newFakeGorgias(t)
	jane := provider.seedCustomer(integrationRequester)
	existing := provider.seedTicket(jane, "open", "normal", []string{integrationIssueTag, "vip"}, "2026-01-26T14:02:00")
	closed := provider.seedTicket(jane, "closed", "normal", []string{integrationIssueTag}, "2026-01-27T09:00:00")
	provider.seedNote(closed, "Refund already issued on Jan 13, see REF-771.")
	otherTag := provider.seedTicket(jane, "open", "low", []string{"password-reset"}, "2026-01-28T09:00:00")
	otherCustomer := provider.seedTicket(provider.seedCustomer("ben@meridian.example.com"), "open", "low", []string{integrationIssueTag}, "2026-01-27T11:00:00")
	lookalike := provider.seedTicket(provider.seedCustomer("jane@acme.example.com.au"), "open", "low", []string{integrationIssueTag}, "2026-01-28T11:00:00")
	harness := newTriageHarness(t, provider, defaultRequestTimeout)
	input := integrationIssueInput()
	input.Acknowledgement = acknowledgement

	outcome := harness.runTriage(t, "repeat-contact", input)
	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	require.Equal(t, existing, outcome.Ticket.ID)
	require.Equal(t, gorgias.TicketPriorityHigh, outcome.Ticket.Priority)
	require.Equal(t, []string{integrationIssueTag, "vip", RepeatContactTag}, outcome.Ticket.Tags)
	require.False(t, outcome.WasAlreadyApplied)
	list := provider.lastRequest("list").query
	require.Equal(t, strconv.FormatInt(jane, 10), list.Get("customer_id"))
	require.Equal(t, "updated_datetime:desc", list.Get("order_by"))
	require.Equal(t, "false", list.Get("trashed"))
	require.Equal(t, "100", list.Get("limit"))
	require.JSONEq(t, `{"names":["dex-repeat-contact"]}`, provider.lastRequest("addTags").body)
	require.JSONEq(t, `{"priority":"high"}`, provider.lastRequest("update").body, "the ticket was already open")

	ticket := provider.ticket(existing)
	require.Len(t, ticket.messagesOn(gorgias.MessageChannelInternalNote), 1, "exactly one triage note")
	replies := ticket.agentEmails()
	require.Len(t, replies, 1, "exactly one acknowledgement")
	require.Equal(t, outcome.AcknowledgementID, replies[0].id)
	require.Equal(t, acknowledgement, replies[0].bodyText)
	require.JSONEq(t, `{"channel":"email","via":"api","from_agent":true,"public":true,"sender":{"email":"`+integrationEmail+`"},
		"receiver":{"email":"jane@acme.example.com"},"source":{"type":"email","from":{"address":"`+supportAddress+`"},
		"to":[{"address":"jane@acme.example.com"}]},"subject":"Re: Seeded ticket","body_text":"`+acknowledgement+`",
		"body_html":"`+acknowledgement+`","external_id":"`+replies[0].externalID+`"}`, provider.lastRequest("reply").body)
	for _, decoy := range []int64{closed, otherTag, otherCustomer, lookalike} {
		snapshot := provider.ticket(decoy)
		require.Equal(t, snapshot.seededAt, snapshot.updatedAt, "ticket %d must not be touched", decoy)
	}
	require.Len(t, provider.ticket(closed).messages, 2)
	require.Equal(t, 5, provider.ticketCount(), "no ticket was created")
}

func TestSecondPageIsReadWhenTheFirstHoldsNoMatchWithRealDex(t *testing.T) {
	provider := newFakeGorgias(t)
	jane := provider.seedCustomer(integrationRequester)
	for index := 0; index < gorgias.MaxSearchPageSize; index++ {
		provider.seedTicket(jane, "closed", "normal", []string{integrationIssueTag}, "2026-01-28T09:"+twoDigits(index%60)+":00")
	}
	existing := provider.seedTicket(jane, "open", "normal", []string{integrationIssueTag}, "2026-01-20T14:02:00")
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "second-page", integrationIssueInput())
	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	require.Equal(t, existing, outcome.Ticket.ID)
	cursors := provider.queryValues("list", "cursor")
	require.Len(t, cursors, 2)
	require.Empty(t, cursors[0])
	require.NotEmpty(t, cursors[1])
}

func twoDigits(value int) string {
	return strings.TrimPrefix(strconv.Itoa(100+value), "1")
}

// TestSlowCreateIsSentOnceWithRealDex proves sync durability keeps Dex from dispatching a second create while the first is in flight.
func TestSlowCreateIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeGorgias(t)
	provider.delaysFirstCreate = true
	harness := newTriageHarness(t, provider, slowRequestTimeout)

	startedAt := time.Now()
	outcome := harness.runTriage(t, "slow-create", integrationIssueInput())
	require.GreaterOrEqual(t, time.Since(startedAt), slowResponseDelay)
	require.Equal(t, TriageTicketOpened, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("create"), "no second dispatch while the first was in flight")
	require.Zero(t, provider.count("lookup"))
	require.Equal(t, 1, provider.ticketCount())
}

func TestSlowNoteIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeGorgias(t)
	provider.delaysFirstNote = true
	harness := newTriageHarness(t, provider, slowRequestTimeout)

	outcome := harness.runTriage(t, "slow-note", integrationIssueInput())
	require.Equal(t, TriageTicketOpened, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("note"), "no second dispatch while the first was in flight")
	require.Len(t, provider.ticket(outcome.Ticket.ID).messagesOn(gorgias.MessageChannelInternalNote), 1)
}

// TestSlowUpdateIsSafeToRepeatWithRealDex lets async Dex dispatch the update again; both attempts write the same values.
func TestSlowUpdateIsSafeToRepeatWithRealDex(t *testing.T) {
	provider := newFakeGorgias(t)
	existing := provider.seedTicket(provider.seedCustomer(integrationRequester), "open", "normal", []string{integrationIssueTag}, "2026-01-26T14:02:00")
	provider.delaysFirstUpdate = true
	harness := newTriageHarness(t, provider, slowRequestTimeout)

	outcome := harness.runTriage(t, "slow-update", integrationIssueInput())
	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("update"), 2, "Dex dispatched the update again past its local phase")
	ticket := provider.ticket(existing)
	require.Equal(t, "open", ticket.status)
	require.Equal(t, "high", ticket.priority)
	require.Equal(t, []string{integrationIssueTag, RepeatContactTag}, ticket.tags, "the repeated write did not duplicate the tag")
	require.Len(t, ticket.messagesOn(gorgias.MessageChannelInternalNote), 1, "exactly one triage note")
	t.Logf("slow update: updates=%d addTags=%d reads=%d", provider.count("update"), provider.count("addTags"), provider.count("read"))
}

func TestLostCreateResponseIsFoundByExternalIDAndNeverResentWithRealDex(t *testing.T) {
	provider := newFakeGorgias(t)
	provider.losesFirstCreateResponse = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "lost-create", integrationIssueInput())
	require.Equal(t, TriageTicketOpened, outcome.Action)
	require.False(t, outcome.NeedsReview)
	require.True(t, outcome.WasTicketCreatedByEarlierAttempt, "the retry found the ticket by its external ID")
	require.Equal(t, 1, provider.count("create"), "an unconfirmed create is never resent")
	require.Equal(t, 1, provider.count("lookup"))
	require.Equal(t, 1, provider.ticketCount())
	require.Len(t, provider.ticket(outcome.Ticket.ID).messagesOn(gorgias.MessageChannelInternalNote), 1)
}

func TestFailedCreateThatGorgiasDoesNotShowNeedsReviewWithRealDex(t *testing.T) {
	provider := newFakeGorgias(t)
	provider.failsFirstCreateWithoutStoring = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "failed-create", integrationIssueInput())
	require.Equal(t, TriageTicketCreationUncertain, outcome.Action)
	require.True(t, outcome.NeedsReview)
	require.Equal(t, "createTicket", outcome.ReviewReason)
	require.Equal(t, "an earlier attempt of this Step sent the request and Gorgias shows no ticket with its external ID, so it is not sent again", outcome.ReviewDetail)
	require.Equal(t, 1, provider.count("create"), "a create Gorgias may still apply is never resent")
	require.Equal(t, 3, provider.count("lookup"), "the read-only lookup was repeated within its budget before uncertain")
	require.Zero(t, provider.ticketCount())
	require.Zero(t, provider.count("note"))
}

func TestLostReplyResponseIsFoundByExternalIDAndNeverResentWithRealDex(t *testing.T) {
	provider := newFakeGorgias(t)
	provider.seedTicket(provider.seedCustomer(integrationRequester), "open", "normal", []string{integrationIssueTag}, "2026-01-26T14:02:00")
	provider.losesFirstReplyResponse = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)
	input := integrationIssueInput()
	input.Acknowledgement = acknowledgement

	outcome := harness.runTriage(t, "lost-reply", input)
	require.Equal(t, TriageTicketFollowedUp, outcome.Action)
	require.False(t, outcome.NeedsReview)
	require.Equal(t, 1, provider.count("reply"), "a second reply would be a second email to the customer")
	replies := provider.ticket(outcome.Ticket.ID).agentEmails()
	require.Len(t, replies, 1)
	require.Equal(t, replies[0].id, outcome.AcknowledgementID)
}

func TestRateLimitedCreateWaitsAndCreatesOneTicketWithRealDex(t *testing.T) {
	provider := newFakeGorgias(t)
	provider.rateLimitsFirstCreate = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "rate-limited", integrationIssueInput())
	require.Equal(t, TriageTicketOpened, outcome.Action, "a 429 clears the dispatch marker, so the retry may send")
	require.False(t, outcome.WasTicketCreatedByEarlierAttempt)
	times := provider.requestTimes("create")
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), time.Second, "the retry waited for Retry-After")
	require.Equal(t, 1, provider.count("lookup"), "the retry looked for the ticket before sending again")
	require.Equal(t, 1, provider.ticketCount())
}

// TestLostWorkerDuringCreateFindsTheTicketWithoutResendingWithRealDex replaces the Worker while Gorgias holds a stored create; the next attempt finds it.
func TestLostWorkerDuringCreateFindsTheTicketWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeGorgias(t)
	provider.holdsFirstCreate = make(chan struct{})
	harness := newTriageHarness(t, provider, slowRequestTimeout)
	flowID := harness.startTriage(t, "lost-worker", integrationIssueInput())
	require.Eventually(t, func() bool { return provider.count("create") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach Gorgias")
	harness.replaceWorker(t)

	result := harness.waitForFlow(t, flowID)
	close(provider.holdsFirstCreate)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome TriageOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, TriageTicketOpened, outcome.Action)
	require.True(t, outcome.WasTicketCreatedByEarlierAttempt, "the new Worker's attempt found the marker the lost attempt recorded")
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("create"), "the attempt on the new Worker did not resend the create")
	require.Equal(t, 1, provider.ticketCount())
}

func TestRejectedTicketFailsTheFlowWithoutGorgiasTextWithRealDex(t *testing.T) {
	provider := newFakeGorgias(t)
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

func TestInvalidIssueFailsBeforeCallingGorgiasWithRealDex(t *testing.T) {
	provider := newFakeGorgias(t)
	harness := newTriageHarness(t, provider, defaultRequestTimeout)
	input := integrationIssueInput()
	input.Priority = "urgent"
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

// fakeGorgias is a stateful Gorgias REST API fake without idempotency keys, filtering tickets only by customer.
type fakeGorgias struct {
	*httptest.Server
	t     *testing.T
	mutex sync.Mutex
	// delayedRequests tracks handlers still sleeping, so a test can assert their final effect.
	delayedRequests sync.WaitGroup

	clock          time.Time
	nextTicketID   int64
	nextCustomerID int64
	nextMessageID  int64
	customers      map[int64]string
	tickets        map[int64]*fakeTicket
	counts         map[string]int
	requests       map[string][]fakeRecordedRequest

	delaysFirstCreate              bool
	delaysFirstNote                bool
	delaysFirstUpdate              bool
	losesFirstCreateResponse       bool
	failsFirstCreateWithoutStoring bool
	losesFirstReplyResponse        bool
	rateLimitsFirstCreate          bool
	rejectsCreate                  bool
	holdsFirstCreate               chan struct{}
}

type fakeTicket struct {
	id           int64
	subject      string
	status       string
	priority     string
	customerID   int64
	assigneeUser int64
	assigneeTeam int64
	tags         []string
	externalID   string
	createdAt    time.Time
	seededAt     time.Time
	updatedAt    time.Time
	messages     []fakeMessage
}

type fakeMessage struct {
	id          int64
	channel     string
	isFromAgent bool
	senderEmail string
	fromAddress string
	toAddress   string
	subject     string
	bodyText    string
	externalID  string
	createdAt   time.Time
}

type fakeRecordedRequest struct {
	at    time.Time
	query url.Values
	body  string
}

func (ticket fakeTicket) messagesOn(channel string) []fakeMessage {
	var matches []fakeMessage
	for _, message := range ticket.messages {
		if message.channel == channel {
			matches = append(matches, message)
		}
	}
	return matches
}

func (ticket fakeTicket) agentEmails() []fakeMessage {
	var matches []fakeMessage
	for _, message := range ticket.messagesOn(gorgias.MessageChannelEmail) {
		if message.isFromAgent {
			matches = append(matches, message)
		}
	}
	return matches
}

func newFakeGorgias(t *testing.T) *fakeGorgias {
	t.Helper()
	provider := &fakeGorgias{
		t: t, clock: time.Date(2026, 1, 28, 12, 0, 0, 0, time.UTC), nextTicketID: 5500, nextCustomerID: 3900, nextMessageID: 9000,
		customers: map[int64]string{}, tickets: map[int64]*fakeTicket{}, counts: map[string]int{}, requests: map[string][]fakeRecordedRequest{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeGorgias) serveHTTP(response http.ResponseWriter, request *http.Request) {
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(integrationEmail+":"+integrationAPIKey))
	if request.Header.Get("Authorization") != expected {
		provider.writeJSON(response, http.StatusUnauthorized, `{"error":{"msg":"Unauthorized"}}`)
		return
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		provider.writeJSON(response, http.StatusBadRequest, `{"error":{"msg":"invalid_json"}}`)
		return
	}
	path := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/api"), "/")
	segments := strings.Split(strings.TrimPrefix(path, "/tickets/"), "/")
	ticketID, idErr := strconv.ParseInt(segments[0], 10, 64)
	switch {
	case request.Method == http.MethodGet && path == "/customers":
		provider.listCustomers(response, request, body)
	case request.Method == http.MethodGet && path == "/tickets":
		provider.listTickets(response, request, body)
	case request.Method == http.MethodPost && path == "/tickets":
		provider.createTicket(response, request, body)
	case !strings.HasPrefix(path, "/tickets/") || idErr != nil:
		provider.writeJSON(response, http.StatusNotFound, `{"error":{"msg":"Not found"}}`)
	case request.Method == http.MethodGet && len(segments) == 1:
		provider.readTicket(response, request, body, ticketID)
	case request.Method == http.MethodPut && len(segments) == 1:
		provider.updateTicket(response, request, body, ticketID)
	case len(segments) == 2 && segments[1] == "tags" && (request.Method == http.MethodPost || request.Method == http.MethodDelete):
		provider.changeTags(response, request, body, ticketID)
	case request.Method == http.MethodPost && len(segments) == 2 && segments[1] == "messages":
		provider.addMessage(response, request, body, ticketID)
	default:
		provider.writeJSON(response, http.StatusNotFound, `{"error":{"msg":"Not found"}}`)
	}
}

func (provider *fakeGorgias) listCustomers(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("customers", request, body)
	email := request.URL.Query().Get("email")
	provider.mutex.Lock()
	customers := []any{}
	for id, known := range provider.customers {
		if strings.EqualFold(known, email) {
			customers = append(customers, map[string]any{"id": id, "email": known, "name": "Customer", "firstname": "", "lastname": ""})
		}
	}
	provider.mutex.Unlock()
	provider.writeValue(response, http.StatusOK, map[string]any{"object": "list", "uri": "/api/customers", "data": customers,
		"meta": map[string]any{"prev_cursor": nil, "next_cursor": nil}})
}

// listTickets serves both the external_id lookup and the customer list, newest update first, with an offset cursor.
func (provider *fakeGorgias) listTickets(response http.ResponseWriter, request *http.Request, body []byte) {
	query := request.URL.Query()
	externalID := query.Get("external_id")
	if externalID != "" {
		provider.record("lookup", request, body)
	} else {
		provider.record("list", request, body)
	}
	limit, err := strconv.Atoi(query.Get("limit"))
	if err != nil || limit < 1 || limit > 100 {
		provider.writeJSON(response, http.StatusBadRequest, `{"error":{"msg":"SENTINEL","data":{"limit":["invalid"]}}}`)
		return
	}
	offset := 0
	if cursor := query.Get("cursor"); cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err != nil {
			provider.writeJSON(response, http.StatusBadRequest, `{"error":{"msg":"invalid_cursor"}}`)
			return
		}
		offset, _ = strconv.Atoi(strings.TrimPrefix(string(decoded), "offset:"))
	}
	customerID, _ := strconv.ParseInt(query.Get("customer_id"), 10, 64)
	provider.mutex.Lock()
	var matches []*fakeTicket
	for _, ticket := range provider.tickets {
		if (externalID != "" && ticket.externalID != externalID) || (customerID != 0 && ticket.customerID != customerID) {
			continue
		}
		matches = append(matches, ticket)
	}
	sort.Slice(matches, func(left, right int) bool {
		if !matches[left].updatedAt.Equal(matches[right].updatedAt) {
			return matches[left].updatedAt.After(matches[right].updatedAt)
		}
		return matches[left].id > matches[right].id
	})
	page := []any{}
	for _, ticket := range matches[min(offset, len(matches)):min(offset+limit, len(matches))] {
		value := provider.ticketJSON(ticket)
		delete(value, "messages")
		value["messages_count"] = len(ticket.messages)
		page = append(page, value)
	}
	provider.mutex.Unlock()
	var nextCursor any
	if offset+limit < len(matches) {
		nextCursor = base64.StdEncoding.EncodeToString([]byte("offset:" + strconv.Itoa(offset+limit)))
	}
	provider.writeValue(response, http.StatusOK, map[string]any{"object": "list", "uri": "/api/tickets", "data": page,
		"meta": map[string]any{"prev_cursor": nil, "next_cursor": nextCursor}})
}

func (provider *fakeGorgias) createTicket(response http.ResponseWriter, request *http.Request, body []byte) {
	attempt := provider.record("create", request, body)
	if provider.rejectsCreate {
		provider.writeJSON(response, http.StatusBadRequest, `{"error":{"msg":"SENTINEL Failed to create the ticket","data":{"assignee_team":["SENTINEL no such team"]}}}`)
		return
	}
	if provider.rateLimitsFirstCreate && attempt == 1 {
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"error":{"msg":"SENTINEL slow down"}}`)
		return
	}
	if provider.failsFirstCreateWithoutStoring && attempt == 1 {
		provider.writeJSON(response, http.StatusBadGateway, `{"error":{"msg":"SENTINEL upstream"}}`)
		return
	}
	if provider.delaysFirstCreate && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	var payload struct {
		Customer   struct{ Email string }  `json:"customer"`
		Subject    string                  `json:"subject"`
		Status     string                  `json:"status"`
		Priority   string                  `json:"priority"`
		Tags       []struct{ Name string } `json:"tags"`
		ExternalID string                  `json:"external_id"`
		Messages   []struct {
			Channel  string `json:"channel"`
			BodyText string `json:"body_text"`
		} `json:"messages"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	require.Len(provider.t, payload.Messages, 1)
	provider.mutex.Lock()
	ticket := provider.storeTicket(provider.customerIDForEmail(payload.Customer.Email), cmp.Or(payload.Status, "open"), cmp.Or(payload.Priority, "normal"), nil, provider.advanceClock())
	for _, tag := range payload.Tags {
		ticket.tags = append(ticket.tags, tag.Name)
	}
	ticket.subject, ticket.externalID = payload.Subject, payload.ExternalID
	ticket.messages = append(ticket.messages, provider.newMessage(payload.Messages[0].Channel, false, payload.Customer.Email, "", "", payload.Subject, payload.Messages[0].BodyText, ""))
	value := provider.ticketJSON(ticket)
	provider.mutex.Unlock()
	if attempt == 1 && provider.holdsFirstCreate != nil {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		<-provider.holdsFirstCreate
	}
	if provider.losesFirstCreateResponse && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusCreated, value)
}

func (provider *fakeGorgias) readTicket(response http.ResponseWriter, request *http.Request, body []byte, ticketID int64) {
	provider.record("read", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.writeJSON(response, http.StatusNotFound, `{"error":{"msg":"Not found"}}`)
		return
	}
	provider.writeValue(response, http.StatusOK, provider.ticketJSON(ticket))
}

func (provider *fakeGorgias) updateTicket(response http.ResponseWriter, request *http.Request, body []byte, ticketID int64) {
	attempt := provider.record("update", request, body)
	if provider.delaysFirstUpdate && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	var payload struct {
		Status       string              `json:"status"`
		Priority     string              `json:"priority"`
		AssigneeUser *struct{ ID int64 } `json:"assignee_user"`
		AssigneeTeam *struct{ ID int64 } `json:"assignee_team"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.writeJSON(response, http.StatusNotFound, `{"error":{"msg":"Not found"}}`)
		return
	}
	ticket.status, ticket.priority = cmp.Or(payload.Status, ticket.status), cmp.Or(payload.Priority, ticket.priority)
	if payload.AssigneeUser != nil {
		ticket.assigneeUser = payload.AssigneeUser.ID
	}
	if payload.AssigneeTeam != nil {
		ticket.assigneeTeam = payload.AssigneeTeam.ID
	}
	ticket.updatedAt = provider.advanceClock()
	provider.writeValue(response, http.StatusAccepted, provider.ticketJSON(ticket))
}

// changeTags adds or removes tag names as sets, as Gorgias's ticket tag endpoints do.
func (provider *fakeGorgias) changeTags(response http.ResponseWriter, request *http.Request, body []byte, ticketID int64) {
	isAdd := request.Method == http.MethodPost
	if isAdd {
		provider.record("addTags", request, body)
	} else {
		provider.record("removeTags", request, body)
	}
	var payload struct {
		Names []string `json:"names"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.writeJSON(response, http.StatusNotFound, `{"error":{"msg":"Not found"}}`)
		return
	}
	for _, name := range payload.Names {
		switch {
		case isAdd && !slices.Contains(ticket.tags, name):
			ticket.tags = append(ticket.tags, name)
		case !isAdd:
			ticket.tags = slices.DeleteFunc(ticket.tags, func(tag string) bool { return tag == name })
		}
	}
	ticket.updatedAt = provider.advanceClock()
	if isAdd {
		provider.writeJSON(response, http.StatusCreated, ``)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (provider *fakeGorgias) addMessage(response http.ResponseWriter, request *http.Request, body []byte, ticketID int64) {
	var payload struct {
		Channel   string                  `json:"channel"`
		FromAgent bool                    `json:"from_agent"`
		Sender    struct{ Email string }  `json:"sender"`
		Receiver  *struct{ Email string } `json:"receiver"`
		Source    *struct {
			From struct{ Address string }   `json:"from"`
			To   []struct{ Address string } `json:"to"`
		} `json:"source"`
		Subject    string `json:"subject"`
		BodyText   string `json:"body_text"`
		ExternalID string `json:"external_id"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	kind := "note"
	if payload.Channel == gorgias.MessageChannelEmail {
		kind = "reply"
		require.NotNil(provider.t, payload.Source, "an email reply names its route")
		require.NotNil(provider.t, payload.Receiver)
	}
	attempt := provider.record(kind, request, body)
	if provider.delaysFirstNote && kind == "note" && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	provider.mutex.Lock()
	ticket, found := provider.tickets[ticketID]
	if !found {
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusNotFound, `{"error":{"msg":"Not found"}}`)
		return
	}
	fromAddress, toAddress := "", ""
	if payload.Source != nil {
		fromAddress, toAddress = payload.Source.From.Address, payload.Source.To[0].Address
	}
	message := provider.newMessage(payload.Channel, payload.FromAgent, payload.Sender.Email, fromAddress, toAddress, payload.Subject, payload.BodyText, payload.ExternalID)
	ticket.messages = append(ticket.messages, message)
	ticket.updatedAt = message.createdAt
	value := provider.messageJSON(ticketID, message)
	value["sent_datetime"] = nil
	provider.mutex.Unlock()
	if provider.losesFirstReplyResponse && kind == "reply" && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusCreated, value)
}

// newMessage requires provider.mutex.
func (provider *fakeGorgias) newMessage(channel string, isFromAgent bool, senderEmail string, fromAddress string, toAddress string,
	subject string, bodyText string, externalID string) fakeMessage {
	provider.nextMessageID++
	return fakeMessage{
		id: provider.nextMessageID, channel: channel, isFromAgent: isFromAgent, senderEmail: senderEmail, fromAddress: fromAddress,
		toAddress: toAddress, subject: subject, bodyText: bodyText, externalID: externalID, createdAt: provider.advanceClock(),
	}
}

func (provider *fakeGorgias) seedCustomer(email string) int64 {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.customerIDForEmail(email)
}

// seedTicket stores a ticket whose first message is the customer's email to the support address.
func (provider *fakeGorgias) seedTicket(customerID int64, status string, priority string, tags []string, updatedAt string) int64 {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	instant, err := time.Parse("2006-01-02T15:04:05", updatedAt)
	require.NoError(provider.t, err)
	ticket := provider.storeTicket(customerID, status, priority, tags, instant)
	ticket.subject, ticket.seededAt = "Seeded ticket", instant
	provider.nextMessageID++
	ticket.messages = append(ticket.messages, fakeMessage{
		id: provider.nextMessageID, channel: gorgias.MessageChannelEmail, senderEmail: provider.customers[customerID],
		fromAddress: provider.customers[customerID], toAddress: supportAddress, subject: "Seeded ticket", bodyText: "Seeded message.", createdAt: instant,
	})
	return ticket.id
}

func (provider *fakeGorgias) seedNote(ticketID int64, bodyText string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket := provider.tickets[ticketID]
	provider.nextMessageID++
	ticket.messages = append(ticket.messages, fakeMessage{
		id: provider.nextMessageID, channel: gorgias.MessageChannelInternalNote, isFromAgent: true, senderEmail: integrationEmail,
		bodyText: bodyText, createdAt: ticket.updatedAt,
	})
}

// storeTicket requires provider.mutex.
func (provider *fakeGorgias) storeTicket(customerID int64, status string, priority string, tags []string, createdAt time.Time) *fakeTicket {
	provider.nextTicketID++
	ticket := &fakeTicket{
		id: provider.nextTicketID, status: status, priority: priority, customerID: customerID,
		tags: append([]string{}, tags...), createdAt: createdAt, updatedAt: createdAt,
	}
	provider.tickets[ticket.id] = ticket
	return ticket
}

// customerIDForEmail requires provider.mutex; like Gorgias, an unknown email adds a customer.
func (provider *fakeGorgias) customerIDForEmail(email string) int64 {
	for id, known := range provider.customers {
		if strings.EqualFold(known, email) {
			return id
		}
	}
	provider.nextCustomerID++
	provider.customers[provider.nextCustomerID] = email
	return provider.nextCustomerID
}

// advanceClock requires provider.mutex; every write moves updated_datetime forward by one minute.
func (provider *fakeGorgias) advanceClock() time.Time {
	provider.clock = provider.clock.Add(time.Minute)
	return provider.clock
}

// ticketJSON requires provider.mutex; it uses Gorgias's offset-less UTC timestamps.
func (provider *fakeGorgias) ticketJSON(ticket *fakeTicket) map[string]any {
	tags := []any{}
	for index, tag := range ticket.tags {
		tags = append(tags, map[string]any{"id": index + 1, "name": tag, "decoration": nil})
	}
	messages := []any{}
	for _, message := range ticket.messages {
		messages = append(messages, provider.messageJSON(ticket.id, message))
	}
	return map[string]any{
		"id": ticket.id, "uri": "/api/tickets/" + strconv.FormatInt(ticket.id, 10) + "/", "external_id": nullableString(ticket.externalID),
		"status": ticket.status, "priority": ticket.priority, "channel": "email", "via": "api", "from_agent": false, "spam": false,
		"subject": ticket.subject, "language": "en", "customer": map[string]any{"id": ticket.customerID, "email": provider.customers[ticket.customerID], "name": "Customer"},
		"assignee_user": nullableID(ticket.assigneeUser), "assignee_team": nullableID(ticket.assigneeTeam), "tags": tags, "messages": messages,
		"meta": map[string]any{}, "created_datetime": ticket.createdAt.Format(fakeTimestamp), "updated_datetime": ticket.updatedAt.Format(fakeTimestamp),
		"last_message_datetime": nil, "last_received_message_datetime": nil, "closed_datetime": nil, "snooze_datetime": nil, "trashed_datetime": nil,
	}
}

func (provider *fakeGorgias) messageJSON(ticketID int64, message fakeMessage) map[string]any {
	value := map[string]any{
		"id": message.id, "ticket_id": ticketID, "channel": message.channel, "via": "api", "public": message.channel != gorgias.MessageChannelInternalNote,
		"from_agent": message.isFromAgent, "sender": map[string]any{"id": 1, "email": message.senderEmail}, "subject": message.subject,
		"body_text": message.bodyText, "body_html": "<div>" + message.bodyText + "</div>", "stripped_text": nil, "external_id": nullableString(message.externalID),
		"created_datetime": message.createdAt.Format(fakeTimestamp), "sent_datetime": message.createdAt.Format(fakeTimestamp), "failed_datetime": nil,
	}
	if message.fromAddress != "" {
		value["source"] = map[string]any{"type": message.channel, "from": map[string]any{"address": message.fromAddress},
			"to": []map[string]any{{"address": message.toAddress}}}
	}
	return value
}

func nullableID(value int64) any {
	if value == 0 {
		return nil
	}
	return map[string]any{"id": value}
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (provider *fakeGorgias) record(name string, request *http.Request, body []byte) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[name]++
	provider.requests[name] = append(provider.requests[name], fakeRecordedRequest{at: time.Now(), query: request.URL.Query(), body: string(body)})
	return provider.counts[name]
}

func (provider *fakeGorgias) writeValue(response http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	require.NoError(provider.t, err)
	provider.writeJSON(response, status, string(encoded))
}

func (provider *fakeGorgias) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Gorgias-Account-Api-Call-Limit", "1/40")
	response.WriteHeader(status)
	if _, err := io.WriteString(response, body); err != nil {
		provider.t.Logf("fake Gorgias response write failed: %v", err)
	}
}

// dropConnection closes the connection after Gorgias applied the request, as a lost response would.
func (provider *fakeGorgias) dropConnection(response http.ResponseWriter) {
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(provider.t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(provider.t, err)
	require.NoError(provider.t, connection.Close())
}

func (provider *fakeGorgias) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * slowResponseDelay):
		t.Fatal("a delayed fake Gorgias request did not finish")
	}
}

func (provider *fakeGorgias) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakeGorgias) totalRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, requests := range provider.requests {
		total += len(requests)
	}
	return total
}

func (provider *fakeGorgias) ticketCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.tickets)
}

func (provider *fakeGorgias) ticket(ticketID int64) fakeTicket {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	ticket := *provider.tickets[ticketID]
	ticket.messages = append([]fakeMessage(nil), ticket.messages...)
	ticket.tags = append([]string(nil), ticket.tags...)
	return ticket
}

func (provider *fakeGorgias) lastRequest(name string) fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	requests := provider.requests[name]
	require.NotEmpty(provider.t, requests, name)
	return requests[len(requests)-1]
}

func (provider *fakeGorgias) queryValues(name string, key string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var values []string
	for _, request := range provider.requests[name] {
		values = append(values, request.query.Get(key))
	}
	return values
}

func (provider *fakeGorgias) requestTimes(name string) []time.Time {
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

func newTriageHarness(t *testing.T, provider *fakeGorgias, requestTimeout time.Duration) *triageHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "gorgias", Name: ConnectionName}
	providerClient, err := gorgias.New(gorgias.Config{Domain: integrationDomain},
		sdkgo.StaticCredentialProvider[gorgias.Credentials]{reference: {Email: integrationEmail, APIKey: sdkgo.NewSecretString(integrationAPIKey)}},
		gorgias.WithAPIBaseURL(provider.URL+"/api"), gorgias.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := gorgias.NewConnection(providerClient, reference)
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
	flowID := "gorgias-triage-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
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
