// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hackernews

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// MaxStoryTextCharacters bounds StorySearchHit.StoryText, counted in Unicode code points.
const MaxStoryTextCharacters = 2000

const (
	// maxSearchQueryBytes is the Search API's query length limit; a longer query returns HTTP 400.
	maxSearchQueryBytes           = 512
	maxStorySearchHitsPerPage     = 100
	defaultStorySearchHitsPerPage = 20
	// reachableStorySearchHits is how many leading hits of one query the Search API serves across pages.
	reachableStorySearchHits = 1000
)

// StorySearchSort orders story search results. Other values select defect.
type StorySearchSort string

const (
	// StorySearchSortRelevance orders matches by text relevance, then points and comment count.
	StorySearchSortRelevance StorySearchSort = "relevance"
	// StorySearchSortDate orders matches newest first.
	StorySearchSortDate StorySearchSort = "date"
)

// StoryTag is a Hacker News Search tag that narrows a story search. Other values select defect.
type StoryTag string

const (
	// StoryTagStory matches every story, including Ask HN and Show HN posts. SearchStories always applies it.
	StoryTagStory StoryTag = "story"
	// StoryTagShowHN matches Show HN posts.
	StoryTagShowHN StoryTag = "show_hn"
	// StoryTagAskHN matches Ask HN posts.
	StoryTagAskHN StoryTag = "ask_hn"
	// StoryTagFrontPage matches stories that are on the Hacker News front page when the search runs.
	StoryTagFrontPage StoryTag = "front_page"
)

// knownStoryTags lists the accepted tags in StorySearchHit.Tags order, independent of the provider's order.
var knownStoryTags = []StoryTag{StoryTagStory, StoryTagShowHN, StoryTagAskHN, StoryTagFrontPage}

// SearchStoriesInput selects one page of a keyword story search. Every field is optional.
type SearchStoriesInput struct {
	// Query is the full-text keyword query, at most 512 bytes, matched against story titles, URLs, authors,
	// and submitted text. Quoted phrases and -excluded words follow the Search API syntax. Empty matches
	// every story that the other filters allow.
	Query string `json:"query,omitempty"`
	// SortBy is relevance or date. Empty selects relevance; date returns the newest stories first.
	SortBy StorySearchSort `json:"sortBy,omitempty"`
	// Tags lists story, show_hn, ask_hn, or front_page. A story must carry every listed tag. The story tag
	// is always applied, so empty searches all stories, including Ask HN and Show HN posts.
	Tags []StoryTag `json:"tags,omitempty"`
	// CreatedAfter keeps stories submitted strictly after this time. Zero sets no lower bound.
	CreatedAfter time.Time `json:"createdAfter,omitzero"`
	// CreatedBefore keeps stories submitted strictly before this time. Zero sets no upper bound. When both
	// bounds are set, CreatedBefore must be later than CreatedAfter.
	CreatedBefore time.Time `json:"createdBefore,omitzero"`
	// MinPoints keeps stories with at least this many points. Zero sets no minimum; negative is invalid.
	MinPoints int `json:"minPoints,omitempty"`
	// Page is the zero-based result page. Zero selects the first page. Page times HitsPerPage must stay
	// below 1000, because the Search API serves only the first 1000 hits of a query.
	Page int `json:"page,omitempty"`
	// HitsPerPage is the page size from 1 through 100. Zero selects 20.
	HitsPerPage int `json:"hitsPerPage,omitempty"`
}

// StorySearchPage is one page of matching stories in the requested order.
type StorySearchPage struct {
	// Stories contains at most HitsPerPage stories. It is empty when nothing matches. Successive pages of a
	// newest-first search can repeat a story when newer stories arrive between reads.
	Stories []StorySearchHit `json:"stories"`
	// Page is the zero-based number of this page.
	Page int `json:"page"`
	// Pages counts the readable pages. It stops at the first 1000 hits, so it can cover fewer than TotalHits.
	Pages int `json:"pages"`
	// TotalHits is the Search API's count of matching stories. It can be an estimate for large result sets.
	TotalHits int `json:"totalHits"`
	// HitsPerPage is the page size that this read used.
	HitsPerPage int `json:"hitsPerPage"`
	// NextPage is the zero-based number of the next readable page, or zero when this is the last one.
	NextPage int `json:"nextPage"`
}

