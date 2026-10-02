// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validListThreadRepliesInput() teams.ListThreadRepliesInput {
	return teams.ListThreadRepliesInput{TeamID: testTeamID, ChannelID: testChannelID, MessageID: testRootID}
}

func TestListThreadRepliesReadsOnePageAsText(t *testing.T) {
	var provider *recordingGraph
	provider = newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index > 0 {
			require.Equal(t, "$skiptoken=MSwwLDE2NDQ0MzkzODAxNDU", request.URL.RawQuery, "the next-page link is followed verbatim")
			writeJSON(t, response, http.StatusOK, collectionJSON(""))
			return
		}
		deleted := strings.Replace(chatMessageJSON(t, "1616989750004", testRootID, "", "html", "<p>secret</p>", testCreatedAt),
			`"deletedDateTime":null`, `"deletedDateTime":"2026-10-01T09:31:00Z"`, 1)
		bot := strings.Replace(chatMessageJSON(t, "1616989750005", testRootID, "", "text", "Deploy finished", testCreatedAt),
			`"user":{`, `"user":null,"bot":{`, 1)
		bot = strings.Replace(bot, `"application":null`, `"application":{"id":"2b5e3f0a-1d7c-4e0b-9f6a-0c1d2e3f4a5b","displayName":"Deploy Bot"}`, 1)
		writeJSON(t, response, http.StatusOK, collectionJSON(provider.endpoint()+"/teams/"+testTeamID+"/channels/"+testChannelID+"/messages/"+testRootID+"/replies?$skiptoken=MSwwLDE2NDQ0MzkzODAxNDU",
			chatMessageJSON(t, "1616989753153", testRootID, "", "html",
				`<p><at id="0">Ada Lovelace</at>&nbsp;ack <emoji id="like" alt="👍" title="Like"></emoji></p><p>Looking now.<br>ETA 10&nbsp;min</p><script>ignored()</script>`, testCreatedAt),
			deleted, bot,
		))
	})
	client := newTeamsClient(t, provider.endpoint())
	input := validListThreadRepliesInput()
	input.PageSize = 3

	result, err := sdkgo.RunQuery(newTestDexContext("list"), client.ListThreadReplies(), teamsConnection, input)
	require.NoError(t, err)
	require.Equal(t, teams.ListThreadRepliesBranchRead, result.Branch)
	require.Equal(t, testRepliesPath, provider.request(0).path)
	require.Equal(t, "%24top=3", provider.request(0).rawQuery)
	require.Len(t, result.Value.Replies, 3)
	first := result.Value.Replies[0]
	require.Equal(t, teams.Message{
		ID: "1616989753153", ReplyToID: testRootID, MessageType: "message", CreatedAt: testCreatedAt,
		Sender: teams.MessageSender{UserID: testUserID, DisplayName: "Robin Kline"}, Importance: teams.MessageImportanceNormal,
		ContentType: teams.ContentTypeHTML, Text: "Ada Lovelace ack 👍\nLooking now.\nETA 10 min",
		WebURL: "https://teams.microsoft.com/l/message/" + testChannelID + "/1616989753153",
	}, first)
	require.True(t, result.Value.Replies[1].IsDeleted)
	require.Empty(t, result.Value.Replies[1].Text, "a deleted reply's body is not returned")
	require.Equal(t, teams.MessageSender{ApplicationID: "2b5e3f0a-1d7c-4e0b-9f6a-0c1d2e3f4a5b", DisplayName: "Deploy Bot"}, result.Value.Replies[2].Sender)
	require.NotEmpty(t, result.Value.NextCursor)

	input.Cursor = result.Value.NextCursor
	next, err := sdkgo.RunQuery(newTestDexContext("list-next"), client.ListThreadReplies(), teamsConnection, input)
	require.NoError(t, err)
	require.Equal(t, teams.ListThreadRepliesBranchRead, next.Branch)
	require.Empty(t, next.Value.Replies)
	require.Empty(t, next.Value.NextCursor)
	require.Equal(t, testRepliesPath, provider.request(1).path)
}

func TestListThreadRepliesTruncatesLongText(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, collectionJSON("", chatMessageJSON(t, "1616989753153", testRootID, "", "text", "acknowledged by the database team", testCreatedAt)))
	})
	client := newTeamsClient(t, provider.endpoint())
	input := validListThreadRepliesInput()
	input.MaxTextCharacters = 12

	result, err := sdkgo.RunQuery(newTestDexContext("truncate"), client.ListThreadReplies(), teamsConnection, input)
	require.NoError(t, err)
	require.Equal(t, "acknowledged", result.Value.Replies[0].Text)
	require.True(t, result.Value.Replies[0].IsTextTruncated)
}

