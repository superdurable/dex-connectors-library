// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oraclenetsuite

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var customerIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

// GetCustomerInput identifies a customer by its internal NetSuite ID.
type GetCustomerInput struct {
	// CustomerID is the non-secret numeric internal ID shown on the customer record.
	CustomerID string `json:"customerId"`
}

// CustomerSummary contains the customer's internal and entity IDs.
type CustomerSummary struct {
	// ID is the customer's internal ID.
	ID string `json:"id"`
	// EntityID is the customer ID displayed by NetSuite.
	EntityID string `json:"entityId"`
}

// GetCustomerOperation reads a customer without mutating the provider.
type GetCustomerOperation struct{ client *Client }

// GetCustomer binds the Query to this client.
func (client *Client) GetCustomer() GetCustomerOperation { return GetCustomerOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetCustomerOperation) Definition() sdkgo.QueryDefinition { return GetCustomerDefinition }

// Invoke classifies token, transport, HTTP, and customer response outcomes.
func (operation GetCustomerOperation) Invoke(call sdkgo.Call, input GetCustomerInput) sdkgo.QueryAttempt[CustomerSummary] {
	if !customerIDPattern.MatchString(input.CustomerID) {
		return customerBranch(GetCustomerBranchDefect, sdkgo.FailureValidation, "customer internal ID is invalid")
	}
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return customerBranch(GetCustomerBranchDefect, sdkgo.FailureAuthentication, "Oracle NetSuite connection needs authorization")
		}
		return customerRetry(sdkgo.FailureTransport, "Oracle NetSuite credential refresh failed", 0)
	}
	accessToken := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return customerBranch(GetCustomerBranchDefect, sdkgo.FailureAuthentication, "Oracle NetSuite access token is unavailable")
	}
	endpoint := operation.client.baseURL + "/services/rest/record/v1/customer/" + url.PathEscape(input.CustomerID)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return customerBranch(GetCustomerBranchDefect, sdkgo.FailureValidation, "customer request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return customerRetry(sdkgo.FailureTransport, "Oracle NetSuite customer request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) {
		return customerBranch(GetCustomerBranchDefect, sdkgo.FailureAuthentication, "Oracle NetSuite response contained a credential")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return customerRetry(sdkgo.FailureRateLimit, "Oracle NetSuite rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return customerRetry(sdkgo.FailureAvailability, "Oracle NetSuite is temporarily unavailable", retryAfter)
	}
	if response.StatusCode == http.StatusNotFound {
		return customerBranch(GetCustomerBranchNotFound, sdkgo.FailureNotFound, "Oracle NetSuite customer was not found")
	}
	if response.StatusCode != http.StatusOK {
		return customerBranch(GetCustomerBranchProviderRejected, sdkgo.FailureProviderRejection, "Oracle NetSuite rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
		return customerBranch(GetCustomerBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Oracle NetSuite customer response exceeded the configured limit")
	}
	if readErr != nil {
		return customerRetry(sdkgo.FailureTransport, "Oracle NetSuite customer response was interrupted", 0)
	}
	customer, err := decodeCustomer(body, input.CustomerID)
	if err != nil {
		return customerBranch(GetCustomerBranchInvalidResponse, sdkgo.FailureProtocol, "Oracle NetSuite returned invalid customer JSON")
	}
	return sdkgo.NewQueryBranch(GetCustomerBranchFound, customer, nil, sdkgo.Receipt{})
}

func decodeCustomer(body []byte, requestedID string) (CustomerSummary, error) {
	var customer CustomerSummary
	if err := json.Unmarshal(body, &customer); err != nil {
		return CustomerSummary{}, err
	}
	if customer.ID != requestedID || customer.EntityID == "" {
		return CustomerSummary{}, errors.New("customer result is incomplete or has a different internal ID")
	}
	return customer, nil
}

func customerFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "oracle-netsuite", Operation: "getCustomer", Message: message}
}

func customerBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CustomerSummary] {
	failure := customerFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, CustomerSummary{}, &failure, sdkgo.Receipt{})
}

func customerRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[CustomerSummary] {
	return sdkgo.NewQueryRetry[CustomerSummary](customerFailure(kind, message), delay)
}
