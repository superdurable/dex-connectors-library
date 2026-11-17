// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braintree

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const vaultPaymentMethodGraphQL = "mutation VaultPaymentMethod($input: VaultPaymentMethodInput!) { vaultPaymentMethod(input: $input) { paymentMethod { id usage } } }"
const updateCreditCardCardholderNameGraphQL = "mutation UpdateCreditCardCardholderName($input: UpdateCreditCardCardholderNameInput!) { updateCreditCardCardholderName(input: $input) { paymentMethod { id usage } } }"
const updateCreditCardExpirationDateGraphQL = "mutation UpdateCreditCardExpirationDate($input: UpdateCreditCardExpirationDateInput!) { updateCreditCardExpirationDate(input: $input) { paymentMethod { id usage } } }"
const updateCreditCardBillingAddressGraphQL = "mutation UpdateCreditCardBillingAddress($input: UpdateCreditCardBillingAddressInput!) { updateCreditCardBillingAddress(input: $input) { paymentMethod { id usage } } }"
const deletePaymentMethodFromVaultGraphQL = "mutation DeletePaymentMethodFromVault($input: DeletePaymentMethodFromVaultInput!) { deletePaymentMethodFromVault(input: $input) { clientMutationId } }"

// VaultPaymentMethodInput identifies one client-tokenized single-use method.
type VaultPaymentMethodInput struct {
	// PaymentMethodID is the single-use GraphQL ID, not raw card data.
	PaymentMethodID string `json:"paymentMethodId"`
	// CustomerID optionally associates the resulting multi-use method with a customer.
	CustomerID string `json:"customerId,omitempty"`
}

// UpdateCreditCardCardholderNameInput selects a vaulted card and its replacement name.
type UpdateCreditCardCardholderNameInput struct {
	// PaymentMethodID is the vaulted multi-use GraphQL ID.
	PaymentMethodID string `json:"paymentMethodId"`
	// CardholderName is the new non-blank name.
	CardholderName string `json:"cardholderName"`
}

// UpdateCreditCardExpirationDateInput selects a vaulted card and its replacement date.
type UpdateCreditCardExpirationDateInput struct {
	// PaymentMethodID is the vaulted multi-use GraphQL ID.
	PaymentMethodID string `json:"paymentMethodId"`
	// ExpirationMonth is a two-digit month.
	ExpirationMonth string `json:"expirationMonth"`
	// ExpirationYear is a four-digit year.
	ExpirationYear string `json:"expirationYear"`
}

// UpdateCreditCardBillingAddressInput selects a vaulted card and replacement billing address.
type UpdateCreditCardBillingAddressInput struct {
	// PaymentMethodID is the vaulted multi-use GraphQL ID.
	PaymentMethodID string `json:"paymentMethodId"`
	// AddressLine1 is the street address.
	AddressLine1 string `json:"addressLine1"`
	// City is the locality.
	City string `json:"city"`
	// State is the state or province.
	State string `json:"state"`
	// PostalCode is the postal or ZIP code.
	PostalCode string `json:"postalCode"`
	// CountryCode is a Braintree CountryCode value.
	CountryCode string `json:"countryCode"`
}

// PaymentMethodDeleteResult confirms a vaulted method deletion.
type PaymentMethodDeleteResult struct {
	// PaymentMethodID identifies the removed method.
	PaymentMethodID string `json:"paymentMethodId"`
	// Deleted is true only after a successful mutation response.
	Deleted bool `json:"deleted"`
}

// VaultPaymentMethodOperation applies the vaultPaymentMethod mutation with bounded sends.
type VaultPaymentMethodOperation struct{ client *Client }

// VaultPaymentMethod binds the payment method Mutation.
func (client *Client) VaultPaymentMethod() VaultPaymentMethodOperation {
	return VaultPaymentMethodOperation{client}
}

// Definition returns the generated Mutation contract.
func (VaultPaymentMethodOperation) Definition() sdkgo.MutationDefinition {
	return VaultPaymentMethodDefinition
}

