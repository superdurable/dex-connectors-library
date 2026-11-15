// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	// taskEventResourceType keeps task and subtask events; project, section, and story events are dropped.
	taskEventResourceType = "task"
	// maximumEventsPerPage is Asana's documented cap of events per sync token response.
	maximumEventsPerPage = 100
	// taskEventFields asks for names and change details; gids are always returned.
	taskEventFields = "action,created_at,user.name,resource.name,resource.resource_type,resource.resource_subtype," +
		"parent.name,parent.resource_type,change.field,change.action,change.new_value,change.added_value,change.removed_value"
)

// taskEventFetcher reads one project's Asana Events API stream for the poll Trigger kit.
type taskEventFetcher struct {
	client    *Client
	call      sdkgo.Call
	projectID string
}

// taskEventCursor is the committed Trigger cursor: Asana's sync token for one project.
type taskEventCursor struct {
	ProjectID string `json:"projectId"`
	SyncToken string `json:"sync"`
}

// asanaEventPage is GET /events: 200 carries data, sync, and has_more; 412 carries only a fresh sync.
type asanaEventPage struct {
	Data    []asanaEvent `json:"data"`
	Sync    string       `json:"sync"`
	HasMore bool         `json:"has_more"`
}

type asanaEvent struct {
	Action    string             `json:"action"`
	CreatedAt string             `json:"created_at"`
	User      *namedResource     `json:"user"`
	Resource  *asanaEventSubject `json:"resource"`
	Parent    *asanaEventSubject `json:"parent"`
	Change    *asanaEventChange  `json:"change"`
}

type asanaEventSubject struct {
	GID             string `json:"gid"`
	Name            string `json:"name"`
	ResourceType    string `json:"resource_type"`
	ResourceSubtype string `json:"resource_subtype"`
}

type asanaEventChange struct {
	Field        string          `json:"field"`
	Action       string          `json:"action"`
	NewValue     json.RawMessage `json:"new_value"`
	AddedValue   json.RawMessage `json:"added_value"`
	RemovedValue json.RawMessage `json:"removed_value"`
}

// eventListing is one classified /events response.
type eventListing struct {
	page          asanaEventPage
	isSyncInvalid bool
}

func newTaskEventFetcher(client *Client, connection sdkgo.ConnectionRef, configuration TaskChangedTriggerConfiguration) *taskEventFetcher {
	return &taskEventFetcher{
		client:    client,
		call:      pollingtrigger.NewTriggerCredentialCall(connection, TaskChangedTriggerDefinition.Trigger),
		projectID: configuration.ProjectID,
	}
}

// SeedCursor asks for the project's events without a sync token. Asana answers 412 with a fresh token,
// so existing tasks become the baseline and are never delivered.
func (fetcher *taskEventFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	listing, err := fetcher.listEvents(ctx, "")
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	if listing.page.Sync == "" {
		return pollingtrigger.Cursor{}, errors.New("Asana event stream returned no sync token")
	}
	return fetcher.encodeCursor(taskEventCursor{ProjectID: fetcher.projectID, SyncToken: listing.page.Sync})
}

// FetchChanges reads the events after the cursor's sync token. An expired token (HTTP 412), an
// undecodable cursor, or a cursor for another project is marked invalid, so the kit records a new baseline.
func (fetcher *taskEventFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[TaskChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[TaskChangedEvent]]
	var position taskEventCursor
	if err := json.Unmarshal([]byte(cursor.Token), &position); err != nil || position.SyncToken == "" {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Asana event cursor is not decodable"))
	}
	if position.ProjectID != fetcher.projectID {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Asana event cursor belongs to another project"))
	}
	listing, err := fetcher.listEvents(ctx, position.SyncToken)
	if err != nil {
		return page, err
	}
	if listing.isSyncInvalid {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Asana sync token expired or is invalid (HTTP 412)"))
	}
	if listing.page.Sync == "" {
		return page, errors.New("Asana event page has no sync token")
	}
	if len(listing.page.Data) > maximumEventsPerPage {
		return page, fmt.Errorf("Asana event page has %d events, more than %d", len(listing.page.Data), maximumEventsPerPage)
	}
	page.Changes, err = fetcher.convertEvents(position.SyncToken, listing.page.Data)
	if err != nil {
		return page, err
	}
	page.HasMorePages = listing.page.HasMore
	page.Next, err = fetcher.encodeCursor(taskEventCursor{ProjectID: fetcher.projectID, SyncToken: listing.page.Sync})
	return page, err
}

