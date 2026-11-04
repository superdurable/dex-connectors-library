// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package hackernews reads the public Hacker News API through typed Dex Query Steps.
package hackernews

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable client dependency.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport. New copies the client, preserves a nonzero timeout, and disables redirects.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable HTTP configuration and is safe for concurrent calls.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	httpClient       *http.Client
}

// ListStoryIDsInput selects one feed snapshot. It does not select a historical time window.
type ListStoryIDsInput struct {
	// Feed is top, best, new, ask, or show. Empty selects top.
	Feed string `json:"feed"`
	// Limit bounds returned IDs from 1 through 500. Zero selects 50.
	Limit int `json:"limit"`
}

// StoryIDs contains the start of a feed in the provider's current display order.
type StoryIDs struct {
	// IDs contains at most the requested limit. Top stories can include jobs.
	IDs []int64 `json:"ids"`
	// HasMore reports that the API returned more IDs than this operation retained. The API has no page cursor.
	HasMore bool `json:"hasMore"`
	// ObservedAt is the UTC time when this response was read.
	ObservedAt time.Time `json:"observedAt"`
}

// GetItemInput identifies one story, comment, job, poll, or poll option.
type GetItemInput struct {
	// ID is a positive Hacker News item ID.
	ID int64 `json:"id"`
}

// Item preserves public API fields and adds the exact Hacker News permalink.
// TextHTML and Title are untrusted provider text. Applications must escape or sanitize them before HTML rendering.
type Item struct {
	// ID identifies this item.
	ID int64 `json:"id"`
	// Type is story, comment, job, poll, or pollopt.
	Type string `json:"type"`
	// By is the author's public username, when available.
	By string `json:"by,omitempty"`
	// Time is the creation time in Unix seconds, when available.
	Time int64 `json:"time,omitempty"`
	// Title is the provider's title, which can contain HTML entities.
	Title string `json:"title,omitempty"`
	// TextHTML is the submitted story or comment body. Linked article content is not included.
	TextHTML string `json:"text,omitempty"`
	// URL is the submitted external URL. Ask HN and other text posts may have no URL.
	URL string `json:"url,omitempty"`
	// DiscussionURL links directly to this story or comment on Hacker News.
	DiscussionURL string `json:"discussionUrl"`
	// Score is the story score. Hacker News does not expose comment scores.
	Score int `json:"score,omitempty"`
	// Descendants is the provider's comment count for a story or poll.
	Descendants int `json:"descendants,omitempty"`
	// Parent identifies a comment's parent item or a poll option's poll.
	Parent int64 `json:"parent,omitempty"`
	// Kids contains child IDs in the provider's ranked display order.
	Kids []int64 `json:"kids,omitempty"`
	// Deleted reports a deleted item. GetItem selects notFound instead of returning a tombstone.
	Deleted bool `json:"deleted,omitempty"`
	// Dead reports a moderated item. GetItem selects notFound instead of returning it.
	Dead bool `json:"dead,omitempty"`
}

// ListStoryIDsOperation reads one feed with one HTTP request.
type ListStoryIDsOperation struct{ client *Client }

// GetItemOperation reads one item with one HTTP request. It does not fetch its children or external URL.
type GetItemOperation struct{ client *Client }