// StorySearchHit is one matching story as the Search API indexed it. Points and CommentCount can lag the
// live site. Title, Author, and StoryText are untrusted provider text; escape them before HTML rendering.
type StorySearchHit struct {
	// ID is the positive Hacker News item ID, which GetItem reads.
	ID int64 `json:"id"`
	// Title is the story title.
	Title string `json:"title"`
	// URL is the submitted external URL. It is empty for Ask HN and other text posts.
	URL string `json:"url,omitempty"`
	// DiscussionURL links directly to the story's Hacker News discussion.
	DiscussionURL string `json:"discussionUrl"`
	// Author is the submitter's public username.
	Author string `json:"author"`
	// Points is the story score.
	Points int `json:"points"`
	// CommentCount is the story's comment count.
	CommentCount int `json:"commentCount"`
	// CreatedAt is the UTC submission time, at second precision.
	CreatedAt time.Time `json:"createdAt"`
	// Tags lists the story, show_hn, ask_hn, and front_page tags that the story carries, in that order.
	Tags []StoryTag `json:"tags"`
	// StoryText is the submitted text as plain text, at most MaxStoryTextCharacters code points. Paragraphs
	// are separated by a blank line, and markup is removed. It is empty when the story has no text.
	StoryText string `json:"storyText,omitempty"`
	// IsStoryTextTruncated reports that StoryText was cut at MaxStoryTextCharacters.
	IsStoryTextTruncated bool `json:"isStoryTextTruncated,omitempty"`
}

// SearchStoriesOperation searches stories through the Hacker News Search API with one HTTP request.
type SearchStoriesOperation struct{ client *Client }

// storySearchRequest is a validated SearchStoriesInput resolved to its Search API request.
type storySearchRequest struct {
	path        string
	parameters  url.Values
	page        int
	hitsPerPage int
}

// storySearchResponse is the Search API result page. Pointers mark fields that must be present.
type storySearchResponse struct {
	Hits      []storySearchResponseHit `json:"hits"`
	Page      *int                     `json:"page"`
	Pages     *int                     `json:"nbPages"`
	TotalHits *int                     `json:"nbHits"`
}

// storySearchResponseHit holds the Search API hit fields that SearchStories returns. JSON null decodes as zero.
type storySearchResponseHit struct {
	ObjectID     string   `json:"objectID"`
	Title        string   `json:"title"`
	URL          string   `json:"url"`
	Author       string   `json:"author"`
	Points       int      `json:"points"`
	CommentCount int      `json:"num_comments"`
	CreatedAt    int64    `json:"created_at_i"`
	StoryHTML    string   `json:"story_text"`
	Tags         []string `json:"_tags"`
}

// Definition returns the manifest-generated story search contract.
func (SearchStoriesOperation) Definition() sdkgo.QueryDefinition { return SearchStoriesDefinition }

