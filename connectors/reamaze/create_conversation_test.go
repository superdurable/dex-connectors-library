// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validCreateConversationInput() reamaze.CreateConversationInput {
	onHold := reamaze.ConversationStatusOnHold
	return reamaze.CreateConversationInput{
		Subject: "Double charge on order 88213", Message: "I was charged **twice**.\nOrder 88213.", Channel: "support",
		Requester: reamaze.ConversationRequesterInput{Email: "jane@acme.example.com", Name: "Jane Smith"},
		Status:    &onHold, HoldUntil: "2026-07-15T09:00:00Z", Tags: []string{"billing-double-charge"},
		AssigneeEmail: "agent@acme.example.com", ShouldSuppressNotifications: true,
	}
}

func TestCreateConversationSendsTheDispatchKeyOnceAfterRecordingTheMarker(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusCreated, conversationJSON("double-charge-on-order-88213", 5, []string{"billing-double-charge"}))
	})
	ctx := newReamazeDexContext("create")
	result, err := sdkgo.RunMutation(ctx, newReamazeClient(t, provider.URL).CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	require.Equal(t, reamaze.CreateConversationBranchCreated, result.Branch)
	require.Equal(t, "double-charge-on-order-88213", result.Value.Conversation.ID)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, "double-charge-on-order-88213", result.Receipt.ProviderObjectID)
	require.Equal(t, 1, provider.requestCount())
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/api/v1/conversations", request.path)
	require.Equal(t, "application/json", request.header.Get("Content-Type"))
	require.JSONEq(t, `{"conversation":{"subject":"Double charge on order 88213","category":"support","tag_list":["billing-double-charge"],
		"status":5,"hold_until":"2026-07-15T09:00:00Z","assignee":"agent@acme.example.com",
		"data":{"dex_dispatch_key":"`+dispatchKeyOf(result.Receipt.CallID)+`"},
		"message":{"body":"I was charged **twice**.\nOrder 88213.","suppress_notifications":true},
		"user":{"name":"Jane Smith","email":"jane@acme.example.com"}}}`, request.body)
	require.JSONEq(t, `{"reamazeDispatchedCallId":"`+string(result.Receipt.CallID)+`"}`, string(ctx.recordedHeartbeat),
		"the dispatch marker is recorded before the request")
}

