// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	findCustomerByEmailOperation = "findCustomerByEmail"
	customerLookupPageSize       = "10"
)

// FindCustomerByEmailInput names the email address to look up.
type FindCustomerByEmailInput struct {
	// Email is one bare address, such as jane@example.com.
	Email string `json:"email"`
}

// CustomerMatches lists the customers whose primary email address is Email. Gorgias rejects a
// second customer with the same primary address, so it normally holds at most one.
type CustomerMatches struct {
	// Email is the address that was looked up.
	Email string `json:"email"`
	// Customers lists the matching customers; it is empty on the notFound branch.
	Customers []Customer `json:"customers"`
	// HasMore reports that Gorgias listed more customers for the address than Customers holds.
	HasMore bool `json:"hasMore,omitempty"`
}

// FindCustomerByEmailOperation is the findCustomerByEmail Query.
type FindCustomerByEmailOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (FindCustomerByEmailOperation) Definition() sdkgo.QueryDefinition {
	return FindCustomerByEmailDefinition
}

// Invoke reads GET /api/customers?email=..., which matches customers by primary email address.
func (operation FindCustomerByEmailOperation) Invoke(call sdkgo.Call, input FindCustomerByEmailInput) sdkgo.QueryAttempt[CustomerMatches] {
	if !isBareEmailAddress(input.Email) {
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchDefect, CustomerMatches{}, gorgiasFailurePointer(sdkgo.FailureValidation, findCustomerByEmailOperation,
			"email must be one bare email address such as jane@example.com"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, findCustomerByEmailOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchDefect, CustomerMatches{}, failure, sdkgo.Receipt{})
	}
	result := exchangeCustomerLookup(operation.client, call, credentials, findCustomerByEmailOperation, input.Email)
	receipt := operation.client.receipt(call, result.response, 0)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewQueryRetry[CustomerMatches](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchInvalidResponse, CustomerMatches{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchDefect, CustomerMatches{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchProviderRejected, CustomerMatches{}, &result.failure, receipt)
	}
	matches, err := decodeCustomerMatches(result.response.body, input.Email)
	if err != nil {
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchInvalidResponse, CustomerMatches{}, gorgiasFailurePointer(sdkgo.FailureProtocol, findCustomerByEmailOperation,
			"Gorgias returned an invalid customer page: "+err.Error()), receipt)
	}
	if len(matches.Customers) == 0 {
		return sdkgo.NewQueryBranch(FindCustomerByEmailBranchNotFound, matches, nil, receipt)
	}
	if len(matches.Customers) == 1 {
		receipt.ProviderObjectID = strconv.FormatInt(matches.Customers[0].ID, 10)
	}
	return sdkgo.NewQueryBranch(FindCustomerByEmailBranchFound, matches, nil, receipt)
}

// exchangeCustomerLookup lists the customers whose primary email address is email.
func exchangeCustomerLookup(client *Client, call sdkgo.Call, credentials Credentials, operation string, email string) gorgiasExchange {
	return client.exchange(call, credentials, operation, gorgiasRequest{
		method: http.MethodGet, path: "/customers", query: url.Values{"email": {email}, "limit": {customerLookupPageSize}},
	})
}

// decodeCustomerMatches keeps customers whose primary email equals the address, from a list envelope or a bare array.
func decodeCustomerMatches(body []byte, email string) (CustomerMatches, error) {
	var wires []gorgiasCustomerWire
	nextCursor := ""
	if trimmed := bytes.TrimSpace(body); len(trimmed) != 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &wires); err != nil {
			return CustomerMatches{}, errors.New("customer list is not a JSON array of customers")
		}
	} else {
		var err error
		if wires, nextCursor, err = decodeListBody[gorgiasCustomerWire](body, "customer"); err != nil {
			return CustomerMatches{}, err
		}
	}
	matches := CustomerMatches{Email: email, Customers: []Customer{}, HasMore: nextCursor != ""}
	for index, wire := range wires {
		customer, err := decodeCustomerWire(wire)
		if err != nil {
			return CustomerMatches{}, fmt.Errorf("customer %d: %w", index, err)
		}
		if strings.EqualFold(customer.Email, email) {
			matches.Customers = append(matches.Customers, customer)
		}
	}
	return matches, nil
}
