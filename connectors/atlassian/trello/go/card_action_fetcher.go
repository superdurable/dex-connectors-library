// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello

import (
	"bytes"
	"context"
	"encoding/hex"
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
)

const (
	// actionPageSize is the limit of one actions request: at most Trello's 1000 and the kit's page bound.
	actionPageSize = pollingtrigger.MaxChangesPerPage
	// maximumActionPagesPerPoll bounds one walk back through the history to 10,000 actions.
	maximumActionPagesPerPoll = 20
	// actionLookback re-reads recent history, because actions recorded in the same moment can surface late.
	actionLookback = time.Minute
	// maximumSeenActionIDs keeps the cursor token far below the kit's 64 KiB bound.
	maximumSeenActionIDs = 2000
	// cardActionOperationID names the Trigger in credential calls and failures.
	cardActionOperationID = "cardChanged"
)

// cardActionFetcher reads one board's card actions for the poll Trigger kit.
type cardActionFetcher struct {
	client  *Client
	call    sdkgo.Call
	boardID string
}

// cardActionCursor is the committed Trigger cursor. Watermark is the newest delivered action time; blank
// means the board had no card action at the baseline. SeenActionIDs are the sorted IDs already delivered
// within actionLookback of Watermark, so re-reading that window never delivers one twice. When more than
// maximumSeenActionIDs fall inside the window, FloorActionID is the largest ID dropped from the set, and
// every action ID at or below it counts as delivered, so a burst neither repeats nor stalls.
type cardActionCursor struct {
	BoardID       string   `json:"boardId"`
	Watermark     string   `json:"watermark,omitempty"`
	SeenActionIDs []string `json:"seenActionIds,omitempty"`
	FloorActionID string   `json:"floorActionId,omitempty"`
}

// cardActionResource is one Trello action object with the fields cardChanged requests.
type cardActionResource struct {
	ID              string          `json:"id"`
	IDMemberCreator string          `json:"idMemberCreator"`
	Type            string          `json:"type"`
	Date            string          `json:"date"`
	Data            cardActionData  `json:"data"`
	MemberCreator   *memberResource `json:"memberCreator"`
	occurredAt      time.Time       `json:"-"`
}

type cardActionData struct {
	Card       *cardActionCardResource    `json:"card"`
	List       *listResource              `json:"list"`
	ListBefore *listResource              `json:"listBefore"`
	ListAfter  *listResource              `json:"listAfter"`
	Board      *listResource              `json:"board"`
	Old        map[string]json.RawMessage `json:"old"`
	Text       string                     `json:"text"`
}

type cardActionCardResource struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortLink string `json:"shortLink"`
	IDShort   int64  `json:"idShort"`
	IDList    string `json:"idList"`
	Closed    *bool  `json:"closed"`
}

func newCardActionFetcher(client *Client, connection sdkgo.ConnectionRef, configuration CardChangedTriggerConfiguration) *cardActionFetcher {
	return &cardActionFetcher{
		client:  client,
		call:    pollingtrigger.NewTriggerCredentialCall(connection, CardChangedTriggerDefinition.Trigger),
		boardID: configuration.BoardID,
	}
}

// SeedCursor reads the newest page of card actions and keeps only its position, so earlier actions become
// the baseline and are never delivered.
func (fetcher *cardActionFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	newest, err := fetcher.listActionPage(ctx, time.Time{}, "")
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	position := cardActionCursor{BoardID: fetcher.boardID}
	if len(newest) > 0 {
		watermark := latestActionTime(newest)
		position.Watermark = watermark.Format(trelloTimeLayout)
		position.SeenActionIDs, position.FloorActionID = seenActionIDsWithin(nil, "", newest, watermark)
	}
	return encodeCardActionCursor(position)
}

