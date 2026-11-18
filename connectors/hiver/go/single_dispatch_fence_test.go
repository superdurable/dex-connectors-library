// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hiver/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestSharedFenceRetries503OnceThenNeverResendsAcceptedNote(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{"Message":"busy"}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, noteCreatedBody)
	})
	client := newHiverClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("hiver-fence-503", "add-note")
	input := hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."}

	_, err := sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.Error(t, err)
	require.Equal(t, singledispatch.StateCanResend, latestFenceMarker(t, ctx).State)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchAdded, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, "35042650", latestFenceMarker(t, ctx).ProviderID)

	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch, "a saved provider ID is never resent")
	require.Equal(t, 2, provider.requestCount())
}

func TestSharedFenceCountsALostClaimTowardTheSendCap(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{"Message":"busy"}`)
			return
		}
		dropConnection(t, response)
	})
	client := newHiverClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("hiver-fence-lost-claim", "add-note")
	input := hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."}

	_, err := sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.Error(t, err, "the 503 was not processed and is retried")
	ctx.AdvanceAttempt()
	ctx.LoseNextHeartbeat()
	_, err = sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.Error(t, err, "the first possible send is resent")
	ctx.AdvanceAttempt()
	_, err = sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.Error(t, err, "the stale canResend marker counts the lost claim, so one send remains")
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch, "the third possible send reaches the cap")
	require.Equal(t, 4, provider.requestCount())
	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch)
	require.Equal(t, 4, provider.requestCount(), "the fence refuses a send beyond the cap")
}

func TestSharedFenceUnreadableCheckpointRetriesWithoutSending(t *testing.T) {
	provider := newRecordingHiver(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request expected") })
	ctx := singledispatchtest.NewContext("hiver-fence-fail-closed", "add-note")
	ctx.AdvanceAttempt()
	ctx.SetReadError(errors.New("checkpoint unavailable"))
	_, err := sdkgo.RunMutation(ctx, newHiverClient(t, provider.URL).AddNote(), hiverConnection,
		hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."})
	require.Error(t, err, "FailClosed returns Retry instead of a terminal branch")
	require.Zero(t, provider.requestCount())
}

func TestSharedFenceRecordsCanResendBeforeADefiniteRefusal(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusUnprocessableEntity, `{"Message":"invalid"}`)
	})
	ctx := singledispatchtest.NewContext("hiver-fence-refusal", "add-note")
	result, err := sdkgo.RunMutation(ctx, newHiverClient(t, provider.URL).AddNote(), hiverConnection,
		hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."})
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchProviderRejected, result.Branch)
	require.Equal(t, singledispatch.StateCanResend, latestFenceMarker(t, ctx).State)
	require.Equal(t, 1, provider.requestCount(), "a definite refusal is never resent")
}

func TestSharedFenceRefusalAfterAPossibleSendIsUncertain(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusBadGateway, `{"Message":"busy"}`)
			return
		}
		writeJSON(t, response, http.StatusBadRequest, `{"Message":"duplicate"}`)
	})
	client := newHiverClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("hiver-fence-refusal-after-send", "add-note")
	input := hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."}
	_, err := sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch, "the first send may have added the note")
	require.Equal(t, 2, provider.requestCount())
}

func TestSharedFenceRecordsCredentialFailureAsNotSent(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, noteCreatedBody)
	})
	missingCredentialClient, err := hiver.New(hiver.Config{RequestIntervalMilliseconds: 1},
		sdkgo.StaticCredentialProvider[hiver.Credentials]{}, hiver.WithAPIBaseURL(provider.URL+"/v1"))
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("hiver-fence-credential", "add-note")
	input := hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."}

	_, err = sdkgo.RunMutation(ctx, missingCredentialClient.AddNote(), hiverConnection, input)
	require.Error(t, err)
	require.Equal(t, singledispatch.StateNotSent, latestFenceMarker(t, ctx).State)
	require.Zero(t, provider.requestCount())
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, newHiverClient(t, provider.URL).AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchAdded, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestSharedFencePacerWaitEndsBeforeStepDeadline(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			response.Header().Set("Retry-After", "60")
			writeJSON(t, response, http.StatusTooManyRequests, `{"Message":"busy"}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, noteCreatedBody)
	})
	heldClient := newHiverClient(t, provider.URL)
	_, err := sdkgo.RunQuery(singledispatchtest.NewContext("hiver-pacer", "list-inboxes"),
		heldClient.ListInboxes(), hiverConnection, hiver.ListInboxesInput{})
	require.Error(t, err)
	ctx := singledispatchtest.NewContext("hiver-pacer", "add-note")
	cancel := ctx.SetDeadline(time.Now().Add(600 * time.Millisecond))
	defer cancel()
	input := hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."}

	_, err = sdkgo.RunMutation(ctx, heldClient.AddNote(), hiverConnection, input)
	require.Error(t, err)
	require.NoError(t, ctx.Err(), "the not-sent checkpoint must be written while the Step is live")
	require.Equal(t, singledispatch.StateNotSent, latestFenceMarker(t, ctx).State)
	require.Equal(t, 1, provider.requestCount())
	ctx.AdvanceAttempt()
	cancelNext := ctx.SetDeadline(time.Now().Add(5 * time.Second))
	defer cancelNext()
	result, err := sdkgo.RunMutation(ctx, newHiverClient(t, provider.URL).AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchAdded, result.Branch)
	require.Equal(t, 2, provider.requestCount())
}

