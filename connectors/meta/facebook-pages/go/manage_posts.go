// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package facebookpages

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdatePostInput replaces the plain-text message of one Page post.
type UpdatePostInput struct {
	// PostID is the numeric or PageID_PostID Graph API identifier.
	PostID string `json:"postId"`
	// Message is the complete replacement text, not an appended fragment.
	Message string `json:"message"`
}

// DeletePostInput identifies one Page post to delete.
type DeletePostInput struct {
	// PostID is the numeric or PageID_PostID Graph API identifier.
	PostID string `json:"postId"`
}

// ChangedPost identifies the Page post Meta changed.
type ChangedPost struct {
	// PostID is the requested stable post ID.
	PostID string `json:"postId"`
}

// UpdatePostOperation replaces a Page post message.
type UpdatePostOperation struct{ client *Client }

// DeletePostOperation removes a Page post.
type DeletePostOperation struct{ client *Client }

// UpdatePost binds the Mutation to this client.
func (client *Client) UpdatePost() UpdatePostOperation { return UpdatePostOperation{client} }

// DeletePost binds the Mutation to this client.
func (client *Client) DeletePost() DeletePostOperation { return DeletePostOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (UpdatePostOperation) Definition() sdkgo.MutationDefinition { return UpdatePostDefinition }

// Definition returns the manifest-generated Mutation contract.
func (DeletePostOperation) Definition() sdkgo.MutationDefinition { return DeletePostDefinition }

// IdempotencyKey records the call identity; replacing the full text repeats safely.
func (UpdatePostOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdatePostInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records the call identity; deleting the same post repeats safely.
func (DeletePostOperation) IdempotencyKey(callID sdkgo.CallID, _ DeletePostInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke submits an absolute message replacement.
func (operation UpdatePostOperation) Invoke(call sdkgo.Call, input UpdatePostInput) sdkgo.MutationAttempt[ChangedPost] {
	if !postID.MatchString(input.PostID) || strings.TrimSpace(input.Message) == "" || !utf8.ValidString(input.Message) || len(input.Message) > 65536 {
		return postChangeBranch("updatePost", UpdatePostBranchDefect, sdkgo.FailureValidation, "post ID or message is invalid")
	}
	form := url.Values{"message": {input.Message}}
	response := operation.client.changePost(call, http.MethodPost, input.PostID, form)
	return classifyPostChange("updatePost", response, input.PostID, UpdatePostBranchUpdated, UpdatePostBranchNotFound,
		UpdatePostBranchProviderRejected, UpdatePostBranchInvalidResponse, UpdatePostBranchDefect, false)
}

// Invoke requests deletion of one Page post.
func (operation DeletePostOperation) Invoke(call sdkgo.Call, input DeletePostInput) sdkgo.MutationAttempt[ChangedPost] {
	if !postID.MatchString(input.PostID) {
		return postChangeBranch("deletePost", DeletePostBranchDefect, sdkgo.FailureValidation, "post ID is invalid")
	}
	response := operation.client.changePost(call, http.MethodDelete, input.PostID, nil)
	return classifyPostChange("deletePost", response, input.PostID, DeletePostBranchDeleted, DeletePostBranchNotFound,
		DeletePostBranchProviderRejected, DeletePostBranchInvalidResponse, DeletePostBranchDefect, true)
}

func (client *Client) changePost(call sdkgo.Call, method string, postID string, form url.Values) graphResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return graphResponse{err: errCredential}
	}
	accessToken := credentials.PageAccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return graphResponse{err: errCredential}
	}
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+"/"+postID, body)
	if err != nil {
		return graphResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return graphResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if bytes.Contains(responseBody, []byte(accessToken)) {
		return graphResponse{status: response.StatusCode, retryAfter: retryAfter, err: errCredential}
	}
	return graphResponse{body: responseBody, status: response.StatusCode, retryAfter: retryAfter, err: err}
}

func classifyPostChange(operation string, response graphResponse, postID string, success, notFound, rejected, invalid, defect sdkgo.BranchID, allowEmpty bool) sdkgo.MutationAttempt[ChangedPost] {
	if errors.Is(response.err, errCredential) {
		return postChangeBranch(operation, defect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[ChangedPost](postChangeFailure(operation, sdkgo.FailureTransport, "Meta request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[ChangedPost](postChangeFailure(operation, sdkgo.FailureRateLimit, "Meta rate limited the post change"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[ChangedPost](postChangeFailure(operation, sdkgo.FailureAvailability, "Meta post change outcome is not confirmed"), response.retryAfter)
	}
	if graphErr, ok := decodeGraphError(response.body); response.status >= 400 && response.err == nil && ok {
		if graphErr.isThrottled() {
			return sdkgo.NewMutationRetry[ChangedPost](postChangeFailure(operation, sdkgo.FailureRateLimit, "Meta rate limited the post change"), response.retryAfter)
		}
		if graphErr.isTransient {
			return sdkgo.NewMutationRetry[ChangedPost](postChangeFailure(operation, sdkgo.FailureAvailability, "Meta reported a transient error"), response.retryAfter)
		}
	}
	if response.status == http.StatusNotFound {
		return postChangeBranch(operation, notFound, sdkgo.FailureNotFound, "Meta did not find the post")
	}
	if response.status < 200 || response.status >= 300 {
		return postChangeBranch(operation, rejected, sdkgo.FailureProviderRejection, "Meta rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return postChangeBranch(operation, invalid, sdkgo.FailureResponseTooLarge, "Meta response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[ChangedPost](postChangeFailure(operation, sdkgo.FailureTransport, "Meta response was interrupted"), 0)
	}
	if allowEmpty && response.status == http.StatusNoContent && len(response.body) == 0 {
		return sdkgo.NewMutationBranch(success, ChangedPost{PostID: postID}, nil, sdkgo.Receipt{})
	}
	var body struct {
		Success bool `json:"success"`
	}
	if json.Unmarshal(response.body, &body) != nil || !body.Success {
		return postChangeBranch(operation, invalid, sdkgo.FailureProtocol, "Meta returned an invalid post change acknowledgement")
	}
	return sdkgo.NewMutationBranch(success, ChangedPost{PostID: postID}, nil, sdkgo.Receipt{})
}

func postChangeFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "facebook-pages", Operation: operation, Message: message}
}

func postChangeBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ChangedPost] {
	failure := postChangeFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, ChangedPost{}, &failure, sdkgo.Receipt{})
}
