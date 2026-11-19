// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UserMetadata contains the public identity and email of one Copper user.
type UserMetadata struct {
	// ID is the stable Copper user identifier.
	ID int64 `json:"id"`
	// Name is the user's display name.
	Name string `json:"name"`
	// Email is the user's account email.
	Email string `json:"email"`
}

// ListUsersInput selects one Copper users page.
type ListUsersInput struct {
	// PageNumber starts at one; zero uses Copper's first page.
	PageNumber int `json:"pageNumber,omitempty"`
	// PageSize is 1 through 200; zero uses Copper's default of 20.
	PageSize int `json:"pageSize,omitempty"`
}

// UsersPage contains one returned page of user summaries.
type UsersPage struct {
	// Users are the records returned by Copper for this page.
	Users []UserMetadata `json:"users"`
}

// ListUsersOperation searches Copper users without changing them.
type ListUsersOperation struct{ client *Client }

// ListUsers binds the Query to this client.
func (client *Client) ListUsers() ListUsersOperation {
	return ListUsersOperation{client}
}

// Definition returns the generated Query contract.
func (ListUsersOperation) Definition() sdkgo.QueryDefinition {
	return ListUsersDefinition
}

// Invoke reads one page through Copper's users search endpoint.
func (operation ListUsersOperation) Invoke(call sdkgo.Call, input ListUsersInput) sdkgo.QueryAttempt[UsersPage] {
	if input.PageNumber < 0 || input.PageNumber > 5000 || input.PageSize < 0 || input.PageSize > 200 {
		return listUsersBranch(ListUsersBranchDefect, sdkgo.FailureValidation, "invalid users page or filter")
	}
	request := struct {
		PageNumber int `json:"page_number,omitempty"`
		PageSize   int `json:"page_size,omitempty"`
	}{PageNumber: input.PageNumber, PageSize: input.PageSize}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return listUsersBranch(ListUsersBranchDefect, sdkgo.FailureLocalDefect, "users search could not be encoded")
	}
	response := operation.client.personRequest(call.Context, call, http.MethodPost, "/users/search", requestBody)
	if errors.Is(response.err, errCredential) {
		return listUsersBranch(ListUsersBranchDefect, sdkgo.FailureAuthentication, "Copper credentials are unavailable or invalid")
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[UsersPage](queryFailure("listUsers", sdkgo.FailureRateLimit, "Copper rate limited users search"), response.retryAfter)
	}
	if response.status == http.StatusServiceUnavailable || response.status >= 500 || errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[UsersPage](queryFailure("listUsers", sdkgo.FailureAvailability, "Copper users search is unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listUsersBranch(ListUsersBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Copper users response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[UsersPage](queryFailure("listUsers", sdkgo.FailureTransport, "Copper users response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return listUsersBranch(ListUsersBranchProviderRejected, sdkgo.FailureProviderRejection, "Copper rejected HTTP "+strconv.Itoa(response.status))
	}
	var records []json.RawMessage
	if json.Unmarshal(response.body, &records) != nil || records == nil {
		return listUsersBranch(ListUsersBranchInvalidResponse, sdkgo.FailureProtocol, "Copper returned invalid users JSON")
	}
	page := UsersPage{Users: make([]UserMetadata, 0, len(records))}
	for _, record := range records {
		user, decodeError := decodeUser(record)
		if decodeError != nil {
			return listUsersBranch(ListUsersBranchInvalidResponse, sdkgo.FailureProtocol, "Copper returned an invalid user")
		}
		page.Users = append(page.Users, user)
	}
	return sdkgo.NewQueryBranch(ListUsersBranchListed, page, nil, sdkgo.Receipt{})
}

func listUsersBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[UsersPage] {
	failure := queryFailure("listUsers", kind, message)
	return sdkgo.NewQueryBranch(branch, UsersPage{}, &failure, sdkgo.Receipt{})
}

func decodeUser(body []byte) (UserMetadata, error) {
	var user UserMetadata
	if err := json.Unmarshal(body, &user); err != nil {
		return UserMetadata{}, err
	}
	if user.ID <= 0 || strings.TrimSpace(user.Name) == "" || strings.TrimSpace(user.Email) == "" {
		return UserMetadata{}, fmt.Errorf("user identity missing")
	}
	return user, nil
}