// FetchChanges walks the actions after the lookback window from newest to oldest, drops the ones already
// delivered, and returns the oldest actionPageSize in time order. An undecodable cursor, a cursor for
// another board, a 400 naming since or before, or a backlog over maximumActionPagesPerPoll pages is
// marked invalid, so the kit records a new baseline.
func (fetcher *cardActionFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[CardChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[CardChangedEvent]]
	position, err := fetcher.decodeCursor(cursor)
	if err != nil {
		return page, err
	}
	var since, watermark time.Time
	if position.Watermark != "" {
		watermark, _ = time.Parse(time.RFC3339Nano, position.Watermark)
		since = watermark.Add(-actionLookback)
	}
	listed, err := fetcher.walkActionsSince(ctx, since)
	if err != nil {
		return page, err
	}
	seen := map[string]bool{}
	for _, actionID := range position.SeenActionIDs {
		seen[actionID] = true
	}
	fresh := make([]cardActionResource, 0, len(listed))
	for _, action := range listed {
		actionID := strings.ToLower(action.ID)
		if !seen[actionID] && actionID > position.FloorActionID && (since.IsZero() || !action.occurredAt.Before(since)) {
			fresh = append(fresh, action)
		}
	}
	slices.SortFunc(fresh, compareActionsOldestFirst)
	if len(fresh) > actionPageSize {
		fresh, page.HasMorePages = fresh[:actionPageSize], true
	}
	for _, action := range fresh {
		page.Changes = append(page.Changes, newCardChangedTriggerEvent(action.cardChangedEvent()))
	}
	if len(fresh) > 0 {
		watermark = maxTime(watermark, latestActionTime(fresh))
		position.Watermark = watermark.Format(trelloTimeLayout)
	}
	position.SeenActionIDs, position.FloorActionID = seenActionIDsWithin(position.SeenActionIDs, position.FloorActionID, fresh, watermark)
	page.Next, err = encodeCardActionCursor(position)
	return page, err
}

func (fetcher *cardActionFetcher) decodeCursor(cursor pollingtrigger.Cursor) (cardActionCursor, error) {
	var position cardActionCursor
	decoder := json.NewDecoder(strings.NewReader(cursor.Token))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&position); err != nil {
		return position, pollingtrigger.MarkCursorInvalid(errors.New("Trello card action cursor is not decodable"))
	}
	if position.BoardID != fetcher.boardID {
		return position, pollingtrigger.MarkCursorInvalid(errors.New("Trello card action cursor belongs to another board"))
	}
	if position.Watermark != "" {
		if _, err := time.Parse(time.RFC3339Nano, position.Watermark); err != nil {
			return position, pollingtrigger.MarkCursorInvalid(errors.New("Trello card action cursor has an invalid watermark"))
		}
	}
	for _, actionID := range append(slices.Clip(position.SeenActionIDs), position.FloorActionID) {
		if actionID != "" && (!trelloIDPattern.MatchString(actionID) || actionID != strings.ToLower(actionID)) {
			return position, pollingtrigger.MarkCursorInvalid(errors.New("Trello card action cursor has an invalid action ID"))
		}
	}
	return position, nil
}

// walkActionsSince pages from the newest action back with before=<oldest ID> until a short page.
func (fetcher *cardActionFetcher) walkActionsSince(ctx context.Context, since time.Time) ([]cardActionResource, error) {
	var collected []cardActionResource
	before := ""
	for range maximumActionPagesPerPoll {
		listed, err := fetcher.listActionPage(ctx, since, before)
		if err != nil {
			return nil, err
		}
		collected = append(collected, listed...)
		if len(listed) < actionPageSize {
			return collected, nil
		}
		// Trello compares before against action IDs, so page from the smallest ID.
		oldest := slices.MinFunc(listed, func(left, right cardActionResource) int {
			return strings.Compare(strings.ToLower(left.ID), strings.ToLower(right.ID))
		}).ID
		if oldest == before {
			return nil, errors.New("Trello actions page did not move past its before cursor")
		}
		before = oldest
	}
	return nil, pollingtrigger.MarkCursorInvalid(fmt.Errorf("Trello board has more than %d card actions since the last poll", maximumActionPagesPerPoll*actionPageSize))
}

