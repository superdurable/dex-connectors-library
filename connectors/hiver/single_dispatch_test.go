// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hiver"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const noteCreatedBody = `{"data":{"id":"35042650","conversation_id":"573741352","content":"Refund issued <REF-771>.",
	"author":{"id":"540349","email":"agent@acme.example.com"},"mentions":[],"parent_note_id":null,"attachments":[],
	"created_at":"2026-07-21T14:24:22.000000Z"}}`

func TestAddNotePostsOneMultipartNoteAndRecordsTheDispatchMarker(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, noteCreatedBody)
	})
	ctx := newHiverDexContext("note")
	result, err := sdkgo.RunMutation(ctx, newHiverClient(t, provider.URL).AddNote(), hiverConnection, hiver.AddNoteInput{
		InboxID: "105902", ConversationID: "19cfee91188070f8", Content: "Refund issued <REF-771>.",
	})
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchAdded, result.Branch)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/v1/inboxes/105902/conversations/19cfee91188070f8/notes", request.path)
	require.Equal(t, map[string]string{"content": "Refund issued <REF-771>."}, request.formFields(t))
	require.Equal(t, hiver.Note{
		ID: "35042650", ConversationID: "573741352", AuthorID: "540349", AuthorEmail: "agent@acme.example.com",
		CreatedAt: time.Date(2026, 7, 21, 14, 24, 22, 0, time.UTC),
	}, result.Value.Note)
	require.Equal(t, "35042650", result.Receipt.ProviderObjectID)
	require.NotEmpty(t, ctx.recordedHeartbeat, "the dispatch marker is recorded before the send")
}

func TestCreateSharedDraftPostsOneMultipartDraft(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":{"id":1234,"shared_draft_id":123,"reply_to_message_id":834466048}}`)
	})
	client := newHiverClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newHiverDexContext("draft"), client.CreateSharedDraft(), hiverConnection, hiver.CreateSharedDraftInput{
		InboxID: "105902", HiverMessageID: "834466048", Body: "Thanks, Jane. We are refunding the duplicate charge.",
	})
	require.NoError(t, err)
	require.Equal(t, hiver.CreateSharedDraftBranchCreated, result.Branch)
	require.Equal(t, "/v1/inboxes/105902/conversations/shared-drafts", provider.request(0).path)
	require.Equal(t, map[string]string{"body": "Thanks, Jane. We are refunding the duplicate charge.", "hiver_message_id": "834466048"},
		provider.request(0).formFields(t))
	require.Equal(t, hiver.SharedDraft{ID: "123", ConversationID: "1234", ReplyToHiverMessageID: "834466048"}, result.Value)

	result, err = sdkgo.RunMutation(newHiverDexContext("draft-smtp"), client.CreateSharedDraft(), hiverConnection, hiver.CreateSharedDraftInput{
		InboxID: "105902", GmailMessageID: "<abc123@mail.gmail.com>", Body: "Thanks.",
	})
	require.NoError(t, err)
	require.Equal(t, hiver.CreateSharedDraftBranchCreated, result.Branch)
	require.Equal(t, map[string]string{"body": "Thanks.", "gmail_message_id": "<abc123@mail.gmail.com>"}, provider.request(1).formFields(t))
}

func TestSingleDispatchOutcomes(t *testing.T) {
	for name, test := range map[string]struct {
		reply      func(t *testing.T, response http.ResponseWriter)
		wantBranch sdkgo.BranchID
		isRetry    bool
	}{
		"lost response is uncertain": {
			reply: func(t *testing.T, response http.ResponseWriter) { dropConnection(t, response) }, wantBranch: hiver.AddNoteBranchUncertain,
		},
		"server error is uncertain": {
			reply: func(t *testing.T, response http.ResponseWriter) {
				writeJSON(t, response, http.StatusBadGateway, `SENTINEL`)
			},
			wantBranch: hiver.AddNoteBranchUncertain,
		},
		"unreadable success is uncertain": {
			reply: func(t *testing.T, response http.ResponseWriter) {
				writeJSON(t, response, http.StatusCreated, `{"data":{"note":"SENTINEL"}}`)
			},
			wantBranch: hiver.AddNoteBranchUncertain,
		},
		"unknown conversation": {
			reply: func(t *testing.T, response http.ResponseWriter) {
				writeJSON(t, response, http.StatusNotFound, `{"Message":"SENTINEL"}`)
			},
			wantBranch: hiver.AddNoteBranchNotFound,
		},
		"rejected note": {
			reply: func(t *testing.T, response http.ResponseWriter) {
				writeJSON(t, response, http.StatusBadRequest, `{"errors":[{"message":"SENTINEL"}]}`)
			},
			wantBranch: hiver.AddNoteBranchProviderRejected,
		},
		"rate limit is retried and clears the marker": {
			reply: func(t *testing.T, response http.ResponseWriter) {
				response.Header().Set("Retry-After", "2")
				writeJSON(t, response, http.StatusTooManyRequests, `{"Message":"SENTINEL"}`)
			},
			isRetry: true,
		},
		"rate limit with a cut-off body is still a rate limit": {
			reply:   func(t *testing.T, response http.ResponseWriter) { cutOffBody(t, response, http.StatusTooManyRequests) },
			isRetry: true,
		},
		"unknown conversation with a cut-off body is still not found": {
			reply:      func(t *testing.T, response http.ResponseWriter) { cutOffBody(t, response, http.StatusNotFound) },
			wantBranch: hiver.AddNoteBranchNotFound,
		},
	} {
		provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(t, response) })
		ctx := newHiverDexContext("note")
		result, err := sdkgo.RunMutation(ctx, newHiverClient(t, provider.URL).AddNote(), hiverConnection,
			hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."})
		require.Equal(t, 1, provider.requestCount(), name)
		if test.isRetry {
			require.Error(t, err, name)
			require.Nil(t, ctx.recordedHeartbeat, "%s: a request Hiver refused may be sent again", name)
			continue
		}
		require.NoError(t, err, name)
		require.Equal(t, test.wantBranch, result.Branch, name)
		if test.wantBranch == hiver.AddNoteBranchUncertain {
			require.NotNil(t, ctx.recordedHeartbeat, "%s: the marker stays, so no later attempt resends", name)
		}
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "SENTINEL", name)
	}
}

