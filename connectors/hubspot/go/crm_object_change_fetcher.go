// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hubspot

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	// crmObjectChangePageSize keeps one search response of full records well under the default response limit.
	crmObjectChangePageSize = 100
	// searchIndexLagWindow re-reads recent changes, because HubSpot indexes new and updated records after a delay.
	searchIndexLagWindow = 2 * time.Minute
	// maximumSeenRecordVersions keeps the cursor document well under the kit's 64 KiB token bound.
	maximumSeenRecordVersions = 1500
	crmObjectChangeOperation  = "crmObjectChanged"
)

// crmObjectChangeFetcher polls one object type through CRM search sorted by last-modified date.
type crmObjectChangeFetcher struct {
	client        *Client
	call          sdkgo.Call
	configuration CRMObjectChangedTriggerConfiguration
}

// crmObjectChangeCursor is the committed Trigger cursor; all times are Unix milliseconds.
type crmObjectChangeCursor struct {
	ObjectType ObjectType `json:"objectType"`
	// FloorMilliseconds is the baseline's newest change; nothing at or before it is delivered.
	FloorMilliseconds     int64 `json:"floorMs"`
	HighWaterMilliseconds int64 `json:"highWaterMs"`
	// AnchorMilliseconds is the GTE bound of the next search.
	AnchorMilliseconds int64 `json:"anchorMs"`
	// After is HubSpot's offset, set only while over a page of records share AnchorMilliseconds.
	After string `json:"after,omitempty"`
	// Seen maps each record delivered inside the lag window to its delivered version.
	Seen map[string]int64 `json:"seen,omitempty"`
	// DroppedThroughMilliseconds is the newest version pruned from Seen for capacity, not for age. The
	// lag-window re-read starts after it, so a burst larger than Seen is not delivered again every poll.
	DroppedThroughMilliseconds int64 `json:"droppedThroughMs,omitempty"`
}

func newCRMObjectChangeFetcher(client *Client, connection sdkgo.ConnectionRef, configuration CRMObjectChangedTriggerConfiguration) *crmObjectChangeFetcher {
	return &crmObjectChangeFetcher{
		client:        client,
		call:          pollingtrigger.NewTriggerCredentialCall(connection, CRMObjectChangedTriggerDefinition.Trigger),
		configuration: configuration,
	}
}

// SeedCursor reads the newest last-modified date, so every existing record becomes the baseline. An
// object type without records seeds at zero, so every later record is delivered.
func (fetcher *crmObjectChangeFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	objectType := fetcher.configuration.ObjectType
	newest := SearchObjectsInput{
		ObjectType: objectType, Properties: []string{objectType.lastModifiedProperty()}, Limit: 1,
		Sort: &SearchSort{PropertyName: objectType.lastModifiedProperty(), Direction: SortDirectionDescending},
	}
	page, err := fetcher.search(ctx, newest)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	var floor int64
	if len(page.Objects) > 0 {
		if floor, err = recordModifiedMilliseconds(page.Objects[0]); err != nil {
			return pollingtrigger.Cursor{}, err
		}
	}
	return encodeCRMObjectChangeCursor(crmObjectChangeCursor{
		ObjectType: objectType, FloorMilliseconds: floor, HighWaterMilliseconds: floor, AnchorMilliseconds: floor + 1,
	})
}

