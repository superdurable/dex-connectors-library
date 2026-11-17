// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello/go"
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
	require.JSONEq(t, `{"callId":"`+string(result.Receipt.CallID)+`","attempt":1,"state":"claimed","sendCount":1,"providerId":"`+testCardID+`"}`, string(ctx.recordedHeartbeat),
		"the claim, recorded before the request, keeps the created card ID")
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

func TestCreateCardResendsAnUnknownOutcomeUpToTheSendCap(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter, *http.Request)
		kind  sdkgo.FailureKind
	}{
		{name: "server error", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeText(t, response, http.StatusInternalServerError, "SENTINEL")
		}, kind: sdkgo.FailureAvailability},
		{name: "gateway timeout", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeText(t, response, http.StatusGatewayTimeout, "SENTINEL")
		}, kind: sdkgo.FailureAvailability},
		{name: "conflict without key", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeText(t, response, http.StatusConflict, "SENTINEL")
		}, kind: sdkgo.FailureConflict},
		{name: "unlisted client error", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeText(t, response, http.StatusGone, "SENTINEL")
		}, kind: sdkgo.FailureAvailability},
		{name: "redirect", reply: func(response http.ResponseWriter, request *http.Request) {
			http.Redirect(response, request, "https://attacker.example/steal", http.StatusFound)
		}, kind: sdkgo.FailureAvailability},
		{name: "lost response", reply: func(response http.ResponseWriter, _ *http.Request) { dropConnection(t, response) }, kind: sdkgo.FailureTransport},
		{name: "malformed card", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeJSON(t, response, http.StatusOK, `{"id":"SENTINEL"}`)
		}, kind: sdkgo.FailureProtocol},
		{name: "reflected token", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{name: testToken}))
		}, kind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingTrello(t, func(response http.ResponseWriter, request *http.Request, _ int) { test.reply(response, request) })
			ctx := newTrelloDexContext("create-" + test.name)
			sendCap := int(trello.CreateCardDefinition.MaxSends)
			for sends := 1; sends < sendCap; sends++ {
				_, err := sdkgo.RunMutation(ctx, newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
				requireRetry(t, err, test.kind)
				require.Equal(t, sends, provider.requestCount(), "an unknown outcome is resent below the send cap")
				ctx = ctx.nextAttempt()
			}
			result, err := sdkgo.RunMutation(ctx, newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
			require.NoError(t, err)
			require.Equal(t, trello.CreateCardBranchUncertain, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, "[REQ-1042] Replace badge reader", result.Value.Name, "the requested card is echoed for reconciliation")
			require.Empty(t, result.Value.CardID)
			requireNoSentinel(t, result)
			replay, err := sdkgo.RunMutation(ctx.nextAttempt(), newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
			require.NoError(t, err)
			require.Equal(t, trello.CreateCardBranchUncertain, replay.Branch)
			require.Equal(t, sendCap, provider.requestCount(), "no attempt sends beyond the cap")
		})
	}
}

func TestCreateCardDefiniteRefusalsRecordCanResendAndAreNeverResent(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter, *http.Request)
		kind  sdkgo.FailureKind
	}{
		{name: "invalid list", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeText(t, response, http.StatusBadRequest, "invalid value for idList SENTINEL")
		}, kind: sdkgo.FailureValidation},
		{name: "invalid token", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeText(t, response, http.StatusUnauthorized, "invalid token")
		}, kind: sdkgo.FailureAuthentication},
		{name: "missing list", reply: func(response http.ResponseWriter, _ *http.Request) {
			writeText(t, response, http.StatusNotFound, "could not find the board that the card belongs to")
		}, kind: sdkgo.FailureNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingTrello(t, func(response http.ResponseWriter, request *http.Request, _ int) { test.reply(response, request) })
			ctx := newTrelloDexContext("create-" + test.name)
			result, err := sdkgo.RunMutation(ctx, newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
			require.NoError(t, err)
			require.Equal(t, trello.CreateCardBranchProviderRejected, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			requireNoSentinel(t, result)
			require.Equal(t, 1, provider.requestCount())
			require.Contains(t, string(ctx.recordedHeartbeat), `"state":"canResend"`)
		})
	}
}

func TestCreateCardRefusalOfAResendIsUncertain(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeText(t, response, http.StatusBadGateway, "SENTINEL")
			return
		}
		writeText(t, response, http.StatusBadRequest, "invalid value for idList SENTINEL")
	})
	ctx := newTrelloDexContext("create-resend-refused")
	_, err := sdkgo.RunMutation(ctx, newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	result, err := sdkgo.RunMutation(ctx.nextAttempt(), newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
	require.NoError(t, err)
	require.Equal(t, trello.CreateCardBranchUncertain, result.Branch, "the first POST may have created the card")
	require.Equal(t, 2, provider.requestCount())
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
	require.Contains(t, string(ctx.recordedHeartbeat), `"state":"canResend"`, "a 429 records a safe retry")

	refused := newTrelloDexContext("create-refused")
	_, err = sdkgo.RunMutation(refused, newTrelloClient(t, closedLoopbackURL(t)).CreateCard(), trelloConnection, validCreateCardInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Contains(t, string(refused.recordedHeartbeat), `"state":"canResend"`, "a refused connection sent nothing")

	created := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{name: "[REQ-1042] Replace badge reader"}))
	})
	result, err := sdkgo.RunMutation(ctx.nextAttempt(), newTrelloClient(t, created.URL).CreateCard(), trelloConnection, validCreateCardInput())
	require.NoError(t, err)
	require.Equal(t, trello.CreateCardBranchCreated, result.Branch)
}

