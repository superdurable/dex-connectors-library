// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver_test

import (
	"encoding/json"
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
	require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch)
	require.Equal(t, 2, provider.requestCount())
}

func TestSharedFenceRejectsStaleCanResendAfterLostClaim(t *testing.T) {
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
	require.Error(t, err)
	ctx.AdvanceAttempt()
	ctx.LoseNextHeartbeat()
	result, err := sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, client.AddNote(), hiverConnection, input)
	require.NoError(t, err)
	require.Equal(t, hiver.AddNoteBranchUncertain, result.Branch)
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

func latestFenceMarker(t *testing.T, ctx *singledispatchtest.Context) singledispatch.Marker {
	t.Helper()
	frames := ctx.Heartbeats()
	require.NotEmpty(t, frames)
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(frames[len(frames)-1], &marker))
	return marker
}
