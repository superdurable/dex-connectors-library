// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reddit

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// DefaultPostPageLimit is the number of posts a page holds when Limit is blank or zero, as Reddit defaults.
	DefaultPostPageLimit = 25
	// MaxPostPageLimit is the most posts Reddit returns on one listing or search page.
	MaxPostPageLimit = 100
	// MaxSubredditsPerListing is the most subreddit names one ListSubredditPostsInput may join with +.
	MaxSubredditsPerListing = 10
	// MaxSearchQueryCharacters is Reddit's documented search query limit, counted in Unicode code points.
	MaxSearchQueryCharacters = 512
	// MaxSelfTextBytes bounds each ListedPost.SelfText so durable Step results stay small. Longer text is cut
	// at a UTF-8 boundary and IsSelfTextTruncated is set.
	MaxSelfTextBytes = 4 << 10
)

// redditWebOrigin hosts post pages; the configured API origin serves only OAuth API requests.
const redditWebOrigin = "https://www.reddit.com"

// latestRepresentableUnixSecond is 9999-12-31T23:59:59Z, the last instant RFC 3339 can carry.
const latestRepresentableUnixSecond = 253402300799

var subredditNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_]{1,20}$`)

// subredditAccessReasons are Reddit's 403 reason tokens for a community the account may not read.
var subredditAccessReasons = map[string]bool{"private": true, "quarantined": true, "gated": true, "gold_only": true, "banned": true}

// SubredditPostSort orders a subreddit listing.
type SubredditPostSort string

const (
	// SubredditPostSortNew lists the newest posts first.
	SubredditPostSortNew SubredditPostSort = "new"
	// SubredditPostSortHot lists posts by Reddit's current activity ranking.
	SubredditPostSortHot SubredditPostSort = "hot"
	// SubredditPostSortTop lists the highest-scoring posts created within a PostTimeWindow.
	SubredditPostSortTop SubredditPostSort = "top"
	// SubredditPostSortRising lists recent posts that are quickly gaining votes and comments.
	SubredditPostSortRising SubredditPostSort = "rising"
)

// SearchPostSort orders Reddit search results.
type SearchPostSort string

const (
	// SearchPostSortNew returns the newest matching posts first.
	SearchPostSortNew SearchPostSort = "new"
	// SearchPostSortRelevance returns the posts Reddit ranks most relevant to the query first.
	SearchPostSortRelevance SearchPostSort = "relevance"
	// SearchPostSortTop returns the highest-scoring matching posts first.
	SearchPostSortTop SearchPostSort = "top"
	// SearchPostSortHot returns matching posts by Reddit's current activity ranking.
	SearchPostSortHot SearchPostSort = "hot"
	// SearchPostSortComments returns the matching posts with the most comments first.
	SearchPostSortComments SearchPostSort = "comments"
)

// PostTimeWindow limits a top listing or a search to posts created within a recent period.
type PostTimeWindow string

const (
	// PostTimeWindowHour keeps posts created in the last hour.
	PostTimeWindowHour PostTimeWindow = "hour"
	// PostTimeWindowDay keeps posts created in the last day.
	PostTimeWindowDay PostTimeWindow = "day"
	// PostTimeWindowWeek keeps posts created in the last week.
	PostTimeWindowWeek PostTimeWindow = "week"
	// PostTimeWindowMonth keeps posts created in the last month.
	PostTimeWindowMonth PostTimeWindow = "month"
	// PostTimeWindowYear keeps posts created in the last year.
	PostTimeWindowYear PostTimeWindow = "year"
	// PostTimeWindowAll keeps posts of any age.
	PostTimeWindowAll PostTimeWindow = "all"
)

// ListSubredditPostsInput selects one page of posts from one subreddit or several joined with +.
type ListSubredditPostsInput struct {
	// Subreddit is one community name without r/, such as golang, or up to 10 names joined with +, such as
	// golang+programming. Each name has 2 to 21 letters, digits, or underscores and does not start with an underscore.
	Subreddit string `json:"subreddit"`
	// Sort orders the listing: new, hot, top, or rising. Blank lists new posts, newest first.
	Sort SubredditPostSort `json:"sort,omitempty"`
	// TimeWindow limits a top listing to posts from the last hour, day, week, month, or year, or all time.
	// Only the top sort accepts it; blank then reads the last day. Every other sort requires it blank.
	TimeWindow PostTimeWindow `json:"timeWindow,omitempty"`
	// Limit is the most posts on the page, 1 through 100; blank or 0 reads 25. Reddit may return fewer posts
	// while After still continues the listing.
	Limit int `json:"limit,omitempty"`
	// After is the previous page's After cursor, a full t3_ post ID. Blank reads the first page.
	After string `json:"after,omitempty"`
}

// SearchPostsInput selects one page of Reddit search results for posts.
type SearchPostsInput struct {
	// Query is the search text Reddit matches against posts, such as generics, at most 512 characters.
	// It is sent to Reddit unchanged and must not be blank.
	Query string `json:"query"`
	// Subreddit restricts the search to one community, named without r/, such as golang. Blank searches all
	// of Reddit.
	Subreddit string `json:"subreddit,omitempty"`
	// Sort orders the results: new, relevance, top, hot, or comments, which puts the most commented posts
	// first. Blank returns the newest matching posts first.
	Sort SearchPostSort `json:"sort,omitempty"`
	// TimeWindow limits the results to posts from the last hour, day, week, month, or year, or all time.
	// Blank searches all time.
	TimeWindow PostTimeWindow `json:"timeWindow,omitempty"`
	// Limit is the most posts on the page, 1 through 100; blank or 0 reads 25. Reddit may return fewer posts
	// while After still continues the results.
	Limit int `json:"limit,omitempty"`
	// After is the previous page's After cursor, a full t3_ post ID. Blank reads the first page.
	After string `json:"after,omitempty"`
}

// PostPage is one page of posts from a subreddit listing or a search.
type PostPage struct {
	// Posts are the page's posts in Reddit's order for the requested sort. A post can appear on two pages
	// when the listing changes between reads, so deduplicate by ID.
	Posts []ListedPost `json:"posts"`
	// After continues to the next page when non-blank: pass it as the next input's After. Blank means
	// Reddit returned the last page.
	After string `json:"after,omitempty"`
}

// ListedPost contains bounded public metadata for one post on a listing or search page.
type ListedPost struct {
	// ID is the full t3_ post ID, which getPost also accepts.
	ID string `json:"id"`
	// Title is the post's current title.
	Title string `json:"title"`
	// Subreddit is the community name without r/.
	Subreddit string `json:"subreddit"`
	// Author is the poster's username without /u/, or [deleted] after the account was deleted.
	Author string `json:"author"`
	// Permalink is the absolute https://www.reddit.com URL of the post's comments page.
	Permalink string `json:"permalink"`
	// LinkURL is the absolute URL a link post points to, such as an article or an image. It is empty for a
	// self post and when Reddit's value is not an HTTP or HTTPS URL.
	LinkURL string `json:"linkUrl,omitempty"`
	// IsSelf is true for a text post, whose body is SelfText, and false for a link post.
	IsSelf bool `json:"isSelf"`
	// SelfText is a text post's body as plain text in the author's Markdown, never HTML, cut to at most
	// MaxSelfTextBytes. It is empty for link posts and empty text posts.
	SelfText string `json:"selfText,omitempty"`
	// IsSelfTextTruncated reports that SelfText was cut at MaxSelfTextBytes.
	IsSelfTextTruncated bool `json:"isSelfTextTruncated,omitempty"`
	// Score is the current vote score, which may change.
	Score int64 `json:"score"`
	// CommentCount is the current number of comments, which may change.
	CommentCount int64 `json:"commentCount"`
	// CreatedAt is when the post was created, in UTC to the second.
	CreatedAt time.Time `json:"createdAt"`
	// IsOver18 is true when the post is marked NSFW, for adults only.
	IsOver18 bool `json:"isOver18"`
	// IsSpoiler is true when the post is marked as a spoiler.
	IsSpoiler bool `json:"isSpoiler"`
	// IsStickied is true when moderators pinned the post to the top of its subreddit.
	IsStickied bool `json:"isStickied"`
}

// ListSubredditPostsOperation reads one page of an approved Data API subreddit listing.
type ListSubredditPostsOperation struct{ client *Client }

// ListSubredditPosts binds the listing Query to this client.
func (client *Client) ListSubredditPosts() ListSubredditPostsOperation {
	return ListSubredditPostsOperation{client}
}

// Definition returns the generated subreddit listing Query contract.
func (ListSubredditPostsOperation) Definition() sdkgo.QueryDefinition {
	return ListSubredditPostsDefinition
}

// Invoke reads GET /r/{subreddit}/{sort} and classifies the provider outcome.
func (operation ListSubredditPostsOperation) Invoke(call sdkgo.Call, input ListSubredditPostsInput) sdkgo.QueryAttempt[PostPage] {
	path, query, err := subredditListingRequest(input)
	if err != nil {
		return listSubredditPostsBranches.selectFailure(ListSubredditPostsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return operation.client.readPostPage(call, listSubredditPostsBranches, path+"?"+query.Encode(), true)
}

// SearchPostsOperation reads one page of approved Data API post search results.
type SearchPostsOperation struct{ client *Client }

// SearchPosts binds the search Query to this client.
func (client *Client) SearchPosts() SearchPostsOperation { return SearchPostsOperation{client} }

// Definition returns the generated post search Query contract.
func (SearchPostsOperation) Definition() sdkgo.QueryDefinition { return SearchPostsDefinition }

// Invoke reads GET /search, or /r/{subreddit}/search restricted to that subreddit, and classifies the outcome.
func (operation SearchPostsOperation) Invoke(call sdkgo.Call, input SearchPostsInput) sdkgo.QueryAttempt[PostPage] {
	path, query, err := postSearchRequest(input)
	if err != nil {
		return searchPostsBranches.selectFailure(SearchPostsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return operation.client.readPostPage(call, searchPostsBranches, path+"?"+query.Encode(), input.Subreddit != "")
}

// postPageBranches names one page operation's branches for the shared response classification.
type postPageBranches struct {
	operation            string
	succeeded            sdkgo.BranchID
	subredditUnavailable sdkgo.BranchID
	providerRejected     sdkgo.BranchID
	invalidResponse      sdkgo.BranchID
	defect               sdkgo.BranchID
}

var listSubredditPostsBranches = postPageBranches{
	operation: "listSubredditPosts", succeeded: ListSubredditPostsBranchListed,
	subredditUnavailable: ListSubredditPostsBranchSubredditUnavailable, providerRejected: ListSubredditPostsBranchProviderRejected,
	invalidResponse: ListSubredditPostsBranchInvalidResponse, defect: ListSubredditPostsBranchDefect,
}

var searchPostsBranches = postPageBranches{
	operation: "searchPosts", succeeded: SearchPostsBranchSearched,
	subredditUnavailable: SearchPostsBranchSubredditUnavailable, providerRejected: SearchPostsBranchProviderRejected,
	invalidResponse: SearchPostsBranchInvalidResponse, defect: SearchPostsBranchDefect,
}

// readPostPage sends one listing or search request and selects a branch or Retry from the response.
func (client *Client) readPostPage(call sdkgo.Call, branches postPageBranches, pathAndQuery string, isSubredditScoped bool) sdkgo.QueryAttempt[PostPage] {
	response := client.get(call, pathAndQuery)
	if errors.Is(response.err, errCredential) {
		return branches.selectFailure(branches.defect, sdkgo.FailureAuthentication, "Reddit OAuth credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return branches.retry(sdkgo.FailureTransport, "Reddit request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return branches.retry(sdkgo.FailureRateLimit, "Reddit rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return branches.retry(sdkgo.FailureAvailability, "Reddit is temporarily unavailable", response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return branches.selectFailure(branches.invalidResponse, sdkgo.FailureResponseTooLarge, "Reddit response exceeded the configured limit")
	}
	if response.err != nil {
		return branches.retry(sdkgo.FailureTransport, "Reddit response was interrupted", 0)
	}
	if isSubredditScoped {
		if kind, isUnavailable := subredditUnavailableFailureKind(response); isUnavailable {
			return branches.selectFailure(branches.subredditUnavailable, kind, "Reddit reports the subreddit missing, banned, private, or restricted")
		}
	}
	if response.status != http.StatusOK {
		return branches.selectFailure(branches.providerRejected, sdkgo.FailureProviderRejection, "Reddit rejected HTTP "+strconv.Itoa(response.status))
	}
	page, err := decodePostPage(response.body)
	if err != nil {
		return branches.selectFailure(branches.invalidResponse, sdkgo.FailureProtocol, "Reddit returned an invalid post listing")
	}
	return sdkgo.NewQueryBranch(branches.succeeded, page, nil, sdkgo.Receipt{})
}

func (branches postPageBranches) selectFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PostPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "reddit", Operation: branches.operation, Message: message}
	return sdkgo.NewQueryBranch(branch, PostPage{}, &failure, sdkgo.Receipt{})
}

func (branches postPageBranches) retry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[PostPage] {
	return sdkgo.NewQueryRetry[PostPage](sdkgo.Failure{Kind: kind, Provider: "reddit", Operation: branches.operation, Message: message}, delay)
}

// subredditUnavailableFailureKind recognizes Reddit's unknown-name redirect, banned 404, and access-reason 403.
func subredditUnavailableFailureKind(response providerResponse) (sdkgo.FailureKind, bool) {
	switch {
	case response.status >= 300 && response.status < 400 && isSubredditSearchRedirect(response.location):
		return sdkgo.FailureNotFound, true
	case response.status == http.StatusNotFound:
		return sdkgo.FailureNotFound, true
	case response.status == http.StatusForbidden:
		for _, reason := range providerhttp.ReadErrorTokens(response.body, []string{"/reason"}) {
			if subredditAccessReasons[reason] {
				return sdkgo.FailureAuthorization, true
			}
		}
	}
	return "", false
}

func isSubredditSearchRedirect(location string) bool {
	parsed, err := url.Parse(location)
	return err == nil && strings.HasPrefix(parsed.Path, "/subreddits/search")
}

func subredditListingRequest(input ListSubredditPostsInput) (string, url.Values, error) {
	if !isValidSubredditPath(input.Subreddit, MaxSubredditsPerListing) {
		return "", nil, errors.New("Reddit subreddit must be 1 through 10 community names joined with +, without r/")
	}
	sort := input.Sort
	if sort == "" {
		sort = SubredditPostSortNew
	}
	if sort != SubredditPostSortNew && sort != SubredditPostSortHot && sort != SubredditPostSortTop && sort != SubredditPostSortRising {
		return "", nil, errors.New("Reddit listing sort must be new, hot, top, or rising")
	}
	query, err := postPageQuery(input.Limit, input.After)
	if err != nil {
		return "", nil, err
	}
	if sort == SubredditPostSortTop {
		timeWindow := input.TimeWindow
		if timeWindow == "" {
			timeWindow = PostTimeWindowDay
		}
		if !isKnownPostTimeWindow(timeWindow) {
			return "", nil, errors.New("Reddit time window must be hour, day, week, month, year, or all")
		}
		query.Set("t", string(timeWindow))
	} else if input.TimeWindow != "" {
		return "", nil, errors.New("Reddit listing time window applies only to the top sort")
	}
	return "/r/" + input.Subreddit + "/" + string(sort), query, nil
}

func postSearchRequest(input SearchPostsInput) (string, url.Values, error) {
	if strings.TrimSpace(input.Query) == "" || !utf8.ValidString(input.Query) || utf8.RuneCountInString(input.Query) > MaxSearchQueryCharacters {
		return "", nil, errors.New("Reddit search query must be non-blank UTF-8 text of at most 512 characters")
	}
	if input.Subreddit != "" && !isValidSubredditPath(input.Subreddit, 1) {
		return "", nil, errors.New("Reddit search subreddit must be one community name without r/")
	}
	sort := input.Sort
	if sort == "" {
		sort = SearchPostSortNew
	}
	if sort != SearchPostSortNew && sort != SearchPostSortRelevance && sort != SearchPostSortTop && sort != SearchPostSortHot && sort != SearchPostSortComments {
		return "", nil, errors.New("Reddit search sort must be new, relevance, top, hot, or comments")
	}
	timeWindow := input.TimeWindow
	if timeWindow == "" {
		timeWindow = PostTimeWindowAll
	}
	if !isKnownPostTimeWindow(timeWindow) {
		return "", nil, errors.New("Reddit time window must be hour, day, week, month, year, or all")
	}
	query, err := postPageQuery(input.Limit, input.After)
	if err != nil {
		return "", nil, err
	}
	query.Set("q", input.Query)
	query.Set("type", "link")
	query.Set("sort", string(sort))
	query.Set("t", string(timeWindow))
	if input.Subreddit == "" {
		return "/search", query, nil
	}
	query.Set("restrict_sr", "true")
	return "/r/" + input.Subreddit + "/search", query, nil
}

// postPageQuery validates the shared page bounds and cursor and returns their query parameters.
func postPageQuery(limit int, after string) (url.Values, error) {
	if limit < 0 || limit > MaxPostPageLimit {
		return nil, errors.New("Reddit page limit must be 1 through 100; blank or 0 reads 25")
	}
	if limit == 0 {
		limit = DefaultPostPageLimit
	}
	if after != "" && !postIDPattern.MatchString(after) {
		return nil, errors.New("Reddit after cursor must be a full t3_ post ID from the previous page")
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}, "raw_json": {"1"}}
	if after != "" {
		query.Set("after", after)
	}
	return query, nil
}

func isValidSubredditPath(value string, maximumNames int) bool {
	names := strings.Split(value, "+")
	if len(names) > maximumNames {
		return false
	}
	for _, name := range names {
		if !subredditNamePattern.MatchString(name) {
			return false
		}
	}
	return true
}

func isKnownPostTimeWindow(timeWindow PostTimeWindow) bool {
	switch timeWindow {
	case PostTimeWindowHour, PostTimeWindowDay, PostTimeWindowWeek, PostTimeWindowMonth, PostTimeWindowYear, PostTimeWindowAll:
		return true
	}
	return false
}

type redditPostListing struct {
	Kind string `json:"kind"`
	Data struct {
		After    *string `json:"after"`
		Children []struct {
			Kind string               `json:"kind"`
			Data redditListedPostData `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

