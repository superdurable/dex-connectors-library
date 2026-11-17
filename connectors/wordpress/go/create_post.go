// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wordpress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreatePostInput creates a draft by default or publishes immediately when Status is publish.
type CreatePostInput struct {
	// Title is the nonempty post title, entered as plain text or permitted HTML.
	Title string `json:"title"`
	// Content is the post body in the site's accepted HTML or block format.
	Content string `json:"content"`
	// Status is draft or publish; empty defaults to draft.
	Status string `json:"status,omitempty"`
}

// CreatedPost identifies a post created by WordPress without persisting its content.
type CreatedPost struct {
	// ID is the stable numeric WordPress post ID.
	ID int64 `json:"id"`
	// Status is the status WordPress assigned to the new post.
	Status string `json:"status"`
}

// CreatePostOperation creates one post with a durable single-dispatch fence.
type CreatePostOperation struct{ client *Client }

// Definition returns the manifest-generated post-creation contract.
func (CreatePostOperation) Definition() sdkgo.MutationDefinition { return CreatePostDefinition }

// IdempotencyKey records stable call identity; WordPress does not accept it for deduplication.
func (CreatePostOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePostInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates the post under a single-dispatch fence. WordPress has no create idempotency key,
// so an unknown outcome resends while possible sends stay below call.MaxSends() and selects
// uncertain at the cap. A definite refusal is never resent.
func (operation CreatePostOperation) Invoke(call sdkgo.Call, input CreatePostInput) sdkgo.MutationAttempt[CreatedPost] {
	status := input.Status
	if status == "" {
		status = "draft"
	}
	if strings.TrimSpace(input.Title) == "" || !utf8.ValidString(input.Title) || !utf8.ValidString(input.Content) || (status != "draft" && status != "publish") {
		return createFailure(CreatePostBranchDefect, sdkgo.FailureValidation, "title, content, or status is invalid")
	}
	payload, err := json.Marshal(struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Status  string `json:"status"`
	}{Title: input.Title, Content: input.Content, Status: status})
	if err != nil || int64(len(payload)) > operation.client.maxPostBytes {
		return createFailure(CreatePostBranchDefect, sdkgo.FailureValidation, "post exceeds the configured request limit")
	}
	fence, err := singledispatch.NewFence(call, CreatePostDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createFailure(CreatePostBranchDefect, sdkgo.FailureLocalDefect, "WordPress dispatch fence is unavailable")
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return createRetry(sdkgo.FailureAvailability, "WordPress dispatch checkpoint is unreadable; no post was created in this attempt", 0)
	case singledispatch.ReadBackRequired:
		return createUncertain(sdkgo.FailureTransport, "an earlier attempt may have created the WordPress post, and the send cap is reached")
	case singledispatch.ReadBackThenResendAllowed:
		// A saved post ID proves WordPress created the post, so it is never resent.
		if decision.Marker.ProviderID != "" {
			return createUncertain(sdkgo.FailureTransport, "an earlier attempt created the WordPress post, but its confirmation was lost")
		}
	}
	// This attempt's send would be the last one the cap allows when no further send remains.
	canResendUnknownOutcome := decision.MaximumPriorSends+1 < call.MaxSends()
	// An earlier send may have created the post, so a refusal of this resend cannot prove nothing changed.
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return localCreateFailure(fence, hasPossibleEarlierSend, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	password := strings.ReplaceAll(credentials.ApplicationPassword.Reveal(), " ", "")
	if !validWordPressUsername(credentials.Username) || !providerhttp.IsHeaderSafeCredential(password) {
		return localCreateFailure(fence, hasPossibleEarlierSend, sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	endpoint := operation.client.siteURL + "/wp-json/wp/v2/posts"
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return localCreateFailure(fence, hasPossibleEarlierSend, sdkgo.FailureLocalDefect, "WordPress post request could not be built")
	}
	request.SetBasicAuth(credentials.Username, password)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return localCreateFailure(fence, hasPossibleEarlierSend, sdkgo.FailureLocalDefect, "WordPress post request is invalid")
	}
	observation := &dispatchObservation{}
	var response *http.Response
	err = fence.Dispatch(func(requestContext context.Context) error {
		request = request.WithContext(httptrace.WithClientTrace(requestContext, observation.clientTrace()))
		var sendErr error
		response, sendErr = operation.client.httpClient.Do(request)
		return sendErr
	})
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return createRetry(sdkgo.FailureTransport, "WordPress dispatch was not started", 0)
		}
		if observation.isProvablyUndispatched() {
			// A lost canResend checkpoint leaves the claim, which the next attempt counts as a send.
			_ = fence.RecordCanResend()
			return createRetry(sdkgo.FailureTransport, "WordPress could not be reached before sending", 0)
		}
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureTransport, "WordPress post outcome is unknown", 0)
	}
	// Closing a response cannot change a confirmed provider outcome.
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return createRetry(sdkgo.FailureAvailability, "WordPress retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return createRetry(kind, "WordPress did not create the post", retryAfter)
	}
	isDefiniteRefusalStatus := isDefiniteRefusalStatus(response.StatusCode)
	isSuccessStatus := response.StatusCode >= 200 && response.StatusCode < 300
	if !isDefiniteRefusalStatus && !isSuccessStatus {
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureAvailability, "WordPress post outcome is unknown after HTTP "+strconv.Itoa(response.StatusCode), retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureTransport, "WordPress post response could not be read", 0)
	}
	var wire struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	isWireDecoded := json.Unmarshal(body, &wire) == nil
	// A positive post ID in the body means WordPress created the post.
	isAccepted := isWireDecoded && wire.ID > 0
	if isDefiniteRefusalStatus {
		if isAccepted {
			return createUncertain(sdkgo.FailureProtocol, "WordPress returned a contradictory post response")
		}
		return rejectOrSelectUncertain(fence, hasPossibleEarlierSend, rejectionFailureKind(response.StatusCode), "WordPress rejected the post with HTTP "+strconv.Itoa(response.StatusCode))
	}
	if !isAccepted {
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureProtocol, "WordPress post response is malformed", 0)
	}
	// A lost provider-ID checkpoint leaves the claim, which still counts this send.
	_ = fence.RecordProviderID(strconv.FormatInt(wire.ID, 10))
	if response.StatusCode != http.StatusCreated || !validCreatedPostStatus(wire.Status) {
		// WordPress reported a post ID, so a resend could duplicate the post.
		return createUncertain(sdkgo.FailureProtocol, "WordPress created a post but returned an unexpected response")
	}
	return sdkgo.NewMutationBranch(CreatePostBranchCreated, CreatedPost{ID: wire.ID, Status: wire.Status}, nil, sdkgo.Receipt{})
}

