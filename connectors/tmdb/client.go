// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package tmdb implements the TMDB JSON API as typed Dex connector Steps.
package tmdb

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a client dependency during construction.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport; New copies it and disables redirects.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable HTTP settings and resolves credentials for each call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// SearchMoviesInput searches movie titles.
type SearchMoviesInput struct {
	// Query is the provider search expression.
	Query string `json:"query"`
	// Page is the one-based movie search page.
	Page int `json:"page"`
}

// GetMovieInput identifies a movie by TMDB ID.
type GetMovieInput struct {
	// ID is the provider's stable resource identifier.
	ID int64 `json:"id"`
}

// Movie contains selected public movie fields.
type Movie struct {
	// ID is the provider's stable resource identifier.
	ID int64 `json:"id"`
	// Title is the provider's display title.
	Title string `json:"title"`
	// Overview is the movie summary text.
	Overview string `json:"overview"`
	// ReleaseDate is the movie release date.
	ReleaseDate string `json:"release_date"`
}

// MoviePage is one search results page.
type MoviePage struct {
	// Page is the one-based movie search page.
	Page int `json:"page"`
	// TotalPages is the number of matching search pages.
	TotalPages int `json:"total_pages"`
	// TotalResults is the number of matching movies.
	TotalResults int `json:"total_results"`
	// Results contains one movie search page.
	Results []Movie `json:"results"`
}

// SearchMoviesOperation invokes TMDB's searchMovies endpoint.
type SearchMoviesOperation struct {
	client *Client
}

// Definition returns the generated searchMovies contract.
func (SearchMoviesOperation) Definition() sdkgo.QueryDefinition { return SearchMoviesDefinition }

// GetMovieOperation invokes TMDB's getMovie endpoint.
type GetMovieOperation struct {
	client *Client
}

// Definition returns the generated getMovie contract.
func (GetMovieOperation) Definition() sdkgo.QueryDefinition { return GetMovieDefinition }

// New validates configuration without a network call. Zero settings use manifest defaults.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("TMDB credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("TMDB base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// SearchMovies binds searchMovies to this client.
func (client *Client) SearchMovies() SearchMoviesOperation { return SearchMoviesOperation{client} }

// GetMovie binds getMovie to this client.
func (client *Client) GetMovie() GetMovieOperation { return GetMovieOperation{client} }

// Invoke runs this provider operation.
func (operation SearchMoviesOperation) Invoke(call sdkgo.Call, input SearchMoviesInput) sdkgo.QueryAttempt[MoviePage] {
	if strings.TrimSpace(input.Query) == "" || input.Page < 0 || input.Page > 500 {
		return queryFailure[MoviePage]("searchMovies", SearchMoviesBranchDefect, sdkgo.FailureValidation, "query is required and page must be 1–500")
	}
	query := url.Values{"query": {input.Query}}
	if input.Page > 0 {
		query.Set("page", strconv.Itoa(input.Page))
	}
	return runQuery(operation.client, call, "searchMovies", "/search/movie?"+query.Encode(), SearchMoviesBranchSearched, "", SearchMoviesBranchProviderRejected, SearchMoviesBranchInvalidResponse, SearchMoviesBranchDefect, func(body []byte) (MoviePage, error) {
		var page MoviePage
		err := json.Unmarshal(body, &page)
		if err == nil && (page.Results == nil || page.Page <= 0) {
			err = fmt.Errorf("movie page incomplete")
		}
		return page, err
	})
}

// Invoke runs this provider operation.
func (operation GetMovieOperation) Invoke(call sdkgo.Call, input GetMovieInput) sdkgo.QueryAttempt[Movie] {
	if input.ID <= 0 {
		return queryFailure[Movie]("getMovie", GetMovieBranchDefect, sdkgo.FailureValidation, "movie ID must be positive")
	}
	return runQuery(operation.client, call, "getMovie", "/movie/"+strconv.FormatInt(input.ID, 10), GetMovieBranchFound, GetMovieBranchNotFound, GetMovieBranchProviderRejected, GetMovieBranchInvalidResponse, GetMovieBranchDefect, func(body []byte) (Movie, error) {
		var movie Movie
		err := json.Unmarshal(body, &movie)
		if err == nil && movie.ID != input.ID {
			err = fmt.Errorf("movie ID mismatch")
		}
		return movie, err
	})
}

func (client *Client) newRequest(call sdkgo.Call, method, path string, body []byte) (*http.Request, error) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return nil, fmt.Errorf("credentials unavailable")
	}
	if err := credentials.Validate(); err != nil || strings.TrimSpace(credentials.APIKey.Reveal()) == "" || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return nil, fmt.Errorf("credentials invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("request invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("User-Agent", "superdurable-tmdb/0.1")
	return request, nil
}

func runQuery[T any](client *Client, call sdkgo.Call, operation, path string, success, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	request, err := client.newRequest(call, http.MethodGet, path, nil)
	if err != nil {
		return queryFailure[T](operation, defect, sdkgo.FailureAuthentication, "connection credentials or request are invalid")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider request failed"), 0)
	}
	// A read-only response close cannot affect the provider outcome.
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[T](newFailure(operation, sdkgo.FailureRateLimit, "provider rate limited the request"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewQueryRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))

	case response.StatusCode == http.StatusNotFound && notFound != "":
		return queryFailure[T](operation, notFound, sdkgo.FailureNotFound, "provider resource was not found")
	case response.StatusCode != http.StatusOK:
		return queryFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "provider rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return queryFailure[T](operation, invalid, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider response was interrupted"), 0)
	}
	value, err := decode(content)
	if err != nil {
		return queryFailure[T](operation, invalid, sdkgo.FailureProtocol, "provider response is incomplete or malformed")
	}
	return sdkgo.NewQueryBranch(success, value, nil, sdkgo.Receipt{})
}

func runMutation[T any](client *Client, call sdkgo.Call, operation, method, path string, body []byte, extraHeader, extraValue string, success, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.MutationAttempt[T] {
	request, err := client.newRequest(call, method, path, body)
	if err != nil {
		return mutationFailure[T](operation, defect, sdkgo.FailureAuthentication, "connection credentials or request are invalid")
	}
	if extraHeader != "" {
		request.Header.Set(extraHeader, extraValue)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider request failed"), 0)
	}
	// Closing the response cannot change a completed provider write.
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureRateLimit, "provider rate limited the request"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusConflict:
		conflict, _ := providerhttp.ReadBoundedBody(response.Body, providerhttp.MaxErrorBodyBytes)
		names := providerhttp.ReadErrorTokens(conflict, []string{"/name"})
		if len(names) == 1 && names[0] == "invalid_idempotent_request" {
			return mutationFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "idempotency key was used for a different request")
		}
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider is processing the same request"), 0)
	case response.StatusCode == http.StatusNotFound && notFound != "":
		return mutationFailure[T](operation, notFound, sdkgo.FailureNotFound, "provider resource was not found")
	case response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusAccepted:
		return mutationFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "provider rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return mutationFailure[T](operation, invalid, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider response was interrupted"), 0)
	}
	value, err := decode(content)
	if err != nil {
		return mutationFailure[T](operation, invalid, sdkgo.FailureProtocol, "provider response is incomplete or malformed")
	}
	return sdkgo.NewMutationBranch(success, value, nil, sdkgo.Receipt{})
}

func queryFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	var zero T
	failure := newFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}
func mutationFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := newFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}
func newFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "tmdb", Operation: operation, Message: message}
}
