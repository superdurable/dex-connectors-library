// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testIncidentSubject = "[SEV2] INC-1042: Checkout latency above SLO"
	testIncidentHTML    = "<p>p95 checkout latency is <b>4.2 s</b>.</p><p>Owner: payments on-call</p>"
)

var testCreatedAt = time.Date(2026, time.October, 1, 9, 30, 0, 0, time.UTC)

func validChannelMessageInput() teams.PostChannelMessageInput {
	return teams.PostChannelMessageInput{
		TeamID: testTeamID, ChannelID: testChannelID, Subject: testIncidentSubject,
		Content: testIncidentHTML, ContentType: teams.ContentTypeHTML, Importance: teams.MessageImportanceHigh,
	}
}

func TestPostChannelMessageSendsOneRootMessageAsTheSignedInUser(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, chatMessageJSON(t, "1616990032035", "", testIncidentSubject, "html", testIncidentHTML, testCreatedAt))
	})
	client := newTeamsClient(t, provider.endpoint())
	dexContext := newTestDexContext("post")

	result, err := sdkgo.RunMutation(dexContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, teams.PostChannelMessageBranchSent, result.Branch)
	require.Equal(t, teams.PostMessageOutput{
		MessageID: "1616990032035", TeamID: testTeamID, ChannelID: testChannelID, CreatedAt: testCreatedAt,
		Sender: teams.MessageSender{UserID: testUserID, DisplayName: "Robin Kline"},
		WebURL: "https://teams.microsoft.com/l/message/" + testChannelID + "/1616990032035",
	}, result.Value)
	require.Equal(t, "1616990032035", result.Receipt.ProviderObjectID)
	require.Equal(t, testRequestID, result.Receipt.ProviderRequestID)
	require.Equal(t, 1, provider.requestCount())
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, testChannelPath, request.path)
	require.Equal(t, "Bearer "+testAccessToken, request.authorization)
	require.Equal(t, "application/json", request.contentType)
	require.Equal(t, string(result.Receipt.CallID), request.clientRequestID, "the Call ID correlates Graph's logs")
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(request.body), &body))
	require.Equal(t, map[string]any{
		"subject": testIncidentSubject, "importance": "high",
		"body": map[string]any{"contentType": "html", "content": testIncidentHTML},
	}, body)
	require.True(t, dexContext.hasHeartbeat(), "the dispatch checkpoint was recorded before sending")
}

func TestPostChannelMessageRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, "{}")
	})
	client := newTeamsClient(t, provider.endpoint(), teams.WithHTTPClient(&http.Client{}))
	limited, err := teams.New(teams.Config{Endpoint: provider.endpoint(), MaxMessageBytes: 10}, staticTeamsCredentials())
	require.NoError(t, err)
	for name, input := range map[string]teams.PostChannelMessageInput{
		"team ID":            {TeamID: "Operations", ChannelID: testChannelID, Content: "hello"},
		"channel ID":         {TeamID: testTeamID, ChannelID: "General", Content: "hello"},
		"blank content":      {TeamID: testTeamID, ChannelID: testChannelID, Content: " \n"},
		"invisible HTML":     {TeamID: testTeamID, ChannelID: testChannelID, Content: "<p> </p><br>", ContentType: teams.ContentTypeHTML},
		"content type":       {TeamID: testTeamID, ChannelID: testChannelID, Content: "hello", ContentType: "markdown"},
		"importance":         {TeamID: testTeamID, ChannelID: testChannelID, Content: "hello", Importance: "critical"},
		"multi-line subject": {TeamID: testTeamID, ChannelID: testChannelID, Content: "hello", Subject: "one\ntwo"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunMutation(newTestDexContext("invalid"), client.PostChannelMessage(), teamsConnection, input)
			require.NoError(t, err)
			require.Equal(t, teams.PostChannelMessageBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	result, err := sdkgo.RunMutation(newTestDexContext("too-large"), limited.PostChannelMessage(), teamsConnection,
		teams.PostChannelMessageInput{TeamID: testTeamID, ChannelID: testChannelID, Content: "twelve bytes"})
	require.NoError(t, err)
	require.Equal(t, teams.PostChannelMessageBranchDefect, result.Branch)
	require.Contains(t, result.Failure.Message, "maxMessageBytes")
	require.Zero(t, provider.requestCount())
}

func TestThrottledPostClearsTheCheckpointAndIsSentAfterRetryAfter(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			response.Header().Set("Retry-After", "7")
			writeJSON(t, response, http.StatusTooManyRequests, graphErrorJSON("TooManyRequests"))
			return
		}
		writeJSON(t, response, http.StatusCreated, chatMessageJSON(t, "1616990032035", "", testIncidentSubject, "html", testIncidentHTML, testCreatedAt))
	})
	client := newTeamsClient(t, provider.endpoint())
	dexContext := newTestDexContext("throttled")

	_, err := sdkgo.RunMutation(dexContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	requireRetry(t, err, sdkgo.FailureRateLimit)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 7*time.Second, retryAfter.After)
	require.False(t, dexContext.hasHeartbeat(), "Graph throttles before storing, so the post may be sent again")

	result, err := sdkgo.RunMutation(dexContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, teams.PostChannelMessageBranchSent, result.Branch)
	require.Equal(t, 2, provider.countRequests(http.MethodPost))
}

