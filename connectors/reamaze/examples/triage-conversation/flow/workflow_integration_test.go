//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageconversation

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
	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationBrand     = "acme"
	integrationEmail     = "agent@acme.example.com"
	integrationAPIToken  = "reamazeIntegrationToken0123456789"
	integrationRequester = "jane@acme.example.com"
	integrationIssueTag  = "billing-double-charge"
	integrationChannel   = "support"

	defaultRequestTimeout = 5 * time.Second
	// slowResponseDelay outlasts Dex's roughly seven-second async local phase, so an async Step is dispatched again.
	slowResponseDelay  = 9 * time.Second
	slowRequestTimeout = 20 * time.Second

	statusOpen      = 0
	statusResponded = 1
	statusDone      = 2
)

func integrationIssueInput() Input {
	return Input{
		RequesterEmail: integrationRequester, RequesterName: "Jane Smith", Subject: "Double charge on order 88213",
		Message: "I was charged twice for order 88213.", IssueTag: integrationIssueTag, Channel: integrationChannel,
	}
}

func TestNewCustomerOpensOneConversationAndAddsOneInternalNoteWithRealDex(t *testing.T) {
	provider := newFakeReamaze(t)
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "new-customer", integrationIssueInput())
	require.Equal(t, TriageConversationOpened, outcome.Action)
	require.False(t, outcome.IsKnownContact)
	require.False(t, outcome.NeedsReview)
	require.False(t, outcome.WasAlreadyApplied)
	require.Equal(t, 1, provider.conversationCount())
	require.Equal(t, 1, provider.count("create"))
	require.Zero(t, provider.count("search"), "a customer Re:amaze does not know has no conversations to search")
	require.Equal(t, url.Values{"q": {integrationRequester}, "type": {"email"}, "page": {"1"}}, provider.lastRequest("contacts").query)

	var created struct {
		Conversation struct {
			Subject  string            `json:"subject"`
			Category string            `json:"category"`
			TagList  []string          `json:"tag_list"`
			Data     map[string]string `json:"data"`
			Message  map[string]any    `json:"message"`
			User     map[string]string `json:"user"`
		} `json:"conversation"`
	}
	require.NoError(t, json.Unmarshal([]byte(provider.lastRequest("create").body), &created))
	require.Equal(t, "Double charge on order 88213", created.Conversation.Subject)
	require.Equal(t, integrationChannel, created.Conversation.Category)
	require.Equal(t, []string{integrationIssueTag}, created.Conversation.TagList)
	require.Equal(t, map[string]any{"body": "I was charged twice for order 88213."}, created.Conversation.Message)
	require.Equal(t, map[string]string{"name": "Jane Smith", "email": integrationRequester}, created.Conversation.User)
	require.True(t, strings.HasPrefix(created.Conversation.Data[reamaze.DispatchKeyDataAttribute], "dex-"))

	conversation := provider.conversation(outcome.Conversation.ID)
	require.Len(t, conversation.messages, 2, "the customer's message and exactly one triage note")
	note := conversation.messages[1]
	require.Equal(t, 1, note.visibility, "the triage note is internal")
	require.Equal(t, outcome.NoteOriginID, note.originID)
	require.True(t, strings.HasPrefix(note.originID, "dex-"))
	require.Equal(t, BuildOpenedConversationNote(mustCustomerIssue(t, integrationIssueInput())), note.body)
	require.JSONEq(t, `true`, mustJSONField(t, provider.lastRequest("note").body, "suppress_autoresolve"))
}

