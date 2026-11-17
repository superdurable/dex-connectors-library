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

// ListCustomersInput selects one page of customer records.
type ListCustomersInput struct {
	// Limit is the page size from 1 to 1000.
	Limit int `json:"limit"`
	// Offset is the zero-based result offset.
	Offset int `json:"offset"`
}

// CustomerListItem identifies one customer in a list page.
type CustomerListItem struct {
	// ID is the numeric internal customer ID.
	ID string `json:"id"`
}

// CustomerList is a page of customer identities.
type CustomerList struct {
	// Items contains the identities returned on this page.
	Items []CustomerListItem `json:"items"`
	// TotalResults is the provider's total count.
	TotalResults int `json:"totalResults"`
	// HasMore reports whether another page follows.
	HasMore bool `json:"hasMore"`
}

// CreateCustomerInput supplies a company customer and its subsidiary.
type CreateCustomerInput struct {
	// EntityID is the non-secret displayed customer identifier.
	EntityID string `json:"entityId"`
	// CompanyName is the customer's company name.
	CompanyName string `json:"companyName"`
	// SubsidiaryID is the numeric internal ID of the subsidiary.
	SubsidiaryID string `json:"subsidiaryId"`
}

// UpdateCustomerInput replaces one customer's displayed ID.
type UpdateCustomerInput struct {
	// CustomerID is the numeric internal customer ID.
	CustomerID string `json:"customerId"`
	// EntityID is the new displayed customer identifier.
	EntityID string `json:"entityId"`
}

// ListCustomersOperation lists customer identities.
type ListCustomersOperation struct{ client *Client }

// CreateCustomerOperation creates a company customer.
type CreateCustomerOperation struct{ client *Client }

// UpdateCustomerOperation replaces a displayed customer ID.
type UpdateCustomerOperation struct{ client *Client }

// DeleteCustomerOperation deletes a customer.
type DeleteCustomerOperation struct{ client *Client }

// ListCustomers binds the listing Query.
func (client *Client) ListCustomers() ListCustomersOperation { return ListCustomersOperation{client} }

// CreateCustomer binds the creation Mutation.
func (client *Client) CreateCustomer() CreateCustomerOperation {
	return CreateCustomerOperation{client}
}

// UpdateCustomer binds the update Mutation.
func (client *Client) UpdateCustomer() UpdateCustomerOperation {
	return UpdateCustomerOperation{client}
}

// DeleteCustomer binds the deletion Mutation.
func (client *Client) DeleteCustomer() DeleteCustomerOperation {
	return DeleteCustomerOperation{client}
}

// Definition returns the generated listing contract.
func (ListCustomersOperation) Definition() sdkgo.QueryDefinition { return ListCustomersDefinition }

// Definition returns the generated creation contract.
func (CreateCustomerOperation) Definition() sdkgo.MutationDefinition { return CreateCustomerDefinition }

// Definition returns the generated update contract.
func (UpdateCustomerOperation) Definition() sdkgo.MutationDefinition { return UpdateCustomerDefinition }

// Definition returns the generated deletion contract.
func (DeleteCustomerOperation) Definition() sdkgo.MutationDefinition { return DeleteCustomerDefinition }

