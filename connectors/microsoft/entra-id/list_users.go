// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	maxUserFilterLength = 2048
	maxUserSearchLength = 1024
	maxPageTokenLength  = 8192
	usersCollectionPath = "/v1.0/users"
)

// ListUsersInput selects one page of accounts in the tenant. Leave every field
// blank to list all accounts in Microsoft Graph's default order.
type ListUsersInput struct {
	// Filter is an OData $filter expression, such as accountEnabled eq false or startsWith(userPrincipalName,'ada').
	// Operators Microsoft documents as advanced, such as endsWith and ne, also need UsesAdvancedQuery.
	Filter string `json:"filter,omitempty"`
	// Search is a $search expression including its double quotes, such as "displayName:Ada"; it always uses
	// Microsoft's eventually consistent index, so a just-created account may be missing.
	Search string `json:"search,omitempty"`
	// UsesAdvancedQuery sends ConsistencyLevel: eventual with $count=true, which Microsoft requires for advanced
	// filters; results then come from an index that can lag recent changes.
	UsesAdvancedQuery bool `json:"usesAdvancedQuery,omitempty"`
	// PageSize is 1 to 999 accounts; zero uses the connection's listUsersPageSize.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the NextPageToken of the previous page. It carries the original query, so Filter, Search,
	// and PageSize must be blank when it is set; keep UsesAdvancedQuery as it was.
	PageToken string `json:"pageToken,omitempty"`
}

// ListUsersOutput is one page of accounts.
type ListUsersOutput struct {
	// Users lists the page's accounts; it may be empty.
	Users []User `json:"users"`
	// NextPageToken selects the following page; empty means this is the last page. It is Microsoft's
	// complete opaque nextLink URL, which Microsoft says not to take apart.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// ListUsersOperation implements the listUsers connector Query.
type ListUsersOperation struct{ client *Client }

type graphUserPage struct {
	Value    []graphUser `json:"value"`
	NextLink string      `json:"@odata.nextLink"`
}

var listUsersFailureBranches = failureBranches{
	notFound: ListUsersBranchProviderRejected, rejected: ListUsersBranchProviderRejected,
	invalidResponse: ListUsersBranchInvalidResponse, defect: ListUsersBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (ListUsersOperation) Definition() sdkgo.QueryDefinition { return ListUsersDefinition }

// Invoke requests one page of accounts and returns it with the next page token.
func (operation ListUsersOperation) Invoke(call sdkgo.Call, input ListUsersInput) sdkgo.QueryAttempt[ListUsersOutput] {
	const operationID = "listUsers"
	request, err := operation.buildListUsersRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListUsersBranchDefect, ListUsersOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListUsersBranchDefect, ListUsersOutput{}, failure, sdkgo.Receipt{})
	}
	exchange := operation.client.exchange(call, &credential, operationID, request)
	if exchange.outcome != exchangeSucceeded {
		return queryAttemptFromExchange[ListUsersOutput](operation.client, call, exchange, listUsersFailureBranches)
	}
	receipt := operation.client.receipt(call, exchange.response, "")
	output, err := decodeUserPage(exchange.response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(ListUsersBranchInvalidResponse, ListUsersOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned an invalid page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListUsersBranchListed, output, nil, receipt)
}

func (operation ListUsersOperation) buildListUsersRequest(input ListUsersInput) (graphRequest, error) {
	filter, search := strings.TrimSpace(input.Filter), strings.TrimSpace(input.Search)
	if pageToken := strings.TrimSpace(input.PageToken); pageToken != "" {
		if filter != "" || search != "" || input.PageSize != 0 {
			return graphRequest{}, errors.New("leave filter, search, and pageSize blank with pageToken, which carries the original query")
		}
		nextPageURL, err := validateNextPageURL(pageToken)
		if err != nil {
			return graphRequest{}, err
		}
		return graphRequest{method: http.MethodGet, nextPageURL: nextPageURL, usesEventualConsistency: input.UsesAdvancedQuery || nextPageNeedsEventualConsistency(nextPageURL)}, nil
	}
	query := url.Values{"$select": {userSelectedProperties}}
	if filter != "" {
		if len(filter) > maxUserFilterLength || strings.ContainsFunc(filter, isControlCharacter) {
			return graphRequest{}, fmt.Errorf("filter must be at most %d bytes without control characters", maxUserFilterLength)
		}
		query.Set("$filter", filter)
	}
	if search != "" {
		if len(search) > maxUserSearchLength || strings.ContainsFunc(search, isControlCharacter) || !strings.HasPrefix(search, `"`) {
			return graphRequest{}, fmt.Errorf(`search must be a quoted expression such as "displayName:Ada", at most %d bytes`, maxUserSearchLength)
		}
		query.Set("$search", search)
	}
	if input.UsesAdvancedQuery {
		query.Set("$count", "true")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = operation.client.listUsersPageSize
	}
	if pageSize < 1 || pageSize > maxListUsersPageSize {
		return graphRequest{}, fmt.Errorf("pageSize must be from 1 to %d", maxListUsersPageSize)
	}
	query.Set("$top", strconv.Itoa(pageSize))
	return graphRequest{method: http.MethodGet, path: "/users", query: query, usesEventualConsistency: input.UsesAdvancedQuery || search != ""}, nil
}

// validateNextPageURL accepts only a Microsoft Graph users nextLink, so a page token never sends the credential elsewhere.
func validateNextPageURL(value string) (string, error) {
	invalid := errors.New("pageToken must be the NextPageToken of a previous listUsers page")
	if len(value) > maxPageTokenLength || strings.ContainsFunc(value, isControlCharacter) {
		return "", invalid
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host != graphHost || parsed.User != nil || parsed.Fragment != "" ||
		parsed.Path != usersCollectionPath || parsed.RawQuery == "" {
		return "", invalid
	}
	return value, nil
}

// nextPageNeedsEventualConsistency keeps the header Microsoft requires when the original query used $search or $count.
func nextPageNeedsEventualConsistency(nextPageURL string) bool {
	parsed, err := url.Parse(nextPageURL)
	if err != nil {
		return false
	}
	query := parsed.Query()
	return query.Has("$search") || query.Has("$count")
}

func decodeUserPage(body []byte) (ListUsersOutput, error) {
	var page graphUserPage
	if err := json.Unmarshal(body, &page); err != nil {
		return ListUsersOutput{}, errors.New("page is not valid JSON")
	}
	output := ListUsersOutput{Users: make([]User, 0, len(page.Value))}
	for index, resource := range page.Value {
		user, err := convertUser(resource, true)
		if err != nil {
			return ListUsersOutput{}, fmt.Errorf("account %d: %w", index, err)
		}
		output.Users = append(output.Users, user)
	}
	if page.NextLink != "" {
		nextPageURL, err := validateNextPageURL(page.NextLink)
		if err != nil {
			return ListUsersOutput{}, errors.New("next page link is not a Microsoft Graph users URL")
		}
		output.NextPageToken = nextPageURL
	}
	return output, nil
}

func isControlCharacter(character rune) bool { return character < 0x20 || character == 0x7f }