// sendUnknownThenRateLimited runs attempt 1 to an unknown outcome and attempt 2 to a 429 that records canResend.
func sendUnknownThenRateLimited(t *testing.T, ctx *singledispatchtest.Context, client *hiver.Client, input hiver.AddNoteInput) {
	t.Helper()
	_, err := sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.Error(t, err, "the lost response is resent below the cap")
	ctx.AdvanceAttempt()
	_, err = sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.Error(t, err, "the 429 is retried")
	require.Equal(t, singledispatch.StateCanResend, latestFenceMarker(t, ctx).State)
	ctx.AdvanceAttempt()
}

func TestSharedFenceRefusalAfterACanResendMarkerWithAPriorSendIsUncertain(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		switch index {
		case 0:
			dropConnection(t, response)
		case 1:
			writeJSON(t, response, http.StatusTooManyRequests, `{"Message":"busy"}`)
		default:
			writeJSON(t, response, http.StatusBadRequest, `{"Message":"duplicate"}`)
		}
	})
	client := newHiverClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("hiver-fence-canresend-prior-send", "add-note")
	input := hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."}
	sendUnknownThenRateLimited(t, ctx, client, input)

	result, err := sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch, "the first send may have added the note")
	require.Equal(t, 3, provider.requestCount())
}

func TestSharedFenceCredentialFailureAfterACanResendMarkerWithAPriorSend(t *testing.T) {
	for name, test := range map[string]struct {
		credentials sdkgo.CredentialProvider[hiver.Credentials]
		isTerminal  bool
	}{
		"transient failure retries":     {credentials: sdkgo.StaticCredentialProvider[hiver.Credentials]{}},
		"terminal failure is uncertain": {credentials: reauthorizationRequiredCredentials{}, isTerminal: true},
		"unusable API key is uncertain": {credentials: sdkgo.StaticCredentialProvider[hiver.Credentials]{hiverConnection: {APIKey: sdkgo.NewSecretString("bad key")}}, isTerminal: true},
	} {
		provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, index int) {
			if index == 0 {
				dropConnection(t, response)
				return
			}
			writeJSON(t, response, http.StatusTooManyRequests, `{"Message":"busy"}`)
		})
		ctx := singledispatchtest.NewContext("hiver-fence-credential-prior-send", "add-note")
		input := hiver.AddNoteInput{InboxID: "105902", ConversationID: "573741352", Content: "Triaged."}
		sendUnknownThenRateLimited(t, ctx, newHiverClient(t, provider.URL), input)
		heartbeatCount := len(ctx.Heartbeats())
		failingClient, err := hiver.New(hiver.Config{RequestIntervalMilliseconds: 1}, test.credentials, hiver.WithAPIBaseURL(provider.URL+"/v1"))
		require.NoError(t, err, name)

		result, err := sdkgo.RunMutation(ctx, failingClient.AddNote(), hiverConnection, input)
		if test.isTerminal {
			require.NoError(t, err, name)
			require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch, "%s: defect would hide the first possible send", name)
		} else {
			require.Error(t, err, name)
		}
		require.Len(t, ctx.Heartbeats(), heartbeatCount, "%s: notSent would hide the earlier possible send", name)
		require.Equal(t, 2, provider.requestCount(), name)
	}
}

func TestSharedFenceTerminalCredentialFailureOnAFirstSendIsDefect(t *testing.T) {
	provider := newRecordingHiver(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request expected") })
	client, err := hiver.New(hiver.Config{RequestIntervalMilliseconds: 1}, reauthorizationRequiredCredentials{}, hiver.WithAPIBaseURL(provider.URL+"/v1"))
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("hiver-fence-terminal-credential", "create-draft")
	result, err := sdkgo.RunMutation(ctx, client.CreateSharedDraft(), hiverConnection,
		hiver.CreateSharedDraftInput{InboxID: "105902", HiverMessageID: "834466048", Body: "Thanks."})
	require.NoError(t, err)
	require.Equal(t, hiver.CreateSharedDraftBranchDefect, result.Branch)
	require.Equal(t, singledispatch.StateNotSent, latestFenceMarker(t, ctx).State)
}

// reauthorizationRequiredCredentials resolves no credentials, as a revoked connection does.
type reauthorizationRequiredCredentials struct{}

func (reauthorizationRequiredCredentials) Resolve(sdkgo.Call) (hiver.Credentials, error) {
	return hiver.Credentials{}, sdkgo.NewReauthorizationRequiredError(errors.New("revoked"))
}

func latestFenceMarker(t *testing.T, ctx *singledispatchtest.Context) singledispatch.Marker {
	t.Helper()
	frames := ctx.Heartbeats()
	require.NotEmpty(t, frames)
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(frames[len(frames)-1], &marker))
	return marker
}
