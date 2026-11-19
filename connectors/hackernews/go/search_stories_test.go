// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hackernews

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const emptySearchPage = `{"hits":[],"page":0,"nbPages":0,"nbHits":0}`

func TestStorySearchRequestSelectsEndpointAndFilters(t *testing.T) {
	createdAfter := time.Date(2026, time.October, 25, 12, 0, 0, 500_000_000, time.UTC)
	createdBefore := time.Date(2026, time.October, 26, 12, 0, 0, 500_000_000, time.FixedZone("offset", 3600))
	for _, test := range []struct {
		name  string
		input SearchStoriesInput
		path  string
		query url.Values
	}{
		{"defaults", SearchStoriesInput{}, "/search", url.Values{"tags": {"story"}, "page": {"0"}, "hitsPerPage": {"20"}}},
		{"relevance", SearchStoriesInput{Query: "rust compiler", SortBy: StorySearchSortRelevance}, "/search",
			url.Values{"query": {"rust compiler"}, "tags": {"story"}, "page": {"0"}, "hitsPerPage": {"20"}}},
		{"newest show and front page", SearchStoriesInput{
			Query: "postgres", SortBy: StorySearchSortDate, Tags: []StoryTag{StoryTagShowHN, StoryTagStory, StoryTagShowHN, StoryTagFrontPage},
			CreatedAfter: createdAfter, CreatedBefore: createdBefore, MinPoints: 10, Page: 2, HitsPerPage: 100,
		}, "/search_by_date", url.Values{
			"query": {"postgres"}, "tags": {"story,show_hn,front_page"}, "page": {"2"}, "hitsPerPage": {"100"},
			// Fractional bounds round outward to whole seconds, which keeps both comparisons strict.
			"numericFilters": {fmt.Sprintf("created_at_i>%d,created_at_i<%d,points>=10", createdAfter.Unix(), createdBefore.Unix()+1)},
		}},
		{"whole-second bounds", SearchStoriesInput{CreatedAfter: time.Unix(1793016000, 0), CreatedBefore: time.Unix(1793102400, 0), Tags: []StoryTag{StoryTagAskHN}}, "/search",
			url.Values{"tags": {"story,ask_hn"}, "page": {"0"}, "hitsPerPage": {"20"}, "numericFilters": {"created_at_i>1793016000,created_at_i<1793102400"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				require.Equal(t, test.path, r.URL.Path)
				require.Equal(t, test.query, r.URL.Query())
				require.Equal(t, "application/json", r.Header.Get("Accept"))
				require.Empty(t, r.Header.Get("Authorization"))
				_, err := fmt.Fprint(w, strings.Replace(emptySearchPage, `"page":0`, fmt.Sprintf(`"page":%d`, test.input.Page), 1))
				require.NoError(t, err)
			}))
			defer server.Close()
			client, err := New(Config{SearchBaseURL: server.URL}, nil)
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.SearchStories(), testConnection, test.input)
			require.NoError(t, err)
			require.Equal(t, SearchStoriesBranchSearched, result.Branch)
			require.Equal(t, []StorySearchHit{}, result.Value.Stories)
			require.Equal(t, int32(1), requests.Load())
		})
	}
}

func TestStorySearchUsesSearchBaseURLPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/search_by_date", r.URL.Path)
		_, err := fmt.Fprint(w, emptySearchPage)
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{SearchBaseURL: server.URL + "/api/v1/"}, nil)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.SearchStories(), testConnection, SearchStoriesInput{SortBy: StorySearchSortDate})
	require.NoError(t, err)
	require.Equal(t, SearchStoriesBranchSearched, result.Branch)
}