func TestRejectedPostNamesTheGraphErrorCodeWithoutItsMessage(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusForbidden, graphErrorJSON("Forbidden"))
	})
	client := newTeamsClient(t, provider.endpoint())

	result, err := sdkgo.RunMutation(newTestDexContext("forbidden"), client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, teams.PostChannelMessageBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "HTTP 403 Forbidden")
	require.Contains(t, result.Failure.Message, "must belong to the team")
	require.Equal(t, teams.PostMessageOutput{TeamID: testTeamID, ChannelID: testChannelID}, result.Value)
	requireNoSentinel(t, result)
	require.Equal(t, 1, provider.requestCount())
}

func TestRejectedAccessTokenIsRefreshedAndThePostSentOnce(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Header.Get("Authorization") == "Bearer rejected-token" {
			writeJSON(t, response, http.StatusUnauthorized, graphErrorJSON("InvalidAuthenticationToken"))
			return
		}
		writeJSON(t, response, http.StatusCreated, chatMessageJSON(t, "1616990032035", "", testIncidentSubject, "html", testIncidentHTML, testCreatedAt))
	})
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := teams.New(teams.Config{Endpoint: provider.endpoint()}, credentials)
	require.NoError(t, err)

	result, err := sdkgo.RunMutation(newTestDexContext("refresh"), client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, teams.PostChannelMessageBranchSent, result.Branch)
	require.Equal(t, 1, credentials.forcedRefreshes)
	require.Equal(t, "Bearer replacement-token", provider.request(1).authorization)
	require.Equal(t, 2, provider.requestCount())
}

func TestUnconfirmedPostIsConfirmedByReadingTheChannelBackWithoutAResend(t *testing.T) {
	var mutex sync.Mutex
	channel := collectionJSON("")
	provider := newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			// Teams stored the message, but the response never arrives before the connector gives up.
			mutex.Lock()
			channel = collectionJSON("",
				chatMessageJSON(t, "1616990099000", "", "", "html", "<p>Someone else's note</p>", time.Now()),
				chatMessageJSON(t, "1616990032035", "", testIncidentSubject, "html",
					"<p>p95 checkout latency is <strong>4.2&nbsp;s</strong>.</p>\n<p>Owner:   payments on-call</p>", time.Now()),
				chatMessageJSON(t, "1616980000000", "", testIncidentSubject, "html", testIncidentHTML, time.Now().Add(-time.Hour)),
			)
			mutex.Unlock()
			<-request.Context().Done()
			return
		}
		require.Equal(t, testChannelPath, request.URL.Path)
		require.Equal(t, "%24top=50", request.URL.RawQuery)
		mutex.Lock()
		body := channel
		mutex.Unlock()
		writeJSON(t, response, http.StatusOK, body)
	})
	client := newTeamsClient(t, provider.endpoint(), teams.WithHTTPClient(&http.Client{Timeout: 300 * time.Millisecond}))
	dexContext := newTestDexContext("timeout")

	_, err := sdkgo.RunMutation(dexContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	retry := requireRetry(t, err, sdkgo.FailureTransport)
	require.Contains(t, retry.Failure.Message, "outcome is unknown")
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 5*time.Second, retryAfter.After, "the next attempt waits for the message to appear")
	require.True(t, dexContext.hasHeartbeat())

	result, err := sdkgo.RunMutation(dexContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, teams.PostChannelMessageBranchSent, result.Branch)
	require.True(t, result.Value.IsConfirmedByReadBack)
	require.Equal(t, "1616990032035", result.Value.MessageID)
	require.Equal(t, 1, provider.countRequests(http.MethodPost), "an unconfirmed post is never sent again")
}