// IdempotencyKey records the Dex call identity; clientMutationId does not deduplicate.
func (VaultPaymentMethodOperation) IdempotencyKey(callID sdkgo.CallID, _ VaultPaymentMethodInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies vaultPaymentMethod and limits duplicate verification or vaulting.
func (operation VaultPaymentMethodOperation) Invoke(call sdkgo.Call, input VaultPaymentMethodInput) sdkgo.MutationAttempt[PaymentMethodProfile] {
	if !(validGraphQLID(input.PaymentMethodID) && (input.CustomerID == "" || validGraphQLID(input.CustomerID))) {
		return braintreeMutationBranch[PaymentMethodProfile]("vaultPaymentMethod", VaultPaymentMethodBranchDefect, sdkgo.FailureValidation, "payment method input is invalid")
	}
	requestInput := map[string]any{"paymentMethodId": input.PaymentMethodID, "customerId": input.CustomerID}
	if input.CustomerID == "" {
		delete(requestInput, "customerId")
	}
	requestInput["clientMutationId"] = string(call.ID)
	return operation.client.invokeLimitedPaymentMethod(call, VaultPaymentMethodDefinition, "vaultPaymentMethod", vaultPaymentMethodGraphQL, requestInput, VaultPaymentMethodBranchVaulted, VaultPaymentMethodBranchNotFound, VaultPaymentMethodBranchProviderRejected, VaultPaymentMethodBranchDefect, "")
}

// UpdateCreditCardCardholderNameOperation applies the updateCreditCardCardholderName mutation with bounded sends.
type UpdateCreditCardCardholderNameOperation struct{ client *Client }

// UpdateCreditCardCardholderName binds the payment method Mutation.
func (client *Client) UpdateCreditCardCardholderName() UpdateCreditCardCardholderNameOperation {
	return UpdateCreditCardCardholderNameOperation{client}
}

// Definition returns the generated Mutation contract.
func (UpdateCreditCardCardholderNameOperation) Definition() sdkgo.MutationDefinition {
	return UpdateCreditCardCardholderNameDefinition
}

// IdempotencyKey records the Dex call identity; clientMutationId does not deduplicate.
func (UpdateCreditCardCardholderNameOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCreditCardCardholderNameInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies updateCreditCardCardholderName and limits duplicate verification or vaulting.
func (operation UpdateCreditCardCardholderNameOperation) Invoke(call sdkgo.Call, input UpdateCreditCardCardholderNameInput) sdkgo.MutationAttempt[PaymentMethodProfile] {
	if !(validGraphQLID(input.PaymentMethodID) && validCustomerName(input.CardholderName) && input.CardholderName != "") {
		return braintreeMutationBranch[PaymentMethodProfile]("updateCreditCardCardholderName", UpdateCreditCardCardholderNameBranchDefect, sdkgo.FailureValidation, "payment method input is invalid")
	}
	requestInput := map[string]any{"paymentMethodId": input.PaymentMethodID, "cardholderName": input.CardholderName}
	requestInput["clientMutationId"] = string(call.ID)
	return operation.client.invokeLimitedPaymentMethod(call, UpdateCreditCardCardholderNameDefinition, "updateCreditCardCardholderName", updateCreditCardCardholderNameGraphQL, requestInput, UpdateCreditCardCardholderNameBranchUpdated, UpdateCreditCardCardholderNameBranchNotFound, UpdateCreditCardCardholderNameBranchProviderRejected, UpdateCreditCardCardholderNameBranchDefect, input.PaymentMethodID)
}

// UpdateCreditCardExpirationDateOperation applies the updateCreditCardExpirationDate mutation with bounded sends.
type UpdateCreditCardExpirationDateOperation struct{ client *Client }

// UpdateCreditCardExpirationDate binds the payment method Mutation.
func (client *Client) UpdateCreditCardExpirationDate() UpdateCreditCardExpirationDateOperation {
	return UpdateCreditCardExpirationDateOperation{client}
}

// Definition returns the generated Mutation contract.
func (UpdateCreditCardExpirationDateOperation) Definition() sdkgo.MutationDefinition {
	return UpdateCreditCardExpirationDateDefinition
}

// IdempotencyKey records the Dex call identity; clientMutationId does not deduplicate.
func (UpdateCreditCardExpirationDateOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCreditCardExpirationDateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies updateCreditCardExpirationDate and limits duplicate verification or vaulting.
func (operation UpdateCreditCardExpirationDateOperation) Invoke(call sdkgo.Call, input UpdateCreditCardExpirationDateInput) sdkgo.MutationAttempt[PaymentMethodProfile] {
	if !(validGraphQLID(input.PaymentMethodID) && validExpirationDate(input.ExpirationMonth, input.ExpirationYear)) {
		return braintreeMutationBranch[PaymentMethodProfile]("updateCreditCardExpirationDate", UpdateCreditCardExpirationDateBranchDefect, sdkgo.FailureValidation, "payment method input is invalid")
	}
	requestInput := map[string]any{"paymentMethodId": input.PaymentMethodID, "expirationMonth": input.ExpirationMonth, "expirationYear": input.ExpirationYear}
	requestInput["clientMutationId"] = string(call.ID)
	return operation.client.invokeLimitedPaymentMethod(call, UpdateCreditCardExpirationDateDefinition, "updateCreditCardExpirationDate", updateCreditCardExpirationDateGraphQL, requestInput, UpdateCreditCardExpirationDateBranchUpdated, UpdateCreditCardExpirationDateBranchNotFound, UpdateCreditCardExpirationDateBranchProviderRejected, UpdateCreditCardExpirationDateBranchDefect, input.PaymentMethodID)
}

// UpdateCreditCardBillingAddressOperation applies the updateCreditCardBillingAddress mutation with bounded sends.
type UpdateCreditCardBillingAddressOperation struct{ client *Client }

// UpdateCreditCardBillingAddress binds the payment method Mutation.
func (client *Client) UpdateCreditCardBillingAddress() UpdateCreditCardBillingAddressOperation {
	return UpdateCreditCardBillingAddressOperation{client}
}

// Definition returns the generated Mutation contract.
func (UpdateCreditCardBillingAddressOperation) Definition() sdkgo.MutationDefinition {
	return UpdateCreditCardBillingAddressDefinition
}

// IdempotencyKey records the Dex call identity; clientMutationId does not deduplicate.
func (UpdateCreditCardBillingAddressOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCreditCardBillingAddressInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies updateCreditCardBillingAddress and limits duplicate verification or vaulting.
func (operation UpdateCreditCardBillingAddressOperation) Invoke(call sdkgo.Call, input UpdateCreditCardBillingAddressInput) sdkgo.MutationAttempt[PaymentMethodProfile] {
	if !(validBillingAddress(input)) {
		return braintreeMutationBranch[PaymentMethodProfile]("updateCreditCardBillingAddress", UpdateCreditCardBillingAddressBranchDefect, sdkgo.FailureValidation, "payment method input is invalid")
	}
	requestInput := map[string]any{"paymentMethodId": input.PaymentMethodID, "billingAddress": map[string]any{"addressLine1": input.AddressLine1, "adminArea2": input.City, "adminArea1": input.State, "postalCode": input.PostalCode, "countryCode": input.CountryCode}}
	requestInput["clientMutationId"] = string(call.ID)
	return operation.client.invokeLimitedPaymentMethod(call, UpdateCreditCardBillingAddressDefinition, "updateCreditCardBillingAddress", updateCreditCardBillingAddressGraphQL, requestInput, UpdateCreditCardBillingAddressBranchUpdated, UpdateCreditCardBillingAddressBranchNotFound, UpdateCreditCardBillingAddressBranchProviderRejected, UpdateCreditCardBillingAddressBranchDefect, input.PaymentMethodID)
}

// DeletePaymentMethodFromVaultOperation removes a multi-use payment method.
type DeletePaymentMethodFromVaultOperation struct{ client *Client }

// DeletePaymentMethodFromVault binds the vault deletion Mutation.
func (client *Client) DeletePaymentMethodFromVault() DeletePaymentMethodFromVaultOperation {
	return DeletePaymentMethodFromVaultOperation{client}
}

// Definition returns the generated Mutation contract.
func (DeletePaymentMethodFromVaultOperation) Definition() sdkgo.MutationDefinition {
	return DeletePaymentMethodFromVaultDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (DeletePaymentMethodFromVaultOperation) IdempotencyKey(callID sdkgo.CallID, _ GetPaymentMethodInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the selected vaulted payment method.
func (operation DeletePaymentMethodFromVaultOperation) Invoke(call sdkgo.Call, input GetPaymentMethodInput) sdkgo.MutationAttempt[PaymentMethodDeleteResult] {
	if !validGraphQLID(input.PaymentMethodID) {
		return braintreeMutationBranch[PaymentMethodDeleteResult]("deletePaymentMethodFromVault", DeletePaymentMethodFromVaultBranchDefect, sdkgo.FailureValidation, "paymentMethodId is invalid")
	}
	requestInput := map[string]any{"clientMutationId": string(call.ID), "paymentMethodId": input.PaymentMethodID}
	response := operation.client.graphQL(call, call.Context, deletePaymentMethodFromVaultGraphQL, map[string]any{"input": requestInput})
	if attempt, handled := classifyBraintreeMutationResponse[PaymentMethodDeleteResult](response, "deletePaymentMethodFromVault", DeletePaymentMethodFromVaultBranchNotFound, DeletePaymentMethodFromVaultBranchProviderRejected, DeletePaymentMethodFromVaultBranchInvalidResponse, DeletePaymentMethodFromVaultBranchDefect, false); handled {
		return attempt
	}
	var wire struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(response.body, &wire) != nil || len(wire.Data["deletePaymentMethodFromVault"]) == 0 || string(wire.Data["deletePaymentMethodFromVault"]) == "null" {
		return braintreeMutationBranch[PaymentMethodDeleteResult]("deletePaymentMethodFromVault", DeletePaymentMethodFromVaultBranchInvalidResponse, sdkgo.FailureProtocol, "Braintree vault deletion payload is missing")
	}
	return sdkgo.NewMutationBranch(DeletePaymentMethodFromVaultBranchDeleted, PaymentMethodDeleteResult{PaymentMethodID: input.PaymentMethodID, Deleted: true}, nil, sdkgo.Receipt{})
}

func (client *Client) invokeLimitedPaymentMethod(call sdkgo.Call, definition sdkgo.MutationDefinition, operation, graphQL string, requestInput map[string]any, success, notFound, rejected, defect sdkgo.BranchID, expectedID string) sdkgo.MutationAttempt[PaymentMethodProfile] {
	fence, err := singledispatch.NewFence(call, definition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return braintreeMutationBranch[PaymentMethodProfile](operation, defect, sdkgo.FailureLocalDefect, "payment method dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return braintreeMutationUncertain[PaymentMethodProfile](operation, "payment method outcome cannot be confirmed")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = client.graphQL(call, requestContext, graphQL, map[string]any{"input": requestInput})
		return nil
	})
	if err != nil {
		return braintreeMutationRetry[PaymentMethodProfile](operation, sdkgo.FailureAvailability, "payment method dispatch was deferred", 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return braintreeMutationRetry[PaymentMethodProfile](operation, sdkgo.FailureAvailability, "payment method resend checkpoint is unavailable", 0)
		}
	}
	if attempt, handled := classifyBraintreeMutationResponse[PaymentMethodProfile](response, operation, notFound, rejected, "", defect, decision.MaximumPriorSends > 0); handled {
		return attempt
	}
	var wire struct {
		Data map[string]struct {
			PaymentMethod PaymentMethodProfile `json:"paymentMethod"`
		} `json:"data"`
	}
	if json.Unmarshal(response.body, &wire) != nil || !validGraphQLID(wire.Data[operation].PaymentMethod.ID) || wire.Data[operation].PaymentMethod.Usage != "MULTI_USE" || (expectedID != "" && wire.Data[operation].PaymentMethod.ID != expectedID) {
		return braintreeMutationRetry[PaymentMethodProfile](operation, sdkgo.FailureProtocol, "Braintree payment method response is uncertain", 0)
	}
	return sdkgo.NewMutationBranch(success, wire.Data[operation].PaymentMethod, nil, sdkgo.Receipt{})
}

func validExpirationDate(month, year string) bool {
	if len(month) != 2 || len(year) != 4 {
		return false
	}
	m, err := strconv.Atoi(month)
	if err != nil || m < 1 || m > 12 {
		return false
	}
	y, err := strconv.Atoi(year)
	return err == nil && y >= 2000 && y <= 9999
}

func validBillingAddress(input UpdateCreditCardBillingAddressInput) bool {
	return validGraphQLID(input.PaymentMethodID) && strings.TrimSpace(input.AddressLine1) != "" && len(input.AddressLine1) <= 255 && strings.TrimSpace(input.City) != "" && len(input.City) <= 255 && len(input.State) <= 255 && strings.TrimSpace(input.PostalCode) != "" && len(input.PostalCode) <= 9 && len(input.CountryCode) == 2
}
