// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package square

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Invoice preserves a Square invoice, including the payment schedule and recipient.
type Invoice struct {
	// Invoice is the complete provider invoice object returned by Square.
	Invoice map[string]any `json:"invoice"`
}

// InvoicePage is one page of Square invoices.
type InvoicePage struct {
	// Invoices contains the returned provider invoice objects.
	Invoices []map[string]any `json:"invoices"`
	// Cursor continues the list when set.
	Cursor string `json:"cursor"`
}

// DeletedInvoice confirms deletion of a draft invoice.
type DeletedInvoice struct {
	// InvoiceID is the deleted Square invoice ID.
	InvoiceID string `json:"invoiceId"`
}

// ListInvoicesInput selects one invoice page for a seller location.
type ListInvoicesInput struct {
	// LocationID is the Square location ID.
	LocationID string `json:"locationId"`
	// Cursor continues a previous page; blank starts with the first page.
	Cursor string `json:"cursor,omitempty"`
}

// GetInvoiceInput identifies one Square invoice.
type GetInvoiceInput struct {
	// InvoiceID is the Square invoice ID.
	InvoiceID string `json:"invoiceId"`
}

// CreateInvoiceInput creates a single-balance draft invoice delivered by email.
type CreateInvoiceInput struct {
	// OrderID is an open Square order not already associated with an invoice.
	OrderID string `json:"orderId"`
	// LocationID is the order's Square location ID.
	LocationID string `json:"locationId"`
	// CustomerID is a Square customer with an email address.
	CustomerID string `json:"customerId"`
	// DueDate is the balance due date in YYYY-MM-DD format.
	DueDate string `json:"dueDate"`
}

// UpdateInvoiceInput changes an invoice description with optimistic version control.
type UpdateInvoiceInput struct {
	// InvoiceID is the Square invoice ID.
	InvoiceID string `json:"invoiceId"`
	// Version is the current Square invoice version, including zero for a new draft.
	Version int32 `json:"version"`
	// Description replaces the invoice description; blank clears it.
	Description string `json:"description"`
}

// PublishInvoiceInput publishes a draft invoice at its current version.
type PublishInvoiceInput struct {
	// InvoiceID is the Square invoice ID.
	InvoiceID string `json:"invoiceId"`
	// Version is the current Square invoice version.
	Version int32 `json:"version"`
}

// CancelInvoiceInput cancels a published invoice at its current version.
type CancelInvoiceInput struct {
	// InvoiceID is the Square invoice ID.
	InvoiceID string `json:"invoiceId"`
	// Version is the current Square invoice version.
	Version int32 `json:"version"`
}

// DeleteInvoiceInput deletes a draft invoice at its current version.
type DeleteInvoiceInput struct {
	// InvoiceID is the Square invoice ID.
	InvoiceID string `json:"invoiceId"`
	// Version is the current Square invoice version.
	Version int32 `json:"version"`
}

// ListInvoicesOperation lists invoices at a seller location.
type ListInvoicesOperation struct{ client *Client }

// GetInvoiceOperation reads one invoice.
type GetInvoiceOperation struct{ client *Client }

// CreateInvoiceOperation creates a draft invoice.
type CreateInvoiceOperation struct{ client *Client }

// UpdateInvoiceOperation updates an invoice description.
type UpdateInvoiceOperation struct{ client *Client }

// PublishInvoiceOperation publishes a draft invoice.
type PublishInvoiceOperation struct{ client *Client }

// CancelInvoiceOperation cancels a published invoice.
type CancelInvoiceOperation struct{ client *Client }

// DeleteInvoiceOperation deletes a draft invoice.
type DeleteInvoiceOperation struct{ client *Client }

// ListInvoices binds the listInvoices Query to this client.
func (client *Client) ListInvoices() ListInvoicesOperation { return ListInvoicesOperation{client} }

// GetInvoice binds the getInvoice Query to this client.
func (client *Client) GetInvoice() GetInvoiceOperation { return GetInvoiceOperation{client} }

// CreateInvoice binds the createInvoice Mutation to this client.
func (client *Client) CreateInvoice() CreateInvoiceOperation { return CreateInvoiceOperation{client} }

// UpdateInvoice binds the updateInvoice Mutation to this client.
func (client *Client) UpdateInvoice() UpdateInvoiceOperation { return UpdateInvoiceOperation{client} }

// PublishInvoice binds the publishInvoice Mutation to this client.
func (client *Client) PublishInvoice() PublishInvoiceOperation {
	return PublishInvoiceOperation{client}
}

