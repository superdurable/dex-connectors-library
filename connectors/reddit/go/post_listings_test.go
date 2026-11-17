// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reddit

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestListSubredditPostsSendsBoundedListingRequestAndProjectsPosts(t *testing.T) {
	longSelfText := strings.Repeat("é", MaxSelfTextBytes) // two bytes per character, so the cut lands mid-text
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/r/golang+programming/top", r.URL.Path)
		require.Equal(t, url.Values{"limit": {"50"}, "t": {"week"}, "after": {"t3_prev01"}, "raw_json": {"1"}}, r.URL.Query())
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		require.Equal(t, "desktop:client-id:v0.1.0 (by /u/example)", r.Header.Get("User-Agent"))
		writeListing(t, w, "t3_next02",
			postJSON(map[string]any{"name": "t3_self01", "is_self": true, "selftext": longSelfText, "url": "https://www.reddit.com/r/golang/comments/self01/a_question/", "stickied": true, "author_fullname": "t2_not_persisted"}),
			postJSON(map[string]any{"name": "t3_link01", "subreddit": "programming", "url": "https://example.com/article", "over_18": true, "spoiler": true}),
			postJSON(map[string]any{"name": "t3_cross1", "url": "/r/golang/comments/orig01/original_post/"}),
			postJSON(map[string]any{"name": "t3_odd001", "url": "javascript:alert(1)"}),
		)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListSubredditPosts(), testConnection, ListSubredditPostsInput{
		Subreddit: "golang+programming", Sort: SubredditPostSortTop, TimeWindow: PostTimeWindowWeek, Limit: 50, After: "t3_prev01",
	})
	require.NoError(t, err)
	require.Equal(t, ListSubredditPostsBranchListed, result.Branch)
	require.Nil(t, result.Failure)
	page := result.Value
	require.Equal(t, "t3_next02", page.After)
	require.Len(t, page.Posts, 4)
	selfPost := page.Posts[0]
	require.Equal(t, "t3_self01", selfPost.ID)
	require.Equal(t, "https://www.reddit.com/r/golang/comments/self01/example_title/", selfPost.Permalink)
	require.True(t, selfPost.IsSelf)
	require.Empty(t, selfPost.LinkURL)
	require.True(t, selfPost.IsSelfTextTruncated)
	require.Equal(t, strings.Repeat("é", MaxSelfTextBytes/2), selfPost.SelfText)
	require.True(t, selfPost.IsStickied)
	require.Equal(t, time.Date(2026, 11, 12, 16, 30, 0, 0, time.UTC), selfPost.CreatedAt)
	require.Equal(t, ListedPost{
		ID: "t3_link01", Title: "Example title", Subreddit: "programming", Author: "example_author",
		Permalink: "https://www.reddit.com/r/golang/comments/link01/example_title/", LinkURL: "https://example.com/article",
		Score: 42, CommentCount: 7, CreatedAt: time.Date(2026, 11, 12, 16, 30, 0, 0, time.UTC), IsOver18: true, IsSpoiler: true,
	}, page.Posts[1])
	require.Equal(t, "https://www.reddit.com/r/golang/comments/orig01/original_post/", page.Posts[2].LinkURL)
	require.Empty(t, page.Posts[3].LinkURL)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not_persisted")
	require.NotContains(t, string(encoded), testAccessToken)
	require.Contains(t, string(encoded), `"createdAt":"2026-11-12T16:30:00Z"`)
}

