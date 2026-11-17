// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var orderDecimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,10})(\.[0-9]{1,4})?$`)
var orderCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

// UpsertEcommerceOrderInput identifies a store order and its absolute commercial state.
type UpsertEcommerceOrderInput struct {
	// ConnectionID is the numeric Deep Data connection ID.
	ConnectionID string `json:"connectionId"`
	// StoreOrderID is the stable order ID in the external store.
	StoreOrderID string `json:"storeOrderId"`
	// StoreCustomerID is the stable customer ID in the external store.
	StoreCustomerID string `json:"storeCustomerId"`
	// Email is the purchasing contact's email address.
	Email string `json:"email"`
	// CreatedAt is the store creation timestamp in RFC3339 format.
	CreatedAt string `json:"createdAt"`
	// Currency is the three-letter ISO currency code.
	Currency string `json:"currency"`
	// FinalAmount is the non-negative total in major currency units, with up to four decimal places.
	FinalAmount string `json:"finalAmount"`
	// CreationSource is REAL_TIME or HISTORICAL; REAL_TIME can start order automations.
	CreationSource string `json:"creationSource"`
	// Status is the provider's normalized order status.
	Status string `json:"status"`
}

// EcommerceOrderUpsertMetadata confirms the provider's keyed order identity.
type EcommerceOrderUpsertMetadata struct {
	// ConnectionID is the provider's ecommerce connection ID.
	ConnectionID string `json:"connectionId"`
	// StoreOrderID is the stable external order ID.
	StoreOrderID string `json:"storeOrderId"`
	// Email is the contact email returned for this order.
	Email string `json:"email"`
}

// UpsertEcommerceOrderOperation writes a keyed order through GraphQL.
type UpsertEcommerceOrderOperation struct{ client *Client }

// UpsertEcommerceOrder binds the keyed order Mutation.
func (client *Client) UpsertEcommerceOrder() UpsertEcommerceOrderOperation {
	return UpsertEcommerceOrderOperation{client}
}

// Definition returns the generated upsert contract.
func (UpsertEcommerceOrderOperation) Definition() sdkgo.MutationDefinition {
	return UpsertEcommerceOrderDefinition
}

// IdempotencyKey correlates a stable keyed upsert across retries.
func (UpsertEcommerceOrderOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertEcommerceOrderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates or replaces an order at its store key.
func (operation UpsertEcommerceOrderOperation) Invoke(call sdkgo.Call, input UpsertEcommerceOrderInput) sdkgo.MutationAttempt[EcommerceOrderUpsertMetadata] {
	const name = "upsertEcommerceOrder"
	if !numericID.MatchString(input.ConnectionID) || !validOrderIdentifier(input.StoreOrderID) || !validOrderIdentifier(input.StoreCustomerID) || !validContactEmail(input.Email) || !orderCurrency.MatchString(input.Currency) || !orderDecimal.MatchString(input.FinalAmount) || !validOrderTimestamp(input.CreatedAt) || (input.CreationSource != "REAL_TIME" && input.CreationSource != "HISTORICAL") || !validNormalizedOrderStatus(input.Status) {
		return upsertOrderBranch(UpsertEcommerceOrderBranchDefect, sdkgo.FailureValidation, "order identity, amount, source, status, or timestamp is invalid")
	}
	connectionID, parseErr := strconv.ParseInt(input.ConnectionID, 10, 32)
	if parseErr != nil || connectionID < 1 {
		return upsertOrderBranch(UpsertEcommerceOrderBranchDefect, sdkgo.FailureValidation, "connection ID must be a positive 32-bit integer")
	}
	email, _ := json.Marshal(input.Email)
	storeOrderID, _ := json.Marshal(input.StoreOrderID)
	storeCustomerID, _ := json.Marshal(input.StoreCustomerID)
	createdAt, _ := json.Marshal(input.CreatedAt)
	currency, _ := json.Marshal(input.Currency)
	query := fmt.Sprintf(`mutation { upsertOrder(order: { legacyConnectionId: %d, storeOrderId: %s, storeCustomerId: %s, email: %s, storeCreatedDate: %s, currency: %s, finalAmount: %s, creationSource: %s, normalizedStatus: %s }) { connectionId storeOrderId email } }`, connectionID, storeOrderID, storeCustomerID, email, createdAt, currency, input.FinalAmount, input.CreationSource, input.Status)
	requestBody, _ := json.Marshal(map[string]string{"query": query})
	body, status, retryAfter, err := operation.client.request(call, http.MethodPost, "/ecom/graphql", requestBody)
	if errors.Is(err, errCredential) {
		return upsertOrderBranch(UpsertEcommerceOrderBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[EcommerceOrderUpsertMetadata](queryFailure(name, sdkgo.FailureTransport, "order upsert outcome unknown"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[EcommerceOrderUpsertMetadata](queryFailure(name, sdkgo.FailureRateLimit, "provider rate limited order upsert"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[EcommerceOrderUpsertMetadata](queryFailure(name, sdkgo.FailureAvailability, "order upsert outcome unknown"), retryAfter)
	}
	if status != http.StatusOK {
		return upsertOrderBranch(UpsertEcommerceOrderBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if err != nil {
		return sdkgo.NewMutationRetry[EcommerceOrderUpsertMetadata](queryFailure(name, sdkgo.FailureTransport, "order upsert response interrupted"), 0)
	}
	var response struct {
		Data struct {
			UpsertOrder *struct {
				ConnectionID json.Number `json:"connectionId"`
				StoreOrderID string      `json:"storeOrderId"`
				Email        string      `json:"email"`
			} `json:"upsertOrder"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(body, &response) != nil {
		return sdkgo.NewMutationRetry[EcommerceOrderUpsertMetadata](queryFailure(name, sdkgo.FailureProtocol, "order upsert response invalid"), 0)
	}
	if len(response.Errors) > 0 {
		return upsertOrderBranch(UpsertEcommerceOrderBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected the GraphQL order")
	}
	order := response.Data.UpsertOrder
	if order == nil || order.ConnectionID.String() != input.ConnectionID || order.StoreOrderID != input.StoreOrderID || order.Email != input.Email {
		return sdkgo.NewMutationRetry[EcommerceOrderUpsertMetadata](queryFailure(name, sdkgo.FailureProtocol, "order upsert confirmation invalid"), 0)
	}
	result := EcommerceOrderUpsertMetadata{ConnectionID: order.ConnectionID.String(), StoreOrderID: order.StoreOrderID, Email: order.Email}
	return sdkgo.NewMutationBranch(UpsertEcommerceOrderBranchUpserted, result, nil, dealReceipt(call, result.StoreOrderID))
}

func validOrderIdentifier(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 250 && !strings.ContainsAny(value, "\r\n")
}

func validOrderTimestamp(value string) bool {
	parsed, err := time.Parse(time.RFC3339, value)
	return err == nil && parsed.Year() >= 2000
}

func validNormalizedOrderStatus(value string) bool {
	switch value {
	case "PENDING", "COMPLETED", "ABANDONED", "RECOVERED", "WAITING", "CANCELLED", "REFUNDED", "FAILED", "RETURNED", "PENDING_PAYMENT":
		return true
	default:
		return false
	}
}

func upsertOrderBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[EcommerceOrderUpsertMetadata] {
	failure := queryFailure("upsertEcommerceOrder", kind, message)
	return sdkgo.NewMutationBranch(branch, EcommerceOrderUpsertMetadata{}, &failure, sdkgo.Receipt{})
}
