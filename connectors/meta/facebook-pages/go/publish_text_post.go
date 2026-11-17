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

// Invoke publishes under a single-dispatch fence. Meta has no post idempotency key, so an
// unknown outcome resends while possible sends stay below call.MaxSends() and selects
// uncertain at the cap. A checkpointed post ID completes without resending, and a
// definite refusal is never resent.
func (operation PublishTextPostOperation) Invoke(call sdkgo.Call, input PublishTextPostInput) sdkgo.MutationAttempt[PublishedPost] {
	if !numericID.MatchString(input.PageID) || strings.TrimSpace(input.Message) == "" || !utf8.ValidString(input.Message) || len(input.Message) > 65536 {
		return publishBranch("defect", sdkgo.FailureValidation, "Page ID or message is invalid")
	}
	fence, err := singledispatch.NewFence(call, PublishTextPostDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return publishBranch("defect", sdkgo.FailureLocalDefect, "Meta dispatch fence is unavailable")
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return retryPublish(sdkgo.FailureAvailability, "Meta dispatch checkpoint is unreadable; no post was sent in this attempt", 0)
	case singledispatch.ReadBackRequired, singledispatch.ReadBackThenResendAllowed:
		// Meta returned this ID only for a post it accepted, so the post is never resent.
		if postID.MatchString(decision.Marker.ProviderID) {
			return sdkgo.NewMutationBranch(PublishTextPostBranchPublished, PublishedPost{PostID: decision.Marker.ProviderID}, nil, sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return uncertainPublish(sdkgo.FailureTransport, "an earlier attempt may have published the post, and the send cap is reached")
		}
	}
	// This attempt's send would be the last one the cap allows when no further send remains.
	canResendUnknownOutcome := decision.MaximumPriorSends+1 < call.MaxSends()
	// An earlier send may have published, so a refusal of this resend cannot prove nothing changed.
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.PageAccessToken.Reveal()) {
		return localPublishFailure(fence, hasPossibleEarlierSend, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	accessToken := credentials.PageAccessToken.Reveal()
	form := url.Values{"message": {input.Message}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/"+input.PageID+"/feed", strings.NewReader(form.Encode()))
	if err != nil {
		return localPublishFailure(fence, hasPossibleEarlierSend, sdkgo.FailureLocalDefect, "post request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return localPublishFailure(fence, hasPossibleEarlierSend, sdkgo.FailureLocalDefect, "post request is invalid")
	}
	dispatch := &postDispatch{client: operation.client.httpClient, request: request, observation: &dispatchObservation{}}
	if err := fence.Dispatch(dispatch.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return retryPublish(sdkgo.FailureTransport, "Meta dispatch was not started", 0)
		}
		if dispatch.observation.isProvablyUndispatched() {
			// A lost canResend checkpoint leaves the claim, which the next attempt counts as a send.
			_ = fence.RecordCanResend()
			return retryPublish(sdkgo.FailureTransport, "Meta could not be reached before sending", 0)
		}
		return resendOrSelectUncertainPublish(canResendUnknownOutcome, sdkgo.FailureTransport, "Meta publish outcome is unknown", 0)
	}
	response := dispatch.response
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	responseBody, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	isClientError := response.StatusCode >= 400 && response.StatusCode < 500
	// A Graph throttling code means Meta refused the post before processing it.
	isRateLimited := response.StatusCode == http.StatusTooManyRequests || (isClientError && readErr == nil && isGraphThrottleBody(responseBody))
	if isRateLimited || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return retryPublish(sdkgo.FailureAvailability, "Meta retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if isRateLimited {
			kind = sdkgo.FailureRateLimit
		}
		return retryPublish(kind, "Meta did not process the post", retryAfter)
	}
	acceptedPostID := ""
	if readErr == nil {
		acceptedPostID = readPostID(responseBody)
	}
	if isDefiniteRefusalStatus(response.StatusCode) {
		if acceptedPostID != "" {
			// The body reports a created post, so a resend could duplicate it.
			return uncertainPublish(sdkgo.FailureProtocol, "Meta returned a contradictory publish response")
		}
		return rejectOrSelectUncertainPublish(fence, hasPossibleEarlierSend, rejectionFailureKind(response.StatusCode), "Meta rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return resendOrSelectUncertainPublish(canResendUnknownOutcome, sdkgo.FailureAvailability, "Meta publish outcome is unknown after HTTP "+strconv.Itoa(response.StatusCode), retryAfter)
	}
	if readErr != nil {
		return resendOrSelectUncertainPublish(canResendUnknownOutcome, sdkgo.FailureTransport, "Meta publish response could not be read", 0)
	}
	if acceptedPostID == "" {
		return resendOrSelectUncertainPublish(canResendUnknownOutcome, sdkgo.FailureProtocol, "Meta publish response is malformed", 0)
	}
	// A lost provider-ID checkpoint leaves the claim, which still counts this send.
	_ = fence.RecordProviderID(acceptedPostID)
	return sdkgo.NewMutationBranch(PublishTextPostBranchPublished, PublishedPost{PostID: acceptedPostID}, nil, sdkgo.Receipt{})
}

type postDispatch struct {
	client      *http.Client
	request     *http.Request
	response    *http.Response
	observation *dispatchObservation
}

func (dispatch *postDispatch) send(requestContext context.Context) error {
	request := dispatch.request.WithContext(httptrace.WithClientTrace(requestContext, dispatch.observation.clientTrace()))
	response, err := dispatch.client.Do(request)
	dispatch.response = response
	return err
}

// readPostID returns the body's post ID, or empty when the body has no valid one.
func readPostID(body []byte) string {
	var envelope struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &envelope) != nil || !postID.MatchString(envelope.ID) {
		return ""
	}
	return envelope.ID
}

// isDefiniteRefusalStatus reports the HTTP statuses that prove Meta refused the request.
func isDefiniteRefusalStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

func rejectionFailureKind(statusCode int) sdkgo.FailureKind {
	switch statusCode {
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusForbidden:
		return sdkgo.FailureAuthorization
	}
	return sdkgo.FailureProviderRejection
}

// resendOrSelectUncertainPublish retries a possibly published post while the send cap allows another send.
func resendOrSelectUncertainPublish(canResendUnknownOutcome bool, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[PublishedPost] {
	if canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return retryPublish(kind, message, retryAfter)
	}
	return uncertainPublish(kind, message)
}

// rejectOrSelectUncertainPublish selects providerRejected only when no earlier send may have published.
func rejectOrSelectUncertainPublish(fence *singledispatch.Fence, hasPossibleEarlierSend bool, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PublishedPost] {
	if hasPossibleEarlierSend {
		return uncertainPublish(kind, message)
	}
	// The branch is terminal, so a lost canResend checkpoint cannot cause a resend.
	_ = fence.RecordCanResend()
	return publishBranch("providerRejected", kind, message)
}

// localPublishFailure selects defect only when no earlier send may have published.
func localPublishFailure(fence *singledispatch.Fence, hasPossibleEarlierSend bool, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PublishedPost] {
	if hasPossibleEarlierSend {
		// notSent would hide the earlier possible send, so nothing is recorded.
		return retryPublish(kind, message+"; no post was sent in this attempt", 0)
	}
	// The branch is terminal, so a lost notSent checkpoint cannot cause a resend.
	_ = fence.RecordNotSent()
	return publishBranch("defect", kind, message)
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