func TestListSubredditPostsDefaultsToNewestPageWithoutCursor(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		input ListSubredditPostsInput
		path  string
		query url.Values
	}{
		{name: "blank sort reads new", input: ListSubredditPostsInput{Subreddit: "golang"}, path: "/r/golang/new", query: url.Values{"limit": {"25"}, "raw_json": {"1"}}},
		{name: "top without a window reads the last day", input: ListSubredditPostsInput{Subreddit: "golang", Sort: SubredditPostSortTop}, path: "/r/golang/top", query: url.Values{"limit": {"25"}, "t": {"day"}, "raw_json": {"1"}}},
		{name: "hot", input: ListSubredditPostsInput{Subreddit: "Go_Lang", Sort: SubredditPostSortHot, Limit: 100}, path: "/r/Go_Lang/hot", query: url.Values{"limit": {"100"}, "raw_json": {"1"}}},
		{name: "rising", input: ListSubredditPostsInput{Subreddit: "de", Sort: SubredditPostSortRising, Limit: 1}, path: "/r/de/rising", query: url.Values{"limit": {"1"}, "raw_json": {"1"}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, testCase.path, r.URL.Path)
				require.Equal(t, testCase.query, r.URL.Query())
				_, err := w.Write([]byte(`{"kind":"Listing","data":{"after":null,"dist":0,"children":[]}}`))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListSubredditPosts(), testConnection, testCase.input)
			require.NoError(t, err)
			require.Equal(t, ListSubredditPostsBranchListed, result.Branch)
			require.Equal(t, PostPage{Posts: []ListedPost{}}, result.Value)
			encoded, err := json.Marshal(result.Value)
			require.NoError(t, err)
			require.JSONEq(t, `{"posts":[]}`, string(encoded))
		})
	}
}

func TestListSubredditPostsFollowsAfterUntilTheListingEnds(t *testing.T) {
	var requestedCursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		after := r.URL.Query().Get("after")
		requestedCursors = append(requestedCursors, after)
		switch after {
		case "":
			writeListing(t, w, "t3_page01", postJSON(map[string]any{"name": "t3_page01"}))
		case "t3_page01":
			writeListing(t, w, "t3_page02", postJSON(map[string]any{"name": "t3_page01"}), postJSON(map[string]any{"name": "t3_page02"}))
		default:
			writeListing(t, w, "", postJSON(map[string]any{"name": "t3_page03"}))
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	seen := map[string]bool{}
	var ids []string
	after := ""
	for range 5 {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListSubredditPosts(), testConnection, ListSubredditPostsInput{Subreddit: "golang", After: after})
		require.NoError(t, err)
		require.Equal(t, ListSubredditPostsBranchListed, result.Branch)
		for _, post := range result.Value.Posts {
			if !seen[post.ID] {
				seen[post.ID] = true
				ids = append(ids, post.ID)
			}
		}
		after = result.Value.After
		if after == "" {
			break
		}
	}
	require.Equal(t, []string{"", "t3_page01", "t3_page02"}, requestedCursors)
	require.Equal(t, []string{"t3_page01", "t3_page02", "t3_page03"}, ids)
}