func TestCreateConversationOmitsUnsetFieldsAndSendsOpenStatusZero(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusCreated, conversationJSON("password-reset", 0, []string{}))
	})
	open := reamaze.ConversationStatusOpen
	ctx := newReamazeDexContext("create-minimal")
	result, err := sdkgo.RunMutation(ctx, newReamazeClient(t, provider.URL).CreateConversation(), reamazeConnection, reamaze.CreateConversationInput{
		Subject: "Password reset", Message: "Please reset it.", Channel: "support",
		Requester: reamaze.ConversationRequesterInput{Email: "ben@example.com"}, Status: &open,
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"conversation":{"subject":"Password reset","category":"support","status":0,
		"data":{"dex_dispatch_key":"`+dispatchKeyOf(result.Receipt.CallID)+`"},"message":{"body":"Please reset it."},"user":{"email":"ben@example.com"}}}`,
		provider.request(0).body)
}

func TestCreateConversationReconcilesAnUnconfirmedAttemptWithoutResending(t *testing.T) {
	const slug = "double-charge-on-order-88213"
	for _, test := range []struct {
		name         string
		firstReply   func(http.ResponseWriter)
		isListed     bool
		listedData   func(sdkgo.CallID) map[string]any
		readKey      func(sdkgo.CallID) string
		wantBranch   sdkgo.BranchID
		wantAlready  bool
		wantFailure  sdkgo.FailureKind
		wantRequests int
	}{
		{name: "lost response, list omits data, read confirms", firstReply: func(response http.ResponseWriter) { dropConnection(t, response) },
			isListed: true, readKey: dispatchKeyOf, wantBranch: reamaze.CreateConversationBranchCreated, wantAlready: true, wantRequests: 3},
		{name: "lost response, list echoes data", firstReply: func(response http.ResponseWriter) { dropConnection(t, response) },
			isListed: true, listedData: func(callID sdkgo.CallID) map[string]any {
				return map[string]any{"dex_dispatch_key": dispatchKeyOf(callID)}
			},
			wantBranch: reamaze.CreateConversationBranchCreated, wantAlready: true, wantRequests: 2},
		{name: "server error, nothing created", firstReply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadGateway, `{"error":"SENTINEL"}`)
		}, wantBranch: reamaze.CreateConversationBranchUncertain, wantFailure: sdkgo.FailureTransport, wantRequests: 2},
		{name: "list ignores the data filter", firstReply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusServiceUnavailable, ``)
		}, isListed: true, readKey: func(sdkgo.CallID) string { return "dex-another-step" }, wantBranch: reamaze.CreateConversationBranchUncertain,
			wantFailure: sdkgo.FailureTransport, wantRequests: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			var callID sdkgo.CallID
			provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				switch {
				case request.Method == http.MethodPost:
					test.firstReply(response)
				case request.URL.Path == "/api/v1/conversations":
					conversations := []any{}
					if test.isListed {
						conversation := conversationJSON(slug, 0, nil)
						delete(conversation, "data")
						if test.listedData != nil {
							conversation["data"] = test.listedData(callID)
						}
						conversations = append(conversations, conversation)
					}
					writeValue(t, response, http.StatusOK, pageJSON("conversations", conversations, 1))
				default:
					conversation := conversationJSON(slug, 0, nil)
					conversation["data"] = map[string]any{"dex_dispatch_key": test.readKey(callID)}
					writeValue(t, response, http.StatusOK, conversation)
				}
			})
			client := newReamazeClient(t, provider.URL)
			first := newReamazeDexContext("create-reconcile")
			_, err := sdkgo.RunMutation(first, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
			require.Error(t, err, "an unconfirmed send is retried as a reconciliation")
			var marker map[string]string
			require.NoError(t, json.Unmarshal(first.recordedHeartbeat, &marker))
			callID = sdkgo.CallID(marker["reamazeDispatchedCallId"])

			result, err := sdkgo.RunMutation(first.nextAttempt(), client.CreateConversation(), reamazeConnection, validCreateConversationInput())
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantAlready, result.Value.WasAlreadyApplied)
			if test.wantAlready {
				require.Equal(t, slug, result.Value.Conversation.ID)
			}
			if test.wantFailure != "" {
				require.Equal(t, test.wantFailure, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
			require.Equal(t, test.wantRequests, provider.requestCount())
			reconciliation := provider.request(1)
			require.Equal(t, http.MethodGet, reconciliation.method)
			require.Equal(t, map[string][]string{"filter": {"all"}, "data[dex_dispatch_key]": {dispatchKeyOf(callID)}}, reconciliation.query)
			if test.wantRequests == 3 {
				require.Equal(t, "/api/v1/conversations/"+slug, provider.request(2).path, "the listed candidate is confirmed by a single read")
			}
		})
	}
}

func TestCreateConversationKeepsReconcilingAfterARetriedReadBack(t *testing.T) {
	var callID sdkgo.CallID
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case request.Method == http.MethodPost:
			dropConnection(t, response)
		case index == 1:
			writeJSON(t, response, http.StatusServiceUnavailable, ``)
		default:
			conversation := conversationJSON("double-charge-on-order-88213", 0, nil)
			conversation["data"] = map[string]any{"dex_dispatch_key": dispatchKeyOf(callID)}
			writeValue(t, response, http.StatusOK, pageJSON("conversations", []any{conversation}, 1))
		}
	})
	client := newReamazeClient(t, provider.URL)
	first := newReamazeDexContext("create-read-back-retry")
	_, err := sdkgo.RunMutation(first, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	var marker map[string]string
	require.NoError(t, json.Unmarshal(first.recordedHeartbeat, &marker))
	callID = sdkgo.CallID(marker["reamazeDispatchedCallId"])

	second := first.nextAttempt()
	_, err = sdkgo.RunMutation(second, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err, "a read-back that fails retryably is retried")
	result, err := sdkgo.RunMutation(second.nextAttempt(), client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	require.Equal(t, reamaze.CreateConversationBranchCreated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 3, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(2).method, "the third attempt still finds the marker and never resends")
}

func TestCreateConversationNeverSelectsDefectForCredentialsAfterAnEarlierDispatch(t *testing.T) {
	for _, test := range []struct {
		name        string
		resolveErr  error
		isRetryable bool
	}{
		{name: "credential store outage", resolveErr: errors.New("project object read failed"), isRetryable: true},
		{name: "reauthorization required", resolveErr: sdkgo.ErrReauthorizationRequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodPost {
					writeJSON(t, response, http.StatusBadGateway, `{"error":"SENTINEL"}`)
					return
				}
				writeValue(t, response, http.StatusOK, pageJSON("conversations", []any{}, 0))
			})
			client, credentials := newSwitchingReamazeClient(t, provider.URL)
			first := newReamazeDexContext("create-credentials")
			_, err := sdkgo.RunMutation(first, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
			require.Error(t, err, "a 502 keeps the marker and retries")

			credentials.resolveErr = test.resolveErr
			second := first.nextAttempt()
			result, err := sdkgo.RunMutation(second, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
			require.Equal(t, 1, provider.requestCount(), "nothing is sent without credentials")
			if !test.isRetryable {
				require.NoError(t, err)
				require.Equal(t, reamaze.CreateConversationBranchUncertain, result.Branch, "a sent write is never reported as defect")
				require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
				return
			}
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)

			credentials.resolveErr = nil
			result, err = sdkgo.RunMutation(second.nextAttempt(), client.CreateConversation(), reamazeConnection, validCreateConversationInput())
			require.NoError(t, err)
			require.Equal(t, reamaze.CreateConversationBranchUncertain, result.Branch)
			require.Equal(t, 2, provider.requestCount())
			require.Equal(t, http.MethodGet, provider.request(1).method, "the next attempt reads back instead of resending")
		})
	}
}

func TestCreateConversationResendsOnlyAfterAProvableNonApplication(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			response.Header().Set("Retry-After", "1")
			writeJSON(t, response, http.StatusTooManyRequests, `{"error":"SENTINEL"}`)
			return
		}
		writeValue(t, response, http.StatusCreated, conversationJSON("double-charge-on-order-88213", 0, nil))
	})
	client := newReamazeClient(t, provider.URL)
	first := newReamazeDexContext("create-429")
	_, err := sdkgo.RunMutation(first, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	require.Nil(t, first.recordedHeartbeat, "a 429 clears the marker")
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	require.Equal(t, reamaze.CreateConversationBranchCreated, result.Branch)
	require.Equal(t, http.MethodPost, provider.request(1).method, "the retry sends again")
}

func TestCreateConversationTerminalOutcomes(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusUnprocessableEntity, `{"errors":{"category":["SENTINEL is invalid"]}}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, `{"slug":"SENTINEL/../x"}`)
	})
	client := newReamazeClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newReamazeDexContext("create-422"), client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	require.Equal(t, reamaze.CreateConversationBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	require.NotContains(t, result.Failure.Message, "SENTINEL")

	result, err = sdkgo.RunMutation(newReamazeDexContext("create-malformed"), client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	require.Equal(t, reamaze.CreateConversationBranchUncertain, result.Branch, "a 2xx without a valid conversation cannot be confirmed")
	require.NotContains(t, result.Failure.Message, "SENTINEL")
}

func TestCreateConversationSendsNothingWithoutAMarkerOrValidInput(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusCreated, conversationJSON("x", 0, nil))
	})
	client := newReamazeClient(t, provider.URL)
	rejecting := newReamazeDexContext("create-no-marker")
	rejecting.rejectsHeartbeat = true
	_, err := sdkgo.RunMutation(rejecting, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err, "without a stored marker nothing is sent and the attempt is retried")

	done := reamaze.ConversationStatusDone
	invalidStatus := reamaze.ConversationStatus(10)
	for name, change := range map[string]func(*reamaze.CreateConversationInput){
		"blank subject":        func(input *reamaze.CreateConversationInput) { input.Subject = " " },
		"blank message":        func(input *reamaze.CreateConversationInput) { input.Message = "" },
		"channel path":         func(input *reamaze.CreateConversationInput) { input.Channel = "support/../x" },
		"display requester":    func(input *reamaze.CreateConversationInput) { input.Requester.Email = "Jane <jane@acme.example.com>" },
		"padded name":          func(input *reamaze.CreateConversationInput) { input.Requester.Name = " Jane" },
		"unknown status":       func(input *reamaze.CreateConversationInput) { input.Status = &invalidStatus },
		"hold without on hold": func(input *reamaze.CreateConversationInput) { input.Status = &done },
		"hold not RFC 3339":    func(input *reamaze.CreateConversationInput) { input.HoldUntil = "tomorrow" },
		"display assignee":     func(input *reamaze.CreateConversationInput) { input.AssigneeEmail = "Agent <agent@acme.example.com>" },
		"comma tag":            func(input *reamaze.CreateConversationInput) { input.Tags = []string{"a,b"} },
	} {
		input := validCreateConversationInput()
		change(&input)
		result, err := sdkgo.RunMutation(newReamazeDexContext("create-invalid"), client.CreateConversation(), reamazeConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, reamaze.CreateConversationBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.requestCount())
}
