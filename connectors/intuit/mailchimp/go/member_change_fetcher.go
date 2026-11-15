// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

const (
	memberChangedOperation = "memberChanged"
	// memberPollPageSize stays below pollingtrigger.MaxChangesPerPage and Mailchimp's 1000-contact count.
	memberPollPageSize = 250
	// memberPollLookback rereads recent changes, because a change can become listable after a later one.
	memberPollLookback = 5 * time.Minute
	// maximumMemberPollOffset bounds tie paging of contacts that share one change second.
	maximumMemberPollOffset = 1_000_000
)

// memberSnapshotOptions keeps the most recently changed contacts; older ones are outside the lookback.
var memberSnapshotOptions = pollingtrigger.KeyedSnapshotOptions{EvictLeastRecentlySeen: true}

// memberChangeFetcher lists one audience's contacts by last change time for the poll Trigger kit.
type memberChangeFetcher struct {
	client        *Client
	call          sdkgo.Call
	configuration MemberChangedTriggerConfiguration
}

// memberPollCursor is the committed Trigger cursor. Times are RFC 3339 seconds in UTC, as Mailchimp lists them.
type memberPollCursor struct {
	AudienceID string `json:"audienceId"`
	// Baseline is the newest change second at seed time; contacts changed before it are never delivered, and
	// contacts changed in it are delivered unless the seed snapshot holds that version.
	Baseline string `json:"baseline,omitempty"`
	// Watermark is the newest change time any pass has read.
	Watermark string `json:"watermark,omitempty"`
	// PassSince and PassOffset continue a pass that spans pages; both are empty between passes.
	PassSince  string `json:"passSince,omitempty"`
	PassOffset int    `json:"passOffset,omitempty"`
}

func newMemberChangeFetcher(client *Client, connection sdkgo.ConnectionRef, configuration MemberChangedTriggerConfiguration) *memberChangeFetcher {
	return &memberChangeFetcher{
		client: client, configuration: configuration,
		call: pollingtrigger.NewTriggerCredentialCall(connection, MemberChangedTriggerDefinition.Trigger),
	}
}

// SeedCursor records the newest contact change time as the baseline and snapshots the contacts changed in
// that second, so a contact changed later in the same second is still delivered. An empty audience has no
// baseline. When a whole page shares the newest second, the baseline moves one second later and that
// second's later changes are not delivered.
func (fetcher *memberChangeFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	page, err := fetcher.listMembers(ctx, url.Values{
		"count": {strconv.Itoa(memberPollPageSize)}, "offset": {"0"}, "sort_field": {"last_changed"}, "sort_dir": {"DESC"},
		"fields": {memberListFields},
	}, memberPollPageSize)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	cursor := memberPollCursor{AudienceID: fetcher.configuration.AudienceID}
	var newest *time.Time
	var seeded []pollingtrigger.KeyedItem
	for _, member := range page.Members {
		if member.LastChangedAt == nil {
			continue
		}
		if newest == nil {
			newest = member.LastChangedAt
		}
		if !member.LastChangedAt.Equal(*newest) {
			break
		}
		seeded = append(seeded, memberSnapshotItem{audienceID: fetcher.configuration.AudienceID, member: member})
	}
	if newest == nil {
		return encodeMemberPollCursor(cursor, nil)
	}
	cursor.Baseline, cursor.Watermark = formatCursorTime(*newest), formatCursorTime(*newest)
	if len(seeded) == len(page.Members) && len(page.Members) == memberPollPageSize {
		// More contacts may share the newest second than one page holds, so none of that second is delivered.
		cursor.Baseline, seeded = formatCursorTime(newest.Add(time.Second)), nil
	}
	snapshot, err := pollingtrigger.ApplyKeyedSnapshotChanges(nil, seeded, nil, memberSnapshotOptions)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	return encodeMemberPollCursor(cursor, snapshot)
}

