// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func validCreateCardInput() trello.CreateCardInput {
	due := time.Date(2026, 10, 15, 17, 0, 0, 0, time.FixedZone("PDT", -7*60*60))
	return trello.CreateCardInput{
		ListID: testListID, Name: " [REQ-1042] Replace badge reader ", Description: "Badge reader at door 4 is offline.\nFacilities approved.",
		Due: &due, LabelIDs: []string{testLabelID, otherLabelID}, MemberIDs: []string{testMemberID}, Position: "top",
	}
}

func TestCreateCardSendsOneJSONRequestAfterRecordingTheDispatchMarker(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{name: "[REQ-1042] Replace badge reader"}))
	})
	ctx := newTrelloDexContext("create")
	result, err := sdkgo.RunMutation(ctx, newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
	require.NoError(t, err)
	require.Equal(t, trello.CreateCardBranchCreated, result.Branch)
	require.Equal(t, trello.CreateCardOutput{
		CardID: testCardID, Name: "[REQ-1042] Replace badge reader", ListID: testListID, BoardID: testBoardID, ShortLink: "LrrmgFyd",
		URL: "https://trello.com/c/LrrmgFyd/19-replace-badge-reader", ShortURL: "https://trello.com/c/LrrmgFyd",
	}, result.Value)
	require.Equal(t, testCardID, result.Receipt.ProviderObjectID)
	require.Equal(t, 1, provider.requestCount())
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/cards", request.path)
	require.Empty(t, request.rawQuery, "card content travels in the JSON body, never in the URL")
	require.Equal(t, "application/json", request.contentType)
	require.Empty(t, request.header.Get("Idempotency-Key"), "Trello documents no key, and Go would retry a keyed POST on its own")
	require.JSONEq(t, `{"idList":"`+testListID+`","name":"[REQ-1042] Replace badge reader",
		"desc":"Badge reader at door 4 is offline.\nFacilities approved.","due":"2026-10-16T00:00:00.000Z",
		"idLabels":"`+testLabelID+`,`+otherLabelID+`","idMembers":"`+testMemberID+`","pos":"top"}`, request.body)
	require.JSONEq(t, `{"trelloDispatchedCallId":"`+string(result.Receipt.CallID)+`"}`, string(ctx.recordedHeartbeat),
		"the dispatch marker is recorded before the request")
}

func TestCreateCardOmitsUnsetFieldsAndSendsANumericPosition(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{name: "Paint the lobby"}))
	})
	_, err := sdkgo.RunMutation(newTrelloDexContext("create-minimal"), newTrelloClient(t, provider.URL).CreateCard(), trelloConnection,
		trello.CreateCardInput{ListID: testListID, Name: "Paint the lobby", Description: "  ", Position: "16384.5"})
	require.NoError(t, err)
	require.JSONEq(t, `{"idList":"`+testListID+`","name":"Paint the lobby","pos":16384.5}`, provider.request(0).body)
}

func TestCreateCardNeverResendsARequestWhoseOutcomeIsUnknown(t *testing.T) {
	for _, test := range []struct {
		name   string
		reply  func(http.ResponseWriter, *http.Request)
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{name: "server error", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeText(t, response, http.StatusInternalServerError, "SENTINEL")
		}, branch: trello.CreateCardBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "gateway timeout", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeText(t, response, http.StatusGatewayTimeout, "SENTINEL")
		}, branch: trello.CreateCardBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "redirect", reply: func(response http.ResponseWriter, request *http.Request) {
			http.Redirect(response, request, "https://attacker.example/steal", http.StatusFound)
		}, branch: trello.CreateCardBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "lost response", reply: func(response http.ResponseWriter, _ *http.Request) { dropConnection(t, response) },
			branch: trello.CreateCardBranchUncertain, kind: sdkgo.FailureTransport},
		{name: "malformed card", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeJSON(t, response, http.StatusOK, `{"id":"SENTINEL"}`)
		}, branch: trello.CreateCardBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "reflected token", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{name: testToken}))
		}, branch: trello.CreateCardBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "invalid list", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeText(t, response, http.StatusBadRequest, "invalid value for idList SENTINEL")
		}, branch: trello.CreateCardBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "invalid token", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeText(t, response, http.StatusUnauthorized, "invalid token")
		}, branch: trello.CreateCardBranchProviderRejected, kind: sdkgo.FailureAuthentication},
		{name: "missing list", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeText(t, response, http.StatusNotFound, "could not find the board that the card belongs to")
		}, branch: trello.CreateCardBranchProviderRejected, kind: sdkgo.FailureNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingTrello(t, func(response http.ResponseWriter, request *http.Request, _ int) { test.reply(response, request) })
			ctx := newTrelloDexContext("create-" + test.name)
			result, err := sdkgo.RunMutation(ctx, newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
			require.NoError(t, err, "only a provable non-application is retried")
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, "[REQ-1042] Replace badge reader", result.Value.Name, "the requested card is echoed for reconciliation")
			require.Empty(t, result.Value.CardID)
			requireNoSentinel(t, result)
			require.Equal(t, 1, provider.requestCount())
			require.NotEmpty(t, ctx.recordedHeartbeat, "the marker stays, so a replayed attempt sends nothing")
		})
	}
}