// CancelInvoice binds the cancelInvoice Mutation to this client.
func (client *Client) CancelInvoice() CancelInvoiceOperation { return CancelInvoiceOperation{client} }

// DeleteInvoice binds the deleteInvoice Mutation to this client.
func (client *Client) DeleteInvoice() DeleteInvoiceOperation { return DeleteInvoiceOperation{client} }

// Definition returns the manifest-generated contract.
func (ListInvoicesOperation) Definition() sdkgo.QueryDefinition { return ListInvoicesDefinition }

// Definition returns the manifest-generated contract.
func (GetInvoiceOperation) Definition() sdkgo.QueryDefinition { return GetInvoiceDefinition }

// Definition returns the manifest-generated contract.
func (CreateInvoiceOperation) Definition() sdkgo.MutationDefinition { return CreateInvoiceDefinition }

// Definition returns the manifest-generated contract.
func (UpdateInvoiceOperation) Definition() sdkgo.MutationDefinition { return UpdateInvoiceDefinition }

// Definition returns the manifest-generated contract.
func (PublishInvoiceOperation) Definition() sdkgo.MutationDefinition { return PublishInvoiceDefinition }

// Definition returns the manifest-generated contract.
func (CancelInvoiceOperation) Definition() sdkgo.MutationDefinition { return CancelInvoiceDefinition }

// Definition returns the manifest-generated contract.
func (DeleteInvoiceOperation) Definition() sdkgo.MutationDefinition { return DeleteInvoiceDefinition }

