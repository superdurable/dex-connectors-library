// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

const (
	ticketChangedTriggerOperation = "ticketChanged"
	// ticketPollPageLimit is Zoho Desk's largest search page.
	ticketPollPageLimit = MaxSearchLimit
	// ticketPollMaximumFrom keeps from+limit within the 1,000 results Zoho Desk's search API documents.
	ticketPollMaximumFrom = 1000 - ticketPollPageLimit
	// TicketSearchIndexLookback is how far before the newest modified time it has read each ticketChanged
	// sweep starts again, because Zoho Desk indexes changed tickets for search with a delay. A ticket
	// version re-read inside the lookback is recognized by the cursor snapshot and not delivered twice.
	TicketSearchIndexLookback = 5 * time.Minute
	// ticketSnapshotEntries bounds the ticket versions the cursor remembers; the least recently seen go first.
	ticketSnapshotEntries = pollingtrigger.DefaultKeyedSnapshotEntries
	// maximumBaselinePages bounds the first sweep, which only records the lookback window as a baseline.
	maximumBaselinePages = ticketSnapshotEntries / ticketPollPageLimit
	// maximumServerClockSkew is the Date header skew beyond which the baseline ignores it as implausible.
	maximumServerClockSkew = 24 * time.Hour
)

// ticketPollSnapshotOptions evicts the oldest ticket versions, because the search is a delta feed.
var ticketPollSnapshotOptions = pollingtrigger.KeyedSnapshotOptions{MaxEntries: ticketSnapshotEntries, EvictLeastRecentlySeen: true}

// ticketChangeFetcher reads one organization's tickets in ascending modified-time order for the poll kit.
type ticketChangeFetcher struct {
	client       *Client
	call         sdkgo.Call
	departmentID string
}

// ticketChangeCursor is the committed position. Floor is the inclusive lower bound of modifiedTimeRange,
// From the offset among tickets modified exactly at Floor, SweepStart the Floor the current sweep began
// at, and HighWater the newest modified time read.
type ticketChangeCursor struct {
	OrgID        string `json:"orgId"`
	DepartmentID string `json:"departmentId,omitempty"`
	SweepStart   string `json:"sweepStart"`
	Floor        string `json:"floor"`
	From         int    `json:"from,omitempty"`
	HighWater    string `json:"highWater"`
}

// ticketVersion is one ticket version tracked by the cursor snapshot.
type ticketVersion struct{ ticket Ticket }

func (version ticketVersion) SnapshotKey() string            { return version.ticket.ID }
func (version ticketVersion) SnapshotHash() string           { return ticketVersionHash(version.ticket) }
func (version ticketVersion) SnapshotValue() json.RawMessage { return nil }

// ticketPollPage is one search page; received counts results before duplicates collapse, so fullness survives.
type ticketPollPage struct {
	tickets    []Ticket
	received   int
	last       time.Time
	serverTime time.Time
}

// ticketSearchPosition is a decoded cursor.
type ticketSearchPosition struct {
	sweepStart time.Time
	floor      time.Time
	from       int
	highWater  time.Time
}

func newTicketChangeFetcher(client *Client, connection sdkgo.ConnectionRef, configuration TicketChangedTriggerConfiguration) *ticketChangeFetcher {
	return &ticketChangeFetcher{
		client:       client,
		call:         pollingtrigger.NewTriggerCredentialCall(connection, TicketChangedTriggerDefinition.Trigger),
		departmentID: configuration.DepartmentID,
	}
}

// SeedCursor records every ticket modified inside the lookback before now as the baseline and delivers none.
// A skewed local clock yields to Zoho Desk's Date header, so a fast clock skips nothing.
func (fetcher *ticketChangeFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	now := fetcher.client.now().UTC()
	start := now.Add(-TicketSearchIndexLookback)
	position := ticketSearchPosition{sweepStart: start, floor: start, highWater: now}
	var snapshot *projectconfig.KeyedSnapshot
	for pageNumber := 0; pageNumber < maximumBaselinePages; pageNumber++ {
		page, err := fetcher.searchPage(ctx, position)
		if err != nil {
			return pollingtrigger.Cursor{}, err
		}
		if snapshot, err = applyTicketVersions(snapshot, page.tickets); err != nil {
			return pollingtrigger.Cursor{}, err
		}
		if pageNumber == 0 && isServerClockSkewed(page.serverTime, now) {
			position.highWater = page.serverTime
		}
		next, hasMorePages := nextTicketSearchPosition(position, page)
		position = next
		if !hasMorePages {
			return fetcher.encodeCursor(position, snapshot)
		}
	}
	return pollingtrigger.Cursor{}, fmt.Errorf("Zoho Desk ticketChanged baseline exceeded %d search pages", maximumBaselinePages)
}

