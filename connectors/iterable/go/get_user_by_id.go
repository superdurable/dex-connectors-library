// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package iterable

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetUserByIDInput selects a user by their stable Iterable user ID.
type GetUserByIDInput struct {
	// UserID is the ID shown on the user's Iterable profile.
	UserID string `json:"userId"`
}

// GetUserByIDOperation reads one user's identity by ID.
type GetUserByIDOperation struct{ client *Client }

// GetUserByID binds this Query to the client.
func (client *Client) GetUserByID() GetUserByIDOperation { return GetUserByIDOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetUserByIDOperation) Definition() sdkgo.QueryDefinition { return GetUserByIDDefinition }

// Invoke reads one user by ID and classifies retryable and definitive outcomes.
func (operation GetUserByIDOperation) Invoke(call sdkgo.Call, input GetUserByIDInput) sdkgo.QueryAttempt[UserIdentity] {
	if !validUserID(input.UserID) {
		return userIDBranch(GetUserByIDBranchDefect, sdkgo.FailureValidation, "user ID must be 1–256 printable path-safe characters")
	}
	response := operation.client.get(call, "/users/byUserId/"+url.PathEscape(input.UserID))
	if errors.Is(response.err, errCredential) {
		return userIDBranch(GetUserByIDBranchDefect, sdkgo.FailureAuthentication, "Iterable credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) || (response.err != nil && !errors.Is(response.err, providerhttp.ErrBodyTooLarge)) {
		return sdkgo.NewQueryRetry[UserIdentity](userIDFailure(sdkgo.FailureTransport, "Iterable request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[UserIdentity](userIDFailure(sdkgo.FailureRateLimit, "Iterable rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[UserIdentity](userIDFailure(sdkgo.FailureAvailability, "Iterable is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return userIDBranch(GetUserByIDBranchNotFound, sdkgo.FailureNotFound, "Iterable did not expose the user")
	}
	if response.status != http.StatusOK {
		return userIDBranch(GetUserByIDBranchProviderRejected, sdkgo.FailureProviderRejection, "Iterable rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return userIDBranch(GetUserByIDBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Iterable response exceeded the configured limit")
	}
	var payload struct {
		User struct {
			Email  string `json:"email"`
			UserID string `json:"userId"`
		} `json:"user"`
	}
	if err := json.Unmarshal(response.body, &payload); err != nil || payload.User.UserID != input.UserID || !validEmail(payload.User.Email) {
		return userIDBranch(GetUserByIDBranchInvalidResponse, sdkgo.FailureProtocol, "Iterable returned invalid user JSON")
	}
	return sdkgo.NewQueryBranch(GetUserByIDBranchFound, UserIdentity{Email: payload.User.Email, UserID: payload.User.UserID}, nil, sdkgo.Receipt{})
}

func userIDFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "iterable", Operation: "getUserByID", Message: message}
}

func userIDBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[UserIdentity] {
	failure := userIDFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, UserIdentity{}, &failure, sdkgo.Receipt{})
}