// IdempotencyKey derives the stable Square key for draft creation.
func (CreateInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateInvoiceInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// IdempotencyKey derives the stable Square key for the sparse update.
func (UpdateInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateInvoiceInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// IdempotencyKey derives the stable Square key for publishing.
func (PublishInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ PublishInvoiceInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// IdempotencyKey identifies a repeated cancellation request.
func (CancelInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelInvoiceInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// IdempotencyKey identifies a repeated draft deletion request.
func (DeleteInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteInvoiceInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// Invoke lists one page of invoices for the requested location.
func (operation ListInvoicesOperation) Invoke(call sdkgo.Call, input ListInvoicesInput) sdkgo.QueryAttempt[InvoicePage] {
	if !validPathSegment(input.LocationID) {
		return failedAttempt[InvoicePage]("listInvoices", ListInvoicesBranchDefect, sdkgo.FailureValidation, "locationId is required")
	}
	query := url.Values{"location_id": {input.LocationID}}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	return executeRead(operation.client, call, "listInvoices", "/invoices", query, ListInvoicesBranchListed, ListInvoicesBranchProviderRejected, ListInvoicesBranchProviderRejected, ListInvoicesBranchInvalidResponse, ListInvoicesBranchDefect, decodeInvoicePage)
}

// Invoke retrieves one complete invoice by ID.
func (operation GetInvoiceOperation) Invoke(call sdkgo.Call, input GetInvoiceInput) sdkgo.QueryAttempt[Invoice] {
	if !validPathSegment(input.InvoiceID) {
		return failedAttempt[Invoice]("getInvoice", GetInvoiceBranchDefect, sdkgo.FailureValidation, "invoiceId is required")
	}
	return executeRead(operation.client, call, "getInvoice", "/invoices/"+url.PathEscape(input.InvoiceID), url.Values{}, GetInvoiceBranchFound, GetInvoiceBranchNotFound, GetInvoiceBranchProviderRejected, GetInvoiceBranchInvalidResponse, GetInvoiceBranchDefect, decodeInvoice)
}

func decodeInvoicePage(body []byte) (InvoicePage, error) {
	var result InvoicePage
	if err := json.Unmarshal(body, &result); err != nil {
		return InvoicePage{}, err
	}
	if result.Invoices == nil {
		result.Invoices = []map[string]any{}
	}
	return result, nil
}

func decodeInvoice(body []byte) (Invoice, error) {
	var result Invoice
	if err := json.Unmarshal(body, &result); err != nil {
		return Invoice{}, err
	}
	if result.Invoice == nil || result.Invoice["id"] == nil {
		return Invoice{}, errors.New("missing invoice")
	}
	return result, nil
}

// Invoke creates one email-delivered balance invoice without a card-on-file charge.
func (operation CreateInvoiceOperation) Invoke(call sdkgo.Call, input CreateInvoiceInput) sdkgo.MutationAttempt[Invoice] {
	if !validPathSegment(input.OrderID) || !validPathSegment(input.LocationID) || !validPathSegment(input.CustomerID) || !validInvoiceDate(input.DueDate) {
		return invoiceMutationDefect("createInvoice", CreateInvoiceBranchDefect)
	}
	invoice := map[string]any{"order_id": input.OrderID, "location_id": input.LocationID, "primary_recipient": map[string]any{"customer_id": input.CustomerID}, "payment_requests": []any{map[string]any{"request_type": "BALANCE", "due_date": input.DueDate}}, "delivery_method": "EMAIL", "accepted_payment_methods": map[string]any{"card": true}}
	return executeInvoiceMutation(operation.client, call, invoiceWriteRequest{"createInvoice", http.MethodPost, "/invoices", map[string]any{"idempotency_key": call.IdempotencyKey, "invoice": invoice}, CreateInvoiceBranchCreated, CreateInvoiceBranchProviderRejected, CreateInvoiceBranchInvalidResponse, CreateInvoiceBranchDefect})
}

func validInvoiceDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

// Invoke changes only the invoice description at the supplied current version.
func (operation UpdateInvoiceOperation) Invoke(call sdkgo.Call, input UpdateInvoiceInput) sdkgo.MutationAttempt[Invoice] {
	if !validPathSegment(input.InvoiceID) || input.Version < 0 {
		return invoiceMutationDefect("updateInvoice", UpdateInvoiceBranchDefect)
	}
	invoice := map[string]any{"version": input.Version, "description": input.Description}
	return executeInvoiceMutation(operation.client, call, invoiceWriteRequest{"updateInvoice", http.MethodPut, "/invoices/" + url.PathEscape(input.InvoiceID), map[string]any{"idempotency_key": call.IdempotencyKey, "invoice": invoice}, UpdateInvoiceBranchUpdated, UpdateInvoiceBranchProviderRejected, UpdateInvoiceBranchInvalidResponse, UpdateInvoiceBranchDefect})
}

// Invoke publishes a draft invoice using its current version and a stable provider key.
func (operation PublishInvoiceOperation) Invoke(call sdkgo.Call, input PublishInvoiceInput) sdkgo.MutationAttempt[Invoice] {
	if !validPathSegment(input.InvoiceID) || input.Version < 0 {
		return invoiceMutationDefect("publishInvoice", PublishInvoiceBranchDefect)
	}
	return executeInvoiceMutation(operation.client, call, invoiceWriteRequest{"publishInvoice", http.MethodPost, "/invoices/" + url.PathEscape(input.InvoiceID) + "/publish", map[string]any{"idempotency_key": call.IdempotencyKey, "version": input.Version}, PublishInvoiceBranchPublished, PublishInvoiceBranchProviderRejected, PublishInvoiceBranchInvalidResponse, PublishInvoiceBranchDefect})
}

// Invoke cancels a published invoice at its current version.
func (operation CancelInvoiceOperation) Invoke(call sdkgo.Call, input CancelInvoiceInput) sdkgo.MutationAttempt[Invoice] {
	if !validPathSegment(input.InvoiceID) || input.Version < 0 {
		return invoiceMutationDefect("cancelInvoice", CancelInvoiceBranchDefect)
	}
	return executeInvoiceMutation(operation.client, call, invoiceWriteRequest{"cancelInvoice", http.MethodPost, "/invoices/" + url.PathEscape(input.InvoiceID) + "/cancel", map[string]any{"version": input.Version}, CancelInvoiceBranchCanceled, CancelInvoiceBranchProviderRejected, CancelInvoiceBranchInvalidResponse, CancelInvoiceBranchDefect})
}

type invoiceWriteRequest struct {
	operation      string
	method         string
	path           string
	body           map[string]any
	successBranch  sdkgo.BranchID
	rejectedBranch sdkgo.BranchID
	invalidBranch  sdkgo.BranchID
	defectBranch   sdkgo.BranchID
}

func invoiceMutationDefect(operation string, branch sdkgo.BranchID) sdkgo.MutationAttempt[Invoice] {
	return sdkgo.NewMutationBranch(branch, Invoice{}, failureFor(operation, sdkgo.FailureValidation, "invoice input is invalid"), sdkgo.Receipt{})
}

func executeInvoiceMutation(client *Client, call sdkgo.Call, write invoiceWriteRequest) sdkgo.MutationAttempt[Invoice] {
	payload, err := json.Marshal(write.body)
	if err != nil {
		return invoiceMutationDefect(write.operation, write.defectBranch)
	}
	request, err := http.NewRequestWithContext(call.Context, write.method, client.baseURL+write.path, bytes.NewReader(payload))
	if err != nil {
		return invoiceMutationDefect(write.operation, write.defectBranch)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	authorization := client.authorize(call, write.operation, request, write.defectBranch, write.rejectedBranch, write.invalidBranch)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, Invoice{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[Invoice](*failureFor(write.operation, sdkgo.FailureAvailability, "provider invoice outcome is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[Invoice](*failureFor(write.operation, sdkgo.FailureRateLimit, "provider rate limited invoice write"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[Invoice](*failureFor(write.operation, sdkgo.FailureAvailability, "provider invoice write is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return sdkgo.NewMutationBranch(write.rejectedBranch, Invoice{}, failureFor(write.operation, sdkgo.FailureProviderRejection, "provider rejected invoice write with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationBranch(write.invalidBranch, Invoice{}, failureFor(write.operation, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit"), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[Invoice](*failureFor(write.operation, sdkgo.FailureAvailability, "provider invoice response was interrupted"), 0)
	}
	invoice, err := decodeInvoice(responseBody)
	if err != nil {
		return sdkgo.NewMutationBranch(write.invalidBranch, Invoice{}, failureFor(write.operation, sdkgo.FailureProtocol, "provider returned invalid invoice JSON"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(write.successBranch, invoice, nil, sdkgo.Receipt{})
}

// Invoke permanently removes a draft invoice at its current version.
func (operation DeleteInvoiceOperation) Invoke(call sdkgo.Call, input DeleteInvoiceInput) sdkgo.MutationAttempt[DeletedInvoice] {
	if !validPathSegment(input.InvoiceID) || input.Version < 0 {
		return sdkgo.NewMutationBranch(DeleteInvoiceBranchDefect, DeletedInvoice{}, failureFor("deleteInvoice", sdkgo.FailureValidation, "invoice ID and nonnegative version are required"), sdkgo.Receipt{})
	}
	query := url.Values{"version": {strconv.FormatInt(int64(input.Version), 10)}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodDelete, operation.client.baseURL+"/invoices/"+url.PathEscape(input.InvoiceID)+"?"+query.Encode(), nil)
	if err != nil {
		return sdkgo.NewMutationBranch(DeleteInvoiceBranchDefect, DeletedInvoice{}, failureFor("deleteInvoice", sdkgo.FailureLocalDefect, "invoice delete request could not be built"), sdkgo.Receipt{})
	}
	request.Header.Set("Accept", "application/json")
	authorization := operation.client.authorize(call, "deleteInvoice", request, DeleteInvoiceBranchDefect, DeleteInvoiceBranchProviderRejected, DeleteInvoiceBranchInvalidResponse)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, DeletedInvoice{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedInvoice](*failureFor("deleteInvoice", sdkgo.FailureAvailability, "provider invoice delete outcome is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[DeletedInvoice](*failureFor("deleteInvoice", sdkgo.FailureRateLimit, "provider rate limited invoice deletion"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[DeletedInvoice](*failureFor("deleteInvoice", sdkgo.FailureAvailability, "provider invoice deletion is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusNotFound:
		return sdkgo.NewMutationBranch(DeleteInvoiceBranchNotFound, DeletedInvoice{}, failureFor("deleteInvoice", sdkgo.FailureNotFound, "invoice was not found"), sdkgo.Receipt{})
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return sdkgo.NewMutationBranch(DeleteInvoiceBranchProviderRejected, DeletedInvoice{}, failureFor("deleteInvoice", sdkgo.FailureProviderRejection, "provider rejected invoice deletion with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationBranch(DeleteInvoiceBranchInvalidResponse, DeletedInvoice{}, failureFor("deleteInvoice", sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit"), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[DeletedInvoice](*failureFor("deleteInvoice", sdkgo.FailureAvailability, "provider invoice delete response was interrupted"), 0)
	}
	var deletedResponse map[string]any
	if err := json.Unmarshal(responseBody, &deletedResponse); err != nil || deletedResponse == nil {
		return sdkgo.NewMutationBranch(DeleteInvoiceBranchInvalidResponse, DeletedInvoice{}, failureFor("deleteInvoice", sdkgo.FailureProtocol, "provider returned invalid invoice JSON"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(DeleteInvoiceBranchDeleted, DeletedInvoice{InvoiceID: input.InvoiceID}, nil, sdkgo.Receipt{})
}