type redditListedPostData struct {
	Name         string  `json:"name"`
	Title        string  `json:"title"`
	Subreddit    string  `json:"subreddit"`
	Author       string  `json:"author"`
	Permalink    string  `json:"permalink"`
	URL          string  `json:"url"`
	IsSelf       bool    `json:"is_self"`
	SelfText     string  `json:"selftext"`
	Score        int64   `json:"score"`
	CommentCount int64   `json:"num_comments"`
	CreatedUTC   float64 `json:"created_utc"`
	IsOver18     bool    `json:"over_18"`
	IsSpoiler    bool    `json:"spoiler"`
	IsStickied   bool    `json:"stickied"`
}

func decodePostPage(body []byte) (PostPage, error) {
	var listing redditPostListing
	if err := json.Unmarshal(body, &listing); err != nil {
		return PostPage{}, err
	}
	if listing.Kind != "Listing" || listing.Data.Children == nil || len(listing.Data.Children) > MaxPostPageLimit {
		return PostPage{}, errors.New("Reddit listing shape is invalid")
	}
	page := PostPage{Posts: make([]ListedPost, 0, len(listing.Data.Children))}
	if listing.Data.After != nil && *listing.Data.After != "" {
		if !postIDPattern.MatchString(*listing.Data.After) {
			return PostPage{}, errors.New("Reddit listing cursor is invalid")
		}
		page.After = *listing.Data.After
	}
	for _, child := range listing.Data.Children {
		if child.Kind != "t3" {
			return PostPage{}, errors.New("Reddit listing contains a non-post item")
		}
		post, err := convertListedPost(child.Data)
		if err != nil {
			return PostPage{}, err
		}
		page.Posts = append(page.Posts, post)
	}
	return page, nil
}

