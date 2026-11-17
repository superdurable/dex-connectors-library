// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paddle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// CustomerIDInput selects one Paddle customer by its non-secret ID.
type CustomerIDInput struct {
	// CustomerID is a ctm_ ID from the Paddle Customers page or an API response.
	CustomerID string `json:"customerId"`
}

// ListCustomersInput selects a page of Paddle customers.
type ListCustomersInput struct {
	// PerPage is the page size from 1 through 200.
	PerPage int `json:"perPage"`
	// After is an optional ctm_ ID from the previous page's nextCursor.
	After string `json:"after,omitempty"`
	// Email optionally filters for one exact email address.
	Email string `json:"email,omitempty"`
}

// CreateCustomerInput supplies a customer's email and name.
type CreateCustomerInput struct {
	// Email is the unique customer email address.
	Email string `json:"email"`
	// Name is the customer's full name; blank omits it.
	Name string `json:"name,omitempty"`
}

// UpdateCustomerInput replaces a customer's name and email.
type UpdateCustomerInput struct {
	// CustomerID identifies the customer to update.
	CustomerID string `json:"customerId"`
	// Email is the replacement email address.
	Email string `json:"email"`
	// Name is the replacement full name.
	Name string `json:"name"`
}

// PaddleCustomer contains customer identity and non-secret profile fields.
type PaddleCustomer struct {
	// ID is the Paddle customer ID.
	ID string `json:"id"`
	// Email is the customer's email address.
	Email string `json:"email"`
	// Name is the customer's full name, when supplied.
	Name string `json:"name"`
	// Status is active or archived.
	Status string `json:"status"`
}

// CustomerPage contains one page and its next cursor.
type CustomerPage struct {
	// Items are customers in this page.
	Items []PaddleCustomer `json:"items"`
	// HasMore says whether another page exists.
	HasMore bool `json:"hasMore"`
	// NextCursor is the next page's ctm_ ID, or blank after the last page.
	NextCursor string `json:"nextCursor"`
}

// ListCustomersOperation lists Paddle customers.
type ListCustomersOperation struct{ client *Client }

// GetCustomerOperation reads one Paddle customer.
type GetCustomerOperation struct{ client *Client }

// CreateCustomerOperation creates one Paddle customer.
type CreateCustomerOperation struct{ client *Client }

// UpdateCustomerOperation replaces customer name and email.
type UpdateCustomerOperation struct{ client *Client }

// ArchiveCustomerOperation archives a customer.
type ArchiveCustomerOperation struct{ client *Client }

// ListCustomers binds the listing Query.
func (client *Client) ListCustomers() ListCustomersOperation { return ListCustomersOperation{client} }

// GetCustomer binds the lookup Query.
func (client *Client) GetCustomer() GetCustomerOperation { return GetCustomerOperation{client} }

// CreateCustomer binds the creation Mutation.
func (client *Client) CreateCustomer() CreateCustomerOperation {
	return CreateCustomerOperation{client}
}

// UpdateCustomer binds the update Mutation.
func (client *Client) UpdateCustomer() UpdateCustomerOperation {
	return UpdateCustomerOperation{client}
}

// ArchiveCustomer binds the archive Mutation.
func (client *Client) ArchiveCustomer() ArchiveCustomerOperation {
	return ArchiveCustomerOperation{client}
}

// Definition returns the generated listing contract.
func (ListCustomersOperation) Definition() sdkgo.QueryDefinition { return ListCustomersDefinition }

// Definition returns the generated lookup contract.
func (GetCustomerOperation) Definition() sdkgo.QueryDefinition { return GetCustomerDefinition }

// Definition returns the generated creation contract.
func (CreateCustomerOperation) Definition() sdkgo.MutationDefinition { return CreateCustomerDefinition }

// Definition returns the generated update contract.
func (UpdateCustomerOperation) Definition() sdkgo.MutationDefinition { return UpdateCustomerDefinition }

// Definition returns the generated archive contract.
func (ArchiveCustomerOperation) Definition() sdkgo.MutationDefinition {
	return ArchiveCustomerDefinition
}