func TestListSubredditPostsRejectsInvalidInputBeforeProviderCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	for name, input := range map[string]ListSubredditPostsInput{
		"blank subreddit":                {},
		"r/ prefix":                      {Subreddit: "r/golang"},
		"leading underscore":             {Subreddit: "_golang"},
		"one character":                  {Subreddit: "g"},
		"22 characters":                  {Subreddit: strings.Repeat("g", 22)},
		"space":                          {Subreddit: "go lang"},
		"empty name in multi":            {Subreddit: "golang+"},
		"eleven names":                   {Subreddit: strings.TrimSuffix(strings.Repeat("golang+", 11), "+")},
		"unknown sort":                   {Subreddit: "golang", Sort: "controversial"},
		"time window without top":        {Subreddit: "golang", Sort: SubredditPostSortNew, TimeWindow: PostTimeWindowWeek},
		"time window with blank sort":    {Subreddit: "golang", TimeWindow: PostTimeWindowDay},
		"unknown time window":            {Subreddit: "golang", Sort: SubredditPostSortTop, TimeWindow: "decade"},
		"negative limit":                 {Subreddit: "golang", Limit: -1},
		"limit above Reddit's page size": {Subreddit: "golang", Limit: 101},
		"cursor of another kind":         {Subreddit: "golang", After: "t1_comment"},
		"cursor with uppercase":          {Subreddit: "golang", After: "t3_ABC"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(queryContext{}, client.ListSubredditPosts(), testConnection, input)
			require.NoError(t, err)
			require.Equal(t, ListSubredditPostsBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Zero(t, calls.Load())
}

func TestSearchPostsSendsRestrictedAndSiteWideSearches(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		input SearchPostsInput
		path  string
		query url.Values
	}{
		{
			name:  "restricted to one subreddit",
			input: SearchPostsInput{Query: `generics "type sets"`, Subreddit: "golang", Sort: SearchPostSortTop, TimeWindow: PostTimeWindowMonth, Limit: 10, After: "t3_prev01"},
			path:  "/r/golang/search",
			query: url.Values{"q": {`generics "type sets"`}, "restrict_sr": {"true"}, "type": {"link"}, "sort": {"top"}, "t": {"month"}, "limit": {"10"}, "after": {"t3_prev01"}, "raw_json": {"1"}},
		},
		{
			name:  "site-wide defaults",
			input: SearchPostsInput{Query: "generics"},
			path:  "/search",
			query: url.Values{"q": {"generics"}, "type": {"link"}, "sort": {"new"}, "t": {"all"}, "limit": {"25"}, "raw_json": {"1"}},
		},
		{
			name:  "comments sort",
			input: SearchPostsInput{Query: "generics", Sort: SearchPostSortComments, TimeWindow: PostTimeWindowHour},
			path:  "/search",
			query: url.Values{"q": {"generics"}, "type": {"link"}, "sort": {"comments"}, "t": {"hour"}, "limit": {"25"}, "raw_json": {"1"}},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, testCase.path, r.URL.Path)
				require.Equal(t, testCase.query, r.URL.Query())
				require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
				writeListing(t, w, "t3_match01", postJSON(map[string]any{"name": "t3_match01", "title": "Generics in practice"}))
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).SearchPosts(), testConnection, testCase.input)
			require.NoError(t, err)
			require.Equal(t, SearchPostsBranchSearched, result.Branch)
			require.Equal(t, "t3_match01", result.Value.After)
			require.Equal(t, "Generics in practice", result.Value.Posts[0].Title)
		})
	}
}

