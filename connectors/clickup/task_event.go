// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	signatureHeader = "X-Signature"

	// maximumWebhookIdentifierBytes bounds webhook, task, and history item IDs, which form event IDs.
	maximumWebhookIdentifierBytes = 64
	// maximumHistoryItems bounds the history items one TaskEvent carries.
	maximumHistoryItems = 20
	historyFieldStatus  = "status"
)

// ClickUp task webhook events that the taskEvent Trigger decodes. It answers 200 to every other
// event, such as listCreated, without recording it.
const (
	// EventTaskCreated is a new task; ClickUp also sends taskStatusUpdated for it.
	EventTaskCreated = "taskCreated"
	// EventTaskUpdated is any change to a task.
	EventTaskUpdated = "taskUpdated"
	// EventTaskDeleted is a deleted task; it carries no history items.
	EventTaskDeleted = "taskDeleted"
	// EventTaskPriorityUpdated is a priority change.
	EventTaskPriorityUpdated = "taskPriorityUpdated"
	// EventTaskStatusUpdated is a status change, with the status before and after.
	EventTaskStatusUpdated = "taskStatusUpdated"
	// EventTaskAssigneeUpdated is an added or removed assignee.
	EventTaskAssigneeUpdated = "taskAssigneeUpdated"
	// EventTaskDueDateUpdated is a due date change.
	EventTaskDueDateUpdated = "taskDueDateUpdated"
	// EventTaskTagUpdated is an added or removed tag.
	EventTaskTagUpdated = "taskTagUpdated"
	// EventTaskMoved is a task moved to another List.
	EventTaskMoved = "taskMoved"
	// EventTaskCommentPosted is a new comment.
	EventTaskCommentPosted = "taskCommentPosted"
	// EventTaskCommentUpdated is an edited comment.
	EventTaskCommentUpdated = "taskCommentUpdated"
	// EventTaskTimeEstimateUpdated is a time estimate change.
	EventTaskTimeEstimateUpdated = "taskTimeEstimateUpdated"
	// EventTaskTimeTrackedUpdated is tracked time added, changed, or deleted.
	EventTaskTimeTrackedUpdated = "taskTimeTrackedUpdated"
)

var supportedTaskEvents = []string{
	EventTaskCreated, EventTaskUpdated, EventTaskDeleted, EventTaskPriorityUpdated, EventTaskStatusUpdated,
	EventTaskAssigneeUpdated, EventTaskDueDateUpdated, EventTaskTagUpdated, EventTaskMoved, EventTaskCommentPosted,
	EventTaskCommentUpdated, EventTaskTimeEstimateUpdated, EventTaskTimeTrackedUpdated,
}

// TaskEvents returns every event the taskEvent Trigger decodes, in a stable order.
func TaskEvents() []string {
	return slices.Clone(supportedTaskEvents)
}

// TaskEventTriggerConfiguration selects the events one taskEvent binding accepts. An empty list accepts
// every event TaskEvents returns. An event the binding rejects is still answered 200. The events a
// binding can receive are those its ClickUp webhook subscribes to. NewTaskEventTrigger panics for an
// invalid configuration, so call Validate on untrusted values.
type TaskEventTriggerConfiguration struct {
	// Events lists accepted event names, such as taskStatusUpdated; empty accepts every task event.
	Events []string `json:"events,omitempty"`
}

// Validate checks that every configured event is a supported task event listed once.
func (configuration TaskEventTriggerConfiguration) Validate() error {
	for _, event := range configuration.Events {
		if !slices.Contains(supportedTaskEvents, event) {
			return fmt.Errorf("ClickUp webhook event %q is not a supported task event", event)
		}
	}
	if containsDuplicate(configuration.Events) {
		return errors.New("ClickUp webhook events list one event twice")
	}
	return nil
}

func (configuration TaskEventTriggerConfiguration) acceptsEvent(event string) bool {
	if len(configuration.Events) == 0 {
		return slices.Contains(supportedTaskEvents, event)
	}
	return slices.Contains(configuration.Events, event)
}

// TaskEvent is one verified ClickUp task webhook event. The Trigger event's ID is
// "{webhook_id}:{event}:{history_item_id}", or "{webhook_id}:{event}:{task_id}" for an event without
// history items, so ClickUp's retry of the same event reuses it.
type TaskEvent struct {
	// EventID is the Trigger event ID described above.
	EventID string `json:"eventId"`
	// WebhookID is the ClickUp webhook that delivered the event.
	WebhookID string `json:"webhookId"`
	// Event is the event name, such as taskStatusUpdated.
	Event string `json:"event"`
	// TaskID is the task the event is about.
	TaskID string `json:"taskId"`
	// HistoryItems describe the change, newest information first as ClickUp sends it; empty for taskDeleted.
	HistoryItems []TaskHistoryItem `json:"historyItems"`
	// OccurredAt is the first history item's time, or when the endpoint received an event without one.
	OccurredAt time.Time `json:"occurredAt"`
}

