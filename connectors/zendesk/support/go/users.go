// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// User is the end-user profile data this connector returns to a Flow.
type User struct {
	// ID is the positive Zendesk user ID.
	ID int64 `json:"id"`
	// Name is the user's display name.
	Name string `json:"name"`
	// Email is the user's primary email when one is available.
	Email string `json:"email,omitempty"`
	// ExternalID is the account-unique application identifier when assigned.
	ExternalID string `json:"externalId,omitempty"`
	// Role is the Zendesk role, usually end-user.
	Role string `json:"role,omitempty"`
	// OrganizationID is the default organization ID when assigned.
	OrganizationID int64 `json:"organizationId,omitempty"`
	// Phone is the user's phone number when one is available.
	Phone string `json:"phone,omitempty"`
	// Tags are the user's Zendesk tags when user tagging is enabled.
	Tags []string `json:"tags,omitempty"`
	// Suspended reports whether the user is suspended.
	Suspended bool `json:"suspended"`
}

// ListUsersInput selects one cursor page of users.
type ListUsersInput struct {
	// PageSize is 1 through 100; zero uses 25.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor is the previous page's NextCursor; blank begins at the first page.
	Cursor string `json:"cursor,omitempty"`
}

// ListUsersOutput contains one user page.
type ListUsersOutput struct {
	// Users is the page of users visible to the agent.
	Users []User `json:"users"`
	// NextCursor is blank when no later page exists.
	NextCursor string `json:"nextCursor"`
}

// GetUserInput identifies one user by ID.
type GetUserInput struct {
	// UserID is the positive Zendesk user ID.
	UserID int64 `json:"userId"`
}

// GetUserOutput contains one user.
type GetUserOutput struct {
	// User is the requested user.
	User User `json:"user"`
}

// SearchUsersInput selects one offset page of users matching a Zendesk query.
type SearchUsersInput struct {
	// Query is a nonempty Zendesk user search term or expression.
	Query string `json:"query"`
	// Page is 1 through 400; zero begins at page 1.
	Page int `json:"page,omitempty"`
	// PageSize is 1 through 100; zero uses 25.
	PageSize int `json:"pageSize,omitempty"`
}

// SearchUsersOutput contains one search page.
type SearchUsersOutput struct {
	// Users are the matched users on this page.
	Users []User `json:"users"`
	// NextPage is zero when no later page exists.
	NextPage int `json:"nextPage"`
}

type zendeskUserRecord struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Email          string   `json:"email"`
	ExternalID     string   `json:"external_id"`
	Role           string   `json:"role"`
	OrganizationID int64    `json:"organization_id"`
	Phone          string   `json:"phone"`
	Tags           []string `json:"tags"`
	Suspended      bool     `json:"suspended"`
}

// ListUsersOperation lists users.
type ListUsersOperation struct{ client *Client }

// GetUserOperation reads one user.
type GetUserOperation struct{ client *Client }

// SearchUsersOperation searches users.
type SearchUsersOperation struct{ client *Client }

// ListUsers binds the user list Query to this client.
func (client *Client) ListUsers() ListUsersOperation { return ListUsersOperation{client} }

// GetUser binds the user read Query to this client.
func (client *Client) GetUser() GetUserOperation { return GetUserOperation{client} }

// SearchUsers binds the user search Query to this client.
func (client *Client) SearchUsers() SearchUsersOperation { return SearchUsersOperation{client} }

// Definition returns the generated list-users Query contract.
func (ListUsersOperation) Definition() sdkgo.QueryDefinition { return ListUsersDefinition }

// Definition returns the generated get-user Query contract.
func (GetUserOperation) Definition() sdkgo.QueryDefinition { return GetUserDefinition }

// Definition returns the generated search-users Query contract.
func (SearchUsersOperation) Definition() sdkgo.QueryDefinition { return SearchUsersDefinition }

