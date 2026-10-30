// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom_test

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const replyText = "Hello <b>Jane</b> & team,\nthanks for waiting.\n\nWe refunded order 88213."

// replyHTML is replyText as the connector sends it: escaped paragraphs with line breaks.
const replyHTML = "<p>Hello &lt;b&gt;Jane&lt;/b&gt; &amp; team,<br>thanks for waiting.</p><p>We refunded order 88213.</p>"

// replyPlainText is replyText as Intercom's display_as=plaintext renders it.
const replyPlainText = "Hello <b>Jane</b> & team,\nthanks for waiting.\n\nWe refunded order 88213."

func validReplyInput() intercom.ReplyToConversationInput {
	return intercom.ReplyToConversationInput{
		ConversationID: testConversation, AdminID: testAdminID, MessageType: intercom.ReplyMessageTypeComment, Body: replyText,
	}
}

func TestReplySendsEscapedHTMLAsTheAdminAfterRecordingACheckpoint(t *testing.T) {
	now := time.Now()
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil,
			testPart{id: "900", partType: "comment", body: replyHTML, authorType: "admin", authorID: "991267", createdAt: now.Unix()},
			testPart{id: "901", partType: "comment", body: replyHTML, authorType: "admin", authorID: testAdminID, createdAt: now.Unix()},
		))
	})
	dexContext := newTestDexContext("reply")
	result, err := sdkgo.RunMutation(dexContext, newIntercomClient(t, provider.URL).ReplyToConversation(), intercomConnection, validReplyInput())
	require.NoError(t, err)
	require.Equal(t, intercom.ReplyToConversationBranchReplied, result.Branch)
	require.Equal(t, "901", result.Value.Part.ID, "the part by this admin with this text")
	require.Equal(t, intercom.ConversationStateOpen, result.Value.ConversationState)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, "901", result.Receipt.Metadata["partId"])
	require.True(t, dexContext.hasHeartbeat(), "the dispatch checkpoint stays recorded")

	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/conversations/"+testConversation+"/reply", request.path)
	require.JSONEq(t, `{"message_type":"comment","type":"admin","admin_id":"`+testAdminID+`","body":"`+strings.ReplaceAll(replyHTML, `"`, `\"`)+`"}`, request.body)
}

func TestReplyNoteIdentifiesItsPartWhenIntercomChangesTheText(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil,
			testPart{id: "902", partType: "note", body: "<p>Rewritten by Intercom</p>", authorType: "admin", authorID: testAdminID, createdAt: time.Now().Unix()},
		))
	})
	input := validReplyInput()
	input.MessageType = intercom.ReplyMessageTypeNote
	result, err := sdkgo.RunMutation(newTestDexContext("reply-note"), newIntercomClient(t, provider.URL).ReplyToConversation(), intercomConnection, input)
	require.NoError(t, err)
	require.Equal(t, intercom.ReplyToConversationBranchReplied, result.Branch)
	require.Equal(t, "902", result.Value.Part.ID)
	require.Contains(t, provider.request(0).body, `"message_type":"note"`)
}

