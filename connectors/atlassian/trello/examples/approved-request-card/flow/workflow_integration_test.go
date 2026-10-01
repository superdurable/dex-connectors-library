//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvedrequestcard

import (
	"context"
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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationAPIKey  = "0123456789abcdef0123456789abcdef"
	integrationToken   = "ATTAintegrationToken0123456789abcdef"
	integrationBoardID = "6512f0a1c2d3e4f5a6b70001"
	intakeListID       = "6512f0a1c2d3e4f5a6b70101"
	approvedListID     = "6512f0a1c2d3e4f5a6b70102"
	complianceLabelID  = "6512f0a1c2d3e4f5a6b70201"
	unknownLabelID     = "6512f0a1c2d3e4f5a6b70299"
	adaMemberID        = "6512f0a1c2d3e4f5a6b70301"
	missingCardID      = "6512f0a1c2d3e4f5a6ffffff"

	shortRequestTimeout = 500 * time.Millisecond
	// slowRequestTimeout outlasts the fake's nine-second responses and Dex's roughly seven-second async local phase.
	slowRequestTimeout = 20 * time.Second
	slowProviderDelay  = 9 * time.Second
	fakePageSize       = 2

	markerReject         = "[reject]"
	markerRateLimit      = "[rate-limit]"
	markerServerError    = "[server-error]"
	markerCreateTimeout  = "[create-timeout]"
	markerSlowCreate     = "[slow-create]"
	markerHoldCreate     = "[hold-create]"
	markerSlowUpdate     = "[slow-update]"
	markerSlowComment    = "[slow-comment]"
	markerCommentTimeout = "[comment-timeout]"

	existingRequestID = "REQ-2001"
)

var (
	cardPathPattern    = regexp.MustCompile(`^/1/cards/([0-9a-f]{24})(/actions/comments)?$`)
	boardCardsPattern  = regexp.MustCompile(`^/1/boards/([0-9a-f]{24})/cards$`)
	expectedAuthHeader = `OAuth oauth_consumer_key="` + integrationAPIKey + `", oauth_token="` + integrationToken + `"`
	integrationDue     = time.Date(2026, 10, 15, 17, 0, 0, 0, time.UTC)
)

func TestNewRequestIsCreatedOnceAfterPagingTheBoardAndCommentedWithRealDex(t *testing.T) {
	provider := newFakeTrello(t)
	harness := newTrelloHarness(t, provider, shortRequestTimeout, DuplicateCheck{PageSize: fakePageSize})

	record := harness.runRequestFlow(t, "created", requestInput("REQ-1042", "Replace badge reader"))
	require.Equal(t, PhaseReady, record.Phase)
	require.False(t, record.IsExistingCardReused, "neither [REQ-10420] nor an archived [REQ-1042] card is the same open request")
	require.Equal(t, 2, record.OpenCardPagesRead, "the duplicate check followed nextBefore to the second page")
	require.Equal(t, 1, record.CreateAttempts)
	require.NotEmpty(t, record.CommentID)
	require.False(t, record.IsCommentOutcomeUnknown)

	cardName := "[REQ-1042] Replace badge reader"
	cardID := provider.cardIDForName(cardName)
	require.Equal(t, cardID, record.CardID)
	require.Equal(t, 1, provider.createCount(cardName))
	card := provider.snapshot(cardID)
	require.Equal(t, approvedListID, card.listID)
	require.Equal(t, []string{complianceLabelID}, card.labelIDs)
	require.Equal(t, []string{adaMemberID}, card.memberIDs)
	require.Equal(t, "2026-10-15T17:00:00.000Z", card.due)
	require.Equal(t, []string{"Approved by Grace Hopper.\n\nBudget code FAC-7."}, provider.commentTexts(cardID))
	queries := provider.listQueries()
	require.Len(t, queries, 2)
	require.Equal(t, "open", queries[0].Get("filter"))
	require.Equal(t, "2", queries[0].Get("limit"))
	require.Equal(t, "-id", queries[0].Get("sort"))
	require.Empty(t, queries[0].Get("before"))
	require.Equal(t, fakeCardID(2), queries[1].Get("before"), "the second page continues before the oldest card of the first")
}

