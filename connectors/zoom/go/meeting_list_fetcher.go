// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom

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
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// meetingListCursorVersion versions the committed cursor token; another value records a new baseline.
	meetingListCursorVersion = 1
	// maximumMeetingListPages covers the snapshot bound at Zoom's largest page size, plus one page.
	maximumMeetingListPages = pollingtrigger.DefaultKeyedSnapshotEntries/maximumPageSize + 2
	// meetingChangesPerPage keeps one commit small; further changes follow in the same poll.
	meetingChangesPerPage = 100
)

// meetingSnapshotOptions keeps each meeting's last listed state, so updates and removals carry it.
var meetingSnapshotOptions = pollingtrigger.KeyedSnapshotOptions{KeepValues: true}

// meetingListFetcher diffs full meeting listings; Zoom has no change feed, so the snapshot is the cursor.
type meetingListFetcher struct {
	client        *Client
	call          sdkgo.Call
	configuration MeetingChangedTriggerConfiguration
}

// meetingListCursor is the committed token. HostID is the Zoom user whose meetings the snapshot holds.
// Sequence counts the commits that carried at least one difference; a scheduled change's event ID
// includes it, so a meeting that leaves the list and returns with the same content, such as one
// recovered from Zoom's Recently Deleted list, gets a new ID instead of its first scheduled ID.
type meetingListCursor struct {
	Version  int    `json:"version"`
	HostID   string `json:"hostId,omitempty"`
	Sequence int64  `json:"sequence,omitempty"`
}

// meetingListing is one complete listing, one entry per meeting ID.
type meetingListing struct {
	meetings []listedMeeting
	hostIDs  map[string]bool
}

// meetingSnapshotChange is one difference between the stored snapshot and a listing.
type meetingSnapshotChange struct {
	kind     MeetingChangeKind
	key      string
	previous *projectconfig.KeyedSnapshotEntry
	current  *listedMeeting
}

func newMeetingListFetcher(client *Client, connection sdkgo.ConnectionRef, configuration MeetingChangedTriggerConfiguration) *meetingListFetcher {
	return &meetingListFetcher{
		client: client, configuration: configuration,
		call: pollingtrigger.NewTriggerCredentialCall(connection, MeetingChangedTriggerDefinition.Trigger),
	}
}

// SeedCursor lists every scheduled meeting and keeps the listing as the baseline without delivering it.
func (fetcher *meetingListFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	listing, err := fetcher.listAllMeetings(ctx)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	diff, err := pollingtrigger.DiffKeyedSnapshot(nil, listing.keyedItems(), meetingSnapshotOptions)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("Zoom meeting baseline: %w", err)
	}
	return encodeMeetingListCursor(meetingListCursor{Version: meetingListCursorVersion, HostID: listing.soleHostID()}, diff.Next)
}

// FetchChanges returns up to meetingChangesPerPage snapshot differences; a foreign or undecodable cursor reseeds.
func (fetcher *meetingListFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[MeetingChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[MeetingChangedEvent]]
	var position meetingListCursor
	if err := json.Unmarshal([]byte(cursor.Token), &position); err != nil || position.Version != meetingListCursorVersion || cursor.Snapshot == nil {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Zoom meeting list cursor is not decodable"))
	}
	listing, err := fetcher.listAllMeetings(ctx)
	if err != nil {
		return page, err
	}
	if position.HostID != "" && len(listing.meetings) > 0 && !listing.hostIDs[position.HostID] {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Zoom meeting list belongs to another Zoom user than the stored snapshot"))
	}
	if position.HostID == "" {
		position.HostID = listing.soleHostID()
	}
	diff, err := pollingtrigger.DiffKeyedSnapshot(cursor.Snapshot, listing.keyedItems(), meetingSnapshotOptions)
	if err != nil {
		return page, fmt.Errorf("Zoom meeting snapshot: %w", err)
	}
	changes := orderedMeetingChanges(diff, listing)
	next := diff.Next
	sequence := position.Sequence
	if len(changes) > 0 {
		position.Sequence++
	}
	if len(changes) > meetingChangesPerPage {
		changes, page.HasMorePages = changes[:meetingChangesPerPage], true
		if next, err = applyMeetingChanges(cursor.Snapshot, changes); err != nil {
			return page, err
		}
	}
	observedAt := fetcher.client.now()
	for _, change := range changes {
		if !fetcher.configuration.delivers(change.kind) {
			continue
		}
		event, err := change.triggerEvent(sequence, observedAt)
		if err != nil {
			return page, err
		}
		page.Changes = append(page.Changes, event)
	}
	page.Next, err = encodeMeetingListCursor(position, next)
	return page, err
}

