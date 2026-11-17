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

// ListEcommerceOrdersInput filters Deep Data orders.
type ListEcommerceOrdersInput struct {
	// ConnectionID optionally filters by numeric ecommerce connection ID.
	ConnectionID string `json:"connectionId,omitempty"`
	// ExternalID optionally filters by store order ID.
	ExternalID string `json:"externalId,omitempty"`
	// Email optionally filters by buyer email address.
	Email string `json:"email,omitempty"`
	// Offset skips this many orders.
	Offset int `json:"offset,omitempty"`
	// Limit caps the returned orders; zero uses the provider default, at most 100.
	Limit int `json:"limit,omitempty"`
}

// GetEcommerceOrderInput selects one order by numeric provider ID.
type GetEcommerceOrderInput struct {
	// OrderID is the numeric Deep Data order ID.
	OrderID string `json:"orderId"`
}

// EcommerceOrderMetadata contains a bounded order summary without addresses or links.
type EcommerceOrderMetadata struct {
	// ID is the numeric provider order ID.
	ID string `json:"id"`
	// ConnectionID is the owning ecommerce integration.
	ConnectionID string `json:"connectionId"`
	// ExternalID is the store order ID.
	ExternalID string `json:"externalId"`
	// CustomerID is the associated Deep Data customer ID.
	CustomerID string `json:"customerId"`
	// Email is the buyer email address.
	Email string `json:"email"`
	// Currency is the three-letter ISO currency code.
	Currency string `json:"currency"`
	// TotalMinor is the order total in minor currency units.
	TotalMinor int64 `json:"totalMinor"`
	// State is the provider order state code.
	State int `json:"state"`
}

// EcommerceOrdersPage contains orders matching the requested filters.
type EcommerceOrdersPage struct {
	// Orders contains the selected order summaries.
	Orders []EcommerceOrderMetadata `json:"orders"`
	// Total is the provider's matching order count.
	Total int `json:"total"`
}

// ListEcommerceOrdersOperation reads Deep Data orders.
type ListEcommerceOrdersOperation struct{ client *Client }

// GetEcommerceOrderOperation reads one Deep Data order.
type GetEcommerceOrderOperation struct{ client *Client }

// ListEcommerceOrders binds the listing Query.
func (client *Client) ListEcommerceOrders() ListEcommerceOrdersOperation {
	return ListEcommerceOrdersOperation{client}
}

// GetEcommerceOrder binds the lookup Query.
func (client *Client) GetEcommerceOrder() GetEcommerceOrderOperation {
	return GetEcommerceOrderOperation{client}
}

// Definition returns the manifest-generated listing contract.
func (ListEcommerceOrdersOperation) Definition() sdkgo.QueryDefinition {
	return ListEcommerceOrdersDefinition
}

// Definition returns the manifest-generated lookup contract.
func (GetEcommerceOrderOperation) Definition() sdkgo.QueryDefinition {
	return GetEcommerceOrderDefinition
}

// Invoke reads orders by connection, store order ID, or buyer email.
func (operation ListEcommerceOrdersOperation) Invoke(call sdkgo.Call, input ListEcommerceOrdersInput) sdkgo.QueryAttempt[EcommerceOrdersPage] {
	branches := queryBranches{found: ListEcommerceOrdersBranchListed, rejected: ListEcommerceOrdersBranchProviderRejected, invalid: ListEcommerceOrdersBranchInvalidResponse, defect: ListEcommerceOrdersBranchDefect}
	if input.ConnectionID != "" && !numericID.MatchString(input.ConnectionID) || len(input.ExternalID) > 250 || strings.ContainsAny(input.ExternalID, "\r\n") || input.Email != "" && !validContactEmail(input.Email) || input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return queryBranch("listEcommerceOrders", branches.defect, EcommerceOrdersPage{}, sdkgo.FailureValidation, "connection ID, external ID, or email is invalid")
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
	if input.Offset != 0 {
		query.Set("offset", strconv.Itoa(input.Offset))
	}
	if input.Limit != 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	path := "/ecomOrders"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return invokeQuery(call, operation.client, "listEcommerceOrders", path, branches, decodeEcommerceOrdersPage)
}