func TestOpenCardWithTheRequestIDIsMovedLabeledAndCommentedWithRealDex(t *testing.T) {
	provider := newFakeTrello(t)
	harness := newTrelloHarness(t, provider, shortRequestTimeout, DuplicateCheck{PageSize: fakePageSize})

	record := harness.runRequestFlow(t, "reused", requestInput(existingRequestID, "Badge reader offline"))
	require.Equal(t, PhaseReady, record.Phase)
	require.True(t, record.IsExistingCardReused)
	require.Equal(t, fakeCardID(4), record.CardID)
	require.Zero(t, record.CreateAttempts)
	require.Zero(t, provider.totalCreateCount())
	require.Equal(t, approvedListID, record.Card.ListID, "moving the card to the approved list is its status change")
	require.True(t, record.Card.HasLabel(complianceLabelID))
	card := provider.snapshot(fakeCardID(4))
	require.Equal(t, approvedListID, card.listID)
	require.Equal(t, []string{"6512f0a1c2d3e4f5a6b70202", complianceLabelID}, card.labelIDs, "the card's own label is kept")
	require.Len(t, provider.commentTexts(fakeCardID(4)), 1)
}

func TestFullDuplicateCheckStopsAsIncompleteWithoutCreatingWithRealDex(t *testing.T) {
	provider := newFakeTrello(t)
	harness := newTrelloHarness(t, provider, shortRequestTimeout, DuplicateCheck{PageSize: fakePageSize, MaximumPages: 1})

	record := harness.runRequestFlow(t, "incomplete", requestInput("REQ-1042", "Replace badge reader"))
	require.Equal(t, PhaseDuplicateCheckIncomplete, record.Phase)
	require.Zero(t, provider.totalCreateCount(), "an incomplete duplicate check never creates")
}

func TestRejectedCreateCompletesAsRejectedWithRealDex(t *testing.T) {
	provider := newFakeTrello(t)
	harness := newTrelloHarness(t, provider, shortRequestTimeout, DuplicateCheck{PageSize: fakePageSize})

	record := harness.runRequestFlow(t, "rejected", requestInput("REQ-3001", markerReject+" Paint the lobby"))
	require.Equal(t, PhaseRejected, record.Phase)
	require.Empty(t, record.CardID)
	require.Equal(t, 1, provider.createCount("[REQ-3001] "+markerReject+" Paint the lobby"))
}

func TestRateLimitedCreateIsRetriedAfterRetryAfterWithRealDex(t *testing.T) {
	provider := newFakeTrello(t)
	harness := newTrelloHarness(t, provider, shortRequestTimeout, DuplicateCheck{PageSize: fakePageSize})

	record := harness.runRequestFlow(t, "rate-limited", requestInput("REQ-3002", markerRateLimit+" Door sensor noisy"))
	require.Equal(t, PhaseReady, record.Phase)
	require.Equal(t, 1, record.CreateAttempts, "a 429 created nothing and cleared the checkpoint, so Dex retried the same Step execution")
	attempts := provider.createTimesFor("[REQ-3002] " + markerRateLimit + " Door sensor noisy")
	require.Len(t, attempts, 2)
	require.GreaterOrEqual(t, attempts[1].Sub(attempts[0]), time.Second, "the retry waits for Retry-After")
}

func TestTimeoutAfterDispatchIsReconciledWithoutCreatingAgainWithRealDex(t *testing.T) {
	provider := newFakeTrello(t)
	harness := newTrelloHarness(t, provider, shortRequestTimeout, DuplicateCheck{PageSize: fakePageSize})
	cardName := "[REQ-3003] " + markerCreateTimeout + " Lift jammed"
	flowID := harness.startRequestFlow(t, "timeout", requestInput("REQ-3003", markerCreateTimeout+" Lift jammed"))

	uncertain := harness.waitForPhase(t, flowID, PhaseNeedsReconciliation)
	require.NotNil(t, uncertain.UncertainCreate)
	require.Equal(t, sdkgo.FailureTransport, uncertain.UncertainCreate.FailureKind)
	require.NotEmpty(t, uncertain.UncertainCreate.CallID)
	require.Equal(t, 1, provider.createCount(cardName), "a timeout after dispatch is never retried")

	ctx := integrationContext(t)
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, harness.flow.ConfirmCreatedCard, ConfirmCreatedCardInput{CardID: missingCardID}, nil))
	harness.waitForReconciliationNote(t, flowID, NoteReportedCardMissing)
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, harness.flow.ConfirmCreatedCard, ConfirmCreatedCardInput{CardID: fakeCardID(1)}, nil))
	harness.waitForReconciliationNote(t, flowID, NoteReportedCardMismatch)

	createdID := provider.cardIDForName(cardName)
	require.NoError(t, harness.client.InvokeRPC(ctx, flowID, harness.flow.ConfirmCreatedCard, ConfirmCreatedCardInput{CardID: " " + createdID + " "}, nil))
	record := harness.waitForFlowOutput(t, flowID)
	require.Equal(t, PhaseReady, record.Phase)
	require.Equal(t, createdID, record.CardID)
	require.Nil(t, record.UncertainCreate)
	require.Equal(t, 1, record.CreateAttempts)
	require.Equal(t, 1, provider.createCount(cardName), "reconciliation adopted the existing card")
	require.Len(t, provider.commentTexts(createdID), 1)
}

