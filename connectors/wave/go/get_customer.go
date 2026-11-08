// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wave

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetCustomerInput selects a customer inside one Wave business.
type GetCustomerInput struct {
	// BusinessID is the exact GraphQL ID of the Wave business.
	BusinessID string `json:"businessId"`
	// CustomerID is the exact GraphQL ID of the customer.
	CustomerID string `json:"customerId"`
}

// CustomerProfile contains the selected customer ID and name.
type CustomerProfile struct {
	// BusinessID is the business used for the query.
	BusinessID string `json:"businessId"`
	// ID is the customer GraphQL ID returned by Wave.
	ID string `json:"id"`
	// Name is the customer's display name.
	Name string `json:"name"`
}

// GetCustomerOperation reads one customer through Wave GraphQL.
type GetCustomerOperation struct{ client *Client }

// GetCustomer binds the Query to this client.
func (client *Client) GetCustomer() GetCustomerOperation { return GetCustomerOperation{client} }

// Definition returns the manifest-generated Query worker.
func (GetCustomerOperation) Definition() sdkgo.QueryDefinition { return GetCustomerDefinition }

// Invoke sends a fixed read-only GraphQL query and classifies the response.
func (operation GetCustomerOperation) Invoke(call sdkgo.Call, input GetCustomerInput) sdkgo.QueryAttempt[CustomerProfile] {
	if !validGraphQLID(input.BusinessID) || !validGraphQLID(input.CustomerID) {
		return customerBranch(GetCustomerBranchDefect, sdkgo.FailureValidation, "businessId and customerId must each contain 1–128 printable characters")
	}
	response := operation.client.get(call, input)
	if errors.Is(response.err, errCredential) {
		return customerBranch(GetCustomerBranchDefect, sdkgo.FailureAuthentication, "Wave credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[CustomerProfile](customerFailure(sdkgo.FailureTransport, "Wave request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[CustomerProfile](customerFailure(sdkgo.FailureRateLimit, "Wave rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[CustomerProfile](customerFailure(sdkgo.FailureAvailability, "Wave is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return customerBranch(GetCustomerBranchProviderRejected, sdkgo.FailureProviderRejection, "Wave rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return customerBranch(GetCustomerBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Wave response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[CustomerProfile](customerFailure(sdkgo.FailureTransport, "Wave response was interrupted"), 0)
	}
	profile, isNotFound, err := decodeCustomer(response.body, input)
	if err != nil {
		return customerBranch(GetCustomerBranchInvalidResponse, sdkgo.FailureProtocol, "Wave returned invalid customer JSON")
	}
	if isNotFound {
		return customerBranch(GetCustomerBranchNotFound, sdkgo.FailureNotFound, "Wave did not expose the customer")
	}
	return sdkgo.NewQueryBranch(GetCustomerBranchFound, profile, nil, sdkgo.Receipt{})
}

func validGraphQLID(value string) bool {
	if len(value) == 0 || len(value) > 128 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func decodeCustomer(body []byte, input GetCustomerInput) (CustomerProfile, bool, error) {
	var wire struct {
		Data *struct {
			Business *struct {
				Customer *struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"customer"`
			} `json:"business"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CustomerProfile{}, false, err
	}
	if len(wire.Errors) > 0 {
		return CustomerProfile{}, false, errors.New("GraphQL errors were returned")
	}
	if wire.Data == nil {
		return CustomerProfile{}, false, errors.New("GraphQL data is missing")
	}
	if wire.Data.Business == nil || wire.Data.Business.Customer == nil {
		return CustomerProfile{}, true, nil
	}
	customer := wire.Data.Business.Customer
	if customer.ID != input.CustomerID || customer.Name == "" {
		return CustomerProfile{}, false, errors.New("customer identity or name is invalid")
	}
	return CustomerProfile{BusinessID: input.BusinessID, ID: customer.ID, Name: customer.Name}, false, nil
}

func customerFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "wave", Operation: "getCustomer", Message: message}
}

func customerBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CustomerProfile] {
	failure := customerFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, CustomerProfile{}, &failure, sdkgo.Receipt{})
}
