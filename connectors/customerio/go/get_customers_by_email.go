// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package customerio looks up Customer.io profiles in Dex Flows.
package customerio

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetCustomersByEmailInput identifies an exact non-secret customer email address.
type GetCustomersByEmailInput struct {
	// Email is the complete email address shown on the Customer.io People page.
	Email string `json:"email"`
}

// CustomerLookupPage contains Customer.io profile IDs matching an email.
type CustomerLookupPage struct {
	// Email is the address supplied to Customer.io.
	Email string `json:"email"`
	// CustomerIDs are the matching cio_id values in provider order.
	CustomerIDs []string `json:"customerIDs"`
}

// GetCustomersByEmailOperation reads customer IDs through the App API.
type GetCustomersByEmailOperation struct{ client *Client }

// GetCustomersByEmail binds the Query to this client.
func (client *Client) GetCustomersByEmail() GetCustomersByEmailOperation {
	return GetCustomersByEmailOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (GetCustomersByEmailOperation) Definition() sdkgo.QueryDefinition {
	return GetCustomersByEmailDefinition
}

// Invoke performs one bounded lookup and classifies retryable and terminal outcomes.
func (operation GetCustomersByEmailOperation) Invoke(call sdkgo.Call, input GetCustomersByEmailInput) sdkgo.QueryAttempt[CustomerLookupPage] {
	if !validEmail(input.Email) {
		return customersBranch(GetCustomersByEmailBranchDefect, sdkgo.FailureValidation, "email must be one valid address of at most 320 bytes")
	}
	response := operation.client.get(call, input.Email)
	if errors.Is(response.err, errCredential) {
		return customersBranch(GetCustomersByEmailBranchDefect, sdkgo.FailureAuthentication, "Customer.io credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[CustomerLookupPage](customersFailure(sdkgo.FailureTransport, "Customer.io request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[CustomerLookupPage](customersFailure(sdkgo.FailureRateLimit, "Customer.io rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[CustomerLookupPage](customersFailure(sdkgo.FailureAvailability, "Customer.io is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return customersBranch(GetCustomersByEmailBranchProviderRejected, sdkgo.FailureProviderRejection, "Customer.io rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return customersBranch(GetCustomersByEmailBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Customer.io response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[CustomerLookupPage](customersFailure(sdkgo.FailureTransport, "Customer.io response was interrupted"), 0)
	}
	page, err := decodeCustomers(response.body, input.Email)
	if err != nil {
		return customersBranch(GetCustomersByEmailBranchInvalidResponse, sdkgo.FailureProtocol, "Customer.io returned invalid customer-lookup JSON")
	}
	return sdkgo.NewQueryBranch(GetCustomersByEmailBranchFound, page, nil, sdkgo.Receipt{})
}

func validEmail(email string) bool {
	if len(email) == 0 || len(email) > 320 || !utf8.ValidString(email) || strings.TrimSpace(email) != email {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && !strings.ContainsAny(email, "\r\n\t")
}

func decodeCustomers(body []byte, email string) (CustomerLookupPage, error) {
	var wire struct {
		Results []struct {
			CustomerID string `json:"cio_id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CustomerLookupPage{}, err
	}
	if wire.Results == nil {
		return CustomerLookupPage{}, errors.New("customer results are missing")
	}
	page := CustomerLookupPage{Email: email, CustomerIDs: make([]string, 0, len(wire.Results))}
	for _, result := range wire.Results {
		if result.CustomerID == "" {
			return CustomerLookupPage{}, errors.New("customer ID is missing")
		}
		page.CustomerIDs = append(page.CustomerIDs, result.CustomerID)
	}
	return page, nil
}

func customersFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "customerio", Operation: "getCustomersByEmail", Message: message}
}

func customersBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CustomerLookupPage] {
	failure := customersFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, CustomerLookupPage{}, &failure, sdkgo.Receipt{})
}
