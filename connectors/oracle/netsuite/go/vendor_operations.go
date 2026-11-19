// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oraclenetsuite

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

func isDefiniteNetSuiteRefusal(status int, body []byte) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	var fields struct {
		ID         string `json:"id"`
		ExternalID string `json:"externalId"`
		Detail     string `json:"detail"`
	}
	return json.Unmarshal(body, &fields) == nil && fields.ID == "" && fields.ExternalID == "" && strings.TrimSpace(fields.Detail) != ""
}

// ListVendorsInput selects one page of vendor records.
type ListVendorsInput struct {
	// Limit is the page size from 1 to 1000.
	Limit int `json:"limit"`
	// Offset is the zero-based result offset.
	Offset int `json:"offset"`
}

// VendorListItem identifies one vendor in a list page.
type VendorListItem struct {
	// ID is the numeric internal vendor ID.
	ID string `json:"id"`
}

// VendorList is a page of vendor identities.
type VendorList struct {
	// Items contains the identities returned on this page.
	Items []VendorListItem `json:"items"`
	// TotalResults is the provider's total count.
	TotalResults int `json:"totalResults"`
	// HasMore reports whether another page follows.
	HasMore bool `json:"hasMore"`
}

// CreateVendorInput supplies a company vendor and its subsidiary.
type CreateVendorInput struct {
	// EntityID is the non-secret displayed vendor identifier.
	EntityID string `json:"entityId"`
	// CompanyName is the vendor's company name.
	CompanyName string `json:"companyName"`
	// SubsidiaryID is the optional numeric subsidiary ID for OneWorld accounts.
	SubsidiaryID string `json:"subsidiaryId"`
}

// UpdateVendorInput replaces one vendor's displayed ID.
type UpdateVendorInput struct {
	// VendorID is the numeric internal vendor ID.
	VendorID string `json:"vendorId"`
	// EntityID is the new displayed vendor identifier.
	EntityID string `json:"entityId"`
}

// ListVendorsOperation lists vendor identities.
type ListVendorsOperation struct{ client *Client }

// CreateVendorOperation creates a company vendor.
type CreateVendorOperation struct{ client *Client }

// UpdateVendorOperation replaces a displayed vendor ID.
type UpdateVendorOperation struct{ client *Client }

// DeleteVendorOperation deletes a vendor.
type DeleteVendorOperation struct{ client *Client }

// ListVendors binds the listing Query.
func (client *Client) ListVendors() ListVendorsOperation { return ListVendorsOperation{client} }

// CreateVendor binds the creation Mutation.
func (client *Client) CreateVendor() CreateVendorOperation {
	return CreateVendorOperation{client}
}

// UpdateVendor binds the update Mutation.
func (client *Client) UpdateVendor() UpdateVendorOperation {
	return UpdateVendorOperation{client}
}

// DeleteVendor binds the deletion Mutation.
func (client *Client) DeleteVendor() DeleteVendorOperation {
	return DeleteVendorOperation{client}
}

// Definition returns the generated listing contract.
func (ListVendorsOperation) Definition() sdkgo.QueryDefinition { return ListVendorsDefinition }

// Definition returns the generated creation contract.
func (CreateVendorOperation) Definition() sdkgo.MutationDefinition { return CreateVendorDefinition }

// Definition returns the generated update contract.
func (UpdateVendorOperation) Definition() sdkgo.MutationDefinition { return UpdateVendorDefinition }

// Definition returns the generated deletion contract.
func (DeleteVendorOperation) Definition() sdkgo.MutationDefinition { return DeleteVendorDefinition }

