// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	findCustomerByEmailOperationID = "findCustomerByEmail"
	customerLookupFailureSubject   = "customer lookup"
	// MaxCustomerMatches is the size of the one customer page the lookup reads.
	MaxCustomerMatches = 50
)

// FindCustomerByEmailInput names a service desk and the email address to look up among its customers.
type FindCustomerByEmailInput struct {
	// ServiceDeskID is the numeric service desk ID, as the service desk picker stores it.
	ServiceDeskID string `json:"serviceDeskId"`
	// Email is one bare address, such as jane@example.com, compared without case.
	Email string `json:"email"`
}

// CustomerMatches lists the service desk's customers whose email address equals the looked-up address.
// Atlassian can hold more than one account for one address, so the Flow decides which one it means.
type CustomerMatches struct {
	// Email is the address that was looked up.
	Email string `json:"email"`
	// Customers lists the matching customers in the provider's order; it is empty on notFound.
	Customers []Customer `json:"customers"`
	// HasMore reports that the provider's text match listed more than MaxCustomerMatches customers, so a
	// match beyond the first page was not compared.
	HasMore bool `json:"hasMore,omitempty"`
}

// Customer is one Jira Service Management customer without its email address.
type Customer struct {
	// AccountID is the customer's Atlassian account ID, which createTicket accepts as RaiseOnBehalfOfAccountID.
	AccountID string `json:"accountId"`
	// DisplayName is the customer's display name, which privacy settings may replace.
	DisplayName string `json:"displayName,omitempty"`
	// IsActive reports an active account; Atlassian omits the flag for some accounts, which reads as true.
	IsActive bool `json:"isActive"`
}

// FindCustomerByEmailOperation implements the findCustomerByEmail Query.
type FindCustomerByEmailOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (FindCustomerByEmailOperation) Definition() sdkgo.QueryDefinition {
	return FindCustomerByEmailDefinition
}

// Invoke reads the first page of the service desk's customers that match the address as text, with
// the experimental-API opt-in header the endpoint requires, and keeps only exact address matches.
func (operation FindCustomerByEmailOperation) Invoke(call sdkgo.Call, input FindCustomerByEmailInput) sdkgo.QueryAttempt[CustomerMatches] {
	client := operation.client
	serviceDeskID, err := validateNumericID(input.ServiceDeskID, "serviceDeskId")
	if err != nil {
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchDefect, CustomerMatches{}, failurePointer(findCustomerByEmailOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	email, err := validateEmailAddress(input.Email)
	if err != nil {
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchDefect, CustomerMatches{}, failurePointer(findCustomerByEmailOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	output := CustomerMatches{Email: email, Customers: []Customer{}}
	session, cancel, sessionErr := client.startSession(call, findCustomerByEmailOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewQueryRetry[CustomerMatches](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchDefect, output, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, providerRequest{
		method: http.MethodGet, api: serviceDeskAPI, path: "/servicedesk/" + url.PathEscape(serviceDeskID) + "/customer",
		query:          url.Values{"query": {email}, "start": {"0"}, "limit": {strconv.Itoa(MaxCustomerMatches)}},
		isExperimental: true,
	})
	classification := client.classifyRead(findCustomerByEmailOperationID, customerLookupFailureSubject, result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case readSucceeded:
	case readRetry:
		return sdkgo.NewQueryRetry[CustomerMatches](classification.failure, classification.retryAfter)
	case readDefect:
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchDefect, output, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchInvalidResponse, output, &classification.failure, receipt)
	case readNotFound:
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchProviderRejected, output, failurePointer(findCustomerByEmailOperationID, sdkgo.FailureNotFound,
			"Jira Service Management has no service desk "+serviceDeskID+" visible to the connection"), receipt)
	default:
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchProviderRejected, output, &classification.failure, receipt)
	}
	page, err := decodeServiceDeskPage[serviceDeskUser](result.response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchInvalidResponse, output, failurePointer(findCustomerByEmailOperationID, sdkgo.FailureProtocol,
			"Jira Service Management returned an invalid customer page: "+err.Error()), receipt)
	}
	output.HasMore = !page.IsLastPage
	for _, user := range page.Values {
		if !strings.EqualFold(strings.TrimSpace(user.EmailAddress), email) || !accountIDPattern.MatchString(user.AccountID) {
			continue
		}
		output.Customers = append(output.Customers, Customer{AccountID: user.AccountID, DisplayName: user.DisplayName, IsActive: user.Active == nil || *user.Active})
	}
	if len(output.Customers) == 0 {
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchNotFound, output, failurePointer(findCustomerByEmailOperationID, sdkgo.FailureNotFound,
			"no customer of the service desk has this email address"), receipt)
	}
	receipt.ProviderObjectID = output.Customers[0].AccountID
	return sdkgo.NewQueryBranch(FindCustomerByEmailBranchFound, output, nil, receipt)
}