// FetchChanges reads one search page after cursor and returns the ticket versions the snapshot lacks. An
// undecodable cursor, one for another organization or department, or a search that rejects the cursor's
// time range or offset is marked invalid, so the kit records a new baseline.
func (fetcher *ticketChangeFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[TicketChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[TicketChangedEvent]]
	position, err := fetcher.decodeCursor(cursor.Token)
	if err != nil {
		return page, pollingtrigger.MarkCursorInvalid(err)
	}
	received, err := fetcher.searchPage(ctx, position)
	if err != nil {
		return page, err
	}
	for _, ticket := range received.tickets {
		stored := findTicketVersion(cursor.Snapshot, ticket.ID)
		if stored != nil && stored.Hash == ticketVersionHash(ticket) {
			continue
		}
		kind := ticketChangeKind(ticket, stored != nil, position.sweepStart)
		page.Changes = append(page.Changes, newTicketChangedTriggerEvent(fetcher.client.organizationID, fetcher.departmentID, kind, ticket))
	}
	snapshot, err := applyTicketVersions(cursor.Snapshot, received.tickets)
	if err != nil {
		return page, err
	}
	next, hasMorePages := nextTicketSearchPosition(position, received)
	page.HasMorePages = hasMorePages
	page.Next, err = fetcher.encodeCursor(next, snapshot)
	return page, err
}

// nextTicketSearchPosition advances by modified time, so a ticket changed during a sweep only moves later
// in the order and is never skipped. A full page of one instant pages by offset; past Zoho Desk's offset
// limit it steps one millisecond, which skips the rest of that instant. A short page ends the sweep, and
// the next sweep starts TicketSearchIndexLookback before the newest modified time read.
func nextTicketSearchPosition(position ticketSearchPosition, page ticketPollPage) (ticketSearchPosition, bool) {
	for _, ticket := range page.tickets {
		if ticket.ModifiedAt.After(position.highWater) {
			position.highWater = ticket.ModifiedAt.UTC()
		}
	}
	if page.received < ticketPollPageLimit {
		start := position.highWater.Add(-TicketSearchIndexLookback)
		return ticketSearchPosition{sweepStart: start, floor: start, highWater: position.highWater}, false
	}
	last := page.last.UTC()
	switch {
	case last.After(position.floor):
		position.floor, position.from = last, 0
	case position.from+ticketPollPageLimit <= ticketPollMaximumFrom:
		position.from += ticketPollPageLimit
	default:
		position.floor, position.from = position.floor.Add(time.Millisecond), 0
	}
	return position, true
}

// searchPage sends one GET /api/v1/tickets/search sorted by ascending modifiedTime from position.
func (fetcher *ticketChangeFetcher) searchPage(ctx context.Context, position ticketSearchPosition) (ticketPollPage, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return ticketPollPage{}, err
	}
	session, cancel, sessionFailure := fetcher.client.startSessionWithContext(ctx, fetcher.call, ticketChangedTriggerOperation)
	if sessionFailure != nil {
		return ticketPollPage{}, fmt.Errorf("Zoho Desk ticketChanged poll: %s", sessionFailure.failure.Message)
	}
	defer cancel()
	result := fetcher.client.exchange(session, ticketChangedTriggerOperation, deskRequest{
		method: http.MethodGet, path: "/tickets/search", query: fetcher.searchParameters(position),
	})
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRateLimited:
		// Zoho Desk meters API credits per organization, so the kit pauses every binding of the connection.
		return ticketPollPage{}, pollingtrigger.MarkRateLimited(errors.New(result.failure.Message), result.retryAfter)
	case exchangeRejected:
		if isTicketCursorRejected(result) {
			return ticketPollPage{}, pollingtrigger.MarkCursorInvalid(errors.New(result.failure.Message))
		}
		return ticketPollPage{}, errors.New(result.failure.Message)
	default:
		return ticketPollPage{}, errors.New(result.failure.Message)
	}
	return decodeTicketPollPage(result.response)
}

func (fetcher *ticketChangeFetcher) searchParameters(position ticketSearchPosition) url.Values {
	upper := fetcher.client.now().UTC()
	if upper.Before(position.floor) {
		upper = position.floor
	}
	parameters := url.Values{
		"modifiedTimeRange": {formatZohoTimestamp(position.floor) + "," + formatZohoTimestamp(upper)},
		"sortBy":            {"modifiedTime"},
		"from":              {strconv.Itoa(position.from)},
		"limit":             {strconv.Itoa(ticketPollPageLimit)},
	}
	if fetcher.departmentID != "" {
		parameters.Set("departmentId", fetcher.departmentID)
	}
	return parameters
}

// isTicketCursorRejected reports a 400 or 422 naming the cursor-derived modifiedTimeRange or from.
func isTicketCursorRejected(result deskExchange) bool {
	status := result.response.statusCode
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		return false
	}
	for _, fieldError := range result.errorSummary.fieldErrors {
		if strings.HasPrefix(fieldError, "/modifiedTimeRange") || strings.HasPrefix(fieldError, "/from") {
			return true
		}
	}
	return false
}

