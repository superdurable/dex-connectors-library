// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventbrite

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListAttendeesByEventInput selects a one-based attendee page for one event.
type ListAttendeesByEventInput struct {
	EventID string `json:"eventId"`
	Page    int    `json:"page,omitempty"`
}

// ListAttendeesByOrganizationInput selects a one-based attendee page across an organization.
type ListAttendeesByOrganizationInput struct {
	OrganizationID string `json:"organizationId"`
	Page           int    `json:"page,omitempty"`
}

// AttendeesPage contains attendee identities without profiles, answers, barcodes, or email.
type AttendeesPage struct {
	Attendees  []AttendeeMetadata `json:"attendees"`
	PageNumber int                `json:"pageNumber"`
	PageCount  int                `json:"pageCount"`
}

// GetOrderInput selects one order by numeric Eventbrite ID.
type GetOrderInput struct {
	OrderID string `json:"orderId"`
}

// OrderMetadata excludes the purchaser's name, email, payment details, and answers.
type OrderMetadata struct {
	ID        string `json:"id"`
	EventID   string `json:"eventId"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

// ListOrdersByEventInput selects a one-based order page for one event.
type ListOrdersByEventInput struct {
	EventID string `json:"eventId"`
	Page    int    `json:"page,omitempty"`
}

// ListOrdersByOrganizationInput selects a one-based order page across an organization.
type ListOrdersByOrganizationInput struct {
	OrganizationID string `json:"organizationId"`
	Page           int    `json:"page,omitempty"`
}

// OrdersPage contains order identities without purchaser or payment details.
type OrdersPage struct {
	Orders     []OrderMetadata `json:"orders"`
	PageNumber int             `json:"pageNumber"`
	PageCount  int             `json:"pageCount"`
}

// ListAttendeesByEventOperation reads one event's attendee page.
type ListAttendeesByEventOperation struct{ client *Client }

// ListAttendeesByOrganizationOperation reads an organization's attendee page.
type ListAttendeesByOrganizationOperation struct{ client *Client }

// GetOrderOperation reads one order's metadata.
type GetOrderOperation struct{ client *Client }

// ListOrdersByEventOperation reads one event's order page.
type ListOrdersByEventOperation struct{ client *Client }

// ListOrdersByOrganizationOperation reads an organization's order page.
type ListOrdersByOrganizationOperation struct{ client *Client }

// ListAttendeesByEvent binds the query to this client.
func (client *Client) ListAttendeesByEvent() ListAttendeesByEventOperation {
	return ListAttendeesByEventOperation{client}
}

// ListAttendeesByOrganization binds the query to this client.
func (client *Client) ListAttendeesByOrganization() ListAttendeesByOrganizationOperation {
	return ListAttendeesByOrganizationOperation{client}
}

// GetOrder binds the query to this client.
func (client *Client) GetOrder() GetOrderOperation { return GetOrderOperation{client} }

// ListOrdersByEvent binds the query to this client.
func (client *Client) ListOrdersByEvent() ListOrdersByEventOperation {
	return ListOrdersByEventOperation{client}
}

// ListOrdersByOrganization binds the query to this client.
func (client *Client) ListOrdersByOrganization() ListOrdersByOrganizationOperation {
	return ListOrdersByOrganizationOperation{client}
}

// Definition returns the manifest-generated operation contract.
func (ListAttendeesByEventOperation) Definition() sdkgo.QueryDefinition {
	return ListAttendeesByEventDefinition
}

// Definition returns the manifest-generated operation contract.
func (ListAttendeesByOrganizationOperation) Definition() sdkgo.QueryDefinition {
	return ListAttendeesByOrganizationDefinition
}

// Definition returns the manifest-generated operation contract.
func (GetOrderOperation) Definition() sdkgo.QueryDefinition { return GetOrderDefinition }

// Definition returns the manifest-generated operation contract.
func (ListOrdersByEventOperation) Definition() sdkgo.QueryDefinition {
	return ListOrdersByEventDefinition
}

// Definition returns the manifest-generated operation contract.
func (ListOrdersByOrganizationOperation) Definition() sdkgo.QueryDefinition {
	return ListOrdersByOrganizationDefinition
}

// Invoke reads a page of attendees in one event.
func (operation ListAttendeesByEventOperation) Invoke(call sdkgo.Call, input ListAttendeesByEventInput) sdkgo.QueryAttempt[AttendeesPage] {
	branches := queryBranches{found: ListAttendeesByEventBranchListed, notFound: ListAttendeesByEventBranchNotFound, rejected: ListAttendeesByEventBranchProviderRejected, invalid: ListAttendeesByEventBranchInvalidResponse, defect: ListAttendeesByEventBranchDefect}
	page, valid := pageNumber(input.Page)
	if !numericID.MatchString(input.EventID) || !valid {
		return queryBranch("listAttendeesByEvent", branches.defect, AttendeesPage{}, sdkgo.FailureValidation, "event ID or page is invalid")
	}
	return invokeQuery(call, operation.client, "listAttendeesByEvent", "/events/"+input.EventID+"/attendees/?page="+strconv.Itoa(page), branches, decodeAttendeesPage)
}

// Invoke reads a page of attendees across an organization.
func (operation ListAttendeesByOrganizationOperation) Invoke(call sdkgo.Call, input ListAttendeesByOrganizationInput) sdkgo.QueryAttempt[AttendeesPage] {
	branches := queryBranches{found: ListAttendeesByOrganizationBranchListed, notFound: ListAttendeesByOrganizationBranchNotFound, rejected: ListAttendeesByOrganizationBranchProviderRejected, invalid: ListAttendeesByOrganizationBranchInvalidResponse, defect: ListAttendeesByOrganizationBranchDefect}
	page, valid := pageNumber(input.Page)
	if !numericID.MatchString(input.OrganizationID) || !valid {
		return queryBranch("listAttendeesByOrganization", branches.defect, AttendeesPage{}, sdkgo.FailureValidation, "organization ID or page is invalid")
	}
	return invokeQuery(call, operation.client, "listAttendeesByOrganization", "/organizations/"+input.OrganizationID+"/attendees/?page="+strconv.Itoa(page), branches, decodeAttendeesPage)
}

// Invoke reads one order without purchaser or payment data.
func (operation GetOrderOperation) Invoke(call sdkgo.Call, input GetOrderInput) sdkgo.QueryAttempt[OrderMetadata] {
	branches := queryBranches{found: GetOrderBranchFound, notFound: GetOrderBranchNotFound, rejected: GetOrderBranchProviderRejected, invalid: GetOrderBranchInvalidResponse, defect: GetOrderBranchDefect}
	if !numericID.MatchString(input.OrderID) {
		return queryBranch("getOrder", branches.defect, OrderMetadata{}, sdkgo.FailureValidation, "order ID is invalid")
	}
	return invokeQuery(call, operation.client, "getOrder", "/orders/"+input.OrderID+"/", branches, decodeOrder)
}

// Invoke reads a page of orders in one event.
func (operation ListOrdersByEventOperation) Invoke(call sdkgo.Call, input ListOrdersByEventInput) sdkgo.QueryAttempt[OrdersPage] {
	branches := queryBranches{found: ListOrdersByEventBranchListed, notFound: ListOrdersByEventBranchNotFound, rejected: ListOrdersByEventBranchProviderRejected, invalid: ListOrdersByEventBranchInvalidResponse, defect: ListOrdersByEventBranchDefect}
	page, valid := pageNumber(input.Page)
	if !numericID.MatchString(input.EventID) || !valid {
		return queryBranch("listOrdersByEvent", branches.defect, OrdersPage{}, sdkgo.FailureValidation, "event ID or page is invalid")
	}
	return invokeQuery(call, operation.client, "listOrdersByEvent", "/events/"+input.EventID+"/orders/?page="+strconv.Itoa(page), branches, decodeOrdersPage)
}

// Invoke reads a page of orders across an organization.
func (operation ListOrdersByOrganizationOperation) Invoke(call sdkgo.Call, input ListOrdersByOrganizationInput) sdkgo.QueryAttempt[OrdersPage] {
	branches := queryBranches{found: ListOrdersByOrganizationBranchListed, notFound: ListOrdersByOrganizationBranchNotFound, rejected: ListOrdersByOrganizationBranchProviderRejected, invalid: ListOrdersByOrganizationBranchInvalidResponse, defect: ListOrdersByOrganizationBranchDefect}
	page, valid := pageNumber(input.Page)
	if !numericID.MatchString(input.OrganizationID) || !valid {
		return queryBranch("listOrdersByOrganization", branches.defect, OrdersPage{}, sdkgo.FailureValidation, "organization ID or page is invalid")
	}
	return invokeQuery(call, operation.client, "listOrdersByOrganization", "/organizations/"+input.OrganizationID+"/orders/?page="+strconv.Itoa(page), branches, decodeOrdersPage)
}

func decodeAttendeesPage(body []byte) (AttendeesPage, error) {
	var wire struct {
		Attendees  []json.RawMessage `json:"attendees"`
		Pagination pagination        `json:"pagination"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return AttendeesPage{}, err
	}
	if wire.Attendees == nil || !wire.Pagination.valid() {
		return AttendeesPage{}, fmt.Errorf("attendee page missing")
	}
	page := AttendeesPage{Attendees: make([]AttendeeMetadata, 0, len(wire.Attendees)), PageNumber: wire.Pagination.PageNumber, PageCount: wire.Pagination.PageCount}
	for _, raw := range wire.Attendees {
		attendee, err := decodeAttendee(raw)
		if err != nil {
			return AttendeesPage{}, err
		}
		page.Attendees = append(page.Attendees, attendee)
	}
	return page, nil
}

