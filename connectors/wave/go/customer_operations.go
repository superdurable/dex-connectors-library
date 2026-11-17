// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wave

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const listCustomersGraphQL = "query ListCustomers($businessId: ID!, $page: Int!, $pageSize: Int!) { business(id: $businessId) { id customers(page: $page, pageSize: $pageSize, sort: [NAME_ASC]) { pageInfo { currentPage totalPages totalCount } edges { node { id name } } } } }"
const createCustomerGraphQL = "mutation CreateCustomer($input: CustomerCreateInput!) { customerCreate(input: $input) { didSucceed inputErrors { code } customer { id name } } }"
const updateCustomerGraphQL = "mutation UpdateCustomer($input: CustomerPatchInput!) { customerPatch(input: $input) { didSucceed inputErrors { code } customer { id name } } }"
const deleteCustomerGraphQL = "mutation DeleteCustomer($input: CustomerDeleteInput!) { customerDelete(input: $input) { didSucceed inputErrors { code } } }"

// ListCustomersInput selects a business and one 1-based page.
type ListCustomersInput struct {
	// BusinessID is the exact Wave business ID.
	BusinessID string `json:"businessId"`
	// Page is the 1-based page number.
	Page int `json:"page"`
	// PageSize is the number of customers, from 1 to 100.
	PageSize int `json:"pageSize"`
}

// CustomerPage is one page of customers within a business.
type CustomerPage struct {
	// BusinessID identifies the queried business.
	BusinessID string `json:"businessId"`
	// Customers contains returned customer identities and names.
	Customers []CustomerProfile `json:"customers"`
	// CurrentPage is the returned 1-based page.
	CurrentPage int `json:"currentPage"`
	// TotalPages is the number of available pages.
	TotalPages int `json:"totalPages"`
	// TotalCount is the number of matching customers.
	TotalCount int `json:"totalCount"`
}

// CreateCustomerInput names a customer within one business.
type CreateCustomerInput struct {
	// BusinessID is the exact Wave business ID.
	BusinessID string `json:"businessId"`
	// Name is the customer's display name.
	Name string `json:"name"`
}

// UpdateCustomerInput replaces a customer's display name.
type UpdateCustomerInput struct {
	// BusinessID identifies the business for the returned profile.
	BusinessID string `json:"businessId"`
	// CustomerID is the exact Wave customer ID.
	CustomerID string `json:"customerId"`
	// Name is the replacement display name.
	Name string `json:"name"`
}

// DeleteCustomerInput identifies a customer to delete.
type DeleteCustomerInput struct {
	// CustomerID is the exact Wave customer ID.
	CustomerID string `json:"customerId"`
}

// DeletedCustomer identifies a deleted customer.
type DeletedCustomer struct {
	// ID is the deleted Wave customer ID.
	ID string `json:"id"`
}

// ListCustomersOperation reads one Wave customer page.
type ListCustomersOperation struct{ client *Client }

// ListCustomers binds the Query to this client.
func (client *Client) ListCustomers() ListCustomersOperation { return ListCustomersOperation{client} }

// Definition returns the generated Query contract.
func (ListCustomersOperation) Definition() sdkgo.QueryDefinition { return ListCustomersDefinition }

