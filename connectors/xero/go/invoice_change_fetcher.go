// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	invoiceChangedTriggerName = "invoiceChanged"
	// invoiceChangePageSize is Xero's documented paged-invoice size; every paged invoice carries its line items.
	invoiceChangePageSize = 100
	// invoiceChangeOverlap re-reads recent changes, because If-Modified-Since has one-second precision
	// and a change may become visible after a later one.
	invoiceChangeOverlap = 5 * time.Second
	// maximumSeenInvoiceChanges keeps the cursor token under the kit's 64 KiB bound.
	maximumSeenInvoiceChanges = 2000
	// maximumInvoiceChangeWindowPages bounds paging through invoices changed within one overlap window.
	maximumInvoiceChangeWindowPages = 1000
	ascendingUpdateOrder            = "UpdatedDateUTC ASC"
	descendingUpdateOrder           = "UpdatedDateUTC DESC"
)

// invoiceChangeFetcher polls GET /Invoices ordered by UpdatedDateUTC for the poll Trigger kit.
type invoiceChangeFetcher struct {
	client        *Client
	call          sdkgo.Call
	configuration InvoiceChangedTriggerConfiguration
}

// invoiceChangeCursor is the committed Trigger cursor. Since is the newest UpdatedDateUTC delivered, in
// Unix milliseconds; Seen lists changes already delivered inside the re-read overlap window. Page is set
// only while more than one page of invoices changed within that window.
type invoiceChangeCursor struct {
	Filter   string   `json:"filter"`
	TenantID string   `json:"tenantId,omitempty"`
	Since    int64    `json:"since"`
	Page     int      `json:"page,omitempty"`
	Seen     []string `json:"seen,omitempty"`
}

type invoiceChangePageWire struct {
	DateTimeUTC string         `json:"DateTimeUTC"`
	Pagination  *pageWire      `json:"pagination"`
	Invoices    *[]invoiceWire `json:"Invoices"`
}

// listedInvoiceChanges is one decoded page with Xero's server time and the tenant the request used.
type listedInvoiceChanges struct {
	invoices     []Invoice
	serverTime   time.Time
	tenantID     string
	hasMorePages bool
}

func newInvoiceChangeFetcher(client *Client, connection sdkgo.ConnectionRef, configuration InvoiceChangedTriggerConfiguration) *invoiceChangeFetcher {
	return &invoiceChangeFetcher{
		client: client, configuration: configuration,
		call: pollingtrigger.NewTriggerCredentialCall(connection, InvoiceChangedTriggerDefinition.Trigger),
	}
}

// SeedCursor reads the most recently changed invoices once and starts at Xero's server time, so existing
// invoices become the baseline and are never delivered.
func (fetcher *invoiceChangeFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	listed, err := fetcher.listPage(ctx, nil, 1, descendingUpdateOrder)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	since := listed.serverTime
	if since.IsZero() {
		since = fetcher.client.now().UTC()
	}
	for _, invoice := range listed.invoices {
		if invoice.UpdatedAt.After(since) {
			since = invoice.UpdatedAt
		}
	}
	position := invoiceChangeCursor{Filter: fetcher.configuration.filterKey(), TenantID: listed.tenantID, Since: since.UnixMilli()}
	for _, invoice := range listed.invoices {
		if !invoice.UpdatedAt.Before(invoiceChangeWindowStart(position.Since)) {
			position.Seen = append(position.Seen, seenInvoiceChangeKey(invoice))
		}
	}
	position.Seen = pruneSeenInvoiceChanges(position.Seen, position.Since)
	return encodeInvoiceChangeCursor(position)
}

// FetchChanges reads one page of invoices changed since the cursor's overlap window and skips the changes
// it already delivered. A cursor for another filter or organisation is marked invalid, so the kit records
// a new baseline.
func (fetcher *invoiceChangeFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[InvoiceChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[InvoiceChangedEvent]]
	var position invoiceChangeCursor
	if err := json.Unmarshal([]byte(cursor.Token), &position); err != nil || position.Since <= 0 || position.Page < 0 {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Xero invoice cursor is not decodable"))
	}
	if position.Filter != fetcher.configuration.filterKey() {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Xero invoice cursor belongs to another invoice filter"))
	}
	pageNumber := max(position.Page, 1)
	windowStart := invoiceChangeWindowStart(position.Since)
	listed, err := fetcher.listPage(ctx, &windowStart, pageNumber, ascendingUpdateOrder)
	if err != nil {
		return page, err
	}
	if listed.tenantID != position.TenantID {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Xero invoice cursor belongs to another organisation"))
	}
	seen := make(map[string]bool, len(position.Seen)+len(listed.invoices))
	for _, key := range position.Seen {
		seen[key] = true
	}
	next := position
	newestUpdate := time.UnixMilli(position.Since).UTC()
	for _, invoice := range listed.invoices {
		if invoice.UpdatedAt.IsZero() {
			return page, errInvoiceWithoutUpdatedAt
		}
		if invoice.UpdatedAt.After(newestUpdate) {
			newestUpdate = invoice.UpdatedAt
		}
		key := seenInvoiceChangeKey(invoice)
		if invoice.UpdatedAt.Before(windowStart) || seen[key] {
			continue
		}
		seen[key] = true
		next.Seen = append(next.Seen, key)
		page.Changes = append(page.Changes, newInvoiceChangedTriggerEvent(invoice, listed.tenantID))
	}
	switch {
	case !listed.hasMorePages:
		next.Since, next.Page = newestUpdate.UnixMilli(), 0
	case invoiceChangeWindowStart(newestUpdate.UnixMilli()).After(windowStart):
		next.Since, next.Page, page.HasMorePages = newestUpdate.UnixMilli(), 0, true
	case pageNumber >= maximumInvoiceChangeWindowPages:
		return page, fmt.Errorf("more than %d pages of Xero invoices changed within one %s window", maximumInvoiceChangeWindowPages, invoiceChangeOverlap)
	default:
		// Every invoice on this full page changed inside the window, so only the page number can advance.
		next.Page, page.HasMorePages = pageNumber+1, true
	}
	next.Seen = pruneSeenInvoiceChanges(next.Seen, next.Since)
	page.Next, err = encodeInvoiceChangeCursor(next)
	return page, err
}