func TestRepeatContactReopensTheCustomersConversationAndLeavesDecoysUntouchedWithRealDex(t *testing.T) {
	provider := newFakeReamaze(t)
	provider.seedContact(integrationRequester, "Jane Smith")
	existing := provider.seedConversation(integrationRequester, statusResponded, []string{integrationIssueTag, "vip"}, "2026-01-26T14:02:00Z")
	resolved := provider.seedConversation(integrationRequester, statusDone, []string{integrationIssueTag}, "2026-01-27T09:00:00Z")
	provider.seedNote(resolved, "Refund already issued on Jan 13, see REF-771.")
	copied := provider.seedConversation("ben@meridian.example.com", statusOpen, []string{integrationIssueTag}, "2026-01-28T11:00:00Z")
	provider.addFollower(copied, integrationRequester)
	lookalike := provider.seedConversation("jane@acme.example.com.au", statusOpen, []string{integrationIssueTag}, "2026-01-28T12:00:00Z")
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "repeat-contact", integrationIssueInput())
	require.Equal(t, TriageConversationFollowedUp, outcome.Action)
	require.True(t, outcome.IsKnownContact)
	require.Equal(t, existing, outcome.Conversation.ID)
	require.Equal(t, reamaze.ConversationStatusOpen, outcome.Conversation.Status, "a responded conversation is reopened")
	require.Equal(t, []string{integrationIssueTag, "vip", RepeatContactTag}, outcome.Conversation.Tags)
	require.False(t, outcome.WasAlreadyApplied)
	require.Equal(t, url.Values{"for": {integrationRequester}, "tag": {integrationIssueTag}, "sort": {"changed"}, "page": {"1"}},
		provider.lastRequest("search").query)
	require.JSONEq(t, `{"conversation":{"status":0,"tag_list":["billing-double-charge","vip","dex-repeat-contact"]}}`, provider.lastRequest("update").body)

	conversation := provider.conversation(existing)
	require.Len(t, conversation.messages, 2, "exactly one triage note")
	require.Equal(t, BuildFollowUpNote(mustCustomerIssue(t, integrationIssueInput())), conversation.messages[1].body)
	for _, decoy := range []string{resolved, copied, lookalike} {
		snapshot := provider.conversation(decoy)
		require.Equal(t, snapshot.seededAt, snapshot.changedAt, "conversation %s must not be touched", decoy)
	}
	require.Len(t, provider.conversation(resolved).messages, 2)
	require.Equal(t, 4, provider.conversationCount(), "no conversation was created")
	require.Equal(t, 1, provider.count("update"))
}

func TestSecondSearchPageIsReadWhenTheFirstHoldsNoCandidateWithRealDex(t *testing.T) {
	provider := newFakeReamaze(t)
	provider.seedContact(integrationRequester, "Jane Smith")
	existing := provider.seedConversation(integrationRequester, statusOpen, []string{integrationIssueTag}, "2026-01-02T09:00:00Z")
	for index := 0; index < 30; index++ {
		provider.seedConversation(integrationRequester, statusDone, []string{integrationIssueTag}, fmt.Sprintf("2026-01-20T09:%02d:00Z", index))
	}
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "second-page", integrationIssueInput())
	require.Equal(t, TriageConversationFollowedUp, outcome.Action)
	require.Equal(t, existing, outcome.Conversation.ID)
	require.Equal(t, []string{"1", "2"}, provider.queryValues("search", "page"))
}

// Sync durability: Dex sends no second create while the first is still in flight.
func TestSlowCreateIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeReamaze(t)
	provider.delaysFirstCreate = true
	harness := newTriageHarness(t, provider, slowRequestTimeout)

	startedAt := time.Now()
	outcome := harness.runTriage(t, "slow-create", integrationIssueInput())
	require.GreaterOrEqual(t, time.Since(startedAt), slowResponseDelay)
	require.Equal(t, TriageConversationOpened, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("create"), "no second dispatch while the first was in flight")
	require.Zero(t, provider.count("reconcile"))
	require.Equal(t, 1, provider.conversationCount())
}

func TestSlowNoteIsSentOnceWithRealDex(t *testing.T) {
	provider := newFakeReamaze(t)
	provider.delaysFirstNote = true
	harness := newTriageHarness(t, provider, slowRequestTimeout)

	outcome := harness.runTriage(t, "slow-note", integrationIssueInput())
	require.Equal(t, TriageConversationOpened, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("note"), "no second dispatch while the first was in flight")
	require.Len(t, provider.conversation(outcome.Conversation.ID).messages, 2)
}