// Invoke reads one bounded cursor page of users.
func (operation ListUsersOperation) Invoke(call sdkgo.Call, input ListUsersInput) sdkgo.QueryAttempt[ListUsersOutput] {
	const operationName = "listUsers"
	if input.PageSize < 0 || input.PageSize > 100 || len(input.Cursor) > maximumCursorLength || strings.ContainsAny(input.Cursor, "\r\n\t") {
		return sdkgo.NewQueryBranch(ListUsersBranchDefect, ListUsersOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "pageSize or cursor is invalid"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListUsersBranchDefect, ListUsersOutput{}, failure, sdkgo.Receipt{})
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 25
	}
	query := url.Values{"page[size]": {strconv.Itoa(pageSize)}}
	if input.Cursor != "" {
		query.Set("page[after]", input.Cursor)
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: "/users", query: query})
	receipt := operation.client.receipt(call, result.response, 0)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewQueryRetry[ListUsersOutput](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(ListUsersBranchInvalidResponse, ListUsersOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(ListUsersBranchDefect, ListUsersOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(ListUsersBranchProviderRejected, ListUsersOutput{}, &result.failure, receipt)
	}
	users, nextCursor, err := decodeUserPage(result.response.body, pageSize)
	if err != nil {
		return sdkgo.NewQueryBranch(ListUsersBranchInvalidResponse, ListUsersOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned an invalid user page"), receipt)
	}
	return sdkgo.NewQueryBranch(ListUsersBranchListed, ListUsersOutput{Users: users, NextCursor: nextCursor}, nil, receipt)
}

// Invoke reads one user by ID.
func (operation GetUserOperation) Invoke(call sdkgo.Call, input GetUserInput) sdkgo.QueryAttempt[GetUserOutput] {
	const operationName = "getUser"
	if input.UserID < 1 {
		return sdkgo.NewQueryBranch(GetUserBranchDefect, GetUserOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "userId must be positive"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetUserBranchDefect, GetUserOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: userPath(input.UserID)})
	receipt := operation.client.receipt(call, result.response, input.UserID)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewQueryRetry[GetUserOutput](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewQueryBranch(GetUserBranchNotFound, GetUserOutput{}, &result.failure, receipt)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(GetUserBranchInvalidResponse, GetUserOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(GetUserBranchDefect, GetUserOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetUserBranchProviderRejected, GetUserOutput{}, &result.failure, receipt)
	}
	user, err := decodeUserEnvelope(result.response.body)
	if err != nil || user.ID != input.UserID {
		return sdkgo.NewQueryBranch(GetUserBranchInvalidResponse, GetUserOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned an invalid user"), receipt)
	}
	return sdkgo.NewQueryBranch(GetUserBranchFound, GetUserOutput{User: user}, nil, receipt)
}

// Invoke searches one bounded offset page of users.
func (operation SearchUsersOperation) Invoke(call sdkgo.Call, input SearchUsersInput) sdkgo.QueryAttempt[SearchUsersOutput] {
	const operationName = "searchUsers"
	queryText := strings.TrimSpace(input.Query)
	if queryText == "" || len(queryText) > 512 || strings.ContainsAny(queryText, "\r\n") || input.Page < 0 || input.Page > 400 || input.PageSize < 0 || input.PageSize > 100 {
		return sdkgo.NewQueryBranch(SearchUsersBranchDefect, SearchUsersOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "query, page, or pageSize is invalid"), sdkgo.Receipt{})
	}
	page := input.Page
	if page == 0 {
		page = 1
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 25
	}
	if page*pageSize > 10000 {
		return sdkgo.NewQueryBranch(SearchUsersBranchDefect, SearchUsersOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "Zendesk search stops at 10000 users"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewQueryBranch(SearchUsersBranchDefect, SearchUsersOutput{}, failure, sdkgo.Receipt{})
	}
	query := url.Values{"query": {queryText}, "page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(pageSize)}}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: "/users/search", query: query})
	receipt := operation.client.receipt(call, result.response, 0)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewQueryRetry[SearchUsersOutput](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(SearchUsersBranchInvalidResponse, SearchUsersOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(SearchUsersBranchDefect, SearchUsersOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(SearchUsersBranchProviderRejected, SearchUsersOutput{}, &result.failure, receipt)
	}
	var envelope struct {
		Users    []zendeskUserRecord `json:"users"`
		NextPage *string             `json:"next_page"`
	}
	if err := json.Unmarshal(result.response.body, &envelope); err != nil || envelope.Users == nil || len(envelope.Users) > pageSize {
		return sdkgo.NewQueryBranch(SearchUsersBranchInvalidResponse, SearchUsersOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned an invalid user search page"), receipt)
	}
	users, err := decodeUserRecords(envelope.Users)
	if err != nil {
		return sdkgo.NewQueryBranch(SearchUsersBranchInvalidResponse, SearchUsersOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned an invalid user"), receipt)
	}
	nextPage := 0
	if envelope.NextPage != nil && *envelope.NextPage != "" && page*pageSize < 10000 {
		nextPage = page + 1
	}
	return sdkgo.NewQueryBranch(SearchUsersBranchSearched, SearchUsersOutput{Users: users, NextPage: nextPage}, nil, receipt)
}

func decodeUserPage(body []byte, pageSize int) ([]User, string, error) {
	var envelope struct {
		Users []zendeskUserRecord `json:"users"`
		Meta  struct {
			HasMore     bool   `json:"has_more"`
			AfterCursor string `json:"after_cursor"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, "", err
	}
	if envelope.Users == nil || len(envelope.Users) > pageSize || (envelope.Meta.HasMore && envelope.Meta.AfterCursor == "") {
		return nil, "", errors.New("invalid user page")
	}
	users, err := decodeUserRecords(envelope.Users)
	if err != nil {
		return nil, "", err
	}
	if envelope.Meta.HasMore {
		return users, envelope.Meta.AfterCursor, nil
	}
	return users, "", nil
}

func decodeUserEnvelope(body []byte) (User, error) {
	var envelope struct {
		User *zendeskUserRecord `json:"user"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return User{}, err
	}
	if envelope.User == nil {
		return User{}, errors.New("missing user")
	}
	return decodeUserRecord(*envelope.User)
}

func decodeUserRecords(records []zendeskUserRecord) ([]User, error) {
	users := make([]User, 0, len(records))
	for _, record := range records {
		user, err := decodeUserRecord(record)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

func decodeUserRecord(record zendeskUserRecord) (User, error) {
	if record.ID < 1 || strings.TrimSpace(record.Name) == "" {
		return User{}, errors.New("user has no ID or name")
	}
	return User{ID: record.ID, Name: record.Name, Email: record.Email, ExternalID: record.ExternalID, Role: record.Role, OrganizationID: record.OrganizationID, Phone: record.Phone, Tags: record.Tags, Suspended: record.Suspended}, nil
}

func userPath(userID int64) string { return "/users/" + strconv.FormatInt(userID, 10) }
