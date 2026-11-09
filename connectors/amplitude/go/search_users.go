// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package amplitude

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SearchUsersInput identifies a user by ID, device ID, Amplitude ID, or user ID prefix.
type SearchUsersInput struct {
	// User is the non-secret search term accepted by Amplitude User Search.
	User string `json:"user"`
}

// UserSearchPage contains the matched user IDs and Amplitude's match type.
type UserSearchPage struct {
	// User is the search term supplied to Amplitude.
	User string `json:"user"`
	// UserIDs are the matched user IDs in provider order.
	UserIDs []string `json:"userIDs"`
	// MatchType names the matching strategy reported by Amplitude.
	MatchType string `json:"matchType"`
}

// SearchUsersOperation searches project users through the Dashboard REST API.
type SearchUsersOperation struct{ client *Client }

// SearchUsers binds the Query to this client.
func (client *Client) SearchUsers() SearchUsersOperation { return SearchUsersOperation{client} }

// Definition returns the manifest-generated Query worker.
func (SearchUsersOperation) Definition() sdkgo.QueryDefinition { return SearchUsersDefinition }

// Invoke performs one bounded search and classifies retryable and terminal outcomes.
func (operation SearchUsersOperation) Invoke(call sdkgo.Call, input SearchUsersInput) sdkgo.QueryAttempt[UserSearchPage] {
	if !validUserSearch(input.User) {
		return usersBranch(SearchUsersBranchDefect, sdkgo.FailureValidation, "user must contain 1–256 printable characters")
	}
	response := operation.client.get(call, input.User)
	if errors.Is(response.err, errCredential) {
		return usersBranch(SearchUsersBranchDefect, sdkgo.FailureAuthentication, "Amplitude credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[UserSearchPage](usersFailure(sdkgo.FailureTransport, "Amplitude request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[UserSearchPage](usersFailure(sdkgo.FailureRateLimit, "Amplitude rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[UserSearchPage](usersFailure(sdkgo.FailureAvailability, "Amplitude is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return usersBranch(SearchUsersBranchProviderRejected, sdkgo.FailureProviderRejection, "Amplitude rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return usersBranch(SearchUsersBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Amplitude response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[UserSearchPage](usersFailure(sdkgo.FailureTransport, "Amplitude response was interrupted"), 0)
	}
	page, err := decodeUsers(response.body, input.User)
	if err != nil {
		return usersBranch(SearchUsersBranchInvalidResponse, sdkgo.FailureProtocol, "Amplitude returned invalid user-search JSON")
	}
	return sdkgo.NewQueryBranch(SearchUsersBranchSearched, page, nil, sdkgo.Receipt{})
}

func validUserSearch(user string) bool {
	if len(user) == 0 || len(user) > 256 || !utf8.ValidString(user) || strings.TrimSpace(user) != user {
		return false
	}
	for _, character := range user {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func decodeUsers(body []byte, user string) (UserSearchPage, error) {
	var wire struct {
		Matches []struct {
			UserID string `json:"user_id"`
		} `json:"matches"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return UserSearchPage{}, err
	}
	if wire.Matches == nil || wire.Type == "" {
		return UserSearchPage{}, errors.New("user search page is missing")
	}
	page := UserSearchPage{User: user, UserIDs: make([]string, 0, len(wire.Matches)), MatchType: wire.Type}
	for _, match := range wire.Matches {
		if match.UserID == "" {
			return UserSearchPage{}, errors.New("matched user ID is missing")
		}
		page.UserIDs = append(page.UserIDs, match.UserID)
	}
	return page, nil
}

func usersFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amplitude", Operation: "searchUsers", Message: message}
}

func usersBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[UserSearchPage] {
	failure := usersFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, UserSearchPage{}, &failure, sdkgo.Receipt{})
}
