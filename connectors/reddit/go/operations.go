// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reddit

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var postIDPattern = regexp.MustCompile(`^t3_[a-z0-9]{1,20}$`)

// GetPostInput names a Reddit post by its full t3_ ID.
type GetPostInput struct {
	// PostID is the full t3_ ID copied from the Reddit post URL or API.
	PostID string `json:"postId"`
}

// Post contains bounded public metadata for one Reddit post.
type Post struct {
	// ID is the full t3_ post ID.
	ID string `json:"id"`
	// Title is the post's current title.
	Title string `json:"title"`
	// Subreddit is the community name without r/.
	Subreddit string `json:"subreddit"`
	// Permalink is the relative Reddit post URL.
	Permalink string `json:"permalink"`
	// Score is the current vote score, which may change.
	Score int64 `json:"score"`
}

// GetPostOperation reads one approved Data API post.
type GetPostOperation struct{ client *Client }

// GetPost binds the Query to this client.
func (client *Client) GetPost() GetPostOperation { return GetPostOperation{client} }

// Definition returns the generated post Query contract.
func (GetPostOperation) Definition() sdkgo.QueryDefinition { return GetPostDefinition }

// Invoke reads one post and classifies the provider outcome.
func (operation GetPostOperation) Invoke(call sdkgo.Call, input GetPostInput) sdkgo.QueryAttempt[Post] {
	if !postIDPattern.MatchString(input.PostID) {
		return postBranch(GetPostBranchDefect, sdkgo.FailureValidation, "Reddit post ID must be a full t3_ identifier")
	}
	query := url.Values{"id": {input.PostID}, "raw_json": {"1"}}
	response := operation.client.get(call, "/api/info?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return postBranch(GetPostBranchDefect, sdkgo.FailureAuthentication, "Reddit OAuth credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return postRetry(sdkgo.FailureTransport, "Reddit request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return postRetry(sdkgo.FailureRateLimit, "Reddit rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return postRetry(sdkgo.FailureAvailability, "Reddit is temporarily unavailable", response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return postBranch(GetPostBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Reddit response exceeded the configured limit")
	}
	if response.err != nil {
		return postRetry(sdkgo.FailureTransport, "Reddit response was interrupted", 0)
	}
	if response.status != http.StatusOK {
		return postBranch(GetPostBranchProviderRejected, sdkgo.FailureProviderRejection, "Reddit rejected HTTP "+strconv.Itoa(response.status))
	}
	post, found, err := decodePost(response.body, input.PostID)
	if err != nil {
		return postBranch(GetPostBranchInvalidResponse, sdkgo.FailureProtocol, "Reddit returned invalid post JSON")
	}
	if !found {
		return sdkgo.NewQueryBranch(GetPostBranchMissing, Post{}, nil, sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetPostBranchFound, post, nil, sdkgo.Receipt{})
}

func decodePost(body []byte, postID string) (Post, bool, error) {
	var listing struct {
		Kind string `json:"kind"`
		Data struct {
			Children []struct {
				Kind string `json:"kind"`
				Data struct {
					Name      string `json:"name"`
					Title     string `json:"title"`
					Subreddit string `json:"subreddit"`
					Permalink string `json:"permalink"`
					Score     int64  `json:"score"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		return Post{}, false, err
	}
	if listing.Kind != "Listing" || listing.Data.Children == nil || len(listing.Data.Children) > 1 {
		return Post{}, false, errors.New("Reddit listing shape is invalid")
	}
	if len(listing.Data.Children) == 0 {
		return Post{}, false, nil
	}
	child := listing.Data.Children[0]
	if child.Kind != "t3" || child.Data.Name != postID || child.Data.Title == "" || child.Data.Subreddit == "" || !strings.HasPrefix(child.Data.Permalink, "/r/") || strings.ContainsAny(child.Data.Permalink, "\x00\r\n") {
		return Post{}, false, errors.New("Reddit post fields are invalid")
	}
	return Post{ID: child.Data.Name, Title: child.Data.Title, Subreddit: child.Data.Subreddit, Permalink: child.Data.Permalink, Score: child.Data.Score}, true, nil
}

func postFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "reddit", Operation: "getPost", Message: message}
}
func postBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Post] {
	failure := postFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, Post{}, &failure, sdkgo.Receipt{})
}
func postRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[Post] {
	return sdkgo.NewQueryRetry[Post](postFailure(kind, message), delay)
}
