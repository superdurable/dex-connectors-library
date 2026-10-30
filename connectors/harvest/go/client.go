// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package harvest implements read-only Harvest API Queries.
package harvest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable HTTP dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport; New copies it and disables redirects.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable HTTP configuration and resolves credentials per call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	accountID        string
}

// New validates configuration without contacting Harvest.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Harvest credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Harvest base URL: %w", err)
	}
	if !decimalID.MatchString(config.AccountID) {
		return nil, fmt.Errorf("Harvest accountId must be a decimal ID")
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials, accountID: config.AccountID}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var (
	decimalID = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	safeKey   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

// ListProjectsInput selects a Harvest project page; Cursor comes from the prior result.
type ListProjectsInput struct {
	// Cursor is the opaque token from the prior page; blank selects the first.
	Cursor string `json:"cursor,omitempty"`
	// PerPage caps records in one page; zero uses 100.
	PerPage int `json:"perPage,omitempty"`
}

// Project is a Harvest project identity, name, and active state.
type Project struct {
	// ID is the provider-assigned resource identity.
	ID int64 `json:"id"`
	// Name is the resource display name.
	Name string `json:"name"`
	// IsActive reports whether the project is active.
	IsActive bool `json:"isActive"`
}

// ProjectPage contains projects and a cursor for the next page.
type ProjectPage struct {
	// Projects contains the returned Harvest projects.
	Projects []Project `json:"projects"`
	// NextCursor selects the next page; blank means this was the last page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// ListTimeEntriesInput selects a Harvest time-entry page; Cursor comes from the prior result.
type ListTimeEntriesInput struct {
	// Cursor is the opaque token from the prior page; blank selects the first.
	Cursor string `json:"cursor,omitempty"`
	// PerPage caps records in one page; zero uses 100.
	PerPage int `json:"perPage,omitempty"`
}

// TimeEntry is a tracked-time record with its project identity.
type TimeEntry struct {
	// ID is the provider-assigned resource identity.
	ID int64 `json:"id"`
	// Project identifies the time entry's Harvest project.
	Project struct {
		// ID is the provider-assigned resource identity.
		ID int64 `json:"id"`
	} `json:"project"`
	// Hours is the tracked duration in decimal hours.
	Hours float64 `json:"hours"`
	// SpentDate is the tracked calendar day in YYYY-MM-DD format.
	SpentDate string `json:"spentDate"`
}

// TimeEntryPage contains tracked time and a cursor for the next page.
type TimeEntryPage struct {
	// TimeEntries contains the returned Harvest time records.
	TimeEntries []TimeEntry `json:"timeEntries"`
	// NextCursor selects the next page; blank means this was the last page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// ListProjects returns the bound listProjects Query.
func (client *Client) ListProjects() ListProjectsOperation { return ListProjectsOperation{client} }

// ListProjectsOperation performs one Harvest read.
type ListProjectsOperation struct{ client *Client }

// Definition returns the manifest-generated operation contract.
func (ListProjectsOperation) Definition() sdkgo.QueryDefinition { return ListProjectsDefinition }

// Invoke validates input and classifies the HTTP result.
func (operation ListProjectsOperation) Invoke(call sdkgo.Call, input ListProjectsInput) sdkgo.QueryAttempt[ProjectPage] {
	if err := validateHarvestPage(input.Cursor, input.PerPage); err != nil {
		return failedBranch[ProjectPage]("listProjects", ListProjectsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	path := harvestPagePath("/projects", input.Cursor, input.PerPage)
	return invokeGet(operation.client, call, "listProjects", path, ListProjectsBranchListed, ListProjectsBranchProviderRejected, decodeListProjects)
}
func decodeListProjects(body []byte) (ProjectPage, error) {
	var wire struct {
		// Projects contains the returned Harvest projects.
		Projects []struct {
			// ID is the provider-assigned resource identity.
			ID int64 `json:"id"`
			// Name is the resource display name.
			Name string `json:"name"`
			// IsActive reports whether the project is active.
			IsActive bool `json:"is_active"`
		} `json:"projects"`
		Links struct {
			Next *string `json:"next"`
		} `json:"links"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return ProjectPage{}, err
	}
	if wire.Projects == nil {
		return ProjectPage{}, fmt.Errorf("missing projects")
	}
	next, err := nextHarvestCursor(wire.Links.Next, "/projects")
	if err != nil {
		return ProjectPage{}, err
	}
	page := ProjectPage{Projects: make([]Project, 0, len(wire.Projects)), NextCursor: next}
	for _, p := range wire.Projects {
		page.Projects = append(page.Projects, Project{ID: p.ID, Name: p.Name, IsActive: p.IsActive})
	}
	return page, nil
}

// ListTimeEntries returns the bound listTimeEntries Query.
func (client *Client) ListTimeEntries() ListTimeEntriesOperation {
	return ListTimeEntriesOperation{client}
}

// ListTimeEntriesOperation performs one Harvest read.
type ListTimeEntriesOperation struct{ client *Client }

// Definition returns the manifest-generated operation contract.
func (ListTimeEntriesOperation) Definition() sdkgo.QueryDefinition { return ListTimeEntriesDefinition }

// Invoke validates input and classifies the HTTP result.
func (operation ListTimeEntriesOperation) Invoke(call sdkgo.Call, input ListTimeEntriesInput) sdkgo.QueryAttempt[TimeEntryPage] {
	if err := validateHarvestPage(input.Cursor, input.PerPage); err != nil {
		return failedBranch[TimeEntryPage]("listTimeEntries", ListTimeEntriesBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	path := harvestPagePath("/time_entries", input.Cursor, input.PerPage)
	return invokeGet(operation.client, call, "listTimeEntries", path, ListTimeEntriesBranchListed, ListTimeEntriesBranchProviderRejected, decodeListTimeEntries)
}
func decodeListTimeEntries(body []byte) (TimeEntryPage, error) {
	var wire struct {
		// TimeEntries contains the returned Harvest time records.
		TimeEntries []struct {
			// ID is the provider-assigned resource identity.
			ID int64 `json:"id"`
			// Project identifies the time entry's Harvest project.
			Project struct {
				// ID is the provider-assigned resource identity.
				ID int64 `json:"id"`
			} `json:"project"`
			// Hours is the tracked duration in decimal hours.
			Hours float64 `json:"hours"`
			// SpentDate is the tracked calendar day in YYYY-MM-DD format.
			SpentDate string `json:"spent_date"`
		} `json:"time_entries"`
		Links struct {
			Next *string `json:"next"`
		} `json:"links"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TimeEntryPage{}, err
	}
	if wire.TimeEntries == nil {
		return TimeEntryPage{}, fmt.Errorf("missing time_entries")
	}
	next, err := nextHarvestCursor(wire.Links.Next, "/time_entries")
	if err != nil {
		return TimeEntryPage{}, err
	}
	page := TimeEntryPage{TimeEntries: make([]TimeEntry, 0, len(wire.TimeEntries)), NextCursor: next}
	for _, e := range wire.TimeEntries {
		page.TimeEntries = append(page.TimeEntries, TimeEntry{ID: e.ID, Project: e.Project, Hours: e.Hours, SpentDate: e.SpentDate})
	}
	return page, nil
}

func validateHarvestPage(cursor string, perPage int) error {
	if perPage < 0 || perPage > 2000 {
		return fmt.Errorf("perPage must be 1 to 2000 or zero")
	}
	if cursor != "" && (!safeKey.MatchString(cursor) || len(cursor) > 128) {
		return fmt.Errorf("cursor is invalid")
	}
	return nil
}
func harvestPagePath(path, cursor string, perPage int) string {
	size := perPage
	if size == 0 {
		size = 100
	}
	values := url.Values{"per_page": {strconv.Itoa(size)}}
	if cursor != "" {
		values.Set("cursor", cursor)
	}
	return path + "?" + values.Encode()
}
func nextHarvestCursor(next *string, path string) (string, error) {
	if next == nil || *next == "" {
		return "", nil
	}
	parsed, err := url.Parse(*next)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "https" || parsed.Host != "api.harvestapp.com" || parsed.Path != "/v2"+path || parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("unexpected Harvest next URL")
	}
	cursor := parsed.Query().Get("cursor")
	if !safeKey.MatchString(cursor) {
		return "", fmt.Errorf("invalid Harvest cursor")
	}
	return cursor, nil
}

func invokeGet[T any](client *Client, call sdkgo.Call, operationID, path string, successBranch, notFoundBranch sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return failedBranch[T](operationID, "defect", sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if err := credentials.Validate(); err != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return failedBranch[T](operationID, "defect", sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return failedBranch[T](operationID, "defect", sdkgo.FailureLocalDefect, "request cannot be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Harvest-Account-Id", client.accountID)
	request.Header.Set("User-Agent", "superdurable-harvest-connector")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return retryBranch[T](operationID, sdkgo.FailureAvailability, "provider request failed", 0)
	}
	// Closing a read response cannot change its outcome.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		switch {
		case response.StatusCode == http.StatusTooManyRequests:
			return retryBranch[T](operationID, sdkgo.FailureRateLimit, "provider rate limit", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
		case response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= http.StatusInternalServerError:
			return retryBranch[T](operationID, sdkgo.FailureAvailability, "provider temporarily unavailable", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
		case response.StatusCode == http.StatusNotFound && notFoundBranch != "providerRejected":
			return failedBranch[T](operationID, notFoundBranch, sdkgo.FailureNotFound, "provider resource was not found")
		case response.StatusCode == http.StatusUnauthorized:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureAuthentication, "provider rejected the API key")
		case response.StatusCode == http.StatusForbidden:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureAuthorization, "provider denied the request")
		default:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureProviderRejection, "provider rejected HTTP "+strconv.Itoa(response.StatusCode))
		}
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedBranch[T](operationID, "invalidResponse", sdkgo.FailureResponseTooLarge, "provider response exceeded configured limit")
	}
	if err != nil {
		return retryBranch[T](operationID, sdkgo.FailureAvailability, "provider response was interrupted", 0)
	}
	value, err := decode(body)
	if err != nil {
		return failedBranch[T](operationID, "invalidResponse", sdkgo.FailureProtocol, "provider returned unusable JSON")
	}
	return sdkgo.NewQueryBranch(successBranch, value, nil, sdkgo.Receipt{})
}
func failedBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	var zero T
	failure := sdkgo.Failure{Kind: kind, Provider: "harvest", Operation: operationID, Message: message}
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}
func retryBranch[T any](operationID string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "harvest", Operation: operationID, Message: message}
	return sdkgo.NewQueryRetry[T](failure, after)
}