func TestServerErrorIsCreatedAgainOnlyAfterOperatorApprovalWithRealDex(t *testing.T) {
	provider := newFakeTrello(t)
	harness := newTrelloHarness(t, provider, shortRequestTimeout, DuplicateCheck{PageSize: fakePageSize})
	cardName := "[REQ-3004] " + markerServerError + " HVAC alarm"
	flowID := harness.startRequestFlow(t, "server-error", requestInput("REQ-3004", markerServerError+" HVAC alarm"))

	uncertain := harness.waitForPhase(t, flowID, PhaseNeedsReconciliation)
	require.Equal(t, sdkgo.FailureAvailability, uncertain.UncertainCreate.FailureKind)
	require.Equal(t, 1, provider.createCount(cardName))

	require.NoError(t, harness.client.InvokeRPC(integrationContext(t), flowID, harness.flow.ApproveCardCreateRetry, nil, nil))
	record := harness.waitForFlowOutput(t, flowID)
	require.Equal(t, PhaseReady, record.Phase)
	require.Equal(t, 2, record.CreateAttempts)
	require.Equal(t, 2, provider.createCount(cardName), "only the approved retry sent a second create")
}

// TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex is the duplicate-dispatch test: an async fallback
// attempt would send a create that outlasts Dex's roughly seven-second local phase a second time.
func TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex(t *testing.T) {
	provider := newFakeTrello(t)
	harness := newTrelloHarness(t, provider, slowRequestTimeout, DuplicateCheck{PageSize: fakePageSize})

	startedAt := time.Now()
	record := harness.runRequestFlow(t, "slow-create", requestInput("REQ-3005", markerSlowCreate+" Generator test overdue"))
	require.GreaterOrEqual(t, time.Since(startedAt), slowProviderDelay)
	require.Equal(t, PhaseReady, record.Phase)
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.createCount("[REQ-3005] "+markerSlowCreate+" Generator test overdue"), "one Step execution dispatches one create request")
	require.Equal(t, 1, provider.totalCreateCount())
}

// TestSlowMoveBackupAttemptLeavesTheSameCardWithRealDex proves updateCard is safe under async durability: a
// backup attempt reads the labels again, resends the same absolute values, and the card ends the same.
func TestSlowMoveBackupAttemptLeavesTheSameCardWithRealDex(t *testing.T) {
	provider := newFakeTrello(t)
	slowCardID := provider.seedCard("[REQ-3006] "+markerSlowUpdate+" Sprinkler inspection", intakeListID, false)
	harness := newTrelloHarness(t, provider, slowRequestTimeout, DuplicateCheck{PageSize: fakePageSize})

	record := harness.runRequestFlow(t, "slow-move", requestInput("REQ-3006", markerSlowUpdate+" Sprinkler inspection"))
	require.Equal(t, PhaseReady, record.Phase)
	require.Equal(t, slowCardID, record.CardID)
	provider.waitForDelayedRequests(t)
	bodies := provider.updateBodies(slowCardID)
	require.GreaterOrEqual(t, len(bodies), 2, "Dex dispatched a backup attempt while the first nine-second update was in flight")
	for _, body := range bodies {
		require.JSONEq(t, bodies[0], body, "every attempt sends the same absolute values")
	}
	card := provider.snapshot(slowCardID)
	require.Equal(t, approvedListID, card.listID)
	require.Equal(t, []string{complianceLabelID}, card.labelIDs, "the label was added once")
	require.Equal(t, "2026-10-15T17:00:00.000Z", card.due)
	require.Len(t, provider.commentTexts(slowCardID), 1, "the Flow moved on exactly once")
	t.Logf("slow move: updates=%d", len(bodies))
}

