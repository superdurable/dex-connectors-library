// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TicketField is safe ticket-field metadata returned by Zendesk.
type TicketField struct {
	// ID is the stable Zendesk ticket-field ID.
	ID int64 `json:"id"`
	// Title is the name agents see in Zendesk.
	Title string `json:"title"`
	// Type is the Zendesk field type, such as subject or text.
	Type string `json:"type"`
	// Active reports whether the field is currently enabled.
	Active bool `json:"active"`
	// Required reports whether agents must supply this field.
	Required bool `json:"required"`
}

// ListTicketFieldsInput selects a cursor page of ticket fields.
type ListTicketFieldsInput struct {
	// PageSize is 1 through 100; zero uses 25.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor is the NextCursor from a prior page; blank begins at the first page.
	Cursor string `json:"cursor,omitempty"`
}

// ListTicketFieldsOutput contains one field page and its next cursor.
type ListTicketFieldsOutput struct {
	// Fields is the ticket-field metadata on this page.
	Fields []TicketField `json:"fields"`
	// NextCursor is blank when no later page exists.
	NextCursor string `json:"nextCursor"`
}

// GetTicketFieldInput identifies one ticket field.
type GetTicketFieldInput struct {
	// FieldID is the positive Zendesk ticket-field ID.
	FieldID int64 `json:"fieldId"`
}

// GetTicketFieldOutput contains the requested field.
type GetTicketFieldOutput struct {
	// Field is the safe ticket-field metadata.
	Field TicketField `json:"field"`
}

// ListTicketFieldsOperation reads ticket-field pages.
type ListTicketFieldsOperation struct{ client *Client }

// GetTicketFieldOperation reads one ticket field.
type GetTicketFieldOperation struct{ client *Client }

// ListTicketFields binds the ticket-field list Query to this client.
func (client *Client) ListTicketFields() ListTicketFieldsOperation {
	return ListTicketFieldsOperation{client}
}

// GetTicketField binds the ticket-field read Query to this client.
func (client *Client) GetTicketField() GetTicketFieldOperation {
	return GetTicketFieldOperation{client}
}

// Definition returns the generated ticket-field list contract.
func (ListTicketFieldsOperation) Definition() sdkgo.QueryDefinition {
	return ListTicketFieldsDefinition
}

// Definition returns the generated ticket-field read contract.
func (GetTicketFieldOperation) Definition() sdkgo.QueryDefinition { return GetTicketFieldDefinition }

// Invoke reads one bounded ticket-field page.
func (operation ListTicketFieldsOperation) Invoke(call sdkgo.Call, input ListTicketFieldsInput) sdkgo.QueryAttempt[ListTicketFieldsOutput] {
	const operationName = "listTicketFields"
	if input.PageSize < 0 || input.PageSize > 100 || len(input.Cursor) > maximumCursorLength || strings.ContainsAny(input.Cursor, "\r\n\t") {
		return sdkgo.NewQueryBranch(ListTicketFieldsBranchDefect, ListTicketFieldsOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "pageSize or cursor is invalid"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListTicketFieldsBranchDefect, ListTicketFieldsOutput{}, failure, sdkgo.Receipt{})
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 25
	}
	query := url.Values{"page[size]": {strconv.Itoa(pageSize)}}
	if input.Cursor != "" {
		query.Set("page[after]", input.Cursor)
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: "/ticket_fields", query: query})
	receipt := operation.client.receipt(call, result.response, 0)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewQueryRetry[ListTicketFieldsOutput](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(ListTicketFieldsBranchInvalidResponse, ListTicketFieldsOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(ListTicketFieldsBranchDefect, ListTicketFieldsOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(ListTicketFieldsBranchProviderRejected, ListTicketFieldsOutput{}, &result.failure, receipt)
	}
	var envelope struct {
		Fields []TicketField `json:"ticket_fields"`
		Meta   struct {
			HasMore     bool   `json:"has_more"`
			AfterCursor string `json:"after_cursor"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(result.response.body, &envelope); err != nil || envelope.Fields == nil || len(envelope.Fields) > pageSize || (envelope.Meta.HasMore && envelope.Meta.AfterCursor == "") {
		return sdkgo.NewQueryBranch(ListTicketFieldsBranchInvalidResponse, ListTicketFieldsOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned an invalid ticket-field page"), receipt)
	}
	for _, field := range envelope.Fields {
		if err := validateTicketField(field); err != nil {
			return sdkgo.NewQueryBranch(ListTicketFieldsBranchInvalidResponse, ListTicketFieldsOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned an invalid ticket field"), receipt)
		}
	}
	nextCursor := ""
	if envelope.Meta.HasMore {
		nextCursor = envelope.Meta.AfterCursor
	}
	return sdkgo.NewQueryBranch(ListTicketFieldsBranchListed, ListTicketFieldsOutput{Fields: envelope.Fields, NextCursor: nextCursor}, nil, receipt)
}

// Invoke reads one ticket field by ID.
func (operation GetTicketFieldOperation) Invoke(call sdkgo.Call, input GetTicketFieldInput) sdkgo.QueryAttempt[GetTicketFieldOutput] {
	const operationName = "getTicketField"
	if input.FieldID < 1 {
		return sdkgo.NewQueryBranch(GetTicketFieldBranchDefect, GetTicketFieldOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, operationName, "fieldId must be positive"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, operationName)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetTicketFieldBranchDefect, GetTicketFieldOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, operationName, zendeskRequest{method: http.MethodGet, path: "/ticket_fields/" + strconv.FormatInt(input.FieldID, 10)})
	receipt := operation.client.receipt(call, result.response, 0)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewQueryRetry[GetTicketFieldOutput](result.failure, result.retryAfter)
	case exchangeNotFound:
		return sdkgo.NewQueryBranch(GetTicketFieldBranchNotFound, GetTicketFieldOutput{}, &result.failure, receipt)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(GetTicketFieldBranchInvalidResponse, GetTicketFieldOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(GetTicketFieldBranchDefect, GetTicketFieldOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetTicketFieldBranchProviderRejected, GetTicketFieldOutput{}, &result.failure, receipt)
	}
	var envelope struct {
		Field *TicketField `json:"ticket_field"`
	}
	if err := json.Unmarshal(result.response.body, &envelope); err != nil || envelope.Field == nil || validateTicketField(*envelope.Field) != nil || envelope.Field.ID != input.FieldID {
		return sdkgo.NewQueryBranch(GetTicketFieldBranchInvalidResponse, GetTicketFieldOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, operationName, "Zendesk returned an invalid ticket field"), receipt)
	}
	return sdkgo.NewQueryBranch(GetTicketFieldBranchFound, GetTicketFieldOutput{Field: *envelope.Field}, nil, receipt)
}

func validateTicketField(field TicketField) error {
	if field.ID < 1 || strings.TrimSpace(field.Title) == "" || strings.TrimSpace(field.Type) == "" {
		return errors.New("ticket field has no ID, title, or type")
	}
	return nil
}