// FetchChanges reads one search page after cursor. An undecodable cursor, a cursor for another object
// type, and a paging offset HubSpot rejects are marked invalid, so the kit records a new baseline.
func (fetcher *crmObjectChangeFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[CRMObjectChangedEvent]], error) {
	var changes pollingtrigger.ChangePage[sdkgo.TriggerEvent[CRMObjectChangedEvent]]
	position, err := decodeCRMObjectChangeCursor(cursor.Token)
	if err != nil {
		return changes, pollingtrigger.MarkCursorInvalid(err)
	}
	if position.ObjectType != fetcher.configuration.ObjectType {
		return changes, pollingtrigger.MarkCursorInvalid(errors.New("HubSpot change cursor belongs to another object type"))
	}
	page, err := fetcher.search(ctx, newCRMObjectChangeSearch(fetcher.configuration, position.AnchorMilliseconds, position.After))
	if err != nil {
		if position.After != "" && isSearchRequestRejected(err) {
			return changes, pollingtrigger.MarkCursorInvalid(fmt.Errorf("HubSpot rejected the change search offset: %w", err))
		}
		return changes, err
	}
	next := position
	next.Seen = make(map[string]int64, len(position.Seen)+len(page.Objects))
	for objectID, modified := range position.Seen {
		next.Seen[objectID] = modified
	}
	pageNewest := position.AnchorMilliseconds
	for _, object := range page.Objects {
		modified, err := recordModifiedMilliseconds(object)
		if err != nil {
			return changes, err
		}
		pageNewest = max(pageNewest, modified)
		if delivered, isSeen := next.Seen[object.ID]; (isSeen && delivered >= modified) || modified <= position.FloorMilliseconds {
			continue
		}
		kind := CRMObjectChangeUpdated
		if object.CreatedAt.UnixMilli() > position.HighWaterMilliseconds {
			kind = CRMObjectChangeCreated
		}
		changes.Changes = append(changes.Changes, newCRMObjectChangedTriggerEvent(object, modified, kind))
		next.Seen[object.ID] = modified
		next.HighWaterMilliseconds = max(next.HighWaterMilliseconds, modified)
	}
	next.pruneSeen()
	switch {
	case page.NextAfter != "" && pageNewest > position.AnchorMilliseconds:
		// Re-anchoring beats offset paging: an edit moves a record to the end and would shift the offsets.
		next.AnchorMilliseconds, next.After, changes.HasMorePages = pageNewest, "", true
	case page.NextAfter != "":
		offset, _ := strconv.Atoi(page.NextAfter)
		if offset+crmObjectChangePageSize > MaximumSearchResults {
			return changes, pollingtrigger.MarkCursorInvalid(fmt.Errorf("more than %d HubSpot records share one last-modified time", MaximumSearchResults))
		}
		next.After, changes.HasMorePages = page.NextAfter, true
	default:
		next.AnchorMilliseconds, next.After = next.lagWindowStart(), ""
	}
	changes.Next, err = encodeCRMObjectChangeCursor(next)
	return changes, err
}

// newCRMObjectChangeSearch builds the ascending last-modified search the Trigger repeats.
func newCRMObjectChangeSearch(configuration CRMObjectChangedTriggerConfiguration, anchorMilliseconds int64, after string) SearchObjectsInput {
	modifiedProperty := configuration.ObjectType.lastModifiedProperty()
	properties := append(slices.Clone(configuration.Properties), "createdate", modifiedProperty)
	return SearchObjectsInput{
		ObjectType: configuration.ObjectType,
		FilterGroups: []SearchFilterGroup{{Filters: []SearchFilter{{
			PropertyName: modifiedProperty, Operator: FilterOperatorGreaterThanOrEqual, Value: strconv.FormatInt(anchorMilliseconds, 10),
		}}}},
		Sort:       &SearchSort{PropertyName: modifiedProperty, Direction: SortDirectionAscending},
		Properties: properties, Limit: crmObjectChangePageSize, After: after,
	}
}

// changeSearchError is one unsuccessful search exchange; its message carries no record data.
type changeSearchError struct{ outcome providerOutcome }

func (searchError *changeSearchError) Error() string {
	return "HubSpot change search: " + searchError.outcome.failure.Message
}

// search sends one CRM search; 429, 423, and the daily limit pause the whole connection.
func (fetcher *crmObjectChangeFetcher) search(ctx context.Context, input SearchObjectsInput) (ObjectPage, error) {
	body, err := input.searchRequestBody()
	if err != nil {
		return ObjectPage{}, crmObjectChangedTriggerConfigurationError(err)
	}
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return ObjectPage{}, err
	}
	request := providerRequest{method: http.MethodPost, path: objectsPath(input.ObjectType, "search"), body: body}
	response, outcome := fetcher.client.exchangeWithContext(ctx, fetcher.call, crmObjectChangeOperation, request, "")
	if outcome != nil {
		failure := &changeSearchError{outcome: *outcome}
		switch outcome.failure.Kind {
		case sdkgo.FailureRateLimit:
			return ObjectPage{}, pollingtrigger.MarkRateLimited(failure, outcome.retryAfter)
		case sdkgo.FailureQuotaExhausted:
			// The daily limit resets at midnight in the account's time zone; back off without a known wait.
			return ObjectPage{}, pollingtrigger.MarkRateLimited(failure, 0)
		}
		return ObjectPage{}, failure
	}
	page, err := decodeObjectPage(input.ObjectType, response.body)
	if err != nil {
		return ObjectPage{}, fmt.Errorf("HubSpot change search returned an invalid page: %w", err)
	}
	if len(page.Objects) > input.Limit {
		return ObjectPage{}, fmt.Errorf("HubSpot change search returned %d records, more than %d", len(page.Objects), input.Limit)
	}
	return page, nil
}

