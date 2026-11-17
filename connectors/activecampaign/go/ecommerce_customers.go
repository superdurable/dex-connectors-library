// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListEcommerceCustomersInput filters Deep Data customers.
type ListEcommerceCustomersInput struct {
	// ConnectionID optionally filters by numeric ecommerce connection ID.
	ConnectionID string `json:"connectionId,omitempty"`
	// ExternalID optionally filters by store customer ID.
	ExternalID string `json:"externalId,omitempty"`
	// Email optionally filters by customer email address.
	Email string `json:"email,omitempty"`
}

// GetEcommerceCustomerInput selects one customer by numeric provider ID.
type GetEcommerceCustomerInput struct {
	// CustomerID is the numeric Deep Data customer ID.
	CustomerID string `json:"customerId"`
}

// EcommerceCustomerMetadata identifies one store customer without aggregate or provider links.
type EcommerceCustomerMetadata struct {
	// ID is the numeric provider customer ID.
	ID string `json:"id"`
	// ConnectionID is the owning ecommerce connection.
	ConnectionID string `json:"connectionId"`
	// ExternalID is the store's customer identifier.
	ExternalID string `json:"externalId"`
	// Email is the customer's email address.
	Email string `json:"email"`
}

// EcommerceCustomersPage contains customers matching the requested filters.
type EcommerceCustomersPage struct {
	// Customers contains the matching customer identities.
	Customers []EcommerceCustomerMetadata `json:"customers"`
	// Total is the provider's matching customer count.
	Total int `json:"total"`
}

// ListEcommerceCustomersOperation reads ecommerce customers.
type ListEcommerceCustomersOperation struct{ client *Client }

// GetEcommerceCustomerOperation reads one ecommerce customer.
type GetEcommerceCustomerOperation struct{ client *Client }

// ListEcommerceCustomers binds the listing Query.
func (client *Client) ListEcommerceCustomers() ListEcommerceCustomersOperation {
	return ListEcommerceCustomersOperation{client}
}

// GetEcommerceCustomer binds the lookup Query.
func (client *Client) GetEcommerceCustomer() GetEcommerceCustomerOperation {
	return GetEcommerceCustomerOperation{client}
}

// Definition returns the manifest-generated listing contract.
func (ListEcommerceCustomersOperation) Definition() sdkgo.QueryDefinition {
	return ListEcommerceCustomersDefinition
}

// Definition returns the manifest-generated lookup contract.
func (GetEcommerceCustomerOperation) Definition() sdkgo.QueryDefinition {
	return GetEcommerceCustomerDefinition
}

// Invoke reads customers by connection, store ID, or email.
func (operation ListEcommerceCustomersOperation) Invoke(call sdkgo.Call, input ListEcommerceCustomersInput) sdkgo.QueryAttempt[EcommerceCustomersPage] {
	branches := queryBranches{found: ListEcommerceCustomersBranchListed, rejected: ListEcommerceCustomersBranchProviderRejected, invalid: ListEcommerceCustomersBranchInvalidResponse, defect: ListEcommerceCustomersBranchDefect}
	if input.ConnectionID != "" && !numericID.MatchString(input.ConnectionID) || len(input.ExternalID) > 250 || strings.ContainsAny(input.ExternalID, "\r\n") || input.Email != "" && !validContactEmail(input.Email) {
		return queryBranch("listEcommerceCustomers", branches.defect, EcommerceCustomersPage{}, sdkgo.FailureValidation, "connection ID, external ID, or email is invalid")
	}
	query := url.Values{}
	if input.ConnectionID != "" {
		query.Set("filters[connectionid]", input.ConnectionID)
	}
	if input.ExternalID != "" {
		query.Set("filters[externalid]", input.ExternalID)
	}
	if input.Email != "" {
		query.Set("filters[email]", input.Email)
	}
	path := "/ecomCustomers"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return invokeQuery(call, operation.client, "listEcommerceCustomers", path, branches, decodeEcommerceCustomersPage)
}

// Invoke reads one Deep Data customer by ID.
func (operation GetEcommerceCustomerOperation) Invoke(call sdkgo.Call, input GetEcommerceCustomerInput) sdkgo.QueryAttempt[EcommerceCustomerMetadata] {
	branches := queryBranches{found: GetEcommerceCustomerBranchFound, notFound: GetEcommerceCustomerBranchNotFound, rejected: GetEcommerceCustomerBranchProviderRejected, invalid: GetEcommerceCustomerBranchInvalidResponse, defect: GetEcommerceCustomerBranchDefect}
	if !numericID.MatchString(input.CustomerID) {
		return queryBranch("getEcommerceCustomer", branches.defect, EcommerceCustomerMetadata{}, sdkgo.FailureValidation, "customer ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getEcommerceCustomer", "/ecomCustomers/"+input.CustomerID, branches, decodeEcommerceCustomer)
}

type ecommerceCustomerWire struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connectionid"`
	ExternalID   string `json:"externalid"`
	Email        string `json:"email"`
}

func decodeEcommerceCustomersPage(body []byte) (EcommerceCustomersPage, error) {
	var wire struct {
		EcomCustomers []ecommerceCustomerWire `json:"ecomCustomers"`
		Meta          struct {
			Total string `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EcommerceCustomersPage{}, err
	}
	total, err := strconv.Atoi(wire.Meta.Total)
	if wire.EcomCustomers == nil || err != nil || total < 0 {
		return EcommerceCustomersPage{}, fmt.Errorf("customer collection or count missing")
	}
	page := EcommerceCustomersPage{Customers: make([]EcommerceCustomerMetadata, 0, len(wire.EcomCustomers)), Total: total}
	for _, item := range wire.EcomCustomers {
		customer, err := ecommerceCustomerMetadata(item)
		if err != nil {
			return EcommerceCustomersPage{}, err
		}
		page.Customers = append(page.Customers, customer)
	}
	return page, nil
}

func decodeEcommerceCustomer(body []byte) (EcommerceCustomerMetadata, error) {
	var wire struct {
		EcomCustomer *ecommerceCustomerWire `json:"ecomCustomer"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EcommerceCustomerMetadata{}, err
	}
	if wire.EcomCustomer == nil {
		return EcommerceCustomerMetadata{}, fmt.Errorf("customer missing")
	}
	return ecommerceCustomerMetadata(*wire.EcomCustomer)
}

func ecommerceCustomerMetadata(wire ecommerceCustomerWire) (EcommerceCustomerMetadata, error) {
	if !numericID.MatchString(wire.ID) || !numericID.MatchString(wire.ConnectionID) || wire.ExternalID == "" || !validContactEmail(wire.Email) {
		return EcommerceCustomerMetadata{}, fmt.Errorf("customer identity invalid")
	}
	return EcommerceCustomerMetadata{ID: wire.ID, ConnectionID: wire.ConnectionID, ExternalID: wire.ExternalID, Email: wire.Email}, nil
}