func TestCreateCardRetriesServiceUnavailableButNeverResendsAnAcceptedCard(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			response.Header().Set("Retry-After", "2")
			writeText(t, response, http.StatusServiceUnavailable, "SENTINEL")
			return
		}
		writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{name: "[REQ-1042] Replace badge reader"}))
	})
	first := newTrelloDexContext("create-service-unavailable")
	_, err := sdkgo.RunMutation(first, newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 2*time.Second, retryAfter.After)
	require.Contains(t, string(first.recordedHeartbeat), `"state":"canResend"`)
	second := first.nextAttempt()
	result, err := sdkgo.RunMutation(second, newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
	require.NoError(t, err)
	require.Equal(t, trello.CreateCardBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCount())

	result, err = sdkgo.RunMutation(second.nextAttempt(), newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
	require.NoError(t, err)
	require.Equal(t, trello.CreateCardBranchUncertain, result.Branch, "the saved card ID forbids a resend")
	require.Equal(t, 2, provider.requestCount())
}

func TestCreateCardCountsAMissingOrStaleCheckpointTowardTheCap(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeText(t, response, http.StatusServiceUnavailable, "SENTINEL")
	})
	client := newTrelloClient(t, provider.URL)
	first := newTrelloDexContext("create-stale-retry")
	_, err := sdkgo.RunMutation(first, client.CreateCard(), trelloConnection, validCreateCardInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	lost := first.nextAttempt()
	lost.previousHeartbeat = nil
	_, err = sdkgo.RunMutation(lost, client.CreateCard(), trelloConnection, validCreateCardInput(), sdkgo.WithMaxSends(2))
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, 2, provider.requestCount(), "one counted earlier attempt is below a cap of 2")
	third := lost.nextAttempt()
	third.previousHeartbeat = first.recordedHeartbeat
	_, err = sdkgo.RunMutation(third, client.CreateCard(), trelloConnection, validCreateCardInput(), sdkgo.WithMaxSends(2))
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, 3, provider.requestCount(), "the stale grant counts attempt two, which is below a cap of 2")
	fourth := third.nextAttempt()
	fourth.previousHeartbeat = first.recordedHeartbeat
	result, err := sdkgo.RunMutation(fourth, client.CreateCard(), trelloConnection, validCreateCardInput(), sdkgo.WithMaxSends(2))
	require.NoError(t, err)
	require.Equal(t, trello.CreateCardBranchUncertain, result.Branch, "the stale grant counts both later attempts")
	require.Equal(t, 3, provider.requestCount())
}

func TestCreateCardRetriesAfterClaimHeartbeatFails(t *testing.T) {
	provider := newRecordingTrello(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, cardJSON(cardJSONOptions{name: "[REQ-1042] Replace badge reader"}))
	})
	client := newTrelloClient(t, provider.URL)
	first := newTrelloDexContext("create-heartbeat-failure")
	first.heartbeatFailuresRemaining = 1
	_, err := sdkgo.RunMutation(first, client.CreateCard(), trelloConnection, validCreateCardInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Zero(t, provider.requestCount())
	require.Contains(t, string(first.recordedHeartbeat), `"state":"notSent"`)
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.CreateCard(), trelloConnection, validCreateCardInput())
	require.NoError(t, err)
	require.Equal(t, trello.CreateCardBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateCardRetriesWithoutSendingWhenAttemptWindowIsTooShort(t *testing.T) {
	provider := newRecordingTrello(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	ctx := newTrelloDexContext("create-short-window")
	attemptContext, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	ctx.Context = attemptContext
	_, err := sdkgo.RunMutation(ctx, newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, 0, provider.requestCount())
}

func TestCreateCardCutsRequestBeforeDexAttemptDeadline(t *testing.T) {
	provider := newRecordingTrello(t, func(_ http.ResponseWriter, request *http.Request, _ int) {
		<-request.Context().Done()
	})
	ctx := newTrelloDexContext("create-deadline")
	attemptContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx.Context = attemptContext
	started := time.Now()
	_, err := sdkgo.RunMutation(ctx, newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Less(t, time.Since(started), 4500*time.Millisecond)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateCardUntrustedCheckpointRetriesWithoutSending(t *testing.T) {
	provider := newRecordingTrello(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	first := newTrelloDexContext("create-replay")
	first.recordedHeartbeat = json.RawMessage(`{"callId":"earlier","attempt":1,"state":"claimed"}`)
	_, err := sdkgo.RunMutation(first.nextAttempt(), newTrelloClient(t, provider.URL).CreateCard(), trelloConnection, validCreateCardInput())
	requireRetry(t, err, sdkgo.FailureAvailability)

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
