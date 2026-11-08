// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package wordpress reads and creates WordPress posts through typed Dex Steps.
// The runnable examples/post-inspection Flow shows connection and Worker registration.
package wordpress

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable client dependency.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a transport; New copies it and disables redirects.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable site settings and resolves WordPress credentials for each call. It is safe for concurrent use.
type Client struct {
	siteURL          string
	maxResponseBytes int64
	maxPostBytes     int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// GetPostInput selects one numeric WordPress post ID.
type GetPostInput struct {
	// PostID is a positive ID from WordPress Posts or the REST API.
	PostID int64 `json:"postId"`
}

// ListPostsInput selects one published-post page.
type ListPostsInput struct {
	// Page is one-based; zero selects page one.
	Page int `json:"page,omitempty"`
	// PerPage is 1 through 100; zero selects WordPress's default of ten.
	PerPage int `json:"perPage,omitempty"`
	// After is an RFC 3339 publication instant; empty has no lower bound.
	After string `json:"after,omitempty"`
}

// RenderedText holds provider HTML. Escape or sanitize it before embedding in another page.
type RenderedText struct {
	// Rendered is WordPress's rendered HTML, not plain text.
	Rendered string `json:"rendered"`
}

// Post is a published WordPress post. Title and Content contain untrusted provider HTML.
type Post struct {
	// ID is the stable numeric WordPress post ID.
	ID int64 `json:"id"`
	// Slug is the current URL slug and can change after an edit.
	Slug string `json:"slug"`
	// Status is publish for a post returned by these operations.
	Status string `json:"status"`
	// Link is the provider's public post URL.
	Link string `json:"link"`
	// Title is the rendered title HTML.
	Title RenderedText `json:"title"`
	// Content is the rendered body HTML.
	Content RenderedText `json:"content"`
	// DateGMT is WordPress's UTC publication time without a zone suffix.
	DateGMT string `json:"dateGMT"`
}

// PostPage is one published-post page and WordPress's total-count headers.
type PostPage struct {
	// Posts contains this page in WordPress's order, possibly empty.
	Posts []Post `json:"posts"`
	// Page is the one-based page requested.
	Page int `json:"page"`
	// Total is the matching post count reported by WordPress.
	Total int `json:"total"`
	// TotalPages is the page count reported by WordPress.
	TotalPages int `json:"totalPages"`
}

// GetPostOperation reads one published post.
type GetPostOperation struct{ client *Client }

// ListPostsOperation reads one page of published posts.
type ListPostsOperation struct{ client *Client }

// New validates site configuration without contacting WordPress. Credentials are resolved for each call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("WordPress credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	siteURL, err := providerhttp.ValidateBaseURL(config.SiteURL)
	if err != nil {
		return nil, fmt.Errorf("WordPress site URL: %w", err)
	}
	if strings.HasSuffix(siteURL, "/wp-json") {
		return nil, fmt.Errorf("WordPress site URL must omit /wp-json")
	}
	client := &Client{siteURL: siteURL, maxResponseBytes: config.MaxResponseBytes, maxPostBytes: config.MaxPostBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("WordPress client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GetPost returns the typed post lookup Query bound to this client.
func (client *Client) GetPost() GetPostOperation { return GetPostOperation{client} }

// ListPosts returns the typed published-post list Query bound to this client.
func (client *Client) ListPosts() ListPostsOperation { return ListPostsOperation{client} }

// CreatePost returns the typed post-creation Mutation bound to this client.
func (client *Client) CreatePost() CreatePostOperation { return CreatePostOperation{client} }

// Definition returns the manifest-generated post lookup contract.
func (GetPostOperation) Definition() sdkgo.QueryDefinition { return GetPostDefinition }

// Definition returns the manifest-generated post-list contract.
func (ListPostsOperation) Definition() sdkgo.QueryDefinition { return ListPostsDefinition }

// Invoke reads a published post by numeric ID. A visible draft selects notFound instead of returning private content.
func (operation GetPostOperation) Invoke(call sdkgo.Call, input GetPostInput) sdkgo.QueryAttempt[Post] {
	if input.PostID <= 0 {
		return failedQuery[Post]("getPost", GetPostBranchDefect, sdkgo.FailureValidation, "postId must be positive")
	}
	return invokeWordPressQuery(operation.client, call, "getPost", "/wp-json/wp/v2/posts/"+strconv.FormatInt(input.PostID, 10), nil,
		queryBranches{found: GetPostBranchFound, notFound: GetPostBranchNotFound, rejected: GetPostBranchProviderRejected, invalid: GetPostBranchInvalidResponse, defect: GetPostBranchDefect},
		func(body []byte, _ http.Header) (Post, error) { return decodePublishedPost(body) })
}

// Invoke lists a bounded page of published posts. After is validated before any provider request.
func (operation ListPostsOperation) Invoke(call sdkgo.Call, input ListPostsInput) sdkgo.QueryAttempt[PostPage] {
	if input.Page < 0 || input.PerPage < 0 || input.PerPage > 100 {
		return failedQuery[PostPage]("listPosts", ListPostsBranchDefect, sdkgo.FailureValidation, "page must be nonnegative and perPage must be at most 100")
	}
	if input.After != "" {
		if _, err := time.Parse(time.RFC3339Nano, input.After); err != nil {
			return failedQuery[PostPage]("listPosts", ListPostsBranchDefect, sdkgo.FailureValidation, "after must be an RFC 3339 instant")
		}
	}
	page := input.Page
	if page == 0 {
		page = 1
	}
	query := url.Values{"status": {"publish"}, "page": {strconv.Itoa(page)}}
	if input.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(input.PerPage))
	}
	if input.After != "" {
		query.Set("after", input.After)
	}
	return invokeWordPressQuery(operation.client, call, "listPosts", "/wp-json/wp/v2/posts", query,
		queryBranches{found: ListPostsBranchListed, rejected: ListPostsBranchProviderRejected, invalid: ListPostsBranchInvalidResponse, defect: ListPostsBranchDefect},
		func(body []byte, headers http.Header) (PostPage, error) { return decodePostPage(body, headers, page) })
}

type queryBranches struct{ found, notFound, rejected, invalid, defect sdkgo.BranchID }

var errPostNotPublished = errors.New("post is not published")

func invokeWordPressQuery[T any](client *Client, call sdkgo.Call, operationID, path string, query url.Values, branches queryBranches, decode func([]byte, http.Header) (T, error)) sdkgo.QueryAttempt[T] {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return failedQuery[T](operationID, branches.defect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	password := strings.ReplaceAll(credentials.ApplicationPassword.Reveal(), " ", "")
	if !validWordPressUsername(credentials.Username) || !providerhttp.IsHeaderSafeCredential(password) {
		return failedQuery[T](operationID, branches.defect, sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	endpoint := client.siteURL + path
	if query != nil {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return failedQuery[T](operationID, branches.defect, sdkgo.FailureLocalDefect, "WordPress request could not be built")
	}
	request.SetBasicAuth(credentials.Username, password)
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return retryQuery[T](operationID, sdkgo.FailureAvailability, "WordPress request failed", 0)
	}
	// Closing a read-only response cannot change the query outcome.
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return retryQuery[T](operationID, sdkgo.FailureRateLimit, "WordPress rate limited the request", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= http.StatusInternalServerError:
		return retryQuery[T](operationID, sdkgo.FailureAvailability, "WordPress is temporarily unavailable", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusNotFound && branches.notFound != "":
		return failedQuery[T](operationID, branches.notFound, sdkgo.FailureNotFound, "WordPress did not find the post")
	case response.StatusCode == http.StatusUnauthorized:
		return failedQuery[T](operationID, branches.rejected, sdkgo.FailureAuthentication, "WordPress rejected the application password")
	case response.StatusCode == http.StatusForbidden:
		return failedQuery[T](operationID, branches.rejected, sdkgo.FailureAuthorization, "WordPress denied access")
	case response.StatusCode != http.StatusOK:
		return failedQuery[T](operationID, branches.rejected, sdkgo.FailureProviderRejection, "WordPress rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedQuery[T](operationID, branches.invalid, sdkgo.FailureResponseTooLarge, "WordPress response exceeded the configured limit")
	}
	if err != nil {
		return retryQuery[T](operationID, sdkgo.FailureAvailability, "WordPress response was interrupted", 0)
	}
	value, err := decode(body, response.Header)
	if errors.Is(err, errPostNotPublished) && branches.notFound != "" {
		return failedQuery[T](operationID, branches.notFound, sdkgo.FailureNotFound, "WordPress post is not published")
	}
	if err != nil {
		return failedQuery[T](operationID, branches.invalid, sdkgo.FailureProtocol, "WordPress returned unusable post data")
	}
	return sdkgo.NewQueryBranch(branches.found, value, nil, sdkgo.Receipt{})
}

func decodePublishedPost(body []byte) (Post, error) {
	var wire struct {
		ID      int64        `json:"id"`
		Slug    string       `json:"slug"`
		Status  string       `json:"status"`
		Link    string       `json:"link"`
		Title   RenderedText `json:"title"`
		Content RenderedText `json:"content"`
		DateGMT string       `json:"date_gmt"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return Post{}, err
	}
	if wire.Status != "publish" {
		return Post{}, errPostNotPublished
	}
	if wire.ID <= 0 || wire.Slug == "" || wire.Link == "" || wire.DateGMT == "" {
		return Post{}, fmt.Errorf("incomplete post")
	}
	if _, err := time.Parse("2006-01-02T15:04:05", wire.DateGMT); err != nil {
		return Post{}, fmt.Errorf("invalid WordPress publication time")
	}
	return Post{ID: wire.ID, Slug: wire.Slug, Status: wire.Status, Link: wire.Link, Title: wire.Title, Content: wire.Content, DateGMT: wire.DateGMT}, nil
}

func decodePostPage(body []byte, headers http.Header, page int) (PostPage, error) {
	var posts []json.RawMessage
	if err := json.Unmarshal(body, &posts); err != nil || posts == nil {
		return PostPage{}, fmt.Errorf("invalid post page")
	}
	total, err := strconv.Atoi(headers.Get("X-WP-Total"))
	if err != nil || total < 0 {
		return PostPage{}, fmt.Errorf("invalid post total")
	}
	totalPages, err := strconv.Atoi(headers.Get("X-WP-TotalPages"))
	if err != nil || totalPages < 0 {
		return PostPage{}, fmt.Errorf("invalid post page count")
	}
	output := PostPage{Posts: make([]Post, 0, len(posts)), Page: page, Total: total, TotalPages: totalPages}
	for _, rawPost := range posts {
		post, err := decodePublishedPost(rawPost)
		if err != nil {
			return PostPage{}, err
		}
		output.Posts = append(output.Posts, post)
	}
	return output, nil
}

func validWordPressUsername(username string) bool {
	return username != "" && utf8.ValidString(username) && !strings.ContainsAny(username, ":\r\n\x00")
}

func failedQuery[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	var zero T
	failure := sdkgo.Failure{Kind: kind, Provider: "wordpress", Operation: operationID, Message: message}
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func retryQuery[T any](operationID string, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "wordpress", Operation: operationID, Message: message}
	return sdkgo.NewQueryRetry[T](failure, retryAfter)
}