func TestStorySearchDecodesHitsAndPaging(t *testing.T) {
	longText := strings.Repeat("é", MaxStoryTextCharacters+5)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		_, err := fmt.Fprintf(w, `{"hits":[
			{"objectID":"42003","title":"Show HN: A compiler explorer","url":"https://example.com/explorer","author":"langtools","points":12,"num_comments":3,
			 "created_at":"2026-10-26T11:50:00Z","created_at_i":1793015400,"story_text":null,"_tags":["show_hn","author_langtools","story","story_42003"]},
			{"objectID":"41990","title":"Ask HN: How do you test a compiler?","url":null,"author":"testwriter","points":null,"num_comments":null,
			 "created_at_i":1793007000,"story_text":"We fuzz the parser &amp; the <i>optimizer</i>.<p>See <a href=\"https:&#x2F;&#x2F;example.com&#x2F;fuzz\">https:&#x2F;&#x2F;example.com&#x2F;fuzz</a>\n  for details.<p><pre><code>  run   --all\n</code></pre>","_tags":["story","ask_hn","front_page"]},
			{"objectID":"41980","title":"Long notes","created_at_i":1793000100,"story_text":"%s","_tags":null}
		],"page":%s,"nbPages":3,"nbHits":5,"hitsPerPage":3,"exhaustiveNbHits":false}`, longText, page)
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{SearchBaseURL: server.URL}, nil)
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(queryContext{}, client.SearchStories(), testConnection, SearchStoriesInput{Query: "compiler", HitsPerPage: 3})
	require.NoError(t, err)
	require.Equal(t, SearchStoriesBranchSearched, result.Branch)
	require.Equal(t, StorySearchPage{Page: 0, Pages: 3, TotalHits: 5, HitsPerPage: 3, NextPage: 1, Stories: []StorySearchHit{
		{
			ID: 42003, Title: "Show HN: A compiler explorer", URL: "https://example.com/explorer", DiscussionURL: "https://news.ycombinator.com/item?id=42003",
			Author: "langtools", Points: 12, CommentCount: 3, CreatedAt: time.Unix(1793015400, 0).UTC(), Tags: []StoryTag{StoryTagStory, StoryTagShowHN},
		},
		{
			ID: 41990, Title: "Ask HN: How do you test a compiler?", DiscussionURL: "https://news.ycombinator.com/item?id=41990", Author: "testwriter",
			CreatedAt: time.Unix(1793007000, 0).UTC(), Tags: []StoryTag{StoryTagStory, StoryTagAskHN, StoryTagFrontPage},
			StoryText: "We fuzz the parser & the optimizer.\n\nSee https://example.com/fuzz for details.\n\nrun --all",
		},
		{
			ID: 41980, Title: "Long notes", DiscussionURL: "https://news.ycombinator.com/item?id=41980", CreatedAt: time.Unix(1793000100, 0).UTC(),
			Tags: []StoryTag{}, StoryText: strings.Repeat("é", MaxStoryTextCharacters), IsStoryTextTruncated: true,
		},
	}}, result.Value)
	require.Equal(t, time.UTC, result.Value.Stories[0].CreatedAt.Location())

	result, err = sdkgo.RunQuery(queryContext{}, client.SearchStories(), testConnection, SearchStoriesInput{Query: "compiler", HitsPerPage: 3, Page: 2})
	require.NoError(t, err)
	require.Equal(t, 2, result.Value.Page)
	require.Zero(t, result.Value.NextPage, "the last readable page has no next page")
}