func TestUnconfirmedReplyIsReconciledFromTheConversationAndNeverResent(t *testing.T) {
	for _, test := range []struct {
		name           string
		part           testPart
		expectedBranch sdkgo.BranchID
	}{
		{name: "matching part", part: testPart{id: "903", partType: "comment", body: replyPlainText, authorType: "admin", authorID: testAdminID},
			expectedBranch: intercom.ReplyToConversationBranchReplied},
		{name: "whitespace rendering differs", part: testPart{id: "903", partType: "comment", body: "Hello <b>Jane</b> &amp; team, thanks for waiting. We refunded order 88213.", authorType: "admin", authorID: testAdminID},
			expectedBranch: intercom.ReplyToConversationBranchReplied},
		{name: "other text", part: testPart{id: "903", partType: "comment", body: "Another reply", authorType: "admin", authorID: testAdminID},
			expectedBranch: sdkgo.UncertainBranchID},
		{name: "other admin", part: testPart{id: "903", partType: "comment", body: replyPlainText, authorType: "admin", authorID: "991267"},
			expectedBranch: sdkgo.UncertainBranchID},
		{name: "note instead of comment", part: testPart{id: "903", partType: "note", body: replyPlainText, authorType: "admin", authorID: testAdminID},
			expectedBranch: sdkgo.UncertainBranchID},
		{name: "older than the dispatch", part: testPart{id: "903", partType: "comment", body: replyPlainText, authorType: "admin", authorID: testAdminID, createdAt: time.Now().Add(-time.Hour).Unix()},
			expectedBranch: sdkgo.UncertainBranchID},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.part.createdAt == 0 {
				test.part.createdAt = time.Now().Unix()
			}
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, request *http.Request, index int) {
				if index == 0 {
					writeJSON(t, response, http.StatusBadGateway, `{"type":"error.list","errors":[{"code":"server_error"}]}`)
					return
				}
				require.Equal(t, http.MethodGet, request.Method, "a later attempt only reads")
				writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil, test.part))
			})
			client := newIntercomClient(t, provider.URL)
			dexContext := newTestDexContext("reply-reconcile")
			_, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "a 502 may have applied the reply, so the attempt retries with the checkpoint kept")
			require.True(t, dexContext.hasHeartbeat())

			result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, 2, provider.requestCount())
			require.Equal(t, "/conversations/"+testConversation+"?display_as=plaintext", provider.request(1).path)
			if test.expectedBranch == intercom.ReplyToConversationBranchReplied {
				require.True(t, result.Value.WasAlreadyApplied)
				require.Equal(t, "903", result.Value.Part.ID)
				return
			}
			require.Equal(t, "an earlier attempt of this Step sent the reply without a confirmed outcome, and the conversation shows no matching part, so it is not sent again",
				result.Failure.Message)
		})
	}
}

func TestReplyReconciliationReadFailuresAreRetriedOrUncertain(t *testing.T) {
	for _, test := range []struct {
		name           string
		status         int
		expectedBranch sdkgo.BranchID
	}{
		{name: "rate limited read", status: http.StatusTooManyRequests},
		{name: "missing conversation", status: http.StatusNotFound, expectedBranch: intercom.ReplyToConversationBranchNotFound},
		{name: "rejected read", status: http.StatusUnauthorized, expectedBranch: sdkgo.UncertainBranchID},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, `{"type":"error.list","errors":[{"code":"some_code","message":"SENTINEL"}]}`)
			})
			dexContext := newTestDexContext("reply-read-" + test.name)
			require.NoError(t, dexContext.RecordHeartbeat(map[string]any{"intercomReplyDispatchedAt": time.Now().Unix()}))
			result, err := sdkgo.RunMutation(dexContext, newIntercomClient(t, provider.URL).ReplyToConversation(), intercomConnection, validReplyInput())
			require.Equal(t, http.MethodGet, provider.request(0).method)
			require.Equal(t, 1, provider.requestCount(), "an attempt that finds the checkpoint never sends")
			if test.expectedBranch == "" {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
		})
	}
}

func TestRateLimitedOrUnsentReplyClearsTheCheckpointForAResend(t *testing.T) {
	t.Run("rate limited", func(t *testing.T) {
		provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, index int) {
			if index == 0 {
				response.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(5*time.Second).Unix(), 10))
				writeJSON(t, response, http.StatusTooManyRequests, `{"type":"error.list","errors":[{"code":"rate_limit_exceeded"}]}`)
				return
			}
			writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil,
				testPart{id: "904", partType: "comment", body: replyHTML, authorType: "admin", authorID: testAdminID, createdAt: time.Now().Unix()}))
		})
		client := newIntercomClient(t, provider.URL)
		dexContext := newTestDexContext("reply-rate-limited")
		_, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
		var retryAfter *dex.RetryAfterError
		require.ErrorAs(t, err, &retryAfter)
		require.InDelta(t, 5*time.Second, retryAfter.After, float64(2*time.Second))
		require.False(t, dexContext.hasHeartbeat(), "Intercom rejected the reply before applying it")

		result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
		require.NoError(t, err)
		require.Equal(t, intercom.ReplyToConversationBranchReplied, result.Branch)
		require.Equal(t, http.MethodPost, provider.request(1).method, "the retry sends the reply")
	})
	t.Run("connection refused", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		closedAddress := listener.Addr().String()
		require.NoError(t, listener.Close())
		dexContext := newTestDexContext("reply-refused")
		_, err = sdkgo.RunMutation(dexContext, newIntercomClient(t, "http://"+closedAddress).ReplyToConversation(), intercomConnection, validReplyInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.Equal(t, "Intercom could not be reached; no request was sent", retry.Failure.Message)
		require.False(t, dexContext.hasHeartbeat())
	})
}