// TaskHistoryItem is one change in a task event, without the changed values except a status name.
type TaskHistoryItem struct {
	// ID is ClickUp's history item ID.
	ID string `json:"id"`
	// Field is the changed field, such as status, assignee_add, tag, content, or task_creation.
	Field string `json:"field"`
	// OccurredAt is when the change happened.
	OccurredAt time.Time `json:"occurredAt,omitzero"`
	// UserID is the user who made the change, or zero when ClickUp reports none.
	UserID int64 `json:"userId,omitempty"`
	// ParentID is the history item's parent, such as the task's List for taskCreated.
	ParentID string `json:"parentId,omitempty"`
	// StatusBefore is the previous status name of a status change.
	StatusBefore string `json:"statusBefore,omitempty"`
	// StatusAfter is the new status name of a status change.
	StatusAfter string `json:"statusAfter,omitempty"`
}

type webhookEventWire struct {
	Event        string            `json:"event"`
	WebhookID    string            `json:"webhook_id"`
	TaskID       flexibleString    `json:"task_id"`
	HistoryItems []historyItemWire `json:"history_items"`
}

type historyItemWire struct {
	ID       flexibleString   `json:"id"`
	Field    string           `json:"field"`
	Date     unixMilliseconds `json:"date"`
	ParentID flexibleString   `json:"parent_id"`
	User     *userWire        `json:"user"`
	Before   json.RawMessage  `json:"before"`
	After    json.RawMessage  `json:"after"`
}

// verifyTaskEventRequest checks X-Signature, the hex HMAC-SHA256 of the body under the webhook secret.
func verifyTaskEventRequest(request webhooktrigger.Request, credentials Credentials) error {
	secret := credentials.WebhookSecret.Reveal()
	if secret == "" {
		return fmt.Errorf("ClickUp webhook_secret is not configured: %w", webhooktrigger.ErrVerificationUnavailable)
	}
	signature, err := hex.DecodeString(strings.TrimSpace(request.Header.Get(signatureHeader)))
	if err != nil || len(signature) != sha256.Size {
		return errors.New("X-Signature is missing or is not a hex SHA-256 signature")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(request.Body) // A hash write never fails.
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return errors.New("X-Signature does not match the body")
	}
	return nil
}

// decodeTaskEventRequest decodes a verified request; an event other than a task event is valid but unhandled.
func decodeTaskEventRequest(request webhooktrigger.Request) (sdkgo.TriggerEvent[TaskEvent], bool, error) {
	var wire webhookEventWire
	if err := json.Unmarshal(request.Body, &wire); err != nil {
		return sdkgo.TriggerEvent[TaskEvent]{}, false, errors.New("ClickUp webhook body is not a JSON object")
	}
	if wire.Event == "" || !isPrintableIdentifier(wire.WebhookID) {
		return sdkgo.TriggerEvent[TaskEvent]{}, false, errors.New("ClickUp webhook event has no event name or webhook_id")
	}
	if !slices.Contains(supportedTaskEvents, wire.Event) {
		return sdkgo.TriggerEvent[TaskEvent]{}, false, nil
	}
	taskID := string(wire.TaskID)
	if !taskIDPattern.MatchString(taskID) {
		return sdkgo.TriggerEvent[TaskEvent]{}, false, errors.New("ClickUp task event has no valid task_id")
	}
	event := TaskEvent{WebhookID: wire.WebhookID, Event: wire.Event, TaskID: taskID, HistoryItems: []TaskHistoryItem{}}
	for _, item := range wire.HistoryItems {
		if len(event.HistoryItems) == maximumHistoryItems {
			break
		}
		decoded, err := decodeHistoryItem(item)
		if err != nil {
			return sdkgo.TriggerEvent[TaskEvent]{}, false, err
		}
		event.HistoryItems = append(event.HistoryItems, decoded)
	}
	event.EventID = wire.WebhookID + ":" + wire.Event + ":" + taskID
	event.OccurredAt = request.ReceivedAt.UTC()
	if len(event.HistoryItems) != 0 {
		event.EventID = wire.WebhookID + ":" + wire.Event + ":" + event.HistoryItems[0].ID
		if !event.HistoryItems[0].OccurredAt.IsZero() {
			event.OccurredAt = event.HistoryItems[0].OccurredAt
		}
	}
	return sdkgo.TriggerEvent[TaskEvent]{ID: event.EventID, OccurredAt: event.OccurredAt, Payload: event}, true, nil
}

func decodeHistoryItem(item historyItemWire) (TaskHistoryItem, error) {
	decoded := TaskHistoryItem{ID: string(item.ID), Field: item.Field, ParentID: string(item.ParentID)}
	if !isPrintableIdentifier(decoded.ID) {
		return TaskHistoryItem{}, errors.New("ClickUp history item has no valid id")
	}
	if occurredAt := item.Date.timePointer(); occurredAt != nil {
		decoded.OccurredAt = *occurredAt
	}
	if item.User != nil && item.User.ID > 0 {
		decoded.UserID = int64(item.User.ID)
	}
	if item.Field == historyFieldStatus {
		decoded.StatusBefore, decoded.StatusAfter = historyStatusName(item.Before), historyStatusName(item.After)
	}
	return decoded, nil
}

// historyStatusName reads the status name of a status history value; any other shape yields empty.
func historyStatusName(value json.RawMessage) string {
	var status struct {
		Status *string `json:"status"`
	}
	if len(bytes.TrimSpace(value)) == 0 || json.Unmarshal(value, &status) != nil || status.Status == nil {
		return ""
	}
	return *status.Status
}

func isPrintableIdentifier(value string) bool {
	if value == "" || len(value) > maximumWebhookIdentifierBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] <= ' ' || value[index] > '~' || value[index] == ':' {
			return false
		}
	}
	return true
}
