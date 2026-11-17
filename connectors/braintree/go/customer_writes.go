// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braintree

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const createCustomerGraphQL = "mutation CreateCustomer($input: CreateCustomerInput!) { createCustomer(input: $input) { customer { id firstName lastName email } } }"
const updateCustomerGraphQL = "mutation UpdateCustomer($input: UpdateCustomerInput!) { updateCustomer(input: $input) { customer { id firstName lastName email } } }"
const deleteCustomerGraphQL = "mutation DeleteCustomer($input: DeleteCustomerInput!) { deleteCustomer(input: $input) { clientMutationId } }"

// CreateCustomerInput supplies non-secret customer contact fields.
type CreateCustomerInput struct {
	// FirstName is the optional given name.
	FirstName string `json:"firstName,omitempty"`
	// LastName is the optional family name.
	LastName string `json:"lastName,omitempty"`
	// Email is the optional contact address.
	Email string `json:"email,omitempty"`
}

// UpdateCustomerInput replaces the selected contact fields.
type UpdateCustomerInput struct {
	// CustomerID is the Braintree GraphQL customer ID.
	CustomerID string `json:"customerId"`
	// FirstName is the replacement given name; blank clears it.
	FirstName string `json:"firstName"`
	// LastName is the replacement family name; blank clears it.
	LastName string `json:"lastName"`
	// Email is the replacement contact address; blank clears it.
	Email string `json:"email"`
}

// CustomerDeleteResult confirms the selected customer deletion.
type CustomerDeleteResult struct {
	// CustomerID identifies the deleted customer.
	CustomerID string `json:"customerId"`
	// Deleted is true only on a successful mutation response.
	Deleted bool `json:"deleted"`
}

// CreateCustomerOperation creates a customer with bounded sends.
type CreateCustomerOperation struct{ client *Client }

// CreateCustomer binds the customer creation Mutation.
func (client *Client) CreateCustomer() CreateCustomerOperation {
	return CreateCustomerOperation{client}
}

// Definition returns the generated Mutation contract.
func (CreateCustomerOperation) Definition() sdkgo.MutationDefinition { return CreateCustomerDefinition }