// FetchChanges reads one ascending page of changed contacts; the snapshot drops versions already delivered.
func (fetcher *memberChangeFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[MemberChangedEvent]], error) {
	position, err := fetcher.decodeCursor(cursor.Token)
	if err != nil {
		return pollingtrigger.ChangePage[sdkgo.TriggerEvent[MemberChangedEvent]]{}, pollingtrigger.MarkCursorInvalid(err)
	}
	since, err := position.passStart()
	if err != nil {
		return pollingtrigger.ChangePage[sdkgo.TriggerEvent[MemberChangedEvent]]{}, pollingtrigger.MarkCursorInvalid(err)
	}
	query := url.Values{
		"count": {strconv.Itoa(memberPollPageSize)}, "offset": {strconv.Itoa(position.PassOffset)},
		"sort_field": {"last_changed"}, "sort_dir": {"ASC"}, "fields": {memberListFields},
	}
	if since != nil {
		query.Set("since_last_changed", formatMailchimpTime(*since))
	}
	page, err := fetcher.listMembers(ctx, query, memberPollPageSize)
	if err != nil {
		return pollingtrigger.ChangePage[sdkgo.TriggerEvent[MemberChangedEvent]]{}, err
	}
	return fetcher.buildChangePage(position, since, page, cursor.Snapshot)
}

// buildChangePage delivers versions newer than the baseline and the snapshot, and advances the pass.
func (fetcher *memberChangeFetcher) buildChangePage(position memberPollCursor, since *time.Time, page ListMembersOutput, snapshot *projectconfig.KeyedSnapshot) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[MemberChangedEvent]], error) {
	baseline, _ := parseCursorTime(position.Baseline)
	watermark, _ := parseCursorTime(position.Watermark)
	var events []sdkgo.TriggerEvent[MemberChangedEvent]
	var upserted []pollingtrigger.KeyedItem
	listed := make([]Member, 0, len(page.Members))
	for _, member := range page.Members {
		if member.LastChangedAt == nil {
			continue // Without a change time a contact cannot be ordered; Mailchimp sets it on every write.
		}
		listed = append(listed, member)
		if watermark == nil || member.LastChangedAt.After(*watermark) {
			watermark = member.LastChangedAt
		}
		item := memberSnapshotItem{audienceID: fetcher.configuration.AudienceID, member: member}
		if baseline != nil && member.LastChangedAt.Before(*baseline) {
			continue
		}
		if stored := findSnapshotEntry(snapshot, item.SnapshotKey()); stored != nil && stored.Hash == item.SnapshotHash() {
			continue
		}
		upserted = append(upserted, item)
		if fetcher.configuration.accepts(member.Status) {
			events = append(events, newMemberChangedTriggerEvent(fetcher.configuration.AudienceID, member))
		}
	}
	nextSnapshot, err := pollingtrigger.ApplyKeyedSnapshotChanges(snapshot, upserted, nil, memberSnapshotOptions)
	if err != nil {
		return pollingtrigger.ChangePage[sdkgo.TriggerEvent[MemberChangedEvent]]{}, err
	}
	next := memberPollCursor{AudienceID: position.AudienceID, Baseline: position.Baseline}
	if watermark != nil {
		next.Watermark = formatCursorTime(*watermark)
	}
	hasMorePages := len(page.Members) == memberPollPageSize && position.PassOffset+len(page.Members) < page.TotalItems
	if hasMorePages {
		if next.PassSince, next.PassOffset, err = continuePass(since, position.PassOffset, page.Members, listed); err != nil {
			return pollingtrigger.ChangePage[sdkgo.TriggerEvent[MemberChangedEvent]]{}, err
		}
	}
	encoded, err := encodeMemberPollCursor(next, nextSnapshot)
	if err != nil {
		return pollingtrigger.ChangePage[sdkgo.TriggerEvent[MemberChangedEvent]]{}, err
	}
	return pollingtrigger.ChangePage[sdkgo.TriggerEvent[MemberChangedEvent]]{Changes: events, Next: encoded, HasMorePages: hasMorePages}, nil
}

// continuePass restarts one second before the page's last change, or advances by offset within one second.
func continuePass(since *time.Time, offset int, members []Member, listed []Member) (string, int, error) {
	if len(listed) != 0 {
		last := listed[len(listed)-1].LastChangedAt.Add(-time.Second)
		first := *listed[0].LastChangedAt
		if first.Before(*listed[len(listed)-1].LastChangedAt) && (since == nil || last.After(*since)) {
			return formatCursorTime(last), 0, nil
		}
	}
	if offset+len(members) > maximumMemberPollOffset {
		return "", 0, fmt.Errorf("more than %d Mailchimp contacts changed within one second", maximumMemberPollOffset)
	}
	passSince := ""
	if since != nil {
		passSince = formatCursorTime(*since)
	}
	return passSince, offset + len(members), nil
}