// New validates config without accessing the network. Zero fields use manifest defaults.
// The generated constructor supplies credentials, but public Hacker News reads never resolve or transmit them.
func New(config Config, _ sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Hacker News base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListStoryIDs returns the feed query bound to this client.
func (client *Client) ListStoryIDs() ListStoryIDsOperation { return ListStoryIDsOperation{client} }

// GetItem returns the item query bound to this client.
func (client *Client) GetItem() GetItemOperation { return GetItemOperation{client} }

// Definition returns the manifest-generated feed query contract.
func (ListStoryIDsOperation) Definition() sdkgo.QueryDefinition { return ListStoryIDsDefinition }

// Invoke reads a bounded current feed snapshot and classifies provider failures for Dex retry.
func (operation ListStoryIDsOperation) Invoke(call sdkgo.Call, input ListStoryIDsInput) sdkgo.QueryAttempt[StoryIDs] {
	if input.Feed == "" {
		input.Feed = "top"
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	switch input.Feed {
	case "top", "best", "new", "ask", "show":
	default:
		return failedBranch[StoryIDs]("listStoryIDs", ListStoryIDsBranchDefect, sdkgo.FailureValidation, "feed must be top, best, new, ask, or show")
	}
	if input.Limit < 1 || input.Limit > 500 {
		return failedBranch[StoryIDs]("listStoryIDs", ListStoryIDsBranchDefect, sdkgo.FailureValidation, "limit must be from 1 through 500")
	}
	return getJSON(operation.client, call, "listStoryIDs", "/"+input.Feed+"stories.json", func(body []byte) sdkgo.QueryAttempt[StoryIDs] {
		return decodeStoryIDs(body, input.Limit)
	})
}

func decodeStoryIDs(body []byte, limit int) sdkgo.QueryAttempt[StoryIDs] {
	var ids []int64
	if err := json.Unmarshal(body, &ids); err != nil || ids == nil {
		return failedBranch[StoryIDs]("listStoryIDs", ListStoryIDsBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News returned an invalid feed")
	}
	for _, id := range ids {
		if id <= 0 {
			return failedBranch[StoryIDs]("listStoryIDs", ListStoryIDsBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News returned an invalid item ID")
		}
	}
	result := StoryIDs{
		IDs:     ids[:min(limit, len(ids))],
		HasMore: len(ids) > limit, ObservedAt: time.Now().UTC(),
	}
	return sdkgo.NewQueryBranch(ListStoryIDsBranchListed, result, nil, sdkgo.Receipt{})
}

// Definition returns the manifest-generated item query contract.
func (GetItemOperation) Definition() sdkgo.QueryDefinition { return GetItemDefinition }

// Invoke reads one public item. Null, deleted, and dead items select notFound without retrying.
func (operation GetItemOperation) Invoke(call sdkgo.Call, input GetItemInput) sdkgo.QueryAttempt[Item] {
	if input.ID <= 0 {
		return failedBranch[Item]("getItem", GetItemBranchDefect, sdkgo.FailureValidation, "item ID must be positive")
	}
	return getJSON(operation.client, call, "getItem", "/item/"+strconv.FormatInt(input.ID, 10)+".json", func(body []byte) sdkgo.QueryAttempt[Item] {
		return decodeItem(body, input.ID)
	})
}

func decodeItem(body []byte, requestedID int64) sdkgo.QueryAttempt[Item] {
	var item *Item
	if err := json.Unmarshal(body, &item); err != nil {
		return failedBranch[Item]("getItem", GetItemBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News returned malformed item JSON")
	}
	if item == nil || item.Deleted || item.Dead {
		return failedBranch[Item]("getItem", GetItemBranchNotFound, sdkgo.FailureNotFound, "Hacker News item is deleted or dead")
	}
	if item.ID != requestedID {
		return failedBranch[Item]("getItem", GetItemBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News returned a different item ID")
	}
	switch item.Type {
	case "story", "comment", "job", "poll", "pollopt":
	default:
		return failedBranch[Item]("getItem", GetItemBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News returned an unknown item type")
	}
	for _, id := range item.Kids {
		if id <= 0 {
			return failedBranch[Item]("getItem", GetItemBranchInvalidResponse, sdkgo.FailureProtocol, "Hacker News returned an invalid child ID")
		}
	}
	item.DiscussionURL = "https://news.ycombinator.com/item?id=" + strconv.FormatInt(item.ID, 10)
	return sdkgo.NewQueryBranch(GetItemBranchFound, *item, nil, sdkgo.Receipt{})
}

func getJSON[T any](client *Client, call sdkgo.Call, operation, path string, decode func([]byte) sdkgo.QueryAttempt[T]) sdkgo.QueryAttempt[T] {
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return failedBranch[T](operation, sdkgo.DefectBranchID, sdkgo.FailureValidation, "Hacker News request configuration is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "superdurable-hacker-news-daily/0.1")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[T](failure(operation, sdkgo.FailureAvailability, "Hacker News request failed"), 0)
	}
	// Closing a read-only response cannot change the query outcome.
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewQueryRetry[T](failure(operation, kind, "Hacker News is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusNotFound && operation == "getItem":
		return failedBranch[T](operation, GetItemBranchNotFound, sdkgo.FailureNotFound, "Hacker News resource was not found")
	case response.StatusCode != http.StatusOK:
		return failedBranch[T](operation, GetItemBranchProviderRejected, sdkgo.FailureProviderRejection, "Hacker News rejected the request")
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedBranch[T](operation, GetItemBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Hacker News response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](failure(operation, sdkgo.FailureAvailability, "Hacker News response was interrupted"), 0)
	}
	return decode(body)
}

func failedBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	var value T
	failure := failure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func failure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "hacker-news", Operation: operation, Message: message}
}