func TestUnconfirmedPostWithoutAMatchSelectsUncertain(t *testing.T) {
	for name, readBack := range map[string]func(*testing.T, http.ResponseWriter){
		"no match": func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, collectionJSON("", chatMessageJSON(t, "1616990099000", "", "", "text", "unrelated", time.Now())))
		},
		"two identical messages": func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, collectionJSON("",
				chatMessageJSON(t, "1616990032035", "", testIncidentSubject, "html", testIncidentHTML, time.Now()),
				chatMessageJSON(t, "1616990032036", "", testIncidentSubject, "html", testIncidentHTML, time.Now()),
			))
		},
		"read-back forbidden": func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusForbidden, graphErrorJSON("Forbidden"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodPost {
					writeJSON(t, response, http.StatusServiceUnavailable, graphErrorJSON("ServiceNotAvailable"))
					return
				}
				readBack(t, response)
			})
			client := newTeamsClient(t, provider.endpoint())
			dexContext := newTestDexContext("uncertain-" + strings.ReplaceAll(name, " ", "-"))

			_, err := sdkgo.RunMutation(dexContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
			retry := requireRetry(t, err, sdkgo.FailureAvailability)
			require.Contains(t, retry.Failure.Message, "HTTP 503 ServiceNotAvailable")

			result, err := sdkgo.RunMutation(dexContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
			require.NoError(t, err)
			require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
			require.Contains(t, result.Failure.Message, "not sent again")
			requireNoSentinel(t, result)
			require.Equal(t, 1, provider.countRequests(http.MethodPost))
		})
	}
}

func TestUnusableAcceptedResponseIsReconciledInsteadOfResent(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusCreated, `{"id":"not-a-message-id"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, collectionJSON("", chatMessageJSON(t, "1616990032035", "", testIncidentSubject, "html", testIncidentHTML, time.Now())))
	})
	client := newTeamsClient(t, provider.endpoint())
	dexContext := newTestDexContext("unusable")

	_, err := sdkgo.RunMutation(dexContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	requireRetry(t, err, sdkgo.FailureProtocol)
	result, err := sdkgo.RunMutation(dexContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, teams.PostChannelMessageBranchSent, result.Branch)
	require.True(t, result.Value.IsConfirmedByReadBack)
	require.Equal(t, 1, provider.countRequests(http.MethodPost))
}

func TestPostIsNotSentWhenTheCheckpointCannotBeRecorded(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, "{}")
	})
	client := newTeamsClient(t, provider.endpoint())
	dexContext := newTestDexContext("checkpoint-failure")
	dexContext.recordErr = errors.New("dex unavailable")

	_, err := sdkgo.RunMutation(dexContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Zero(t, provider.requestCount())
}

func TestPostResponseReflectingTheAccessTokenIsNotTrusted(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusCreated, chatMessageJSON(t, "1616990032035", "", testIncidentSubject, "text", testAccessToken, testCreatedAt))
			return
		}
		writeJSON(t, response, http.StatusOK, collectionJSON(""))
	})
	client := newTeamsClient(t, provider.endpoint())
	dexContext := newTestDexContext("reflected")

	_, err := sdkgo.RunMutation(dexContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	retry := requireRetry(t, err, sdkgo.FailureProtocol)
	require.Contains(t, retry.Failure.Message, "credential material")
	result, err := sdkgo.RunMutation(dexContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	requireNoSentinel(t, result)
}