// IdempotencyKey identifies the call; NetSuite does not deduplicate this creation key.
func (CreateVendorOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateVendorInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies the replacement call.
func (UpdateVendorOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateVendorInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies the deletion call.
func (DeleteVendorOperation) IdempotencyKey(callID sdkgo.CallID, _ GetVendorInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

type vendorHTTPResult struct {
	status     int
	location   string
	body       []byte
	retryAfter time.Duration
	err        error
	failure    *sdkgo.Failure
}

func (client *Client) requestVendor(call sdkgo.Call, ctx context.Context, method, path string, payload any, operationName string) vendorHTTPResult {
	credentials, err := sdkgo.ResolveCredential(ctx, client.credentials, call, client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return vendorHTTPResult{failure: vendorOperationFailure(operationName, sdkgo.FailureAuthentication, "Oracle NetSuite connection needs authorization")}
		}
		return vendorHTTPResult{err: err}
	}
	accessToken := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return vendorHTTPResult{failure: vendorOperationFailure(operationName, sdkgo.FailureAuthentication, "Oracle NetSuite access token is unavailable")}
	}
	var requestBody io.Reader
	if payload != nil {
		encoded, encodeErr := json.Marshal(payload)
		if encodeErr != nil {
			return vendorHTTPResult{failure: vendorOperationFailure(operationName, sdkgo.FailureValidation, "vendor request is invalid")}
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+"/services/rest/record/v1/vendor"+path, requestBody)
	if err != nil {
		return vendorHTTPResult{failure: vendorOperationFailure(operationName, sdkgo.FailureValidation, "vendor request could not be built")}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return vendorHTTPResult{err: err}
	}
	defer response.Body.Close()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) {
		return vendorHTTPResult{status: response.StatusCode, failure: vendorOperationFailure(operationName, sdkgo.FailureAuthentication, "Oracle NetSuite response contained a credential")}
	}
	return vendorHTTPResult{status: response.StatusCode, location: response.Header.Get("Location"), body: body,
		retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}

func vendorOperationFailure(operationName string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "oracle-netsuite", Operation: operationName, Message: message}
}

func isVendorRetry(result vendorHTTPResult) bool {
	return result.err != nil || result.status == http.StatusRequestTimeout || result.status == http.StatusTooManyRequests || result.status >= 500
}

// Invoke returns one page of vendors without fetching their individual records.
func (operation ListVendorsOperation) Invoke(call sdkgo.Call, input ListVendorsInput) sdkgo.QueryAttempt[VendorList] {
	const operationName = "listVendors"
	if input.Limit < 1 || input.Limit > 1000 || input.Offset < 0 || input.Offset%input.Limit != 0 {
		return sdkgo.NewQueryBranch(ListVendorsBranchDefect, VendorList{}, vendorOperationFailure(operationName, sdkgo.FailureValidation, "vendor limit or offset is invalid"), sdkgo.Receipt{})
	}
	query := url.Values{"limit": {strconv.Itoa(input.Limit)}, "offset": {strconv.Itoa(input.Offset)}}
	result := operation.client.requestVendor(call, call.Context, http.MethodGet, "?"+query.Encode(), nil, operationName)
	if result.failure != nil {
		return sdkgo.NewQueryBranch(ListVendorsBranchDefect, VendorList{}, result.failure, sdkgo.Receipt{})
	}
	if isVendorRetry(result) {
		return sdkgo.NewQueryRetry[VendorList](*vendorOperationFailure(operationName, sdkgo.FailureAvailability, "vendor listing is temporarily unavailable"), result.retryAfter)
	}
	if result.status != http.StatusOK {
		return sdkgo.NewQueryBranch(ListVendorsBranchProviderRejected, VendorList{}, vendorOperationFailure(operationName, sdkgo.FailureProviderRejection, "NetSuite rejected vendor listing"), sdkgo.Receipt{})
	}
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		TotalResults int  `json:"totalResults"`
		HasMore      bool `json:"hasMore"`
	}
	if json.Unmarshal(result.body, &page) != nil || page.Items == nil || page.TotalResults < 0 {
		return sdkgo.NewQueryBranch(ListVendorsBranchInvalidResponse, VendorList{}, vendorOperationFailure(operationName, sdkgo.FailureProtocol, "NetSuite returned an invalid vendor page"), sdkgo.Receipt{})
	}
	output := VendorList{Items: make([]VendorListItem, 0, len(page.Items)), TotalResults: page.TotalResults, HasMore: page.HasMore}
	for _, item := range page.Items {
		if !vendorIDPattern.MatchString(item.ID) {
			return sdkgo.NewQueryBranch(ListVendorsBranchInvalidResponse, VendorList{}, vendorOperationFailure(operationName, sdkgo.FailureProtocol, "NetSuite returned an invalid vendor ID"), sdkgo.Receipt{})
		}
		output.Items = append(output.Items, VendorListItem{ID: item.ID})
	}
	return sdkgo.NewQueryBranch(ListVendorsBranchListed, output, nil, sdkgo.Receipt{})
}

