// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// InvoiceChangeKind is the derived kind of one invoiceChanged delivery.
type InvoiceChangeKind string

const (
	// InvoiceChangeCreated is an invoice at its first version: QuickBooks reports SyncToken 0.
	InvoiceChangeCreated InvoiceChangeKind = "created"
	// InvoiceChangeUpdated is any later version, including an edit, an applied payment that lowers
	// Balance, an email that sets EmailStatus, and a void that zeroes the amounts. An invoice created and
	// edited between two polls arrives once, as updated.
	InvoiceChangeUpdated InvoiceChangeKind = "updated"
)

// newInvoiceSyncToken is the SyncToken QuickBooks gives an entity when it is created.
const newInvoiceSyncToken = "0"

// InvoiceChangedTriggerConfiguration is the invoiceChanged binding configuration. A binding watches every
// invoice of the connection's company, so it has no fields; an application narrows the changes it starts
// Flows for in its Trigger target's accept function, for example by customer.
type InvoiceChangedTriggerConfiguration struct{}

// Validate accepts every configuration; it exists for the generated Trigger factory and future fields.
func (InvoiceChangedTriggerConfiguration) Validate() error { return nil }

// InvoiceChangedEvent is one invoice version read by the invoiceChanged poll. QuickBooks's invoice query
// does not return deleted invoices, so a deletion is never delivered.
type InvoiceChangedEvent struct {
	// RealmID is the QuickBooks company the invoice belongs to.
	RealmID string `json:"realmId"`
	// Environment is the connection's QuickBooks environment, production or sandbox.
	Environment Environment `json:"environment"`
	// ChangeKind is the derived change kind; see InvoiceChangeKind.
	ChangeKind InvoiceChangeKind `json:"changeKind"`
	// Invoice is the invoice as QuickBooks returned it in the poll, with its SyncToken and UpdatedAt.
	Invoice Invoice `json:"invoice"`
}

// newInvoiceChangedTriggerEvent converts one polled invoice. Its ID is a pure function of the company and
// the invoice version, so a refetch after a crash yields the same ID.
func newInvoiceChangedTriggerEvent(realmID string, environment Environment, invoice Invoice) sdkgo.TriggerEvent[InvoiceChangedEvent] {
	kind := InvoiceChangeUpdated
	if invoice.SyncToken == newInvoiceSyncToken {
		kind = InvoiceChangeCreated
	}
	occurredAt := time.Time{}
	if invoice.UpdatedAt != nil {
		occurredAt = invoice.UpdatedAt.UTC()
	}
	return sdkgo.TriggerEvent[InvoiceChangedEvent]{
		ID:         invoiceChangeID(realmID, environment, invoice),
		OccurredAt: occurredAt,
		Payload:    InvoiceChangedEvent{RealmID: realmID, Environment: environment, ChangeKind: kind, Invoice: invoice},
	}
}

// invoiceChangeID hashes the company, invoice Id, SyncToken, and LastUpdatedTime, so it stays short.
func invoiceChangeID(realmID string, environment Environment, invoice Invoice) string {
	updated := ""
	if invoice.UpdatedAt != nil {
		updated = invoice.UpdatedAt.UTC().Format(time.RFC3339)
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{string(environment), realmID, invoice.InvoiceID, invoice.SyncToken, updated}, "\x00")))
	return "qbo-invoice-" + hex.EncodeToString(digest[:16])
}

func invoiceChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("quickbooks invoiceChanged configuration: %w", err)
}
