// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ads

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListAccessibleCustomersInput bounds how many account names enter Flow state.
type ListAccessibleCustomersInput struct {
	// MaxCustomers accepts 1 through 10000 names; zero uses 1000.
	MaxCustomers int `json:"maxCustomers,omitempty"`
}

// AccessibleCustomers contains the customer resource names returned by Google Ads.
type AccessibleCustomers struct {
	// ResourceNames contain IDs in the form customers/{customer-id}.
	ResourceNames []string `json:"resourceNames"`
}

// ListAccessibleCustomersOperation reads accounts directly accessible to the Google identity.
type ListAccessibleCustomersOperation struct{ client *Client }

// ListAccessibleCustomers binds the customer Query to this client.
func (client *Client) ListAccessibleCustomers() ListAccessibleCustomersOperation {
	return ListAccessibleCustomersOperation{client}
}

// Definition returns the manifest-generated customer listing contract.
func (ListAccessibleCustomersOperation) Definition() sdkgo.QueryDefinition {
	return ListAccessibleCustomersDefinition
}

// Invoke retrieves and validates a bounded list of directly accessible customer names.
func (operation ListAccessibleCustomersOperation) Invoke(call sdkgo.Call, input ListAccessibleCustomersInput) sdkgo.QueryAttempt[AccessibleCustomers] {
	if input.MaxCustomers < 0 || input.MaxCustomers > 10000 {
		return customerBranch(ListAccessibleCustomersBranchDefect, sdkgo.FailureValidation, "maximum customer count is invalid")
	}
	if input.MaxCustomers == 0 {
		input.MaxCustomers = 1000
	}
	response := operation.client.get(call, "/v26/customers:listAccessibleCustomers")
	if errors.Is(response.err, errCredential) {
		return customerBranch(ListAccessibleCustomersBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return customerRetry(sdkgo.FailureTransport, "Google Ads request failed", 0)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return customerBranch(ListAccessibleCustomersBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Google Ads response exceeded the configured limit")
	}
	if response.err != nil {
		return customerRetry(sdkgo.FailureTransport, "Google Ads response was interrupted", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return customerRetry(sdkgo.FailureRateLimit, "Google Ads rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return customerRetry(sdkgo.FailureAvailability, "Google Ads is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return customerBranch(ListAccessibleCustomersBranchProviderRejected, sdkgo.FailureProviderRejection, "Google Ads rejected HTTP "+strconv.Itoa(response.status))
	}
	customers, err := decodeAccessibleCustomers(response.body, input.MaxCustomers)
	if err != nil {
		return customerBranch(ListAccessibleCustomersBranchInvalidResponse, sdkgo.FailureProtocol, "Google Ads returned invalid customer JSON")
	}
	return sdkgo.NewQueryBranch(ListAccessibleCustomersBranchListed, customers, nil, sdkgo.Receipt{})
}

var customerNamePattern = regexp.MustCompile(`^customers/[0-9]{1,20}$`)

func decodeAccessibleCustomers(body []byte, maxCustomers int) (AccessibleCustomers, error) {
	var payload struct {
		ResourceNames []string `json:"resourceNames"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return AccessibleCustomers{}, err
	}
	if len(payload.ResourceNames) > maxCustomers {
		return AccessibleCustomers{}, errors.New("Google Ads customer list exceeds the requested limit")
	}
	result := AccessibleCustomers{ResourceNames: make([]string, 0, len(payload.ResourceNames))}
	seen := make(map[string]struct{}, len(payload.ResourceNames))
	for _, name := range payload.ResourceNames {
		if !customerNamePattern.MatchString(name) {
			return AccessibleCustomers{}, errors.New("Google Ads customer name is invalid")
		}
		if _, exists := seen[name]; exists {
			return AccessibleCustomers{}, errors.New("Google Ads returned duplicate customer names")
		}
		seen[name] = struct{}{}
		result.ResourceNames = append(result.ResourceNames, name)
	}
	return result, nil
}

func customerBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[AccessibleCustomers] {
	failure := customerFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, AccessibleCustomers{}, &failure, sdkgo.Receipt{})
}

func customerRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[AccessibleCustomers] {
	return sdkgo.NewQueryRetry[AccessibleCustomers](customerFailure(kind, message), delay)
}

func customerFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "google-ads", Operation: "listAccessibleCustomers", Message: message}
}
