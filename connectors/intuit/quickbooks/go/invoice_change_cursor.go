// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	// invoiceChangeLookback rereads invoices whose LastUpdatedTime is up to this long before the newest one
	// delivered, so a write QuickBooks stamped earlier but made visible after a poll still arrives.
	invoiceChangeLookback = 30 * time.Second
	// maximumSeenInvoiceVersions bounds the versions the cursor remembers inside the lookback window, so
	// the cursor stays far below the kit's 64 KiB token bound.
	maximumSeenInvoiceVersions = 1000
	// invoiceTiePageOverlap rereads part of the previous page, since QuickBooks leaves the order of equal
	// timestamps unspecified.
	invoiceTiePageOverlap = 20
)

// invoiceChangeCursor is the committed Trigger cursor. QuickBooks offers no change token, so the cursor
// is a LastUpdatedTime window. Since is the newest LastUpdatedTime second delivered or baselined and From
// the query's lower bound, normally Since minus invoiceChangeLookback; both are UTC RFC 3339 seconds and
// blank means the company had no invoice at the baseline. Seen holds "<unix second>/<Id>:<SyncToken>" of
// every version at or after From already delivered or baselined. Offset pages through a full window.
type invoiceChangeCursor struct {
	RealmID     string      `json:"realmId"`
	Environment Environment `json:"environment"`
	Since       string      `json:"since,omitempty"`
	From        string      `json:"from,omitempty"`
	Seen        []string    `json:"seen,omitempty"`
	Offset      int         `json:"offset,omitempty"`
}

// invoiceWindow is a decoded cursor position.
type invoiceWindow struct {
	since, from time.Time
	seen        map[string]time.Time
	offset      int
}

func invoiceVersionKey(invoice Invoice) string { return invoice.InvoiceID + ":" + invoice.SyncToken }

// advance records a page read from window and returns the next window. The lower bound never moves back,
// so no version below it is read again; the page offset continues only while the lower bound stays put.
func (window invoiceWindow) advance(invoices []polledInvoice, isFullPage bool) invoiceWindow {
	next := invoiceWindow{since: window.since, from: window.from, seen: make(map[string]time.Time, len(window.seen)+len(invoices))}
	for key, second := range window.seen {
		next.seen[key] = second
	}
	for _, polled := range invoices {
		next.seen[invoiceVersionKey(polled.invoice)] = polled.updatedAt
		if polled.updatedAt.After(next.since) {
			next.since = polled.updatedAt
		}
	}
	if next.since.IsZero() {
		return next
	}
	if floor := next.since.Add(-invoiceChangeLookback); floor.After(next.from) {
		next.from = floor
	}
	next.pruneToBound()
	if isFullPage && next.from.Equal(window.from) {
		next.offset = max(0, window.offset+len(invoices)-invoiceTiePageOverlap)
	}
	return next
}

// pruneToBound drops versions below the lower bound, then whole oldest seconds while more versions than
// the cursor keeps remain, raising the lower bound past them. A single second with more versions than
// that moves the window past it; later versions in that second are not delivered.
func (window *invoiceWindow) pruneToBound() {
	seconds := map[time.Time]int{}
	for key, second := range window.seen {
		if second.Before(window.from) {
			delete(window.seen, key)
			continue
		}
		seconds[second]++
	}
	ordered := make([]time.Time, 0, len(seconds))
	for second := range seconds {
		ordered = append(ordered, second)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Before(ordered[j]) })
	for remaining := len(window.seen); remaining > maximumSeenInvoiceVersions; ordered = ordered[1:] {
		remaining -= seconds[ordered[0]]
		if len(ordered) == 1 {
			window.since = ordered[0].Add(time.Second)
			window.from, window.seen = window.since, map[string]time.Time{}
			return
		}
		window.from = ordered[1]
		for key, second := range window.seen {
			if second.Before(window.from) {
				delete(window.seen, key)
			}
		}
	}
}

func encodeInvoiceChangeCursor(realmID string, environment Environment, window invoiceWindow) (pollingtrigger.Cursor, error) {
	position := invoiceChangeCursor{RealmID: realmID, Environment: environment, Offset: window.offset}
	if !window.since.IsZero() {
		position.Since, position.From = window.since.Format(time.RFC3339), window.from.Format(time.RFC3339)
	}
	for key, second := range window.seen {
		position.Seen = append(position.Seen, strconv.FormatInt(second.Unix(), 10)+"/"+key)
	}
	sort.Strings(position.Seen)
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode QuickBooks invoice cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}

func decodeInvoiceChangeCursor(token string) (invoiceChangeCursor, invoiceWindow, error) {
	var position invoiceChangeCursor
	if err := json.Unmarshal([]byte(token), &position); err != nil || !realmIDPattern.MatchString(position.RealmID) {
		return invoiceChangeCursor{}, invoiceWindow{}, errors.New("QuickBooks invoice cursor is not decodable")
	}
	if position.Offset < 0 || len(position.Seen) > maximumSeenInvoiceVersions {
		return invoiceChangeCursor{}, invoiceWindow{}, errors.New("QuickBooks invoice cursor is out of bounds")
	}
	window := invoiceWindow{offset: position.Offset, seen: make(map[string]time.Time, len(position.Seen))}
	if position.Since == "" && position.From == "" && len(position.Seen) == 0 {
		return position, window, nil
	}
	since, sinceErr := time.Parse(time.RFC3339, position.Since)
	from, fromErr := time.Parse(time.RFC3339, position.From)
	if sinceErr != nil || fromErr != nil || from.After(since) {
		return invoiceChangeCursor{}, invoiceWindow{}, errors.New("QuickBooks invoice cursor has an invalid watermark")
	}
	window.since, window.from = since.UTC(), from.UTC()
	for _, entry := range position.Seen {
		unix, key, isEntry := strings.Cut(entry, "/")
		seconds, err := strconv.ParseInt(unix, 10, 64)
		second := time.Unix(seconds, 0).UTC()
		if !isEntry || err != nil || !strings.Contains(key, ":") || second.Before(window.from) || second.After(window.since) {
			return invoiceChangeCursor{}, invoiceWindow{}, errors.New("QuickBooks invoice cursor has an invalid delivered version")
		}
		window.seen[key] = second
	}
	return position, window, nil
}