// passStart returns the pending pass's bound, or the lookback before the watermark, never earlier than one
// second before the baseline; nil lists every contact.
func (position memberPollCursor) passStart() (*time.Time, error) {
	if position.PassSince != "" || position.PassOffset != 0 {
		return parseCursorTime(position.PassSince)
	}
	watermark, err := parseCursorTime(position.Watermark)
	if err != nil || watermark == nil {
		return nil, err
	}
	start := watermark.Add(-memberPollLookback)
	if baseline, err := parseCursorTime(position.Baseline); err != nil {
		return nil, err
	} else if baseline != nil && start.Before(baseline.Add(-time.Second)) {
		// One second before the baseline lists the baseline second whether since_last_changed is exclusive or not.
		start = baseline.Add(-time.Second)
	}
	return &start, nil
}

func (fetcher *memberChangeFetcher) decodeCursor(token string) (memberPollCursor, error) {
	var position memberPollCursor
	if err := json.Unmarshal([]byte(token), &position); err != nil {
		return memberPollCursor{}, errors.New("Mailchimp memberChanged cursor is not valid JSON")
	}
	if position.AudienceID != fetcher.configuration.AudienceID {
		return memberPollCursor{}, errors.New("Mailchimp memberChanged cursor belongs to another audience")
	}
	for _, value := range []string{position.Baseline, position.Watermark, position.PassSince} {
		if _, err := parseCursorTime(value); err != nil {
			return memberPollCursor{}, err
		}
	}
	if position.PassOffset < 0 || position.PassOffset > maximumMemberPollOffset {
		return memberPollCursor{}, errors.New("Mailchimp memberChanged cursor offset is out of range")
	}
	return position, nil
}

// listMembers sends one GET /lists/{list_id}/members and classifies its failure for the kit.
func (fetcher *memberChangeFetcher) listMembers(ctx context.Context, query url.Values, pageSize int) (ListMembersOutput, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return ListMembersOutput{}, err
	}
	connection, failure := fetcher.client.resolveConnection(fetcher.call, memberChangedOperation)
	if failure != nil {
		return ListMembersOutput{}, errors.New(failure.Message)
	}
	result := fetcher.client.exchangeWithContext(ctx, connection, memberChangedOperation, mailchimpRequest{
		method: http.MethodGet, path: "/lists/" + fetcher.configuration.AudienceID + "/members", query: query,
	})
	switch {
	case result.outcome == exchangeSucceeded:
		offset, _ := strconv.Atoi(query.Get("offset"))
		page, err := decodeMemberPage(result.response.body, offset, pageSize)
		if err != nil {
			return ListMembersOutput{}, fmt.Errorf("Mailchimp returned an invalid page of contacts: %w", err)
		}
		return page, nil
	case result.outcome == exchangeRateLimited:
		return ListMembersOutput{}, pollingtrigger.MarkRateLimited(errors.New(result.failure.Message), result.retryAfter)
	case result.outcome == exchangeRejected && result.response.statusCode == http.StatusBadRequest:
		return ListMembersOutput{}, pollingtrigger.MarkCursorInvalid(errors.New(result.failure.Message))
	default:
		return ListMembersOutput{}, errors.New(result.failure.Message)
	}
}

func encodeMemberPollCursor(position memberPollCursor, snapshot *projectconfig.KeyedSnapshot) (pollingtrigger.Cursor, error) {
	token, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	return pollingtrigger.Cursor{Token: string(token), Snapshot: snapshot}, nil
}

func findSnapshotEntry(snapshot *projectconfig.KeyedSnapshot, key string) *projectconfig.KeyedSnapshotEntry {
	if snapshot == nil {
		return nil
	}
	// The kit stores entries sorted by key.
	index := sort.Search(len(snapshot.Entries), func(index int) bool { return snapshot.Entries[index].Key >= key })
	if index == len(snapshot.Entries) || snapshot.Entries[index].Key != key {
		return nil
	}
	return &snapshot.Entries[index]
}

func formatCursorTime(value time.Time) string { return value.UTC().Format(time.RFC3339) }

func parseCursorTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, errors.New("Mailchimp memberChanged cursor holds an invalid time")
	}
	parsed = parsed.UTC()
	return &parsed, nil
}