// Invoke reads one Deep Data order by provider ID.
func (operation GetEcommerceOrderOperation) Invoke(call sdkgo.Call, input GetEcommerceOrderInput) sdkgo.QueryAttempt[EcommerceOrderMetadata] {
	branches := queryBranches{found: GetEcommerceOrderBranchFound, notFound: GetEcommerceOrderBranchNotFound, rejected: GetEcommerceOrderBranchProviderRejected, invalid: GetEcommerceOrderBranchInvalidResponse, defect: GetEcommerceOrderBranchDefect}
	if !numericID.MatchString(input.OrderID) {
		return queryBranch("getEcommerceOrder", branches.defect, EcommerceOrderMetadata{}, sdkgo.FailureValidation, "order ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getEcommerceOrder", "/ecomOrders/"+input.OrderID, branches, decodeEcommerceOrder)
}

type ecommerceOrderWire struct {
	ID           string          `json:"id"`
	ConnectionID string          `json:"connectionid"`
	ExternalID   string          `json:"externalid"`
	CustomerID   string          `json:"customerid"`
	Email        string          `json:"email"`
	Currency     string          `json:"currency"`
	TotalPrice   json.RawMessage `json:"totalPrice"`
	State        json.RawMessage `json:"state"`
}

func decodeEcommerceOrdersPage(body []byte) (EcommerceOrdersPage, error) {
	var wire struct {
		EcomOrders []ecommerceOrderWire `json:"ecomOrders"`
		Meta       struct {
			Total string `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EcommerceOrdersPage{}, err
	}
	total, err := strconv.Atoi(wire.Meta.Total)
	if wire.EcomOrders == nil || err != nil || total < 0 {
		return EcommerceOrdersPage{}, fmt.Errorf("order collection or count missing")
	}
	page := EcommerceOrdersPage{Orders: make([]EcommerceOrderMetadata, 0, len(wire.EcomOrders)), Total: total}
	for _, item := range wire.EcomOrders {
		order, err := ecommerceOrderMetadata(item)
		if err != nil {
			return EcommerceOrdersPage{}, err
		}
		page.Orders = append(page.Orders, order)
	}
	return page, nil
}

func decodeEcommerceOrder(body []byte) (EcommerceOrderMetadata, error) {
	var wire struct {
		EcomOrder *ecommerceOrderWire `json:"ecomOrder"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EcommerceOrderMetadata{}, err
	}
	if wire.EcomOrder == nil {
		return EcommerceOrderMetadata{}, fmt.Errorf("order missing")
	}
	return ecommerceOrderMetadata(*wire.EcomOrder)
}

func ecommerceOrderMetadata(wire ecommerceOrderWire) (EcommerceOrderMetadata, error) {
	if !numericID.MatchString(wire.ID) || !numericID.MatchString(wire.ConnectionID) || wire.ExternalID == "" || !numericID.MatchString(wire.CustomerID) || !validContactEmail(wire.Email) || len(wire.Currency) != 3 {
		return EcommerceOrderMetadata{}, fmt.Errorf("order identity invalid")
	}
	total, err := parseDealInteger(wire.TotalPrice)
	if err != nil {
		return EcommerceOrderMetadata{}, fmt.Errorf("order total invalid: %w", err)
	}
	state, err := parseDealInteger(wire.State)
	if err != nil || state < 0 || state > 9 {
		return EcommerceOrderMetadata{}, fmt.Errorf("order state invalid")
	}
	return EcommerceOrderMetadata{ID: wire.ID, ConnectionID: wire.ConnectionID, ExternalID: wire.ExternalID, CustomerID: wire.CustomerID, Email: wire.Email, Currency: wire.Currency, TotalMinor: total, State: int(state)}, nil
}