func TestListThreadRepliesRefusesLinksOffTheThread(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, collectionJSON("https://attacker.example/v1.0/teams/"+testTeamID+"/channels/"+testChannelID+"/messages/"+testRootID+"/replies?$skiptoken=x"))
	})
	client := newTeamsClient(t, provider.endpoint())

	result, err := sdkgo.RunQuery(newTestDexContext("next-link"), client.ListThreadReplies(), teamsConnection, validListThreadRepliesInput())
	require.NoError(t, err)
	require.Equal(t, teams.ListThreadRepliesBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)

	for name, cursor := range map[string]string{
		"another host":   "https://attacker.example/v1.0/teams/" + testTeamID + "/channels/" + testChannelID + "/messages/" + testRootID + "/replies?$skiptoken=x",
		"another thread": provider.endpoint() + "/teams/" + testTeamID + "/channels/" + testChannelID + "/messages/1616000000000/replies?$skiptoken=x",
		"user info":      strings.Replace(provider.endpoint(), "http://", "http://user:pass@", 1) + "/teams/" + testTeamID + "/channels/" + testChannelID + "/messages/" + testRootID + "/replies",
		"relative":       "/teams/" + testTeamID + "/channels/" + testChannelID + "/messages/" + testRootID + "/replies",
	} {
		t.Run(name, func(t *testing.T) {
			input := validListThreadRepliesInput()
			input.Cursor = cursor
			result, err := sdkgo.RunQuery(newTestDexContext("cursor"), client.ListThreadReplies(), teamsConnection, input)
			require.NoError(t, err)
			require.Equal(t, teams.ListThreadRepliesBranchDefect, result.Branch)
		})
	}
	require.Equal(t, 1, provider.requestCount(), "a foreign cursor never reaches the network")
}

func TestListThreadRepliesMapsGraphFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		code       string
		branch     sdkgo.BranchID
		kind       sdkgo.FailureKind
		isRetried  bool
		retryAfter string
	}{
		{name: "not found", status: http.StatusNotFound, code: "NotFound", branch: teams.ListThreadRepliesBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "forbidden", status: http.StatusForbidden, code: "Forbidden", branch: teams.ListThreadRepliesBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "bad request", status: http.StatusBadRequest, code: "BadRequest", branch: teams.ListThreadRepliesBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "throttled", status: http.StatusTooManyRequests, code: "TooManyRequests", kind: sdkgo.FailureRateLimit, isRetried: true, retryAfter: "3"},
		{name: "unavailable", status: http.StatusServiceUnavailable, code: "ServiceNotAvailable", kind: sdkgo.FailureAvailability, isRetried: true},
		{name: "gateway timeout", status: http.StatusGatewayTimeout, code: "UnknownError", kind: sdkgo.FailureAvailability, isRetried: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.retryAfter != "" {
					response.Header().Set("Retry-After", test.retryAfter)
				}
				writeJSON(t, response, test.status, graphErrorJSON(test.code))
			})
			client := newTeamsClient(t, provider.endpoint())
			result, err := sdkgo.RunQuery(newTestDexContext("failure"), client.ListThreadReplies(), teamsConnection, validListThreadRepliesInput())
			if test.isRetried {
				requireRetry(t, err, test.kind)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, test.code)
			requireNoSentinel(t, result)
		})
	}
}

func TestListThreadRepliesRejectsOversizedAndInvalidPages(t *testing.T) {
	for name, body := range map[string]string{
		"oversized":    `{"value":[` + strings.Repeat(" ", 2048) + `]}`,
		"not a list":   `{"error":"none"}`,
		"invalid date": collectionJSON("", strings.Replace(chatMessageJSON(t, "1616989753153", testRootID, "", "text", "ack", testCreatedAt), "2026-10-01T09:30:00.000Z", "yesterday", 1)),
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, body)
			})
			client, err := teams.New(teams.Config{Endpoint: provider.endpoint(), MaxResponseBytes: 1024}, staticTeamsCredentials())
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newTestDexContext("invalid"), client.ListThreadReplies(), teamsConnection, validListThreadRepliesInput())
			require.NoError(t, err)
			require.Equal(t, teams.ListThreadRepliesBranchInvalidResponse, result.Branch)
		})
	}
}

func TestListThreadRepliesValidatesInputWithoutARequest(t *testing.T) {
	client := newTeamsClient(t, "http://127.0.0.1:1/v1.0")
	for name, mutate := range map[string]func(*teams.ListThreadRepliesInput){
		"page size":  func(input *teams.ListThreadRepliesInput) { input.PageSize = 51 },
		"text limit": func(input *teams.ListThreadRepliesInput) { input.MaxTextCharacters = -1 },
		"message ID": func(input *teams.ListThreadRepliesInput) { input.MessageID = "latest" },
		"channel ID": func(input *teams.ListThreadRepliesInput) { input.ChannelID = "19:abc@thread.tacv2/messages" },
		"team ID":    func(input *teams.ListThreadRepliesInput) { input.TeamID = "" },
		"cursor length": func(input *teams.ListThreadRepliesInput) {
			input.Cursor = "http://127.0.0.1:1/" + strings.Repeat("a", 5000)
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := validListThreadRepliesInput()
			mutate(&input)
			started := time.Now()
			result, err := sdkgo.RunQuery(newTestDexContext("validate"), client.ListThreadReplies(), teamsConnection, input)
			require.NoError(t, err)
			require.Equal(t, teams.ListThreadRepliesBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
			require.Less(t, time.Since(started), time.Second, "no request was attempted")
		})
	}
}