// Invoke creates a vendor with a bounded number of possible sends.
func (operation CreateVendorOperation) Invoke(call sdkgo.Call, input CreateVendorInput) sdkgo.MutationAttempt[VendorSummary] {
	const operationName = "createVendor"
	if strings.TrimSpace(input.EntityID) == "" || strings.TrimSpace(input.CompanyName) == "" || input.SubsidiaryID != "" && !vendorIDPattern.MatchString(input.SubsidiaryID) || len(input.EntityID) > 256 || len(input.CompanyName) > 256 {
		return sdkgo.NewMutationBranch(CreateVendorBranchDefect, VendorSummary{}, vendorOperationFailure(operationName, sdkgo.FailureValidation, "vendor name, entity ID, or subsidiary ID is invalid"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateVendorDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateVendorBranchDefect, VendorSummary{}, vendorOperationFailure(operationName, sdkgo.FailureLocalDefect, "vendor dispatch fence is unavailable"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureAvailability, "dispatch checkpoint is unreadable"), 0)
	}
	externalID := "dex-" + string(call.ID)
	if decision.Kind != singledispatch.SendAllowed {
		readBack := operation.client.requestVendor(call, call.Context, http.MethodGet, "/eid:"+url.PathEscape(externalID), nil, operationName)
		if readBack.status == http.StatusOK {
			var vendor VendorSummary
			if json.Unmarshal(readBack.body, &vendor) == nil && vendorIDPattern.MatchString(vendor.ID) && vendor.EntityID == input.EntityID {
				return sdkgo.NewMutationBranch(CreateVendorBranchCreated, vendor, nil, sdkgo.Receipt{ProviderObjectID: vendor.ID})
			}
			if decision.Kind == singledispatch.ReadBackRequired {
				return sdkgo.NewMutationUncertain(VendorSummary{}, *vendorOperationFailure(operationName, sdkgo.FailureProtocol, "created vendor could not be confirmed"), sdkgo.Receipt{})
			}
			return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureProtocol, "vendor read-back is invalid"), 0)
		}
		if isVendorRetry(readBack) {
			return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureAvailability, "vendor read-back is unavailable"), readBack.retryAfter)
		}
		if decision.Kind == singledispatch.ReadBackRequired || decision.MaximumPriorSends >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(VendorSummary{}, *vendorOperationFailure(operationName, sdkgo.FailureTransport, "vendor creation may already have succeeded"), sdkgo.Receipt{})
		}
		if readBack.status != http.StatusNotFound {
			return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureTransport, "vendor read-back is inconclusive"), readBack.retryAfter)
		}
	}
	payload := map[string]any{"entityId": input.EntityID, "companyName": input.CompanyName, "externalId": externalID}
	if input.SubsidiaryID != "" {
		payload["subsidiary"] = map[string]string{"id": input.SubsidiaryID}
	}
	var result vendorHTTPResult
	err = fence.Dispatch(func(ctx context.Context) error {
		result = operation.client.requestVendor(call, ctx, http.MethodPost, "", payload, operationName)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureTransport, "vendor dispatch did not start"), 0)
	}
	if result.failure != nil {
		if decision.MaximumPriorSends > 0 || result.status != 0 {
			return sdkgo.NewMutationRetry[VendorSummary](*result.failure, 0)
		}
		if fence.RecordNotSent() != nil {
			return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureAvailability, "no-send checkpoint failed"), 0)
		}
		return sdkgo.NewMutationBranch(CreateVendorBranchDefect, VendorSummary{}, result.failure, sdkgo.Receipt{})
	}
	if result.status == http.StatusTooManyRequests || result.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureAvailability, "NetSuite requested a retry"), result.retryAfter)
	}
	if isVendorRetry(result) {
		return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureAvailability, "vendor creation outcome is unknown"), result.retryAfter)
	}
	if result.status != http.StatusNoContent {
		if result.err != nil || !isDefiniteNetSuiteRefusal(result.status, result.body) {
			return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureTransport, "vendor creation outcome is unknown"), result.retryAfter)
		}
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureTransport, "an earlier vendor create may have succeeded"), 0)
		}
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureAvailability, "refusal checkpoint failed"), 0)
		}
		return sdkgo.NewMutationBranch(CreateVendorBranchProviderRejected, VendorSummary{}, vendorOperationFailure(operationName, sdkgo.FailureProviderRejection, "NetSuite rejected vendor creation"), sdkgo.Receipt{})
	}
	if result.err != nil || len(result.body) != 0 {
		return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureProtocol, "vendor response is unreadable"), 0)
	}
	locationURL, parseErr := url.Parse(result.location)
	if parseErr != nil {
		return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureProtocol, "NetSuite returned an invalid vendor Location"), 0)
	}
	createdID := strings.TrimPrefix(locationURL.Path, "/services/rest/record/v1/vendor/")
	if !vendorIDPattern.MatchString(createdID) {
		return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureProtocol, "NetSuite did not return a valid created vendor ID"), 0)
	}
	_ = fence.RecordProviderID(createdID)
	return sdkgo.NewMutationBranch(CreateVendorBranchCreated, VendorSummary{ID: createdID, EntityID: input.EntityID}, nil, sdkgo.Receipt{ProviderObjectID: createdID})
}

