// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package square

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Customer is the connector-safe subset of a Square customer profile.
type Customer struct {
	// ID is the Square customer ID.
	ID string `json:"id"`
	// GivenName is the customer's first name when present.
	GivenName string `json:"given_name,omitempty"`
	// FamilyName is the customer's last name when present.
	FamilyName string `json:"family_name,omitempty"`
	// EmailAddress is the customer's email address when present.
	EmailAddress string `json:"email_address,omitempty"`
	// ReferenceID is the seller's optional external system reference.
	ReferenceID string `json:"reference_id,omitempty"`
}

// CustomerPage is one page of Square customer profiles.
type CustomerPage struct {
	// Customers contains zero or more profiles.
	Customers []Customer `json:"customers"`
	// Cursor continues listing when set.
	Cursor string `json:"cursor"`
}

// ListCustomersInput selects one Square customer page.
type ListCustomersInput struct {
	// Cursor continues a previous page; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
}

// SearchCustomersInput finds profiles by exact email or external reference.
type SearchCustomersInput struct {
	// EmailAddress matches a customer's email address exactly when set.
	EmailAddress string `json:"emailAddress,omitempty"`
	// ReferenceID matches a customer's external reference exactly when set.
	ReferenceID string `json:"referenceId,omitempty"`
	// Cursor continues a previous search page; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
}

// GetCustomerInput identifies one Square customer.
type GetCustomerInput struct {
	// CustomerID is the Square customer ID.
	CustomerID string `json:"customerId"`
}

// CreateCustomerInput supplies public profile fields for a new Square customer.
type CreateCustomerInput struct {
	// GivenName is the customer's first name; at least one name or email is required.
	GivenName string `json:"givenName,omitempty"`
	// FamilyName is the customer's last name; at least one name or email is required.
	FamilyName string `json:"familyName,omitempty"`
	// EmailAddress is the customer's email address; at least one name or email is required.
	EmailAddress string `json:"emailAddress,omitempty"`
	// ReferenceID is an optional external system reference, not an idempotency key.
	ReferenceID string `json:"referenceId,omitempty"`
}

// UpdateCustomerInput replaces selected public fields on an existing Square customer.
type UpdateCustomerInput struct {
	// CustomerID is the Square customer ID.
	CustomerID string `json:"customerId"`
	// GivenName replaces the first name when nonblank.
	GivenName string `json:"givenName,omitempty"`
	// FamilyName replaces the last name when nonblank.
	FamilyName string `json:"familyName,omitempty"`
	// EmailAddress replaces the email address when nonblank.
	EmailAddress string `json:"emailAddress,omitempty"`
}

// DeleteCustomerInput identifies a Square customer to delete.
type DeleteCustomerInput struct {
	// CustomerID is the Square customer ID.
	CustomerID string `json:"customerId"`
}

// DeletedCustomer reports the deleted Square customer ID.
type DeletedCustomer struct {
	// CustomerID is the deleted Square customer ID.
	CustomerID string `json:"customerId"`
}

// ListCustomersOperation lists Square customer profiles.
type ListCustomersOperation struct{ client *Client }

// SearchCustomersOperation searches Square customer profiles.
type SearchCustomersOperation struct{ client *Client }

// GetCustomerOperation reads one Square customer profile.
type GetCustomerOperation struct{ client *Client }

// CreateCustomerOperation creates one Square customer profile.
type CreateCustomerOperation struct{ client *Client }

// UpdateCustomerOperation updates one Square customer profile.
type UpdateCustomerOperation struct{ client *Client }

// DeleteCustomerOperation deletes one Square customer profile.
type DeleteCustomerOperation struct{ client *Client }

// ListCustomers binds the listCustomers Query to this client.
func (client *Client) ListCustomers() ListCustomersOperation { return ListCustomersOperation{client} }

// SearchCustomers binds the searchCustomers Query to this client.
func (client *Client) SearchCustomers() SearchCustomersOperation {
	return SearchCustomersOperation{client}
}

// GetCustomer binds the getCustomer Query to this client.
func (client *Client) GetCustomer() GetCustomerOperation { return GetCustomerOperation{client} }

// CreateCustomer binds the createCustomer Mutation to this client.
func (client *Client) CreateCustomer() CreateCustomerOperation {
	return CreateCustomerOperation{client}
}

