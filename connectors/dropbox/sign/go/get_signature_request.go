// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dropboxsign

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

// GetSignatureRequestInput selects a signature request by its non-secret ID.
type GetSignatureRequestInput struct {
	// SignatureRequestID is the identifier returned when Dropbox Sign creates the request.
	SignatureRequestID string `json:"signatureRequestId"`
}

// SignatureRequestStatus excludes signer identities, document contents, and custom fields.
type SignatureRequestStatus struct {
	// ID is the Dropbox Sign signature-request ID.
	ID string `json:"id"`
	// Title is the signature-request title.
	Title string `json:"title"`
	// IsComplete reports whether all required signatures are complete.
	IsComplete bool `json:"isComplete"`
	// IsDeclined reports whether a signer declined the request.
	IsDeclined bool `json:"isDeclined"`
	// HasError reports a provider-side request error.
	HasError bool `json:"hasError"`
}

// GetSignatureRequestOperation reads one signature request's status.
type GetSignatureRequestOperation struct{ client *Client }

// GetSignatureRequest binds the Query to this client.
func (client *Client) GetSignatureRequest() GetSignatureRequestOperation {
	return GetSignatureRequestOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetSignatureRequestOperation) Definition() sdkgo.QueryDefinition {
	return GetSignatureRequestDefinition
}

// Invoke reads one signature request and classifies retryable and definitive outcomes.
func (operation GetSignatureRequestOperation) Invoke(call sdkgo.Call, input GetSignatureRequestInput) sdkgo.QueryAttempt[SignatureRequestStatus] {
	if !validSignatureRequestID(input.SignatureRequestID) {
		return signatureRequestBranch(GetSignatureRequestBranchDefect, sdkgo.FailureValidation, "signature request ID must be 1–256 printable characters")
	}
	response := operation.client.get(call, "/signature_request/"+url.PathEscape(input.SignatureRequestID))
	if errors.Is(response.err, errCredential) {
		return signatureRequestBranch(GetSignatureRequestBranchDefect, sdkgo.FailureAuthentication, "Dropbox Sign credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[SignatureRequestStatus](signatureRequestFailure(sdkgo.FailureTransport, "Dropbox Sign request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[SignatureRequestStatus](signatureRequestFailure(sdkgo.FailureRateLimit, "Dropbox Sign rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[SignatureRequestStatus](signatureRequestFailure(sdkgo.FailureAvailability, "Dropbox Sign is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return signatureRequestBranch(GetSignatureRequestBranchNotFound, sdkgo.FailureNotFound, "Dropbox Sign did not expose the signature request")
	}
	if response.status != http.StatusOK {
		return signatureRequestBranch(GetSignatureRequestBranchProviderRejected, sdkgo.FailureProviderRejection, "Dropbox Sign rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return signatureRequestBranch(GetSignatureRequestBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Dropbox Sign response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[SignatureRequestStatus](signatureRequestFailure(sdkgo.FailureTransport, "Dropbox Sign response was interrupted"), 0)
	}
	status, err := decodeSignatureRequest(response.body, input.SignatureRequestID)
	if err != nil {
		return signatureRequestBranch(GetSignatureRequestBranchInvalidResponse, sdkgo.FailureProtocol, "Dropbox Sign returned invalid signature request JSON")
	}
	return sdkgo.NewQueryBranch(GetSignatureRequestBranchFound, status, nil, sdkgo.Receipt{})
}

func validSignatureRequestID(signatureRequestID string) bool {
	if len(signatureRequestID) == 0 || len(signatureRequestID) > 256 || !utf8.ValidString(signatureRequestID) {
		return false
	}
	for _, character := range signatureRequestID {
		if unicode.IsControl(character) || character == '/' || character == '?' || character == '#' {
			return false
		}
	}
	return true
}

func decodeSignatureRequest(body []byte, requestedSignatureRequestID string) (SignatureRequestStatus, error) {
	var wire struct {
		SignatureRequest struct {
			ID         string `json:"signature_request_id"`
			Title      string `json:"title"`
			IsComplete *bool  `json:"is_complete"`
			IsDeclined *bool  `json:"is_declined"`
			HasError   *bool  `json:"has_error"`
		} `json:"signature_request"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SignatureRequestStatus{}, err
	}
	request := wire.SignatureRequest
	if request.ID != requestedSignatureRequestID || strings.TrimSpace(request.Title) == "" || request.IsComplete == nil || request.IsDeclined == nil || request.HasError == nil {
		return SignatureRequestStatus{}, fmt.Errorf("signature request identity or status missing")
	}
	return SignatureRequestStatus{ID: request.ID, Title: request.Title, IsComplete: *request.IsComplete, IsDeclined: *request.IsDeclined, HasError: *request.HasError}, nil
}

func signatureRequestFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "dropboxsign", Operation: "getSignatureRequest", Message: message}
}

func signatureRequestBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SignatureRequestStatus] {
	failure := signatureRequestFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, SignatureRequestStatus{}, &failure, sdkgo.Receipt{})
}