func TestSearchPostsRejectsInvalidInputBeforeProviderCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	for name, input := range map[string]SearchPostsInput{
		"blank query":            {},
		"whitespace query":       {Query: " \t"},
		"513 characters":         {Query: strings.Repeat("é", MaxSearchQueryCharacters+1)},
		"invalid UTF-8":          {Query: "generics\xff"},
		"multi subreddit":        {Query: "generics", Subreddit: "golang+programming"},
		"invalid subreddit":      {Query: "generics", Subreddit: "r/golang"},
		"unknown sort":           {Query: "generics", Sort: "oldest"},
		"unknown time window":    {Query: "generics", TimeWindow: "decade"},
		"limit above page size":  {Query: "generics", Limit: 101},
		"cursor of another kind": {Query: "generics", After: "t5_subreddit"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(queryContext{}, client.SearchPosts(), testConnection, input)
			require.NoError(t, err)
			require.Equal(t, SearchPostsBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	result, err := sdkgo.RunQuery(queryContext{}, client.SearchPosts(), testConnection, SearchPostsInput{Query: strings.Repeat("é", MaxSearchQueryCharacters)})
	require.NoError(t, err)
	require.NotEqual(t, SearchPostsBranchDefect, result.Branch)
	require.Equal(t, int32(1), calls.Load())
}

func TestPostPagesClassifyUnavailableSubredditsAndRejections(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		status   int
		location string
		body     string
		search   SearchPostsInput
		listing  ListSubredditPostsInput
		branch   sdkgo.BranchID
		kind     sdkgo.FailureKind
	}{
		{name: "unknown name redirects to subreddit search", status: http.StatusFound, location: "https://oauth.reddit.com/subreddits/search.json?q=nosuchcommunity", branch: ListSubredditPostsBranchSubredditUnavailable, kind: sdkgo.FailureNotFound},
		{name: "relative redirect to subreddit search", status: http.StatusMovedPermanently, location: "/subreddits/search?q=nosuchcommunity", branch: ListSubredditPostsBranchSubredditUnavailable, kind: sdkgo.FailureNotFound},
		{name: "banned", status: http.StatusNotFound, body: `{"reason":"banned","message":"Not Found","error":404}`, branch: ListSubredditPostsBranchSubredditUnavailable, kind: sdkgo.FailureNotFound},
		{name: "private", status: http.StatusForbidden, body: `{"reason":"private","message":"Forbidden","error":403}`, branch: ListSubredditPostsBranchSubredditUnavailable, kind: sdkgo.FailureAuthorization},
		{name: "quarantined", status: http.StatusForbidden, body: `{"reason":"quarantined","message":"Forbidden","error":403}`, branch: ListSubredditPostsBranchSubredditUnavailable, kind: sdkgo.FailureAuthorization},
		{name: "forbidden without a reason", status: http.StatusForbidden, body: `{"message":"Forbidden","error":403}`, branch: ListSubredditPostsBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"message":"Unauthorized","error":401}`, branch: ListSubredditPostsBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
		{name: "redirect elsewhere", status: http.StatusFound, location: "https://www.reddit.com/login", branch: ListSubredditPostsBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if testCase.location != "" {
					w.Header().Set("Location", testCase.location)
				}
				w.WriteHeader(testCase.status)
				_, err := w.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL)
			listing, err := sdkgo.RunQuery(queryContext{}, client.ListSubredditPosts(), testConnection, ListSubredditPostsInput{Subreddit: "nosuchcommunity"})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, listing.Branch)
			require.Equal(t, testCase.kind, listing.Failure.Kind)
			require.Equal(t, "listSubredditPosts", listing.Failure.Operation)
			restricted, err := sdkgo.RunQuery(queryContext{}, client.SearchPosts(), testConnection, SearchPostsInput{Query: "generics", Subreddit: "nosuchcommunity"})
			require.NoError(t, err)
			require.Equal(t, string(testCase.branch), string(restricted.Branch))
			require.Equal(t, testCase.kind, restricted.Failure.Kind)
			siteWide, err := sdkgo.RunQuery(queryContext{}, client.SearchPosts(), testConnection, SearchPostsInput{Query: "generics"})
			require.NoError(t, err)
			require.Equal(t, SearchPostsBranchProviderRejected, siteWide.Branch, "a site-wide search names no subreddit")
		})
	}
}

func TestPostPagesRetryTransientFailuresAndHonorRateLimitHeaders(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		status      int
		headers     map[string]string
		failureKind sdkgo.FailureKind
		delay       time.Duration
	}{
		{name: "429 with Retry-After", status: http.StatusTooManyRequests, headers: map[string]string{"Retry-After": "3", "X-Ratelimit-Reset": "120"}, failureKind: sdkgo.FailureRateLimit, delay: 3 * time.Second},
		{name: "429 with only the reset header", status: http.StatusTooManyRequests, headers: map[string]string{"X-Ratelimit-Remaining": "0.0", "X-Ratelimit-Reset": "7"}, failureKind: sdkgo.FailureRateLimit, delay: 7 * time.Second},
		{name: "429 with a fractional reset", status: http.StatusTooManyRequests, headers: map[string]string{"X-Ratelimit-Reset": "7.2"}, failureKind: sdkgo.FailureRateLimit, delay: 8 * time.Second},
		{name: "429 with a reset beyond the cap", status: http.StatusTooManyRequests, headers: map[string]string{"X-Ratelimit-Reset": "999999"}, failureKind: sdkgo.FailureRateLimit, delay: time.Hour},
		{name: "429 with a malformed reset", status: http.StatusTooManyRequests, headers: map[string]string{"X-Ratelimit-Reset": "soon"}, failureKind: sdkgo.FailureRateLimit},
		{name: "429 without headers", status: http.StatusTooManyRequests, failureKind: sdkgo.FailureRateLimit},
		{name: "503 ignores the reset header", status: http.StatusServiceUnavailable, headers: map[string]string{"X-Ratelimit-Reset": "7"}, failureKind: sdkgo.FailureAvailability},
		{name: "500", status: http.StatusInternalServerError, failureKind: sdkgo.FailureAvailability},
		{name: "408", status: http.StatusRequestTimeout, failureKind: sdkgo.FailureAvailability},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				for name, value := range testCase.headers {
					w.Header().Set(name, value)
				}
				w.WriteHeader(testCase.status)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL)
			for operationName, run := range map[string]func() error{
				"listSubredditPosts": func() error {
					_, err := sdkgo.RunQuery(queryContext{}, client.ListSubredditPosts(), testConnection, ListSubredditPostsInput{Subreddit: "golang"})
					return err
				},
				"searchPosts": func() error {
					_, err := sdkgo.RunQuery(queryContext{}, client.SearchPosts(), testConnection, SearchPostsInput{Query: "generics"})
					return err
				},
				"getPost": func() error {
					_, err := sdkgo.RunQuery(queryContext{}, client.GetPost(), testConnection, GetPostInput{PostID: "t3_abc123"})
					return err
				},
			} {
				err := run()
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry, operationName)
				require.Equal(t, testCase.failureKind, retry.Failure.Kind, operationName)
				var delayed *dex.RetryAfterError
				if testCase.delay == 0 {
					require.False(t, errors.As(err, &delayed), operationName)
				} else {
					require.ErrorAs(t, err, &delayed, operationName)
					require.Equal(t, testCase.delay, delayed.After, operationName)
				}
			}
			require.Equal(t, int32(3), requests.Load())
		})
	}
}

func TestPostPagesRetryTransportFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()
	client := newTestClient(t, address)
	_, err := sdkgo.RunQuery(queryContext{}, client.ListSubredditPosts(), testConnection, ListSubredditPostsInput{Subreddit: "golang"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	_, err = sdkgo.RunQuery(queryContext{}, client.SearchPosts(), testConnection, SearchPostsInput{Query: "generics"})
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
}

func TestPostPagesSelectInvalidResponseForMalformedListings(t *testing.T) {
	validPost := postJSON(map[string]any{})
	for name, body := range map[string]string{
		"not JSON":             `<html>busy</html>`,
		"not a listing":        `{"kind":"t3","data":{}}`,
		"null children":        `{"kind":"Listing","data":{"after":null,"children":null}}`,
		"comment child":        `{"kind":"Listing","data":{"children":[{"kind":"t1","data":{"name":"t1_abc"}}]}}`,
		"invalid post ID":      `{"kind":"Listing","data":{"children":[` + postJSON(map[string]any{"name": "t3_UPPER"}) + `]}}`,
		"blank title":          `{"kind":"Listing","data":{"children":[` + postJSON(map[string]any{"title": ""}) + `]}}`,
		"absolute permalink":   `{"kind":"Listing","data":{"children":[` + postJSON(map[string]any{"permalink": "https://evil.example/r/x"}) + `]}}`,
		"zero creation time":   `{"kind":"Listing","data":{"children":[` + postJSON(map[string]any{"created_utc": 0}) + `]}}`,
		"fractional score":     `{"kind":"Listing","data":{"children":[` + postJSON(map[string]any{"score": 1.5}) + `]}}`,
		"invalid after cursor": `{"kind":"Listing","data":{"after":"t5_community","children":[` + validPost + `]}}`,
		"more than 100 posts":  `{"kind":"Listing","data":{"children":[` + strings.TrimSuffix(strings.Repeat(validPost+",", MaxPostPageLimit+1), ",") + `]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, err := w.Write([]byte(body))
				require.NoError(t, err)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL)
			listing, err := sdkgo.RunQuery(queryContext{}, client.ListSubredditPosts(), testConnection, ListSubredditPostsInput{Subreddit: "golang"})
			require.NoError(t, err)
			require.Equal(t, ListSubredditPostsBranchInvalidResponse, listing.Branch)
			require.Equal(t, sdkgo.FailureProtocol, listing.Failure.Kind)
			search, err := sdkgo.RunQuery(queryContext{}, client.SearchPosts(), testConnection, SearchPostsInput{Query: "generics"})
			require.NoError(t, err)
			require.Equal(t, SearchPostsBranchInvalidResponse, search.Branch)
		})
	}
}

