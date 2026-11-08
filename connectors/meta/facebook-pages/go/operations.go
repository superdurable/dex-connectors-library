// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package facebookpages

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListPostsInput selects a Facebook Page and an optional feed cursor.
type ListPostsInput struct {
	// PageID is the numeric Page ID from Meta Business Suite.
	PageID string `json:"pageId"`
	// After is Meta's opaque cursor from a prior page; blank starts at the first page.
	After string `json:"after,omitempty"`
}

// PagePostMetadata contains one post's public identity and display fields.
type PagePostMetadata struct {
	// ID is Meta's stable Page post ID.
	ID string `json:"id"`
	// Message is the optional text visible on the post.
	Message string `json:"message"`
	// CreatedTime is Meta's raw post timestamp.
	CreatedTime string `json:"createdTime"`
	// PermalinkURL is the optional public HTTPS URL of the post.
	PermalinkURL string `json:"permalinkUrl"`
}

// PagePostsPage contains one bounded Page feed page.
type PagePostsPage struct {
	// Posts are the feed posts in provider order.
	Posts []PagePostMetadata `json:"posts"`
	// NextCursor is blank when Meta reported no later page.
	NextCursor string `json:"nextCursor"`
}

// GetPostInput selects one post ID from listPosts or a Page event.
type GetPostInput struct {
	// PostID is a numeric or PageID_PostID Graph API identifier.
	PostID string `json:"postId"`
}

// ListPostsOperation reads one Page feed page.
type ListPostsOperation struct{ client *Client }

// GetPostOperation reads one post's public metadata.
type GetPostOperation struct{ client *Client }

// ListPosts binds the feed Query to this client.
func (client *Client) ListPosts() ListPostsOperation { return ListPostsOperation{client} }

// GetPost binds the post Query to this client.
func (client *Client) GetPost() GetPostOperation { return GetPostOperation{client} }

// Definition returns the generated feed Query contract.
func (ListPostsOperation) Definition() sdkgo.QueryDefinition { return ListPostsDefinition }

// Definition returns the generated post Query contract.
func (GetPostOperation) Definition() sdkgo.QueryDefinition { return GetPostDefinition }

var numericID = regexp.MustCompile(`^[0-9]{1,40}$`)
var postID = regexp.MustCompile(`^[0-9]{1,40}(?:_[0-9]{1,40})?$`)

// Invoke lists posts while projecting only ID, message, timestamp, and permalink.
func (operation ListPostsOperation) Invoke(call sdkgo.Call, input ListPostsInput) sdkgo.QueryAttempt[PagePostsPage] {
	if !numericID.MatchString(input.PageID) || len(input.After) > 4096 || strings.ContainsAny(input.After, "\r\n\x00") {
		return queryBranch("listPosts", ListPostsBranchDefect, PagePostsPage{}, sdkgo.FailureValidation, "Page ID or cursor is invalid")
	}
	query := url.Values{"fields": {"id,message,created_time,permalink_url"}, "limit": {"100"}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	response := operation.client.get(call, "/"+input.PageID+"/feed?"+query.Encode())
	if decision, handled := classifyGraphResponse("listPosts", response, PagePostsPage{}, ListPostsBranchNotFound, ListPostsBranchProviderRejected, ListPostsBranchInvalidResponse, ListPostsBranchDefect); handled {
		return decision
	}
	var wire struct {
		Data   []postWire `json:"data"`
		Paging struct {
			Cursors struct {
				After string `json:"after"`
			} `json:"cursors"`
		} `json:"paging"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Data == nil || len(wire.Data) > 100 || len(wire.Paging.Cursors.After) > 4096 {
		return queryBranch("listPosts", ListPostsBranchInvalidResponse, PagePostsPage{}, sdkgo.FailureProtocol, "Meta returned invalid Page feed JSON")
	}
	page := PagePostsPage{Posts: make([]PagePostMetadata, 0, len(wire.Data)), NextCursor: wire.Paging.Cursors.After}
	for _, post := range wire.Data {
		metadata, valid := projectPost(post)
		if !valid {
			return queryBranch("listPosts", ListPostsBranchInvalidResponse, PagePostsPage{}, sdkgo.FailureProtocol, "Meta returned incomplete post metadata")
		}
		page.Posts = append(page.Posts, metadata)
	}
	return sdkgo.NewQueryBranch(ListPostsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke reads one post without comments, reactions, or other user data.
func (operation GetPostOperation) Invoke(call sdkgo.Call, input GetPostInput) sdkgo.QueryAttempt[PagePostMetadata] {
	if !postID.MatchString(input.PostID) {
		return queryBranch("getPost", GetPostBranchDefect, PagePostMetadata{}, sdkgo.FailureValidation, "post ID is invalid")
	}
	query := url.Values{"fields": {"id,message,created_time,permalink_url"}}
	response := operation.client.get(call, "/"+input.PostID+"?"+query.Encode())
	if decision, handled := classifyGraphResponse("getPost", response, PagePostMetadata{}, GetPostBranchNotFound, GetPostBranchProviderRejected, GetPostBranchInvalidResponse, GetPostBranchDefect); handled {
		return decision
	}
	var wire postWire
	if err := json.Unmarshal(response.body, &wire); err != nil {
		return queryBranch("getPost", GetPostBranchInvalidResponse, PagePostMetadata{}, sdkgo.FailureProtocol, "Meta returned invalid post JSON")
	}
	metadata, valid := projectPost(wire)
	if !valid {
		return queryBranch("getPost", GetPostBranchInvalidResponse, PagePostMetadata{}, sdkgo.FailureProtocol, "Meta returned incomplete post metadata")
	}
	return sdkgo.NewQueryBranch(GetPostBranchFound, metadata, nil, sdkgo.Receipt{})
}

type postWire struct {
	ID           string `json:"id"`
	Message      string `json:"message"`
	CreatedTime  string `json:"created_time"`
	PermalinkURL string `json:"permalink_url"`
}

func projectPost(wire postWire) (PagePostMetadata, bool) {
	if !postID.MatchString(wire.ID) || wire.CreatedTime == "" || (wire.PermalinkURL != "" && !strings.HasPrefix(wire.PermalinkURL, "https://")) {
		return PagePostMetadata{}, false
	}
	return PagePostMetadata{ID: wire.ID, Message: wire.Message, CreatedTime: wire.CreatedTime, PermalinkURL: wire.PermalinkURL}, true
}

func classifyGraphResponse[Output any](operation string, response graphResponse, empty Output, notFound, rejected, invalid, defect sdkgo.BranchID) (sdkgo.QueryAttempt[Output], bool) {
	if errors.Is(response.err, errCredential) {
		return queryBranch(operation, defect, empty, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid"), true
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureTransport, "Meta request failed"), 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureRateLimit, "Meta rate limited the request"), response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureAvailability, "Meta is temporarily unavailable"), response.retryAfter), true
	}
	if response.status == http.StatusNotFound {
		return queryBranch(operation, notFound, empty, sdkgo.FailureNotFound, "Meta did not find the resource"), true
	}
	if response.status != http.StatusOK {
		return queryBranch(operation, rejected, empty, sdkgo.FailureProviderRejection, "Meta rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return queryBranch(operation, invalid, empty, sdkgo.FailureResponseTooLarge, "Meta response exceeded the configured limit"), true
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[Output](queryFailure(operation, sdkgo.FailureTransport, "Meta response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[Output]{}, false
}

func queryBranch[Output any](operation string, branch sdkgo.BranchID, value Output, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[Output] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func queryFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "facebook-pages", Operation: operation, Message: message}
}
