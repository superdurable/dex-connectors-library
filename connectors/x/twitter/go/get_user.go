// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package x

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

// GetUserOperation reads one public X profile.
type GetUserOperation struct{ client *Client }

// GetUser binds the Query to this client.
func (client *Client) GetUser() GetUserOperation { return GetUserOperation{client: client} }

// Definition returns the manifest-generated Query contract.
func (GetUserOperation) Definition() sdkgo.QueryDefinition { return GetUserDefinition }

// Invoke retrieves a user by their public handle.
func (operation GetUserOperation) Invoke(call sdkgo.Call, input GetUserInput) sdkgo.QueryAttempt[UserMetadata] {
	if !validUsername(input.Username) {
		return userBranch(GetUserBranchDefect, sdkgo.FailureValidation, "X username is invalid")
	}
	response := operation.client.get(call, "/users/by/username/"+url.PathEscape(input.Username))
	if errors.Is(response.err, errCredential) {
		return userBranch(GetUserBranchDefect, sdkgo.FailureAuthentication, "X credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return userRetry(sdkgo.FailureTransport, "X user request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return userRetry(sdkgo.FailureRateLimit, "X rate limited the lookup", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return userRetry(sdkgo.FailureAvailability, "X user lookup is temporarily unavailable", response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return userBranch(GetUserBranchNotFound, sdkgo.FailureNotFound, "X did not expose the user")
	}
	if response.status != http.StatusOK {
		return userBranch(GetUserBranchProviderRejected, sdkgo.FailureProviderRejection, "X rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return userBranch(GetUserBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "X user response exceeded the configured limit")
	}
	if response.err != nil {
		return userRetry(sdkgo.FailureTransport, "X user response was interrupted", 0)
	}
	var wire struct {
		Data   *UserMetadata `json:"data"`
		Errors []struct {
			Type string `json:"type"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil {
		return userBranch(GetUserBranchInvalidResponse, sdkgo.FailureProtocol, "X returned invalid user JSON")
	}
	if wire.Data == nil {
		for _, providerError := range wire.Errors {
			switch postProblemType(providerError.Type) {
			case "resource-not-found":
				return userBranch(GetUserBranchNotFound, sdkgo.FailureNotFound, "X did not expose the user")
			case "not-authorized-for-resource", "client-forbidden":
				return userBranch(GetUserBranchProviderRejected, sdkgo.FailureProviderRejection, "X denied access to the user")
			}
		}
		return userBranch(GetUserBranchInvalidResponse, sdkgo.FailureProtocol, "X user data was missing")
	}
	if !validPostID(wire.Data.ID) || !strings.EqualFold(wire.Data.Username, input.Username) || strings.TrimSpace(wire.Data.Name) == "" {
		return userBranch(GetUserBranchInvalidResponse, sdkgo.FailureProtocol, "X returned an incomplete user")
	}
	return sdkgo.NewQueryBranch(GetUserBranchFound, *wire.Data, nil, sdkgo.Receipt{})
}

func validUsername(username string) bool {
	if len(username) == 0 || len(username) > 15 {
		return false
	}
	for _, character := range username {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func userFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "x-twitter", Operation: "getUser", Message: message}
}
func userBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[UserMetadata] {
	failure := userFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, UserMetadata{}, &failure, sdkgo.Receipt{})
}
func userRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[UserMetadata] {
	return sdkgo.NewQueryRetry[UserMetadata](userFailure(kind, message), delay)
}
