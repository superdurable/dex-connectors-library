// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package acrobatsign

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

// GetAgreementInput selects an agreement by its non-secret ID.
type GetAgreementInput struct {
	// AgreementID is the ID returned when Acrobat Sign creates the agreement.
	AgreementID string `json:"agreementId"`
}

// AgreementStatus excludes participants and document contents.
type AgreementStatus struct {
	// ID is the Acrobat Sign agreement ID.
	ID string `json:"id"`
	// Name is the agreement title.
	Name string `json:"name"`
	// Status is an Acrobat Sign agreement state such as SIGNED.
	Status string `json:"status"`
}

// GetAgreementOperation reads one agreement's identity and state.
type GetAgreementOperation struct{ client *Client }

// GetAgreement binds the Query to this client.
func (client *Client) GetAgreement() GetAgreementOperation { return GetAgreementOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetAgreementOperation) Definition() sdkgo.QueryDefinition { return GetAgreementDefinition }

// Invoke reads one agreement and classifies retryable and definitive outcomes.
func (operation GetAgreementOperation) Invoke(call sdkgo.Call, input GetAgreementInput) sdkgo.QueryAttempt[AgreementStatus] {
	if !validAgreementID(input.AgreementID) {
		return agreementBranch(GetAgreementBranchDefect, sdkgo.FailureValidation, "agreement ID must be 1–256 printable characters")
	}
	response := operation.client.get(call, "/agreements/"+url.PathEscape(input.AgreementID))
	if errors.Is(response.err, errCredential) {
		return agreementBranch(GetAgreementBranchDefect, sdkgo.FailureAuthentication, "Acrobat Sign credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[AgreementStatus](agreementFailure(sdkgo.FailureTransport, "Acrobat Sign request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[AgreementStatus](agreementFailure(sdkgo.FailureRateLimit, "Acrobat Sign rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[AgreementStatus](agreementFailure(sdkgo.FailureAvailability, "Acrobat Sign is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errInvalidAccessPoint) {
		return agreementBranch(GetAgreementBranchInvalidResponse, sdkgo.FailureProtocol, "Acrobat Sign returned an unsafe API access point")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return agreementBranch(GetAgreementBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Acrobat Sign response exceeded the configured limit")
	}
	if response.isDiscovery && response.status != http.StatusOK {
		return agreementBranch(GetAgreementBranchProviderRejected, sdkgo.FailureProviderRejection, "Acrobat Sign rejected discovery HTTP "+strconv.Itoa(response.status))
	}
	if response.status == http.StatusNotFound {
		return agreementBranch(GetAgreementBranchNotFound, sdkgo.FailureNotFound, "Acrobat Sign did not expose the agreement")
	}
	if response.status != http.StatusOK {
		return agreementBranch(GetAgreementBranchProviderRejected, sdkgo.FailureProviderRejection, "Acrobat Sign rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[AgreementStatus](agreementFailure(sdkgo.FailureTransport, "Acrobat Sign response was interrupted"), 0)
	}
	agreement, err := decodeAgreement(response.body, input.AgreementID)
	if err != nil {
		return agreementBranch(GetAgreementBranchInvalidResponse, sdkgo.FailureProtocol, "Acrobat Sign returned invalid agreement JSON")
	}
	return sdkgo.NewQueryBranch(GetAgreementBranchFound, agreement, nil, sdkgo.Receipt{})
}

func validAgreementID(agreementID string) bool {
	if len(agreementID) == 0 || len(agreementID) > 256 || !utf8.ValidString(agreementID) {
		return false
	}
	for _, character := range agreementID {
		if unicode.IsControl(character) || character == '/' || character == '?' || character == '#' {
			return false
		}
	}
	return true
}

func decodeAgreement(body []byte, requestedAgreementID string) (AgreementStatus, error) {
	var wire struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return AgreementStatus{}, err
	}
	if wire.ID != requestedAgreementID || strings.TrimSpace(wire.Name) == "" || strings.TrimSpace(wire.Status) == "" {
		return AgreementStatus{}, fmt.Errorf("agreement identity or status missing")
	}
	return AgreementStatus{ID: wire.ID, Name: wire.Name, Status: wire.Status}, nil
}

func agreementFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "adobe-acrobat-sign", Operation: "getAgreement", Message: message}
}

func agreementBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[AgreementStatus] {
	failure := agreementFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, AgreementStatus{}, &failure, sdkgo.Receipt{})
}
