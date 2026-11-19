// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package acrobatsign

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdateAgreementVisibilityInput selects one agreement and an absolute SHOW or HIDE setting.
type UpdateAgreementVisibilityInput struct {
	// AgreementID is the non-secret ID from an agreement list or creation response.
	AgreementID string `json:"agreementId"`
	// Visibility is SHOW or HIDE in the current caller's agreement list.
	Visibility string `json:"visibility"`
}

// AgreementVisibility confirms the requested list visibility without document content.
type AgreementVisibility struct {
	// AgreementID is the agreement whose visibility was set.
	AgreementID string `json:"agreementId"`
	// Visibility is the requested SHOW or HIDE value.
	Visibility string `json:"visibility"`
}

// UpdateAgreementVisibilityOperation sets the caller's agreement list visibility.
type UpdateAgreementVisibilityOperation struct{ client *Client }

// UpdateAgreementVisibility binds the Mutation to this client.
func (client *Client) UpdateAgreementVisibility() UpdateAgreementVisibilityOperation {
	return UpdateAgreementVisibilityOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (UpdateAgreementVisibilityOperation) Definition() sdkgo.MutationDefinition {
	return UpdateAgreementVisibilityDefinition
}

// IdempotencyKey derives the stable call identity for the repeat-safe absolute setting.
func (UpdateAgreementVisibilityOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateAgreementVisibilityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke writes the same visibility on every attempt; a repeat cannot create a second agreement.
func (operation UpdateAgreementVisibilityOperation) Invoke(call sdkgo.Call, input UpdateAgreementVisibilityInput) sdkgo.MutationAttempt[AgreementVisibility] {
	if !validAgreementID(input.AgreementID) || input.Visibility != "SHOW" && input.Visibility != "HIDE" {
		return agreementVisibilityBranch(UpdateAgreementVisibilityBranchDefect, sdkgo.FailureValidation, "agreement ID or visibility is invalid")
	}
	body, err := json.Marshal(struct {
		Visibility string `json:"visibility"`
	}{Visibility: input.Visibility})
	if err != nil {
		return agreementVisibilityBranch(UpdateAgreementVisibilityBranchDefect, sdkgo.FailureLocalDefect, "visibility request could not be encoded")
	}
	response := operation.client.put(call, "/agreements/"+url.PathEscape(input.AgreementID)+"/me/visibility", body)
	if errors.Is(response.err, errCredential) {
		return agreementVisibilityBranch(UpdateAgreementVisibilityBranchDefect, sdkgo.FailureAuthentication, "Acrobat Sign credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[AgreementVisibility](agreementVisibilityFailure(sdkgo.FailureTransport, "Acrobat Sign request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[AgreementVisibility](agreementVisibilityFailure(sdkgo.FailureRateLimit, "Acrobat Sign rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[AgreementVisibility](agreementVisibilityFailure(sdkgo.FailureAvailability, "Acrobat Sign is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errInvalidAccessPoint) {
		return agreementVisibilityBranch(UpdateAgreementVisibilityBranchInvalidResponse, sdkgo.FailureProtocol, "Acrobat Sign returned an unsafe API access point")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return agreementVisibilityBranch(UpdateAgreementVisibilityBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Acrobat Sign response exceeded the configured limit")
	}
	if response.isDiscovery && response.status != http.StatusOK {
		return agreementVisibilityBranch(UpdateAgreementVisibilityBranchProviderRejected, sdkgo.FailureProviderRejection, "Acrobat Sign rejected discovery HTTP "+strconv.Itoa(response.status))
	}
	if response.status == http.StatusNotFound {
		return agreementVisibilityBranch(UpdateAgreementVisibilityBranchNotFound, sdkgo.FailureNotFound, "Acrobat Sign did not expose the agreement")
	}
	if response.status < 200 || response.status >= 300 {
		return agreementVisibilityBranch(UpdateAgreementVisibilityBranchProviderRejected, sdkgo.FailureProviderRejection, "Acrobat Sign rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[AgreementVisibility](agreementVisibilityFailure(sdkgo.FailureTransport, "Acrobat Sign response was interrupted"), 0)
	}
	output := AgreementVisibility{AgreementID: input.AgreementID, Visibility: input.Visibility}
	receipt := sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "adobe-acrobat-sign", ProviderObjectID: input.AgreementID, ObservedAt: time.Now().UTC()}
	return sdkgo.NewMutationBranch(UpdateAgreementVisibilityBranchUpdated, output, nil, receipt)
}

func agreementVisibilityFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "adobe-acrobat-sign", Operation: "updateAgreementVisibility", Message: message}
}

func agreementVisibilityBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AgreementVisibility] {
	failure := agreementVisibilityFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, AgreementVisibility{}, &failure, sdkgo.Receipt{})
}