// TestSlowCommentIsSentOnceUnderSyncDurabilityWithRealDex guards addComment's sync durability.
func TestSlowCommentIsSentOnceUnderSyncDurabilityWithRealDex(t *testing.T) {
	provider := newFakeTrello(t)
	harness := newTrelloHarness(t, provider, slowRequestTimeout, DuplicateCheck{PageSize: fakePageSize})

	cardName := "[REQ-3007] " + markerSlowComment + " Exit sign dark"
	record := harness.runRequestFlow(t, "slow-comment", requestInput("REQ-3007", markerSlowComment+" Exit sign dark"))
	require.Equal(t, PhaseReady, record.Phase)
	require.NotEmpty(t, record.CommentID)
	provider.waitForDelayedRequests(t)
	require.Len(t, provider.commentTexts(provider.cardIDForName(cardName)), 1, "one Step execution dispatches one comment request")
}

func TestCommentTimeoutIsRecordedAndNeverResentWithRealDex(t *testing.T) {
	provider := newFakeTrello(t)
	harness := newTrelloHarness(t, provider, shortRequestTimeout, DuplicateCheck{PageSize: fakePageSize})

	cardName := "[REQ-3008] " + markerCommentTimeout + " Freight door stuck"
	record := harness.runRequestFlow(t, "comment-timeout", requestInput("REQ-3008", markerCommentTimeout+" Freight door stuck"))
	require.Equal(t, PhaseReady, record.Phase)
	require.True(t, record.IsCommentOutcomeUnknown)
	require.Empty(t, record.CommentID)
	require.Len(t, provider.commentTexts(provider.cardIDForName(cardName)), 1, "an uncertain comment is never re-sent")
}

// TestLostWorkerDuringCreateSelectsUncertainWithoutResendingWithRealDex replaces the Worker while Trello holds
// the create; the next attempt finds the heartbeat checkpoint and sends nothing, and an operator confirms the card.
func TestLostWorkerDuringCreateSelectsUncertainWithoutResendingWithRealDex(t *testing.T) {
	provider := newFakeTrello(t)
	provider.holdsCreate = make(chan struct{})
	harness := newTrelloHarness(t, provider, slowRequestTimeout, DuplicateCheck{PageSize: fakePageSize})
	cardName := "[REQ-3009] " + markerHoldCreate + " Roof hatch alarm"
	flowID := harness.startRequestFlow(t, "lost-worker", requestInput("REQ-3009", markerHoldCreate+" Roof hatch alarm"))
	require.Eventually(t, func() bool { return provider.createCount(cardName) == 1 }, 30*time.Second, 50*time.Millisecond,
		"the first attempt must reach Trello")
	harness.replaceWorker(t)

	uncertain := harness.waitForPhase(t, flowID, PhaseNeedsReconciliation)
	close(provider.holdsCreate)
	require.Equal(t, "an earlier attempt of this Step may have sent the request, so it is not sent again", uncertain.UncertainCreate.FailureMessage,
		"the new Worker's attempt found the checkpoint the lost attempt recorded")
	provider.waitForDelayedRequests(t)
	require.Equal(t, 1, provider.createCount(cardName), "the attempt on the new Worker did not resend the create")

	createdID := provider.cardIDForName(cardName)
	require.NoError(t, harness.client.InvokeRPC(integrationContext(t), flowID, harness.flow.ConfirmCreatedCard, ConfirmCreatedCardInput{CardID: createdID}, nil))
	record := harness.waitForFlowOutput(t, flowID)
	require.Equal(t, PhaseReady, record.Phase)
	require.Equal(t, 1, provider.createCount(cardName))
}

func TestRejectedMoveCompletesWithTheExistingCardWithRealDex(t *testing.T) {
	provider := newFakeTrello(t)
	harness := newTrelloHarness(t, provider, shortRequestTimeout, DuplicateCheck{PageSize: fakePageSize})

	input := requestInput(existingRequestID, "Badge reader offline")
	input.LabelIDs = []string{unknownLabelID}
	record := harness.runRequestFlow(t, "rejected-move", input)
	require.Equal(t, PhaseMoveRejected, record.Phase)
	require.Equal(t, fakeCardID(4), record.CardID)
	require.Equal(t, intakeListID, provider.snapshot(fakeCardID(4)).listID, "Trello applied nothing")
	require.Empty(t, provider.commentTexts(fakeCardID(4)), "no approval comment on a card that could not be moved")
}

