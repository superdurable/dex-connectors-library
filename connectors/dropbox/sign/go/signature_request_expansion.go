// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dropboxsign

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListSignatureRequestsInput selects a bounded page of accessible requests.
type ListSignatureRequestsInput struct {
	// Page is one-based; zero selects the first page.
	Page int `json:"page,omitempty"`
	// PageSize is from 1 to 100; zero selects 20.
	PageSize int `json:"pageSize,omitempty"`
	// Query is an optional Dropbox Sign search expression, up to 1000 characters.
	Query string `json:"query,omitempty"`
}

// SignatureRequestPage contains titles and statuses without signer or document data.
type SignatureRequestPage struct {
	// Requests contains the selected request identities and statuses.
	Requests []SignatureRequestStatus `json:"requests"`
	// Page is the one-based page returned by Dropbox Sign.
	Page int `json:"page"`
	// TotalPages is the reported number of pages.
	TotalPages int `json:"totalPages"`
}

// UpdateSignerNameInput selects one signer and their replacement display name.
type UpdateSignerNameInput struct {
	// SignatureRequestID is the request being updated.
	SignatureRequestID string `json:"signatureRequestId"`
	// SignatureID is the signer identity from Dropbox Sign.
	SignatureID string `json:"signatureId"`
	// Name is the replacement signer display name.
	Name string `json:"name"`
}

// UpdatedSignerName confirms Dropbox Sign accepted a replacement name.
type UpdatedSignerName struct {
	// SignatureRequestID is the updated request ID.
	SignatureRequestID string `json:"signatureRequestId"`
	// Updated is true when Dropbox Sign accepted the replacement.
	Updated bool `json:"updated"`
}

// ListSignatureRequestsOperation lists request identities and statuses.
type ListSignatureRequestsOperation struct{ client *Client }

// UpdateSignerNameOperation replaces a signer display name.
type UpdateSignerNameOperation struct{ client *Client }

// ListSignatureRequests binds request listing to this client.
func (client *Client) ListSignatureRequests() ListSignatureRequestsOperation {
	return ListSignatureRequestsOperation{client}
}

// UpdateSignerName binds signer name replacement to this client.
func (client *Client) UpdateSignerName() UpdateSignerNameOperation {
	return UpdateSignerNameOperation{client}
}

// Definition returns the generated request list Query contract.
func (ListSignatureRequestsOperation) Definition() sdkgo.QueryDefinition {
	return ListSignatureRequestsDefinition
}

// Definition returns the generated signer name Mutation contract.
func (UpdateSignerNameOperation) Definition() sdkgo.MutationDefinition {
	return UpdateSignerNameDefinition
}

