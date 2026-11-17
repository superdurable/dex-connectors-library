// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hackernews

import (
	"encoding/json"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetArticleThreadInput identifies a public story or poll by item ID.
type GetArticleThreadInput struct {
	// ID is a positive Hacker News article ID.
	ID int64 `json:"id"`
}

// ArticleThread contains one indexed article and its public comment tree.
// Title, TextHTML, and comment text are untrusted provider content.
type ArticleThread struct {
	// ID is the article item ID.
	ID int64 `json:"id"`
	// Title is the article title.
	Title string `json:"title"`
	// Author is the article submitter's public username.
	Author string `json:"author,omitempty"`
	// URL is the external source URL, when present.
	URL string `json:"url,omitempty"`
	// TextHTML is the submitted story body, when present.
	TextHTML string `json:"textHtml,omitempty"`
	// DiscussionURL links to the article on Hacker News.
	DiscussionURL string `json:"discussionUrl"`
	// Comments are the indexed public comments in their nested order.
	Comments []ArticleComment `json:"comments"`
}

// ArticleComment is a public comment with its replies.
type ArticleComment struct {
	// ID is the comment item ID.
	ID int64 `json:"id"`
	// Author is the commenter's public username, when available.
	Author string `json:"author,omitempty"`
	// TextHTML is untrusted comment HTML.
	TextHTML string `json:"textHtml,omitempty"`
	// DiscussionURL links directly to the comment on Hacker News.
	DiscussionURL string `json:"discussionUrl"`
	// Replies contains nested public comments in provider order.
	Replies []ArticleComment `json:"replies,omitempty"`
}

// GetArticleThreadOperation reads one article and its indexed comments.
type GetArticleThreadOperation struct{ client *Client }

// GetArticleThread binds the article thread Query to this client.
func (client *Client) GetArticleThread() GetArticleThreadOperation {
	return GetArticleThreadOperation{client}
}

// Definition returns the generated article thread Query contract.
func (GetArticleThreadOperation) Definition() sdkgo.QueryDefinition {
	return GetArticleThreadDefinition
}

// Invoke reads the Search API's nested article endpoint with one HTTP request.
func (operation GetArticleThreadOperation) Invoke(call sdkgo.Call, input GetArticleThreadInput) sdkgo.QueryAttempt[ArticleThread] {
	if input.ID <= 0 {
		return failedBranch[ArticleThread]("getArticleThread", GetArticleThreadBranchDefect, sdkgo.FailureValidation, "article ID must be positive")
	}
	requestURL := operation.client.searchBaseURL + "/items/" + strconv.FormatInt(input.ID, 10)
	return getJSON(operation.client, call, "getArticleThread", requestURL, func(body []byte) sdkgo.QueryAttempt[ArticleThread] {
		var response *articleThreadResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return failedBranch[ArticleThread]("getArticleThread", GetArticleThreadBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News Search returned malformed article JSON")
		}
		if response == nil {
			return failedBranch[ArticleThread]("getArticleThread", GetArticleThreadBranchNotFound, sdkgo.FailureNotFound, "Hacker News article was not found")
		}
		if response.ID != input.ID || response.Title == "" {
			return failedBranch[ArticleThread]("getArticleThread", GetArticleThreadBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News Search returned an invalid article")
		}
		remaining := 5000
		comments, isValid := convertArticleComments(response.Children, response.ID, 0, &remaining)
		if !isValid {
			return failedBranch[ArticleThread]("getArticleThread", GetArticleThreadBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News Search returned an invalid comment tree")
		}
		article := ArticleThread{ID: response.ID, Title: response.Title, Author: response.Author, URL: response.URL, TextHTML: response.TextHTML, DiscussionURL: itemDiscussionURL(response.ID), Comments: comments}
		return sdkgo.NewQueryBranch(GetArticleThreadBranchFound, article, nil, sdkgo.Receipt{})
	})
}

type articleThreadResponse struct {
	ID       int64                   `json:"id"`
	Title    string                  `json:"title"`
	Author   string                  `json:"author"`
	URL      string                  `json:"url"`
	TextHTML string                  `json:"text"`
	ParentID int64                   `json:"parent_id"`
	Children []articleThreadResponse `json:"children"`
}

func convertArticleComments(children []articleThreadResponse, parentID int64, depth int, remaining *int) ([]ArticleComment, bool) {
	if depth >= 64 {
		return nil, false
	}
	comments := make([]ArticleComment, 0, len(children))
	for _, child := range children {
		*remaining = *remaining - 1
		if *remaining < 0 || child.ID <= 0 || (child.ParentID != 0 && child.ParentID != parentID) {
			return nil, false
		}
		replies, isValid := convertArticleComments(child.Children, child.ID, depth+1, remaining)
		if !isValid {
			return nil, false
		}
		comments = append(comments, ArticleComment{ID: child.ID, Author: child.Author, TextHTML: child.TextHTML, DiscussionURL: itemDiscussionURL(child.ID), Replies: replies})
	}
	return comments, true
}

func itemDiscussionURL(id int64) string {
	return "https://news.ycombinator.com/item?id=" + strconv.FormatInt(id, 10)
}