// TestSlowUpdateIsSafeToRepeatWithRealDex lets async Dex dispatch the update again; both attempts write the same values.
func TestSlowUpdateIsSafeToRepeatWithRealDex(t *testing.T) {
	provider := newFakeReamaze(t)
	provider.seedContact(integrationRequester, "Jane Smith")
	existing := provider.seedConversation(integrationRequester, statusResponded, []string{integrationIssueTag}, "2026-01-26T14:02:00Z")
	provider.delaysFirstUpdate = true
	harness := newTriageHarness(t, provider, slowRequestTimeout)

	outcome := harness.runTriage(t, "slow-update", integrationIssueInput())
	require.Equal(t, TriageConversationFollowedUp, outcome.Action)
	provider.waitForDelayedRequests(t)
	require.GreaterOrEqual(t, provider.count("update"), 2, "Dex dispatched the update again past its local phase")
	conversation := provider.conversation(existing)
	require.Equal(t, statusOpen, conversation.status)
	require.Equal(t, []string{integrationIssueTag, RepeatContactTag}, conversation.tags, "the repeated write did not duplicate the tag")
	require.Len(t, conversation.messages, 2, "exactly one triage note")
	t.Logf("slow update: updates=%d reads=%d", provider.count("update"), provider.count("read"))
}

func TestLostCreateResponseIsReconciledWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeReamaze(t)
	provider.losesFirstCreateResponse = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "lost-create", integrationIssueInput())
	require.Equal(t, TriageConversationOpened, outcome.Action)
	require.True(t, outcome.WasAlreadyApplied, "the retried attempt found the conversation by its dispatch key")
	require.False(t, outcome.NeedsReview)
	require.Equal(t, 1, provider.count("create"), "an unconfirmed create is never resent")
	require.Equal(t, 1, provider.count("reconcile"))
	require.Equal(t, 1, provider.count("read"), "the listed candidate's key is confirmed by a single conversation read")
	require.Equal(t, 1, provider.conversationCount())
	require.Len(t, provider.conversation(outcome.Conversation.ID).messages, 2, "the Flow continued to the note")
}

func TestUnappliedCreateWithAnUnknownOutcomeCompletesAsNeedsReviewWithRealDex(t *testing.T) {
	provider := newFakeReamaze(t)
	provider.failsFirstCreateWithoutApplying = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "unknown-create", integrationIssueInput())
	require.Equal(t, TriageConversationCreationUncertain, outcome.Action)
	require.True(t, outcome.NeedsReview)
	require.Equal(t, "createConversation", outcome.ReviewReason)
	require.Contains(t, outcome.ReviewDetail, "shows no record with its key")
	require.Equal(t, 1, provider.count("create"), "a 502 is never resent, because Re:amaze may still apply it")
	require.Equal(t, 1, provider.count("reconcile"))
	require.Zero(t, provider.conversationCount())
	require.Zero(t, provider.count("note"))
}

func TestLostNoteResponseIsReconciledByOriginIDWithRealDex(t *testing.T) {
	provider := newFakeReamaze(t)
	provider.losesFirstNoteResponse = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "lost-note", integrationIssueInput())
	require.Equal(t, TriageConversationOpened, outcome.Action)
	require.False(t, outcome.NeedsReview)
	require.True(t, outcome.NoteWasAlreadyApplied, "the retried attempt found its note by origin_id")
	require.Equal(t, 1, provider.count("note"), "an unconfirmed note is never resent")
	require.Equal(t, 1, provider.count("messages"), "one read reconciled the note")
	messages := provider.conversation(outcome.Conversation.ID).messages
	require.Len(t, messages, 2)
	require.Equal(t, outcome.NoteOriginID, messages[1].originID)
}

func TestRateLimitedCreateWaitsAndCreatesOneConversationWithRealDex(t *testing.T) {
	provider := newFakeReamaze(t)
	provider.rateLimitsFirstCreate = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)

	outcome := harness.runTriage(t, "rate-limited", integrationIssueInput())
	require.Equal(t, TriageConversationOpened, outcome.Action, "a 429 clears the dispatch marker, so the retry may send")
	require.False(t, outcome.WasAlreadyApplied)
	times := provider.requestTimes("create")
	require.Len(t, times, 2)
	require.GreaterOrEqual(t, times[1].Sub(times[0]), time.Second, "the retry waited for Retry-After")
	require.Equal(t, 1, provider.conversationCount())
}