// listPage sends one GET /Invoices request with the binding's filters, line items, and the given order.
func (fetcher *invoiceChangeFetcher) listPage(ctx context.Context, modifiedSince *time.Time, pageNumber int, order string) (listedInvoiceChanges, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return listedInvoiceChanges{}, err
	}
	request, bounds, err := buildListInvoicesRequest(ListInvoicesInput{
		Statuses: fetcher.configuration.Statuses, Type: fetcher.configuration.Type,
		ModifiedSince: modifiedSince, Page: pageNumber, PageSize: invoiceChangePageSize,
	})
	if err != nil {
		return listedInvoiceChanges{}, invoiceChangedTriggerConfigurationError(err)
	}
	request.query.Set("order", order)
	result := fetcher.client.exchangeWithContext(ctx, fetcher.call, invoiceChangedTriggerName, request)
	if result.outcome != exchangeSucceeded {
		return listedInvoiceChanges{}, invoiceChangePollFailure(result)
	}
	var wire invoiceChangePageWire
	if err := json.Unmarshal(result.response.body, &wire); err != nil || wire.Invoices == nil {
		return listedInvoiceChanges{}, errors.New("Xero invoice page is not a JSON object with an Invoices list")
	}
	if len(*wire.Invoices) > pollingtrigger.MaxChangesPerPage {
		return listedInvoiceChanges{}, fmt.Errorf("Xero invoice page has %d invoices, more than %d", len(*wire.Invoices), pollingtrigger.MaxChangesPerPage)
	}
	listed := listedInvoiceChanges{tenantID: result.metadata[TenantIDReceiptKey]}
	if wire.DateTimeUTC != "" {
		if listed.serverTime, err = parseDotNetDate(wire.DateTimeUTC); err != nil {
			return listedInvoiceChanges{}, errors.New("Xero invoice page has an invalid DateTimeUTC")
		}
	}
	for _, element := range *wire.Invoices {
		invoice, err := decodeInvoice(element)
		if err != nil {
			return listedInvoiceChanges{}, fmt.Errorf("Xero invoice page has an invalid invoice: %w", err)
		}
		listed.invoices = append(listed.invoices, invoice)
	}
	listed.hasMorePages = bounds.hasMorePages(wire.Pagination, len(listed.invoices))
	return listed, nil
}

// invoiceChangePollFailure pauses the connection for Xero's minute, concurrency, and daily limits, and
// lets the kit back off this binding for any other failure.
func invoiceChangePollFailure(result xeroExchange) error {
	failure := errors.New("Xero invoiceChanged poll: " + result.failure.Message)
	switch {
	case result.outcome == exchangeDailyLimit:
		// The daily limit resets hours later; Retry-After carries the full delay, uncapped.
		seconds, _ := strconv.ParseInt(result.metadata[RetryAfterSecondsReceiptKey], 10, 64)
		return pollingtrigger.MarkRateLimited(failure, time.Duration(seconds)*time.Second)
	case result.outcome == exchangeRetry && result.failure.Kind == sdkgo.FailureRateLimit:
		return pollingtrigger.MarkRateLimited(failure, result.retryAfter)
	default:
		return failure
	}
}

// invoiceChangeWindowStart is the If-Modified-Since instant: the overlap before since, to the second.
func invoiceChangeWindowStart(sinceMilliseconds int64) time.Time {
	return time.UnixMilli(sinceMilliseconds).UTC().Add(-invoiceChangeOverlap).Truncate(time.Second)
}

// seenInvoiceChangeKey sorts by change instant: 13-digit milliseconds, then a hash of the invoice ID.
func seenInvoiceChangeKey(invoice Invoice) string {
	digest := sha256.Sum256([]byte(strings.ToLower(invoice.InvoiceID)))
	return fmt.Sprintf("%013d-%s", invoice.UpdatedAt.UnixMilli(), hex.EncodeToString(digest[:6]))
}

// pruneSeenInvoiceChanges keeps keys inside the window of since, newest maximumSeenInvoiceChanges first.
// An evicted change can be delivered again with its same Trigger event ID, which the target deduplicates.
func pruneSeenInvoiceChanges(keys []string, sinceMilliseconds int64) []string {
	floor := fmt.Sprintf("%013d", invoiceChangeWindowStart(sinceMilliseconds).UnixMilli())
	kept := make([]string, 0, len(keys))
	for _, key := range keys {
		if key >= floor {
			kept = append(kept, key)
		}
	}
	slices.Sort(kept)
	kept = slices.Compact(kept)
	if len(kept) > maximumSeenInvoiceChanges {
		kept = kept[len(kept)-maximumSeenInvoiceChanges:]
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

func encodeInvoiceChangeCursor(position invoiceChangeCursor) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Xero invoice cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}
