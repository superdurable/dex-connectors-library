// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	// TicketChangePageSize is the per_page of each ticketChanged list request, Freshdesk's maximum of 100.
	TicketChangePageSize = 100
	// MaxTicketListPage is the last page Freshdesk serves for List All Tickets (30,000 tickets).
	MaxTicketListPage = 300
	// TicketChangeOverlap is how far before the committed updated_at high-water mark each poll window
	// starts, so a ticket whose update commits a little after its timestamp is still read. Tickets already
	// delivered in the window are recognized by ID and updated_at and not delivered again.
	TicketChangeOverlap = time.Minute
	// maximumDeliveredTicketKeys bounds the cursor's dedupe set under the 64 KiB cursor token limit; past it
	// the oldest keys drop and the cursor floor moves past them, so they are not reread.
	maximumDeliveredTicketKeys = 1500
	// maximumSeedPages bounds the seed listing of tickets changed within the overlap before the baseline.
	maximumSeedPages = 15
	// maximumClockSkew is the local-to-Freshdesk clock difference above which the seed uses Freshdesk's clock.
	maximumClockSkew = 2 * time.Second

	ticketChangedOperation = "ticketChanged"
	ticketListTimeLayout   = "2006-01-02T15:04:05Z"
)

// ticketChangeFetcher reads Freshdesk's List All Tickets in updated_at order for the poll Trigger kit.
type ticketChangeFetcher struct {
	client        *Client
	call          sdkgo.Call
	configuration TicketChangedTriggerConfiguration
}

// ticketChangeCursor is the committed Trigger cursor. UpdatedSince is the highest updated_at read so far.
// While a backlog drains, WindowStart is the last read updated_at and Page is zero; Page 2 or more pages
// on from WindowStart only while more than a page of tickets share that one second. Floor is the earliest
// a later window starts: Freshdesk's time of the last complete read less the overlap and request timeout,
// or the oldest second kept when the dedupe set overflowed. Delivered holds "<id>@<updated_at Unix>" keys
// inside the window.
type ticketChangeCursor struct {
	UpdatedSince string   `json:"updatedSince"`
	WindowStart  string   `json:"windowStart,omitempty"`
	Page         int      `json:"page,omitempty"`
	Floor        string   `json:"floor,omitempty"`
	Delivered    []string `json:"delivered,omitempty"`
}

// ticketListPage is one decoded List All Tickets response.
type ticketListPage struct {
	tickets     []Ticket
	hasNextPage bool
	serverTime  time.Time
}

func newTicketChangeFetcher(client *Client, connection sdkgo.ConnectionRef, configuration TicketChangedTriggerConfiguration) *ticketChangeFetcher {
	return &ticketChangeFetcher{
		client:        client,
		call:          pollingtrigger.NewTriggerCredentialCall(connection, TicketChangedTriggerDefinition.Trigger),
		configuration: configuration,
	}
}

// SeedCursor records the baseline: the high-water mark is Freshdesk's current time from the response Date
// header, or the local clock without one, and tickets changed within TicketChangeOverlap before it are
// remembered as delivered, so no existing ticket is reported.
func (fetcher *ticketChangeFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	baseline := fetcher.client.now().UTC().Truncate(time.Second)
	seeded, err := fetcher.listSeedWindow(ctx, baseline)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	// A skewed local clock would put the window in the wrong place, so list again on Freshdesk's clock.
	if !seeded.serverTime.IsZero() && (seeded.serverTime.Sub(baseline) > maximumClockSkew || baseline.Sub(seeded.serverTime) > maximumClockSkew) {
		if seeded, err = fetcher.listSeedWindow(ctx, seeded.serverTime); err != nil {
			return pollingtrigger.Cursor{}, err
		}
	}
	return encodeTicketChangeCursor(seeded.cursor)
}

// seededTicketWindow is a baseline cursor and the Freshdesk time its first response reported.
type seededTicketWindow struct {
	cursor     ticketChangeCursor
	serverTime time.Time
}