// IdempotencyKey records the Dex call identity; Braintree does not deduplicate customer creation.
func (CreateCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one customer, with possible duplicates limited by the send cap.
func (operation CreateCustomerOperation) Invoke(call sdkgo.Call, input CreateCustomerInput) sdkgo.MutationAttempt[CustomerProfile] {
	if !validCustomerName(input.FirstName) || !validCustomerName(input.LastName) || !validCustomerEmail(input.Email) {
		return braintreeMutationBranch[CustomerProfile]("createCustomer", CreateCustomerBranchDefect, sdkgo.FailureValidation, "customer contact fields are invalid")
	}
	requestInput := map[string]any{"clientMutationId": string(call.ID), "customer": map[string]any{"firstName": input.FirstName, "lastName": input.LastName, "email": input.Email}}
	fence, err := singledispatch.NewFence(call, CreateCustomerDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return braintreeMutationBranch[CustomerProfile]("createCustomer", CreateCustomerBranchDefect, sdkgo.FailureLocalDefect, "customer dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return braintreeMutationUncertain[CustomerProfile]("createCustomer", "customer creation may have completed without a stable lookup key")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.graphQL(call, requestContext, createCustomerGraphQL, map[string]any{"input": requestInput})
		return nil
	})
	if err != nil {
		return braintreeMutationRetry[CustomerProfile]("createCustomer", sdkgo.FailureAvailability, "customer dispatch was deferred", 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return braintreeMutationRetry[CustomerProfile]("createCustomer", sdkgo.FailureAvailability, "customer resend checkpoint is unavailable", 0)
		}
	}
	return customerProfileMutationAttempt(response, "createCustomer", "createCustomer", "", CreateCustomerBranchCreated, "", CreateCustomerBranchProviderRejected, "", CreateCustomerBranchDefect, decision.MaximumPriorSends > 0)
}

// UpdateCustomerOperation replaces selected customer contact fields.
type UpdateCustomerOperation struct{ client *Client }

// UpdateCustomer binds the customer update Mutation.
func (client *Client) UpdateCustomer() UpdateCustomerOperation {
	return UpdateCustomerOperation{client}
}

// Definition returns the generated Mutation contract.
func (UpdateCustomerOperation) Definition() sdkgo.MutationDefinition { return UpdateCustomerDefinition }

// IdempotencyKey records the stable Dex call identity.
func (UpdateCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies the same absolute contact field values on every retry.
func (operation UpdateCustomerOperation) Invoke(call sdkgo.Call, input UpdateCustomerInput) sdkgo.MutationAttempt[CustomerProfile] {
	if !validGraphQLID(input.CustomerID) || !validCustomerName(input.FirstName) || !validCustomerName(input.LastName) || !validCustomerEmail(input.Email) {
		return braintreeMutationBranch[CustomerProfile]("updateCustomer", UpdateCustomerBranchDefect, sdkgo.FailureValidation, "customer contact fields are invalid")
	}
	requestInput := map[string]any{"clientMutationId": string(call.ID), "customerId": input.CustomerID, "customer": map[string]any{"firstName": input.FirstName, "lastName": input.LastName, "email": input.Email}}
	response := operation.client.graphQL(call, call.Context, updateCustomerGraphQL, map[string]any{"input": requestInput})
	return customerProfileMutationAttempt(response, "updateCustomer", "updateCustomer", input.CustomerID, UpdateCustomerBranchUpdated, UpdateCustomerBranchNotFound, UpdateCustomerBranchProviderRejected, UpdateCustomerBranchInvalidResponse, UpdateCustomerBranchDefect, false)
}

// DeleteCustomerOperation deletes a customer with no vaulted payment methods.
type DeleteCustomerOperation struct{ client *Client }

// DeleteCustomer binds the customer deletion Mutation.
func (client *Client) DeleteCustomer() DeleteCustomerOperation {
	return DeleteCustomerOperation{client}
}

// Definition returns the generated Mutation contract.
func (DeleteCustomerOperation) Definition() sdkgo.MutationDefinition { return DeleteCustomerDefinition }

// IdempotencyKey records the stable Dex call identity.
func (DeleteCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ GetCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the selected customer, treating a provider refusal as final.
func (operation DeleteCustomerOperation) Invoke(call sdkgo.Call, input GetCustomerInput) sdkgo.MutationAttempt[CustomerDeleteResult] {
	if !validGraphQLID(input.CustomerID) {
		return braintreeMutationBranch[CustomerDeleteResult]("deleteCustomer", DeleteCustomerBranchDefect, sdkgo.FailureValidation, "customerId is invalid")
	}
	requestInput := map[string]any{"clientMutationId": string(call.ID), "customerId": input.CustomerID}
	response := operation.client.graphQL(call, call.Context, deleteCustomerGraphQL, map[string]any{"input": requestInput})
	if attempt, handled := classifyBraintreeMutationResponse[CustomerDeleteResult](response, "deleteCustomer", DeleteCustomerBranchNotFound, DeleteCustomerBranchProviderRejected, DeleteCustomerBranchInvalidResponse, DeleteCustomerBranchDefect, false); handled {
		return attempt
	}
	var wire struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(response.body, &wire) != nil || len(wire.Data["deleteCustomer"]) == 0 || string(wire.Data["deleteCustomer"]) == "null" {
		return braintreeMutationBranch[CustomerDeleteResult]("deleteCustomer", DeleteCustomerBranchInvalidResponse, sdkgo.FailureProtocol, "Braintree deletion payload is missing")
	}
	return sdkgo.NewMutationBranch(DeleteCustomerBranchDeleted, CustomerDeleteResult{CustomerID: input.CustomerID, Deleted: true}, nil, sdkgo.Receipt{})
}

func customerProfileMutationAttempt(response providerResponse, operation, field, expectedID string, success, notFound, rejected, invalid, defect sdkgo.BranchID, hasPriorSend bool) sdkgo.MutationAttempt[CustomerProfile] {
	if attempt, handled := classifyBraintreeMutationResponse[CustomerProfile](response, operation, notFound, rejected, invalid, defect, hasPriorSend); handled {
		return attempt
	}
	var wire struct {
		Data map[string]struct {
			Customer CustomerProfile `json:"customer"`
		} `json:"data"`
	}
	if json.Unmarshal(response.body, &wire) != nil || !validGraphQLID(wire.Data[field].Customer.ID) || (expectedID != "" && wire.Data[field].Customer.ID != expectedID) {
		if invalid == "" {
			return braintreeMutationRetry[CustomerProfile](operation, sdkgo.FailureProtocol, "Braintree customer creation response is uncertain", 0)
		}
		return braintreeMutationBranch[CustomerProfile](operation, invalid, sdkgo.FailureProtocol, "Braintree customer payload is invalid")
	}
	return sdkgo.NewMutationBranch(success, wire.Data[field].Customer, nil, sdkgo.Receipt{})
}

func classifyBraintreeMutationResponse[T any](response providerResponse, operation string, notFound, rejected, invalid, defect sdkgo.BranchID, hasPriorSend bool) (sdkgo.MutationAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		if hasPriorSend {
			return braintreeMutationUncertain[T](operation, "an earlier customer mutation may have completed"), true
		}
		return braintreeMutationBranch[T](operation, defect, sdkgo.FailureAuthentication, "Braintree credentials are unavailable"), true
	}
	if errors.Is(response.err, errRequest) || response.err != nil && !errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return braintreeMutationRetry[T](operation, sdkgo.FailureTransport, "Braintree mutation outcome is unknown", 0), true
	}
	if response.status == http.StatusTooManyRequests {
		return braintreeMutationRetry[T](operation, sdkgo.FailureRateLimit, "Braintree rate limited the mutation", response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return braintreeMutationRetry[T](operation, sdkgo.FailureAvailability, "Braintree mutation outcome is unknown", response.retryAfter), true
	}
	if response.status != http.StatusOK {
		if hasPriorSend {
			return braintreeMutationUncertain[T](operation, "an earlier customer mutation may have completed"), true
		}
		return braintreeMutationBranch[T](operation, rejected, sdkgo.FailureProviderRejection, "Braintree rejected HTTP "+strconv.Itoa(response.status)), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		if invalid == "" {
			return braintreeMutationRetry[T](operation, sdkgo.FailureResponseTooLarge, "Braintree mutation response is uncertain", 0), true
		}
		return braintreeMutationBranch[T](operation, invalid, sdkgo.FailureResponseTooLarge, "Braintree response exceeded the configured limit"), true
	}
	var wire struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil {
		if invalid == "" {
			return braintreeMutationRetry[T](operation, sdkgo.FailureProtocol, "Braintree mutation response is uncertain", 0), true
		}
		return braintreeMutationBranch[T](operation, invalid, sdkgo.FailureProtocol, "Braintree returned invalid GraphQL JSON"), true
	}
	if len(wire.Errors) > 0 {
		switch classifyGraphQLErrors(wire.Errors) {
		case graphQLErrorRateLimited:
			return braintreeMutationRetry[T](operation, sdkgo.FailureRateLimit, "Braintree reported a resource limit", response.retryAfter), true
		case graphQLErrorUnavailable:
			return braintreeMutationRetry[T](operation, sdkgo.FailureAvailability, "Braintree reported a temporary failure", response.retryAfter), true
		case graphQLErrorNotFound:
			if hasPriorSend {
				return braintreeMutationUncertain[T](operation, "an earlier mutation may have completed"), true
			}
			if notFound != "" {
				return braintreeMutationBranch[T](operation, notFound, sdkgo.FailureNotFound, "Braintree did not find the resource"), true
			}
		}
		if hasPriorSend {
			return braintreeMutationUncertain[T](operation, "an earlier customer mutation may have completed"), true
		}
		return braintreeMutationBranch[T](operation, rejected, sdkgo.FailureProviderRejection, "Braintree rejected the mutation"), true
	}
	if len(wire.Data) == 0 || string(wire.Data) == "null" {
		if invalid == "" {
			return braintreeMutationRetry[T](operation, sdkgo.FailureProtocol, "Braintree mutation response is uncertain", 0), true
		}
		return braintreeMutationBranch[T](operation, invalid, sdkgo.FailureProtocol, "Braintree GraphQL data is missing"), true
	}
	return sdkgo.MutationAttempt[T]{}, false
}

func braintreeMutationBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "braintree", Operation: operation, Message: message}
	return sdkgo.NewMutationBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}

func braintreeMutationRetry[T any](operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: kind, Provider: "braintree", Operation: operation, Message: message}, delay)
}

func braintreeMutationUncertain[T any](operation, message string) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationUncertain(*new(T), sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "braintree", Operation: operation, Message: message}, sdkgo.Receipt{})
}

func validCustomerName(value string) bool {
	return len(value) <= 255 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n")
}

func validCustomerEmail(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 254 || strings.TrimSpace(value) != value {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value
}