func convertListedPost(data redditListedPostData) (ListedPost, error) {
	if !postIDPattern.MatchString(data.Name) || data.Title == "" || data.Subreddit == "" ||
		!strings.HasPrefix(data.Permalink, "/r/") || strings.ContainsAny(data.Permalink, "\x00\r\n") {
		return ListedPost{}, errors.New("Reddit post fields are invalid")
	}
	if math.IsNaN(data.CreatedUTC) || data.CreatedUTC <= 0 || data.CreatedUTC > latestRepresentableUnixSecond {
		return ListedPost{}, errors.New("Reddit post creation time is invalid")
	}
	selfText, isSelfTextTruncated := truncateUTF8(data.SelfText, MaxSelfTextBytes)
	return ListedPost{
		ID: data.Name, Title: data.Title, Subreddit: data.Subreddit, Author: data.Author,
		Permalink: redditWebOrigin + data.Permalink, LinkURL: linkedURL(data), IsSelf: data.IsSelf,
		SelfText: selfText, IsSelfTextTruncated: isSelfTextTruncated,
		Score: data.Score, CommentCount: data.CommentCount, CreatedAt: time.Unix(int64(data.CreatedUTC), 0).UTC(),
		IsOver18: data.IsOver18, IsSpoiler: data.IsSpoiler, IsStickied: data.IsStickied,
	}, nil
}

// linkedURL returns a link post's absolute HTTP(S) target; a crosspost's relative Reddit path joins the web origin.
func linkedURL(data redditListedPostData) string {
	if data.IsSelf || data.URL == "" || strings.ContainsAny(data.URL, "\x00\r\n\t ") {
		return ""
	}
	target := data.URL
	if strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "//") {
		target = redditWebOrigin + target
	}
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return ""
	}
	return target
}

func truncateUTF8(value string, maxBytes int) (string, bool) {
	if len(value) <= maxBytes {
		return value, false
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut], true
}