func requestInput(requestID string, title string) Input {
	due := integrationDue
	return Input{
		RequestID: requestID, Title: title, Details: "Badge reader at door 4 is offline.\nFacilities approved the spend.",
		ApprovedBy: "Grace Hopper", ApprovalNote: "Budget code FAC-7.", BoardID: integrationBoardID, ListID: approvedListID,
		DueAt: &due, LabelIDs: []string{complianceLabelID}, MemberIDs: []string{adaMemberID},
	}
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// fakeTrello is a credential-safe, stateful Trello fake. A marker in the card name selects a failure.
type fakeTrello struct {
	*httptest.Server
	t                *testing.T
	mutex            sync.Mutex
	delayedRequests  sync.WaitGroup
	nextCardNumber   int
	nextActionNumber int
	cards            map[string]*fakeCard
	createTimes      map[string][]time.Time
	updateBodiesByID map[string][]string
	comments         map[string][]string
	queries          []url.Values
	holdsCreate      chan struct{}
}

type fakeCard struct {
	name      string
	listID    string
	isClosed  bool
	due       string
	labelIDs  []string
	memberIDs []string
}

var fakeLists = map[string]string{intakeListID: "Intake", approvedListID: "Approved"}

var fakeLabels = map[string]string{complianceLabelID: "Compliance", "6512f0a1c2d3e4f5a6b70202": "Facilities"}

func fakeCardID(number int) string { return fmt.Sprintf("6512f0a1c2d3e4f5a6%06x", number) }

func newFakeTrello(t *testing.T) *fakeTrello {
	t.Helper()
	provider := &fakeTrello{
		t: t, nextCardNumber: 100, nextActionNumber: 1,
		cards: map[string]*fakeCard{
			fakeCardID(1): {name: "[REQ-10420] Replace badge reader", listID: intakeListID},
			fakeCardID(2): {name: "Follow-up on REQ-1042", listID: intakeListID},
			fakeCardID(3): {name: "[REQ-1042] Replace badge reader", listID: approvedListID, isClosed: true},
			fakeCardID(4): {name: "[" + existingRequestID + "] Badge reader offline", listID: intakeListID, labelIDs: []string{"6512f0a1c2d3e4f5a6b70202"}},
		},
		createTimes: map[string][]time.Time{}, updateBodiesByID: map[string][]string{}, comments: map[string][]string{},
	}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *fakeTrello) serveHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Authorization") != expectedAuthHeader || request.URL.Query().Has("token") || request.URL.Query().Has("key") {
		provider.writeText(response, http.StatusUnauthorized, "invalid key SENTINEL")
		return
	}
	contents, err := io.ReadAll(request.Body)
	require.NoError(provider.t, err)
	if match := boardCardsPattern.FindStringSubmatch(request.URL.Path); match != nil && request.Method == http.MethodGet {
		provider.listBoardCards(response, request, match[1])
		return
	}
	if request.Method == http.MethodPost && request.URL.Path == "/1/cards" {
		provider.createCard(response, request, contents)
		return
	}
	match := cardPathPattern.FindStringSubmatch(request.URL.Path)
	if match == nil {
		provider.writeText(response, http.StatusNotFound, "Cannot "+request.Method+" SENTINEL")
		return
	}
	provider.mutex.Lock()
	card, exists := provider.cards[match[1]]
	provider.mutex.Unlock()
	if !exists {
		provider.writeText(response, http.StatusNotFound, "The requested resource was not found. SENTINEL")
		return
	}
	switch {
	case request.Method == http.MethodGet && match[2] == "":
		provider.mutex.Lock()
		body := provider.cardJSON(match[1], request.URL.Query().Get("members") == "true")
		provider.mutex.Unlock()
		provider.writeJSON(response, http.StatusOK, body)
	case request.Method == http.MethodPut && match[2] == "":
		provider.updateCard(response, match[1], card, contents)
	case request.Method == http.MethodPost && match[2] != "":
		provider.addComment(response, request, match[1], card, contents)
	default:
		provider.writeText(response, http.StatusNotFound, "Cannot "+request.Method+" SENTINEL")
	}
}

func (provider *fakeTrello) listBoardCards(response http.ResponseWriter, request *http.Request, boardID string) {
	query := request.URL.Query()
	provider.mutex.Lock()
	provider.queries = append(provider.queries, query)
	var cardIDs []string
	for cardID, card := range provider.cards {
		isOpen := !card.isClosed
		if boardID == integrationBoardID && (query.Get("filter") != "open" || isOpen) && (query.Get("before") == "" || cardID < query.Get("before")) {
			cardIDs = append(cardIDs, cardID)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(cardIDs)))
	limit, err := strconv.Atoi(query.Get("limit"))
	require.NoError(provider.t, err)
	if len(cardIDs) > limit {
		cardIDs = cardIDs[:limit]
	}
	rendered := make([]string, 0, len(cardIDs))
	for _, cardID := range cardIDs {
		rendered = append(rendered, provider.cardJSON(cardID, false))
	}
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, "["+strings.Join(rendered, ",")+"]")
}

