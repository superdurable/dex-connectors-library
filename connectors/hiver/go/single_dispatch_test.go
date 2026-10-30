// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
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

func TestAddNotePostsOneMultipartNoteAndRecordsTheFenceClaim(t *testing.T) {
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
	require.Contains(t, string(ctx.recordedHeartbeat), `"state":"claimed"`)
	require.Contains(t, string(ctx.recordedHeartbeat), `"providerId":"35042650"`)
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
		"service unavailable is retried": {
			reply: func(t *testing.T, response http.ResponseWriter) {
				writeJSON(t, response, http.StatusServiceUnavailable, `{"Message":"SENTINEL"}`)
			},
			isRetry: true,
		},
		"conflict without read back is uncertain": {
			reply: func(t *testing.T, response http.ResponseWriter) {
				writeJSON(t, response, http.StatusConflict, `{"Message":"SENTINEL"}`)
			},
			wantBranch: hiver.AddNoteBranchUncertain,
		},
		"interrupted accepted body is uncertain": {
			reply:      func(t *testing.T, response http.ResponseWriter) { cutOffBody(t, response, http.StatusCreated) },
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
		"rate limit is retried and records canResend": {
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
			require.Contains(t, string(ctx.recordedHeartbeat), `"state":"canResend"`, name)
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
	require.Equal(t, "an earlier attempt may have sent the request, so it is not sent again", result.Failure.Message)
	require.Equal(t, 1, provider.requestCount(), "the next attempt found the marker and sent nothing")
}

func TestPriorDispatchPrecedesCredentialResolution(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) { dropConnection(t, response) })
	first := newHiverDexContext("note")
	input := hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."}
	result, err := sdkgo.RunMutation(first, newHiverClient(t, provider.URL).AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch)
	clientWithoutCredentials, err := hiver.New(hiver.Config{RequestIntervalMilliseconds: 1},
		sdkgo.StaticCredentialProvider[hiver.Credentials]{}, hiver.WithAPIBaseURL(provider.URL+"/v1"))
	require.NoError(t, err)
	result, err = sdkgo.RunMutation(first.nextAttempt(), clientWithoutCredentials.AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestTooLittleAttemptTimeRecordsNotSentBeforeRetry(t *testing.T) {
	provider := newRecordingHiver(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request expected") })
	stepContext := newHiverDexContext("note")
	deadlineContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
	defer cancel()
	stepContext.Context = deadlineContext
	_, err := sdkgo.RunMutation(stepContext, newHiverClient(t, provider.URL).AddNote(), hiverConnection,
		hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."})
	require.Error(t, err)
	require.Contains(t, string(stepContext.recordedHeartbeat), `"state":"notSent"`)
	require.Zero(t, provider.requestCount())
}

func TestDraftServiceUnavailableRetriesWithSameCallID(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{"Message":"busy"}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, `{"data":{"id":573741352,"shared_draft_id":901,"reply_to_message_id":834466048}}`)
	})
	client := newHiverClient(t, provider.URL)
	input := hiver.CreateSharedDraftInput{InboxID: "105902", HiverMessageID: "834466048", Body: "Thanks."}
	first := newHiverDexContext("draft")
	_, err := sdkgo.RunMutation(first, client.CreateSharedDraft(), hiverConnection, input)
	require.Error(t, err)
	require.Contains(t, string(first.recordedHeartbeat), `"state":"canResend"`)
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.CreateSharedDraft(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.CreateSharedDraftBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.NotEmpty(t, provider.request(0).header.Get("X-Dex-Call-ID"))
	require.Equal(t, provider.request(0).header.Get("X-Dex-Call-ID"), provider.request(1).header.Get("X-Dex-Call-ID"))
}

func TestAcceptedDraftWithoutIDIsNeverResent(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":{"id":573741352}}`)
	})
	client := newHiverClient(t, provider.URL)
	input := hiver.CreateSharedDraftInput{InboxID: "105902", HiverMessageID: "834466048", Body: "Thanks."}
	first := newHiverDexContext("draft")
	result, err := sdkgo.RunMutation(first, client.CreateSharedDraft(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.CreateSharedDraftBranchUncertain, result.Branch)
	result, err = sdkgo.RunMutation(first.nextAttempt(), client.CreateSharedDraft(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.CreateSharedDraftBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestAcceptedNoteBodyCutAtAttemptDeadlineIsNeverResent(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		response.Header().Set("Content-Length", "100")
		response.WriteHeader(http.StatusCreated)
		response.(http.Flusher).Flush()
		<-request.Context().Done()
	})
	stepContext := newHiverDexContext("note")
	deadlineContext, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	stepContext.Context = deadlineContext
	client := newHiverClient(t, provider.URL)
	input := hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."}
	result, err := sdkgo.RunMutation(stepContext, client.AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
	result, err = sdkgo.RunMutation(stepContext.nextAttempt(), client.AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestTracedDialFailureRecordsSafeRetry(t *testing.T) {
	client, err := hiver.New(hiver.Config{RequestIntervalMilliseconds: 1}, testCredentialProvider(),
		hiver.WithAPIBaseURL(closedLoopbackURL(t)+"/v1"))
	require.NoError(t, err)
	stepContext := newHiverDexContext("note")
	_, err = sdkgo.RunMutation(stepContext, client.AddNote(), hiverConnection,
		hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."})
	require.Error(t, err)
	require.Contains(t, string(stepContext.recordedHeartbeat), `"state":"canResend"`)
}

type failingRoundTrip func(*http.Request) (*http.Response, error)

func (roundTrip failingRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestUntracedDialErrorFailsClosed(t *testing.T) {
	provider := newRecordingHiver(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request expected") })
	client, err := hiver.New(hiver.Config{RequestIntervalMilliseconds: 1}, testCredentialProvider(),
		hiver.WithAPIBaseURL(provider.URL+"/v1"), hiver.WithHTTPClient(&http.Client{Transport: failingRoundTrip(func(*http.Request) (*http.Response, error) {
			return nil, &net.OpError{Op: "dial", Err: errors.New("synthetic failure without connection trace")}
		})}))
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(newHiverDexContext("note"), client.AddNote(), hiverConnection,
		hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."})
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch)
	require.Zero(t, provider.requestCount())
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

func TestDispatchCheckpointThatDexNeverStoredFailsClosed(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			dropConnection(t, response)
			return
		}
		t.Fatal("a later attempt must not resend without a canResend or notSent checkpoint")
	})
	client := newHiverClient(t, provider.URL)
	input := hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."}
	lostWorker := newHiverDexContext("note")
	lostWorker.losesRecordedHeartbeat = true
	_, err := sdkgo.RunMutation(lostWorker, client.AddNote(), hiverConnection, input)
	require.NoError(t, err, "Dex never receives this attempt's result, only the retry runs on")

	result, err := sdkgo.RunMutation(lostWorker.nextAttempt(), client.AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount(), "a missing checkpoint fails closed")
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