// listSeedWindow remembers every ticket changed within TicketChangeOverlap before baseline.
func (fetcher *ticketChangeFetcher) listSeedWindow(ctx context.Context, baseline time.Time) (seededTicketWindow, error) {
	var seeded seededTicketWindow
	since := baseline
	for page := 1; page <= maximumSeedPages; page++ {
		listed, err := fetcher.listTicketPage(ctx, baseline.Add(-TicketChangeOverlap), page)
		if err != nil {
			return seeded, err
		}
		if page == 1 {
			seeded.serverTime = listed.serverTime
		}
		for _, ticket := range listed.tickets {
			seeded.cursor.Delivered = append(seeded.cursor.Delivered, deliveredTicketKey(ticket))
			if ticket.UpdatedAt.After(since) {
				since = ticket.UpdatedAt
			}
		}
		if !listed.hasNextPage {
			break
		}
	}
	seeded.cursor.UpdatedSince = since.Format(ticketListTimeLayout)
	var floor time.Time
	seeded.cursor.Delivered, floor = pruneDeliveredTicketKeys(seeded.cursor.Delivered, since.Add(-TicketChangeOverlap))
	seeded.cursor.Floor = formatTicketCursorFloor(floor, since)
	return seeded, nil
}

// FetchChanges reads one page of tickets updated since the cursor's window start, oldest first. An
// undecodable cursor and a Freshdesk 400 for the cursor's query are marked invalid, so the kit records a
// new baseline; a 429 pauses the connection for Retry-After.
func (fetcher *ticketChangeFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[TicketChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[TicketChangedEvent]]
	decoded, err := decodeTicketChangeCursor(cursor)
	if err != nil {
		return page, pollingtrigger.MarkCursorInvalid(err)
	}
	listed, err := fetcher.listTicketPage(ctx, decoded.windowStart, decoded.pageNumber)
	if err != nil {
		return page, err
	}
	delivered := make(map[string]bool, len(decoded.position.Delivered))
	deliveredTicketIDs := make(map[string]bool, len(decoded.position.Delivered))
	for _, key := range decoded.position.Delivered {
		delivered[key] = true
		deliveredTicketIDs[strings.SplitN(key, "@", 2)[0]] = true
	}
	// A backlog window starts at the last second read, so creation is judged from the committed position.
	createdFloor := decoded.since.Add(-TicketChangeOverlap)
	if decoded.windowStart.Before(createdFloor) {
		createdFloor = decoded.windowStart
	}
	next := ticketChangeCursor{Delivered: decoded.position.Delivered}
	highWater := decoded.since
	for _, ticket := range listed.tickets {
		key := deliveredTicketKey(ticket)
		if delivered[key] || ticket.UpdatedAt.Before(decoded.windowStart) {
			continue
		}
		kind := TicketChangeUpdated
		// A ticket created inside the window that the binding never delivered is new to the binding.
		if !ticket.CreatedAt.Before(createdFloor) && !deliveredTicketIDs[strconv.FormatInt(ticket.ID, 10)] {
			kind = TicketChangeCreated
		}
		delivered[key] = true
		next.Delivered = append(next.Delivered, key)
		if ticket.UpdatedAt.After(highWater) {
			highWater = ticket.UpdatedAt
		}
		if fetcher.configuration.DeliversChangeKind(kind) {
			page.Changes = append(page.Changes, newTicketChangedTriggerEvent(kind, ticket))
		}
	}
	next.UpdatedSince = highWater.Format(ticketListTimeLayout)
	floor := decoded.floor
	nextWindowStart := steadyTicketWindowStart(highWater, floor)
	isLastServedPage := decoded.pageNumber >= MaxTicketListPage && len(listed.tickets) == TicketChangePageSize
	switch {
	case isLastServedPage:
		// Freshdesk serves no page past 300 and names none, so the rest of this one second is skipped.
		page.HasMorePages = true
		floor = decoded.windowStart.Add(time.Second)
		nextWindowStart = steadyTicketWindowStart(highWater, floor)
	case !listed.hasNextPage || len(listed.tickets) == 0:
	case listed.tickets[len(listed.tickets)-1].UpdatedAt.After(decoded.windowStart):
		// Offsets shift when a listed ticket changes, so restart at the last second read instead of paging.
		page.HasMorePages = true
		nextWindowStart = listed.tickets[len(listed.tickets)-1].UpdatedAt
		next.WindowStart = nextWindowStart.Format(ticketListTimeLayout)
	default:
		// More than a page shares one second, so only an offset makes progress.
		page.HasMorePages = true
		nextWindowStart = decoded.windowStart
		next.WindowStart, next.Page = decoded.windowStart.Format(ticketListTimeLayout), decoded.pageNumber+1
	}
	if !page.HasMorePages && !listed.serverTime.IsZero() {
		// Every update older than this has committed and been read, so a quiet helpdesk stops rereading.
		settled := listed.serverTime.Add(-TicketChangeOverlap - defaultRequestTimeout)
		if settled.After(floor) {
			floor = settled
		}
		nextWindowStart = steadyTicketWindowStart(highWater, floor)
	}
	pruneFrom := steadyTicketWindowStart(highWater, floor)
	if nextWindowStart.Before(pruneFrom) {
		pruneFrom = nextWindowStart
	}
	var overflowFloor time.Time
	next.Delivered, overflowFloor = pruneDeliveredTicketKeys(next.Delivered, pruneFrom)
	if overflowFloor.After(floor) {
		floor = overflowFloor
	}
	next.Floor = formatTicketCursorFloor(floor, highWater)
	page.Next, err = encodeTicketChangeCursor(next)
	return page, err
}