func (provider *fakeTrello) createCard(response http.ResponseWriter, request *http.Request, contents []byte) {
	var body struct {
		IDList    string `json:"idList"`
		Name      string `json:"name"`
		Due       string `json:"due"`
		IDLabels  string `json:"idLabels"`
		IDMembers string `json:"idMembers"`
	}
	require.NoError(provider.t, json.Unmarshal(contents, &body))
	name := body.Name
	provider.mutex.Lock()
	provider.createTimes[name] = append(provider.createTimes[name], time.Now())
	attempt := len(provider.createTimes[name])
	provider.mutex.Unlock()
	if _, isKnownList := fakeLists[body.IDList]; !isKnownList {
		provider.writeText(response, http.StatusNotFound, "could not find the board that the card belongs to SENTINEL")
		return
	}
	switch {
	case strings.Contains(name, markerReject):
		provider.writeText(response, http.StatusBadRequest, "invalid value for name SENTINEL")
	case strings.Contains(name, markerRateLimit) && attempt == 1:
		response.Header().Set("Retry-After", "1")
		provider.writeJSON(response, http.StatusTooManyRequests, `{"error":"API_TOKEN_LIMIT_EXCEEDED","message":"Rate limit exceeded SENTINEL"}`)
	case strings.Contains(name, markerServerError) && attempt == 1:
		provider.writeText(response, http.StatusInternalServerError, "SENTINEL internal error")
	case strings.Contains(name, markerCreateTimeout):
		provider.storeCard(body.Name, body.IDList, body.Due, body.IDLabels, body.IDMembers)
		// Trello created the card, but the response never arrives before the connector gives up.
		<-request.Context().Done()
	case strings.Contains(name, markerHoldCreate):
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		provider.storeCard(body.Name, body.IDList, body.Due, body.IDLabels, body.IDMembers)
		select {
		case <-provider.holdsCreate:
		case <-request.Context().Done():
		}
	case strings.Contains(name, markerSlowCreate):
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		cardID := provider.storeCard(body.Name, body.IDList, body.Due, body.IDLabels, body.IDMembers)
		time.Sleep(slowProviderDelay)
		provider.writeCard(response, cardID)
	default:
		provider.writeCard(response, provider.storeCard(body.Name, body.IDList, body.Due, body.IDLabels, body.IDMembers))
	}
}

func (provider *fakeTrello) updateCard(response http.ResponseWriter, cardID string, card *fakeCard, contents []byte) {
	var body map[string]json.RawMessage
	require.NoError(provider.t, json.Unmarshal(contents, &body))
	var labelIDs []string
	if raw, isSet := body["idLabels"]; isSet {
		var joined string
		require.NoError(provider.t, json.Unmarshal(raw, &joined))
		labelIDs = splitIDs(joined)
		for _, labelID := range labelIDs {
			if _, isKnown := fakeLabels[labelID]; !isKnown {
				provider.writeText(response, http.StatusBadRequest, "invalid value for idLabels SENTINEL")
				return
			}
		}
	}
	provider.mutex.Lock()
	provider.updateBodiesByID[cardID] = append(provider.updateBodiesByID[cardID], string(contents))
	if raw, isSet := body["idList"]; isSet {
		require.NoError(provider.t, json.Unmarshal(raw, &card.listID))
	}
	if raw, isSet := body["closed"]; isSet {
		require.NoError(provider.t, json.Unmarshal(raw, &card.isClosed))
	}
	if raw, isSet := body["due"]; isSet {
		var due *string
		require.NoError(provider.t, json.Unmarshal(raw, &due))
		card.due = ""
		if due != nil {
			card.due = *due
		}
	}
	if _, isSet := body["idLabels"]; isSet {
		card.labelIDs = labelIDs
	}
	rendered := provider.cardJSON(cardID, false)
	provider.mutex.Unlock()
	if strings.Contains(card.name, markerSlowUpdate) {
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		// Trello applied the update, but the response arrives after Dex's async local phase ends.
		time.Sleep(slowProviderDelay)
	}
	provider.writeJSON(response, http.StatusOK, rendered)
}

