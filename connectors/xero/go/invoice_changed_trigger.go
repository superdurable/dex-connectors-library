// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// InvoiceChangedTriggerConfiguration is the invoiceChanged binding configuration that the
// invoiceChangeFilter unit saves in Dex Web. Both filters are optional and combine. Changing the filter
// of a binding that already polled makes its next poll record a new baseline for the new filter.
type InvoiceChangedTriggerConfiguration struct {
	// Type keeps only ACCREC sales invoices or ACCPAY bills; blank keeps both.
	Type InvoiceType `json:"type,omitempty"`
	// Statuses keeps invoices whose status after the change is one of these Xero statuses; empty keeps
	// every status, including DELETED and VOIDED.
	Statuses []InvoiceStatus `json:"statuses,omitempty"`
}

// Validate rejects an unknown type or status and a repeated status; blank filters are valid.
func (configuration InvoiceChangedTriggerConfiguration) Validate() error {
	if configuration.Type != "" {
		if err := validateInvoiceType(configuration.Type); err != nil {
			return err
		}
	}
	if err := validateInvoiceStatuses("statuses", configuration.Statuses); err != nil {
		return err
	}
	seen := map[InvoiceStatus]bool{}
	for _, status := range configuration.Statuses {
		if seen[status] {
			return fmt.Errorf("statuses lists %s more than once", status)
		}
		seen[status] = true
	}
	return nil
}

// filterKey is the canonical filter the cursor records, so a changed filter records a new baseline.
func (configuration InvoiceChangedTriggerConfiguration) filterKey() string {
	statuses := make([]string, 0, len(configuration.Statuses))
	for _, status := range configuration.Statuses {
		statuses = append(statuses, string(status))
	}
	slices.Sort(statuses)
	return "type=" + string(configuration.Type) + ";statuses=" + strings.Join(statuses, ",")
}

// InvoiceChangedEvent is one sales invoice or bill as Xero listed it after a change. Xero reports no
// change type in a list, so a creation and an update look alike: compare Invoice.Status with the
// application's own record when the difference matters.
type InvoiceChangedEvent struct {
	// Invoice is the invoice with its line items, exact decimal totals, and UpdatedAt, the
	// UpdatedDateUTC instant of this change.
	Invoice Invoice `json:"invoice"`
	// TenantID is the Xero-Tenant-Id an OAuth connection read the invoice with; blank for a Custom
	// Connection, which reaches exactly one organisation.
	TenantID string `json:"tenantId,omitempty"`
}

// newInvoiceChangedTriggerEvent converts one listed invoice. Its ID is a pure function of the invoice and
// its UpdatedDateUTC, so a refetch after a crash yields the same ID and a later change a new one.
func newInvoiceChangedTriggerEvent(invoice Invoice, tenantID string) sdkgo.TriggerEvent[InvoiceChangedEvent] {
	return sdkgo.TriggerEvent[InvoiceChangedEvent]{
		ID:         invoiceChangeID(invoice.InvoiceID, invoice.UpdatedAt),
		OccurredAt: invoice.UpdatedAt.UTC(),
		Payload:    InvoiceChangedEvent{Invoice: invoice, TenantID: tenantID},
	}
}

// invoiceChangeID hashes the invoice ID and change instant, so it stays short and URL-safe.
func invoiceChangeID(invoiceID string, updatedAt time.Time) string {
	digest := sha256.Sum256([]byte(strings.ToLower(invoiceID) + "\x00" + strconv.FormatInt(updatedAt.UnixMilli(), 10)))
	return "xero-invoice-" + hex.EncodeToString(digest[:16])
}

func invoiceChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("xero invoiceChanged configuration: %w", err)
}

var errInvoiceWithoutUpdatedAt = errors.New("Xero listed an invoice without UpdatedDateUTC")