// IdempotencyKey identifies the call; NetSuite does not deduplicate this creation key.
func (CreateCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies the replacement call.
func (UpdateCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies the deletion call.
func (DeleteCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ GetCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

type customerHTTPResult struct {
	status     int
	location   string
	body       []byte
	retryAfter time.Duration
	err        error
	failure    *sdkgo.Failure
}

func (client *Client) requestCustomer(call sdkgo.Call, ctx context.Context, method, path string, payload any, operationName string) customerHTTPResult {
	credentials, err := sdkgo.ResolveCredential(ctx, client.credentials, call, client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return customerHTTPResult{failure: customerOperationFailure(operationName, sdkgo.FailureAuthentication, "Oracle NetSuite connection needs authorization")}
		}
		return customerHTTPResult{err: err}
	}
	accessToken := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return customerHTTPResult{failure: customerOperationFailure(operationName, sdkgo.FailureAuthentication, "Oracle NetSuite access token is unavailable")}
	}
	var requestBody io.Reader
	if payload != nil {
		encoded, encodeErr := json.Marshal(payload)
		if encodeErr != nil {
			return customerHTTPResult{failure: customerOperationFailure(operationName, sdkgo.FailureValidation, "customer request is invalid")}
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+"/services/rest/record/v1/customer"+path, requestBody)
	if err != nil {
		return customerHTTPResult{failure: customerOperationFailure(operationName, sdkgo.FailureValidation, "customer request could not be built")}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return customerHTTPResult{err: err}
	}
	defer response.Body.Close()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) {
		return customerHTTPResult{failure: customerOperationFailure(operationName, sdkgo.FailureAuthentication, "Oracle NetSuite response contained a credential")}
	}
	return customerHTTPResult{status: response.StatusCode, location: response.Header.Get("Location"), body: body,
		retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}

func customerOperationFailure(operationName string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "oracle-netsuite", Operation: operationName, Message: message}
}

func isCustomerRetry(result customerHTTPResult) bool {
	return result.err != nil || result.status == http.StatusRequestTimeout || result.status == http.StatusTooManyRequests || result.status >= 500
}

// Invoke returns one page of customers without fetching their individual records.
func (operation ListCustomersOperation) Invoke(call sdkgo.Call, input ListCustomersInput) sdkgo.QueryAttempt[CustomerList] {
	const operationName = "listCustomers"
	if input.Limit < 1 || input.Limit > 1000 || input.Offset < 0 || input.Offset%input.Limit != 0 {
		return sdkgo.NewQueryBranch(ListCustomersBranchDefect, CustomerList{}, customerOperationFailure(operationName, sdkgo.FailureValidation, "customer limit or offset is invalid"), sdkgo.Receipt{})
	}
	query := url.Values{"limit": {strconv.Itoa(input.Limit)}, "offset": {strconv.Itoa(input.Offset)}}
	result := operation.client.requestCustomer(call, call.Context, http.MethodGet, "?"+query.Encode(), nil, operationName)
	if result.failure != nil {
		return sdkgo.NewQueryBranch(ListCustomersBranchDefect, CustomerList{}, result.failure, sdkgo.Receipt{})
	}
	if isCustomerRetry(result) {
		return sdkgo.NewQueryRetry[CustomerList](*customerOperationFailure(operationName, sdkgo.FailureAvailability, "customer listing is temporarily unavailable"), result.retryAfter)
	}
	if result.status != http.StatusOK {
		return sdkgo.NewQueryBranch(ListCustomersBranchProviderRejected, CustomerList{}, customerOperationFailure(operationName, sdkgo.FailureProviderRejection, "NetSuite rejected customer listing"), sdkgo.Receipt{})
	}
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		TotalResults int  `json:"totalResults"`
		HasMore      bool `json:"hasMore"`
	}
	if json.Unmarshal(result.body, &page) != nil || page.Items == nil || page.TotalResults < 0 {
		return sdkgo.NewQueryBranch(ListCustomersBranchInvalidResponse, CustomerList{}, customerOperationFailure(operationName, sdkgo.FailureProtocol, "NetSuite returned an invalid customer page"), sdkgo.Receipt{})
	}
	output := CustomerList{Items: make([]CustomerListItem, 0, len(page.Items)), TotalResults: page.TotalResults, HasMore: page.HasMore}
	for _, item := range page.Items {
		if !customerIDPattern.MatchString(item.ID) {
			return sdkgo.NewQueryBranch(ListCustomersBranchInvalidResponse, CustomerList{}, customerOperationFailure(operationName, sdkgo.FailureProtocol, "NetSuite returned an invalid customer ID"), sdkgo.Receipt{})
		}
		output.Items = append(output.Items, CustomerListItem{ID: item.ID})
	}
	return sdkgo.NewQueryBranch(ListCustomersBranchListed, output, nil, sdkgo.Receipt{})
}

