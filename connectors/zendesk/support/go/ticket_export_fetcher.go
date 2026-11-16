// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"context"
	"encoding/json"
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
	ticketExportPath      = "/incremental/tickets/cursor"
	ticketChangedFeedName = "ticketChanged"
	// ticketExportPageSize keeps a page of tickets with descriptions well under the default response limit;
	// Zendesk's own default and maximum is 1000.
	ticketExportPageSize = 100
	// minimumTicketExportPageSize is the smallest page an oversized response halves to before the poll fails.
	minimumTicketExportPageSize = 10
	// maximumBaselinePages bounds the seed's drain of the changes since ticketExportSeedLag ago.
	maximumBaselinePages = 100
	// ticketExportStartTimeLag keeps a restart's start_time over one minute old: Zendesk serves no data for
	// the latest minute and rejects a start_time inside it.
	ticketExportStartTimeLag = 61 * time.Second
	// ticketExportSeedLag starts the baseline five minutes back, so a Worker clock up to four minutes ahead
	// of Zendesk's still sends a start_time Zendesk accepts; the seed drains those changes undelivered.
	ticketExportSeedLag = 5 * time.Minute
)

// ticketExportFetcher reads the account's cursor-based incremental ticket export for the poll Trigger kit.
type ticketExportFetcher struct {
	client         *Client
	call           sdkgo.Call
	includeDeleted bool
	// pageSize starts at ticketExportPageSize and halves after an oversized response, for this process.
	pageSize int
}

// ticketExportCursor is the committed Trigger cursor. ResumeAt is a Unix time at or before every change
// after AfterCursor, so a rejected or missing cursor restarts the export from it without a gap.
type ticketExportCursor struct {
	AfterCursor    string `json:"afterCursor,omitempty"`
	ResumeAt       int64  `json:"resumeAt"`
	IncludeDeleted bool   `json:"includeDeleted,omitempty"`
}

type zendeskTicketExportPage struct {
	Tickets     []zendeskExportTicketWire `json:"tickets"`
	AfterCursor *string                   `json:"after_cursor"`
	EndOfStream bool                      `json:"end_of_stream"`
}

type zendeskExportTicketWire struct {
	zendeskTicketWire
	GeneratedTimestamp int64 `json:"generated_timestamp"`
}

// ticketExportPosition is where one export request starts: a cursor, or a start_time when cursor is empty.
type ticketExportPosition struct {
	cursor    string
	startTime int64
}

// errTicketExportCursorRejected marks an HTTP 400 or 422 answer to a cursor request.
var errTicketExportCursorRejected = errors.New("Zendesk rejected the incremental export cursor")

func newTicketExportFetcher(client *Client, connection sdkgo.ConnectionRef, configuration TicketChangedTriggerConfiguration) *ticketExportFetcher {
	return &ticketExportFetcher{
		client:         client,
		call:           pollingtrigger.NewTriggerCredentialCall(connection, TicketChangedTriggerDefinition.Trigger),
		includeDeleted: configuration.IncludeDeleted,
		pageSize:       ticketExportPageSize,
	}
}

// SeedCursor starts the export ticketExportSeedLag ago and drains it to the end of the stream, keeping
// only the final cursor, so tickets changed before the first poll are never delivered.
func (fetcher *ticketExportFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	position := ticketExportCursor{ResumeAt: fetcher.client.now().Add(-ticketExportSeedLag).Unix(), IncludeDeleted: fetcher.includeDeleted}
	request := ticketExportPosition{startTime: position.ResumeAt}
	for page := 0; page < maximumBaselinePages; page++ {
		listed, err := fetcher.listExportPage(ctx, request)
		if err != nil {
			return pollingtrigger.Cursor{}, err
		}
		position = advanceTicketExportCursor(position, listed)
		if listed.EndOfStream {
			return encodeTicketExportCursor(position)
		}
		if position.AfterCursor == "" || position.AfterCursor == request.cursor {
			return pollingtrigger.Cursor{}, errors.New("Zendesk incremental export page has neither end_of_stream nor a new after_cursor")
		}
		request = ticketExportPosition{cursor: position.AfterCursor}
	}
	return pollingtrigger.Cursor{}, fmt.Errorf("Zendesk incremental export baseline exceeded %d pages", maximumBaselinePages)
}

// FetchChanges reads one export page after cursor. A cursor Zendesk rejects with HTTP 400 or 422, a cursor
// without an after_cursor, or a cursor saved with another IncludeDeleted restarts from its ResumeAt time.
// An undecodable cursor is marked invalid, so the kit records a new baseline. A rejected start_time restart
// fails the poll and backs off instead: a new baseline also starts from a start_time and would lose changes.
func (fetcher *ticketExportFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[TicketChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[TicketChangedEvent]]
	var position ticketExportCursor
	if err := json.Unmarshal([]byte(cursor.Token), &position); err != nil || position.ResumeAt < 1 {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Zendesk incremental export cursor is not decodable"))
	}
	restart := ticketExportPosition{startTime: min(position.ResumeAt, fetcher.client.now().Add(-ticketExportStartTimeLag).Unix())}
	request := restart
	if position.AfterCursor != "" && position.IncludeDeleted == fetcher.includeDeleted {
		request = ticketExportPosition{cursor: position.AfterCursor}
	}
	listed, err := fetcher.listExportPage(ctx, request)
	if errors.Is(err, errTicketExportCursorRejected) {
		position.AfterCursor, request = "", restart
		listed, err = fetcher.listExportPage(ctx, request)
	}
	if err != nil {
		return page, err
	}
	if len(listed.Tickets) > pollingtrigger.MaxChangesPerPage {
		return page, fmt.Errorf("Zendesk incremental export page has %d tickets, more than %d", len(listed.Tickets), pollingtrigger.MaxChangesPerPage)
	}
	for _, wire := range listed.Tickets {
		decoded, err := decodeExportTicket(wire, fetcher.client.agentTicketURL)
		if err != nil {
			return page, fmt.Errorf("Zendesk incremental export page has an invalid ticket: %w", err)
		}
		if decoded.Status == TicketStatusDeleted && !fetcher.includeDeleted {
			continue
		}
		page.Changes = append(page.Changes, newTicketChangedTriggerEvent(decoded, wire.GeneratedTimestamp))
	}
	position.IncludeDeleted = fetcher.includeDeleted
	next := advanceTicketExportCursor(position, listed)
	if !listed.EndOfStream {
		if next.AfterCursor == "" || next.AfterCursor == request.cursor {
			return page, errors.New("Zendesk incremental export page has neither end_of_stream nor a new after_cursor")
		}
		page.HasMorePages = true
	}
	page.Next, err = encodeTicketExportCursor(next)
	return page, err
}