// UpdateCustomer binds the updateCustomer Mutation to this client.
func (client *Client) UpdateCustomer() UpdateCustomerOperation {
	return UpdateCustomerOperation{client}
}

// DeleteCustomer binds the deleteCustomer Mutation to this client.
func (client *Client) DeleteCustomer() DeleteCustomerOperation {
	return DeleteCustomerOperation{client}
}

// Definition returns the manifest-generated contract.
func (ListCustomersOperation) Definition() sdkgo.QueryDefinition { return ListCustomersDefinition }

// Definition returns the manifest-generated contract.
func (SearchCustomersOperation) Definition() sdkgo.QueryDefinition { return SearchCustomersDefinition }

// Definition returns the manifest-generated contract.
func (GetCustomerOperation) Definition() sdkgo.QueryDefinition { return GetCustomerDefinition }

// Definition returns the manifest-generated contract.
func (CreateCustomerOperation) Definition() sdkgo.MutationDefinition { return CreateCustomerDefinition }

// Definition returns the manifest-generated contract.
func (UpdateCustomerOperation) Definition() sdkgo.MutationDefinition { return UpdateCustomerDefinition }

// Definition returns the manifest-generated contract.
func (DeleteCustomerOperation) Definition() sdkgo.MutationDefinition { return DeleteCustomerDefinition }