// The replacement Worker's attempt finds the stored dispatch marker, reads back, and sends nothing.
func TestLostWorkerDuringCreateSelectsUncertainWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeReamaze(t)
	provider.holdsFirstCreate = make(chan struct{})
	harness := newTriageHarness(t, provider, slowRequestTimeout)
	flowID := harness.startTriage(t, "lost-worker", integrationIssueInput())
	require.Eventually(t, func() bool { return provider.count("create") == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach Re:amaze")
	harness.replaceWorker(t)

	result := harness.waitForFlow(t, flowID)
	close(provider.holdsFirstCreate)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome TriageOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, TriageConversationCreationUncertain, outcome.Action)
	require.True(t, outcome.NeedsReview)
	require.Contains(t, outcome.ReviewDetail, "shows no record with its key",
		"the new Worker's attempt found the lost attempt's marker and reconciled instead of sending")
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.count("create"), "the attempt on the new Worker did not resend the create")
	require.Equal(t, 1, provider.conversationCount(), "the held create still landed, which a person must now find")
}

func TestRejectedConversationFailsTheFlowWithoutReamazeTextWithRealDex(t *testing.T) {
	provider := newFakeReamaze(t)
	provider.rejectsCreate = true
	harness := newTriageHarness(t, provider, defaultRequestTimeout)
	flowID := harness.startTriage(t, "rejected", integrationIssueInput())

	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired optional providerRejected branch fails the Flow")
	require.NotContains(t, result.ErrorMessage, "SENTINEL")
	require.NotContains(t, result.ErrorMessage, integrationAPIToken)
	require.Equal(t, 1, provider.count("create"), "a conclusive rejection is not retried")
	require.Zero(t, provider.conversationCount())
	t.Logf("rejected create failure: %s", result.ErrorMessage)
}