// steadyTicketWindowStart is where the next poll starts once no page remains: TicketChangeOverlap before
// highWater, or floor when it is later.
func steadyTicketWindowStart(highWater time.Time, floor time.Time) time.Time {
	windowStart := highWater.Add(-TicketChangeOverlap)
	if floor.After(windowStart) {
		return floor
	}
	return windowStart
}

// formatTicketCursorFloor keeps floor only while it still raises the steady window start.
func formatTicketCursorFloor(floor time.Time, highWater time.Time) string {
	if !floor.After(highWater.Add(-TicketChangeOverlap)) {
		return ""
	}
	return floor.Format(ticketListTimeLayout)
}

// listTicketPage sends one GET /api/v2/tickets request for tickets updated at or after windowStart.
func (fetcher *ticketChangeFetcher) listTicketPage(ctx context.Context, windowStart time.Time, pageNumber int) (ticketListPage, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return ticketListPage{}, err
	}
	credentials, err := fetcher.resolveCredentials(ctx)
	if err != nil {
		return ticketListPage{}, err
	}
	query := url.Values{
		"updated_since": {windowStart.UTC().Format(ticketListTimeLayout)},
		"order_by":      {"updated_at"}, "order_type": {"asc"},
		"per_page": {strconv.Itoa(TicketChangePageSize)}, "page": {strconv.Itoa(pageNumber)},
	}
	result := fetcher.client.exchangeWithContext(ctx, credentials, ticketChangedOperation, freshdeskRequest{method: http.MethodGet, path: "/tickets", query: query})
	failure := errors.New("Freshdesk ticket list request: " + result.failure.Message)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRateLimited:
		return ticketListPage{}, pollingtrigger.MarkRateLimited(failure, result.retryAfter)
	case exchangeRejected:
		if result.response.statusCode == http.StatusBadRequest {
			return ticketListPage{}, pollingtrigger.MarkCursorInvalid(failure)
		}
		return ticketListPage{}, failure
	default:
		return ticketListPage{}, failure
	}
	tickets, err := decodeTicketList(result.response.body)
	if err != nil {
		return ticketListPage{}, fmt.Errorf("Freshdesk ticket list page is invalid: %w", err)
	}
	listed := ticketListPage{tickets: tickets, hasNextPage: hasNextPage(result.response.header)}
	if serverTime, err := http.ParseTime(result.response.header.Get("Date")); err == nil {
		listed.serverTime = serverTime.UTC().Truncate(time.Second)
	}
	return listed, nil
}

// resolveCredentials reads the connection's API key with the poller's context when the provider supports it.
func (fetcher *ticketChangeFetcher) resolveCredentials(ctx context.Context) (Credentials, error) {
	var credentials Credentials
	var err error
	if contextProvider, ok := fetcher.client.credentials.(sdkgo.ContextCredentialProvider[Credentials]); ok {
		credentials, err = contextProvider.ResolveContext(ctx, fetcher.call)
	} else {
		credentials, err = fetcher.client.credentials.Resolve(fetcher.call)
	}
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return Credentials{}, errors.New("Freshdesk ticketChanged credentials are unavailable")
	}
	return credentials, nil
}

