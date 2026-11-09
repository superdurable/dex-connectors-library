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
	post, problem, err := decodePost(response.body, input.PostID)
	if err != nil {
		return postBranch(GetPostBranchInvalidResponse, sdkgo.FailureProtocol, "X returned invalid Post JSON")
	}
	switch problem {
	case postProblemNotFound:
		return postBranch(GetPostBranchNotFound, sdkgo.FailureNotFound, "X did not expose the Post")
	case postProblemNotAuthorized:
		return postBranch(GetPostBranchProviderRejected, sdkgo.FailureProviderRejection, "X denied access to the Post")
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

type postProblem int

const (
	postProblemNone postProblem = iota
	postProblemNotFound
	postProblemNotAuthorized
)

// decodePost returns the Post, or the problem X reported in an HTTP 200
// errors array without data, such as a deleted Post.
func decodePost(body []byte, requestedPostID string) (PostMetadata, postProblem, error) {
	var wire struct {
		Data *struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"data"`
		Errors []struct {
			Type string `json:"type"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return PostMetadata{}, postProblemNone, err
	}
	if wire.Data == nil {
		problem := postProblemNone
		for _, providerError := range wire.Errors {
			switch postProblemType(providerError.Type) {
			case "resource-not-found":
				return PostMetadata{}, postProblemNotFound, nil
			case "not-authorized-for-resource", "client-forbidden":
				problem = postProblemNotAuthorized
			}
		}
		if problem != postProblemNone {
			return PostMetadata{}, problem, nil
		}
		return PostMetadata{}, postProblemNone, fmt.Errorf("Post data missing")
	}
	if wire.Data.ID != requestedPostID || strings.TrimSpace(wire.Data.Text) == "" {
		return PostMetadata{}, postProblemNone, fmt.Errorf("Post identity or text missing")
	}
	return PostMetadata{ID: wire.Data.ID, Text: wire.Data.Text}, postProblemNone, nil
}

// postProblemType returns the final segment of an X problem type URI, which
// uses either the api.x.com or legacy api.twitter.com host.
func postProblemType(problemType string) string {
	for _, prefix := range []string{"https://api.x.com/2/problems/", "https://api.twitter.com/2/problems/"} {
		if strings.HasPrefix(problemType, prefix) {
			return strings.TrimPrefix(problemType, prefix)
		}
	}
	return ""
}

func postFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "x-twitter", Operation: "getPost", Message: message}
}

func postBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PostMetadata] {
	failure := postFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, PostMetadata{}, &failure, sdkgo.Receipt{})
}
