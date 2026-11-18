// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom_test

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

const replyText = "Hello <b>Jane</b> & team,\nthanks for waiting.\n\nWe refunded order 88213."

// replyHTML is replyText as the connector sends it: escaped paragraphs with line breaks.
const replyHTML = "<p>Hello &lt;b&gt;Jane&lt;/b&gt; &amp; team,<br>thanks for waiting.</p><p>We refunded order 88213.</p>"

// replyPlainText is replyText as Intercom's display_as=plaintext renders it.
const replyPlainText = "Hello <b>Jane</b> & team,\nthanks for waiting.\n\nWe refunded order 88213."

// sendCapOfOne makes a fenced reply read back on every later attempt instead of resending.
var sendCapOfOne = sdkgo.WithMaxSends(1)

func validReplyInput() intercom.ReplyToConversationInput {
	return intercom.ReplyToConversationInput{
		ConversationID: testConversation, AdminID: testAdminID, MessageType: intercom.ReplyMessageTypeComment, Body: replyText,
	}
}

// fenceState returns the state of the single-dispatch checkpoint the last attempt recorded.
func fenceState(t *testing.T, dexContext *testDexContext) string {
	t.Helper()
	var marker struct {
		State string `json:"state"`
	}
	isFound, err := dexContext.GetLastHeartbeatValue(&marker)
	require.NoError(t, err)
	require.True(t, isFound, "the fence records a checkpoint")
	return marker.State
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
	require.Equal(t, "claimed", fenceState(t, dexContext), "the dispatch claim stays recorded")

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

func TestUnconfirmedReplyIsReadBackBeforeAnyResend(t *testing.T) {
	for _, test := range []struct {
		name           string
		part           testPart
		expectedBranch sdkgo.BranchID
	}{
		{name: "matching part", part: testPart{id: "903", partType: "comment", body: replyPlainText, authorType: "admin", authorID: testAdminID},
			expectedBranch: intercom.ReplyToConversationBranchReplied},
		{name: "whitespace rendering differs", part: testPart{id: "903", partType: "comment", body: "Hello <b>Jane</b> &amp; team, thanks for waiting. We refunded order 88213.", authorType: "admin", authorID: testAdminID},
			expectedBranch: intercom.ReplyToConversationBranchReplied},
		{name: "matching part without an ID", part: testPart{partType: "comment", body: replyPlainText, authorType: "admin", authorID: testAdminID},
			expectedBranch: sdkgo.UncertainBranchID},
		{name: "other text", part: testPart{id: "903", partType: "comment", body: "Another reply", authorType: "admin", authorID: testAdminID},
			expectedBranch: sdkgo.UncertainBranchID},
		{name: "other admin", part: testPart{id: "903", partType: "comment", body: replyPlainText, authorType: "admin", authorID: "991267"},
			expectedBranch: sdkgo.UncertainBranchID},
		{name: "note instead of comment", part: testPart{id: "903", partType: "note", body: replyPlainText, authorType: "admin", authorID: testAdminID},
			expectedBranch: sdkgo.UncertainBranchID},
		{name: "older than the first attempt", part: testPart{id: "903", partType: "comment", body: replyPlainText, authorType: "admin", authorID: testAdminID, createdAt: time.Now().Add(-time.Hour).Unix()},
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
				require.Equal(t, http.MethodGet, request.Method, "at the cap a later attempt only reads")
				writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil, test.part))
			})
			client := newIntercomClient(t, provider.URL)
			dexContext := newTestDexContext("reply-reconcile")
			dexContext.firstAttemptAt = time.Now()
			_, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput(), sendCapOfOne)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "a 502 may have applied the reply, so the attempt retries with the claim kept")
			require.Equal(t, "claimed", fenceState(t, dexContext))

			dexContext.startNextAttempt()
			result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput(), sendCapOfOne)
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, 2, provider.requestCount())
			require.Equal(t, "/conversations/"+testConversation+"?display_as=plaintext", provider.request(1).path)
			switch test.expectedBranch {
			case intercom.ReplyToConversationBranchReplied:
				require.True(t, result.Value.WasAlreadyApplied)
				require.Equal(t, "903", result.Value.Part.ID)
			case sdkgo.UncertainBranchID:
				require.Contains(t, result.Failure.Message, "an earlier attempt of this Step may have sent the reply, and the conversation shows ")
			}
		})
	}
}