// IdempotencyKey derives a stable Square-compatible key for customer creation.
func (CreateCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCustomerInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// IdempotencyKey identifies repeated absolute updates of one customer.
func (UpdateCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCustomerInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// IdempotencyKey identifies repeated deletion of one customer.
func (DeleteCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteCustomerInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// Invoke lists one Square customer page.
func (operation ListCustomersOperation) Invoke(call sdkgo.Call, input ListCustomersInput) sdkgo.QueryAttempt[CustomerPage] {
	query := url.Values{}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	return executeRead(operation.client, call, "listCustomers", "/customers", query, ListCustomersBranchListed, ListCustomersBranchProviderRejected, ListCustomersBranchProviderRejected, ListCustomersBranchInvalidResponse, ListCustomersBranchDefect, decodeCustomerPage)
}

// Invoke reads one Square customer.
func (operation GetCustomerOperation) Invoke(call sdkgo.Call, input GetCustomerInput) sdkgo.QueryAttempt[Customer] {
	if !validPathSegment(input.CustomerID) {
		return failedAttempt[Customer]("getCustomer", GetCustomerBranchDefect, sdkgo.FailureValidation, "customerId must be a path segment")
	}
	return executeRead(operation.client, call, "getCustomer", "/customers/"+url.PathEscape(input.CustomerID), url.Values{}, GetCustomerBranchFound, GetCustomerBranchNotFound, GetCustomerBranchProviderRejected, GetCustomerBranchInvalidResponse, GetCustomerBranchDefect, decodeCustomer)
}

func decodeCustomerPage(body []byte) (CustomerPage, error) {
	var page CustomerPage
	if err := json.Unmarshal(body, &page); err != nil {
		return CustomerPage{}, err
	}
	if page.Customers == nil {
		page.Customers = []Customer{}
	}
	return page, nil
}

func decodeCustomer(body []byte) (Customer, error) {
	var response struct {
		Customer Customer `json:"customer"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Customer.ID == "" {
		return Customer{}, errors.New("missing customer")
	}
	return response.Customer, nil
}

// Invoke searches by exact email or external reference.
func (operation SearchCustomersOperation) Invoke(call sdkgo.Call, input SearchCustomersInput) sdkgo.QueryAttempt[CustomerPage] {
	if input.EmailAddress == "" && input.ReferenceID == "" {
		return failedAttempt[CustomerPage]("searchCustomers", SearchCustomersBranchDefect, sdkgo.FailureValidation, "emailAddress or referenceId is required")
	}
	filter := map[string]any{}
	if input.EmailAddress != "" {
		filter["email_address"] = map[string]string{"exact": input.EmailAddress}
	}
	if input.ReferenceID != "" {
		filter["reference_id"] = map[string]string{"exact": input.ReferenceID}
	}
	requestBody := map[string]any{"query": map[string]any{"filter": filter}}
	if input.Cursor != "" {
		requestBody["cursor"] = input.Cursor
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return failedAttempt[CustomerPage]("searchCustomers", SearchCustomersBranchDefect, sdkgo.FailureLocalDefect, "customer search input is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/customers/search", bytes.NewReader(payload))
	if err != nil {
		return failedAttempt[CustomerPage]("searchCustomers", SearchCustomersBranchDefect, sdkgo.FailureLocalDefect, "customer search request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	authorization := operation.client.authorize(call, "searchCustomers", request, SearchCustomersBranchDefect, SearchCustomersBranchProviderRejected, SearchCustomersBranchInvalidResponse)
	if authorization.failure != nil {
		return sdkgo.NewQueryBranch(authorization.branch, CustomerPage{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[CustomerPage](*failureFor("searchCustomers", sdkgo.FailureAvailability, "provider customer search failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[CustomerPage](*failureFor("searchCustomers", sdkgo.FailureRateLimit, "provider rate limited customer search"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewQueryRetry[CustomerPage](*failureFor("searchCustomers", sdkgo.FailureAvailability, "provider customer search is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return failedAttempt[CustomerPage]("searchCustomers", SearchCustomersBranchProviderRejected, sdkgo.FailureProviderRejection, "provider rejected customer search with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedAttempt[CustomerPage]("searchCustomers", SearchCustomersBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[CustomerPage](*failureFor("searchCustomers", sdkgo.FailureAvailability, "provider customer search response was interrupted"), 0)
	}
	page, err := decodeCustomerPage(body)
	if err != nil {
		return failedAttempt[CustomerPage]("searchCustomers", SearchCustomersBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned invalid JSON")
	}
	return sdkgo.NewQueryBranch(SearchCustomersBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke creates a customer with Square's request idempotency key.
func (operation CreateCustomerOperation) Invoke(call sdkgo.Call, input CreateCustomerInput) sdkgo.MutationAttempt[Customer] {
	if input.GivenName == "" && input.FamilyName == "" && input.EmailAddress == "" {
		return customerMutationDefect("createCustomer", CreateCustomerBranchDefect)
	}
	body := map[string]any{"idempotency_key": call.IdempotencyKey}
	if input.GivenName != "" {
		body["given_name"] = input.GivenName
	}
	if input.FamilyName != "" {
		body["family_name"] = input.FamilyName
	}
	if input.EmailAddress != "" {
		body["email_address"] = input.EmailAddress
	}
	if input.ReferenceID != "" {
		body["reference_id"] = input.ReferenceID
	}
	return executeCustomerMutation(operation.client, call, customerWriteRequest{"createCustomer", http.MethodPost, "/customers", body, CreateCustomerBranchCreated, "", CreateCustomerBranchProviderRejected, CreateCustomerBranchInvalidResponse, CreateCustomerBranchDefect})
}

// Invoke replaces selected customer profile fields.
func (operation UpdateCustomerOperation) Invoke(call sdkgo.Call, input UpdateCustomerInput) sdkgo.MutationAttempt[Customer] {
	if !validPathSegment(input.CustomerID) || (input.GivenName == "" && input.FamilyName == "" && input.EmailAddress == "") {
		return customerMutationDefect("updateCustomer", UpdateCustomerBranchDefect)
	}
	body := map[string]any{}
	if input.GivenName != "" {
		body["given_name"] = input.GivenName
	}
	if input.FamilyName != "" {
		body["family_name"] = input.FamilyName
	}
	if input.EmailAddress != "" {
		body["email_address"] = input.EmailAddress
	}
	return executeCustomerMutation(operation.client, call, customerWriteRequest{"updateCustomer", http.MethodPut, "/customers/" + url.PathEscape(input.CustomerID), body, UpdateCustomerBranchUpdated, UpdateCustomerBranchNotFound, UpdateCustomerBranchProviderRejected, UpdateCustomerBranchInvalidResponse, UpdateCustomerBranchDefect})
}

// Invoke deletes a customer profile by ID.
func (operation DeleteCustomerOperation) Invoke(call sdkgo.Call, input DeleteCustomerInput) sdkgo.MutationAttempt[DeletedCustomer] {
	if !validPathSegment(input.CustomerID) {
		return sdkgo.NewMutationBranch(DeleteCustomerBranchDefect, DeletedCustomer{}, failureFor("deleteCustomer", sdkgo.FailureValidation, "customerId must be a path segment"), sdkgo.Receipt{})
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodDelete, operation.client.baseURL+"/customers/"+url.PathEscape(input.CustomerID), nil)
	if err != nil {
		return sdkgo.NewMutationBranch(DeleteCustomerBranchDefect, DeletedCustomer{}, failureFor("deleteCustomer", sdkgo.FailureLocalDefect, "customer delete request could not be built"), sdkgo.Receipt{})
	}
	request.Header.Set("Accept", "application/json")
	authorization := operation.client.authorize(call, "deleteCustomer", request, DeleteCustomerBranchDefect, DeleteCustomerBranchProviderRejected, DeleteCustomerBranchDefect)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, DeletedCustomer{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedCustomer](*failureFor("deleteCustomer", sdkgo.FailureAvailability, "provider delete outcome is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[DeletedCustomer](*failureFor("deleteCustomer", sdkgo.FailureRateLimit, "provider rate limited customer deletion"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[DeletedCustomer](*failureFor("deleteCustomer", sdkgo.FailureAvailability, "provider customer deletion is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusNotFound:
		return sdkgo.NewMutationBranch(DeleteCustomerBranchNotFound, DeletedCustomer{}, failureFor("deleteCustomer", sdkgo.FailureNotFound, "customer was not found"), sdkgo.Receipt{})
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return sdkgo.NewMutationBranch(DeleteCustomerBranchProviderRejected, DeletedCustomer{}, failureFor("deleteCustomer", sdkgo.FailureProviderRejection, "provider rejected customer deletion with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(DeleteCustomerBranchDeleted, DeletedCustomer{CustomerID: input.CustomerID}, nil, sdkgo.Receipt{})
}

type customerWriteRequest struct {
	operation      string
	method         string
	path           string
	body           map[string]any
	successBranch  sdkgo.BranchID
	notFoundBranch sdkgo.BranchID
	rejectedBranch sdkgo.BranchID
	invalidBranch  sdkgo.BranchID
	defectBranch   sdkgo.BranchID
}

func customerMutationDefect(operation string, branch sdkgo.BranchID) sdkgo.MutationAttempt[Customer] {
	return sdkgo.NewMutationBranch(branch, Customer{}, failureFor(operation, sdkgo.FailureValidation, "customer input is invalid"), sdkgo.Receipt{})
}

func executeCustomerMutation(client *Client, call sdkgo.Call, write customerWriteRequest) sdkgo.MutationAttempt[Customer] {
	payload, err := json.Marshal(write.body)
	if err != nil {
		return customerMutationDefect(write.operation, write.defectBranch)
	}
	request, err := http.NewRequestWithContext(call.Context, write.method, client.baseURL+write.path, bytes.NewReader(payload))
	if err != nil {
		return customerMutationDefect(write.operation, write.defectBranch)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	authorization := client.authorize(call, write.operation, request, write.defectBranch, write.rejectedBranch, write.invalidBranch)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, Customer{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[Customer](*failureFor(write.operation, sdkgo.FailureAvailability, "provider customer write outcome is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[Customer](*failureFor(write.operation, sdkgo.FailureRateLimit, "provider rate limited customer write"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[Customer](*failureFor(write.operation, sdkgo.FailureAvailability, "provider customer write is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusNotFound && write.notFoundBranch != "":
		return sdkgo.NewMutationBranch(write.notFoundBranch, Customer{}, failureFor(write.operation, sdkgo.FailureNotFound, "customer was not found"), sdkgo.Receipt{})
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return sdkgo.NewMutationBranch(write.rejectedBranch, Customer{}, failureFor(write.operation, sdkgo.FailureProviderRejection, "provider rejected customer write with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationBranch(write.invalidBranch, Customer{}, failureFor(write.operation, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit"), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[Customer](*failureFor(write.operation, sdkgo.FailureAvailability, "provider customer response was interrupted"), 0)
	}
	customer, err := decodeCustomer(body)
	if err != nil {
		return sdkgo.NewMutationBranch(write.invalidBranch, Customer{}, failureFor(write.operation, sdkgo.FailureProtocol, "provider returned invalid JSON"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(write.successBranch, customer, nil, sdkgo.Receipt{})
}