func (provider *fakeTrello) addComment(response http.ResponseWriter, request *http.Request, cardID string, card *fakeCard, contents []byte) {
	var body struct {
		Text string `json:"text"`
	}
	require.NoError(provider.t, json.Unmarshal(contents, &body))
	provider.mutex.Lock()
	provider.comments[cardID] = append(provider.comments[cardID], body.Text)
	provider.nextActionNumber++
	actionID := fmt.Sprintf("6512f0a1c2d3e4f5a6e%05x", provider.nextActionNumber)
	provider.mutex.Unlock()
	switch {
	case strings.Contains(card.name, markerCommentTimeout):
		<-request.Context().Done()
		return
	case strings.Contains(card.name, markerSlowComment):
		provider.delayedRequests.Add(1)
		defer provider.delayedRequests.Done()
		time.Sleep(slowProviderDelay)
	}
	provider.writeJSON(response, http.StatusOK, fmt.Sprintf(`{"id":%q,"idMemberCreator":%q,"type":"commentCard",`+
		`"date":"2026-09-30T16:20:00.396Z","data":{"text":%q,"card":{"id":%q}}}`, actionID, adaMemberID, body.Text, cardID))
}

// seedCard adds an existing card to the board, newer than every seeded card.
func (provider *fakeTrello) seedCard(name string, listID string, isClosed bool) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	cardID := fakeCardID(50 + len(provider.cards))
	provider.cards[cardID] = &fakeCard{name: name, listID: listID, isClosed: isClosed}
	return cardID
}

func (provider *fakeTrello) storeCard(name string, listID string, due string, labelIDs string, memberIDs string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.nextCardNumber++
	cardID := fakeCardID(provider.nextCardNumber)
	provider.cards[cardID] = &fakeCard{name: name, listID: listID, due: due, labelIDs: splitIDs(labelIDs), memberIDs: splitIDs(memberIDs)}
	return cardID
}

func (provider *fakeTrello) writeCard(response http.ResponseWriter, cardID string) {
	provider.mutex.Lock()
	rendered := provider.cardJSON(cardID, false)
	provider.mutex.Unlock()
	provider.writeJSON(response, http.StatusOK, rendered)
}

// cardJSON renders a card the way Trello does; the caller holds the mutex.
func (provider *fakeTrello) cardJSON(cardID string, includesMembers bool) string {
	card := provider.cards[cardID]
	due := "null"
	if card.due != "" {
		due = strconv.Quote(card.due)
	}
	labelIDs, labels := []string{}, []string{}
	for _, labelID := range card.labelIDs {
		labelIDs = append(labelIDs, strconv.Quote(labelID))
		labels = append(labels, fmt.Sprintf(`{"id":%q,"idBoard":%q,"name":%q,"color":"green"}`, labelID, integrationBoardID, fakeLabels[labelID]))
	}
	memberIDs := []string{}
	for _, memberID := range card.memberIDs {
		memberIDs = append(memberIDs, strconv.Quote(memberID))
	}
	expansions := ""
	if includesMembers {
		expansions = fmt.Sprintf(`,"members":[],"list":{"id":%q,"name":%q}`, card.listID, fakeLists[card.listID])
	}
	return fmt.Sprintf(`{"id":%q,"shortLink":"Ab12Cd34","name":%q,"desc":"","idBoard":%q,"idList":%q,"closed":%t,"due":%s,`+
		`"dueComplete":false,"start":null,"idLabels":[%s],"labels":[%s],"idMembers":[%s],"pos":16384,`+
		`"url":"https://trello.com/c/Ab12Cd34/%s","shortUrl":"https://trello.com/c/Ab12Cd34","dateLastActivity":"2026-09-30T16:15:00.000Z"%s}`,
		cardID, card.name, integrationBoardID, card.listID, card.isClosed, due,
		strings.Join(labelIDs, ","), strings.Join(labels, ","), strings.Join(memberIDs, ","), cardID, expansions)
}

func (provider *fakeTrello) writeJSON(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		provider.t.Logf("fake Trello response write failed: %v", err)
	}
}

func (provider *fakeTrello) writeText(response http.ResponseWriter, status int, body string) {
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(status)
	if _, err := response.Write([]byte(body)); err != nil {
		provider.t.Logf("fake Trello response write failed: %v", err)
	}
}

// waitForDelayedRequests waits for every slow or held fake response, so a late duplicate would be counted.
func (provider *fakeTrello) waitForDelayedRequests(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		provider.delayedRequests.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Minute):
		t.Fatal("delayed fake Trello requests did not finish")
	}
}

func (provider *fakeTrello) createCount(name string) int { return len(provider.createTimesFor(name)) }

func (provider *fakeTrello) createTimesFor(name string) []time.Time {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]time.Time(nil), provider.createTimes[name]...)
}