func TestInvalidIssueFailsBeforeCallingReamazeWithRealDex(t *testing.T) {
	provider := newFakeReamaze(t)
	harness := newTriageHarness(t, provider, defaultRequestTimeout)
	input := integrationIssueInput()
	input.Channel = "support/../staff"
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

func mustJSONField(t *testing.T, body string, field string) string {
	t.Helper()
	var document struct {
		Message map[string]json.RawMessage `json:"message"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &document))
	return string(document.Message[field])
}

// fakeReamaze is a stateful Re:amaze fake whose requester filter, like for, also matches followers.
type fakeReamaze struct {
	*httptest.Server
	t     *testing.T
	mutex sync.Mutex
	// delayedRequests tracks handlers still sleeping, so a test can assert their final effect.
	delayedRequests sync.WaitGroup

	clock            time.Time
	nextConversation int
	contacts         map[string]string
	conversations    map[string]*fakeConversation
	counts           map[string]int
	requests         map[string][]fakeRecordedRequest

	delaysFirstCreate               bool
	delaysFirstNote                 bool
	delaysFirstUpdate               bool
	losesFirstCreateResponse        bool
	losesFirstNoteResponse          bool
	failsFirstCreateWithoutApplying bool
	rateLimitsFirstCreate           bool
	rejectsCreate                   bool
	holdsFirstCreate                chan struct{}
}

type fakeConversation struct {
	slug          string
	subject       string
	category      string
	status        int
	tags          []string
	authorEmail   string
	authorName    string
	assigneeEmail string
	followers     []string
	data          map[string]string
	createdAt     time.Time
	seededAt      time.Time
	changedAt     time.Time
	messages      []fakeMessage
}

type fakeMessage struct {
	body       string
	visibility int
	originID   string
	userEmail  string
	createdAt  time.Time
}

type fakeRecordedRequest struct {
	at    time.Time
	query url.Values
	body  string
}

func newFakeReamaze(t *testing.T) *fakeReamaze {
	t.Helper()
	provider := &fakeReamaze{
		t: t, clock: time.Date(2026, 1, 28, 9, 0, 0, 0, time.UTC), contacts: map[string]string{},
		conversations: map[string]*fakeConversation{}, counts: map[string]int{}, requests: map[string][]fakeRecordedRequest{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeReamaze) serveHTTP(response http.ResponseWriter, request *http.Request) {
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(integrationEmail+":"+integrationAPIToken))
	if request.Header.Get("Authorization") != expected {
		provider.writeJSON(response, http.StatusUnauthorized, `{"error":"SENTINEL invalid login"}`)
		return
	}
	if request.Header.Get("Accept") != "application/json" {
		provider.writeJSON(response, http.StatusNotAcceptable, `{"error":"SENTINEL use application/json"}`)
		return
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		provider.writeJSON(response, http.StatusBadRequest, `{}`)
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/api/v1")
	segments := strings.Split(strings.TrimPrefix(path, "/conversations/"), "/")
	switch {
	case request.Method == http.MethodGet && path == "/contacts":
		provider.listContacts(response, request, body)
	case request.Method == http.MethodGet && path == "/conversations":
		provider.listConversations(response, request, body)
	case request.Method == http.MethodPost && path == "/conversations":
		provider.createConversation(response, request, body)
	case strings.HasPrefix(path, "/conversations/") && len(segments) == 1 && request.Method == http.MethodGet:
		provider.readConversation(response, request, body, segments[0])
	case strings.HasPrefix(path, "/conversations/") && len(segments) == 1 && request.Method == http.MethodPut:
		provider.updateConversation(response, request, body, segments[0])
	case strings.HasPrefix(path, "/conversations/") && len(segments) == 2 && segments[1] == "messages" && request.Method == http.MethodGet:
		provider.listMessages(response, request, body, segments[0])
	case strings.HasPrefix(path, "/conversations/") && len(segments) == 2 && segments[1] == "messages" && request.Method == http.MethodPost:
		provider.addMessage(response, request, body, segments[0])
	default:
		provider.writeJSON(response, http.StatusNotFound, `{"error":"SENTINEL not found"}`)
	}
}

func (provider *fakeReamaze) listContacts(response http.ResponseWriter, request *http.Request, body []byte) {
	provider.record("contacts", request, body)
	query := strings.ToLower(request.URL.Query().Get("q"))
	provider.mutex.Lock()
	contacts := []any{}
	for email, name := range provider.contacts {
		if strings.Contains(strings.ToLower(email), query) || strings.Contains(strings.ToLower(name), query) {
			contacts = append(contacts, map[string]any{"name": name, "email": email, "friendly_name": nil, "notes": []any{}})
		}
	}
	provider.mutex.Unlock()
	provider.writeValue(response, http.StatusOK, map[string]any{"page_size": 30, "page_count": min(len(contacts), 1), "total_count": len(contacts), "contacts": contacts})
}

func (provider *fakeReamaze) listConversations(response http.ResponseWriter, request *http.Request, body []byte) {
	query := request.URL.Query()
	dataFilter := map[string]string{}
	for key, values := range query {
		if strings.HasPrefix(key, "data[") && strings.HasSuffix(key, "]") {
			dataFilter[strings.TrimSuffix(strings.TrimPrefix(key, "data["), "]")] = values[0]
		}
	}
	if len(dataFilter) != 0 {
		provider.record("reconcile", request, body)
	} else {
		provider.record("search", request, body)
	}
	page, err := strconv.Atoi(query.Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	var tags []string
	if query.Get("tag") != "" {
		tags = strings.Split(query.Get("tag"), ",")
	}
	provider.mutex.Lock()
	var matches []*fakeConversation
	for _, conversation := range provider.conversations {
		if query.Get("filter") != "all" && conversation.status == 4 {
			continue
		}
		if user := query.Get("for"); user != "" && !strings.EqualFold(conversation.authorEmail, user) && !containsFold(conversation.followers, user) {
			continue
		}
		if len(tags) != 0 && !overlapsFold(tags, conversation.tags) {
			continue
		}
		if !hasData(conversation.data, dataFilter) {
			continue
		}
		matches = append(matches, conversation)
	}
	sort.Slice(matches, func(left, right int) bool {
		if query.Get("sort") == "changed" {
			return matches[left].changedAt.After(matches[right].changedAt)
		}
		return matches[left].createdAt.After(matches[right].createdAt)
	})
	start, end := min((page-1)*30, len(matches)), min(page*30, len(matches))
	conversations := []any{}
	for _, conversation := range matches[start:end] {
		// Re:amaze documents data only on single conversation reads.
		entry := provider.conversationJSON(conversation)
		delete(entry, "data")
		conversations = append(conversations, entry)
	}
	provider.mutex.Unlock()
	provider.writeValue(response, http.StatusOK, map[string]any{
		"page_size": 30, "page_count": (len(matches) + 29) / 30, "total_count": len(matches), "conversations": conversations,
	})
}

func (provider *fakeReamaze) readConversation(response http.ResponseWriter, request *http.Request, body []byte, slug string) {
	provider.record("read", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	conversation, isFound := provider.conversations[slug]
	if !isFound {
		provider.writeJSON(response, http.StatusNotFound, `{"error":"SENTINEL not found"}`)
		return
	}
	provider.writeValue(response, http.StatusOK, provider.conversationJSON(conversation))
}

func (provider *fakeReamaze) listMessages(response http.ResponseWriter, request *http.Request, body []byte, slug string) {
	provider.record("messages", request, body)
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	conversation, isFound := provider.conversations[slug]
	if !isFound {
		provider.writeJSON(response, http.StatusNotFound, `{"error":"SENTINEL not found"}`)
		return
	}
	messages := []any{}
	for index := len(conversation.messages) - 1; index >= 0 && len(messages) < 30; index-- {
		message := conversation.messages[index]
		messages = append(messages, map[string]any{
			"body": message.body, "visibility": message.visibility, "origin": 7, "origin_id": nullableString(message.originID),
			"created_at": message.createdAt.Format(time.RFC3339Nano), "user": map[string]any{"name": "User", "email": message.userEmail},
			"recipients": []any{}, "attachments": []any{},
		})
	}
	provider.writeValue(response, http.StatusOK, map[string]any{
		"page_size": 30, "page_count": (len(conversation.messages) + 29) / 30, "total_count": len(conversation.messages), "messages": messages,
	})
}

func (provider *fakeReamaze) createConversation(response http.ResponseWriter, request *http.Request, body []byte) {
	attempt := provider.record("create", request, body)
	if provider.rejectsCreate {
		provider.writeJSON(response, http.StatusUnprocessableEntity, `{"errors":{"category":["SENTINEL is not a channel of this brand"]}}`)
		return
	}
	if provider.rateLimitsFirstCreate && attempt == 1 {
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"error":"SENTINEL slow down"}`)
		return
	}
	if provider.failsFirstCreateWithoutApplying && attempt == 1 {
		provider.writeJSON(response, http.StatusBadGateway, `<html>SENTINEL bad gateway</html>`)
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
		Conversation struct {
			Subject  string            `json:"subject"`
			Category string            `json:"category"`
			TagList  []string          `json:"tag_list"`
			Status   *int              `json:"status"`
			Data     map[string]string `json:"data"`
			Message  struct {
				Body string `json:"body"`
			} `json:"message"`
			User struct {
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"user"`
		} `json:"conversation"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	provider.mutex.Lock()
	input := payload.Conversation
	if _, isKnown := provider.contacts[input.User.Email]; !isKnown {
		provider.contacts[input.User.Email] = input.User.Name
	}
	createdAt := provider.advanceClock()
	conversation := provider.storeConversation(input.Subject, input.User.Email, statusOpen, input.TagList, createdAt)
	conversation.category, conversation.authorName, conversation.data = input.Category, input.User.Name, input.Data
	if input.Status != nil {
		conversation.status = *input.Status
	}
	conversation.messages = []fakeMessage{{body: input.Message.Body, userEmail: input.User.Email, createdAt: createdAt}}
	value := provider.conversationJSON(conversation)
	provider.mutex.Unlock()
	if provider.losesFirstCreateResponse && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusCreated, value)
}

func (provider *fakeReamaze) updateConversation(response http.ResponseWriter, request *http.Request, body []byte, slug string) {
	attempt := provider.record("update", request, body)
	if provider.delaysFirstUpdate && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	var payload struct {
		Conversation struct {
			Status   *int      `json:"status"`
			TagList  *[]string `json:"tag_list"`
			Assignee *struct {
				Email string `json:"email"`
			} `json:"assignee"`
		} `json:"conversation"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	conversation, isFound := provider.conversations[slug]
	if !isFound {
		provider.writeJSON(response, http.StatusNotFound, `{"error":"SENTINEL not found"}`)
		return
	}
	if payload.Conversation.Status != nil {
		conversation.status = *payload.Conversation.Status
	}
	if payload.Conversation.TagList != nil {
		conversation.tags = append([]string(nil), (*payload.Conversation.TagList)...)
	}
	if payload.Conversation.Assignee != nil {
		conversation.assigneeEmail = payload.Conversation.Assignee.Email
	}
	conversation.changedAt = provider.advanceClock()
	provider.writeValue(response, http.StatusOK, provider.conversationJSON(conversation))
}

func (provider *fakeReamaze) addMessage(response http.ResponseWriter, request *http.Request, body []byte, slug string) {
	attempt := provider.record("note", request, body)
	if provider.delaysFirstNote && attempt == 1 {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowResponseDelay)
	}
	var payload struct {
		Message struct {
			Body       string `json:"body"`
			Visibility int    `json:"visibility"`
			OriginID   string `json:"origin_id"`
		} `json:"message"`
	}
	require.NoError(provider.t, json.Unmarshal(body, &payload))
	provider.mutex.Lock()
	conversation, isFound := provider.conversations[slug]
	if !isFound {
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusNotFound, `{"error":"SENTINEL not found"}`)
		return
	}
	message := fakeMessage{
		body: payload.Message.Body, visibility: payload.Message.Visibility, originID: payload.Message.OriginID,
		userEmail: integrationEmail, createdAt: provider.advanceClock(),
	}
	conversation.messages = append(conversation.messages, message)
	conversation.changedAt = message.createdAt
	value := map[string]any{
		"body": message.body, "visibility": message.visibility, "origin_id": message.originID, "origin": 7,
		"created_at": message.createdAt.Format(time.RFC3339Nano), "user": map[string]any{"name": "Agent", "email": integrationEmail},
		"conversation": map[string]any{"subject": conversation.subject, "slug": slug, "created_at": conversation.createdAt.Format(time.RFC3339Nano)},
	}
	provider.mutex.Unlock()
	if provider.losesFirstNoteResponse && attempt == 1 {
		provider.dropConnection(response)
		return
	}
	provider.writeValue(response, http.StatusCreated, value)
}

// dropConnection closes the connection after Re:amaze applied the request, as a lost response would.
func (provider *fakeReamaze) dropConnection(response http.ResponseWriter) {
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(provider.t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(provider.t, err)
	require.NoError(provider.t, connection.Close())
}

func (provider *fakeReamaze) seedContact(email string, name string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.contacts[email] = name
}

// seedConversation stores a conversation the customer started, last changed at changedAt.
func (provider *fakeReamaze) seedConversation(authorEmail string, status int, tags []string, changedAt string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	instant, err := time.Parse(time.RFC3339, changedAt)
	require.NoError(provider.t, err)
	conversation := provider.storeConversation("Seeded conversation", authorEmail, status, tags, instant)
	conversation.seededAt = instant
	conversation.messages = []fakeMessage{{body: "Seeded customer message.", userEmail: authorEmail, createdAt: instant}}
	return conversation.slug
}

func (provider *fakeReamaze) seedNote(slug string, body string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	conversation := provider.conversations[slug]
	conversation.messages = append(conversation.messages, fakeMessage{body: body, visibility: 1, userEmail: integrationEmail, createdAt: conversation.changedAt})
}

func (provider *fakeReamaze) addFollower(slug string, email string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.conversations[slug].followers = append(provider.conversations[slug].followers, email)
}

// storeConversation requires provider.mutex; like Re:amaze, the slug derives from the subject.
func (provider *fakeReamaze) storeConversation(subject string, authorEmail string, status int, tags []string, createdAt time.Time) *fakeConversation {
	provider.nextConversation++
	slug := strings.ToLower(strings.Join(strings.Fields(subject), "-")) + "-" + strconv.Itoa(provider.nextConversation)
	conversation := &fakeConversation{
		slug: slug, subject: subject, category: integrationChannel, status: status, tags: append([]string{}, tags...),
		authorEmail: authorEmail, authorName: "Customer", data: map[string]string{}, createdAt: createdAt, changedAt: createdAt,
	}
	provider.conversations[slug] = conversation
	return conversation
}

// conversationJSON requires provider.mutex.
func (provider *fakeReamaze) conversationJSON(conversation *fakeConversation) map[string]any {
	var assignee any
	if conversation.assigneeEmail != "" {
		assignee = map[string]any{"name": "Staff", "email": conversation.assigneeEmail}
	}
	followers := []any{map[string]any{"name": conversation.authorName, "email": conversation.authorEmail}}
	for _, email := range conversation.followers {
		followers = append(followers, map[string]any{"name": "Follower", "email": email})
	}
	data := map[string]any{}
	for key, value := range conversation.data {
		data[key] = value
	}
	first := conversation.messages
	firstBody := ""
	if len(first) != 0 {
		firstBody = first[0].body
	}
	return map[string]any{
		"subject": conversation.subject, "slug": conversation.slug, "status": conversation.status,
		"created_at": conversation.createdAt.Format(time.RFC3339Nano), "tag_list": append([]string{}, conversation.tags...),
		"message":               map[string]any{"body": firstBody},
		"last_customer_message": map[string]any{"body": "SENTINEL customer text", "created_at": conversation.createdAt.Format(time.RFC3339Nano)},
		"author":                map[string]any{"name": conversation.authorName, "email": conversation.authorEmail},
		"assignee":              assignee,
		"category":              map[string]any{"name": "Support", "slug": conversation.category, "email": "support@acme.example.com", "channel": 1},
		"data":                  data, "followers": followers,
	}
}

// advanceClock requires provider.mutex; every write moves the clock forward by one minute.
func (provider *fakeReamaze) advanceClock() time.Time {
	provider.clock = provider.clock.Add(time.Minute)
	return provider.clock
}

func (provider *fakeReamaze) record(name string, request *http.Request, body []byte) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.counts[name]++
	provider.requests[name] = append(provider.requests[name], fakeRecordedRequest{at: time.Now(), query: request.URL.Query(), body: string(body)})
	return provider.counts[name]
}

func (provider *fakeReamaze) writeValue(response http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	require.NoError(provider.t, err)
	provider.writeJSON(response, status, string(encoded))
}

func (provider *fakeReamaze) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("X-Request-Id", "fake-request")
	response.WriteHeader(status)
	if _, err := io.WriteString(response, body); err != nil {
		provider.t.Logf("fake Re:amaze response write failed: %v", err)
	}
}

func (provider *fakeReamaze) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * slowResponseDelay):
		t.Fatal("a delayed fake Re:amaze request did not finish")
	}
}

func (provider *fakeReamaze) count(name string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.counts[name]
}

func (provider *fakeReamaze) totalRequests() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, requests := range provider.requests {
		total += len(requests)
	}
	return total
}

func (provider *fakeReamaze) conversationCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.conversations)
}

func (provider *fakeReamaze) conversation(slug string) fakeConversation {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	conversation := *provider.conversations[slug]
	conversation.messages = append([]fakeMessage(nil), conversation.messages...)
	conversation.tags = append([]string(nil), conversation.tags...)
	return conversation
}

func (provider *fakeReamaze) lastRequest(name string) fakeRecordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	requests := provider.requests[name]
	require.NotEmpty(provider.t, requests, name)
	return requests[len(requests)-1]
}

func (provider *fakeReamaze) queryValues(name string, key string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var values []string
	for _, request := range provider.requests[name] {
		values = append(values, request.query.Get(key))
	}
	return values
}

func (provider *fakeReamaze) requestTimes(name string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var times []time.Time
	for _, request := range provider.requests[name] {
		times = append(times, request.at)
	}
	return times
}

func containsFold(values []string, value string) bool {
	for _, candidate := range values {
		if strings.EqualFold(candidate, value) {
			return true
		}
	}
	return false
}

func overlapsFold(wanted []string, actual []string) bool {
	for _, value := range wanted {
		if containsFold(actual, value) {
			return true
		}
	}
	return false
}

func hasData(data map[string]string, filter map[string]string) bool {
	for key, value := range filter {
		if data[key] != value {
			return false
		}
	}
	return true
}

func nullableString(value string) any {
	if value == "" {
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

func newTriageHarness(t *testing.T, provider *fakeReamaze, requestTimeout time.Duration) *triageHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "reamaze", Name: ConnectionName}
	providerClient, err := reamaze.New(reamaze.Config{Brand: integrationBrand},
		sdkgo.StaticCredentialProvider[reamaze.Credentials]{reference: {Email: integrationEmail, APIToken: sdkgo.NewSecretString(integrationAPIToken)}},
		reamaze.WithAPIBaseURL(provider.URL+"/api/v1"), reamaze.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := reamaze.NewConnection(providerClient, reference)
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
	flowID := "reamaze-triage-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
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