// convertEvents keeps task events in Asana's order. Asana returns the same events for a re-requested sync
// token, so the requested token plus an ordinal among identical events in its page keeps IDs stable across
// refetches, and two identical events on either side of a page boundary still get distinct IDs.
func (fetcher *taskEventFetcher) convertEvents(requestedSyncToken string, events []asanaEvent) ([]sdkgo.TriggerEvent[TaskChangedEvent], error) {
	now := fetcher.client.now()
	occurrences := map[string]int{}
	var changes []sdkgo.TriggerEvent[TaskChangedEvent]
	for _, event := range events {
		if event.Resource == nil || event.Resource.ResourceType != taskEventResourceType {
			continue
		}
		payload, err := convertTaskEvent(fetcher.projectID, event)
		if err != nil {
			return nil, fmt.Errorf("Asana event page has an invalid task event: %w", err)
		}
		identity := requestedSyncToken + "\x00" + taskEventIdentity(fetcher.projectID, event)
		occurrences[identity]++
		if occurrence := occurrences[identity]; occurrence > 1 {
			identity += "\x00" + strconv.Itoa(occurrence)
		}
		digest := sha256.Sum256([]byte(identity))
		changes = append(changes, sdkgo.TriggerEvent[TaskChangedEvent]{
			ID: "asana-" + hex.EncodeToString(digest[:16]), OccurredAt: parseEventTime(event.CreatedAt, now), Payload: payload,
		})
	}
	return changes, nil
}

func convertTaskEvent(projectID string, event asanaEvent) (TaskChangedEvent, error) {
	action := TaskChangeAction(event.Action)
	if !gidPattern.MatchString(event.Resource.GID) {
		return TaskChangedEvent{}, errors.New("task gid is not an Asana gid")
	}
	if !isKnownTaskChangeAction(action) {
		return TaskChangedEvent{}, fmt.Errorf("unknown action %q", event.Action)
	}
	payload := TaskChangedEvent{
		ProjectID: projectID, Action: action, OccurredAt: event.CreatedAt,
		Task:        TaskReference{ID: event.Resource.GID, Name: event.Resource.Name},
		TaskSubtype: event.Resource.ResourceSubtype,
	}
	if event.Change != nil && action == TaskChangeChanged {
		payload.ChangedField, payload.ChangeAction = event.Change.Field, event.Change.Action
	}
	if event.Parent != nil && gidPattern.MatchString(event.Parent.GID) {
		payload.Parent = &EventParentReference{ID: event.Parent.GID, ResourceType: event.Parent.ResourceType, Name: event.Parent.Name}
	}
	if event.User != nil && gidPattern.MatchString(event.User.GID) {
		payload.User = &UserReference{ID: event.User.GID, Name: event.User.Name}
	}
	return payload, nil
}

// taskEventIdentity joins the event's immutable facts. Names are left out because a rename between two
// fetches of the same sync token would otherwise change the ID.
func taskEventIdentity(projectID string, event asanaEvent) string {
	parts := []string{projectID, event.Resource.GID, event.Action, event.CreatedAt}
	if event.User != nil {
		parts = append(parts, event.User.GID)
	} else {
		parts = append(parts, "")
	}
	if event.Parent != nil {
		parts = append(parts, event.Parent.GID)
	} else {
		parts = append(parts, "")
	}
	if event.Change != nil {
		parts = append(parts, event.Change.Field, event.Change.Action,
			changeValueIdentity(event.Change.NewValue), changeValueIdentity(event.Change.AddedValue), changeValueIdentity(event.Change.RemovedValue))
	}
	return strings.Join(parts, "\x00")
}

