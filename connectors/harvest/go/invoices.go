// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListInvoicesInput selects one cursor-paginated invoice page.
type ListInvoicesInput struct {
	// Cursor is the opaque cursor returned by the prior page.
	Cursor string `json:"cursor,omitempty"`
	// PerPage is from 1 to 2000; zero uses 100.
	PerPage int `json:"perPage,omitempty"`
	// ClientID optionally restricts invoices to one client.
	ClientID int64 `json:"clientId,omitempty"`
	// State optionally selects draft, open, paid, or closed invoices.
	State string `json:"state,omitempty"`
}

// GetInvoiceInput identifies an invoice by its Harvest ID.
type GetInvoiceInput struct {
	ID int64 `json:"id"`
}

// InvoiceLineItemInput describes one free-form invoice line item.
type InvoiceLineItemInput struct {
	// Kind is a configured Harvest invoice item category.
	Kind string `json:"kind"`
	// Description describes the billed work or item.
	Description string `json:"description,omitempty"`
	// Quantity is a positive item count or duration.
	Quantity float64 `json:"quantity"`
	// UnitPrice is a nonnegative amount in the invoice currency.
	UnitPrice float64 `json:"unitPrice"`
	// Taxed applies the invoice's first tax percentage.
	Taxed bool `json:"taxed"`
}

// CreateInvoiceInput creates a free-form invoice for a client.
type CreateInvoiceInput struct {
	// ClientID identifies the billed client.
	ClientID int64 `json:"clientId"`
	// Number optionally sets the invoice number; blank lets Harvest assign one.
	Number string `json:"number,omitempty"`
	// Subject is the invoice subject.
	Subject string `json:"subject,omitempty"`
	// Notes appear on the invoice.
	Notes string `json:"notes,omitempty"`
	// DueDate is an ISO calendar date; blank uses the payment term.
	DueDate string `json:"dueDate,omitempty"`
	// PaymentTerm is one of Harvest's supported terms; blank uses its default.
	PaymentTerm string `json:"paymentTerm,omitempty"`
	// LineItems are the items to bill.
	LineItems []InvoiceLineItemInput `json:"lineItems"`
}

// UpdateInvoiceInput sets absolute invoice header fields.
type UpdateInvoiceInput struct {
	// ID is the positive Harvest invoice ID.
	ID int64 `json:"id"`
	// Number replaces the invoice number when supplied.
	Number *string `json:"number,omitempty"`
	// Subject replaces the subject when supplied.
	Subject *string `json:"subject,omitempty"`
	// Notes replaces the notes when supplied; blank clears them.
	Notes *string `json:"notes,omitempty"`
	// DueDate replaces the due date as an ISO calendar date.
	DueDate *string `json:"dueDate,omitempty"`
	// PaymentTerm replaces the payment term.
	PaymentTerm *string `json:"paymentTerm,omitempty"`
}

// DeleteInvoiceInput identifies an invoice to delete.
type DeleteInvoiceInput struct {
	ID int64 `json:"id"`
}

// InvoiceRecord contains summary fields for a Harvest invoice.
type InvoiceRecord struct {
	// ID is assigned by Harvest.
	ID int64 `json:"id"`
	// ClientID identifies the billed client.
	ClientID int64 `json:"clientId"`
	// Number is the invoice number.
	Number string `json:"number"`
	// Subject is the invoice subject.
	Subject string `json:"subject"`
	// State is draft, open, paid, or closed.
	State string `json:"state"`
	// Amount is the invoice total in its currency.
	Amount float64 `json:"amount"`
	// DueAmount is the unpaid amount in its currency.
	DueAmount float64 `json:"dueAmount"`
	// IssueDate is the issue date in YYYY-MM-DD form.
	IssueDate string `json:"issueDate"`
	// DueDate is the due date in YYYY-MM-DD form.
	DueDate string `json:"dueDate"`
}

// InvoicePage contains one invoice page and its next cursor.
type InvoicePage struct {
	// Invoices are the records in this page.
	Invoices []InvoiceRecord `json:"invoices"`
	// NextCursor is blank on the final page.
	NextCursor string `json:"nextCursor,omitempty"`
}

// DeletedInvoice identifies a deleted invoice.
type DeletedInvoice struct {
	ID int64 `json:"id"`
}

// ListInvoicesOperation reads an invoice page.
type ListInvoicesOperation struct{ client *Client }