func decodeTicketList(body []byte) ([]Ticket, error) {
	var wires []freshdeskTicketWire
	if err := json.Unmarshal(body, &wires); err != nil {
		return nil, errors.New("the response is not a ticket array")
	}
	if len(wires) > TicketChangePageSize {
		return nil, fmt.Errorf("the page has %d tickets, more than %d", len(wires), TicketChangePageSize)
	}
	tickets := make([]Ticket, 0, len(wires))
	for _, wire := range wires {
		ticket, err := decodeTicketWire(wire)
		if err != nil {
			return nil, err
		}
		tickets = append(tickets, ticket)
	}
	return tickets, nil
}

// decodedTicketChangeCursor is a validated cursor and the request it selects.
type decodedTicketChangeCursor struct {
	position    ticketChangeCursor
	since       time.Time
	floor       time.Time
	windowStart time.Time
	pageNumber  int
}

func decodeTicketChangeCursor(cursor pollingtrigger.Cursor) (decodedTicketChangeCursor, error) {
	var decoded decodedTicketChangeCursor
	if err := json.Unmarshal([]byte(cursor.Token), &decoded.position); err != nil {
		return decoded, errTicketChangeCursorInvalid
	}
	position := decoded.position
	var err error
	if decoded.since, err = time.Parse(ticketListTimeLayout, position.UpdatedSince); err != nil {
		return decoded, errTicketChangeCursorInvalid
	}
	if position.Floor != "" {
		decoded.floor, err = time.Parse(ticketListTimeLayout, position.Floor)
		if err != nil {
			return decoded, errTicketChangeCursorInvalid
		}
	}
	decoded.windowStart, decoded.pageNumber = steadyTicketWindowStart(decoded.since, decoded.floor), 1
	if position.Page == 0 && position.WindowStart == "" {
		return decoded, nil
	}
	windowStart, err := time.Parse(ticketListTimeLayout, position.WindowStart)
	if err != nil || windowStart.After(decoded.since) || position.Page == 1 || position.Page < 0 || position.Page > MaxTicketListPage {
		return decoded, errTicketChangeCursorInvalid
	}
	if position.Page == 0 {
		// A backlog window never starts before the floor.
		decoded.windowStart = windowStart
		if decoded.floor.After(windowStart) {
			decoded.windowStart = decoded.floor
		}
		return decoded, nil
	}
	// Offset paging repeats the exact query of its first page.
	decoded.windowStart, decoded.pageNumber = windowStart, position.Page
	return decoded, nil
}

func encodeTicketChangeCursor(position ticketChangeCursor) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Freshdesk ticketChanged cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}

func deliveredTicketKey(ticket Ticket) string {
	return strconv.FormatInt(ticket.ID, 10) + "@" + strconv.FormatInt(ticket.UpdatedAt.Unix(), 10)
}

// pruneDeliveredTicketKeys keeps keys updated at or after windowStart, newest maximumDeliveredTicketKeys.
// When the bound drops keys, floor is the earliest second whose keys are all kept, so a later window that
// starts there rereads no dropped version; otherwise floor is zero.
func pruneDeliveredTicketKeys(keys []string, windowStart time.Time) (pruned []string, floor time.Time) {
	type deliveredKey struct {
		key       string
		updatedAt int64
	}
	kept := make([]deliveredKey, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		parts := strings.SplitN(key, "@", 2)
		if len(parts) != 2 || seen[key] {
			continue
		}
		updatedAt, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || updatedAt < windowStart.Unix() {
			continue
		}
		seen[key] = true
		kept = append(kept, deliveredKey{key: key, updatedAt: updatedAt})
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].updatedAt < kept[j].updatedAt })
	if dropped := len(kept) - maximumDeliveredTicketKeys; dropped > 0 {
		floorSecond := kept[dropped].updatedAt
		if kept[dropped-1].updatedAt == floorSecond {
			// Part of this second was dropped, so the window starts after it and its kept keys go too.
			floorSecond++
		}
		floor = time.Unix(floorSecond, 0).UTC()
		kept = kept[dropped:]
		for len(kept) != 0 && kept[0].updatedAt < floorSecond {
			kept = kept[1:]
		}
	}
	if len(kept) == 0 {
		return nil, floor
	}
	pruned = make([]string, len(kept))
	for index, entry := range kept {
		pruned[index] = entry.key
	}
	return pruned, floor
}