// changeValueIdentity is a referenced object's gid, or the compacted JSON of a scalar value.
func changeValueIdentity(value json.RawMessage) string {
	var reference struct {
		GID string `json:"gid"`
	}
	if json.Unmarshal(value, &reference) == nil && reference.GID != "" {
		return "gid:" + reference.GID
	}
	var compacted bytes.Buffer
	if json.Compact(&compacted, value) != nil {
		return ""
	}
	return compacted.String()
}

// listEvents sends one GET /events request for the project, with syncToken unless seeding.
func (fetcher *taskEventFetcher) listEvents(ctx context.Context, syncToken string) (eventListing, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return eventListing{}, err
	}
	credentials, err := fetcher.resolveCredentials(ctx)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return eventListing{}, errors.New("Asana Trigger credentials are unavailable; save a personal access token for the connection")
	}
	query := url.Values{"resource": {fetcher.projectID}, "opt_fields": {taskEventFields}}
	if syncToken != "" {
		query.Set("sync", syncToken)
	}
	requestContext, cancel := context.WithTimeout(ctx, operationDeadline)
	defer cancel()
	session := &operationSession{context: requestContext, call: fetcher.call, credentials: credentials}
	result := fetcher.client.exchange(session, asanaRequest{method: http.MethodGet, path: "/events", query: query})
	return fetcher.classifyEventListing(result)
}

// resolveCredentials rereads the token under the poll context, because Trigger calls have no Dex context.
func (fetcher *taskEventFetcher) resolveCredentials(ctx context.Context) (Credentials, error) {
	if provider, ok := fetcher.client.credentials.(sdkgo.ContextCredentialProvider[Credentials]); ok {
		return provider.ResolveContext(ctx, fetcher.call)
	}
	return fetcher.client.credentials.Resolve(fetcher.call)
}

func (fetcher *taskEventFetcher) classifyEventListing(result exchangeResult) (eventListing, error) {
	if result.err != nil {
		return eventListing{}, errors.New("Asana event stream request failed before a response")
	}
	response := result.response
	switch {
	case response.statusCode == http.StatusPreconditionFailed:
		var page asanaEventPage
		if response.reflectsSecret || json.Unmarshal(response.body, &page) != nil || page.Sync == "" {
			return eventListing{}, errors.New("Asana answered HTTP 412 without a fresh sync token")
		}
		return eventListing{page: asanaEventPage{Sync: page.Sync}, isSyncInvalid: true}, nil
	case response.statusCode == http.StatusTooManyRequests:
		// Asana's limits are per user and per domain; the kit pauses every binding of the connection.
		return eventListing{}, pollingtrigger.MarkRateLimited(errors.New("Asana rate limited the event stream request (HTTP 429)"), fetcher.client.retryAfter(response))
	case !isSuccessStatus(response.statusCode):
		return eventListing{}, fmt.Errorf("Asana rejected the event stream request with HTTP %d", response.statusCode)
	case response.isBodyTooLarge:
		return eventListing{}, errors.New("Asana event page exceeds the configured maxResponseBytes")
	case response.hasBodyReadFailed:
		return eventListing{}, errors.New("Asana event page response was interrupted")
	case response.reflectsSecret:
		return eventListing{}, errors.New("Asana event page contains credential material")
	}
	var page asanaEventPage
	if err := json.Unmarshal(response.body, &page); err != nil {
		return eventListing{}, errors.New("Asana event page is not valid JSON")
	}
	return eventListing{page: page}, nil
}

func (fetcher *taskEventFetcher) encodeCursor(position taskEventCursor) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Asana event cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}