func TestUnconfirmedReplyIsResentBelowTheCapAfterAnAbsentReadBack(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch index {
		case 0:
			writeJSON(t, response, http.StatusBadGateway, `{"type":"error.list","errors":[{"code":"server_error"}]}`)
		case 1:
			require.Equal(t, http.MethodGet, request.Method, "the read-back comes before the resend")
			writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil))
		default:
			require.Equal(t, http.MethodPost, request.Method)
			writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil,
				testPart{id: "905", partType: "comment", body: replyHTML, authorType: "admin", authorID: testAdminID, createdAt: time.Now().Unix()}))
		}
	})
	client := newIntercomClient(t, provider.URL)
	dexContext := newTestDexContext("reply-resend")
	_, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)

	dexContext.startNextAttempt()
	result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
	require.NoError(t, err)
	require.Equal(t, intercom.ReplyToConversationBranchReplied, result.Branch)
	require.Equal(t, "905", result.Value.Part.ID)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 3, provider.requestCount())
}

func TestUnconfirmedReplySelectsUncertainAtTheSendCap(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil))
			return
		}
		writeJSON(t, response, http.StatusBadGateway, `{"type":"error.list","errors":[{"code":"server_error"}]}`)
	})
	client := newIntercomClient(t, provider.URL)
	dexContext := newTestDexContext("reply-cap")
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "attempt %d sends below the cap of three", attempt)
		dexContext.startNextAttempt()
	}
	result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	posts := 0
	for index := range provider.requestCount() {
		if provider.request(index).method == http.MethodPost {
			posts++
		}
	}
	require.Equal(t, 3, posts, "the reply is sent at most three times")
	require.Equal(t, 6, provider.requestCount(), "every attempt after the first reads back first")
}