// listAllMeetings restarts once after a 400 on a later page: Zoom expires page tokens after 15 minutes.
func (fetcher *meetingListFetcher) listAllMeetings(ctx context.Context) (meetingListing, error) {
	for restart := 0; ; restart++ {
		listing, isPageTokenRejected, err := fetcher.doListAllMeetings(ctx)
		if !isPageTokenRejected || restart > 0 {
			return listing, err
		}
	}
}

func (fetcher *meetingListFetcher) doListAllMeetings(ctx context.Context) (meetingListing, bool, error) {
	byKey := map[string]listedMeeting{}
	listing := meetingListing{hostIDs: map[string]bool{}}
	requestedPageTokens := map[string]bool{}
	pageToken := ""
	for pageNumber := 0; pageNumber < maximumMeetingListPages; pageNumber++ {
		listed, isPageTokenRejected, err := fetcher.listMeetingPage(ctx, pageToken)
		if err != nil {
			return meetingListing{}, isPageTokenRejected, err
		}
		for _, summary := range listed.Meetings {
			meeting, err := newListedMeeting(summary)
			if err != nil {
				return meetingListing{}, false, err
			}
			key := meeting.SnapshotKey()
			// Zoom can list a meeting twice, such as a recurring meeting's occurrences or a meeting that
			// shifts across pages; the earliest listed start wins, then the smaller hash, so any order agrees
			// and a recurring meeting tracks its next occurrence.
			if existing, isListed := byKey[key]; !isListed || meeting.listsBefore(existing) {
				byKey[key] = meeting
			}
			if summary.HostID != "" {
				listing.hostIDs[summary.HostID] = true
			}
		}
		if listed.NextPageToken == "" {
			for _, meeting := range byKey {
				listing.meetings = append(listing.meetings, meeting)
			}
			sort.Slice(listing.meetings, func(i, j int) bool { return listing.meetings[i].SnapshotKey() < listing.meetings[j].SnapshotKey() })
			return listing, false, nil
		}
		if requestedPageTokens[listed.NextPageToken] {
			return meetingListing{}, false, errors.New("Zoom meeting list repeated a page token")
		}
		requestedPageTokens[listed.NextPageToken] = true
		pageToken = listed.NextPageToken
	}
	return meetingListing{}, false, fmt.Errorf("Zoom meeting list exceeded %d pages", maximumMeetingListPages)
}

// listMeetingPage sends one List meetings request and reports whether Zoom rejected the page token.
func (fetcher *meetingListFetcher) listMeetingPage(ctx context.Context, pageToken string) (MeetingPage, bool, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return MeetingPage{}, false, err
	}
	credentials, err := sdkgo.ResolveCredential(ctx, fetcher.client.credentials, fetcher.call, fetcher.client.refreshDriver)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return MeetingPage{}, false, errors.New("Zoom connection requires reauthorization; reconnect the connection")
	case err != nil || validateResolvedCredentials(credentials) != nil:
		return MeetingPage{}, false, errors.New("Zoom Trigger credentials are unavailable")
	}
	query := url.Values{"type": {string(MeetingListScheduled)}, "page_size": {strconv.Itoa(maximumPageSize)}}
	if pageToken != "" {
		query.Set("next_page_token", pageToken)
	}
	response, _, err := fetcher.client.exchangeWithContext(fetcher.call, ctx, &credentials,
		providerRequest{method: http.MethodGet, path: currentUserPath + "/meetings", query: query}, false)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return MeetingPage{}, false, ctxErr
		}
		return MeetingPage{}, false, errors.New("Zoom meeting list request could not reach Zoom")
	}
	if pageToken != "" && response.statusCode == http.StatusBadRequest {
		return MeetingPage{}, true, errors.New("Zoom rejected the meeting list page token with HTTP 400")
	}
	classification := fetcher.client.classifyReadResponse(listMeetingsOperationID, response)
	if classification.outcome != statusOutcomeUsable {
		failure := errors.New("Zoom meeting list request: " + classification.failure.Message)
		if response.statusCode == http.StatusTooManyRequests {
			// The daily-limit 429 is a rejection for a Step; a poller waits Retry-After, or backs off without it.
			return MeetingPage{}, false, pollingtrigger.MarkRateLimited(failure,
				providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), fetcher.client.now()))
		}
		return MeetingPage{}, false, failure
	}
	listed, err := decodeMeetingPage(response.body)
	if err != nil {
		return MeetingPage{}, false, errors.New("Zoom returned an unusable meeting page")
	}
	return listed, false, nil
}

