// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package amplitude

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UnsetUserPropertyInput removes one user property from future events.
type UnsetUserPropertyInput struct {
	// UserID is the existing Amplitude user ID.
	UserID string `json:"userID"`
	// PropertyName names the user property to remove.
	PropertyName string `json:"propertyName"`
}

// UnsetUserPropertyOperation removes one property using Identify $unset.
type UnsetUserPropertyOperation struct{ client *Client }

// UnsetUserProperty binds this Mutation to the client.
func (client *Client) UnsetUserProperty() UnsetUserPropertyOperation {
	return UnsetUserPropertyOperation{client}
}

// Definition returns the generated Mutation contract.
func (UnsetUserPropertyOperation) Definition() sdkgo.MutationDefinition {
	return UnsetUserPropertyDefinition
}

// IdempotencyKey identifies retries of the same absolute removal.
func (UnsetUserPropertyOperation) IdempotencyKey(callID sdkgo.CallID, _ UnsetUserPropertyInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes a property and retries ambiguous outcomes safely.
func (operation UnsetUserPropertyOperation) Invoke(call sdkgo.Call, input UnsetUserPropertyInput) sdkgo.MutationAttempt[IdentifiedUser] {
	const operationID = "unsetUserProperty"
	if !validUserSearch(input.UserID) || !validUserSearch(input.PropertyName) {
		return unsetBranch(UnsetUserPropertyBranchDefect, sdkgo.FailureValidation, "Amplitude user or property input is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return unsetBranch(UnsetUserPropertyBranchDefect, sdkgo.FailureAuthentication, "Amplitude API key is unavailable")
	}
	identification, err := json.Marshal(struct {
		UserID         string                       `json:"user_id"`
		UserProperties map[string]map[string]string `json:"user_properties"`
	}{UserID: input.UserID, UserProperties: map[string]map[string]string{"$unset": {input.PropertyName: "-"}}})
	if err != nil {
		return unsetBranch(UnsetUserPropertyBranchDefect, sdkgo.FailureLocalDefect, "Amplitude unset could not be encoded")
	}
	form := url.Values{"api_key": {credentials.APIKey.Reveal()}, "identification": {string(identification)}}
	response := operation.client.post(call, "/identify", "application/x-www-form-urlencoded", []byte(form.Encode()))
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[IdentifiedUser](unsetFailure(sdkgo.FailureTransport, "Amplitude unset request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		if response.err != nil {
			kind = sdkgo.FailureTransport
		}
		return sdkgo.NewMutationRetry[IdentifiedUser](unsetFailure(kind, "Amplitude unset should be retried"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return unsetBranch(UnsetUserPropertyBranchProviderRejected, sdkgo.FailureProviderRejection, "Amplitude refused the property removal")
	}
	value := IdentifiedUser{UserID: input.UserID, PropertyName: input.PropertyName}
	return sdkgo.NewMutationBranch(UnsetUserPropertyBranchUnset, value, nil, sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "amplitude", ProviderObjectID: input.UserID})
}

func unsetFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "amplitude", Operation: "unsetUserProperty", Message: message}
}

func unsetBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[IdentifiedUser] {
	failure := unsetFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, IdentifiedUser{}, &failure, sdkgo.Receipt{})
}