// listActionPage sends one GET /boards/{board}/actions request for the watched card action types, newest
// first. A zero since and a blank before read the newest page.
func (fetcher *cardActionFetcher) listActionPage(ctx context.Context, since time.Time, before string) ([]cardActionResource, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return nil, err
	}
	credentials, err := fetcher.resolveCredentials(ctx)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return nil, errors.New("Trello cardChanged credentials are unavailable; save an API key and a token for the connection")
	}
	filters := make([]string, 0, len(watchedCardActionTypes))
	for _, actionType := range watchedCardActionTypes {
		filters = append(filters, string(actionType))
	}
	query := url.Values{
		"filter": {strings.Join(filters, ",")}, "limit": {strconv.Itoa(actionPageSize)},
		"fields": {"id,idMemberCreator,type,date,data"}, "memberCreator": {"true"}, "memberCreator_fields": {"fullName,username"},
	}
	if !since.IsZero() {
		query.Set("since", since.UTC().Format(trelloTimeLayout))
	}
	if before != "" {
		query.Set("before", before)
	}
	session := &operationSession{context: ctx, call: fetcher.call, credentials: credentials}
	result := fetcher.client.exchange(session, trelloRequest{method: http.MethodGet, path: joinPath("boards", fetcher.boardID, "actions"), query: query})
	if err := fetcher.classifyActionPage(result); err != nil {
		return nil, err
	}
	var listed []cardActionResource
	if err := json.Unmarshal(result.response.body, &listed); err != nil {
		return nil, errors.New("Trello actions page is not a JSON array of actions")
	}
	if len(listed) > actionPageSize {
		return nil, fmt.Errorf("Trello actions page has %d actions, more than the requested %d", len(listed), actionPageSize)
	}
	for i := range listed {
		if err := listed[i].validate(); err != nil {
			return nil, fmt.Errorf("Trello actions page has an invalid action: %w", err)
		}
	}
	return listed, nil
}

// resolveCredentials stops broker work with ctx when the provider supports it; poll calls have no Step context.
func (fetcher *cardActionFetcher) resolveCredentials(ctx context.Context) (Credentials, error) {
	if contextProvider, ok := fetcher.client.credentials.(sdkgo.ContextCredentialProvider[Credentials]); ok {
		return contextProvider.ResolveContext(ctx, fetcher.call)
	}
	return fetcher.client.credentials.Resolve(fetcher.call)
}

// classifyActionPage maps a failed exchange to a poll error: 429 pauses the connection, and a 400 that
// names since or before rejects the cursor; any other failure backs off this binding.
func (fetcher *cardActionFetcher) classifyActionPage(result exchangeResult) error {
	classification := fetcher.client.classifyRead(cardActionOperationID, "board action history", result)
	switch classification.outcome {
	case readSucceeded:
		return nil
	case readRetry:
		if result.err == nil && result.response.statusCode == http.StatusTooManyRequests {
			// Trello sends no Retry-After, so the pause defaults to one rate-limit window.
			return pollingtrigger.MarkRateLimited(errors.New(classification.failure.Message), fetcher.client.rateLimitDelay(result.response))
		}
	case readRejected:
		body := bytes.ToLower(result.response.body)
		if result.response.statusCode == http.StatusBadRequest && (bytes.Contains(body, []byte("since")) || bytes.Contains(body, []byte("before"))) {
			return pollingtrigger.MarkCursorInvalid(errors.New(classification.failure.Message))
		}
	}
	return errors.New(classification.failure.Message)
}

// validate checks the fields cardChanged relies on and parses the action time.
func (action *cardActionResource) validate() error {
	if !trelloIDPattern.MatchString(action.ID) {
		return errors.New("action id is not a Trello ID")
	}
	if !slices.Contains(watchedCardActionTypes, CardActionType(action.Type)) {
		return fmt.Errorf("action %s has unrequested type %q", action.ID, action.Type)
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, action.Date)
	if err != nil {
		return fmt.Errorf("action %s date is not an ISO 8601 time", action.ID)
	}
	action.occurredAt = occurredAt.UTC()
	if action.Data.Card == nil || !trelloIDPattern.MatchString(action.Data.Card.ID) {
		return fmt.Errorf("action %s has no card ID", action.ID)
	}
	return nil
}