// isSearchRequestRejected reports HubSpot's 400 for a request, such as paging past 10,000 results.
func isSearchRequestRejected(err error) bool {
	var searchError *changeSearchError
	return errors.As(err, &searchError) && searchError.outcome.failure.Kind == sdkgo.FailureValidation
}

// lagWindowStart is where the next caught-up search starts: inside the lag window, never in the baseline,
// and after any version pruned from Seen for capacity.
func (position crmObjectChangeCursor) lagWindowStart() int64 {
	return max(position.FloorMilliseconds+1, position.HighWaterMilliseconds-searchIndexLagWindow.Milliseconds(),
		position.DroppedThroughMilliseconds+1)
}

// pruneSeen keeps the newest versions inside the lag window. When more remain than Seen holds, it drops the
// oldest and moves DroppedThroughMilliseconds past them, which narrows the lag window for that burst
// instead of delivering the dropped versions again on every poll.
func (position *crmObjectChangeCursor) pruneSeen() {
	windowStart := position.lagWindowStart()
	type seenVersion struct {
		objectID string
		modified int64
	}
	kept := make([]seenVersion, 0, len(position.Seen))
	for objectID, modified := range position.Seen {
		if modified >= windowStart {
			kept = append(kept, seenVersion{objectID: objectID, modified: modified})
		}
	}
	slices.SortFunc(kept, func(left, right seenVersion) int {
		if left.modified != right.modified {
			return cmp.Compare(right.modified, left.modified)
		}
		return compareDecimalIDs(left.objectID, right.objectID)
	})
	if len(kept) > maximumSeenRecordVersions {
		position.DroppedThroughMilliseconds = max(position.DroppedThroughMilliseconds, kept[maximumSeenRecordVersions].modified)
		kept = kept[:maximumSeenRecordVersions]
	}
	position.Seen = make(map[string]int64, len(kept))
	for _, version := range kept {
		position.Seen[version.objectID] = version.modified
	}
	if len(position.Seen) == 0 {
		position.Seen = nil
	}
}

func compareDecimalIDs(left string, right string) int {
	if len(left) != len(right) {
		return len(left) - len(right)
	}
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	}
	return 0
}

func encodeCRMObjectChangeCursor(position crmObjectChangeCursor) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode HubSpot change cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}

// decodeCRMObjectChangeCursor validates a stored cursor, which is untrusted persisted state.
func decodeCRMObjectChangeCursor(token string) (crmObjectChangeCursor, error) {
	var position crmObjectChangeCursor
	if err := json.Unmarshal([]byte(token), &position); err != nil {
		return crmObjectChangeCursor{}, errors.New("HubSpot change cursor is not decodable")
	}
	switch {
	case position.ObjectType.validate() != nil:
		return crmObjectChangeCursor{}, errors.New("HubSpot change cursor has no valid object type")
	case position.FloorMilliseconds < 0 || position.HighWaterMilliseconds < position.FloorMilliseconds ||
		position.AnchorMilliseconds <= position.FloorMilliseconds || position.DroppedThroughMilliseconds < 0 ||
		position.DroppedThroughMilliseconds > position.HighWaterMilliseconds:
		return crmObjectChangeCursor{}, errors.New("HubSpot change cursor times are inconsistent")
	case position.After != "" && !searchCursorPattern.MatchString(position.After):
		return crmObjectChangeCursor{}, errors.New("HubSpot change cursor has an invalid paging offset")
	case len(position.Seen) > maximumSeenRecordVersions:
		return crmObjectChangeCursor{}, errors.New("HubSpot change cursor holds too many record versions")
	}
	for objectID := range position.Seen {
		if !recordIDPattern.MatchString(objectID) {
			return crmObjectChangeCursor{}, errors.New("HubSpot change cursor has an invalid record ID")
		}
	}
	return position, nil
}