// IdempotencyKey records call identity; Paddle does not deduplicate customer creation with this key.
func (CreateCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records the replacement call identity.
func (UpdateCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records the archive call identity.
func (ArchiveCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ CustomerIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

type paddleExchange struct {
	status     int
	body       []byte
	retryAfter time.Duration
	err        error
}

func (client *Client) exchange(call sdkgo.Call, ctx context.Context, method, path string, payload any) paddleExchange {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return paddleExchange{err: errCredential}
	}
	apiKey := credentials.APIKey.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiKey) {
		return paddleExchange{err: errCredential}
	}
	var requestBody io.Reader
	if payload != nil {
		encoded, encodeErr := json.Marshal(payload)
		if encodeErr != nil {
			return paddleExchange{err: errRequest}
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, requestBody)
	if err != nil {
		return paddleExchange{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return paddleExchange{err: errRequest}
	}
	defer response.Body.Close()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if bytes.Contains(body, []byte(apiKey)) {
		return paddleExchange{err: errCredential}
	}
	return paddleExchange{status: response.StatusCode, body: body, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}

func customerFailure(operationName string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "paddle", Operation: operationName, Message: message}
}

func customerRetryable(exchange paddleExchange) bool {
	return errors.Is(exchange.err, errRequest) || exchange.status == http.StatusRequestTimeout || exchange.status == http.StatusTooManyRequests || exchange.status >= 500
}

func validPaddleID(identifier, prefix string) bool {
	if len(identifier) != len(prefix)+26 || !strings.HasPrefix(identifier, prefix) {
		return false
	}
	for _, character := range identifier[len(prefix):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validCustomerEmail(email string) bool {
	return len(email) > 2 && len(email) <= 320 && strings.Count(email, "@") == 1 && !strings.ContainsAny(email, " \r\n\t")
}

func decodePaddleCustomer(body []byte, expectedID string) (PaddleCustomer, bool) {
	var envelope struct {
		Data struct {
			ID     string `json:"id"`
			Email  string `json:"email"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return PaddleCustomer{}, false
	}
	customer := PaddleCustomer{ID: envelope.Data.ID, Email: envelope.Data.Email, Name: envelope.Data.Name, Status: envelope.Data.Status}
	if !validPaddleID(customer.ID, "ctm_") || !validCustomerEmail(customer.Email) || (customer.Status != "active" && customer.Status != "archived") || expectedID != "" && expectedID != customer.ID {
		return PaddleCustomer{}, false
	}
	return customer, true
}

func decodeCustomerPage(body []byte) (CustomerPage, bool) {
	var envelope struct {
		Data []PaddleCustomer `json:"data"`
		Meta struct {
			Pagination struct {
				Next    string `json:"next"`
				HasMore bool   `json:"has_more"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Data == nil {
		return CustomerPage{}, false
	}
	page := CustomerPage{Items: envelope.Data, HasMore: envelope.Meta.Pagination.HasMore}
	for _, customer := range page.Items {
		if !validPaddleID(customer.ID, "ctm_") || !validCustomerEmail(customer.Email) || (customer.Status != "active" && customer.Status != "archived") {
			return CustomerPage{}, false
		}
	}
	if page.HasMore {
		next, err := url.Parse(envelope.Meta.Pagination.Next)
		if err != nil || next.Path != "/customers" || !validPaddleID(next.Query().Get("after"), "ctm_") {
			return CustomerPage{}, false
		}
		page.NextCursor = next.Query().Get("after")
	}
	return page, true
}

// Invoke reads one customer page.
func (operation ListCustomersOperation) Invoke(call sdkgo.Call, input ListCustomersInput) sdkgo.QueryAttempt[CustomerPage] {
	const name = "listCustomers"
	if input.PerPage < 1 || input.PerPage > 200 || input.After != "" && !validPaddleID(input.After, "ctm_") || input.Email != "" && !validCustomerEmail(input.Email) {
		return sdkgo.NewQueryBranch(ListCustomersBranchDefect, CustomerPage{}, customerFailure(name, sdkgo.FailureValidation, "customer page input is invalid"), sdkgo.Receipt{})
	}
	query := url.Values{"per_page": {strconv.Itoa(input.PerPage)}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	if input.Email != "" {
		query.Set("email", input.Email)
	}
	exchange := operation.client.exchange(call, call.Context, http.MethodGet, "/customers?"+query.Encode(), nil)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewQueryBranch(ListCustomersBranchDefect, CustomerPage{}, customerFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if customerRetryable(exchange) {
		return sdkgo.NewQueryRetry[CustomerPage](*customerFailure(name, sdkgo.FailureAvailability, "customer listing is unavailable"), exchange.retryAfter)
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewQueryBranch(ListCustomersBranchProviderRejected, CustomerPage{}, customerFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected customer listing"), sdkgo.Receipt{})
	}
	page, ok := decodeCustomerPage(exchange.body)
	if !ok || exchange.err != nil {
		return sdkgo.NewQueryBranch(ListCustomersBranchInvalidResponse, CustomerPage{}, customerFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid customer page"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(ListCustomersBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke reads one customer by ID.
func (operation GetCustomerOperation) Invoke(call sdkgo.Call, input CustomerIDInput) sdkgo.QueryAttempt[PaddleCustomer] {
	const name = "getCustomer"
	if !validPaddleID(input.CustomerID, "ctm_") {
		return sdkgo.NewQueryBranch(GetCustomerBranchDefect, PaddleCustomer{}, customerFailure(name, sdkgo.FailureValidation, "customer ID is invalid"), sdkgo.Receipt{})
	}
	exchange := operation.client.exchange(call, call.Context, http.MethodGet, "/customers/"+url.PathEscape(input.CustomerID), nil)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewQueryBranch(GetCustomerBranchDefect, PaddleCustomer{}, customerFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if customerRetryable(exchange) {
		return sdkgo.NewQueryRetry[PaddleCustomer](*customerFailure(name, sdkgo.FailureAvailability, "customer lookup is unavailable"), exchange.retryAfter)
	}
	if exchange.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetCustomerBranchNotFound, PaddleCustomer{}, customerFailure(name, sdkgo.FailureNotFound, "customer was not found"), sdkgo.Receipt{})
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewQueryBranch(GetCustomerBranchProviderRejected, PaddleCustomer{}, customerFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected customer lookup"), sdkgo.Receipt{})
	}
	customer, ok := decodePaddleCustomer(exchange.body, input.CustomerID)
	if !ok || exchange.err != nil {
		return sdkgo.NewQueryBranch(GetCustomerBranchInvalidResponse, PaddleCustomer{}, customerFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid customer"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetCustomerBranchFound, customer, nil, sdkgo.Receipt{})
}

// Invoke creates a customer with a bounded possible-send count and email read-back.
func (operation CreateCustomerOperation) Invoke(call sdkgo.Call, input CreateCustomerInput) sdkgo.MutationAttempt[PaddleCustomer] {
	const name = "createCustomer"
	if !validCustomerEmail(input.Email) || len(input.Name) > 1024 {
		return sdkgo.NewMutationBranch(CreateCustomerBranchDefect, PaddleCustomer{}, customerFailure(name, sdkgo.FailureValidation, "customer email or name is invalid"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateCustomerDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateCustomerBranchDefect, PaddleCustomer{}, customerFailure(name, sdkgo.FailureLocalDefect, "customer dispatch fence is unavailable"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationUncertain(PaddleCustomer{}, *customerFailure(name, sdkgo.FailureTransport, "customer creation outcome is unknown"), sdkgo.Receipt{})
	}
	if decision.Kind != singledispatch.SendAllowed {
		query := url.Values{"email": {input.Email}, "per_page": {"1"}}
		readBack := operation.client.exchange(call, call.Context, http.MethodGet, "/customers?"+query.Encode(), nil)
		if readBack.status == http.StatusOK && readBack.err == nil {
			page, ok := decodeCustomerPage(readBack.body)
			if !ok {
				return sdkgo.NewMutationUncertain(PaddleCustomer{}, *customerFailure(name, sdkgo.FailureProtocol, "customer read-back is invalid"), sdkgo.Receipt{})
			}
			if len(page.Items) == 1 && page.Items[0].Email == input.Email && page.Items[0].Name == input.Name {
				return sdkgo.NewMutationBranch(CreateCustomerBranchCreated, page.Items[0], nil, sdkgo.Receipt{ProviderObjectID: page.Items[0].ID})
			}
			if len(page.Items) != 0 {
				return sdkgo.NewMutationUncertain(PaddleCustomer{}, *customerFailure(name, sdkgo.FailureProtocol, "customer read-back disagreed with request"), sdkgo.Receipt{})
			}
		} else if customerRetryable(readBack) {
			return sdkgo.NewMutationRetry[PaddleCustomer](*customerFailure(name, sdkgo.FailureAvailability, "customer read-back is unavailable"), readBack.retryAfter)
		} else {
			return sdkgo.NewMutationUncertain(PaddleCustomer{}, *customerFailure(name, sdkgo.FailureTransport, "customer read-back was rejected"), sdkgo.Receipt{})
		}
		if decision.Kind == singledispatch.ReadBackRequired || decision.MaximumPriorSends >= call.MaxSends() {
			return sdkgo.NewMutationUncertain(PaddleCustomer{}, *customerFailure(name, sdkgo.FailureTransport, "customer creation reached its send cap"), sdkgo.Receipt{})
		}
	}
	var exchange paddleExchange
	err = fence.Dispatch(func(ctx context.Context) error {
		exchange = operation.client.exchange(call, ctx, http.MethodPost, "/customers", map[string]string{"email": input.Email, "name": input.Name})
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[PaddleCustomer](*customerFailure(name, sdkgo.FailureTransport, "customer dispatch did not start"), 0)
	}
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewMutationBranch(CreateCustomerBranchDefect, PaddleCustomer{}, customerFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if exchange.status == http.StatusTooManyRequests || exchange.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationUncertain(PaddleCustomer{}, *customerFailure(name, sdkgo.FailureTransport, "retry checkpoint failed"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[PaddleCustomer](*customerFailure(name, sdkgo.FailureAvailability, "Paddle requested a retry"), exchange.retryAfter)
	}
	if customerRetryable(exchange) {
		return sdkgo.NewMutationRetry[PaddleCustomer](*customerFailure(name, sdkgo.FailureAvailability, "customer creation outcome is unknown"), exchange.retryAfter)
	}
	if exchange.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(PaddleCustomer{}, *customerFailure(name, sdkgo.FailureTransport, "an earlier create may have succeeded"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(CreateCustomerBranchProviderRejected, PaddleCustomer{}, customerFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected customer creation"), sdkgo.Receipt{})
	}
	customer, ok := decodePaddleCustomer(exchange.body, "")
	if !ok || exchange.err != nil || customer.Email != input.Email {
		return sdkgo.NewMutationUncertain(PaddleCustomer{}, *customerFailure(name, sdkgo.FailureProtocol, "Paddle accepted customer creation but returned invalid data"), sdkgo.Receipt{})
	}
	_ = fence.RecordProviderID(customer.ID)
	return sdkgo.NewMutationBranch(CreateCustomerBranchCreated, customer, nil, sdkgo.Receipt{ProviderObjectID: customer.ID})
}

// Invoke replaces a customer's email and name; replaying the same values is safe.
func (operation UpdateCustomerOperation) Invoke(call sdkgo.Call, input UpdateCustomerInput) sdkgo.MutationAttempt[PaddleCustomer] {
	const name = "updateCustomer"
	if !validPaddleID(input.CustomerID, "ctm_") || !validCustomerEmail(input.Email) || len(input.Name) > 1024 {
		return sdkgo.NewMutationBranch(UpdateCustomerBranchDefect, PaddleCustomer{}, customerFailure(name, sdkgo.FailureValidation, "customer ID, email, or name is invalid"), sdkgo.Receipt{})
	}
	exchange := operation.client.exchange(call, call.Context, http.MethodPatch, "/customers/"+url.PathEscape(input.CustomerID), map[string]string{"email": input.Email, "name": input.Name})
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewMutationBranch(UpdateCustomerBranchDefect, PaddleCustomer{}, customerFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if customerRetryable(exchange) {
		return sdkgo.NewMutationRetry[PaddleCustomer](*customerFailure(name, sdkgo.FailureAvailability, "customer update is unavailable"), exchange.retryAfter)
	}
	if exchange.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(UpdateCustomerBranchNotFound, PaddleCustomer{}, customerFailure(name, sdkgo.FailureNotFound, "customer was not found"), sdkgo.Receipt{})
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewMutationBranch(UpdateCustomerBranchProviderRejected, PaddleCustomer{}, customerFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected customer update"), sdkgo.Receipt{})
	}
	customer, ok := decodePaddleCustomer(exchange.body, input.CustomerID)
	if !ok || exchange.err != nil {
		return sdkgo.NewMutationBranch(UpdateCustomerBranchInvalidResponse, PaddleCustomer{}, customerFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid customer"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(UpdateCustomerBranchUpdated, customer, nil, sdkgo.Receipt{ProviderObjectID: customer.ID})
}

// Invoke archives a customer; repeating the same status replacement is safe.
func (operation ArchiveCustomerOperation) Invoke(call sdkgo.Call, input CustomerIDInput) sdkgo.MutationAttempt[PaddleCustomer] {
	const name = "archiveCustomer"
	if !validPaddleID(input.CustomerID, "ctm_") {
		return sdkgo.NewMutationBranch(ArchiveCustomerBranchDefect, PaddleCustomer{}, customerFailure(name, sdkgo.FailureValidation, "customer ID is invalid"), sdkgo.Receipt{})
	}
	exchange := operation.client.exchange(call, call.Context, http.MethodPatch, "/customers/"+url.PathEscape(input.CustomerID), map[string]string{"status": "archived"})
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewMutationBranch(ArchiveCustomerBranchDefect, PaddleCustomer{}, customerFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if customerRetryable(exchange) {
		return sdkgo.NewMutationRetry[PaddleCustomer](*customerFailure(name, sdkgo.FailureAvailability, "customer archival is unavailable"), exchange.retryAfter)
	}
	if exchange.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(ArchiveCustomerBranchNotFound, PaddleCustomer{}, customerFailure(name, sdkgo.FailureNotFound, "customer was not found"), sdkgo.Receipt{})
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewMutationBranch(ArchiveCustomerBranchProviderRejected, PaddleCustomer{}, customerFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected customer archival"), sdkgo.Receipt{})
	}
	customer, ok := decodePaddleCustomer(exchange.body, input.CustomerID)
	if !ok || exchange.err != nil || customer.Status != "archived" {
		return sdkgo.NewMutationBranch(ArchiveCustomerBranchInvalidResponse, PaddleCustomer{}, customerFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid archive result"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(ArchiveCustomerBranchArchived, customer, nil, sdkgo.Receipt{ProviderObjectID: customer.ID})
}
