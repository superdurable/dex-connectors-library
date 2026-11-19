// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventbrite

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetTicketClassInput identifies one ticket class within an event.
type GetTicketClassInput struct {
	EventID       string `json:"eventId"`
	TicketClassID string `json:"ticketClassId"`
}

// ListTicketClassesInput selects a one-based page of an event's ticket classes.
type ListTicketClassesInput struct {
	EventID string `json:"eventId"`
	Page    int    `json:"page,omitempty"`
}

// UpdateTicketClassInput replaces a ticket class name with plain text.
type UpdateTicketClassInput struct {
	EventID       string `json:"eventId"`
	TicketClassID string `json:"ticketClassId"`
	Name          string `json:"name"`
}

// TicketClassMetadata contains a ticket class's public identity and capacity.
type TicketClassMetadata struct {
	ID            string `json:"id"`
	EventID       string `json:"eventId"`
	Name          string `json:"name"`
	QuantityTotal int    `json:"quantityTotal"`
}

// TicketClassesPage contains one page of ticket classes.
type TicketClassesPage struct {
	TicketClasses []TicketClassMetadata `json:"ticketClasses"`
	PageNumber    int                   `json:"pageNumber"`
	PageCount     int                   `json:"pageCount"`
}

// GetTicketClassOperation reads a ticket class.
type GetTicketClassOperation struct{ client *Client }

// ListTicketClassesOperation reads a ticket class page.
type ListTicketClassesOperation struct{ client *Client }

// UpdateTicketClassOperation replaces a ticket class name.
type UpdateTicketClassOperation struct{ client *Client }

// GetTicketClass binds the query to this client.
func (client *Client) GetTicketClass() GetTicketClassOperation {
	return GetTicketClassOperation{client}
}

// ListTicketClasses binds the query to this client.
func (client *Client) ListTicketClasses() ListTicketClassesOperation {
	return ListTicketClassesOperation{client}
}

// UpdateTicketClass binds the mutation to this client.
func (client *Client) UpdateTicketClass() UpdateTicketClassOperation {
	return UpdateTicketClassOperation{client}
}

// Definition returns the manifest-generated operation contract.
func (GetTicketClassOperation) Definition() sdkgo.QueryDefinition { return GetTicketClassDefinition }

// Definition returns the manifest-generated operation contract.
func (ListTicketClassesOperation) Definition() sdkgo.QueryDefinition {
	return ListTicketClassesDefinition
}

// Definition returns the manifest-generated operation contract.
func (UpdateTicketClassOperation) Definition() sdkgo.MutationDefinition {
	return UpdateTicketClassDefinition
}