// Invoke creates a customer with a bounded number of possible sends.
func (operation CreateCustomerOperation) Invoke(call sdkgo.Call, input CreateCustomerInput) sdkgo.MutationAttempt[CustomerSummary] {
	const operationName = "createCustomer"
	if strings.TrimSpace(input.EntityID) == "" || strings.TrimSpace(input.CompanyName) == "" || !customerIDPattern.MatchString(input.SubsidiaryID) || len(input.EntityID) > 256 || len(input.CompanyName) > 256 {
		return sdkgo.NewMutationBranch(CreateCustomerBranchDefect, CustomerSummary{}, customerOperationFailure(operationName, sdkgo.FailureValidation, "customer name, entity ID, or subsidiary ID is invalid"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateCustomerDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateCustomerBranchDefect, CustomerSummary{}, customerOperationFailure(operationName, sdkgo.FailureLocalDefect, "customer dispatch fence is unavailable"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationUncertain(CustomerSummary{}, *customerOperationFailure(operationName, sdkgo.FailureTransport, "customer creation may already have succeeded"), sdkgo.Receipt{})
	}
	externalID := "dex-" + string(call.ID)
	if decision.Kind != singledispatch.SendAllowed {
		readBack := operation.client.requestCustomer(call, call.Context, http.MethodGet, "/eid:"+url.PathEscape(externalID), nil, operationName)
		if readBack.status == http.StatusOK {
			var customer CustomerSummary
			if json.Unmarshal(readBack.body, &customer) == nil && customerIDPattern.MatchString(customer.ID) && customer.EntityID == input.EntityID {
				return sdkgo.NewMutationBranch(CreateCustomerBranchCreated, customer, nil, sdkgo.Receipt{ProviderObjectID: customer.ID})
			}
			return sdkgo.NewMutationUncertain(CustomerSummary{}, *customerOperationFailure(operationName, sdkgo.FailureProtocol, "created customer could not be confirmed"), sdkgo.Receipt{})
		}
		if isCustomerRetry(readBack) {
			return sdkgo.NewMutationRetry[CustomerSummary](*customerOperationFailure(operationName, sdkgo.FailureAvailability, "customer read-back is unavailable"), readBack.retryAfter)
		}
		if readBack.status != http.StatusNotFound || decision.Kind == singledispatch.ReadBackRequired || decision.MaximumPriorSends >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(CustomerSummary{}, *customerOperationFailure(operationName, sdkgo.FailureTransport, "customer creation may already have succeeded"), sdkgo.Receipt{})
		}
	}
	payload := map[string]any{"entityId": input.EntityID, "companyName": input.CompanyName, "externalId": externalID, "subsidiary": map[string]string{"id": input.SubsidiaryID}}
	var result customerHTTPResult
	err = fence.Dispatch(func(ctx context.Context) error {
		result = operation.client.requestCustomer(call, ctx, http.MethodPost, "", payload, operationName)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[CustomerSummary](*customerOperationFailure(operationName, sdkgo.FailureTransport, "customer dispatch did not start"), 0)
	}
	if result.failure != nil {
		return sdkgo.NewMutationBranch(CreateCustomerBranchDefect, CustomerSummary{}, result.failure, sdkgo.Receipt{})
	}
	if result.status == http.StatusTooManyRequests || result.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationUncertain(CustomerSummary{}, *customerOperationFailure(operationName, sdkgo.FailureTransport, "retry checkpoint failed"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[CustomerSummary](*customerOperationFailure(operationName, sdkgo.FailureAvailability, "NetSuite requested a retry"), result.retryAfter)
	}
	if isCustomerRetry(result) {
		return sdkgo.NewMutationRetry[CustomerSummary](*customerOperationFailure(operationName, sdkgo.FailureAvailability, "customer creation outcome is unknown"), result.retryAfter)
	}
	if result.status != http.StatusNoContent {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CustomerSummary{}, *customerOperationFailure(operationName, sdkgo.FailureTransport, "an earlier customer create may have succeeded"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(CreateCustomerBranchProviderRejected, CustomerSummary{}, customerOperationFailure(operationName, sdkgo.FailureProviderRejection, "NetSuite rejected customer creation"), sdkgo.Receipt{})
	}
	locationURL, parseErr := url.Parse(result.location)
	if parseErr != nil {
		return sdkgo.NewMutationUncertain(CustomerSummary{}, *customerOperationFailure(operationName, sdkgo.FailureProtocol, "NetSuite returned an invalid customer Location"), sdkgo.Receipt{})
	}
	createdID := strings.TrimPrefix(locationURL.Path, "/services/rest/record/v1/customer/")
	if !customerIDPattern.MatchString(createdID) {
		return sdkgo.NewMutationUncertain(CustomerSummary{}, *customerOperationFailure(operationName, sdkgo.FailureProtocol, "NetSuite did not return a valid created customer ID"), sdkgo.Receipt{})
	}
	_ = fence.RecordProviderID(createdID)
	return sdkgo.NewMutationBranch(CreateCustomerBranchCreated, CustomerSummary{ID: createdID, EntityID: input.EntityID}, nil, sdkgo.Receipt{ProviderObjectID: createdID})
}

// Invoke replaces the displayed customer ID with the same value on every retry.
func (operation UpdateCustomerOperation) Invoke(call sdkgo.Call, input UpdateCustomerInput) sdkgo.MutationAttempt[CustomerSummary] {
	const operationName = "updateCustomer"
	if !customerIDPattern.MatchString(input.CustomerID) || strings.TrimSpace(input.EntityID) == "" || len(input.EntityID) > 256 {
		return sdkgo.NewMutationBranch(UpdateCustomerBranchDefect, CustomerSummary{}, customerOperationFailure(operationName, sdkgo.FailureValidation, "customer ID or entity ID is invalid"), sdkgo.Receipt{})
	}
	result := operation.client.requestCustomer(call, call.Context, http.MethodPatch, "/"+url.PathEscape(input.CustomerID), map[string]string{"entityId": input.EntityID}, operationName)
	if result.failure != nil {
		return sdkgo.NewMutationBranch(UpdateCustomerBranchDefect, CustomerSummary{}, result.failure, sdkgo.Receipt{})
	}
	if isCustomerRetry(result) {
		return sdkgo.NewMutationRetry[CustomerSummary](*customerOperationFailure(operationName, sdkgo.FailureAvailability, "customer update is temporarily unavailable"), result.retryAfter)
	}
	if result.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(UpdateCustomerBranchNotFound, CustomerSummary{}, customerOperationFailure(operationName, sdkgo.FailureNotFound, "customer was not found"), sdkgo.Receipt{})
	}
	if result.status != http.StatusNoContent {
		return sdkgo.NewMutationBranch(UpdateCustomerBranchProviderRejected, CustomerSummary{}, customerOperationFailure(operationName, sdkgo.FailureProviderRejection, "NetSuite rejected customer update"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(UpdateCustomerBranchUpdated, CustomerSummary{ID: input.CustomerID, EntityID: input.EntityID}, nil, sdkgo.Receipt{ProviderObjectID: input.CustomerID})
}

// Invoke deletes a customer; repeated deletion cannot create a duplicate.
func (operation DeleteCustomerOperation) Invoke(call sdkgo.Call, input GetCustomerInput) sdkgo.MutationAttempt[CustomerSummary] {
	const operationName = "deleteCustomer"
	if !customerIDPattern.MatchString(input.CustomerID) {
		return sdkgo.NewMutationBranch(DeleteCustomerBranchDefect, CustomerSummary{}, customerOperationFailure(operationName, sdkgo.FailureValidation, "customer ID is invalid"), sdkgo.Receipt{})
	}
	result := operation.client.requestCustomer(call, call.Context, http.MethodDelete, "/"+url.PathEscape(input.CustomerID), nil, operationName)
	if result.failure != nil {
		return sdkgo.NewMutationBranch(DeleteCustomerBranchDefect, CustomerSummary{}, result.failure, sdkgo.Receipt{})
	}
	if isCustomerRetry(result) {
		return sdkgo.NewMutationRetry[CustomerSummary](*customerOperationFailure(operationName, sdkgo.FailureAvailability, "customer deletion is temporarily unavailable"), result.retryAfter)
	}
	if result.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteCustomerBranchNotFound, CustomerSummary{}, customerOperationFailure(operationName, sdkgo.FailureNotFound, "customer was not found"), sdkgo.Receipt{})
	}
	if result.status != http.StatusNoContent {
		return sdkgo.NewMutationBranch(DeleteCustomerBranchProviderRejected, CustomerSummary{}, customerOperationFailure(operationName, sdkgo.FailureProviderRejection, "NetSuite rejected customer deletion"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(DeleteCustomerBranchDeleted, CustomerSummary{ID: input.CustomerID}, nil, sdkgo.Receipt{ProviderObjectID: input.CustomerID})
}