// Invoke reads one page of matching stories. Invalid input selects defect before a request is sent.
func (operation SearchStoriesOperation) Invoke(call sdkgo.Call, input SearchStoriesInput) sdkgo.QueryAttempt[StorySearchPage] {
	request, err := resolveStorySearchRequest(input)
	if err != nil {
		return failedBranch[StorySearchPage]("searchStories", SearchStoriesBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	requestURL := operation.client.searchBaseURL + request.path + "?" + request.parameters.Encode()
	return getJSON(operation.client, call, "searchStories", requestURL, func(body []byte) sdkgo.QueryAttempt[StorySearchPage] {
		return decodeStorySearchPage(body, request)
	})
}

func resolveStorySearchRequest(input SearchStoriesInput) (storySearchRequest, error) {
	request := storySearchRequest{page: input.Page, hitsPerPage: input.HitsPerPage, parameters: url.Values{}}
	switch input.SortBy {
	case "", StorySearchSortRelevance:
		request.path = "/search"
	case StorySearchSortDate:
		request.path = "/search_by_date"
	default:
		return storySearchRequest{}, errors.New("sortBy must be relevance or date")
	}
	if len(input.Query) > maxSearchQueryBytes {
		return storySearchRequest{}, fmt.Errorf("query must be at most %d bytes", maxSearchQueryBytes)
	}
	tags := []StoryTag{StoryTagStory}
	for _, tag := range input.Tags {
		if !slices.Contains(knownStoryTags, tag) {
			return storySearchRequest{}, errors.New("tags must be story, show_hn, ask_hn, or front_page")
		}
		if !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	if input.MinPoints < 0 {
		return storySearchRequest{}, errors.New("minPoints cannot be negative")
	}
	if !input.CreatedAfter.IsZero() && !input.CreatedBefore.IsZero() && !input.CreatedBefore.After(input.CreatedAfter) {
		return storySearchRequest{}, errors.New("createdBefore must be later than createdAfter")
	}
	if request.hitsPerPage == 0 {
		request.hitsPerPage = defaultStorySearchHitsPerPage
	}
	if request.hitsPerPage < 1 || request.hitsPerPage > maxStorySearchHitsPerPage {
		return storySearchRequest{}, fmt.Errorf("hitsPerPage must be from 1 through %d", maxStorySearchHitsPerPage)
	}
	if request.page < 0 {
		return storySearchRequest{}, errors.New("page cannot be negative")
	}
	if request.page >= reachableStorySearchHits || request.page*request.hitsPerPage >= reachableStorySearchHits {
		return storySearchRequest{}, fmt.Errorf("page times hitsPerPage must be below %d, the hits the Search API serves for one query", reachableStorySearchHits)
	}
	if input.Query != "" {
		request.parameters.Set("query", input.Query)
	}
	tagNames := make([]string, 0, len(tags))
	for _, tag := range tags {
		tagNames = append(tagNames, string(tag))
	}
	request.parameters.Set("tags", strings.Join(tagNames, ","))
	if filters := storySearchNumericFilters(input); filters != "" {
		request.parameters.Set("numericFilters", filters)
	}
	request.parameters.Set("page", strconv.Itoa(request.page))
	request.parameters.Set("hitsPerPage", strconv.Itoa(request.hitsPerPage))
	return request, nil
}

// storySearchNumericFilters ANDs the filters; second-precision bounds stay strict for fractional times.
func storySearchNumericFilters(input SearchStoriesInput) string {
	var filters []string
	if !input.CreatedAfter.IsZero() {
		filters = append(filters, "created_at_i>"+strconv.FormatInt(input.CreatedAfter.Unix(), 10))
	}
	if !input.CreatedBefore.IsZero() {
		createdBefore := input.CreatedBefore.Unix()
		if input.CreatedBefore.Nanosecond() > 0 {
			createdBefore++
		}
		filters = append(filters, "created_at_i<"+strconv.FormatInt(createdBefore, 10))
	}
	if input.MinPoints > 0 {
		filters = append(filters, "points>="+strconv.Itoa(input.MinPoints))
	}
	return strings.Join(filters, ",")
}

func decodeStorySearchPage(body []byte, request storySearchRequest) sdkgo.QueryAttempt[StorySearchPage] {
	var response storySearchResponse
	if err := json.Unmarshal(body, &response); err != nil || response.Hits == nil || response.Page == nil || response.Pages == nil || response.TotalHits == nil {
		return failedBranch[StorySearchPage]("searchStories", SearchStoriesBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News Search returned a malformed result page")
	}
	if *response.Page != request.page || len(response.Hits) > request.hitsPerPage || *response.Pages < 0 || *response.TotalHits < 0 {
		return failedBranch[StorySearchPage]("searchStories", SearchStoriesBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News Search returned an inconsistent result page")
	}
	page := StorySearchPage{
		Stories: make([]StorySearchHit, 0, len(response.Hits)), Page: request.page, Pages: *response.Pages,
		TotalHits: *response.TotalHits, HitsPerPage: request.hitsPerPage,
	}
	if request.page+1 < page.Pages {
		page.NextPage = request.page + 1
	}
	for _, hit := range response.Hits {
		id, err := strconv.ParseInt(hit.ObjectID, 10, 64)
		if err != nil || id <= 0 {
			return failedBranch[StorySearchPage]("searchStories", SearchStoriesBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News Search returned an invalid story ID")
		}
		if hit.CreatedAt <= 0 {
			return failedBranch[StorySearchPage]("searchStories", SearchStoriesBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News Search returned an invalid creation time")
		}
		story := StorySearchHit{
			ID: id, Title: hit.Title, URL: hit.URL, DiscussionURL: "https://news.ycombinator.com/item?id=" + strconv.FormatInt(id, 10),
			Author: hit.Author, Points: hit.Points, CommentCount: hit.CommentCount, CreatedAt: time.Unix(hit.CreatedAt, 0).UTC(),
			Tags: make([]StoryTag, 0, len(knownStoryTags)),
		}
		for _, tag := range knownStoryTags {
			if slices.Contains(hit.Tags, string(tag)) {
				story.Tags = append(story.Tags, tag)
			}
		}
		story.StoryText, story.IsStoryTextTruncated = truncateCharacters(plainTextFromStoryHTML(hit.StoryHTML), MaxStoryTextCharacters)
		page.Stories = append(page.Stories, story)
	}
	return sdkgo.NewQueryBranch(SearchStoriesBranchSearched, page, nil, sdkgo.Receipt{})
}

// plainTextFromStoryHTML keeps the visible text of Hacker News story HTML, with a blank line between paragraphs.
func plainTextFromStoryHTML(storyHTML string) string {
	var text storyTextBuilder
	tokenizer := html.NewTokenizer(strings.NewReader(storyHTML))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			text.endParagraph()
			return strings.Join(text.paragraphs, "\n\n")
		case html.TextToken:
			text.current.Write(tokenizer.Text())
		case html.StartTagToken, html.EndTagToken, html.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			switch atom.Lookup(name) {
			case atom.P, atom.Pre, atom.Br, atom.Div, atom.Li, atom.Blockquote:
				text.endParagraph()
			}
		}
	}
}

// storyTextBuilder collects paragraphs; Hacker News separates them with <p> and seldom closes it.
type storyTextBuilder struct {
	paragraphs []string
	current    strings.Builder
}

// endParagraph collapses whitespace runs in the current paragraph and keeps it when it has text.
func (text *storyTextBuilder) endParagraph() {
	paragraph := strings.Join(strings.Fields(text.current.String()), " ")
	text.current.Reset()
	if paragraph != "" {
		text.paragraphs = append(text.paragraphs, paragraph)
	}
}

func truncateCharacters(text string, maximum int) (string, bool) {
	if utf8.RuneCountInString(text) <= maximum {
		return text, false
	}
	return string([]rune(text)[:maximum]), true
}