// Invoke replaces the displayed vendor ID with the same value on every retry.
func (operation UpdateVendorOperation) Invoke(call sdkgo.Call, input UpdateVendorInput) sdkgo.MutationAttempt[VendorSummary] {
	const operationName = "updateVendor"
	if !vendorIDPattern.MatchString(input.VendorID) || strings.TrimSpace(input.EntityID) == "" || len(input.EntityID) > 256 {
		return sdkgo.NewMutationBranch(UpdateVendorBranchDefect, VendorSummary{}, vendorOperationFailure(operationName, sdkgo.FailureValidation, "vendor ID or entity ID is invalid"), sdkgo.Receipt{})
	}
	result := operation.client.requestVendor(call, call.Context, http.MethodPatch, "/"+url.PathEscape(input.VendorID), map[string]string{"entityId": input.EntityID}, operationName)
	if result.failure != nil {
		if result.status != 0 {
			return sdkgo.NewMutationRetry[VendorSummary](*result.failure, 0)
		}
		return sdkgo.NewMutationBranch(UpdateVendorBranchDefect, VendorSummary{}, result.failure, sdkgo.Receipt{})
	}
	if isVendorRetry(result) {
		return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureAvailability, "vendor update is temporarily unavailable"), result.retryAfter)
	}
	if result.status == http.StatusNotFound && isDefiniteNetSuiteRefusal(result.status, result.body) {
		return sdkgo.NewMutationBranch(UpdateVendorBranchNotFound, VendorSummary{}, vendorOperationFailure(operationName, sdkgo.FailureNotFound, "vendor was not found"), sdkgo.Receipt{})
	}
	if result.status != http.StatusNoContent {
		if isDefiniteNetSuiteRefusal(result.status, result.body) {
			return sdkgo.NewMutationBranch(UpdateVendorBranchProviderRejected, VendorSummary{}, vendorOperationFailure(operationName, sdkgo.FailureProviderRejection, "NetSuite rejected vendor update"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureTransport, "NetSuite write outcome is unknown"), result.retryAfter)
	}
	if len(result.body) != 0 {
		return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureProtocol, "NetSuite returned a contradictory update receipt"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateVendorBranchUpdated, VendorSummary{ID: input.VendorID, EntityID: input.EntityID}, nil, sdkgo.Receipt{ProviderObjectID: input.VendorID})
}

// Invoke deletes a vendor; repeated deletion cannot create a duplicate.
func (operation DeleteVendorOperation) Invoke(call sdkgo.Call, input GetVendorInput) sdkgo.MutationAttempt[VendorSummary] {
	const operationName = "deleteVendor"
	if !vendorIDPattern.MatchString(input.VendorID) {
		return sdkgo.NewMutationBranch(DeleteVendorBranchDefect, VendorSummary{}, vendorOperationFailure(operationName, sdkgo.FailureValidation, "vendor ID is invalid"), sdkgo.Receipt{})
	}
	result := operation.client.requestVendor(call, call.Context, http.MethodDelete, "/"+url.PathEscape(input.VendorID), nil, operationName)
	if result.failure != nil {
		if result.status != 0 {
			return sdkgo.NewMutationRetry[VendorSummary](*result.failure, 0)
		}
		return sdkgo.NewMutationBranch(DeleteVendorBranchDefect, VendorSummary{}, result.failure, sdkgo.Receipt{})
	}
	if isVendorRetry(result) {
		return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureAvailability, "vendor deletion is temporarily unavailable"), result.retryAfter)
	}
	if result.status == http.StatusNotFound && isDefiniteNetSuiteRefusal(result.status, result.body) {
		return sdkgo.NewMutationBranch(DeleteVendorBranchNotFound, VendorSummary{}, vendorOperationFailure(operationName, sdkgo.FailureNotFound, "vendor was not found"), sdkgo.Receipt{})
	}
	if result.status != http.StatusNoContent {
		if isDefiniteNetSuiteRefusal(result.status, result.body) {
			return sdkgo.NewMutationBranch(DeleteVendorBranchProviderRejected, VendorSummary{}, vendorOperationFailure(operationName, sdkgo.FailureProviderRejection, "NetSuite rejected vendor deletion"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureTransport, "NetSuite write outcome is unknown"), result.retryAfter)
	}
	if len(result.body) != 0 {
		return sdkgo.NewMutationRetry[VendorSummary](*vendorOperationFailure(operationName, sdkgo.FailureProtocol, "NetSuite returned a contradictory deletion receipt"), 0)
	}
	return sdkgo.NewMutationBranch(DeleteVendorBranchDeleted, VendorSummary{ID: input.VendorID}, nil, sdkgo.Receipt{ProviderObjectID: input.VendorID})
}