func TestLostReplyResponseKeepsTheCheckpoint(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		hijacker, isHijacker := response.(http.Hijacker)
		require.True(t, isHijacker)
		connection, _, err := hijacker.Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	dexContext := newTestDexContext("reply-lost")
	_, err := sdkgo.RunMutation(dexContext, newIntercomClient(t, provider.URL).ReplyToConversation(), intercomConnection, validReplyInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.True(t, dexContext.hasHeartbeat(), "the request may have been applied")
}

func TestReplyIsNotSentWhenDexDoesNotRecordTheCheckpoint(t *testing.T) {
	provider := newRecordingIntercom(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request may be sent") })
	dexContext := newTestDexContext("reply-no-checkpoint")
	dexContext.recordErr = errors.New("stream closed")
	_, err := sdkgo.RunMutation(dexContext, newIntercomClient(t, provider.URL).ReplyToConversation(), intercomConnection, validReplyInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, "Dex did not record the dispatch checkpoint; nothing was sent", retry.Failure.Message)
	require.Zero(t, provider.requestCount())
}

func TestReplyRejectionsAndInvalidResponsesSelectTheirBranches(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{name: "missing conversation", status: http.StatusNotFound, body: `{"type":"error.list","errors":[{"code":"not_found"}]}`, branch: intercom.ReplyToConversationBranchNotFound},
		{name: "unknown admin", status: http.StatusUnprocessableEntity, body: `{"type":"error.list","errors":[{"code":"parameter_invalid","field":"admin_id"}]}`, branch: intercom.ReplyToConversationBranchProviderRejected},
		{name: "invalid accepted response", status: http.StatusOK, body: `{"type":"conversation","id":"another"}`, branch: intercom.ReplyToConversationBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunMutation(newTestDexContext("reply-"+test.name), newIntercomClient(t, provider.URL).ReplyToConversation(), intercomConnection, validReplyInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, 1, provider.requestCount(), "a conclusive outcome is not retried")
		})
	}
}

func TestReplyValidatesInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingIntercom(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request may be sent") })
	client := newIntercomClient(t, provider.URL)
	for name, mutate := range map[string]func(*intercom.ReplyToConversationInput){
		"no message type": func(input *intercom.ReplyToConversationInput) { input.MessageType = "" },
		"quick reply":     func(input *intercom.ReplyToConversationInput) { input.MessageType = "quick_reply" },
		"blank body":      func(input *intercom.ReplyToConversationInput) { input.Body = " \n " },
		"oversized body": func(input *intercom.ReplyToConversationInput) {
			input.Body = strings.Repeat("a", intercom.MaxTextBytes+1)
		},
		"invalid UTF-8":         func(input *intercom.ReplyToConversationInput) { input.Body = "\xff" },
		"admin email":           func(input *intercom.ReplyToConversationInput) { input.AdminID = "ada@acme.example.com" },
		"last instead of an ID": func(input *intercom.ReplyToConversationInput) { input.ConversationID = "last" },
	} {
		input := validReplyInput()
		mutate(&input)
		dexContext := newTestDexContext("reply-invalid-" + name)
		result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, input)
		require.NoError(t, err)
		require.Equal(t, intercom.ReplyToConversationBranchDefect, result.Branch, name)
		require.False(t, dexContext.hasHeartbeat(), name)
	}
}