func TestPostPagesBoundResponsesAndKeepCredentialsOutOfResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			writeListing(t, w, "", postJSON(map[string]any{"selftext": "echo " + testAccessToken}))
			return
		}
		writeListing(t, w, "", postJSON(map[string]any{"selftext": strings.Repeat("x", 4096)}))
	}))
	defer server.Close()
	credentials := Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}
	client, err := New(Config{BaseURL: server.URL, RedditUsername: "example", MaxResponseBytes: 1024}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	listing, err := sdkgo.RunQuery(queryContext{}, client.ListSubredditPosts(), testConnection, ListSubredditPostsInput{Subreddit: "golang"})
	require.NoError(t, err)
	require.Equal(t, ListSubredditPostsBranchInvalidResponse, listing.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, listing.Failure.Kind)
	search, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).SearchPosts(), testConnection, SearchPostsInput{Query: "generics"})
	require.NoError(t, err)
	require.Equal(t, SearchPostsBranchDefect, search.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, search.Failure.Kind)
	encoded, err := json.Marshal(search)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestRateLimitResetDelay(t *testing.T) {
	for value, expected := range map[string]time.Duration{
		"": 0, "0": 0, "-3": 0, "NaN": 0, "later": 0, " 12 ": 12 * time.Second, "0.4": time.Second, "3600": time.Hour, "+Inf": time.Hour,
	} {
		require.Equal(t, expected, rateLimitResetDelay(value), strconv.Quote(value))
	}
}

