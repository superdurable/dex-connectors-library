// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reddit

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetUserInput identifies a public profile by username without u/.
type GetUserInput struct {
	// Username is the case-insensitive Reddit account name.
	Username string `json:"username"`
}

// UserProfile contains public account metadata.
type UserProfile struct {
	// Name is the account's provider spelling.
	Name string `json:"name"`
	// ID is the full t2_ account ID.
	ID string `json:"id"`
	// CommentKarma is the account's current comment karma.
	CommentKarma int64 `json:"commentKarma"`
	// LinkKarma is the account's current post karma.
	LinkKarma int64 `json:"linkKarma"`
	// ProfileURL links to the public Reddit profile.
	ProfileURL string `json:"profileUrl"`
}

// GetSubredditInput identifies one public community without r/.
type GetSubredditInput struct {
	// Subreddit is the community name.
	Subreddit string `json:"subreddit"`
}

// GetSubredditRulesInput identifies a community whose public rules to read.
type GetSubredditRulesInput struct {
	// Subreddit is the community name without r/.
	Subreddit string `json:"subreddit"`
}

// SubredditRules contains one community's public rules in provider order.
type SubredditRules struct {
	// Rules can be empty when moderators have not configured public rules.
	Rules []SubredditRule `json:"rules"`
}

// SubredditRule describes a community posting or commenting rule.
type SubredditRule struct {
	// ShortName is the rule's displayed title.
	ShortName string `json:"shortName"`
	// Description is the public rule explanation.
	Description string `json:"description,omitempty"`
	// Kind is all, link, or comment, identifying where the rule applies.
	Kind string `json:"kind"`
	// ViolationReason is the public report reason when provided.
	ViolationReason string `json:"violationReason,omitempty"`
}

// Subreddit contains public community details.
type Subreddit struct {
	// Name is the provider's community spelling without r/.
	Name string `json:"name"`
	// ID is the full t5_ community ID.
	ID string `json:"id"`
	// Title is the community's display title.
	Title string `json:"title"`
	// Description is the public description as plain text.
	Description string `json:"description,omitempty"`
	// Subscribers is the current subscriber count.
	Subscribers int64 `json:"subscribers"`
	// URL links to the community on Reddit.
	URL string `json:"url"`
}

// SearchSubredditsInput selects one page of public community search results.
type SearchSubredditsInput struct {
	// Query matches community names and descriptions; it must not be blank.
	Query string `json:"query"`
	// Limit is 1 through 100; zero uses Reddit's default of 25.
	Limit int `json:"limit,omitempty"`
	// After is a full t5_ cursor from the preceding page; blank starts at the first page.
	After string `json:"after,omitempty"`
}

// ListSubredditsInput selects one page from a public community directory.
type ListSubredditsInput struct {
	// Directory is popular, new, or default; blank selects popular.
	Directory string `json:"directory,omitempty"`
	// Limit is 1 through 100; zero uses Reddit's default of 25.
	Limit int `json:"limit,omitempty"`
	// After is a full t5_ cursor from the preceding page; blank starts at the first page.
	After string `json:"after,omitempty"`
}

// SubredditPage contains one page in Reddit's result order.
type SubredditPage struct {
	// Subreddits are the communities returned on this page.
	Subreddits []Subreddit `json:"subreddits"`
	// After is the next cursor, or blank at the end.
	After string `json:"after,omitempty"`
}

// ListPostCommentsInput identifies a post and bounds its top-level comment page.
type ListPostCommentsInput struct {
	// PostID is the full t3_ post ID.
	PostID string `json:"postId"`
	// Limit is 1 through 100; zero selects 25.
	Limit int `json:"limit,omitempty"`
}

// CommentPage contains only top-level comments returned for one post.
type CommentPage struct {
	// Comments are the top-level comments in Reddit's returned order.
	Comments []Comment `json:"comments"`
}

// Comment contains public comment text and metadata.
type Comment struct {
	// ID is the full t1_ comment ID.
	ID string `json:"id"`
	// ParentID identifies the containing post by its full t3_ ID.
	ParentID string `json:"parentId"`
	// Author is the account name or [deleted].
	Author string `json:"author"`
	// Body is untrusted Markdown text.
	Body string `json:"body"`
	// Permalink links to the comment on Reddit.
	Permalink string `json:"permalink"`
	// Score is the current vote score.
	Score int64 `json:"score"`
}

