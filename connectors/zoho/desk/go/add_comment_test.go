// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex/sdk-go/dex"
)

func commentJSON(id string, isPublic bool, content string) map[string]any {
	return map[string]any{
		"id": id, "isPublic": isPublic, "contentType": "plainText", "content": content, "commenterId": testAgentID,
		"commentedTime": "2026-01-28T09:01:00.000Z", "modifiedTime": nil, "attachments": []any{},
		"commenter": map[string]any{"name": "Dex Integration", "type": "AGENT", "email": "SENTINEL@example.com"},
	}
}

func TestAddCommentSendsPlainTextWithTheRequestedVisibilityOnce(t *testing.T) {
	for _, isPublic := range []bool{false, true} {
		provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
			var payload struct {
				Content  string `json:"content"`
				IsPublic bool   `json:"isPublic"`
			}
			require.NoError(t, json.Unmarshal(mustReadBody(t, request), &payload))
			writeValue(t, response, http.StatusOK, commentJSON("4000000529001", payload.IsPublic, payload.Content))
		})
		ctx := newDeskDexContext("comment")
		result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).AddComment(), deskConnection, desk.AddCommentInput{
			TicketID: testTicketID, Content: "Triaged: <b>refund</b> pending.\nPriority High.", IsPublic: isPublic,
		})
		require.NoError(t, err)
		require.Equal(t, desk.AddCommentBranchAdded, result.Branch)
		require.Equal(t, isPublic, result.Value.Comment.IsPublic)
		require.Equal(t, "Triaged: <b>refund</b> pending.\nPriority High.", result.Value.Comment.Content)
		require.Equal(t, "4000000529001", result.Receipt.ProviderObjectID)
		request := provider.request(0)
		require.Equal(t, "/api/v1/tickets/"+testTicketID+"/comments", request.path)
		expectedVisibility := "false"
		if isPublic {
			expectedVisibility = "true"
		}
		require.JSONEq(t, `{"content":"Triaged: <b>refund</b> pending.\nPriority High.","isPublic":`+expectedVisibility+`,"contentType":"plainText"}`, request.body,
			"a private comment states isPublic false instead of relying on Zoho Desk's default")
		require.NotEmpty(t, ctx.recordedHeartbeat)
		requireNoSentinel(t, result)
	}
}

func TestAddCommentResendsUnknownOutcomesAndNeverResendsRefusals(t *testing.T) {
	for _, test := range []struct {
		name    string
		reply   func(http.ResponseWriter)
		branch  sdkgo.BranchID
		isRetry bool
		state   singledispatch.State
	}{
		{name: "lost response", reply: func(response http.ResponseWriter) { dropConnection(t, response) }, isRetry: true, state: singledispatch.StateClaimed},
		{name: "server error", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusInternalServerError, `{"errorCode":"INTERNAL_SERVER_ERROR"}`)
		}, isRetry: true, state: singledispatch.StateClaimed},
		{name: "visibility changed", reply: func(response http.ResponseWriter) {
			writeValue(t, response, http.StatusOK, commentJSON("4000000529001", true, "Triaged."))
		}, branch: desk.AddCommentBranchUncertain, state: singledispatch.StateClaimed},
		{name: "missing ticket", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusNotFound, `{"errorCode":"URL_NOT_FOUND","message":"SENTINEL"}`)
		}, branch: desk.AddCommentBranchNotFound, state: singledispatch.StateCanResend},
		{name: "too large", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusRequestEntityTooLarge, `{"errorCode":"RESOURCE_SIZE_EXCEEDED"}`)
		}, branch: desk.AddCommentBranchProviderRejected, state: singledispatch.StateCanResend},
		{name: "rate limited", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusTooManyRequests, `{"errorCode":"TOO_MANY_REQUESTS"}`)
		}, isRetry: true, state: singledispatch.StateCanResend},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
			ctx := newDeskDexContext("comment-" + test.name)
			result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).AddComment(), deskConnection,
				desk.AddCommentInput{TicketID: testTicketID, Content: "Triaged."})
			require.Equal(t, 1, provider.requestCount())
			require.Equal(t, test.state, recordedMarker(t, ctx).State)
			if test.isRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			requireNoSentinel(t, result)
		})
	}
}

func TestAddCommentWithAReturnedIDIsNeverResent(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, commentJSON("4000000529001", true, "Triaged."))
	})
	client := newDeskClient(t, provider.URL)
	ctx := newDeskDexContext("comment-visibility")
	input := desk.AddCommentInput{TicketID: testTicketID, Content: "Triaged."}
	result, err := sdkgo.RunMutation(ctx, client.AddComment(), deskConnection, input)
	require.NoError(t, err)
	require.Equal(t, desk.AddCommentBranchUncertain, result.Branch, "the comment exists with another visibility")
	require.Equal(t, "4000000529001", recordedMarker(t, ctx).ProviderID)
	result, err = sdkgo.RunMutation(ctx.nextAttempt(), client.AddComment(), deskConnection, input)
	require.NoError(t, err)
	require.Equal(t, desk.AddCommentBranchUncertain, result.Branch, "the read-back finds the other visibility")
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method, "a saved comment ID is read back, never resent")
	require.Equal(t, "/api/v1/tickets/"+testTicketID+"/comments/4000000529001", provider.request(1).path)
}