// IdempotencyKey correlates a Dex call; replacement also makes repeated sends harmless.
func (UpdateTicketClassOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTicketClassInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke retrieves a ticket class without buyer data.
func (operation GetTicketClassOperation) Invoke(call sdkgo.Call, input GetTicketClassInput) sdkgo.QueryAttempt[TicketClassMetadata] {
	branches := queryBranches{found: GetTicketClassBranchFound, notFound: GetTicketClassBranchNotFound, rejected: GetTicketClassBranchProviderRejected, invalid: GetTicketClassBranchInvalidResponse, defect: GetTicketClassBranchDefect}
	if !numericID.MatchString(input.EventID) || !numericID.MatchString(input.TicketClassID) {
		return queryBranch("getTicketClass", branches.defect, TicketClassMetadata{}, sdkgo.FailureValidation, "event ID or ticket class ID is invalid")
	}
	return invokeQuery(call, operation.client, "getTicketClass", ticketClassPath(input.EventID, input.TicketClassID), branches, decodeTicketClass)
}

// Invoke retrieves one ticket class page.
func (operation ListTicketClassesOperation) Invoke(call sdkgo.Call, input ListTicketClassesInput) sdkgo.QueryAttempt[TicketClassesPage] {
	branches := queryBranches{found: ListTicketClassesBranchListed, notFound: ListTicketClassesBranchNotFound, rejected: ListTicketClassesBranchProviderRejected, invalid: ListTicketClassesBranchInvalidResponse, defect: ListTicketClassesBranchDefect}
	page, valid := pageNumber(input.Page)
	if !numericID.MatchString(input.EventID) || !valid {
		return queryBranch("listTicketClasses", branches.defect, TicketClassesPage{}, sdkgo.FailureValidation, "event ID or page is invalid")
	}
	return invokeQuery(call, operation.client, "listTicketClasses", "/events/"+input.EventID+"/ticket_classes/?page="+strconv.Itoa(page), branches, decodeTicketClassesPage)
}

// Invoke replaces a name and reads it back before selecting success.
func (operation UpdateTicketClassOperation) Invoke(call sdkgo.Call, input UpdateTicketClassInput) sdkgo.MutationAttempt[TicketClassMetadata] {
	if !numericID.MatchString(input.EventID) || !numericID.MatchString(input.TicketClassID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 {
		return ticketChangeBranch(UpdateTicketClassBranchDefect, sdkgo.FailureValidation, "event ID, ticket class ID, or name is invalid")
	}
	path := ticketClassPath(input.EventID, input.TicketClassID)
	body, status, delay, err := operation.client.requestJSON(call, http.MethodPost, path, map[string]any{"ticket_class": map[string]string{"name": input.Name}})
	if attempt := classifyTicketChange(status, delay, err); attempt != nil {
		return *attempt
	}
	updated, err := decodeTicketClass(body)
	if err != nil || updated.ID != input.TicketClassID {
		return ticketChangeRetry(sdkgo.FailureProtocol, "Eventbrite ticket class update answer is unconfirmed", 0)
	}
	confirmedBody, status, delay, err := operation.client.get(call, path)
	if attempt := classifyTicketChange(status, delay, err); attempt != nil {
		return *attempt
	}
	confirmed, err := decodeTicketClass(confirmedBody)
	if err != nil || confirmed.ID != input.TicketClassID || confirmed.Name != input.Name {
		return ticketChangeRetry(sdkgo.FailureProtocol, "Eventbrite ticket class name is not yet confirmed", 0)
	}
	return sdkgo.NewMutationBranch(UpdateTicketClassBranchUpdated, confirmed, nil, eventCreationReceipt(call, confirmed.ID))
}

func ticketClassPath(eventID, ticketClassID string) string {
	return "/events/" + eventID + "/ticket_classes/" + ticketClassID + "/"
}

func decodeTicketClass(body []byte) (TicketClassMetadata, error) {
	var wire struct {
		ID            string `json:"id"`
		EventID       string `json:"event_id"`
		Name          string `json:"name"`
		QuantityTotal int    `json:"quantity_total"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TicketClassMetadata{}, err
	}
	if !numericID.MatchString(wire.ID) || !numericID.MatchString(wire.EventID) || wire.Name == "" || wire.QuantityTotal < 0 {
		return TicketClassMetadata{}, fmt.Errorf("ticket class identity or quantity missing")
	}
	return TicketClassMetadata{ID: wire.ID, EventID: wire.EventID, Name: wire.Name, QuantityTotal: wire.QuantityTotal}, nil
}

func decodeTicketClassesPage(body []byte) (TicketClassesPage, error) {
	var wire struct {
		TicketClasses []json.RawMessage `json:"ticket_classes"`
		Pagination    pagination        `json:"pagination"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TicketClassesPage{}, err
	}
	if wire.TicketClasses == nil || !wire.Pagination.valid() {
		return TicketClassesPage{}, fmt.Errorf("ticket class page missing")
	}
	page := TicketClassesPage{TicketClasses: make([]TicketClassMetadata, 0, len(wire.TicketClasses)), PageNumber: wire.Pagination.PageNumber, PageCount: wire.Pagination.PageCount}
	for _, raw := range wire.TicketClasses {
		item, err := decodeTicketClass(raw)
		if err != nil {
			return TicketClassesPage{}, err
		}
		page.TicketClasses = append(page.TicketClasses, item)
	}
	return page, nil
}

func classifyTicketChange(status int, delay time.Duration, err error) *sdkgo.MutationAttempt[TicketClassMetadata] {
	var result sdkgo.MutationAttempt[TicketClassMetadata]
	switch {
	case errors.Is(err, errCredential):
		result = ticketChangeBranch(UpdateTicketClassBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	case status == http.StatusTooManyRequests:
		result = ticketChangeRetry(sdkgo.FailureRateLimit, "Eventbrite rate limited the ticket class request", delay)
	case status == http.StatusRequestTimeout || status >= 500 || errors.Is(err, errRequest):
		result = ticketChangeRetry(sdkgo.FailureAvailability, "Eventbrite ticket class outcome is unconfirmed", delay)
	case status == http.StatusNotFound:
		result = ticketChangeBranch(UpdateTicketClassBranchNotFound, sdkgo.FailureNotFound, "Eventbrite did not find the ticket class")
	case status != http.StatusOK:
		result = ticketChangeBranch(UpdateTicketClassBranchProviderRejected, sdkgo.FailureProviderRejection, "Eventbrite rejected HTTP "+strconv.Itoa(status))
	case errors.Is(err, providerhttp.ErrBodyTooLarge):
		result = ticketChangeRetry(sdkgo.FailureResponseTooLarge, "Eventbrite ticket class response exceeded the limit", 0)
	case err != nil:
		result = ticketChangeRetry(sdkgo.FailureTransport, "Eventbrite ticket class response was interrupted", 0)
	default:
		return nil
	}
	return &result
}

func ticketChangeBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[TicketClassMetadata] {
	failure := queryFailure("updateTicketClass", kind, message)
	return sdkgo.NewMutationBranch(branch, TicketClassMetadata{}, &failure, sdkgo.Receipt{})
}

func ticketChangeRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[TicketClassMetadata] {
	return sdkgo.NewMutationRetry[TicketClassMetadata](queryFailure("updateTicketClass", kind, message), delay)
}