func TestAcceptedReplyWithAnUnusableAnswerIsReadBackAndNeverResent(t *testing.T) {
	answers := map[string]func(t *testing.T, response http.ResponseWriter){
		"another conversation": func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"type":"conversation","id":"another"}`)
		},
		"undecodable body": func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"type":"conversation",`)
		},
		"oversized body": func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"type":"conversation","padding":"`+strings.Repeat("a", 20000)+`"}`)
		},
		"part without an ID": func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil,
				testPart{partType: "comment", body: replyHTML, authorType: "admin", authorID: testAdminID, createdAt: time.Now().Unix()}))
		},
		"body breaks after the headers": func(t *testing.T, response http.ResponseWriter) {
			response.Header().Set("Content-Length", "5000")
			response.WriteHeader(http.StatusOK)
			_, err := response.Write([]byte(`{"type":"conversation"`))
			require.NoError(t, err)
			response.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		},
	}
	readBacks := map[string]struct {
		status         int
		body           func(t *testing.T) string
		expectedBranch sdkgo.BranchID
	}{
		"reply found": {status: http.StatusOK, expectedBranch: intercom.ReplyToConversationBranchReplied, body: func(t *testing.T) string {
			return conversationJSON(t, testConversation, "open", nil,
				testPart{id: "906", partType: "comment", body: replyPlainText, authorType: "admin", authorID: testAdminID, createdAt: time.Now().Unix()})
		}},
		"reply absent": {status: http.StatusOK, expectedBranch: sdkgo.UncertainBranchID, body: func(t *testing.T) string {
			return conversationJSON(t, testConversation, "open", nil)
		}},
		"missing conversation": {status: http.StatusNotFound, expectedBranch: sdkgo.UncertainBranchID, body: func(*testing.T) string {
			return `{"type":"error.list","errors":[{"code":"not_found"}]}`
		}},
	}
	for answerName, answer := range answers {
		for readBackName, readBack := range readBacks {
			t.Run(answerName+"/"+readBackName, func(t *testing.T) {
				provider := newRecordingIntercom(t, func(response http.ResponseWriter, request *http.Request, _ int) {
					if request.Method == http.MethodPost {
						answer(t, response)
						return
					}
					writeJSON(t, response, readBack.status, readBack.body(t))
				})
				client, err := intercom.New(intercom.Config{MaxResponseBytes: 10000}, testCredentialProvider(), intercom.WithAPIBaseURL(provider.URL))
				require.NoError(t, err)
				result, err := sdkgo.RunMutation(newTestDexContext("reply-unusable"), client.ReplyToConversation(), intercomConnection, validReplyInput())
				require.NoError(t, err, "a 2xx was accepted, so the attempt never retries and a later attempt cannot resend")
				require.Equal(t, readBack.expectedBranch, result.Branch)
				require.Equal(t, 2, provider.requestCount(), "the reply is read back once and never resent")
				require.Equal(t, http.MethodGet, provider.request(1).method)
				if readBack.expectedBranch == intercom.ReplyToConversationBranchReplied {
					require.Equal(t, "906", result.Value.Part.ID)
					require.False(t, result.Value.WasAlreadyApplied, "this attempt's own send applied the reply")
				}
			})
		}
	}
}

func TestAcceptedReplyWhoseReadBackIsRateLimitedKeepsReadingBackWithoutResending(t *testing.T) {
	for _, test := range []struct {
		name           string
		laterReadBack  string
		expectedBranch sdkgo.BranchID
	}{
		{name: "reply found later", laterReadBack: "found", expectedBranch: intercom.ReplyToConversationBranchReplied},
		{name: "reply absent later", laterReadBack: "absent", expectedBranch: sdkgo.UncertainBranchID},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now()
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, request *http.Request, index int) {
				switch {
				case index == 0:
					require.Equal(t, http.MethodPost, request.Method)
					writeJSON(t, response, http.StatusOK, `{"type":"conversation","id":"another"}`)
				case request.Method != http.MethodGet:
					t.Errorf("the accepted reply was sent again with %s", request.Method)
					writeJSON(t, response, http.StatusInternalServerError, `{}`)
				case index == 1:
					writeJSON(t, response, http.StatusTooManyRequests, `{"type":"error.list","errors":[{"code":"rate_limit_exceeded"}]}`)
				case test.laterReadBack == "found":
					writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil,
						testPart{id: "907", partType: "comment", body: replyPlainText, authorType: "admin", authorID: testAdminID, createdAt: now.Unix()}))
				default:
					writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil))
				}
			})
			client := newIntercomClient(t, provider.URL)
			dexContext := newTestDexContext("reply-accepted-rate-limited")
			dexContext.firstAttemptAt = now
			_, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "the rate-limited read-back of an accepted reply retries")

			dexContext.startNextAttempt()
			result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, 3, provider.requestCount(), "below the cap, the accepted reply is only read back")
			if test.expectedBranch == intercom.ReplyToConversationBranchReplied {
				require.Equal(t, "907", result.Value.Part.ID)
				require.True(t, result.Value.WasAlreadyApplied)
			}
		})
	}
}

func TestReadBackAfterAResendThatWasRateLimitedPrecedesTheNextSend(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch index {
		case 0:
			writeJSON(t, response, http.StatusBadGateway, `{"type":"error.list","errors":[{"code":"server_error"}]}`)
		case 1:
			writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil))
		case 2:
			require.Equal(t, http.MethodPost, request.Method)
			writeJSON(t, response, http.StatusTooManyRequests, `{"type":"error.list","errors":[{"code":"rate_limit_exceeded"}]}`)
		case 3:
			require.Equal(t, http.MethodGet, request.Method, "the first send may have applied, so attempt three reads back first")
			writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil,
				testPart{id: "907", partType: "comment", body: replyPlainText, authorType: "admin", authorID: testAdminID, createdAt: time.Now().Unix()}))
		default:
			t.Fatal("the reply found in the read-back is never resent")
		}
	})
	client := newIntercomClient(t, provider.URL)
	dexContext := newTestDexContext("reply-after-rate-limited-resend")
	for attempt := 1; attempt <= 2; attempt++ {
		_, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
		require.Error(t, err, "attempt %d retries", attempt)
		dexContext.startNextAttempt()
	}
	require.Equal(t, "canResend", fenceState(t, dexContext))
	result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
	require.NoError(t, err)
	require.Equal(t, intercom.ReplyToConversationBranchReplied, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, "907", result.Value.Part.ID)
	require.Equal(t, 4, provider.requestCount())
}

// failingCredentialProvider resolves no credentials and returns err.
type failingCredentialProvider struct{ err error }

func (provider failingCredentialProvider) Resolve(sdkgo.Call) (intercom.Credentials, error) {
	return intercom.Credentials{}, provider.err
}

func TestReplyCredentialFailures(t *testing.T) {
	transientProvider := failingCredentialProvider{err: errors.New("credential store unavailable")}
	terminalProvider := failingCredentialProvider{err: sdkgo.NewReauthorizationRequiredError(errors.New("token revoked"))}
	for _, test := range []struct {
		name                   string
		credentials            sdkgo.CredentialProvider[intercom.Credentials]
		hasPossibleEarlierSend bool
		expectedBranch         sdkgo.BranchID
	}{
		{name: "transient before any send", credentials: transientProvider},
		{name: "terminal before any send", credentials: terminalProvider, expectedBranch: intercom.ReplyToConversationBranchDefect},
		{name: "transient after a possible send", credentials: transientProvider, hasPossibleEarlierSend: true},
		{name: "terminal after a possible send", credentials: terminalProvider, hasPossibleEarlierSend: true, expectedBranch: sdkgo.UncertainBranchID},
		{name: "project reauthorization", credentials: failingCredentialProvider{err: projectconfig.ErrReauthorizationRequired}, expectedBranch: intercom.ReplyToConversationBranchDefect},
		{name: "connection object missing", credentials: failingCredentialProvider{err: projectconfig.ErrObjectNotFound}, expectedBranch: intercom.ReplyToConversationBranchDefect},
		{name: "connection not selected", credentials: failingCredentialProvider{err: projectconfig.ErrConnectionNotSelected}, expectedBranch: intercom.ReplyToConversationBranchDefect},
		{name: "broker rejected after a possible send", credentials: failingCredentialProvider{err: projectconfig.ErrCredentialBrokerRejected}, hasPossibleEarlierSend: true, expectedBranch: sdkgo.UncertainBranchID},
		{name: "broker unavailable", credentials: failingCredentialProvider{err: projectconfig.ErrCredentialBrokerUnavailable}},
		{name: "connection not configured", credentials: sdkgo.StaticCredentialProvider[intercom.Credentials]{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusBadGateway, `{"type":"error.list","errors":[{"code":"server_error"}]}`)
			})
			dexContext := newTestDexContext("reply-credentials-" + test.name)
			if test.hasPossibleEarlierSend {
				_, err := sdkgo.RunMutation(dexContext, newIntercomClient(t, provider.URL).ReplyToConversation(), intercomConnection, validReplyInput())
				require.Error(t, err)
				dexContext.startNextAttempt()
			}
			client, err := intercom.New(intercom.Config{}, test.credentials, intercom.WithAPIBaseURL(provider.URL))
			require.NoError(t, err)
			requestsBefore := provider.requestCount()
			result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
			require.Equal(t, requestsBefore, provider.requestCount(), "nothing is sent without credentials")
			if test.hasPossibleEarlierSend {
				require.Equal(t, "claimed", fenceState(t, dexContext), "notSent would hide the earlier possible send")
			} else {
				require.Equal(t, "notSent", fenceState(t, dexContext))
			}
			if test.expectedBranch == "" {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
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
		{name: "missing conversation", status: http.StatusNotFound, expectedBranch: sdkgo.UncertainBranchID},
		{name: "rejected read", status: http.StatusUnauthorized, expectedBranch: sdkgo.UncertainBranchID},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if index == 0 {
					writeJSON(t, response, http.StatusBadGateway, `{"type":"error.list","errors":[{"code":"server_error"}]}`)
					return
				}
				writeJSON(t, response, test.status, `{"type":"error.list","errors":[{"code":"some_code","message":"SENTINEL"}]}`)
			})
			client := newIntercomClient(t, provider.URL)
			dexContext := newTestDexContext("reply-read-" + test.name)
			_, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput(), sendCapOfOne)
			require.Error(t, err)
			dexContext.startNextAttempt()
			result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput(), sendCapOfOne)
			require.Equal(t, http.MethodGet, provider.request(1).method)
			require.Equal(t, 2, provider.requestCount(), "an attempt at the cap never sends")
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

func TestUnconfirmableReadBackBelowTheCapSelectsUncertainWithoutResending(t *testing.T) {
	for name, readBack := range map[string]struct {
		status int
		body   string
	}{
		"missing conversation": {status: http.StatusNotFound, body: `{"type":"error.list","errors":[{"code":"not_found"}]}`},
		"refused token":        {status: http.StatusUnauthorized, body: `{"type":"error.list","errors":[{"code":"unauthorized"}]}`},
		"invalid conversation": {status: http.StatusOK, body: `{"type":"conversation","id":"another"}`},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if index == 0 {
					writeJSON(t, response, http.StatusBadGateway, `{"type":"error.list","errors":[{"code":"server_error"}]}`)
					return
				}
				writeJSON(t, response, readBack.status, readBack.body)
			})
			client := newIntercomClient(t, provider.URL)
			dexContext := newTestDexContext("reply-unconfirmable-" + name)
			_, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
			require.Error(t, err)
			dexContext.startNextAttempt()
			result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
			require.NoError(t, err)
			require.Equal(t, sdkgo.UncertainBranchID, result.Branch, "only a read-back that shows no matching part permits a resend")
			require.Equal(t, 2, provider.requestCount())
		})
	}
}

func TestRateLimitedOrUnsentReplyRecordsThatItCanBeResent(t *testing.T) {
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
		require.Equal(t, "canResend", fenceState(t, dexContext), "Intercom rejected the reply before applying it")

		dexContext.startNextAttempt()
		result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
		require.NoError(t, err)
		require.Equal(t, intercom.ReplyToConversationBranchReplied, result.Branch)
		require.Equal(t, http.MethodPost, provider.request(1).method, "the retry sends the reply without a read-back")
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
		require.Equal(t, "canResend", fenceState(t, dexContext))
	})
}

func TestLostReplyResponseKeepsTheClaim(t *testing.T) {
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
	require.Equal(t, "claimed", fenceState(t, dexContext), "the request may have been applied")
}

func TestReplyIsNotSentWhenDexDoesNotRecordTheCheckpoint(t *testing.T) {
	provider := newRecordingIntercom(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request may be sent") })
	dexContext := newTestDexContext("reply-no-checkpoint")
	dexContext.recordErr = errors.New("stream closed")
	_, err := sdkgo.RunMutation(dexContext, newIntercomClient(t, provider.URL).ReplyToConversation(), intercomConnection, validReplyInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, "the reply was not sent before the attempt deadline or checkpoint", retry.Failure.Message)
	require.Zero(t, provider.requestCount())
}

func TestUntrustedReplyCheckpointRetriesWithoutSending(t *testing.T) {
	for name, marker := range map[string]any{
		"foreign call":       map[string]any{"callId": "another-call", "attempt": 1, "state": "claimed"},
		"v0.23.0 checkpoint": map[string]any{"intercomReplyDispatchedCallId": "call", "intercomReplyDispatchedAt": time.Now().Unix()},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingIntercom(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request may be sent") })
			dexContext := newTestDexContext("reply-untrusted")
			encoded, err := json.Marshal(marker)
			require.NoError(t, err)
			dexContext.heartbeat = encoded
			dexContext.startNextAttempt()
			_, err = sdkgo.RunMutation(dexContext, newIntercomClient(t, provider.URL).ReplyToConversation(), intercomConnection, validReplyInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "FailClosed never selects a terminal branch")
			require.Zero(t, provider.requestCount())
		})
	}
}

func TestReplyDefiniteRefusalsSelectTheirBranchesOnlyOnAFirstSend(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{name: "missing conversation", status: http.StatusNotFound, body: `{"type":"error.list","errors":[{"code":"not_found"}]}`, branch: intercom.ReplyToConversationBranchNotFound},
		{name: "unknown admin", status: http.StatusUnprocessableEntity, body: `{"type":"error.list","errors":[{"code":"parameter_invalid","field":"admin_id"}]}`, branch: intercom.ReplyToConversationBranchProviderRejected},
		{name: "rejected token", status: http.StatusUnauthorized, body: `{"type":"error.list","errors":[{"code":"unauthorized"}]}`, branch: intercom.ReplyToConversationBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			dexContext := newTestDexContext("reply-" + test.name)
			result, err := sdkgo.RunMutation(dexContext, newIntercomClient(t, provider.URL).ReplyToConversation(), intercomConnection, validReplyInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, 1, provider.requestCount(), "a definite refusal is not resent")
			require.Equal(t, "canResend", fenceState(t, dexContext), "the refusal proves nothing was applied")
		})
		t.Run(test.name+" after a possible earlier send", func(t *testing.T) {
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, request *http.Request, index int) {
				switch {
				case index == 0:
					writeJSON(t, response, http.StatusBadGateway, `{"type":"error.list","errors":[{"code":"server_error"}]}`)
				case request.Method == http.MethodGet:
					writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil))
				default:
					writeJSON(t, response, test.status, test.body)
				}
			})
			client := newIntercomClient(t, provider.URL)
			dexContext := newTestDexContext("reply-after-unknown-" + test.name)
			_, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
			require.Error(t, err)
			dexContext.startNextAttempt()
			result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
			require.NoError(t, err)
			require.Equal(t, sdkgo.UncertainBranchID, result.Branch, "the earlier send may have applied the reply")
		})
	}
}

func TestReplyUnknownStatusesRetry(t *testing.T) {
	for name, status := range map[string]int{
		"conflict": http.StatusConflict, "gone": http.StatusGone, "redirect": http.StatusFound,
		"unlisted": http.StatusTeapot, "timeout": http.StatusRequestTimeout, "server error": http.StatusInternalServerError,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, status, `{"type":"error.list","errors":[{"code":"some_code"}]}`)
			})
			dexContext := newTestDexContext("reply-unknown-" + name)
			_, err := sdkgo.RunMutation(dexContext, newIntercomClient(t, provider.URL).ReplyToConversation(), intercomConnection, validReplyInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, "claimed", fenceState(t, dexContext), "the reply may have been applied")
		})
	}
}

func TestOversizedReplyRefusalIsAnUnknownOutcome(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadRequest, `{"type":"error.list","padding":"`+strings.Repeat("a", 4000)+`"}`)
	})
	client, err := intercom.New(intercom.Config{MaxResponseBytes: 1024}, testCredentialProvider(), intercom.WithAPIBaseURL(provider.URL))
	require.NoError(t, err)
	dexContext := newTestDexContext("reply-oversized-refusal")
	_, err = sdkgo.RunMutation(dexContext, client.ReplyToConversation(), intercomConnection, validReplyInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a refusal whose body cannot be inspected may hide an applied reply")
	require.Equal(t, sdkgo.FailureResponseTooLarge, retry.Failure.Kind)
	require.Equal(t, "claimed", fenceState(t, dexContext))
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