// GetUserOperation reads one public Reddit profile.
type GetUserOperation struct{ client *Client }

// GetSubredditOperation reads one public community.
type GetSubredditOperation struct{ client *Client }

// GetSubredditRulesOperation reads one community's public rules.
type GetSubredditRulesOperation struct{ client *Client }

// SearchSubredditsOperation searches public communities.
type SearchSubredditsOperation struct{ client *Client }

// ListSubredditsOperation reads a public community directory.
type ListSubredditsOperation struct{ client *Client }

// ListPostCommentsOperation reads top-level comments on one post.
type ListPostCommentsOperation struct{ client *Client }

// GetUser binds the public profile Query to this client.
func (client *Client) GetUser() GetUserOperation { return GetUserOperation{client} }

// GetSubreddit binds the community Query to this client.
func (client *Client) GetSubreddit() GetSubredditOperation { return GetSubredditOperation{client} }

// GetSubredditRules binds the public rules Query to this client.
func (client *Client) GetSubredditRules() GetSubredditRulesOperation {
	return GetSubredditRulesOperation{client}
}

// SearchSubreddits binds the community search Query to this client.
func (client *Client) SearchSubreddits() SearchSubredditsOperation {
	return SearchSubredditsOperation{client}
}

// ListSubreddits binds the public community directory Query to this client.
func (client *Client) ListSubreddits() ListSubredditsOperation {
	return ListSubredditsOperation{client}
}

// ListPostComments binds the comment Query to this client.
func (client *Client) ListPostComments() ListPostCommentsOperation {
	return ListPostCommentsOperation{client}
}

// Definition returns the generated user Query contract.
func (GetUserOperation) Definition() sdkgo.QueryDefinition { return GetUserDefinition }

// Definition returns the generated community Query contract.
func (GetSubredditOperation) Definition() sdkgo.QueryDefinition { return GetSubredditDefinition }

// Definition returns the generated community rules Query contract.
func (GetSubredditRulesOperation) Definition() sdkgo.QueryDefinition {
	return GetSubredditRulesDefinition
}

// Definition returns the generated community search Query contract.
func (SearchSubredditsOperation) Definition() sdkgo.QueryDefinition {
	return SearchSubredditsDefinition
}

// Definition returns the generated community directory Query contract.
func (ListSubredditsOperation) Definition() sdkgo.QueryDefinition { return ListSubredditsDefinition }

// Definition returns the generated comment Query contract.
func (ListPostCommentsOperation) Definition() sdkgo.QueryDefinition {
	return ListPostCommentsDefinition
}