// TestAddCommentReadsBackAnAcceptedCommentByID covers a 2xx whose comment does not decode but carries
// its ID, and a later attempt that finds the saved ID: both read back and never resend.
func TestAddCommentReadsBackAnAcceptedCommentByID(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case request.Method == http.MethodPost:
			writeJSON(t, response, http.StatusCreated, `{"id":"4000000529001","content":"SENTINEL"}`)
		case index == 1:
			writeJSON(t, response, http.StatusServiceUnavailable, `{"errorCode":"SERVICE_UNAVAILABLE"}`)
		default:
			writeValue(t, response, http.StatusOK, commentJSON("4000000529001", false, "Triaged."))
		}
	})
	client := newDeskClient(t, provider.URL)
	input := desk.AddCommentInput{TicketID: testTicketID, Content: "Triaged."}
	ctx := newDeskDexContext("comment-read-back")
	_, err := sdkgo.RunMutation(ctx, client.AddComment(), deskConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, "4000000529001", recordedMarker(t, ctx).ProviderID)

	result, err := sdkgo.RunMutation(ctx.nextAttempt(), client.AddComment(), deskConnection, input)
	require.NoError(t, err)
	require.Equal(t, desk.AddCommentBranchAdded, result.Branch)
	require.Equal(t, "4000000529001", result.Value.Comment.ID)
	require.Equal(t, 3, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(2).method)
}

// TestAddCommentReadsBackASavedIDAtTheSendCap covers finding 39 for comments.
func TestAddCommentReadsBackASavedIDAtTheSendCap(t *testing.T) {
	input := desk.AddCommentInput{TicketID: testTicketID, Content: "Triaged."}
	ctx := capClaimWithSavedProviderID(t, "comment-saved-id", func(client *desk.Client, ctx *deskDexContext) {
		_, err := sdkgo.RunMutation(ctx, client.AddComment(), deskConnection, input)
		require.NoError(t, err)
	}, mustJSON(t, commentJSON("4000000529001", false, "Triaged.")))
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, commentJSON("4000000529001", false, "Triaged."))
	})
	result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).AddComment(), deskConnection, input)
	require.NoError(t, err)
	require.Equal(t, desk.AddCommentBranchAdded, result.Branch)
	require.Equal(t, 1, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(0).method)
}

// TestAddCommentNeverResendsA2xxWithoutAnID covers an accepted comment whose answer has no ID.
func TestAddCommentNeverResendsA2xxWithoutAnID(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	ctx := newDeskDexContext("comment-2xx-no-id")
	result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).AddComment(), deskConnection,
		desk.AddCommentInput{TicketID: testTicketID, Content: "Triaged."})
	require.NoError(t, err)
	require.Equal(t, desk.AddCommentBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestAddCommentResendsBelowTheCapAndSelectsUncertainAtIt(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) { dropConnection(t, response) })
	client := newDeskClient(t, provider.URL)
	input := desk.AddCommentInput{TicketID: testTicketID, Content: "Triaged."}
	ctx := newDeskDexContext("comment-cap")
	for attempt := 1; attempt < 3; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.AddComment(), deskConnection, input)
		requireRetry(t, err, sdkgo.FailureTransport)
		ctx = ctx.nextAttempt()
	}
	result, err := sdkgo.RunMutation(ctx, client.AddComment(), deskConnection, input)
	require.NoError(t, err)
	require.Equal(t, desk.AddCommentBranchUncertain, result.Branch)
	require.Equal(t, 3, provider.requestCount(), "the send cap of three bounds the possible comments")
}

func TestAddCommentRetriesAfter503AndRecordsCanResend(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, attempt int) {
		if attempt == 0 {
			response.Header().Set("Retry-After", "2")
			writeJSON(t, response, http.StatusServiceUnavailable, `{"errorCode":"SERVICE_UNAVAILABLE"}`)
			return
		}
		writeValue(t, response, http.StatusOK, commentJSON("4000000529001", false, "Triaged."))
	})
	ctx := newDeskDexContext("comment-service-unavailable")
	client := newDeskClient(t, provider.URL)
	input := desk.AddCommentInput{TicketID: testTicketID, Content: "Triaged."}
	_, err := sdkgo.RunMutation(ctx, client.AddComment(), deskConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 2*time.Second, retryAfter.After)
	require.Equal(t, singledispatch.StateCanResend, recordedMarker(t, ctx).State, "a 503 records canResend before retry")
	require.Equal(t, 1, provider.requestCount())

	result, err := sdkgo.RunMutation(ctx.nextAttempt(), client.AddComment(), deskConnection, input)
	require.NoError(t, err)
	require.Equal(t, desk.AddCommentBranchAdded, result.Branch)
	require.Equal(t, 2, provider.requestCount())
}

func TestAddCommentRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingDesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, input := range map[string]desk.AddCommentInput{
		"blank content":  {TicketID: testTicketID, Content: " \n"},
		"long content":   {TicketID: testTicketID, Content: strings.Repeat("c", desk.MaxCommentRunes+1)},
		"missing ticket": {Content: "Triaged."},
		"ticket number":  {TicketID: "101a", Content: "Triaged."},
	} {
		ctx := newDeskDexContext("comment-defect")
		result, err := sdkgo.RunMutation(ctx, newDeskClient(t, provider.URL).AddComment(), deskConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, desk.AddCommentBranchDefect, result.Branch, name)
		require.Zero(t, ctx.heartbeatCount, name)
	}
}
