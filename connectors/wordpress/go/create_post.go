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
	"sync/atomic"
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

// Invoke dispatches at most once and fails closed when a previous attempt could have created a post.
func (operation CreatePostOperation) Invoke(call sdkgo.Call, input CreatePostInput) sdkgo.MutationAttempt[CreatedPost] {
	fence, err := singledispatch.NewFence(call, CreatePostDefinition.StepDefaults.ExecuteMethodTimeout)
	if err != nil {
		return createFailure(CreatePostBranchDefect, sdkgo.FailureLocalDefect, "WordPress dispatch fence is unavailable")
	}
	if fence.Decide().Kind != singledispatch.SendAllowed {
		return createUncertain(sdkgo.FailureTransport, "WordPress post outcome cannot be confirmed; do not create another")
	}
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
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return createFailure(CreatePostBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	password := strings.ReplaceAll(credentials.ApplicationPassword.Reveal(), " ", "")
	if !validWordPressUsername(credentials.Username) || !providerhttp.IsHeaderSafeCredential(password) {
		return createFailure(CreatePostBranchDefect, sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	endpoint := operation.client.siteURL + "/wp-json/wp/v2/posts"
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return createFailure(CreatePostBranchDefect, sdkgo.FailureLocalDefect, "WordPress post request could not be built")
	}
	request.SetBasicAuth(credentials.Username, password)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return createFailure(CreatePostBranchDefect, sdkgo.FailureLocalDefect, "WordPress post request is invalid")
	}
	var didGetConnection atomic.Bool
	var response *http.Response
	err = fence.Dispatch(func(requestContext context.Context) error {
		trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { didGetConnection.Store(true) }}
		request = request.WithContext(httptrace.WithClientTrace(requestContext, trace))
		response, err = operation.client.httpClient.Do(request)
		return err
	})
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return createRetry(sdkgo.FailureTransport, "WordPress dispatch was not started", 0)
		}
		if !didGetConnection.Load() {
			if checkpointErr := fence.RecordCanResend(); checkpointErr == nil {
				return createRetry(sdkgo.FailureTransport, "WordPress could not be reached before sending", 0)
			}
		}
		return createUncertain(sdkgo.FailureTransport, "WordPress post outcome is unknown")
	}
	// Closing a response cannot change a confirmed provider outcome.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return createRetry(sdkgo.FailureAvailability, "WordPress retry checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return createRetry(kind, "WordPress did not create the post", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict {
		return createUncertain(sdkgo.FailureAvailability, "WordPress post outcome is unknown")
	}
	if response.StatusCode == http.StatusUnauthorized {
		return createFailure(CreatePostBranchProviderRejected, sdkgo.FailureAuthentication, "WordPress rejected the application password")
	}
	if response.StatusCode == http.StatusForbidden {
		return createFailure(CreatePostBranchProviderRejected, sdkgo.FailureAuthorization, "WordPress denied post creation")
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		return createFailure(CreatePostBranchProviderRejected, sdkgo.FailureProviderRejection, "WordPress rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if response.StatusCode != http.StatusCreated {
		return createUncertain(sdkgo.FailureProtocol, "WordPress returned an unexpected post response")
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return createUncertain(sdkgo.FailureTransport, "WordPress created a post but its response could not be read")
	}
	var wire struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.ID <= 0 || !validCreatedPostStatus(wire.Status) {
		return createUncertain(sdkgo.FailureProtocol, "WordPress created a post but returned no usable ID")
	}
	// A lost provider-ID heartbeat does not permit another send.
	_ = fence.RecordProviderID(strconv.FormatInt(wire.ID, 10))
	return sdkgo.NewMutationBranch(CreatePostBranchCreated, CreatedPost{ID: wire.ID, Status: wire.Status}, nil, sdkgo.Receipt{})
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
