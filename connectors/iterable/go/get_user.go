// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package iterable

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetUserInput selects a user by a non-secret email address.
type GetUserInput struct {
	// Email is the user's email in an Iterable email-based or hybrid project.
	Email string `json:"email"`
}

// UserIdentity excludes arbitrary profile data fields.
type UserIdentity struct {
	// Email is the selected user's email address.
	Email string `json:"email"`
	// UserID is Iterable's optional stable user identifier.
	UserID string `json:"userId"`
}

// GetUserOperation reads one user's identity.
type GetUserOperation struct{ client *Client }

// GetUser binds the Query to this client.
func (client *Client) GetUser() GetUserOperation { return GetUserOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetUserOperation) Definition() sdkgo.QueryDefinition { return GetUserDefinition }

// Invoke reads one user and classifies retryable and definitive outcomes.
func (operation GetUserOperation) Invoke(call sdkgo.Call, input GetUserInput) sdkgo.QueryAttempt[UserIdentity] {
	if !validEmail(input.Email) {
		return userBranch(GetUserBranchDefect, sdkgo.FailureValidation, "email must be 1–256 printable characters with @")
	}
	response := operation.client.get(call, "/users/"+url.PathEscape(input.Email))
	if errors.Is(response.err, errCredential) {
		return userBranch(GetUserBranchDefect, sdkgo.FailureAuthentication, "Iterable credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[UserIdentity](userFailure(sdkgo.FailureTransport, "Iterable request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[UserIdentity](userFailure(sdkgo.FailureRateLimit, "Iterable rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[UserIdentity](userFailure(sdkgo.FailureAvailability, "Iterable is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return userBranch(GetUserBranchNotFound, sdkgo.FailureNotFound, "Iterable did not expose the user")
	}
	if response.status != http.StatusOK {
		return userBranch(GetUserBranchProviderRejected, sdkgo.FailureProviderRejection, "Iterable rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return userBranch(GetUserBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Iterable response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[UserIdentity](userFailure(sdkgo.FailureTransport, "Iterable response was interrupted"), 0)
	}
	identity, isFound, err := decodeUser(response.body, input.Email)
	if err != nil {
		return userBranch(GetUserBranchInvalidResponse, sdkgo.FailureProtocol, "Iterable returned invalid user JSON")
	}
	if !isFound {
		return userBranch(GetUserBranchNotFound, sdkgo.FailureNotFound, "Iterable did not expose the user")
	}
	return sdkgo.NewQueryBranch(GetUserBranchFound, identity, nil, sdkgo.Receipt{})
}

func validEmail(email string) bool {
	if len(email) == 0 || len(email) > 256 || !utf8.ValidString(email) || !strings.Contains(email, "@") {
		return false
	}
	for _, character := range email {
		if unicode.IsControl(character) || unicode.IsSpace(character) || character == '/' || character == '?' || character == '#' {
			return false
		}
	}
	return true
}

// decodeUser treats an absent, null, or empty user as not found; Iterable may answer unknown emails with 200 {}.
func decodeUser(body []byte, requestedEmail string) (UserIdentity, bool, error) {
	var wire struct {
		User json.RawMessage `json:"user"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return UserIdentity{}, false, err
	}
	if len(wire.User) == 0 {
		return UserIdentity{}, false, nil
	}
	var userFields map[string]json.RawMessage
	if err := json.Unmarshal(wire.User, &userFields); err != nil {
		return UserIdentity{}, false, err
	}
	if len(userFields) == 0 {
		return UserIdentity{}, false, nil
	}
	var user struct {
		Email  string `json:"email"`
		UserID string `json:"userId"`
	}
	if err := json.Unmarshal(wire.User, &user); err != nil {
		return UserIdentity{}, false, err
	}
	if !strings.EqualFold(user.Email, requestedEmail) || !validEmail(user.Email) {
		return UserIdentity{}, false, fmt.Errorf("user email does not match requested email")
	}
	return UserIdentity{Email: user.Email, UserID: user.UserID}, true, nil
}

func userFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "iterable", Operation: "getUser", Message: message}
}

func userBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[UserIdentity] {
	failure := userFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, UserIdentity{}, &failure, sdkgo.Receipt{})
}