// postJSON returns one Reddit t3 child whose data overrides a valid post's fields.
func postJSON(overrides map[string]any) string {
	data := map[string]any{
		"name": "t3_abc123", "title": "Example title", "subreddit": "golang", "author": "example_author",
		"permalink": "/r/golang/comments/abc123/example_title/", "url": "", "is_self": false, "selftext": "",
		"score": 42, "num_comments": 7, "created_utc": 1794501000.0, "over_18": false, "spoiler": false, "stickied": false,
	}
	for key, value := range overrides {
		data[key] = value
	}
	if name, isString := data["name"].(string); isString && strings.HasPrefix(name, "t3_") {
		if _, hasPermalink := overrides["permalink"]; !hasPermalink {
			data["permalink"] = "/r/golang/comments/" + strings.TrimPrefix(name, "t3_") + "/example_title/"
		}
	}
	encoded, err := json.Marshal(map[string]any{"kind": "t3", "data": data})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func writeListing(t *testing.T, w http.ResponseWriter, after string, children ...string) {
	t.Helper()
	afterJSON := "null"
	if after != "" {
		afterJSON = strconv.Quote(after)
	}
	_, err := w.Write([]byte(`{"kind":"Listing","data":{"after":` + afterJSON + `,"dist":` + strconv.Itoa(len(children)) + `,"children":[` + strings.Join(children, ",") + `],"before":null}}`))
	require.NoError(t, err)
}
