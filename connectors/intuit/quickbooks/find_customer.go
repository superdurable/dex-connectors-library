// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	findCustomerOperationID = "findCustomer"
	// MaxCustomerMatches bounds the customers one lookup reads; more matches are still ambiguous.
	MaxCustomerMatches = 10
)

// FindCustomerInput identifies a customer by exactly one of its primary email address or display name.
type FindCustomerInput struct {
	// EmailAddress is one bare address such as jane@example.com, matched against PrimaryEmailAddr.
	// QuickBooks does not require email addresses to be unique, so several customers can match.
	EmailAddress string `json:"emailAddress,omitempty"`
	// DisplayName is the customer's exact display name, which QuickBooks keeps unique.
	DisplayName string `json:"displayName,omitempty"`
	// IncludesInactive also matches customers made inactive, which QuickBooks treats as deleted;
	// false matches only active customers.
	IncludesInactive bool `json:"includesInactive,omitempty"`
}

// FindCustomerOutput is the lookup result.
type FindCustomerOutput struct {
	// EmailAddress echoes the trimmed requested address.
	EmailAddress string `json:"emailAddress,omitempty"`
	// DisplayName echoes the trimmed requested display name.
	DisplayName string `json:"displayName,omitempty"`
	// Customer is the one matching customer on the found branch, and nil otherwise.
	Customer *Customer `json:"customer,omitempty"`
	// Matches lists up to MaxCustomerMatches customers on the ambiguous branch.
	Matches []Customer `json:"matches,omitempty"`
}

// FindCustomerOperation implements the findCustomer Query.
type FindCustomerOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (FindCustomerOperation) Definition() sdkgo.QueryDefinition { return FindCustomerDefinition }

// Invoke runs one QuickBooks query on Customer and selects found, notFound, or ambiguous.
func (operation FindCustomerOperation) Invoke(call sdkgo.Call, input FindCustomerInput) sdkgo.QueryAttempt[FindCustomerOutput] {
	client := operation.client
	requested := FindCustomerOutput{EmailAddress: strings.TrimSpace(input.EmailAddress), DisplayName: strings.TrimSpace(input.DisplayName)}
	statement, err := BuildFindCustomerQuery(input)
	if err != nil {
		return sdkgo.NewQueryBranch(FindCustomerBranchDefect, requested,
			quickbooksFailurePointer(sdkgo.FailureValidation, findCustomerOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, findCustomerOperationID, quickbooksRequest{method: http.MethodGet, path: "/query", query: url.Values{"query": {statement}}})
	receipt := client.receipt(call, result, "")
	if attempt, isTerminal := queryAttemptForExchange(result, requested, receipt, queryBranches{
		providerRejected: FindCustomerBranchProviderRejected, invalidResponse: FindCustomerBranchInvalidResponse, defect: FindCustomerBranchDefect,
	}); isTerminal {
		return attempt
	}
	matches, err := decodeCustomerMatches(result.response.body, requested, input.IncludesInactive)
	if err != nil {
		return sdkgo.NewQueryBranch(FindCustomerBranchInvalidResponse, requested,
			quickbooksFailurePointer(sdkgo.FailureProtocol, findCustomerOperationID, "QuickBooks returned an invalid customer list: "+err.Error()), receipt)
	}
	switch len(matches) {
	case 0:
		return sdkgo.NewQueryBranch(FindCustomerBranchNotFound, requested,
			quickbooksFailurePointer(sdkgo.FailureNotFound, findCustomerOperationID, "no QuickBooks customer matches"), receipt)
	case 1:
		requested.Customer = &matches[0]
		receipt.ProviderObjectID = matches[0].CustomerID
		return sdkgo.NewQueryBranch(FindCustomerBranchFound, requested, nil, receipt)
	default:
		requested.Matches = matches
		return sdkgo.NewQueryBranch(FindCustomerBranchAmbiguous, requested,
			quickbooksFailurePointer(sdkgo.FailureConflict, findCustomerOperationID, "several QuickBooks customers match"), receipt)
	}
}

// BuildFindCustomerQuery returns the QuickBooks query findCustomer sends, such as
// select * from Customer where PrimaryEmailAddr = 'jane@example.com' MAXRESULTS 10, so an
// application can review it. It validates the input.
func BuildFindCustomerQuery(input FindCustomerInput) (string, error) {
	emailAddress, displayName := strings.TrimSpace(input.EmailAddress), strings.TrimSpace(input.DisplayName)
	var condition string
	switch {
	case emailAddress != "" && displayName != "":
		return "", errors.New("give either emailAddress or displayName, not both")
	case emailAddress != "":
		if err := validateEmailAddress("emailAddress", emailAddress); err != nil {
			return "", err
		}
		condition = "PrimaryEmailAddr = " + quoteQueryLiteral(emailAddress)
	case displayName != "":
		if err := validateQueryLiteral("displayName", displayName, MaxDisplayNameCharacters); err != nil {
			return "", err
		}
		condition = "DisplayName = " + quoteQueryLiteral(displayName)
	default:
		return "", errors.New("emailAddress or displayName is required")
	}
	if input.IncludesInactive {
		condition += " AND Active IN (true, false)"
	}
	return "select * from Customer where " + condition + " MAXRESULTS " + strconv.Itoa(MaxCustomerMatches), nil
}

// decodeCustomerMatches keeps only exact matches, ignoring case, because QuickBooks's comparison rules are its own.
func decodeCustomerMatches(body []byte, requested FindCustomerOutput, includesInactive bool) ([]Customer, error) {
	elements, err := decodeQueryEntities[customerWire](body, "Customer")
	if err != nil {
		return nil, err
	}
	if len(elements) > MaxCustomerMatches {
		return nil, errors.New("response has more customers than requested")
	}
	matches := []Customer{}
	for _, element := range elements {
		customer, err := decodeCustomer(element)
		if err != nil {
			return nil, err
		}
		isMatch := strings.EqualFold(customer.DisplayName, requested.DisplayName)
		if requested.EmailAddress != "" {
			isMatch = strings.EqualFold(customer.EmailAddress, requested.EmailAddress)
		}
		if isMatch && (includesInactive || customer.IsActive) {
			matches = append(matches, customer)
		}
	}
	return matches, nil
}
