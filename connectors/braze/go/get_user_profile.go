// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braze

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetUserProfileInput selects a Braze user by the application's external ID.
type GetUserProfileInput struct {
	// ExternalID is a non-secret stable application user ID.
	ExternalID string `json:"externalId"`
}

// UserProfile contains only the selected profile fields, excluding custom attributes.
type UserProfile struct {
	// ExternalID is the application-supplied user ID.
	ExternalID string `json:"externalId"`
	// BrazeID is Braze's internal user ID.
	BrazeID string `json:"brazeId,omitempty"`
	// FirstName is the optional given name.
	FirstName string `json:"firstName,omitempty"`
	// LastName is the optional family name.
	LastName string `json:"lastName,omitempty"`
	// Email is the optional email address.
	Email string `json:"email,omitempty"`
}

// GetUserProfileOperation exports one selected profile through Braze's read-only API.
type GetUserProfileOperation struct{ client *Client }

// GetUserProfile binds the Query to this client.
func (client *Client) GetUserProfile() GetUserProfileOperation {
	return GetUserProfileOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetUserProfileOperation) Definition() sdkgo.QueryDefinition { return GetUserProfileDefinition }

// Invoke reads selected profile fields and classifies definitive and retryable outcomes.
func (operation GetUserProfileOperation) Invoke(call sdkgo.Call, input GetUserProfileInput) sdkgo.QueryAttempt[UserProfile] {
	if strings.TrimSpace(input.ExternalID) == "" || !utf8.ValidString(input.ExternalID) || len(input.ExternalID) > 987 {
		return profileBranch(GetUserProfileBranchDefect, sdkgo.FailureValidation, "external ID must be a nonempty UTF-8 value of at most 987 bytes")
	}
	payload, err := json.Marshal(struct {
		ExternalIDs    []string `json:"external_ids"`
		FieldsToExport []string `json:"fields_to_export"`
	}{[]string{input.ExternalID}, []string{"external_id", "braze_id", "first_name", "last_name", "email"}})
	if err != nil {
		return profileBranch(GetUserProfileBranchDefect, sdkgo.FailureLocalDefect, "profile request could not be encoded")
	}
	response := operation.client.post(call, "/users/export/ids", payload)
	if errors.Is(response.err, errCredential) {
		return profileBranch(GetUserProfileBranchDefect, sdkgo.FailureAuthentication, "Braze credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[UserProfile](profileFailure(sdkgo.FailureTransport, "Braze profile request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[UserProfile](profileFailure(sdkgo.FailureRateLimit, "Braze rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[UserProfile](profileFailure(sdkgo.FailureAvailability, "Braze is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		return profileBranch(GetUserProfileBranchProviderRejected, sdkgo.FailureProviderRejection, "Braze rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return profileBranch(GetUserProfileBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Braze response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[UserProfile](profileFailure(sdkgo.FailureTransport, "Braze response was interrupted"), 0)
	}
	profile, isFound, err := decodeProfile(response.body, input.ExternalID)
	if err != nil {
		return profileBranch(GetUserProfileBranchInvalidResponse, sdkgo.FailureProtocol, "Braze returned an invalid profile response")
	}
	if !isFound {
		return profileBranch(GetUserProfileBranchNotFound, sdkgo.FailureNotFound, "Braze did not find the user")
	}
	return sdkgo.NewQueryBranch(GetUserProfileBranchFound, profile, nil, sdkgo.Receipt{})
}

func decodeProfile(body []byte, externalID string) (UserProfile, bool, error) {
	var wire struct {
		Message string `json:"message"`
		Users   []struct {
			ExternalID string `json:"external_id"`
			BrazeID    string `json:"braze_id"`
			FirstName  string `json:"first_name"`
			LastName   string `json:"last_name"`
			Email      string `json:"email"`
		} `json:"users"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return UserProfile{}, false, err
	}
	if wire.Message != "success" || wire.Users == nil || len(wire.Users) > 1 {
		return UserProfile{}, false, fmt.Errorf("invalid profile envelope")
	}
	if len(wire.Users) == 0 {
		return UserProfile{}, false, nil
	}
	user := wire.Users[0]
	if user.ExternalID != externalID {
		return UserProfile{}, false, fmt.Errorf("returned external ID differs")
	}
	return UserProfile{ExternalID: user.ExternalID, BrazeID: user.BrazeID, FirstName: user.FirstName, LastName: user.LastName, Email: user.Email}, true, nil
}

func profileFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "braze", Operation: "getUserProfile", Message: message}
}

func profileBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[UserProfile] {
	failure := profileFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, UserProfile{}, &failure, sdkgo.Receipt{})
}