func (action cardActionResource) cardChangedEvent() CardChangedEvent {
	card := action.Data.Card
	event := CardChangedEvent{
		ActionID: action.ID, ActionType: CardActionType(action.Type), OccurredAt: action.occurredAt,
		MemberCreatorID: action.IDMemberCreator,
		Card:            ActionCard{ID: card.ID, Name: card.Name, ShortLink: card.ShortLink, ShortID: card.IDShort, ListID: card.IDList, IsClosed: card.Closed},
		List:            actionList(action.Data.List), ListBefore: actionList(action.Data.ListBefore), ListAfter: actionList(action.Data.ListAfter),
	}
	if board := action.Data.Board; board != nil {
		event.BoardID, event.BoardName = board.ID, board.Name
	}
	if creator := action.MemberCreator; creator != nil && creator.ID != "" {
		event.MemberCreator = &MemberReference{ID: creator.ID, FullName: creator.FullName, Username: creator.Username}
	}
	if event.ActionType == CardActionUpdated {
		for field := range action.Data.Old {
			event.ChangedFields = append(event.ChangedFields, field)
		}
		slices.Sort(event.ChangedFields)
	}
	if event.ActionType == CardActionCommented {
		event.CommentText = action.Data.Text
	}
	return event
}

func actionList(list *listResource) *ActionList {
	if list == nil || list.ID == "" {
		return nil
	}
	return &ActionList{ID: list.ID, Name: list.Name}
}

// seenActionIDsWithin merges previous and added IDs and keeps those whose ObjectId time is inside the
// lookback window before watermark, newest maximumSeenActionIDs, sorted for a stable cursor token. It
// returns the floor raised to the largest ID dropped over that bound, or blank once the floor's time is
// outside the window, where since already excludes it.
func seenActionIDsWithin(previous []string, floor string, added []cardActionResource, watermark time.Time) ([]string, string) {
	horizon := watermark.Add(-actionLookback - time.Second)
	merged := map[string]bool{}
	for _, actionID := range previous {
		merged[actionID] = true
	}
	for _, action := range added {
		merged[strings.ToLower(action.ID)] = true
	}
	kept := make([]string, 0, len(merged))
	for actionID := range merged {
		if created, ok := objectIDTime(actionID); ok && !created.Before(horizon) {
			kept = append(kept, actionID)
		}
	}
	slices.Sort(kept)
	if len(kept) > maximumSeenActionIDs {
		floor = max(floor, kept[len(kept)-maximumSeenActionIDs-1])
		kept = kept[len(kept)-maximumSeenActionIDs:]
	}
	if created, ok := objectIDTime(floor); !ok || created.Before(horizon) {
		floor = ""
	}
	if len(kept) == 0 {
		return nil, floor
	}
	return kept, floor
}

// objectIDTime reads the creation second Trello's Mongo-style IDs carry in their first four bytes.
func objectIDTime(actionID string) (time.Time, bool) {
	decoded, err := hex.DecodeString(actionID)
	if err != nil || len(decoded) != 12 {
		return time.Time{}, false
	}
	seconds := int64(decoded[0])<<24 | int64(decoded[1])<<16 | int64(decoded[2])<<8 | int64(decoded[3])
	return time.Unix(seconds, 0).UTC(), true
}

func compareActionsOldestFirst(left, right cardActionResource) int {
	if order := left.occurredAt.Compare(right.occurredAt); order != 0 {
		return order
	}
	return strings.Compare(strings.ToLower(left.ID), strings.ToLower(right.ID))
}

func latestActionTime(actions []cardActionResource) time.Time {
	return slices.MaxFunc(actions, compareActionsOldestFirst).occurredAt
}

func maxTime(left, right time.Time) time.Time {
	if left.After(right) {
		return left
	}
	return right
}

func encodeCardActionCursor(position cardActionCursor) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Trello card action cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}