// Invoke reads the requested customer page.
func (operation ListCustomersOperation) Invoke(call sdkgo.Call, input ListCustomersInput) sdkgo.QueryAttempt[CustomerPage] {
	if !validGraphQLID(input.BusinessID) || input.Page < 1 || input.PageSize < 1 || input.PageSize > 100 {
		return waveQueryBranch[CustomerPage]("listCustomers", "defect", sdkgo.FailureValidation, "Wave customer page input is invalid")
	}
	variables := struct {
		BusinessID string `json:"businessId"`
		Page       int    `json:"page"`
		PageSize   int    `json:"pageSize"`
	}{input.BusinessID, input.Page, input.PageSize}
	response := operation.client.sendGraphQL(call, call.Context, listCustomersGraphQL, variables, false)
	if retry, hasRetry := waveQueryRetry[CustomerPage]("listCustomers", response); hasRetry {
		return retry
	}
	if errors.Is(response.err, errCredential) {
		return waveQueryBranch[CustomerPage]("listCustomers", "defect", sdkgo.FailureAuthentication, "Wave credentials are invalid")
	}
	if response.status != http.StatusOK {
		return waveQueryBranch[CustomerPage]("listCustomers", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected customer listing")
	}
	if response.err != nil {
		return waveQueryBranch[CustomerPage]("listCustomers", "invalidResponse", sdkgo.FailureResponseTooLarge, "Wave customer response exceeded the limit")
	}
	var wire struct {
		Data *struct {
			Business *struct {
				Customers *struct {
					PageInfo struct {
						CurrentPage int `json:"currentPage"`
						TotalPages  int `json:"totalPages"`
						TotalCount  int `json:"totalCount"`
					} `json:"pageInfo"`
					Edges []struct {
						Node CustomerProfile `json:"node"`
					} `json:"edges"`
				} `json:"customers"`
			} `json:"business"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || len(wire.Errors) > 0 {
		return waveQueryBranch[CustomerPage]("listCustomers", "invalidResponse", sdkgo.FailureProtocol, "Wave returned invalid customer JSON")
	}
	if wire.Data.Business == nil {
		return waveQueryBranch[CustomerPage]("listCustomers", "notFound", sdkgo.FailureNotFound, "Wave did not find the business")
	}
	if wire.Data.Business.Customers == nil || wire.Data.Business.Customers.Edges == nil {
		return waveQueryBranch[CustomerPage]("listCustomers", "invalidResponse", sdkgo.FailureProtocol, "Wave returned incomplete customer data")
	}
	page := CustomerPage{BusinessID: input.BusinessID, Customers: []CustomerProfile{}, CurrentPage: wire.Data.Business.Customers.PageInfo.CurrentPage, TotalPages: wire.Data.Business.Customers.PageInfo.TotalPages, TotalCount: wire.Data.Business.Customers.PageInfo.TotalCount}
	for _, edge := range wire.Data.Business.Customers.Edges {
		if edge.Node.ID == "" || edge.Node.Name == "" {
			return waveQueryBranch[CustomerPage]("listCustomers", "invalidResponse", sdkgo.FailureProtocol, "Wave returned incomplete customer data")
		}
		edge.Node.BusinessID = input.BusinessID
		page.Customers = append(page.Customers, edge.Node)
	}
	return sdkgo.NewQueryBranch(sdkgo.BranchID("listed"), page, nil, sdkgo.Receipt{})
}

// CreateCustomerOperation creates a customer with bounded possible sends.
type CreateCustomerOperation struct{ client *Client }

// CreateCustomer binds creation to this client.
func (client *Client) CreateCustomer() CreateCustomerOperation {
	return CreateCustomerOperation{client}
}

// Definition returns the generated Mutation contract.
func (CreateCustomerOperation) Definition() sdkgo.MutationDefinition { return CreateCustomerDefinition }

// IdempotencyKey identifies the logical customer creation.
func (CreateCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a customer, selecting uncertainty when the send cap is reached.
func (operation CreateCustomerOperation) Invoke(call sdkgo.Call, input CreateCustomerInput) sdkgo.MutationAttempt[CustomerProfile] {
	if !validGraphQLID(input.BusinessID) || !validCustomerName(input.Name) {
		return waveMutationBranch[CustomerProfile]("createCustomer", "defect", sdkgo.FailureValidation, "Wave customer input is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateCustomerDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return waveMutationBranch[CustomerProfile]("createCustomer", "defect", sdkgo.FailureLocalDefect, "Wave dispatch failed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return createCustomerUncertain()
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	variables := struct {
		Input CreateCustomerInput `json:"input"`
	}{input}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.sendGraphQL(call, requestContext, createCustomerGraphQL, variables, true)
		if errors.Is(response.err, errCredential) {
			return nil
		}
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return waveMutationRetry[CustomerProfile]("createCustomer", sdkgo.FailureAvailability)
	}
	if errors.Is(response.err, errCredential) {
		return createCustomerUncertain()
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return waveMutationRetry[CustomerProfile]("createCustomer", sdkgo.FailureAvailability)
		}
		return sdkgo.NewMutationRetry[CustomerProfile](waveFailure("createCustomer", sdkgo.FailureRateLimit, "Wave rate limited customer creation"), response.retryAfter)
	}
	if response.status == 0 || response.status == http.StatusRequestTimeout || response.status >= 500 || err != nil {
		if canResend {
			return waveMutationRetry[CustomerProfile]("createCustomer", sdkgo.FailureAvailability)
		}
		return createCustomerUncertain()
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return createCustomerUncertain()
		}
		return waveMutationBranch[CustomerProfile]("createCustomer", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected customer creation")
	}
	var wire struct {
		Data *struct {
			CustomerCreate *struct {
				DidSucceed  bool              `json:"didSucceed"`
				Customer    *CustomerProfile  `json:"customer"`
				InputErrors []json.RawMessage `json:"inputErrors"`
			} `json:"customerCreate"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || wire.Data.CustomerCreate == nil || len(wire.Errors) > 0 {
		return createCustomerUncertain()
	}
	if !wire.Data.CustomerCreate.DidSucceed {
		if decision.MaximumPriorSends > 0 {
			return createCustomerUncertain()
		}
		return waveMutationBranch[CustomerProfile]("createCustomer", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected customer creation")
	}
	profile := wire.Data.CustomerCreate.Customer
	if profile == nil || profile.ID == "" || profile.Name != input.Name {
		return createCustomerUncertain()
	}
	profile.BusinessID = input.BusinessID
	return sdkgo.NewMutationBranch(sdkgo.BranchID("created"), *profile, nil, sdkgo.Receipt{})
}

// UpdateCustomerOperation replaces a customer's name.
type UpdateCustomerOperation struct{ client *Client }

// UpdateCustomer binds an absolute rename to this client.
func (client *Client) UpdateCustomer() UpdateCustomerOperation {
	return UpdateCustomerOperation{client}
}

// Definition returns the generated Mutation contract.
func (UpdateCustomerOperation) Definition() sdkgo.MutationDefinition { return UpdateCustomerDefinition }

// IdempotencyKey identifies retries of one customer rename.
func (UpdateCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the customer's display name.
func (operation UpdateCustomerOperation) Invoke(call sdkgo.Call, input UpdateCustomerInput) sdkgo.MutationAttempt[CustomerProfile] {
	if !validGraphQLID(input.BusinessID) || !validGraphQLID(input.CustomerID) || !validCustomerName(input.Name) {
		return waveMutationBranch[CustomerProfile]("updateCustomer", "defect", sdkgo.FailureValidation, "Wave customer input is invalid")
	}
	variables := struct {
		Input struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"input"`
	}{}
	variables.Input.ID, variables.Input.Name = input.CustomerID, input.Name
	response := operation.client.sendGraphQL(call, call.Context, updateCustomerGraphQL, variables, false)
	if retry, hasRetry := waveMutationResponseRetry[CustomerProfile]("updateCustomer", response); hasRetry {
		return retry
	}
	if errors.Is(response.err, errCredential) {
		return waveMutationBranch[CustomerProfile]("updateCustomer", "defect", sdkgo.FailureAuthentication, "Wave credentials are invalid")
	}
	if response.status != http.StatusOK {
		return waveMutationBranch[CustomerProfile]("updateCustomer", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected the customer update")
	}
	if response.err != nil {
		return waveMutationBranch[CustomerProfile]("updateCustomer", "invalidResponse", sdkgo.FailureResponseTooLarge, "Wave customer response exceeded the limit")
	}
	var wire struct {
		Data *struct {
			CustomerPatch *struct {
				DidSucceed  bool              `json:"didSucceed"`
				Customer    *CustomerProfile  `json:"customer"`
				InputErrors []json.RawMessage `json:"inputErrors"`
			} `json:"customerPatch"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || wire.Data.CustomerPatch == nil || len(wire.Errors) > 0 {
		return waveMutationBranch[CustomerProfile]("updateCustomer", "invalidResponse", sdkgo.FailureProtocol, "Wave returned invalid customer JSON")
	}
	if !wire.Data.CustomerPatch.DidSucceed {
		return waveMutationBranch[CustomerProfile]("updateCustomer", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected the customer update")
	}
	profile := wire.Data.CustomerPatch.Customer
	if profile == nil || profile.ID != input.CustomerID || profile.Name != input.Name {
		return waveMutationBranch[CustomerProfile]("updateCustomer", "invalidResponse", sdkgo.FailureProtocol, "Wave returned inconsistent customer data")
	}
	profile.BusinessID = input.BusinessID
	return sdkgo.NewMutationBranch(sdkgo.BranchID("updated"), *profile, nil, sdkgo.Receipt{})
}

// DeleteCustomerOperation removes one customer by ID.
type DeleteCustomerOperation struct{ client *Client }

// DeleteCustomer binds deletion to this client.
func (client *Client) DeleteCustomer() DeleteCustomerOperation {
	return DeleteCustomerOperation{client}
}

// Definition returns the generated Mutation contract.
func (DeleteCustomerOperation) Definition() sdkgo.MutationDefinition { return DeleteCustomerDefinition }

// IdempotencyKey identifies retries of one customer deletion.
func (DeleteCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the customer by ID.
func (operation DeleteCustomerOperation) Invoke(call sdkgo.Call, input DeleteCustomerInput) sdkgo.MutationAttempt[DeletedCustomer] {
	if !validGraphQLID(input.CustomerID) {
		return waveMutationBranch[DeletedCustomer]("deleteCustomer", "defect", sdkgo.FailureValidation, "Wave customer ID is invalid")
	}
	variables := struct {
		Input struct {
			ID string `json:"id"`
		} `json:"input"`
	}{}
	variables.Input.ID = input.CustomerID
	response := operation.client.sendGraphQL(call, call.Context, deleteCustomerGraphQL, variables, false)
	if retry, hasRetry := waveMutationResponseRetry[DeletedCustomer]("deleteCustomer", response); hasRetry {
		return retry
	}
	if errors.Is(response.err, errCredential) {
		return waveMutationBranch[DeletedCustomer]("deleteCustomer", "defect", sdkgo.FailureAuthentication, "Wave credentials are invalid")
	}
	if response.status != http.StatusOK {
		return waveMutationBranch[DeletedCustomer]("deleteCustomer", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected customer deletion")
	}
	if response.err != nil {
		return waveMutationBranch[DeletedCustomer]("deleteCustomer", "invalidResponse", sdkgo.FailureResponseTooLarge, "Wave customer response exceeded the limit")
	}
	var wire struct {
		Data *struct {
			CustomerDelete *struct {
				DidSucceed  bool              `json:"didSucceed"`
				InputErrors []json.RawMessage `json:"inputErrors"`
			} `json:"customerDelete"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || wire.Data.CustomerDelete == nil || len(wire.Errors) > 0 {
		return waveMutationBranch[DeletedCustomer]("deleteCustomer", "invalidResponse", sdkgo.FailureProtocol, "Wave returned invalid customer JSON")
	}
	if !wire.Data.CustomerDelete.DidSucceed {
		return waveMutationBranch[DeletedCustomer]("deleteCustomer", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected customer deletion")
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("deleted"), DeletedCustomer{ID: input.CustomerID}, nil, sdkgo.Receipt{})
}

func validCustomerName(name string) bool {
	return name != "" && len(name) <= 255 && strings.TrimSpace(name) == name
}
func waveFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "wave", Operation: operation, Message: message}
}
func waveQueryBranch[T any](operation, branch string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := waveFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(sdkgo.BranchID(branch), *new(T), &failure, sdkgo.Receipt{})
}
func waveMutationBranch[T any](operation, branch string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := waveFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(sdkgo.BranchID(branch), *new(T), &failure, sdkgo.Receipt{})
}
func waveMutationRetry[T any](operation string, kind sdkgo.FailureKind) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](waveFailure(operation, kind, "Wave request may be retried"), 0)
}
func waveQueryRetry[T any](operation string, response providerResponse) (sdkgo.QueryAttempt[T], bool) {
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[T](waveFailure(operation, sdkgo.FailureAvailability, "Wave request failed"), response.retryAfter), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[T](waveFailure(operation, sdkgo.FailureRateLimit, "Wave rate limited the request"), response.retryAfter), true
	}
	if response.err != nil && !errors.Is(response.err, errCredential) && !errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewQueryRetry[T](waveFailure(operation, sdkgo.FailureTransport, "Wave response was interrupted"), 0), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}
func waveMutationResponseRetry[T any](operation string, response providerResponse) (sdkgo.MutationAttempt[T], bool) {
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return waveMutationRetry[T](operation, sdkgo.FailureAvailability), true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[T](waveFailure(operation, sdkgo.FailureRateLimit, "Wave rate limited the request"), response.retryAfter), true
	}
	if response.err != nil && !errors.Is(response.err, errCredential) && !errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return waveMutationRetry[T](operation, sdkgo.FailureTransport), true
	}
	return sdkgo.MutationAttempt[T]{}, false
}
func createCustomerUncertain() sdkgo.MutationAttempt[CustomerProfile] {
	failure := waveFailure("createCustomer", sdkgo.FailureTransport, "Wave customer may have been created but could not be confirmed")
	return sdkgo.NewMutationUncertain(CustomerProfile{}, failure, sdkgo.Receipt{})
}
