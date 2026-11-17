// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braintree

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const getCustomerGraphQL = "query GetCustomer($id: ID!) { node(id: $id) { __typename ... on Customer { id firstName lastName email } } }"
const getPaymentMethodGraphQL = "query GetPaymentMethod($id: ID!) { node(id: $id) { __typename ... on PaymentMethod { id usage } } }"
const getRefundGraphQL = "query GetRefund($id: ID!) { node(id: $id) { __typename ... on Refund { id status } } }"

// CustomerProfile contains a Braintree customer's contact identity.
type CustomerProfile struct {
	// ID is the GraphQL customer ID.
	ID string `json:"id"`
	// FirstName is the customer's given name, when supplied.
	FirstName string `json:"firstName"`
	// LastName is the customer's family name, when supplied.
	LastName string `json:"lastName"`
	// Email is the customer's email, when supplied.
	Email string `json:"email"`
}

// PaymentMethodProfile contains a payment method's ID and usage.
type PaymentMethodProfile struct {
	// ID is the GraphQL payment method ID.
	ID string `json:"id"`
	// Usage is SINGLE_USE or MULTI_USE when supplied.
	Usage string `json:"usage"`
}

// RefundProfile contains a refund's ID and status.
type RefundProfile struct {
	// ID is the GraphQL refund ID.
	ID string `json:"id"`
	// Status is the current PaymentStatus when supplied.
	Status string `json:"status"`
}

// GetCustomerInput identifies a customer by its GraphQL ID.
type GetCustomerInput struct {
	// CustomerID is a GraphQL ID, not a legacy SDK ID.
	CustomerID string `json:"customerId"`
}

// GetPaymentMethodInput identifies a payment method by its GraphQL ID.
type GetPaymentMethodInput struct {
	// PaymentMethodID is a GraphQL ID, not a legacy SDK ID.
	PaymentMethodID string `json:"paymentMethodId"`
}

// GetRefundInput identifies a refund by its GraphQL ID.
type GetRefundInput struct {
	// RefundID is a GraphQL ID, not a legacy SDK ID.
	RefundID string `json:"refundId"`
}

// GetCustomerOperation reads a customer node.
type GetCustomerOperation struct{ client *Client }

// GetCustomer binds the customer Query to this client.
func (client *Client) GetCustomer() GetCustomerOperation { return GetCustomerOperation{client} }

// Definition returns the manifest-generated Query worker.
func (GetCustomerOperation) Definition() sdkgo.QueryDefinition { return GetCustomerDefinition }

// Invoke reads the selected customer.
func (operation GetCustomerOperation) Invoke(call sdkgo.Call, input GetCustomerInput) sdkgo.QueryAttempt[CustomerProfile] {
	if !validGraphQLID(input.CustomerID) {
		return nodeBranch[CustomerProfile]("getCustomer", GetCustomerBranchDefect, sdkgo.FailureValidation, "customerId is invalid")
	}
	return nodeQueryAttempt[CustomerProfile](operation.client.getNode(call, getCustomerGraphQL, input.CustomerID), "getCustomer", "Customer", input.CustomerID, GetCustomerBranchFound, GetCustomerBranchNotFound, GetCustomerBranchProviderRejected, GetCustomerBranchInvalidResponse, GetCustomerBranchDefect)
}

// GetPaymentMethodOperation reads a payment method node.
type GetPaymentMethodOperation struct{ client *Client }