// IdempotencyKey derives a stable retry identity for name replacement.
func (UpdateSignerNameOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSignerNameInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one page of signature request titles and statuses.
func (operation ListSignatureRequestsOperation) Invoke(call sdkgo.Call, input ListSignatureRequestsInput) sdkgo.QueryAttempt[SignatureRequestPage] {
	if input.Page < 0 || input.PageSize < 0 || input.PageSize > 100 || len(input.Query) > 1000 || !utf8.ValidString(input.Query) || strings.ContainsAny(input.Query, "\r\n\x00") {
		return listSignatureRequestsBranch(ListSignatureRequestsBranchDefect, sdkgo.FailureValidation, "page or query is invalid")
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "page_size": {strconv.Itoa(input.PageSize)}}
	if input.Query != "" {
		query.Set("query", input.Query)
	}
	response := operation.client.get(call, "/signature_request/list?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return listSignatureRequestsBranch(ListSignatureRequestsBranchDefect, sdkgo.FailureAuthentication, "Dropbox Sign credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[SignatureRequestPage](signatureExpansionFailure("listSignatureRequests", sdkgo.FailureTransport, "Dropbox Sign request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[SignatureRequestPage](signatureExpansionFailure("listSignatureRequests", sdkgo.FailureRateLimit, "Dropbox Sign rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[SignatureRequestPage](signatureExpansionFailure("listSignatureRequests", sdkgo.FailureAvailability, "Dropbox Sign is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return listSignatureRequestsBranch(ListSignatureRequestsBranchProviderRejected, sdkgo.FailureProviderRejection, "Dropbox Sign rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listSignatureRequestsBranch(ListSignatureRequestsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Dropbox Sign response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[SignatureRequestPage](signatureExpansionFailure("listSignatureRequests", sdkgo.FailureTransport, "Dropbox Sign response was interrupted"), 0)
	}
	page, err := decodeSignatureRequestPage(response.body)
	if err != nil {
		return listSignatureRequestsBranch(ListSignatureRequestsBranchInvalidResponse, sdkgo.FailureProtocol, "Dropbox Sign returned invalid request list JSON")
	}
	return sdkgo.NewQueryBranch(ListSignatureRequestsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke replaces one in-flight signer's display name.
func (operation UpdateSignerNameOperation) Invoke(call sdkgo.Call, input UpdateSignerNameInput) sdkgo.MutationAttempt[UpdatedSignerName] {
	if !validSignatureRequestID(input.SignatureRequestID) || !validSignatureRequestID(input.SignatureID) || !validSignerName(input.Name) {
		return updateSignerNameBranch(UpdateSignerNameBranchDefect, sdkgo.FailureValidation, "signature request ID, signature ID, or name is invalid")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("signature_id", input.SignatureID); err != nil {
		return updateSignerNameBranch(UpdateSignerNameBranchDefect, sdkgo.FailureLocalDefect, "signer request could not be built")
	}
	if err := writer.WriteField("name", input.Name); err != nil {
		return updateSignerNameBranch(UpdateSignerNameBranchDefect, sdkgo.FailureLocalDefect, "signer request could not be built")
	}
	if err := writer.Close(); err != nil {
		return updateSignerNameBranch(UpdateSignerNameBranchDefect, sdkgo.FailureLocalDefect, "signer request could not be built")
	}
	response := operation.client.send(call, http.MethodPost, "/signature_request/update/"+url.PathEscape(input.SignatureRequestID), body.Bytes(), writer.FormDataContentType())
	if errors.Is(response.err, errCredential) {
		return updateSignerNameBranch(UpdateSignerNameBranchDefect, sdkgo.FailureAuthentication, "Dropbox Sign credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[UpdatedSignerName](signatureExpansionFailure("updateSignerName", sdkgo.FailureTransport, "Dropbox Sign request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[UpdatedSignerName](signatureExpansionFailure("updateSignerName", sdkgo.FailureRateLimit, "Dropbox Sign rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[UpdatedSignerName](signatureExpansionFailure("updateSignerName", sdkgo.FailureAvailability, "Dropbox Sign is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return updateSignerNameBranch(UpdateSignerNameBranchNotFound, sdkgo.FailureNotFound, "Dropbox Sign did not expose the signature request")
	}
	if response.status != http.StatusOK {
		return updateSignerNameBranch(UpdateSignerNameBranchProviderRejected, sdkgo.FailureProviderRejection, "Dropbox Sign rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return updateSignerNameBranch(UpdateSignerNameBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Dropbox Sign response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[UpdatedSignerName](signatureExpansionFailure("updateSignerName", sdkgo.FailureTransport, "Dropbox Sign response was interrupted"), 0)
	}
	var wire struct {
		SignatureRequest struct {
			ID string `json:"signature_request_id"`
		} `json:"signature_request"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.SignatureRequest.ID != input.SignatureRequestID {
		return updateSignerNameBranch(UpdateSignerNameBranchInvalidResponse, sdkgo.FailureProtocol, "Dropbox Sign returned invalid update JSON")
	}
	return sdkgo.NewMutationBranch(UpdateSignerNameBranchUpdated, UpdatedSignerName{SignatureRequestID: input.SignatureRequestID, Updated: true}, nil, sdkgo.Receipt{})
}

func decodeSignatureRequestPage(body []byte) (SignatureRequestPage, error) {
	var wire struct {
		Requests []struct {
			ID         string `json:"signature_request_id"`
			Title      string `json:"title"`
			IsComplete *bool  `json:"is_complete"`
			IsDeclined *bool  `json:"is_declined"`
			HasError   *bool  `json:"has_error"`
		} `json:"signature_requests"`
		ListInfo struct {
			Page     int `json:"page"`
			NumPages int `json:"num_pages"`
		} `json:"list_info"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SignatureRequestPage{}, err
	}
	if wire.Requests == nil || wire.ListInfo.Page < 1 || wire.ListInfo.NumPages < 0 {
		return SignatureRequestPage{}, fmt.Errorf("signature request page is incomplete")
	}
	page := SignatureRequestPage{Requests: make([]SignatureRequestStatus, 0, len(wire.Requests)), Page: wire.ListInfo.Page, TotalPages: wire.ListInfo.NumPages}
	for _, request := range wire.Requests {
		if request.ID == "" || strings.TrimSpace(request.Title) == "" || request.IsComplete == nil || request.IsDeclined == nil || request.HasError == nil {
			return SignatureRequestPage{}, fmt.Errorf("signature request status is incomplete")
		}
		page.Requests = append(page.Requests, SignatureRequestStatus{ID: request.ID, Title: request.Title, IsComplete: *request.IsComplete, IsDeclined: *request.IsDeclined, HasError: *request.HasError})
	}
	return page, nil
}

func validSignerName(name string) bool {
	if name == "" || len(name) > 256 || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func signatureExpansionFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "dropboxsign", Operation: operation, Message: message}
}

func listSignatureRequestsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SignatureRequestPage] {
	failure := signatureExpansionFailure("listSignatureRequests", kind, message)
	return sdkgo.NewQueryBranch(branch, SignatureRequestPage{}, &failure, sdkgo.Receipt{})
}

func updateSignerNameBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[UpdatedSignerName] {
	failure := signatureExpansionFailure("updateSignerName", kind, message)
	return sdkgo.NewMutationBranch(branch, UpdatedSignerName{}, &failure, sdkgo.Receipt{})
}
