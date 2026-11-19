// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package amplitude

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const identifyUserOperationID = "identifyUser"

// IdentifyUserInput absolutely sets one string user property.
type IdentifyUserInput struct {
	// UserID is a non-secret Amplitude user ID.
	UserID string `json:"userID"`
	// PropertyName is a user property shown in Amplitude User Lookup.
	PropertyName string `json:"propertyName"`
	// PropertyValue replaces the current string value; blank sets the empty string.
	PropertyValue string `json:"propertyValue"`
}

// IdentifiedUser identifies a user and the property whose value was set.
type IdentifiedUser struct {
	// UserID is the input user ID.
	UserID string `json:"userID"`
	// PropertyName is the updated property name.
	PropertyName string `json:"propertyName"`
}

// IdentifyUserOperation implements one absolute user property replacement.
type IdentifyUserOperation struct{ client *Client }

// IdentifyUser binds the Mutation to this client.
func (client *Client) IdentifyUser() IdentifyUserOperation { return IdentifyUserOperation{client} }

// Definition returns the generated Mutation contract.
func (IdentifyUserOperation) Definition() sdkgo.MutationDefinition { return IdentifyUserDefinition }

// IdempotencyKey identifies retries of this absolute replacement.
func (IdentifyUserOperation) IdempotencyKey(callID sdkgo.CallID, _ IdentifyUserInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets one user property, retrying unknown outcomes because the update is absolute.
func (operation IdentifyUserOperation) Invoke(call sdkgo.Call, input IdentifyUserInput) sdkgo.MutationAttempt[IdentifiedUser] {
	if !validUserSearch(input.UserID) || !validUserSearch(input.PropertyName) || len(input.PropertyValue) > 8192 || !utf8.ValidString(input.PropertyValue) {
		return identifiedBranch(IdentifyUserBranchDefect, sdkgo.FailureValidation, "Amplitude user or property input is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return identifiedBranch(IdentifyUserBranchDefect, sdkgo.FailureAuthentication, "Amplitude API key is unavailable or invalid")
	}
	identification, err := json.Marshal(struct {
		UserID         string                       `json:"user_id"`
		UserProperties map[string]map[string]string `json:"user_properties"`
	}{UserID: input.UserID, UserProperties: map[string]map[string]string{"$set": {input.PropertyName: input.PropertyValue}}})
	if err != nil {
		return identifiedBranch(IdentifyUserBranchDefect, sdkgo.FailureLocalDefect, "Amplitude identification could not be encoded")
	}
	form := url.Values{"api_key": {credentials.APIKey.Reveal()}, "identification": {string(identification)}}
	response := operation.client.post(call, "/identify", "application/x-www-form-urlencoded", []byte(form.Encode()))
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[IdentifiedUser](identifyFailure(sdkgo.FailureTransport, "Amplitude Identify request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		if response.err != nil {
			kind = sdkgo.FailureTransport
		}
		return sdkgo.NewMutationRetry[IdentifiedUser](identifyFailure(kind, "Amplitude Identify request should be retried"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return identifiedBranch(IdentifyUserBranchProviderRejected, sdkgo.FailureProviderRejection, "Amplitude rejected the user property update")
	}
	value := IdentifiedUser{UserID: input.UserID, PropertyName: input.PropertyName}
	return sdkgo.NewMutationBranch(IdentifyUserBranchIdentified, value, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "amplitude", ProviderObjectID: input.UserID})
}

func identifyFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amplitude", Operation: identifyUserOperationID, Message: message}
}

func identifiedBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[IdentifiedUser] {
	failure := identifyFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, IdentifiedUser{}, &failure, sdkgo.Receipt{})
}