// Invoke reads GET /user/{username}/about with the existing read scope.
func (operation GetUserOperation) Invoke(call sdkgo.Call, input GetUserInput) sdkgo.QueryAttempt[UserProfile] {
	branches := readBranches{operation: "getUser", success: GetUserBranchFound, missing: GetUserBranchNotFound, rejected: GetUserBranchProviderRejected, invalid: GetUserBranchInvalidResponse, defect: GetUserBranchDefect}
	if !usernamePattern.MatchString(input.Username) {
		return failedRead[UserProfile](branches, branches.defect, sdkgo.FailureValidation, "Reddit username is invalid")
	}
	return readPublic(operation.client, call, branches, "/user/"+url.PathEscape(input.Username)+"/about?raw_json=1", false, func(body []byte) (UserProfile, error) {
		var envelope struct {
			Kind string `json:"kind"`
			Data struct {
				Name         string `json:"name"`
				ID           string `json:"id"`
				CommentKarma int64  `json:"comment_karma"`
				LinkKarma    int64  `json:"link_karma"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return UserProfile{}, err
		}
		if envelope.Kind != "t2" || envelope.Data.ID == "" || strings.ContainsAny(envelope.Data.ID, "/?#") || !strings.EqualFold(envelope.Data.Name, input.Username) {
			return UserProfile{}, errors.New("Reddit user fields are invalid")
		}
		return UserProfile{Name: envelope.Data.Name, ID: "t2_" + envelope.Data.ID, CommentKarma: envelope.Data.CommentKarma, LinkKarma: envelope.Data.LinkKarma, ProfileURL: redditWebOrigin + "/user/" + url.PathEscape(envelope.Data.Name)}, nil
	})
}

// Invoke reads GET /r/{subreddit}/about with the existing read scope.
func (operation GetSubredditOperation) Invoke(call sdkgo.Call, input GetSubredditInput) sdkgo.QueryAttempt[Subreddit] {
	branches := readBranches{operation: "getSubreddit", success: GetSubredditBranchFound, missing: GetSubredditBranchUnavailable, rejected: GetSubredditBranchProviderRejected, invalid: GetSubredditBranchInvalidResponse, defect: GetSubredditBranchDefect}
	if !subredditNamePattern.MatchString(input.Subreddit) {
		return failedRead[Subreddit](branches, branches.defect, sdkgo.FailureValidation, "Reddit subreddit name is invalid")
	}
	return readPublic(operation.client, call, branches, "/r/"+input.Subreddit+"/about?raw_json=1", true, func(body []byte) (Subreddit, error) {
		var envelope struct {
			Kind string            `json:"kind"`
			Data subredditResponse `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return Subreddit{}, err
		}
		if envelope.Kind != "t5" || !strings.EqualFold(envelope.Data.Name, input.Subreddit) {
			return Subreddit{}, errors.New("Reddit subreddit fields are invalid")
		}
		return envelope.Data.convert()
	})
}

// Invoke reads GET /r/{subreddit}/about/rules with the existing read scope.
func (operation GetSubredditRulesOperation) Invoke(call sdkgo.Call, input GetSubredditRulesInput) sdkgo.QueryAttempt[SubredditRules] {
	branches := readBranches{operation: "getSubredditRules", success: GetSubredditRulesBranchFound, missing: GetSubredditRulesBranchUnavailable, rejected: GetSubredditRulesBranchProviderRejected, invalid: GetSubredditRulesBranchInvalidResponse, defect: GetSubredditRulesBranchDefect}
	if !subredditNamePattern.MatchString(input.Subreddit) {
		return failedRead[SubredditRules](branches, branches.defect, sdkgo.FailureValidation, "Reddit subreddit name is invalid")
	}
	return readPublic(operation.client, call, branches, "/r/"+input.Subreddit+"/about/rules?raw_json=1", true, func(body []byte) (SubredditRules, error) {
		var response struct {
			Rules []struct {
				ShortName       string `json:"short_name"`
				Description     string `json:"description"`
				Kind            string `json:"kind"`
				ViolationReason string `json:"violation_reason"`
			} `json:"rules"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return SubredditRules{}, err
		}
		if response.Rules == nil {
			return SubredditRules{}, errors.New("Reddit rules are missing")
		}
		rules := SubredditRules{Rules: make([]SubredditRule, 0, len(response.Rules))}
		for _, rule := range response.Rules {
			if rule.ShortName == "" || (rule.Kind != "all" && rule.Kind != "link" && rule.Kind != "comment") {
				return SubredditRules{}, errors.New("Reddit rule fields are invalid")
			}
			rules.Rules = append(rules.Rules, SubredditRule{ShortName: rule.ShortName, Description: rule.Description, Kind: rule.Kind, ViolationReason: rule.ViolationReason})
		}
		return rules, nil
	})
}

// Invoke searches GET /subreddits/search with the existing read scope.
func (operation SearchSubredditsOperation) Invoke(call sdkgo.Call, input SearchSubredditsInput) sdkgo.QueryAttempt[SubredditPage] {
	branches := readBranches{operation: "searchSubreddits", success: SearchSubredditsBranchSearched, rejected: SearchSubredditsBranchProviderRejected, invalid: SearchSubredditsBranchInvalidResponse, defect: SearchSubredditsBranchDefect}
	if strings.TrimSpace(input.Query) == "" || len(input.Query) > 512 {
		return failedRead[SubredditPage](branches, branches.defect, sdkgo.FailureValidation, "Reddit search query is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 25
	}
	if input.Limit < 1 || input.Limit > 100 || (input.After != "" && !strings.HasPrefix(input.After, "t5_")) {
		return failedRead[SubredditPage](branches, branches.defect, sdkgo.FailureValidation, "Reddit search limit or cursor is invalid")
	}
	query := url.Values{"q": {input.Query}, "limit": {strconv.Itoa(input.Limit)}, "raw_json": {"1"}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	return readPublic(operation.client, call, branches, "/subreddits/search?"+query.Encode(), false, func(body []byte) (SubredditPage, error) {
		var envelope struct {
			Kind string `json:"kind"`
			Data struct {
				After    string `json:"after"`
				Children []struct {
					Kind string            `json:"kind"`
					Data subredditResponse `json:"data"`
				} `json:"children"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return SubredditPage{}, err
		}
		if envelope.Kind != "Listing" || envelope.Data.Children == nil || len(envelope.Data.Children) > input.Limit || (envelope.Data.After != "" && !strings.HasPrefix(envelope.Data.After, "t5_")) {
			return SubredditPage{}, errors.New("Reddit subreddit listing is invalid")
		}
		page := SubredditPage{Subreddits: make([]Subreddit, 0, len(envelope.Data.Children)), After: envelope.Data.After}
		for _, child := range envelope.Data.Children {
			if child.Kind != "t5" {
				return SubredditPage{}, errors.New("Reddit subreddit listing has a non-community item")
			}
			subreddit, err := child.Data.convert()
			if err != nil {
				return SubredditPage{}, err
			}
			page.Subreddits = append(page.Subreddits, subreddit)
		}
		return page, nil
	})
}

// Invoke lists GET /subreddits/{directory} with the existing read scope.
func (operation ListSubredditsOperation) Invoke(call sdkgo.Call, input ListSubredditsInput) sdkgo.QueryAttempt[SubredditPage] {
	branches := readBranches{operation: "listSubreddits", success: ListSubredditsBranchListed, rejected: ListSubredditsBranchProviderRejected, invalid: ListSubredditsBranchInvalidResponse, defect: ListSubredditsBranchDefect}
	if input.Directory == "" {
		input.Directory = "popular"
	}
	if input.Directory != "popular" && input.Directory != "new" && input.Directory != "default" {
		return failedRead[SubredditPage](branches, branches.defect, sdkgo.FailureValidation, "Reddit directory is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 25
	}
	if input.Limit < 1 || input.Limit > 100 || (input.After != "" && !strings.HasPrefix(input.After, "t5_")) {
		return failedRead[SubredditPage](branches, branches.defect, sdkgo.FailureValidation, "Reddit directory limit or cursor is invalid")
	}
	query := url.Values{"limit": {strconv.Itoa(input.Limit)}, "raw_json": {"1"}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	return readPublic(operation.client, call, branches, "/subreddits/"+input.Directory+"?"+query.Encode(), false, func(body []byte) (SubredditPage, error) {
		var envelope struct {
			Kind string `json:"kind"`
			Data struct {
				After    string `json:"after"`
				Children []struct {
					Kind string            `json:"kind"`
					Data subredditResponse `json:"data"`
				} `json:"children"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return SubredditPage{}, err
		}
		if envelope.Kind != "Listing" || envelope.Data.Children == nil || len(envelope.Data.Children) > input.Limit || (envelope.Data.After != "" && !strings.HasPrefix(envelope.Data.After, "t5_")) {
			return SubredditPage{}, errors.New("Reddit subreddit directory is invalid")
		}
		page := SubredditPage{Subreddits: make([]Subreddit, 0, len(envelope.Data.Children)), After: envelope.Data.After}
		for _, child := range envelope.Data.Children {
			if child.Kind != "t5" {
				return SubredditPage{}, errors.New("Reddit directory has a non-community item")
			}
			subreddit, err := child.Data.convert()
			if err != nil {
				return SubredditPage{}, err
			}
			page.Subreddits = append(page.Subreddits, subreddit)
		}
		return page, nil
	})
}

// Invoke reads GET /comments/{post} with depth one and a bounded limit.
func (operation ListPostCommentsOperation) Invoke(call sdkgo.Call, input ListPostCommentsInput) sdkgo.QueryAttempt[CommentPage] {
	branches := readBranches{operation: "listPostComments", success: ListPostCommentsBranchListed, missing: ListPostCommentsBranchPostUnavailable, rejected: ListPostCommentsBranchProviderRejected, invalid: ListPostCommentsBranchInvalidResponse, defect: ListPostCommentsBranchDefect}
	if !postIDPattern.MatchString(input.PostID) {
		return failedRead[CommentPage](branches, branches.defect, sdkgo.FailureValidation, "Reddit post ID is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 25
	}
	if input.Limit < 1 || input.Limit > 100 {
		return failedRead[CommentPage](branches, branches.defect, sdkgo.FailureValidation, "Reddit comment limit is invalid")
	}
	query := url.Values{"depth": {"1"}, "limit": {strconv.Itoa(input.Limit)}, "raw_json": {"1"}}
	return readPublic(operation.client, call, branches, "/comments/"+strings.TrimPrefix(input.PostID, "t3_")+"?"+query.Encode(), false, func(body []byte) (CommentPage, error) {
		var listings []struct {
			Kind string `json:"kind"`
			Data struct {
				Children []struct {
					Kind string `json:"kind"`
					Data struct {
						Name      string `json:"name"`
						ParentID  string `json:"parent_id"`
						Author    string `json:"author"`
						Body      string `json:"body"`
						Permalink string `json:"permalink"`
						Score     int64  `json:"score"`
					} `json:"data"`
				} `json:"children"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &listings); err != nil {
			return CommentPage{}, err
		}
		if len(listings) != 2 || listings[0].Kind != "Listing" || listings[1].Kind != "Listing" || listings[1].Data.Children == nil || len(listings[1].Data.Children) > input.Limit {
			return CommentPage{}, errors.New("Reddit comment listing is invalid")
		}
		page := CommentPage{Comments: make([]Comment, 0, len(listings[1].Data.Children))}
		for _, child := range listings[1].Data.Children {
			if child.Kind == "more" {
				continue
			}
			data := child.Data
			if child.Kind != "t1" || !strings.HasPrefix(data.Name, "t1_") || data.ParentID != input.PostID || !strings.HasPrefix(data.Permalink, "/r/") {
				return CommentPage{}, errors.New("Reddit comment fields are invalid")
			}
			page.Comments = append(page.Comments, Comment{ID: data.Name, ParentID: data.ParentID, Author: data.Author, Body: data.Body, Permalink: redditWebOrigin + data.Permalink, Score: data.Score})
		}
		return page, nil
	})
}

type subredditResponse struct {
	ID          string `json:"name"`
	Name        string `json:"display_name"`
	Title       string `json:"title"`
	Description string `json:"public_description"`
	Subscribers int64  `json:"subscribers"`
	Path        string `json:"url"`
}

func (response subredditResponse) convert() (Subreddit, error) {
	if !strings.HasPrefix(response.ID, "t5_") || response.Name == "" || !strings.HasPrefix(response.Path, "/r/") || strings.ContainsAny(response.Path, "\x00\r\n") {
		return Subreddit{}, errors.New("Reddit subreddit data is invalid")
	}
	return Subreddit{Name: response.Name, ID: response.ID, Title: response.Title, Description: response.Description, Subscribers: response.Subscribers, URL: redditWebOrigin + response.Path}, nil
}

type readBranches struct {
	operation                                   string
	success, missing, rejected, invalid, defect sdkgo.BranchID
}

func readPublic[T any](client *Client, call sdkgo.Call, branches readBranches, path string, isSubreddit bool, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	response := client.get(call, path)
	if errors.Is(response.err, errCredential) {
		return failedRead[T](branches, branches.defect, sdkgo.FailureAuthentication, "Reddit OAuth credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return retryRead[T](branches, sdkgo.FailureTransport, "Reddit request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return retryRead[T](branches, sdkgo.FailureRateLimit, "Reddit rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return retryRead[T](branches, sdkgo.FailureAvailability, "Reddit is temporarily unavailable", response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return failedRead[T](branches, branches.invalid, sdkgo.FailureResponseTooLarge, "Reddit response exceeded the configured limit")
	}
	if response.err != nil {
		return retryRead[T](branches, sdkgo.FailureTransport, "Reddit response was interrupted", 0)
	}
	if isSubreddit {
		if kind, unavailable := subredditUnavailableFailureKind(response); unavailable {
			return failedRead[T](branches, branches.missing, kind, "Reddit subreddit is unavailable")
		}
	}
	if response.status == http.StatusNotFound && branches.missing != "" {
		return failedRead[T](branches, branches.missing, sdkgo.FailureNotFound, "Reddit resource was not found")
	}
	if response.status != http.StatusOK {
		return failedRead[T](branches, branches.rejected, sdkgo.FailureProviderRejection, "Reddit rejected HTTP "+strconv.Itoa(response.status))
	}
	value, err := decode(response.body)
	if err != nil {
		return failedRead[T](branches, branches.invalid, sdkgo.FailureProtocol, "Reddit returned invalid "+branches.operation+" JSON")
	}
	return sdkgo.NewQueryBranch(branches.success, value, nil, sdkgo.Receipt{})
}

func failedRead[T any](branches readBranches, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "reddit", Operation: branches.operation, Message: message}
	var value T
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func retryRead[T any](branches readBranches, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "reddit", Operation: branches.operation, Message: message}, delay)
}