func TestAttemptAfterAnUnconfirmedSendSelectsUncertainWithoutSending(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) { dropConnection(t, response) })
	client := newHiverClient(t, provider.URL)
	first := newHiverDexContext("draft")
	input := hiver.CreateSharedDraftInput{InboxID: "105902", HiverMessageID: "834466048", Body: "Thanks."}
	result, err := sdkgo.RunMutation(first, client.CreateSharedDraft(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.CreateSharedDraftBranchUncertain, result.Branch)

	result, err = sdkgo.RunMutation(first.nextAttempt(), client.CreateSharedDraft(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.CreateSharedDraftBranchUncertain, result.Branch)
	require.Equal(t, "an earlier attempt of this Step may have sent the request, so it is not sent again", result.Failure.Message)
	require.Equal(t, 1, provider.requestCount(), "the next attempt found the marker and sent nothing")
}

func TestAttemptThatFindsTheMarkerDoesNotWaitForARequestSlot(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) { dropConnection(t, response) })
	client, err := hiver.New(hiver.Config{RequestIntervalMilliseconds: 60000}, testCredentialProvider(), hiver.WithAPIBaseURL(provider.URL+"/v1"))
	require.NoError(t, err)
	first := newHiverDexContext("draft")
	input := hiver.CreateSharedDraftInput{InboxID: "105902", HiverMessageID: "834466048", Body: "Thanks."}
	result, err := sdkgo.RunMutation(first, client.CreateSharedDraft(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.CreateSharedDraftBranchUncertain, result.Branch)

	backlogged, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	second := first.nextAttempt()
	second.Context = backlogged
	result, err = sdkgo.RunMutation(second, client.CreateSharedDraft(), hiverConnection, input)
	require.NoError(t, err, "the next request slot is a minute away, but the marker decides without a request")
	require.Equal(t, hiver.CreateSharedDraftBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

// TestDispatchCheckpointThatDexNeverStoredIsSentAgain documents the window the checkpoint cannot close.
func TestDispatchCheckpointThatDexNeverStoredIsSentAgain(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			dropConnection(t, response)
			return
		}
		writeJSON(t, response, http.StatusCreated, noteCreatedBody)
	})
	client := newHiverClient(t, provider.URL)
	input := hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."}
	lostWorker := newHiverDexContext("note")
	lostWorker.losesRecordedHeartbeat = true
	_, err := sdkgo.RunMutation(lostWorker, client.AddNote(), hiverConnection, input)
	require.NoError(t, err, "Dex never receives this attempt's result, only the retry runs on")

	result, err := sdkgo.RunMutation(lostWorker.nextAttempt(), client.AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchAdded, result.Branch)
	require.Equal(t, 2, provider.requestCount(), "without a stored checkpoint the retry sends a second note")
}

func TestUnrecordedDispatchMarkerSendsNothing(t *testing.T) {
	provider := newRecordingHiver(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request expected") })
	ctx := newHiverDexContext("note")
	ctx.rejectsHeartbeat = true
	_, err := sdkgo.RunMutation(ctx, newHiverClient(t, provider.URL).AddNote(), hiverConnection,
		hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."})
	require.Error(t, err, "the attempt is retried")
	require.Zero(t, provider.requestCount())
}

func TestSingleDispatchInputsAreValidatedBeforeAnyRequest(t *testing.T) {
	provider := newRecordingHiver(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request expected") })
	client := newHiverClient(t, provider.URL)
	for name, input := range map[string]hiver.AddNoteInput{
		"blank content":   {InboxID: "105902", ConversationID: "1", Content: "  "},
		"oversized note":  {InboxID: "105902", ConversationID: "1", Content: string(make([]byte, 65537))},
		"conversation ID": {InboxID: "105902", ConversationID: "../1", Content: "x"},
	} {
		result, err := sdkgo.RunMutation(newHiverDexContext("note"), client.AddNote(), hiverConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, hiver.AddNoteBranchDefect, result.Branch, name)
	}
	for name, input := range map[string]hiver.CreateSharedDraftInput{
		"no message":         {InboxID: "105902", Body: "x"},
		"both messages":      {InboxID: "105902", HiverMessageID: "1", GmailMessageID: "19cfee91188070f8", Body: "x"},
		"invalid message ID": {InboxID: "105902", GmailMessageID: "<a b>", Body: "x"},
		"blank body":         {InboxID: "105902", HiverMessageID: "1"},
	} {
		result, err := sdkgo.RunMutation(newHiverDexContext("draft"), client.CreateSharedDraft(), hiverConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, hiver.CreateSharedDraftBranchDefect, result.Branch, name)
	}
}