func TestCreateCardRetriesOnlyWhatTrelloProvablyDidNotApply(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusTooManyRequests, `{"error":"API_TOKEN_LIMIT_EXCEEDED","message":"Rate limit exceeded"}`)
	})
	ctx := newTrelloDexContext("create-rate-limited")
	_, err := sdkgo.RunMutation(ctx, newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
	retry := requireRetry(t, err, sdkgo.FailureRateLimit)
	require.Contains(t, retry.Failure.Message, "API_TOKEN_LIMIT_EXCEEDED")
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 10*time.Second, retryAfter.After, "Trello documents no Retry-After, so one rate-limit window is waited out")
	require.Nil(t, ctx.recordedHeartbeat, "a 429 clears the marker so the retry may send")
	require.Equal(t, 2, ctx.heartbeatCount)

	refused := newTrelloDexContext("create-refused")
	_, err = sdkgo.RunMutation(refused, newTrelloClient(t, closedLoopbackURL(t)).CreateCard(), trelloConnection, validCreateCardInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Nil(t, refused.recordedHeartbeat, "a refused connection sent nothing")

	created := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{name: "[REQ-1042] Replace badge reader"}))
	})
	result, err := sdkgo.RunMutation(ctx.nextAttempt(), newTrelloClient(t, created.URL).CreateCard(), trelloConnection, validCreateCardInput())
	require.NoError(t, err)
	require.Equal(t, trello.CreateCardBranchCreated, result.Branch)
}

func TestCreateCardAfterAnEarlierDispatchSendsNothing(t *testing.T) {
	provider := newRecordingTrello(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	first := newTrelloDexContext("create-replay")
	first.recordedHeartbeat = json.RawMessage(`{"trelloDispatchedCallId":"earlier"}`)
	result, err := sdkgo.RunMutation(first.nextAttempt(), newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
	require.NoError(t, err)
	require.Equal(t, trello.CreateCardBranchUncertain, result.Branch)
	require.Equal(t, "an earlier attempt of this Step may have sent the request, so it is not sent again", result.Failure.Message)
	require.Equal(t, "[REQ-1042] Replace badge reader", result.Value.Name)

	unrecordable := newTrelloDexContext("create-unrecordable")
	unrecordable.rejectsHeartbeat = true
	_, err = sdkgo.RunMutation(unrecordable, newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Zero(t, provider.requestCount())
}

func TestCreateCardRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingTrello(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newTrelloClient(t, provider.URL)
	for name, change := range map[string]func(*trello.CreateCardInput){
		"blank name":        func(input *trello.CreateCardInput) { input.Name = " " },
		"two-line name":     func(input *trello.CreateCardInput) { input.Name = "one\ntwo" },
		"long name":         func(input *trello.CreateCardInput) { input.Name = strings.Repeat("x", 16385) },
		"control character": func(input *trello.CreateCardInput) { input.Description = "bell\a" },
		"long description":  func(input *trello.CreateCardInput) { input.Description = strings.Repeat("x", 16385) },
		"missing list":      func(input *trello.CreateCardInput) { input.ListID = "" },
		"list short link":   func(input *trello.CreateCardInput) { input.ListID = "LrrmgFyd" },
		"invalid label":     func(input *trello.CreateCardInput) { input.LabelIDs = []string{"Compliance"} },
		"duplicate member":  func(input *trello.CreateCardInput) { input.MemberIDs = []string{testMemberID, testMemberID} },
		"zero position":     func(input *trello.CreateCardInput) { input.Position = "0" },
		"named position":    func(input *trello.CreateCardInput) { input.Position = "middle" },
		"too many labels":   func(input *trello.CreateCardInput) { input.LabelIDs = make([]string, 51) },
	} {
		input := validCreateCardInput()
		change(&input)
		ctx := newTrelloDexContext("create-invalid")
		result, err := sdkgo.RunMutation(ctx, client.CreateCard(), trelloConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, trello.CreateCardBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
		require.Nil(t, ctx.recordedHeartbeat, "%s: nothing is claimed for invalid input", name)
	}
}