// GetPaymentMethod binds the payment method Query to this client.
func (client *Client) GetPaymentMethod() GetPaymentMethodOperation {
	return GetPaymentMethodOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (GetPaymentMethodOperation) Definition() sdkgo.QueryDefinition {
	return GetPaymentMethodDefinition
}

// Invoke reads the selected payment method.
func (operation GetPaymentMethodOperation) Invoke(call sdkgo.Call, input GetPaymentMethodInput) sdkgo.QueryAttempt[PaymentMethodProfile] {
	if !validGraphQLID(input.PaymentMethodID) {
		return nodeBranch[PaymentMethodProfile]("getPaymentMethod", GetPaymentMethodBranchDefect, sdkgo.FailureValidation, "paymentMethodId is invalid")
	}
	return nodeQueryAttempt[PaymentMethodProfile](operation.client.getNode(call, getPaymentMethodGraphQL, input.PaymentMethodID), "getPaymentMethod", "PaymentMethod", input.PaymentMethodID, GetPaymentMethodBranchFound, GetPaymentMethodBranchNotFound, GetPaymentMethodBranchProviderRejected, GetPaymentMethodBranchInvalidResponse, GetPaymentMethodBranchDefect)
}

// GetRefundOperation reads a refund node.
type GetRefundOperation struct{ client *Client }

// GetRefund binds the refund Query to this client.
func (client *Client) GetRefund() GetRefundOperation { return GetRefundOperation{client} }

// Definition returns the manifest-generated Query worker.
func (GetRefundOperation) Definition() sdkgo.QueryDefinition { return GetRefundDefinition }

// Invoke reads the selected refund.
func (operation GetRefundOperation) Invoke(call sdkgo.Call, input GetRefundInput) sdkgo.QueryAttempt[RefundProfile] {
	if !validGraphQLID(input.RefundID) {
		return nodeBranch[RefundProfile]("getRefund", GetRefundBranchDefect, sdkgo.FailureValidation, "refundId is invalid")
	}
	return nodeQueryAttempt[RefundProfile](operation.client.getNode(call, getRefundGraphQL, input.RefundID), "getRefund", "Refund", input.RefundID, GetRefundBranchFound, GetRefundBranchNotFound, GetRefundBranchProviderRejected, GetRefundBranchInvalidResponse, GetRefundBranchDefect)
}

func nodeQueryAttempt[T any](response providerResponse, operation, typeName, expectedID string, found, notFound, rejected, invalid, defect sdkgo.BranchID) sdkgo.QueryAttempt[T] {
	if errors.Is(response.err, errCredential) {
		return nodeBranch[T](operation, defect, sdkgo.FailureAuthentication, "Braintree credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return nodeRetry[T](operation, sdkgo.FailureTransport, "Braintree request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return nodeRetry[T](operation, sdkgo.FailureRateLimit, "Braintree rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return nodeRetry[T](operation, sdkgo.FailureAvailability, "Braintree is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return nodeBranch[T](operation, rejected, sdkgo.FailureProviderRejection, "Braintree rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return nodeBranch[T](operation, invalid, sdkgo.FailureResponseTooLarge, "Braintree response exceeded the configured limit")
	}
	if response.err != nil {
		return nodeRetry[T](operation, sdkgo.FailureTransport, "Braintree response was interrupted", 0)
	}
	var wire struct {
		Data *struct {
			Node json.RawMessage `json:"node"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil {
		return nodeBranch[T](operation, invalid, sdkgo.FailureProtocol, "Braintree returned invalid GraphQL JSON")
	}
	if len(wire.Errors) > 0 {
		switch classifyGraphQLErrors(wire.Errors) {
		case graphQLErrorNotFound:
			return nodeBranch[T](operation, notFound, sdkgo.FailureNotFound, "Braintree did not find the node")
		case graphQLErrorRateLimited:
			return nodeRetry[T](operation, sdkgo.FailureRateLimit, "Braintree reported a resource limit", response.retryAfter)
		case graphQLErrorUnavailable:
			return nodeRetry[T](operation, sdkgo.FailureAvailability, "Braintree reported a temporary GraphQL failure", response.retryAfter)
		default:
			return nodeBranch[T](operation, rejected, sdkgo.FailureProviderRejection, "Braintree rejected the GraphQL query")
		}
	}
	if wire.Data == nil {
		return nodeBranch[T](operation, invalid, sdkgo.FailureProtocol, "Braintree GraphQL data is missing")
	}
	if len(wire.Data.Node) == 0 {
		return nodeBranch[T](operation, invalid, sdkgo.FailureProtocol, "Braintree GraphQL node is missing")
	}
	if string(wire.Data.Node) == "null" {
		return nodeBranch[T](operation, notFound, sdkgo.FailureNotFound, "Braintree did not expose the node")
	}
	var identity struct {
		Typename string `json:"__typename"`
		ID       string `json:"id"`
	}
	if err := json.Unmarshal(wire.Data.Node, &identity); err != nil || identity.Typename == "" {
		return nodeBranch[T](operation, invalid, sdkgo.FailureProtocol, "Braintree node identity is invalid")
	}
	if identity.Typename != typeName {
		return nodeBranch[T](operation, notFound, sdkgo.FailureNotFound, "Braintree node has another type")
	}
	if identity.ID != expectedID {
		return nodeBranch[T](operation, invalid, sdkgo.FailureProtocol, "Braintree node ID does not match")
	}
	var profile T
	if err := json.Unmarshal(wire.Data.Node, &profile); err != nil {
		return nodeBranch[T](operation, invalid, sdkgo.FailureProtocol, "Braintree node fields are invalid")
	}
	return sdkgo.NewQueryBranch(found, profile, nil, sdkgo.Receipt{})
}

func nodeBranch[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "braintree", Operation: operation, Message: message}
	return sdkgo.NewQueryBranch(branch, *new(T), &failure, sdkgo.Receipt{})
}

func nodeRetry[T any](operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "braintree", Operation: operation, Message: message}, delay)
}
