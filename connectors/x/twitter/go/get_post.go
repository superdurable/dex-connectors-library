// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package x

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetPostInput selects one public X Post by its numeric URL ID.
type GetPostInput struct {
	// PostID is the decimal value after /status/ in an x.com Post URL.
	PostID string `json:"postId"`
}

// PostMetadata contains the public Post identity and text.
type PostMetadata struct {
	// ID is the decimal Post ID returned by X.
	ID string `json:"id"`
	// Text is the Post's public text.
	Text string `json:"text"`
}

// GetPostOperation reads one public Post.
type GetPostOperation struct{ client *Client }

// GetPost binds the Post Query to this client.
func (client *Client) GetPost() GetPostOperation { return GetPostOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetPostOperation) Definition() sdkgo.QueryDefinition { return GetPostDefinition }

// Invoke reads one Post and classifies retryable and definitive outcomes.
func (operation GetPostOperation) Invoke(call sdkgo.Call, input GetPostInput) sdkgo.QueryAttempt[PostMetadata] {
	if !validPostID(input.PostID) {
		return postBranch(GetPostBranchDefect, sdkgo.FailureValidation, "Post ID must be 1–20 decimal digits")
	}
	response := operation.client.get(call, "/tweets/"+input.PostID)
	if errors.Is(response.err, errCredential) {
		return postBranch(GetPostBranchDefect, sdkgo.FailureAuthentication, "X credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[PostMetadata](postFailure(sdkgo.FailureTransport, "X request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[PostMetadata](postFailure(sdkgo.FailureRateLimit, "X rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[PostMetadata](postFailure(sdkgo.FailureAvailability, "X is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return postBranch(GetPostBranchNotFound, sdkgo.FailureNotFound, "X did not expose the Post")
	}
	if response.status != http.StatusOK {
		return postBranch(GetPostBranchProviderRejected, sdkgo.FailureProviderRejection, "X rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return postBranch(GetPostBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "X response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[PostMetadata](postFailure(sdkgo.FailureTransport, "X response was interrupted"), 0)
	}
	post, err := decodePost(response.body, input.PostID)
	if err != nil {
		return postBranch(GetPostBranchInvalidResponse, sdkgo.FailureProtocol, "X returned invalid Post JSON")
	}
	return sdkgo.NewQueryBranch(GetPostBranchFound, post, nil, sdkgo.Receipt{})
}

func validPostID(postID string) bool {
	if len(postID) == 0 || len(postID) > 20 || postID[0] == '0' {
		return false
	}
	for _, digit := range postID {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func decodePost(body []byte, requestedPostID string) (PostMetadata, error) {
	var wire struct {
		Data struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return PostMetadata{}, err
	}
	if wire.Data.ID != requestedPostID || strings.TrimSpace(wire.Data.Text) == "" {
		return PostMetadata{}, fmt.Errorf("Post identity or text missing")
	}
	return PostMetadata{ID: wire.Data.ID, Text: wire.Data.Text}, nil
}

func postFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "x-twitter", Operation: "getPost", Message: message}
}

func postBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PostMetadata] {
	failure := postFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, PostMetadata{}, &failure, sdkgo.Receipt{})
}
