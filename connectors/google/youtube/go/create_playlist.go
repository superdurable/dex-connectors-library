// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youtube

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreatePlaylistInput supplies complete metadata for one new playlist.
type CreatePlaylistInput struct {
	// Title is the new playlist title.
	Title string `json:"title"`
	// Description is optional playlist text; blank creates an empty description.
	Description string `json:"description"`
	// PrivacyStatus is public, unlisted, or private.
	PrivacyStatus string `json:"privacyStatus"`
}

// CreatePlaylistOperation creates one playlist with a bounded send count.
type CreatePlaylistOperation struct{ client *Client }

// CreatePlaylist binds the limited-repeat playlist creation Mutation.
func (client *Client) CreatePlaylist() CreatePlaylistOperation {
	return CreatePlaylistOperation{client}
}

// Definition returns the generated playlist creation contract.
func (CreatePlaylistOperation) Definition() sdkgo.MutationDefinition { return CreatePlaylistDefinition }

// IdempotencyKey uses the stable Step call ID for its dispatch checkpoint.
func (CreatePlaylistOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePlaylistInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

var createPlaylistBranches = operationBranches{
	quotaExhausted: CreatePlaylistBranchQuotaExhausted,
	rejected:       CreatePlaylistBranchProviderRejected,
	defect:         CreatePlaylistBranchDefect,
}

// Invoke sends at most call.MaxSends creates, reading back before a possible resend.
func (operation CreatePlaylistOperation) Invoke(call sdkgo.Call, input CreatePlaylistInput) sdkgo.MutationAttempt[Playlist] {
	if strings.TrimSpace(input.Title) == "" || !validPlaylistPrivacy(input.PrivacyStatus) {
		return sdkgo.NewMutationBranch(CreatePlaylistBranchDefect, Playlist{}, providerFailurePointer("createPlaylist", sdkgo.FailureValidation, "playlist title or privacy status is invalid"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreatePlaylistDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreatePlaylistBranchDefect, Playlist{}, providerFailurePointer("createPlaylist", sdkgo.FailureLocalDefect, "playlist dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationUncertain(Playlist{}, providerFailure("createPlaylist", sdkgo.FailureTransport, "an earlier playlist create may have succeeded"), sdkgo.Receipt{})
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		playlist, found, receipt, outcome := operation.readBackCreatedPlaylist(call, input)
		if outcome != nil {
			if outcome.disposition == outcomeRetry {
				return sdkgo.NewMutationRetry[Playlist](outcome.failure, outcome.retryAfter)
			}
			return sdkgo.NewMutationUncertain(Playlist{}, providerFailure("createPlaylist", outcome.failure.Kind, "an earlier playlist create may have succeeded but the playlist listing was rejected or invalid"), receipt)
		}
		if found {
			return sdkgo.NewMutationBranch(CreatePlaylistBranchCreated, playlist, nil, receipt)
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return sdkgo.NewMutationUncertain(Playlist{}, providerFailure("createPlaylist", sdkgo.FailureTransport, "the send cap was reached and the playlist was not found"), receipt)
		}
	}
	body, err := json.Marshal(map[string]any{"snippet": map[string]string{"title": input.Title, "description": input.Description}, "status": map[string]string{"privacyStatus": input.PrivacyStatus}})
	if err != nil {
		return sdkgo.NewMutationBranch(CreatePlaylistBranchDefect, Playlist{}, providerFailurePointer("createPlaylist", sdkgo.FailureLocalDefect, "playlist creation could not be encoded"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "createPlaylist", providerRequest{method: http.MethodPost, path: "/playlists", query: url.Values{"part": {"snippet,status"}}, body: body, requiresManage: true, writeFence: fence}, "")
	if outcome != nil {
		switch outcome.disposition {
		case outcomeRetryUnprocessed:
			if err := fence.RecordCanResend(); err != nil {
				return sdkgo.NewMutationRetry[Playlist](providerFailure("createPlaylist", sdkgo.FailureAvailability, "playlist no-send checkpoint is unavailable"), 0)
			}
			return sdkgo.NewMutationRetry[Playlist](outcome.failure, outcome.retryAfter)
		case outcomeWriteUnconfirmed:
			return sdkgo.NewMutationRetry[Playlist](outcome.failure, 0)
		case outcomeRetry:
			return sdkgo.NewMutationRetry[Playlist](outcome.failure, outcome.retryAfter)
		}
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(Playlist{}, providerFailure("createPlaylist", outcome.failure.Kind, "an earlier playlist create may have succeeded before this refusal"), outcome.receipt)
		}
		return mutationAttemptForOutcome[Playlist](*outcome, createPlaylistBranches)
	}
	receipt := operation.client.receipt(call, "", response.header)
	var item providerPlaylist
	if decodeResponse(response.body, &item) != nil {
		return sdkgo.NewMutationRetry[Playlist](providerFailure("createPlaylist", sdkgo.FailureProtocol, "YouTube accepted the create but returned malformed JSON"), 0)
	}
	playlist, ok := convertProviderPlaylist(item)
	if !ok || playlist.Title != input.Title || playlist.Description != input.Description || playlist.PrivacyStatus != input.PrivacyStatus {
		return sdkgo.NewMutationRetry[Playlist](providerFailure("createPlaylist", sdkgo.FailureProtocol, "YouTube accepted the create but returned different playlist fields"), 0)
	}
	// The fence claim already prevents an unconfirmed second send if this optional marker fails.
	_ = fence.RecordProviderID(playlist.ID)
	receipt.ProviderObjectID = playlist.ID
	return sdkgo.NewMutationBranch(CreatePlaylistBranchCreated, playlist, nil, receipt)
}

func (operation CreatePlaylistOperation) readBackCreatedPlaylist(call sdkgo.Call, input CreatePlaylistInput) (Playlist, bool, sdkgo.Receipt, *providerOutcome) {
	response, outcome := operation.client.exchange(call, "createPlaylist", providerRequest{method: http.MethodGet, path: "/playlists", query: url.Values{"part": {"snippet,status"}, "mine": {"true"}, "maxResults": {"50"}}, requiresManage: true}, "")
	if outcome != nil {
		return Playlist{}, false, outcome.receipt, outcome
	}
	receipt := operation.client.receipt(call, "", response.header)
	var page providerPlaylistPage
	if decodeResponse(response.body, &page) != nil || validateReturnedPageToken(page.NextPageToken) != nil {
		invalid := providerOutcome{disposition: outcomeInvalidResponse, failure: providerFailure("createPlaylist", sdkgo.FailureProtocol, "YouTube returned an invalid playlist read-back page"), receipt: receipt}
		return Playlist{}, false, receipt, &invalid
	}
	var match Playlist
	matches := 0
	for _, item := range page.Items {
		playlist, ok := convertProviderPlaylist(item)
		if !ok {
			invalid := providerOutcome{disposition: outcomeInvalidResponse, failure: providerFailure("createPlaylist", sdkgo.FailureProtocol, "YouTube returned an invalid playlist during read-back"), receipt: receipt}
			return Playlist{}, false, receipt, &invalid
		}
		if playlist.Title == input.Title && playlist.Description == input.Description && playlist.PrivacyStatus == input.PrivacyStatus {
			match = playlist
			matches++
		}
	}
	if matches > 1 || page.NextPageToken != "" {
		invalid := providerOutcome{disposition: outcomeInvalidResponse, failure: providerFailure("createPlaylist", sdkgo.FailureProtocol, "the playlist read-back is ambiguous or incomplete"), receipt: receipt}
		return Playlist{}, false, receipt, &invalid
	}
	if matches == 1 {
		receipt.ProviderObjectID = match.ID
	}
	return match, matches == 1, receipt, nil
}
