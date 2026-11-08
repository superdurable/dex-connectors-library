// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package facebookpages

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// PublishTextPostInput selects the Page and text to publish.
type PublishTextPostInput struct {
	// PageID is the numeric Page ID from Meta Business Suite.
	PageID string `json:"pageId"`
	// Message is nonempty UTF-8 text for the public Page feed.
	Message string `json:"message"`
}

// PublishedPost identifies a post Meta accepted for publication.
type PublishedPost struct {
	// PostID is Meta's PageID_PostID identifier.
	PostID string `json:"postId"`
}

// PublishTextPostOperation publishes one Page post through a durable dispatch fence.
type PublishTextPostOperation struct{ client *Client }

// PublishTextPost binds the post Mutation to this client.
func (client *Client) PublishTextPost() PublishTextPostOperation {
	return PublishTextPostOperation{client}
}

// Definition returns the manifest-generated post Mutation contract.
func (PublishTextPostOperation) Definition() sdkgo.MutationDefinition {
	return PublishTextPostDefinition
}

// IdempotencyKey records the call identity; Meta does not deduplicate with this key.
func (PublishTextPostOperation) IdempotencyKey(callID sdkgo.CallID, _ PublishTextPostInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke publishes once and fails closed if a response cannot confirm the outcome.
func (operation PublishTextPostOperation) Invoke(call sdkgo.Call, input PublishTextPostInput) sdkgo.MutationAttempt[PublishedPost] {
	if !numericID.MatchString(input.PageID) || strings.TrimSpace(input.Message) == "" || !utf8.ValidString(input.Message) || len(input.Message) > 65536 {
		return publishBranch("defect", sdkgo.FailureValidation, "Page ID or message is invalid")
	}
	fence, err := singledispatch.NewFence(call, PublishTextPostDefinition.StepDefaults.ExecuteMethodTimeout)
	if err != nil {
		return publishBranch("defect", sdkgo.FailureLocalDefect, "Meta dispatch fence is unavailable")
	}
	if fence.Decide().Kind != singledispatch.SendAllowed {
		return uncertainPublish(sdkgo.FailureTransport, "Meta publish outcome cannot be confirmed; inspect the Page before retrying")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return publishBranch("defect", sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	accessToken := credentials.PageAccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return publishBranch("defect", sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	form := url.Values{"message": {input.Message}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/"+input.PageID+"/feed", strings.NewReader(form.Encode()))
	if err != nil {
		return publishBranch("defect", sdkgo.FailureLocalDefect, "post request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return publishBranch("defect", sdkgo.FailureLocalDefect, "post request is invalid")
	}
	dispatch := &postDispatch{client: operation.client.httpClient, request: request}
	if err := fence.Dispatch(dispatch.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return retryPublish(sdkgo.FailureTransport, "Meta dispatch was not started", 0)
		}
		if !dispatch.didGetConnection.Load() {
			if checkpointErr := fence.RecordCanResend(); checkpointErr == nil {
				return retryPublish(sdkgo.FailureTransport, "Meta could not be reached before sending", 0)
			}
		}
		return uncertainPublish(sdkgo.FailureTransport, "Meta publish outcome is unknown")
	}
	response := dispatch.response
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return retryPublish(sdkgo.FailureAvailability, "Meta retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return retryPublish(kind, "Meta did not process the post", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict {
		return uncertainPublish(sdkgo.FailureAvailability, "Meta publish outcome is unknown")
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return publishBranch("providerRejected", kind, "Meta rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return uncertainPublish(sdkgo.FailureTransport, "Meta publish response could not be confirmed")
	}
	var envelope struct {
		ID string `json:"id"`
	}
	if response.StatusCode != http.StatusOK || json.Unmarshal(responseBody, &envelope) != nil || !postID.MatchString(envelope.ID) {
		return uncertainPublish(sdkgo.FailureProtocol, "Meta publish response is incomplete")
	}
	if err := fence.RecordProviderID(envelope.ID); err != nil {
		return uncertainPublish(sdkgo.FailureLocalDefect, "Meta post ID could not be checkpointed")
	}
	return sdkgo.NewMutationBranch(PublishTextPostBranchPublished, PublishedPost{PostID: envelope.ID}, nil, sdkgo.Receipt{})
}

type postDispatch struct {
	client           *http.Client
	request          *http.Request
	response         *http.Response
	didGetConnection atomic.Bool
}

func (dispatch *postDispatch) send(requestContext context.Context) error {
	trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { dispatch.didGetConnection.Store(true) }}
	request := dispatch.request.WithContext(httptrace.WithClientTrace(requestContext, trace))
	response, err := dispatch.client.Do(request)
	dispatch.response = response
	return err
}

func publishFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "facebook-pages", Operation: "publishTextPost", Message: message}
}

func publishBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PublishedPost] {
	failure := publishFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, PublishedPost{}, &failure, sdkgo.Receipt{})
}

func retryPublish(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[PublishedPost] {
	return sdkgo.NewMutationRetry[PublishedPost](publishFailure(kind, message), retryAfter)
}

func uncertainPublish(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PublishedPost] {
	return sdkgo.NewMutationUncertain(PublishedPost{}, publishFailure(kind, message), sdkgo.Receipt{})
}