// decodeTicketPollPage reads a {data, count} search page in Zoho Desk's order; a 204 is an empty page.
// A ticket listed twice keeps its newest version rather than stalling the poll.
func decodeTicketPollPage(response deskResponse) (ticketPollPage, error) {
	page := ticketPollPage{serverTime: responseServerTime(response)}
	if response.statusCode == http.StatusNoContent {
		return page, nil
	}
	var wire searchPageWire
	if err := json.Unmarshal(response.body, &wire); err != nil || wire.Data == nil {
		return ticketPollPage{}, errors.New("Zoho Desk returned an invalid ticket search page")
	}
	if len(*wire.Data) > ticketPollPageLimit {
		return ticketPollPage{}, errors.New("Zoho Desk ticket search page holds more tickets than requested")
	}
	page.received = len(*wire.Data)
	page.tickets = make([]Ticket, 0, page.received)
	positions := make(map[string]int, page.received)
	for index, item := range *wire.Data {
		ticket, err := decodeTicketWire(item)
		if err != nil {
			return ticketPollPage{}, fmt.Errorf("Zoho Desk ticket search result %d: %w", index, err)
		}
		ticket.DescriptionHTML, ticket.IsDescriptionTruncated = "", false
		page.last = ticket.ModifiedAt
		if position, isListed := positions[ticket.ID]; isListed {
			if ticket.ModifiedAt.After(page.tickets[position].ModifiedAt) {
				page.tickets[position] = ticket
			}
			continue
		}
		positions[ticket.ID] = len(page.tickets)
		page.tickets = append(page.tickets, ticket)
	}
	return page, nil
}

// responseServerTime is the response's Date header in UTC, or zero when it is absent or malformed.
func responseServerTime(response deskResponse) time.Time {
	if response.header == nil {
		return time.Time{}
	}
	serverTime, err := http.ParseTime(response.header.Get("Date"))
	if err != nil {
		return time.Time{}
	}
	return serverTime.UTC()
}

// isServerClockSkewed reports skew beyond the header's one-second precision but under maximumServerClockSkew.
func isServerClockSkewed(serverTime time.Time, now time.Time) bool {
	if serverTime.IsZero() {
		return false
	}
	skew := serverTime.Sub(now).Abs()
	return skew > time.Second && skew < maximumServerClockSkew
}

func applyTicketVersions(snapshot *projectconfig.KeyedSnapshot, tickets []Ticket) (*projectconfig.KeyedSnapshot, error) {
	versions := make([]pollingtrigger.KeyedItem, 0, len(tickets))
	for _, ticket := range tickets {
		versions = append(versions, ticketVersion{ticket: ticket})
	}
	return pollingtrigger.ApplyKeyedSnapshotChanges(snapshot, versions, nil, ticketPollSnapshotOptions)
}

func findTicketVersion(snapshot *projectconfig.KeyedSnapshot, ticketID string) *projectconfig.KeyedSnapshotEntry {
	if snapshot == nil {
		return nil
	}
	index, isFound := slices.BinarySearchFunc(snapshot.Entries, ticketID, func(entry projectconfig.KeyedSnapshotEntry, key string) int {
		return strings.Compare(entry.Key, key)
	})
	if !isFound {
		return nil
	}
	return &snapshot.Entries[index]
}

func (fetcher *ticketChangeFetcher) decodeCursor(token string) (ticketSearchPosition, error) {
	var stored ticketChangeCursor
	if err := json.Unmarshal([]byte(token), &stored); err != nil {
		return ticketSearchPosition{}, errTicketCursorUndecodable
	}
	if stored.OrgID != fetcher.client.organizationID || stored.DepartmentID != fetcher.departmentID {
		return ticketSearchPosition{}, errors.New("Zoho Desk ticket cursor belongs to another organization or department")
	}
	sweepStart, sweepStartErr := time.Parse(time.RFC3339Nano, stored.SweepStart)
	floor, floorErr := time.Parse(time.RFC3339Nano, stored.Floor)
	highWater, highWaterErr := time.Parse(time.RFC3339Nano, stored.HighWater)
	if sweepStartErr != nil || floorErr != nil || highWaterErr != nil || stored.From < 0 || stored.From > ticketPollMaximumFrom {
		return ticketSearchPosition{}, errTicketCursorUndecodable
	}
	return ticketSearchPosition{sweepStart: sweepStart.UTC(), floor: floor.UTC(), from: stored.From, highWater: highWater.UTC()}, nil
}

func (fetcher *ticketChangeFetcher) encodeCursor(position ticketSearchPosition, snapshot *projectconfig.KeyedSnapshot) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(ticketChangeCursor{
		OrgID: fetcher.client.organizationID, DepartmentID: fetcher.departmentID,
		SweepStart: formatZohoTimestamp(position.sweepStart), Floor: formatZohoTimestamp(position.floor), From: position.from, HighWater: formatZohoTimestamp(position.highWater),
	})
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Zoho Desk ticket cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded), Snapshot: snapshot}, nil
}