// listExportPage sends one export request, halving per_page while Zendesk's answer exceeds maxResponseBytes.
func (fetcher *ticketExportFetcher) listExportPage(ctx context.Context, position ticketExportPosition) (zendeskTicketExportPage, error) {
	for {
		listed, isOversized, err := fetcher.sendExportRequest(ctx, position)
		if !isOversized {
			return listed, err
		}
		if fetcher.pageSize <= minimumTicketExportPageSize {
			return zendeskTicketExportPage{}, fmt.Errorf("Zendesk incremental export page of %d tickets exceeds maxResponseBytes; raise maxResponseBytes", fetcher.pageSize)
		}
		fetcher.pageSize = max(fetcher.pageSize/2, minimumTicketExportPageSize)
	}
}

// sendExportRequest waits on the runner's and the client's request limits, then sends one export request.
func (fetcher *ticketExportFetcher) sendExportRequest(ctx context.Context, position ticketExportPosition) (zendeskTicketExportPage, bool, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return zendeskTicketExportPage{}, false, err
	}
	if err := fetcher.client.exportRequestLimiter.waitForRequestStart(ctx); err != nil {
		return zendeskTicketExportPage{}, false, err
	}
	credentials, err := fetcher.client.credentials.Resolve(fetcher.call)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return zendeskTicketExportPage{}, false, errors.New("Zendesk Trigger credentials are unavailable")
	}
	query := url.Values{"per_page": {strconv.Itoa(fetcher.pageSize)}}
	if position.cursor != "" {
		query.Set("cursor", position.cursor)
	} else {
		query.Set("start_time", strconv.FormatInt(position.startTime, 10))
	}
	if !fetcher.includeDeleted {
		query.Set("exclude_deleted", "true")
	}
	result := fetcher.client.exchangeWithContext(ctx, credentials, ticketChangedFeedName, zendeskRequest{method: http.MethodGet, path: ticketExportPath, query: query})
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeInvalid:
		if result.failure.Kind == sdkgo.FailureResponseTooLarge {
			return zendeskTicketExportPage{}, true, nil
		}
		return zendeskTicketExportPage{}, false, errors.New("Zendesk incremental export: " + result.failure.Message)
	default:
		return zendeskTicketExportPage{}, false, classifyTicketExportFailure(result, position)
	}
	var listed zendeskTicketExportPage
	if err := json.Unmarshal(result.response.body, &listed); err != nil {
		return zendeskTicketExportPage{}, false, errors.New("Zendesk incremental export page is not valid JSON")
	}
	return listed, false, nil
}

// classifyTicketExportFailure maps a failed export request to the kit's signals; messages never carry the token.
func classifyTicketExportFailure(result zendeskExchange, position ticketExportPosition) error {
	failure := errors.New("Zendesk incremental export: " + result.failure.Message)
	switch status := result.response.statusCode; {
	case status == http.StatusTooManyRequests:
		// The limit is per account, so every binding of the connection pauses.
		return pollingtrigger.MarkRateLimited(failure, result.retryAfter)
	case position.cursor != "" && (status == http.StatusBadRequest || status == http.StatusUnprocessableEntity):
		return fmt.Errorf("%w: %w", errTicketExportCursorRejected, failure)
	case status == http.StatusForbidden:
		return fmt.Errorf("%w; Zendesk allows the incremental ticket export only for administrators, so connect an administrator's email and API token", failure)
	default:
		return failure
	}
}

// advanceTicketExportCursor keeps the page's after_cursor, or the previous one when Zendesk sends none, and
// moves ResumeAt to the newest generated_timestamp, since later pages may hold more changes in that second.
// At the end of the stream every change of that second was served, so ResumeAt moves one second past it.
func advanceTicketExportCursor(position ticketExportCursor, listed zendeskTicketExportPage) ticketExportCursor {
	if listed.AfterCursor != nil && *listed.AfterCursor != "" {
		position.AfterCursor = *listed.AfterCursor
	}
	for _, ticket := range listed.Tickets {
		resumeAt := ticket.GeneratedTimestamp
		if listed.EndOfStream {
			resumeAt++
		}
		position.ResumeAt = max(position.ResumeAt, resumeAt)
	}
	return position
}

func decodeExportTicket(wire zendeskExportTicketWire, agentTicketURL string) (Ticket, error) {
	decoded, err := decodeTicketWire(wire.zendeskTicketWire, agentTicketURL)
	return decoded.ticket, err
}

func encodeTicketExportCursor(position ticketExportCursor) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Zendesk incremental export cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}
