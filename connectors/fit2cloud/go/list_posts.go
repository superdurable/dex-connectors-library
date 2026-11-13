// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fit2cloud

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

const postsPath = "/apis/content.halo.run/v1alpha1/posts"

// ListPostsInput selects a one-based article page; zero values use page 1 and 20 articles.
type ListPostsInput struct {
	// Page is the one-based article page number.
	Page int `json:"page"`
	// PageSize is the number of articles requested, from 1 to 100.
	PageSize int `json:"pageSize"`
}

// PostSummary contains article identity and publication metadata without content or author details.
type PostSummary struct {
	// Name is the stable Halo metadata name.
	Name string `json:"name"`
	// Title is the article title.
	Title string `json:"title"`
	// Slug is the article URL slug.
	Slug string `json:"slug"`
	// Published reports whether the article is published.
	Published bool `json:"published"`
	// Visibility is PUBLIC, INTERNAL, or PRIVATE.
	Visibility string `json:"visibility"`
}

// ListPostsOutput contains one article page and the next page number, or zero at the end.
type ListPostsOutput struct {
	// Posts are the returned articles in provider order.
	Posts []PostSummary `json:"posts"`
	// NextPage is the next one-based page number, or zero if the page is terminal.
	NextPage int `json:"nextPage"`
}

// ListPostsOperation reads Halo article metadata.
type ListPostsOperation struct{ client *Client }

// Definition returns the generated article-list Query contract.
func (ListPostsOperation) Definition() sdkgo.QueryDefinition { return ListPostsDefinition }

// Invoke lists article metadata and classifies provider and transport outcomes.
func (operation ListPostsOperation) Invoke(call sdkgo.Call, input ListPostsInput) sdkgo.QueryAttempt[ListPostsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return postCatalogBranch(ListPostsBranchDefect, sdkgo.FailureAuthentication, "Halo token is unavailable or invalid")
	}
	page, pageSize := input.Page, input.PageSize
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if page < 1 || page > 50000 || pageSize < 1 || pageSize > 100 {
		return postCatalogBranch(ListPostsBranchDefect, sdkgo.FailureValidation, "page or pageSize is outside the supported range")
	}
	values := url.Values{}
	values.Set("page", strconv.Itoa(page))
	values.Set("size", strconv.Itoa(pageSize))
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+postsPath+"?"+values.Encode(), nil)
	if err != nil {
		return postCatalogBranch(ListPostsBranchDefect, sdkgo.FailureValidation, "article-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return postCatalogRetry(sdkgo.FailureAvailability, "Halo request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return postCatalogRetry(sdkgo.FailureRateLimit, "Halo rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return postCatalogRetry(sdkgo.FailureAvailability, "Halo temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return postCatalogBranch(ListPostsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "article response exceeded the configured limit")
	}
	if err != nil {
		return postCatalogRetry(sdkgo.FailureAvailability, "article response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return postCatalogBranch(ListPostsBranchProviderRejected, kind, "Halo rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				Title   string `json:"title"`
				Slug    string `json:"slug"`
				Publish bool   `json:"publish"`
				Visible string `json:"visible"`
			} `json:"spec"`
		} `json:"items"`
		HasNext *bool `json:"hasNext"`
		Page    int   `json:"page"`
		Size    int   `json:"size"`
		Total   int64 `json:"total"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Items == nil || wire.HasNext == nil || wire.Page != page || wire.Size != pageSize || wire.Total < 0 || len(wire.Items) > pageSize {
		return postCatalogBranch(ListPostsBranchInvalidResponse, sdkgo.FailureProtocol, "Halo returned invalid article data")
	}
	posts := make([]PostSummary, 0, len(wire.Items))
	for _, item := range wire.Items {
		if item.Metadata.Name == "" || len(item.Metadata.Name) > 512 || item.Spec.Title == "" || len(item.Spec.Title) > 1024 || item.Spec.Slug == "" || len(item.Spec.Slug) > 1024 || strings.TrimSpace(item.Spec.Slug) != item.Spec.Slug || (item.Spec.Visible != "PUBLIC" && item.Spec.Visible != "INTERNAL" && item.Spec.Visible != "PRIVATE") {
			return postCatalogBranch(ListPostsBranchInvalidResponse, sdkgo.FailureProtocol, "Halo returned an invalid article")
		}
		posts = append(posts, PostSummary{Name: item.Metadata.Name, Title: item.Spec.Title, Slug: item.Spec.Slug, Published: item.Spec.Publish, Visibility: item.Spec.Visible})
	}
	nextPage := 0
	if *wire.HasNext {
		if page == 50000 {
			return postCatalogBranch(ListPostsBranchInvalidResponse, sdkgo.FailureProtocol, "Halo pagination exceeds the supported range")
		}
		nextPage = page + 1
	}
	return sdkgo.NewQueryBranch(ListPostsBranchListed, ListPostsOutput{Posts: posts, NextPage: nextPage}, nil, sdkgo.Receipt{})
}
func isValidAPIKey(value string) bool {
	if !strings.HasPrefix(value, "pat_") || len(value) > 512 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func postCatalogBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListPostsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "fit2cloud", Operation: "listPosts", Message: message}
	return sdkgo.NewQueryBranch(branch, ListPostsOutput{}, &failure, sdkgo.Receipt{})
}
func postCatalogRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListPostsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "fit2cloud", Operation: "listPosts", Message: message}
	return sdkgo.NewQueryRetry[ListPostsOutput](failure, delay)
}