// isDefiniteRefusalStatus reports the HTTP statuses that prove WordPress refused the request.
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

// resendOrSelectUncertain retries a possibly created post while the send cap allows another send.
func resendOrSelectUncertain(canResendUnknownOutcome bool, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[CreatedPost] {
	if canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return createRetry(kind, message, retryAfter)
	}
	return createUncertain(kind, message)
}

// rejectOrSelectUncertain selects providerRejected only when no earlier send of this call may have created the post.
func rejectOrSelectUncertain(fence *singledispatch.Fence, hasPossibleEarlierSend bool, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CreatedPost] {
	if hasPossibleEarlierSend {
		return createUncertain(kind, message)
	}
	// The branch is terminal, so a lost canResend checkpoint cannot cause a resend.
	_ = fence.RecordCanResend()
	return createFailure(CreatePostBranchProviderRejected, kind, message)
}

// localCreateFailure selects defect only when no earlier send may have created the post.
func localCreateFailure(fence *singledispatch.Fence, hasPossibleEarlierSend bool, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CreatedPost] {
	if hasPossibleEarlierSend {
		// notSent would hide the earlier possible send, so nothing is recorded.
		return createRetry(kind, message+"; no post was created in this attempt", 0)
	}
	// The branch is terminal, so a lost notSent checkpoint cannot cause a resend.
	_ = fence.RecordNotSent()
	return createFailure(CreatePostBranchDefect, kind, message)
}

func validCreatedPostStatus(status string) bool {
	switch status {
	case "draft", "publish", "pending", "private", "future":
		return true
	}
	return false
}

func createFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CreatedPost] {
	failure := sdkgo.Failure{Kind: kind, Provider: "wordpress", Operation: "createPost", Message: message}
	return sdkgo.NewMutationBranch(branch, CreatedPost{}, &failure, sdkgo.Receipt{})
}
func createRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[CreatedPost] {
	failure := sdkgo.Failure{Kind: kind, Provider: "wordpress", Operation: "createPost", Message: message}
	return sdkgo.NewMutationRetry[CreatedPost](failure, retryAfter)
}
func createUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CreatedPost] {
	failure := sdkgo.Failure{Kind: kind, Provider: "wordpress", Operation: "createPost", Message: message}
	return sdkgo.NewMutationUncertain(CreatedPost{}, failure, sdkgo.Receipt{})
}
