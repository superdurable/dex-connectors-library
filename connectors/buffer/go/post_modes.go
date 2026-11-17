// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package buffer

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateScheduledPostInput creates a post at a specific UTC time.
type CreateScheduledPostInput struct {
	// ChannelID is the connected channel ID.
	ChannelID string `json:"channelId"`
	// Text is post content; at least one asset is required when blank.
	Text string `json:"text"`
	// Assets are publicly hosted images, videos, or documents.
	Assets []PostAsset `json:"assets,omitempty"`
	// DueAt is the publish time as an RFC 3339 timestamp with a UTC offset.
	DueAt string `json:"dueAt"`
}

// PublishPostNowInput creates a post for immediate publishing.
type PublishPostNowInput struct {
	// ChannelID is the connected channel ID.
	ChannelID string `json:"channelId"`
	// Text is post content; at least one asset is required when blank.
	Text string `json:"text"`
	// Assets are publicly hosted images, videos, or documents.
	Assets []PostAsset `json:"assets,omitempty"`
}

// CreateScheduledPostOperation schedules one text post.
type CreateScheduledPostOperation struct{ client *Client }

// PublishPostNowOperation creates one post for immediate publishing.
type PublishPostNowOperation struct{ client *Client }

// CreateScheduledPost binds scheduled creation to this client.
func (client *Client) CreateScheduledPost() CreateScheduledPostOperation {
	return CreateScheduledPostOperation{client: client}
}

// PublishPostNow binds immediate publishing to this client.
func (client *Client) PublishPostNow() PublishPostNowOperation {
	return PublishPostNowOperation{client: client}
}

// Definition returns the generated scheduled post contract.
func (CreateScheduledPostOperation) Definition() sdkgo.MutationDefinition {
	return CreateScheduledPostDefinition
}

// Definition returns the generated immediate publishing contract.
func (PublishPostNowOperation) Definition() sdkgo.MutationDefinition { return PublishPostNowDefinition }

// IdempotencyKey preserves identity across bounded scheduled creation attempts.
func (CreateScheduledPostOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateScheduledPostInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey preserves identity across bounded immediate publishing attempts.
func (PublishPostNowOperation) IdempotencyKey(callID sdkgo.CallID, _ PublishPostNowInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke schedules one text post with bounded resend after an unknown outcome.
func (operation CreateScheduledPostOperation) Invoke(call sdkgo.Call, input CreateScheduledPostInput) sdkgo.MutationAttempt[PostProfile] {
	const name = "createScheduledPost"
	if !validGraphQLID(input.ChannelID) || strings.TrimSpace(input.Text) == "" && len(input.Assets) == 0 {
		return bufferMutationBranch(CreateScheduledPostBranchDefect, PostProfile{}, name, sdkgo.FailureValidation, "channel ID and text or assets are required")
	}
	dueAt, err := time.Parse(time.RFC3339Nano, input.DueAt)
	if err != nil || dueAt.IsZero() {
		return bufferMutationBranch(CreateScheduledPostBranchDefect, PostProfile{}, name, sdkgo.FailureValidation, "dueAt must be an RFC 3339 timestamp")
	}
	mediaInputs, mediaErr := bufferPostAssetInputs(input.Assets)
	if mediaErr != nil {
		return bufferMutationBranch(CreateScheduledPostBranchDefect, PostProfile{}, name, sdkgo.FailureValidation, mediaErr.Error())
	}
	return operation.client.createPostWithMode(call, name, input.ChannelID, input.Text, "customScheduled", dueAt.UTC().Format(time.RFC3339Nano), mediaInputs, CreateScheduledPostDefinition, CreateScheduledPostBranchCreated, CreateScheduledPostBranchProviderRejected, CreateScheduledPostBranchDefect)
}

// Invoke creates one text post for immediate publishing with bounded resend.
func (operation PublishPostNowOperation) Invoke(call sdkgo.Call, input PublishPostNowInput) sdkgo.MutationAttempt[PostProfile] {
	const name = "publishPostNow"
	if !validGraphQLID(input.ChannelID) || strings.TrimSpace(input.Text) == "" && len(input.Assets) == 0 {
		return bufferMutationBranch(PublishPostNowBranchDefect, PostProfile{}, name, sdkgo.FailureValidation, "channel ID and text or assets are required")
	}
	mediaInputs, mediaErr := bufferPostAssetInputs(input.Assets)
	if mediaErr != nil {
		return bufferMutationBranch(PublishPostNowBranchDefect, PostProfile{}, name, sdkgo.FailureValidation, mediaErr.Error())
	}
	return operation.client.createPostWithMode(call, name, input.ChannelID, input.Text, "shareNow", "", mediaInputs, PublishPostNowDefinition, PublishPostNowBranchCreated, PublishPostNowBranchProviderRejected, PublishPostNowBranchDefect)
}

func (client *Client) createPostWithMode(call sdkgo.Call, name, channelID, text, mode, dueAt string, mediaInputs []map[string]any, definition sdkgo.MutationDefinition, created, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[PostProfile] {
	fence, err := singledispatch.NewFence(call, definition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return bufferMutationBranch(defect, PostProfile{}, name, sdkgo.FailureLocalDefect, "post dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(PostProfile{}, bufferFailure(name, sdkgo.FailureTransport, "an earlier post may have been created"), sdkgo.Receipt{})
	}
	postInput := map[string]any{"channelId": channelID, "text": text, "mode": mode, "schedulingType": "automatic"}
	if dueAt != "" {
		postInput["dueAt"] = dueAt
	}
	if mediaInputs != nil {
		postInput["assets"] = mediaInputs
	}
	var response providerResponse
	err = fence.Dispatch(func(ctx context.Context) error {
		response = client.executeGraphQLWithContext(ctx, call, createPostGraphQL, map[string]any{"input": postInput})
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[PostProfile](bufferFailure(name, sdkgo.FailureAvailability, "post dispatch was not recorded or sent"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[PostProfile](bufferFailure(name, sdkgo.FailureAvailability, "post retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[PostProfile](bufferFailure(name, kind, "Buffer asked to retry the post"), response.retryAfter)
	}
	if err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 || hasUnconfirmedCreatePostResponse(response) {
		if decision.MaximumPriorSends+1 >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(PostProfile{}, bufferFailure(name, sdkgo.FailureTransport, "post outcome is unknown after bounded sends"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[PostProfile](bufferFailure(name, sdkgo.FailureTransport, "post outcome is unknown"), 0)
	}
	return classifyPostMutation(response, name, "createPost", channelID, created, rejected, rejected, defect)
}