func decodeOrder(body []byte) (OrderMetadata, error) {
	var wire struct {
		ID        string `json:"id"`
		EventID   string `json:"event_id"`
		Status    string `json:"status"`
		CreatedAt string `json:"created"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return OrderMetadata{}, err
	}
	if !numericID.MatchString(wire.ID) || !numericID.MatchString(wire.EventID) || wire.Status == "" {
		return OrderMetadata{}, fmt.Errorf("order identity or status missing")
	}
	return OrderMetadata{ID: wire.ID, EventID: wire.EventID, Status: wire.Status, CreatedAt: wire.CreatedAt}, nil
}

func decodeOrdersPage(body []byte) (OrdersPage, error) {
	var wire struct {
		Orders     []json.RawMessage `json:"orders"`
		Pagination pagination        `json:"pagination"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return OrdersPage{}, err
	}
	if wire.Orders == nil || !wire.Pagination.valid() {
		return OrdersPage{}, fmt.Errorf("order page missing")
	}
	page := OrdersPage{Orders: make([]OrderMetadata, 0, len(wire.Orders)), PageNumber: wire.Pagination.PageNumber, PageCount: wire.Pagination.PageCount}
	for _, raw := range wire.Orders {
		order, err := decodeOrder(raw)
		if err != nil {
			return OrdersPage{}, err
		}
		page.Orders = append(page.Orders, order)
	}
	return page, nil
}