func (listing meetingListing) keyedItems() []pollingtrigger.KeyedItem {
	items := make([]pollingtrigger.KeyedItem, 0, len(listing.meetings))
	for _, meeting := range listing.meetings {
		items = append(items, meeting)
	}
	return items
}

func (listing meetingListing) find(key string) *listedMeeting {
	index := sort.Search(len(listing.meetings), func(i int) bool { return listing.meetings[i].SnapshotKey() >= key })
	if index < len(listing.meetings) && listing.meetings[index].SnapshotKey() == key {
		return &listing.meetings[index]
	}
	return nil
}

// soleHostID returns the host when every listed meeting names the same one, and "" otherwise.
func (listing meetingListing) soleHostID() string {
	if len(listing.hostIDs) != 1 {
		return ""
	}
	for hostID := range listing.hostIDs {
		return hostID
	}
	return ""
}

// orderedMeetingChanges merges the diff's sorted lists into one list in meeting key order.
func orderedMeetingChanges(diff pollingtrigger.KeyedSnapshotDiff, listing meetingListing) []meetingSnapshotChange {
	changes := make([]meetingSnapshotChange, 0, len(diff.Added)+len(diff.Changed)+len(diff.Removed))
	for _, added := range diff.Added {
		changes = append(changes, meetingSnapshotChange{kind: MeetingChangeScheduled, key: added.Key, current: listing.find(added.Key)})
	}
	for index := range diff.Changed {
		changed := diff.Changed[index]
		changes = append(changes, meetingSnapshotChange{kind: MeetingChangeUpdated, key: changed.Current.Key, previous: &changed.Previous, current: listing.find(changed.Current.Key)})
	}
	for index := range diff.Removed {
		removed := diff.Removed[index]
		changes = append(changes, meetingSnapshotChange{kind: MeetingChangeRemoved, key: removed.Key, previous: &removed})
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].key < changes[j].key })
	return changes
}

// applyMeetingChanges advances the stored snapshot by only the changes this page returns.
func applyMeetingChanges(previous *projectconfig.KeyedSnapshot, changes []meetingSnapshotChange) (*projectconfig.KeyedSnapshot, error) {
	var upserted []pollingtrigger.KeyedItem
	var removedKeys []string
	for _, change := range changes {
		if change.kind == MeetingChangeRemoved {
			removedKeys = append(removedKeys, change.key)
			continue
		}
		upserted = append(upserted, *change.current)
	}
	next, err := pollingtrigger.ApplyKeyedSnapshotChanges(previous, upserted, removedKeys, meetingSnapshotOptions)
	if err != nil {
		return nil, fmt.Errorf("Zoom meeting snapshot: %w", err)
	}
	return next, nil
}

func (change meetingSnapshotChange) triggerEvent(sequence int64, observedAt time.Time) (sdkgo.TriggerEvent[MeetingChangedEvent], error) {
	payload := MeetingChangedEvent{ChangeKind: change.kind}
	currentHash := ""
	if change.current != nil {
		payload.Meeting, currentHash = change.current.summary, change.current.SnapshotHash()
	}
	if change.previous != nil {
		previous, err := decodeTrackedMeeting(*change.previous)
		if err != nil {
			return sdkgo.TriggerEvent[MeetingChangedEvent]{}, err
		}
		if change.kind == MeetingChangeRemoved {
			payload.Meeting = previous
		} else {
			payload.PreviousMeeting = &previous
		}
	}
	return newMeetingChangedTriggerEvent(change.kind, change.key, change.previous, currentHash, sequence, payload, observedAt), nil
}

func encodeMeetingListCursor(position meetingListCursor, snapshot *projectconfig.KeyedSnapshot) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Zoom meeting list cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded), Snapshot: snapshot}, nil
}
