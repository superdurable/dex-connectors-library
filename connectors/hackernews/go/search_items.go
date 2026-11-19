// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hackernews

import (
	"encoding/json"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// SearchItemsInput selects one page of public Hacker News items.
type SearchItemsInput struct {
	// Query is the full-text query, at most 512 bytes; blank matches all items of the selected type.
	Query string `json:"query,omitempty"`
	// Type is story, comment, poll, or pollopt; blank searches all indexed types.
	Type string `json:"type,omitempty"`
	// SortBy is relevance or date; blank selects relevance.
	SortBy StorySearchSort `json:"sortBy,omitempty"`
	// Page is zero-based; Page times HitsPerPage must stay below 1000.
	Page int `json:"page,omitempty"`
	// HitsPerPage is 1 through 100; zero selects 20.
	HitsPerPage int `json:"hitsPerPage,omitempty"`
	// CreatedAfter keeps items created strictly after this UTC time; zero sets no lower bound.
	CreatedAfter time.Time `json:"createdAfter,omitzero"`
}

// ItemSearchPage contains one page of public search results.
type ItemSearchPage struct {
	// Items appear in the Search API's requested order.
	Items []ItemSearchHit `json:"items"`
	// Page is this zero-based page number.
	Page int `json:"page"`
	// Pages is the number of readable pages.
	Pages int `json:"pages"`
	// TotalHits is the Search API's approximate total result count.
	TotalHits int `json:"totalHits"`
	// HitsPerPage is the requested page size.
	HitsPerPage int `json:"hitsPerPage"`
	// NextPage is the next page number, or zero after the last page.
	NextPage int `json:"nextPage"`
}

// ItemSearchHit is public indexed metadata; Title and Text are untrusted provider content.
type ItemSearchHit struct {
	// ID is the positive Hacker News item ID.
	ID int64 `json:"id"`
	// Type is story, comment, poll, or pollopt.
	Type string `json:"type"`
	// Title is the story or poll title, when present.
	Title string `json:"title,omitempty"`
	// Text is plain submitted or comment text, bounded to 2000 characters.
	Text string `json:"text,omitempty"`
	// URL is the story's external source URL, when present.
	URL string `json:"url,omitempty"`
	// Author is the public username, when present.
	Author string `json:"author,omitempty"`
	// CreatedAt is the item creation time in UTC.
	CreatedAt time.Time `json:"createdAt"`
	// ParentID is a comment's direct parent item ID, when indexed.
	ParentID int64 `json:"parentId,omitempty"`
	// StoryID is the associated story ID for a comment, when indexed.
	StoryID int64 `json:"storyId,omitempty"`
	// DiscussionURL links directly to this item.
	DiscussionURL string `json:"discussionUrl"`
}

// SearchItemsOperation searches mixed public item types with one HTTP request.
type SearchItemsOperation struct{ client *Client }

// SearchItems binds the public item search Query to this client.
func (client *Client) SearchItems() SearchItemsOperation { return SearchItemsOperation{client} }

// Definition returns the generated item search Query contract.
func (SearchItemsOperation) Definition() sdkgo.QueryDefinition { return SearchItemsDefinition }

// Invoke searches one bounded page of public items and retries temporary failures.
func (operation SearchItemsOperation) Invoke(call sdkgo.Call, input SearchItemsInput) sdkgo.QueryAttempt[ItemSearchPage] {
	if len(input.Query) > maxSearchQueryBytes || input.Page < 0 {
		return failedBranch[ItemSearchPage]("searchItems", SearchItemsBranchDefect, sdkgo.FailureValidation, "Hacker News item search query or page is invalid")
	}
	if input.HitsPerPage == 0 {
		input.HitsPerPage = defaultStorySearchHitsPerPage
	}
	if input.HitsPerPage < 1 || input.HitsPerPage > maxStorySearchHitsPerPage || input.Page >= reachableStorySearchHits || input.Page > (reachableStorySearchHits-1)/input.HitsPerPage {
		return failedBranch[ItemSearchPage]("searchItems", SearchItemsBranchDefect, sdkgo.FailureValidation, "Hacker News item search page size is invalid")
	}
	if input.Type != "" && input.Type != "story" && input.Type != "comment" && input.Type != "poll" && input.Type != "pollopt" {
		return failedBranch[ItemSearchPage]("searchItems", SearchItemsBranchDefect, sdkgo.FailureValidation, "Hacker News item type is invalid")
	}
	path := "/search"
	if input.SortBy == StorySearchSortDate {
		path = "/search_by_date"
	} else if input.SortBy != "" && input.SortBy != StorySearchSortRelevance {
		return failedBranch[ItemSearchPage]("searchItems", SearchItemsBranchDefect, sdkgo.FailureValidation, "Hacker News item search sort is invalid")
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "hitsPerPage": {strconv.Itoa(input.HitsPerPage)}}
	if input.Query != "" {
		query.Set("query", input.Query)
	}
	if input.Type != "" {
		query.Set("tags", input.Type)
	}
	if !input.CreatedAfter.IsZero() {
		query.Set("numericFilters", "created_at_i>"+strconv.FormatInt(input.CreatedAfter.Unix(), 10))
	}
	return getJSON(operation.client, call, "searchItems", operation.client.searchBaseURL+path+"?"+query.Encode(), func(body []byte) sdkgo.QueryAttempt[ItemSearchPage] {
		return decodeItemSearchPage(body, input)
	})
}

func decodeItemSearchPage(body []byte, input SearchItemsInput) sdkgo.QueryAttempt[ItemSearchPage] {
	var response struct {
		Hits []struct {
			ObjectID    string   `json:"objectID"`
			Title       string   `json:"title"`
			StoryTitle  string   `json:"story_title"`
			URL         string   `json:"url"`
			Author      string   `json:"author"`
			StoryText   string   `json:"story_text"`
			CommentText string   `json:"comment_text"`
			CreatedAt   int64    `json:"created_at_i"`
			ParentID    int64    `json:"parent_id"`
			StoryID     int64    `json:"story_id"`
			Tags        []string `json:"_tags"`
		} `json:"hits"`
		Page      *int `json:"page"`
		Pages     *int `json:"nbPages"`
		TotalHits *int `json:"nbHits"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Hits == nil || response.Page == nil || response.Pages == nil || response.TotalHits == nil {
		return failedBranch[ItemSearchPage]("searchItems", SearchItemsBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News Search returned malformed items")
	}
	if *response.Page != input.Page || *response.Pages < 0 || *response.TotalHits < 0 || len(response.Hits) > input.HitsPerPage {
		return failedBranch[ItemSearchPage]("searchItems", SearchItemsBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News Search returned inconsistent item paging")
	}
	page := ItemSearchPage{Items: make([]ItemSearchHit, 0, len(response.Hits)), Page: input.Page, Pages: *response.Pages, TotalHits: *response.TotalHits, HitsPerPage: input.HitsPerPage}
	if input.Page+1 < page.Pages {
		page.NextPage = input.Page + 1
	}
	for _, hit := range response.Hits {
		id, err := strconv.ParseInt(hit.ObjectID, 10, 64)
		if err != nil || id <= 0 || hit.CreatedAt <= 0 {
			return failedBranch[ItemSearchPage]("searchItems", SearchItemsBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News Search returned an invalid item")
		}
		itemType := ""
		for _, tag := range hit.Tags {
			if tag == "story" || tag == "comment" || tag == "poll" || tag == "pollopt" {
				itemType = tag
				break
			}
		}
		if itemType == "" || (input.Type != "" && itemType != input.Type) {
			return failedBranch[ItemSearchPage]("searchItems", SearchItemsBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News Search returned an unexpected item type")
		}
		itemText := hit.StoryText
		if itemType == "comment" {
			itemText = hit.CommentText
		}
		plainText, _ := truncateCharacters(plainTextFromStoryHTML(itemText), MaxStoryTextCharacters)
		item := ItemSearchHit{ID: id, Type: itemType, Title: hit.Title, Text: plainText, URL: hit.URL, Author: hit.Author, CreatedAt: time.Unix(hit.CreatedAt, 0).UTC(), ParentID: hit.ParentID, StoryID: hit.StoryID, DiscussionURL: "https://news.ycombinator.com/item?id=" + strconv.FormatInt(id, 10)}
		if item.Title == "" {
			item.Title = hit.StoryTitle
		}
		page.Items = append(page.Items, item)
	}
	return sdkgo.NewQueryBranch(SearchItemsBranchSearched, page, nil, sdkgo.Receipt{})
}