func (provider *fakeTrello) totalCreateCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	total := 0
	for _, times := range provider.createTimes {
		total += len(times)
	}
	return total
}

func (provider *fakeTrello) updateBodies(cardID string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.updateBodiesByID[cardID]...)
}

func (provider *fakeTrello) commentTexts(cardID string) []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.comments[cardID]...)
}

func (provider *fakeTrello) listQueries() []url.Values {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]url.Values(nil), provider.queries...)
}

func (provider *fakeTrello) snapshot(cardID string) fakeCard {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return *provider.cards[cardID]
}

// cardIDForName returns the newest card with name, so an archived decoy with the same name is skipped.
func (provider *fakeTrello) cardIDForName(name string) string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	newestCardID := ""
	for cardID, card := range provider.cards {
		if card.name == name && cardID > newestCardID {
			newestCardID = cardID
		}
	}
	if newestCardID == "" {
		provider.t.Fatalf("fake Trello has no card named %q", name)
	}
	return newestCardID
}

func splitIDs(joined string) []string {
	if joined == "" {
		return nil
	}
	return strings.Split(joined, ",")
}

// trelloHarness owns a real Worker and Client against the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
type trelloHarness struct {
	flow          *Flow
	registry      *dex.Registry
	cache         *blobcache.Cache
	serverAddress string
	workerAddress string
	worker        *dex.Worker
	workerResult  chan error
	client        *dex.Client
}

func newTrelloHarness(t *testing.T, provider *fakeTrello, requestTimeout time.Duration, duplicateCheck DuplicateCheck) *trelloHarness {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "trello", Name: ConnectionName}
	providerClient, err := trello.New(trello.Config{Endpoint: provider.URL + "/1"},
		sdkgo.StaticCredentialProvider[trello.Credentials]{reference: {
			APIKey: sdkgo.NewSecretString(integrationAPIKey), Token: sdkgo.NewSecretString(integrationToken),
		}},
		trello.WithHTTPClient(&http.Client{Timeout: requestTimeout}),
	)
	require.NoError(t, err)
	connection, err := trello.NewConnection(providerClient, reference)
	require.NoError(t, err)
	harness := &trelloHarness{flow: NewFlow(connection, duplicateCheck), serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
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

func (harness *trelloHarness) startWorker(t *testing.T) {
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
func (harness *trelloHarness) replaceWorker(t *testing.T) {
	t.Helper()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	// A forced stop returns the expired context's error; losing the in-flight handler is the point.
	_ = harness.worker.Stop(expired)
	require.NoError(t, <-harness.workerResult)
	harness.startWorker(t)
}

func (harness *trelloHarness) runRequestFlow(t *testing.T, scenario string, input Input) RequestCard {
	t.Helper()
	return harness.waitForFlowOutput(t, harness.startRequestFlow(t, scenario, input))
}

func (harness *trelloHarness) startRequestFlow(t *testing.T, scenario string, input Input) string {
	t.Helper()
	flowID := "trello-request-card-" + scenario + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err := harness.client.StartFlow(integrationContext(t), harness.flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err, "start Flow %s on the Dex Server at %s", flowID, harness.serverAddress)
	return flowID
}

// waitForFlowOutput waits, across server long-poll caps, until the Flow completes.
func (harness *trelloHarness) waitForFlowOutput(t *testing.T, flowID string) RequestCard {
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
		require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
		var record RequestCard
		require.NoError(t, result.DecodeSingleOutput(&record))
		return record
	}
}

func (harness *trelloHarness) waitForPhase(t *testing.T, flowID string, phase string) RequestCard {
	t.Helper()
	return harness.waitForRecord(t, flowID, func(record RequestCard) bool { return record.Phase == phase })
}

func (harness *trelloHarness) waitForReconciliationNote(t *testing.T, flowID string, note string) RequestCard {
	t.Helper()
	return harness.waitForRecord(t, flowID, func(record RequestCard) bool {
		return record.Phase == PhaseNeedsReconciliation && record.ReconciliationNote == note
	})
}

func (harness *trelloHarness) waitForRecord(t *testing.T, flowID string, isExpected func(RequestCard) bool) RequestCard {
	t.Helper()
	ctx := integrationContext(t)
	var record RequestCard
	require.Eventually(t, func() bool {
		record = RequestCard{}
		return harness.client.InvokeRPC(ctx, flowID, harness.flow.GetRequestCard, nil, &record) == nil && isExpected(record)
	}, time.Minute, 100*time.Millisecond, "Flow %s last record %+v", flowID, record)
	return record
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