// ListInvoices binds the invoice-list Query to this client.
func (client *Client) ListInvoices() ListInvoicesOperation { return ListInvoicesOperation{client} }

// Definition returns the generated Query contract.
func (ListInvoicesOperation) Definition() sdkgo.QueryDefinition { return ListInvoicesDefinition }

// Invoke validates filters and reads invoices.
func (operation ListInvoicesOperation) Invoke(call sdkgo.Call, input ListInvoicesInput) sdkgo.QueryAttempt[InvoicePage] {
	if err := validateHarvestPage(input.Cursor, input.PerPage); err != nil {
		return failedBranch[InvoicePage]("listInvoices", ListInvoicesBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	if input.ClientID < 0 || !validInvoiceState(input.State) {
		return failedBranch[InvoicePage]("listInvoices", ListInvoicesBranchDefect, sdkgo.FailureValidation, "invoice filter is invalid")
	}
	path := harvestPagePath("/invoices", input.Cursor, input.PerPage)
	if input.ClientID > 0 {
		path += "&client_id=" + strconv.FormatInt(input.ClientID, 10)
	}
	if input.State != "" {
		path += "&state=" + input.State
	}
	return invokeGet(operation.client, call, "listInvoices", path, ListInvoicesBranchListed, ListInvoicesBranchProviderRejected, decodeInvoicePage)
}

// GetInvoiceOperation reads one invoice.
type GetInvoiceOperation struct{ client *Client }

// GetInvoice binds the invoice lookup Query to this client.
func (client *Client) GetInvoice() GetInvoiceOperation { return GetInvoiceOperation{client} }

// Definition returns the generated Query contract.
func (GetInvoiceOperation) Definition() sdkgo.QueryDefinition { return GetInvoiceDefinition }

// Invoke reads an invoice by ID.
func (operation GetInvoiceOperation) Invoke(call sdkgo.Call, input GetInvoiceInput) sdkgo.QueryAttempt[InvoiceRecord] {
	if input.ID <= 0 {
		return failedBranch[InvoiceRecord]("getInvoice", GetInvoiceBranchDefect, sdkgo.FailureValidation, "invoice ID must be positive")
	}
	return invokeGet(operation.client, call, "getInvoice", "/invoices/"+strconv.FormatInt(input.ID, 10), GetInvoiceBranchFound, GetInvoiceBranchNotFound, decodeInvoiceRecord)
}

// CreateInvoiceOperation creates a free-form invoice with bounded repeat.
type CreateInvoiceOperation struct{ client *Client }

// CreateInvoice binds invoice creation to this client.
func (client *Client) CreateInvoice() CreateInvoiceOperation { return CreateInvoiceOperation{client} }

// Definition returns the generated Mutation contract.
func (CreateInvoiceOperation) Definition() sdkgo.MutationDefinition { return CreateInvoiceDefinition }

// IdempotencyKey records the Dex call identity; Harvest does not deduplicate this POST.
func (CreateInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateInvoiceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an invoice under the limited dispatch fence.
func (operation CreateInvoiceOperation) Invoke(call sdkgo.Call, input CreateInvoiceInput) sdkgo.MutationAttempt[InvoiceRecord] {
	if input.ClientID <= 0 || len(input.LineItems) == 0 || len(input.LineItems) > 100 || !validInvoiceHeader(input.Number, input.Subject, input.Notes, input.DueDate, input.PaymentTerm) {
		return harvestMutationFailure[InvoiceRecord]("createInvoice", CreateInvoiceBranchDefect, sdkgo.FailureValidation, "invoice client, header, or line items are invalid")
	}
	for _, item := range input.LineItems {
		if !validInvoiceLineItem(item) {
			return harvestMutationFailure[InvoiceRecord]("createInvoice", CreateInvoiceBranchDefect, sdkgo.FailureValidation, "invoice line item is invalid")
		}
	}
	type lineWire struct {
		Kind        string  `json:"kind"`
		Description string  `json:"description,omitempty"`
		Quantity    float64 `json:"quantity"`
		UnitPrice   float64 `json:"unit_price"`
		Taxed       bool    `json:"taxed"`
	}
	lines := make([]lineWire, 0, len(input.LineItems))
	for _, item := range input.LineItems {
		lines = append(lines, lineWire{item.Kind, item.Description, item.Quantity, item.UnitPrice, item.Taxed})
	}
	payload, err := json.Marshal(struct {
		ClientID    int64      `json:"client_id"`
		Number      string     `json:"number,omitempty"`
		Subject     string     `json:"subject,omitempty"`
		Notes       string     `json:"notes,omitempty"`
		DueDate     string     `json:"due_date,omitempty"`
		PaymentTerm string     `json:"payment_term,omitempty"`
		LineItems   []lineWire `json:"line_items"`
	}{input.ClientID, input.Number, input.Subject, input.Notes, input.DueDate, input.PaymentTerm, lines})
	if err != nil {
		return harvestMutationFailure[InvoiceRecord]("createInvoice", CreateInvoiceBranchDefect, sdkgo.FailureValidation, "invoice input cannot be encoded")
	}
	return invokeLimitedHarvestCreate(operation.client, call, "createInvoice", CreateInvoiceDefinition.StepDefaults.ExecuteMethodTimeout, "/invoices", payload, CreateInvoiceBranchCreated, CreateInvoiceBranchProviderRejected, CreateInvoiceBranchDefect, decodeInvoiceRecord, func(invoice InvoiceRecord) string { return strconv.FormatInt(invoice.ID, 10) })
}

// UpdateInvoiceOperation changes absolute invoice header fields.
type UpdateInvoiceOperation struct{ client *Client }

// UpdateInvoice binds the invoice update Mutation to this client.
func (client *Client) UpdateInvoice() UpdateInvoiceOperation { return UpdateInvoiceOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateInvoiceOperation) Definition() sdkgo.MutationDefinition { return UpdateInvoiceDefinition }

// IdempotencyKey records the stable Dex call identity.
func (UpdateInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateInvoiceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies a repeat-safe invoice PATCH.
func (operation UpdateInvoiceOperation) Invoke(call sdkgo.Call, input UpdateInvoiceInput) sdkgo.MutationAttempt[InvoiceRecord] {
	if input.ID <= 0 || input.Number == nil && input.Subject == nil && input.Notes == nil && input.DueDate == nil && input.PaymentTerm == nil || !validOptionalInvoiceHeader(input) {
		return harvestMutationFailure[InvoiceRecord]("updateInvoice", UpdateInvoiceBranchDefect, sdkgo.FailureValidation, "invoice ID or update fields are invalid")
	}
	payload, err := json.Marshal(struct {
		Number      *string `json:"number,omitempty"`
		Subject     *string `json:"subject,omitempty"`
		Notes       *string `json:"notes,omitempty"`
		DueDate     *string `json:"due_date,omitempty"`
		PaymentTerm *string `json:"payment_term,omitempty"`
	}{input.Number, input.Subject, input.Notes, input.DueDate, input.PaymentTerm})
	if err != nil {
		return harvestMutationFailure[InvoiceRecord]("updateInvoice", UpdateInvoiceBranchDefect, sdkgo.FailureValidation, "invoice update cannot be encoded")
	}
	return invokeSafeHarvestWrite(operation.client, call, "updateInvoice", http.MethodPatch, "/invoices/"+strconv.FormatInt(input.ID, 10), payload, UpdateInvoiceBranchUpdated, UpdateInvoiceBranchNotFound, UpdateInvoiceBranchProviderRejected, decodeInvoiceRecord)
}

// DeleteInvoiceOperation deletes an invoice.
type DeleteInvoiceOperation struct{ client *Client }

// DeleteInvoice binds the invoice deletion Mutation to this client.
func (client *Client) DeleteInvoice() DeleteInvoiceOperation { return DeleteInvoiceOperation{client} }

// Definition returns the generated Mutation contract.
func (DeleteInvoiceOperation) Definition() sdkgo.MutationDefinition { return DeleteInvoiceDefinition }

// IdempotencyKey records the stable Dex call identity.
func (DeleteInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteInvoiceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends a repeat-safe invoice DELETE.
func (operation DeleteInvoiceOperation) Invoke(call sdkgo.Call, input DeleteInvoiceInput) sdkgo.MutationAttempt[DeletedInvoice] {
	if input.ID <= 0 {
		return harvestMutationFailure[DeletedInvoice]("deleteInvoice", DeleteInvoiceBranchDefect, sdkgo.FailureValidation, "invoice ID must be positive")
	}
	decode := func(_ []byte) (DeletedInvoice, error) { return DeletedInvoice{ID: input.ID}, nil }
	return invokeSafeHarvestWrite(operation.client, call, "deleteInvoice", http.MethodDelete, "/invoices/"+strconv.FormatInt(input.ID, 10), nil, DeleteInvoiceBranchDeleted, DeleteInvoiceBranchNotFound, DeleteInvoiceBranchProviderRejected, decode)
}

func validInvoiceState(state string) bool {
	return state == "" || state == "draft" || state == "open" || state == "paid" || state == "closed"
}
func validInvoiceDate(date string) bool {
	if date == "" {
		return true
	}
	parsed, err := time.Parse("2006-01-02", date)
	return err == nil && parsed.Format("2006-01-02") == date
}
func validInvoicePaymentTerm(term string) bool {
	switch term {
	case "", "custom", "upon receipt", "net 15", "net 30", "net 45", "net 60":
		return true
	}
	return false
}
func validInvoiceHeader(number, subject, notes, dueDate, paymentTerm string) bool {
	return len(number) <= 100 && len(subject) <= 500 && len(notes) <= 10000 && validInvoiceDate(dueDate) && validInvoicePaymentTerm(paymentTerm) && (dueDate == "" || paymentTerm == "" || paymentTerm == "custom")
}
func validOptionalInvoiceHeader(input UpdateInvoiceInput) bool {
	number, subject, notes, dueDate, paymentTerm := "", "", "", "", ""
	if input.Number != nil {
		number = *input.Number
	}
	if input.Subject != nil {
		subject = *input.Subject
	}
	if input.Notes != nil {
		notes = *input.Notes
	}
	if input.DueDate != nil {
		dueDate = *input.DueDate
	}
	if input.PaymentTerm != nil {
		paymentTerm = *input.PaymentTerm
	}
	return validInvoiceHeader(number, subject, notes, dueDate, paymentTerm)
}
func validInvoiceLineItem(item InvoiceLineItemInput) bool {
	return strings.TrimSpace(item.Kind) != "" && len(item.Kind) <= 255 && len(item.Description) <= 5000 && item.Quantity > 0 && !math.IsNaN(item.Quantity) && !math.IsInf(item.Quantity, 0) && item.UnitPrice >= 0 && !math.IsNaN(item.UnitPrice) && !math.IsInf(item.UnitPrice, 0)
}
func decodeInvoiceRecord(body []byte) (InvoiceRecord, error) {
	var wire struct {
		ID     int64 `json:"id"`
		Client struct {
			ID int64 `json:"id"`
		} `json:"client"`
		Number    string  `json:"number"`
		Subject   string  `json:"subject"`
		State     string  `json:"state"`
		Amount    float64 `json:"amount"`
		DueAmount float64 `json:"due_amount"`
		IssueDate string  `json:"issue_date"`
		DueDate   string  `json:"due_date"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return InvoiceRecord{}, err
	}
	if wire.ID <= 0 || wire.Client.ID <= 0 || !validInvoiceState(wire.State) || wire.State == "" || math.IsNaN(wire.Amount) || math.IsInf(wire.Amount, 0) {
		return InvoiceRecord{}, fmt.Errorf("missing invoice identity")
	}
	return InvoiceRecord{ID: wire.ID, ClientID: wire.Client.ID, Number: wire.Number, Subject: wire.Subject, State: wire.State, Amount: wire.Amount, DueAmount: wire.DueAmount, IssueDate: wire.IssueDate, DueDate: wire.DueDate}, nil
}
func decodeInvoicePage(body []byte) (InvoicePage, error) {
	var wire struct {
		Invoices []json.RawMessage `json:"invoices"`
		Links    struct {
			Next *string `json:"next"`
		} `json:"links"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return InvoicePage{}, err
	}
	if wire.Invoices == nil {
		return InvoicePage{}, fmt.Errorf("missing invoices")
	}
	next, err := nextHarvestCursor(wire.Links.Next, "/invoices")
	if err != nil {
		return InvoicePage{}, err
	}
	page := InvoicePage{Invoices: make([]InvoiceRecord, 0, len(wire.Invoices)), NextCursor: next}
	for _, raw := range wire.Invoices {
		invoice, err := decodeInvoiceRecord(raw)
		if err != nil {
			return InvoicePage{}, err
		}
		page.Invoices = append(page.Invoices, invoice)
	}
	return page, nil
}
