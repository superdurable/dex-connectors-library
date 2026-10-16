// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youtube

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// maximumSearchQueryCharacters bounds the keyword query sent to YouTube.
const maximumSearchQueryCharacters = 500

// SearchOrders lists the values SearchVideosInput.Order accepts.
var SearchOrders = []string{"relevance", "date", "viewCount", "rating", "title"}

// SearchVideosInput selects one page of public video search results.
// Query or ChannelID must be set.
type SearchVideosInput struct {
	// Query is the keyword search, up to 500 characters; YouTube supports - to exclude and | to match either term.
	Query string `json:"query,omitempty"`
	// ChannelID limits results to one channel, such as UC followed by 22 characters.
	ChannelID string `json:"channelId,omitempty"`
	// Order is one of SearchOrders; blank uses YouTube's relevance order.
	Order string `json:"order,omitempty"`
	// PublishedAfter limits results to videos published after this time; nil applies no limit.
	PublishedAfter *time.Time `json:"publishedAfter,omitempty"`
	// MaxResults is the page size, 1 to 50; zero uses YouTube's default of 5.
	MaxResults int `json:"maxResults,omitempty"`
	// PageToken continues from the previous page's NextPageToken; blank starts at the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// VideoSearchPage is one page of search results in the requested order.
type VideoSearchPage struct {
	// Results holds this page, possibly empty.
	Results []VideoSearchResult `json:"results"`
	// NextPageToken continues the search; blank means the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
	// TotalResults is YouTube's approximate total, which may exceed the results it serves.
	TotalResults int64 `json:"totalResults"`
}

// VideoSearchResult is one matching video. Title and Description are untrusted provider text.
type VideoSearchResult struct {
	// VideoID is the 11-character video ID.
	VideoID string `json:"videoId"`
	// ChannelID is the publishing channel's ID.
	ChannelID string `json:"channelId"`
	// ChannelTitle is the publishing channel's display name.
	ChannelTitle string `json:"channelTitle"`
	// Title is the video title, which YouTube HTML-escapes in search results.
	Title string `json:"title"`
	// Description is a truncated description.
	Description string `json:"description"`
	// PublishedAt is when the video was published, in UTC.
	PublishedAt time.Time `json:"publishedAt"`
}

type providerSearchList struct {
	NextPageToken string `json:"nextPageToken"`
	PageInfo      *struct {
		TotalResults int64 `json:"totalResults"`
	} `json:"pageInfo"`
	Items *[]struct {
		ID struct {
			Kind    string `json:"kind"`
			VideoID string `json:"videoId"`
		} `json:"id"`
		Snippet struct {
			ChannelID    string `json:"channelId"`
			ChannelTitle string `json:"channelTitle"`
			Title        string `json:"title"`
			Description  string `json:"description"`
			PublishedAt  string `json:"publishedAt"`
		} `json:"snippet"`
	} `json:"items"`
}

// SearchVideosOperation implements the searchVideos connector Query. Each call costs 100 quota units.
type SearchVideosOperation struct{ client *Client }

var searchVideosBranches = operationBranches{
	quotaExhausted: SearchVideosBranchQuotaExhausted, rejected: SearchVideosBranchProviderRejected,
	invalidResponse: SearchVideosBranchInvalidResponse, defect: SearchVideosBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (SearchVideosOperation) Definition() sdkgo.QueryDefinition { return SearchVideosDefinition }

// Invoke sends one search.list request for videos and classifies its attempt.
func (operation SearchVideosOperation) Invoke(call sdkgo.Call, input SearchVideosInput) sdkgo.QueryAttempt[VideoSearchPage] {
	query, err := encodeSearchVideosQuery(input)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchVideosBranchDefect, VideoSearchPage{}, providerFailurePointer("searchVideos", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "searchVideos", providerRequest{method: http.MethodGet, path: "/search", query: query}, input.ChannelID)
	if outcome != nil {
		return queryAttemptForOutcome[VideoSearchPage](*outcome, searchVideosBranches)
	}
	receipt := operation.client.receipt(call, input.ChannelID, response.header)
	page, err := decodeVideoSearchPage(response.body, input.MaxResults)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchVideosBranchInvalidResponse, VideoSearchPage{}, providerFailurePointer("searchVideos", sdkgo.FailureProtocol, err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(SearchVideosBranchSearched, page, nil, receipt)
}

func encodeSearchVideosQuery(input SearchVideosInput) (url.Values, error) {
	query := url.Values{"part": {"snippet"}, "type": {"video"}}
	if strings.TrimSpace(input.Query) == "" && input.ChannelID == "" {
		return nil, errors.New("query or channelId must be set")
	}
	if !utf8.ValidString(input.Query) || utf8.RuneCountInString(input.Query) > maximumSearchQueryCharacters {
		return nil, errors.New("query must be valid UTF-8 of at most 500 characters")
	}
	if strings.TrimSpace(input.Query) != "" {
		query.Set("q", input.Query)
	}
	if input.ChannelID != "" {
		if !channelIDPattern.MatchString(input.ChannelID) {
			return nil, errors.New("channelId must be a YouTube channel ID: UC followed by 22 characters")
		}
		query.Set("channelId", input.ChannelID)
	}
	if input.Order != "" {
		if !slices.Contains(SearchOrders, input.Order) {
			return nil, errors.New("order must be relevance, date, viewCount, rating, or title")
		}
		query.Set("order", input.Order)
	}
	if input.PublishedAfter != nil {
		query.Set("publishedAfter", input.PublishedAfter.UTC().Format(time.RFC3339))
	}
	if err := validatePageRequest(query, input.MaxResults, input.PageToken); err != nil {
		return nil, err
	}
	return query, nil
}

func decodeVideoSearchPage(body []byte, maxResults int) (VideoSearchPage, error) {
	var decoded providerSearchList
	if err := decodeResponse(body, &decoded); err != nil {
		return VideoSearchPage{}, err
	}
	if decoded.Items == nil || decoded.PageInfo == nil {
		return VideoSearchPage{}, errors.New("YouTube returned an incomplete search page")
	}
	if len(*decoded.Items) > max(maxResults, 5) && len(*decoded.Items) > MaximumPageSize {
		return VideoSearchPage{}, errors.New("YouTube returned more results than the page size")
	}
	if err := validateReturnedPageToken(decoded.NextPageToken); err != nil {
		return VideoSearchPage{}, err
	}
	page := VideoSearchPage{Results: make([]VideoSearchResult, 0, len(*decoded.Items)), NextPageToken: decoded.NextPageToken, TotalResults: decoded.PageInfo.TotalResults}
	for _, item := range *decoded.Items {
		if item.ID.Kind != "youtube#video" || !videoIDPattern.MatchString(item.ID.VideoID) || !channelIDPattern.MatchString(item.Snippet.ChannelID) {
			return VideoSearchPage{}, errors.New("YouTube returned a search result that is not a valid video")
		}
		publishedAt, err := parseOptionalTime(item.Snippet.PublishedAt)
		if err != nil || publishedAt == nil {
			return VideoSearchPage{}, errors.New("YouTube returned a search result without a publish time")
		}
		page.Results = append(page.Results, VideoSearchResult{
			VideoID: item.ID.VideoID, ChannelID: item.Snippet.ChannelID, ChannelTitle: item.Snippet.ChannelTitle,
			Title: item.Snippet.Title, Description: item.Snippet.Description, PublishedAt: *publishedAt,
		})
	}
	return page, nil
}
