// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	invoiceChangedOperationID = "invoiceChanged"
	// invoiceChangePageSize keeps one poll page of full invoices well under the default response limit;
	// QuickBooks itself accepts MAXRESULTS up to 1000.
	invoiceChangePageSize = 100
	// maximumSeedPages bounds the baseline scan of the lookback window.
	maximumSeedPages = maximumSeenInvoiceVersions / invoiceChangePageSize
)

// invoiceChangeFetcher polls one company's invoices ordered by MetaData.LastUpdatedTime for the poll
// Trigger kit; see invoiceChangeCursor for the cursor.
type invoiceChangeFetcher struct {
	client *Client
	call   sdkgo.Call
}

// polledInvoice is one decoded invoice with its LastUpdatedTime second.
type polledInvoice struct {
	invoice   Invoice
	updatedAt time.Time
}

func newInvoiceChangeFetcher(client *Client, connection sdkgo.ConnectionRef) *invoiceChangeFetcher {
	return &invoiceChangeFetcher{
		client: client, call: pollingtrigger.NewTriggerCredentialCall(connection, InvoiceChangedTriggerDefinition.Trigger),
	}
}

// SeedCursor reads the most recently changed invoices and records every version in the lookback window
// before the newest LastUpdatedTime, so existing invoices become the baseline and are never delivered.
func (fetcher *invoiceChangeFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	var realmID string
	var baseline []polledInvoice
	var newest time.Time
	isWindowComplete := false
	for page := 0; page < maximumSeedPages && !isWindowComplete; page++ {
		statement := "select * from Invoice ORDERBY MetaData.LastUpdatedTime DESC STARTPOSITION " +
			strconv.Itoa(1+page*invoiceChangePageSize) + " MAXRESULTS " + strconv.Itoa(invoiceChangePageSize)
		pageRealmID, invoices, err := fetcher.queryInvoices(ctx, statement, false)
		if err != nil {
			return pollingtrigger.Cursor{}, err
		}
		realmID = pageRealmID
		isWindowComplete = len(invoices) < invoiceChangePageSize
		for _, polled := range invoices {
			if newest.IsZero() {
				newest = polled.updatedAt
			}
			if polled.updatedAt.Before(newest.Add(-invoiceChangeLookback)) {
				isWindowComplete = true
				break
			}
			baseline = append(baseline, polled)
		}
	}
	window := invoiceWindow{seen: map[string]time.Time{}}
	if len(baseline) != 0 {
		window.since, window.from = newest, newest.Add(-invoiceChangeLookback)
		if !isWindowComplete {
			// The scan stopped inside the window: its oldest second may be partial, so start after it.
			window.from = baseline[len(baseline)-1].updatedAt.Add(time.Second)
		}
		for _, polled := range baseline {
			if !polled.updatedAt.Before(window.from) {
				window.seen[invoiceVersionKey(polled.invoice)] = polled.updatedAt
			}
		}
		if window.from.After(window.since) {
			window.from = window.since.Add(time.Second)
			window.since = window.from
		}
		window.pruneToBound()
	}
	return encodeInvoiceChangeCursor(realmID, fetcher.client.environment, window)
}

// FetchChanges reads one page of invoices changed at or after the window's lower bound, skipping
// versions already delivered or baselined. An undecodable cursor, one for another company or
// environment, or a QuickBooks validation Fault for the watermark query is marked invalid, so the kit
// records a new baseline.
func (fetcher *invoiceChangeFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[InvoiceChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[InvoiceChangedEvent]]
	position, window, err := decodeInvoiceChangeCursor(cursor.Token)
	if err != nil {
		return page, pollingtrigger.MarkCursorInvalid(err)
	}
	if position.Environment != fetcher.client.environment {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("QuickBooks invoice cursor belongs to another environment"))
	}
	statement := "select * from Invoice"
	if position.From != "" {
		statement += " where MetaData.LastUpdatedTime >= " + quoteQueryLiteral(position.From)
	}
	statement += " ORDERBY MetaData.LastUpdatedTime STARTPOSITION " + strconv.Itoa(1+window.offset) +
		" MAXRESULTS " + strconv.Itoa(invoiceChangePageSize)
	realmID, invoices, err := fetcher.queryInvoices(ctx, statement, position.From != "")
	if err != nil {
		return page, err
	}
	if realmID != position.RealmID {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("QuickBooks invoice cursor belongs to another company"))
	}
	previous := window.from
	for _, polled := range invoices {
		if polled.updatedAt.Before(previous) {
			return page, errors.New("QuickBooks invoice page is not in LastUpdatedTime order")
		}
		previous = polled.updatedAt
		if _, isSeen := window.seen[invoiceVersionKey(polled.invoice)]; isSeen {
			continue
		}
		page.Changes = append(page.Changes, newInvoiceChangedTriggerEvent(realmID, fetcher.client.environment, polled.invoice))
	}
	isFullPage := len(invoices) == invoiceChangePageSize
	page.HasMorePages = isFullPage
	page.Next, err = encodeInvoiceChangeCursor(position.RealmID, position.Environment, window.advance(invoices, isFullPage))
	return page, err
}

// queryInvoices sends one invoice query and returns the company it ran in and its invoices. A 429 pauses
// the connection for Retry-After, or the 60 seconds QuickBooks documents without it.
func (fetcher *invoiceChangeFetcher) queryInvoices(ctx context.Context, statement string, hasWatermark bool) (string, []polledInvoice, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return "", nil, err
	}
	result := fetcher.client.exchangeWithContext(ctx, fetcher.call, invoiceChangedOperationID,
		quickbooksRequest{method: http.MethodGet, path: "/query", query: url.Values{"query": {statement}}})
	if result.outcome != exchangeSucceeded {
		failure := fmt.Errorf("QuickBooks invoice poll: %s", result.failure.Message)
		switch {
		case result.failure.Kind == sdkgo.FailureRateLimit:
			return "", nil, pollingtrigger.MarkRateLimited(failure, result.retryAfter)
		case hasWatermark && result.response.statusCode == http.StatusBadRequest && result.failure.Kind == sdkgo.FailureValidation:
			return "", nil, pollingtrigger.MarkCursorInvalid(failure)
		}
		return "", nil, failure
	}
	realmID := result.metadata[RealmIDReceiptKey]
	elements, err := decodeQueryEntities[invoiceWire](result.response.body, "Invoice")
	if err != nil {
		return "", nil, fmt.Errorf("QuickBooks invoice poll returned an invalid page: %w", err)
	}
	if len(elements) > invoiceChangePageSize {
		return "", nil, errors.New("QuickBooks invoice poll returned more invoices than requested")
	}
	invoices := make([]polledInvoice, 0, len(elements))
	for _, element := range elements {
		invoice, err := decodeInvoice(element)
		if err != nil {
			return "", nil, fmt.Errorf("QuickBooks invoice poll returned an invalid invoice: %w", err)
		}
		if invoice.UpdatedAt == nil {
			return "", nil, errors.New("QuickBooks invoice poll returned an invoice without MetaData.LastUpdatedTime")
		}
		invoices = append(invoices, polledInvoice{invoice: invoice, updatedAt: invoice.UpdatedAt.UTC().Truncate(time.Second)})
	}
	return realmID, invoices, nil
}