func TestStorySearchInvalidInputDoesNotCallProvider(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, err := fmt.Fprint(w, strings.Replace(emptySearchPage, `"page":0`, `"page":49`, 1))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{SearchBaseURL: server.URL}, nil)
	require.NoError(t, err)
	createdAt := time.Date(2026, time.October, 26, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name  string
		input SearchStoriesInput
	}{
		{"unknown sort", SearchStoriesInput{SortBy: "popularity"}},
		{"comment tag", SearchStoriesInput{Tags: []StoryTag{"comment"}}},
		{"author tag", SearchStoriesInput{Tags: []StoryTag{"author_pg"}}},
		{"long query", SearchStoriesInput{Query: strings.Repeat("a", 513)}},
		{"negative points", SearchStoriesInput{MinPoints: -1}},
		{"empty window", SearchStoriesInput{CreatedAfter: createdAt, CreatedBefore: createdAt}},
		{"reversed window", SearchStoriesInput{CreatedAfter: createdAt, CreatedBefore: createdAt.Add(-time.Hour)}},
		{"large page size", SearchStoriesInput{HitsPerPage: 101}},
		{"negative page size", SearchStoriesInput{HitsPerPage: -1}},
		{"negative page", SearchStoriesInput{Page: -1}},
		{"page past reachable hits", SearchStoriesInput{Page: 50}},
		{"large page past reachable hits", SearchStoriesInput{Page: 1 << 40, HitsPerPage: 100}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(queryContext{}, client.SearchStories(), testConnection, test.input)
			require.NoError(t, err)
			require.Equal(t, SearchStoriesBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Zero(t, requests.Load())

	// A 512-byte query and the last page that starts within the first 1000 hits are accepted.
	result, err := sdkgo.RunQuery(queryContext{}, client.SearchStories(), testConnection, SearchStoriesInput{Query: strings.Repeat("a", 512), Page: 49})
	require.NoError(t, err)
	require.Equal(t, SearchStoriesBranchSearched, result.Branch)
	require.Equal(t, int32(1), requests.Load())
}

func TestStorySearchRejectsMalformedAndOversizedResponses(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{"bad JSON", `{`},
		{"null", `null`},
		{"null hits", `{"hits":null,"page":0,"nbPages":0,"nbHits":0}`},
		{"missing page", `{"hits":[],"nbPages":0,"nbHits":0}`},
		{"missing page count", `{"hits":[],"page":0,"nbHits":0}`},
		{"missing hit count", `{"hits":[],"page":0,"nbPages":0}`},
		{"other page", `{"hits":[],"page":1,"nbPages":2,"nbHits":2}`},
		{"negative hit count", `{"hits":[],"page":0,"nbPages":0,"nbHits":-1}`},
		{"too many hits", `{"hits":[{"objectID":"1","created_at_i":1},{"objectID":"2","created_at_i":1}],"page":0,"nbPages":1,"nbHits":2}`},
		{"text story ID", `{"hits":[{"objectID":"abc","created_at_i":1}],"page":0,"nbPages":1,"nbHits":1}`},
		{"zero story ID", `{"hits":[{"objectID":"0","created_at_i":1}],"page":0,"nbPages":1,"nbHits":1}`},
		{"missing creation time", `{"hits":[{"objectID":"1"}],"page":0,"nbPages":1,"nbHits":1}`},
		{"oversized", `{"hits":[],"page":0,"nbPages":0,"nbHits":0,"padding":"` + strings.Repeat("x", 200) + `"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, err := fmt.Fprint(w, test.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			client, err := New(Config{SearchBaseURL: server.URL, MaxResponseBytes: 200}, nil)
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.SearchStories(), testConnection, SearchStoriesInput{Query: "compiler", HitsPerPage: 1})
			require.NoError(t, err)
			require.Equal(t, SearchStoriesBranchInvalidResponse, result.Branch)
		})
	}
}

func TestStorySearchHTTPFailureClassification(t *testing.T) {
	for _, test := range []struct {
		status  int
		branch  sdkgo.BranchID
		isRetry bool
	}{
		{429, "", true}, {500, "", true}, {503, "", true}, {408, "", true},
		{400, SearchStoriesBranchProviderRejected, false}, {403, SearchStoriesBranchProviderRejected, false},
		{404, SearchStoriesBranchProviderRejected, false}, {302, SearchStoriesBranchProviderRejected, false},
	} {
		t.Run(fmt.Sprint(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "2")
				w.Header().Set("Location", "/unexpected")
				w.WriteHeader(test.status)
			}))
			defer server.Close()
			client, err := New(Config{SearchBaseURL: server.URL}, nil)
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.SearchStories(), testConnection, SearchStoriesInput{Query: "compiler"})
			if test.isRetry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
		})
	}
}

func TestSearchBaseURLValidation(t *testing.T) {
	for _, searchBaseURL := range []string{"http://example.com/api/v1", "https://example.com/api/v1?x=1", "ftp://example.com"} {
		_, err := New(Config{SearchBaseURL: searchBaseURL}, nil)
		require.ErrorContains(t, err, "Hacker News Search base URL")
	}
	client, err := New(Config{}, nil)
	require.NoError(t, err)
	require.Equal(t, "https://hn.algolia.com/api/v1", client.searchBaseURL)
}

func TestPlainTextFromStoryHTML(t *testing.T) {
	for _, test := range []struct{ name, storyHTML, text string }{
		{"empty", "", ""},
		{"entities", "A &amp; B &#x27;quoted&#x27; &gt; C", "A & B 'quoted' > C"},
		{"paragraphs", "First<p>Second</p><p></p><p>Third", "First\n\nSecond\n\nThird"},
		{"inline markup", "a <i>b</i><b>c</b> <a href=\"https://example.com\">link</a>", "a bc link"},
		{"whitespace", "  spaced\n\tout  ", "spaced out"},
		{"script text is text", "&lt;script&gt;alert(1)&lt;/script&gt;", "<script>alert(1)</script>"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.text, plainTextFromStoryHTML(test.storyHTML))
		})
	}
}
