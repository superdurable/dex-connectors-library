// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package acrobatsign

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CancelAgreementInput selects one agreement to cancel.
type CancelAgreementInput struct {
	// AgreementID is the non-secret identifier returned by Acrobat Sign.
	AgreementID string `json:"agreementId"`
}

// CancelledAgreement confirms the agreement reached the cancelled state.
type CancelledAgreement struct {
	// AgreementID identifies the cancelled agreement.
	AgreementID string `json:"agreementId"`
	// Status is CANCELLED.
	Status string `json:"status"`
}

// CancelAgreementOperation moves an active agreement to its cancelled state.
type CancelAgreementOperation struct{ client *Client }

// CancelAgreement binds the Mutation to this client.
func (client *Client) CancelAgreement() CancelAgreementOperation {
	return CancelAgreementOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (CancelAgreementOperation) Definition() sdkgo.MutationDefinition {
	return CancelAgreementDefinition
}

// IdempotencyKey correlates repeats of the same absolute state transition.
func (CancelAgreementOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelAgreementInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads current status and sends cancellation only when needed.
func (operation CancelAgreementOperation) Invoke(call sdkgo.Call, input CancelAgreementInput) sdkgo.MutationAttempt[CancelledAgreement] {
	if !validAgreementID(input.AgreementID) {
		return cancelAgreementBranch(CancelAgreementBranchDefect, sdkgo.FailureValidation, "agreement ID is invalid")
	}
	path := "/agreements/" + url.PathEscape(input.AgreementID)
	current := operation.client.get(call, path)
	if retry := cancelAgreementRetryForResponse(current); retry != nil {
		return *retry
	}
	if errors.Is(current.err, errCredential) {
		return cancelAgreementBranch(CancelAgreementBranchDefect, sdkgo.FailureAuthentication, "Acrobat Sign credentials are unavailable or invalid")
	}
	if errors.Is(current.err, errInvalidAccessPoint) || errors.Is(current.err, providerhttp.ErrBodyTooLarge) {
		return cancelAgreementBranch(CancelAgreementBranchInvalidResponse, sdkgo.FailureProtocol, "Acrobat Sign returned unsafe or oversized metadata")
	}
	if current.status == http.StatusNotFound && !current.isDiscovery {
		return cancelAgreementBranch(CancelAgreementBranchNotFound, sdkgo.FailureNotFound, "Acrobat Sign did not expose the agreement")
	}
	if current.status != http.StatusOK {
		return cancelAgreementBranch(CancelAgreementBranchProviderRejected, sdkgo.FailureProviderRejection, "Acrobat Sign rejected the agreement lookup")
	}
	if current.err != nil {
		return cancelAgreementRetry(sdkgo.FailureTransport, "Acrobat Sign agreement response was interrupted", 0)
	}
	var metadata struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if json.Unmarshal(current.body, &metadata) != nil || metadata.ID != input.AgreementID || metadata.Status == "" {
		return cancelAgreementBranch(CancelAgreementBranchInvalidResponse, sdkgo.FailureProtocol, "Acrobat Sign returned invalid agreement metadata")
	}
	if metadata.Status == "ABORTED" || metadata.Status == "CANCELLED" {
		return cancelAgreementSuccess(call, input.AgreementID)
	}
	response := operation.client.put(call, path+"/state", []byte(`{"state":"CANCELLED"}`))
	if retry := cancelAgreementRetryForResponse(response); retry != nil {
		return *retry
	}
	if errors.Is(response.err, errCredential) {
		return cancelAgreementBranch(CancelAgreementBranchDefect, sdkgo.FailureAuthentication, "Acrobat Sign credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errInvalidAccessPoint) || errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return cancelAgreementBranch(CancelAgreementBranchInvalidResponse, sdkgo.FailureProtocol, "Acrobat Sign returned unsafe or oversized metadata")
	}
	if response.status < 200 || response.status >= 300 {
		return cancelAgreementBranch(CancelAgreementBranchProviderRejected, sdkgo.FailureProviderRejection, "Acrobat Sign refused cancellation")
	}
	if response.err != nil {
		return cancelAgreementRetry(sdkgo.FailureTransport, "Acrobat Sign cancellation response was interrupted", 0)
	}
	return cancelAgreementSuccess(call, input.AgreementID)
}

func cancelAgreementRetryForResponse(response providerResponse) *sdkgo.MutationAttempt[CancelledAgreement] {
	var attempt sdkgo.MutationAttempt[CancelledAgreement]
	switch {
	case errors.Is(response.err, errRequest):
		attempt = cancelAgreementRetry(sdkgo.FailureTransport, "Acrobat Sign request failed", 0)
	case response.status == http.StatusTooManyRequests:
		attempt = cancelAgreementRetry(sdkgo.FailureRateLimit, "Acrobat Sign rate limited the request", response.retryAfter)
	case response.status == http.StatusRequestTimeout || response.status >= 500:
		attempt = cancelAgreementRetry(sdkgo.FailureAvailability, "Acrobat Sign is temporarily unavailable", response.retryAfter)
	default:
		return nil
	}
	return &attempt
}

func cancelAgreementSuccess(call sdkgo.Call, agreementID string) sdkgo.MutationAttempt[CancelledAgreement] {
	value := CancelledAgreement{AgreementID: agreementID, Status: "CANCELLED"}
	receipt := sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "adobe-acrobat-sign", ProviderObjectID: agreementID, ObservedAt: time.Now().UTC()}
	return sdkgo.NewMutationBranch(CancelAgreementBranchCancelled, value, nil, receipt)
}

func cancelAgreementBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CancelledAgreement] {
	failure := sdkgo.Failure{Kind: kind, Provider: "adobe-acrobat-sign", Operation: "cancelAgreement", Message: message}
	return sdkgo.NewMutationBranch(branch, CancelledAgreement{}, &failure, sdkgo.Receipt{})
}

func cancelAgreementRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[CancelledAgreement] {
	failure := sdkgo.Failure{Kind: kind, Provider: "adobe-acrobat-sign", Operation: "cancelAgreement", Message: message}
	return sdkgo.NewMutationRetry[CancelledAgreement](failure, delay)
}
