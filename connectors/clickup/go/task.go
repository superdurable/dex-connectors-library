// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxDescriptionBytes bounds a task's Markdown description in a Result; a longer one is truncated.
	MaxDescriptionBytes = 65536
	// MaxCustomFields bounds the custom field values one Task carries.
	MaxCustomFields = 100
	// MaxTaskNameBytes bounds a task name in a createTask or updateTask input.
	MaxTaskNameBytes = 1024
	// MaxTagNameBytes bounds one tag name.
	MaxTagNameBytes = 100
)

var (
	taskIDPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	numericIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)
	fieldIDPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
)

// TaskPriority is ClickUp's task priority: 1 urgent, 2 high, 3 normal, or 4 low. Zero means no priority
// in a Task and no change in an input.
type TaskPriority int

const (
	// TaskPriorityUrgent is ClickUp's priority 1.
	TaskPriorityUrgent TaskPriority = 1
	// TaskPriorityHigh is ClickUp's priority 2.
	TaskPriorityHigh TaskPriority = 2
	// TaskPriorityNormal is ClickUp's priority 3.
	TaskPriorityNormal TaskPriority = 3
	// TaskPriorityLow is ClickUp's priority 4.
	TaskPriorityLow TaskPriority = 4
)

// Task is one ClickUp task as the connector returns it.
type Task struct {
	// ID is ClickUp's task ID, such as 86b2x4k7q.
	ID string `json:"id"`
	// CustomID is the Workspace's custom task ID, such as DEV-123, when custom task IDs are enabled.
	CustomID string `json:"customId,omitempty"`
	// Name is the task name.
	Name string `json:"name"`
	// URL is the task's ClickUp web address.
	URL string `json:"url,omitempty"`
	// Status is the task's status in its List's workflow.
	Status TaskStatus `json:"status"`
	// Priority is 1 urgent through 4 low, or zero when the task has no priority.
	Priority TaskPriority `json:"priority,omitempty"`
	// PriorityName is ClickUp's priority name, such as urgent, or empty without a priority.
	PriorityName string `json:"priorityName,omitempty"`
	// DueAt is the due time in UTC, or nil without a due date.
	DueAt *time.Time `json:"dueAt,omitempty"`
	// StartAt is the start time in UTC, or nil without a start date.
	StartAt *time.Time `json:"startAt,omitempty"`
	// CreatedAt is when the task was created.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// UpdatedAt is when the task last changed.
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
	// ClosedAt is when the task reached a closed status, or nil.
	ClosedAt *time.Time `json:"closedAt,omitempty"`
	// IsArchived reports an archived task.
	IsArchived bool `json:"isArchived,omitempty"`
	// Creator is the user who created the task, when ClickUp reports one.
	Creator *User `json:"creator,omitempty"`
	// Assignees are the users the task is assigned to.
	Assignees []User `json:"assignees"`
	// Tags are the task's tag names.
	Tags []string `json:"tags"`
	// ParentTaskID is the parent task of a subtask, or empty for a top-level task.
	ParentTaskID string `json:"parentTaskId,omitempty"`
	// WorkspaceID is the Workspace (ClickUp API team) the task belongs to.
	WorkspaceID string `json:"workspaceId,omitempty"`
	// ListID is the task's home List.
	ListID string `json:"listId,omitempty"`
	// ListName is the home List's name.
	ListName string `json:"listName,omitempty"`
	// FolderID is the home List's Folder, or empty for a folderless List.
	FolderID string `json:"folderId,omitempty"`
	// SpaceID is the Space of the home List.
	SpaceID string `json:"spaceId,omitempty"`
	// MarkdownDescription is the description in Markdown; only getTask returns it.
	MarkdownDescription string `json:"markdownDescription,omitempty"`
	// IsDescriptionTruncated reports a description longer than MaxDescriptionBytes.
	IsDescriptionTruncated bool `json:"isDescriptionTruncated,omitempty"`
	// CustomFields are the task's applicable custom fields with their values; only getTask returns them.
	CustomFields []CustomFieldValue `json:"customFields,omitempty"`
}

// TaskStatus is a status of a List's workflow.
type TaskStatus struct {
	// Name is the status name as the List defines it, such as in progress.
	Name string `json:"name"`
	// Type is ClickUp's status type: open, custom, done, or closed.
	Type string `json:"type,omitempty"`
}

// User is a ClickUp user without contact details.
type User struct {
	// ID is ClickUp's numeric user ID, the value task assignees use.
	ID int64 `json:"id"`
	// Username is the user's display name.
	Username string `json:"username,omitempty"`
}

// CustomFieldValue is one custom field of a task.
type CustomFieldValue struct {
	// ID is the custom field's ID, a UUID.
	ID string `json:"id"`
	// Name is the custom field's name.
	Name string `json:"name"`
	// Type is ClickUp's field type, such as short_text, number, drop_down, labels, or users.
	Type string `json:"type"`
	// Value is ClickUp's value passed through unchanged, or absent when the field is unset. A drop_down
	// value is an option ID or index and a users value is a list of users; see ClickUp's Custom Fields guide.
	Value json.RawMessage `json:"value,omitempty"`
}

// CustomFieldInput sets one custom field on a new task.
type CustomFieldInput struct {
	// ID is the custom field's ID, from getTask or ClickUp's Get Accessible Custom Fields.
	ID string `json:"id"`
	// Value is the JSON value ClickUp expects for the field's type, such as "text", 5, or an option ID.
	Value json.RawMessage `json:"value"`
}

type taskWire struct {
	ID                  string            `json:"id"`
	CustomID            *string           `json:"custom_id"`
	Name                string            `json:"name"`
	URL                 string            `json:"url"`
	Status              taskStatusWire    `json:"status"`
	Priority            json.RawMessage   `json:"priority"`
	DueDate             unixMilliseconds  `json:"due_date"`
	StartDate           unixMilliseconds  `json:"start_date"`
	DateCreated         unixMilliseconds  `json:"date_created"`
	DateUpdated         unixMilliseconds  `json:"date_updated"`
	DateClosed          unixMilliseconds  `json:"date_closed"`
	Archived            bool              `json:"archived"`
	Creator             *userWire         `json:"creator"`
	Assignees           []userWire        `json:"assignees"`
	Tags                []tagWire         `json:"tags"`
	Parent              *string           `json:"parent"`
	TeamID              flexibleString    `json:"team_id"`
	List                locationWire      `json:"list"`
	Folder              locationWire      `json:"folder"`
	Space               locationWire      `json:"space"`
	Description         *string           `json:"description"`
	MarkdownDescription *string           `json:"markdown_description"`
	CustomFields        []customFieldWire `json:"custom_fields"`
}

type taskStatusWire struct {
	Status string `json:"status"`
	Type   string `json:"type"`
}

type userWire struct {
	ID       flexibleInt64 `json:"id"`
	Username string        `json:"username"`
}

type tagWire struct {
	Name string `json:"name"`
}

type locationWire struct {
	ID   flexibleString `json:"id"`
	Name string         `json:"name"`
}

type customFieldWire struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type priorityWire struct {
	ID       flexibleString `json:"id"`
	Priority string         `json:"priority"`
}

// decodeTask decodes one task; withDetails keeps the description and custom fields that only getTask returns.
func decodeTask(wire taskWire, withDetails bool) (Task, error) {
	if !taskIDPattern.MatchString(wire.ID) {
		return Task{}, errors.New("task has no valid ID")
	}
	priority, priorityName, err := decodePriority(wire.Priority)
	if err != nil {
		return Task{}, err
	}
	task := Task{
		ID: wire.ID, Name: wire.Name, URL: wire.URL,
		Status:   TaskStatus{Name: wire.Status.Status, Type: wire.Status.Type},
		Priority: priority, PriorityName: priorityName,
		DueAt: wire.DueDate.timePointer(), StartAt: wire.StartDate.timePointer(), ClosedAt: wire.DateClosed.timePointer(),
		IsArchived: wire.Archived, WorkspaceID: string(wire.TeamID),
		ListID: string(wire.List.ID), ListName: wire.List.Name, FolderID: string(wire.Folder.ID), SpaceID: string(wire.Space.ID),
		Assignees: make([]User, 0, len(wire.Assignees)), Tags: make([]string, 0, len(wire.Tags)),
	}
	if created := wire.DateCreated.timePointer(); created != nil {
		task.CreatedAt = *created
	}
	if updated := wire.DateUpdated.timePointer(); updated != nil {
		task.UpdatedAt = *updated
	}
	if wire.CustomID != nil {
		task.CustomID = *wire.CustomID
	}
	if wire.Parent != nil {
		task.ParentTaskID = *wire.Parent
	}
	if wire.Creator != nil && wire.Creator.ID > 0 {
		task.Creator = &User{ID: int64(wire.Creator.ID), Username: wire.Creator.Username}
	}
	for _, assignee := range wire.Assignees {
		if assignee.ID <= 0 {
			return Task{}, errors.New("task assignee has no valid user ID")
		}
		task.Assignees = append(task.Assignees, User{ID: int64(assignee.ID), Username: assignee.Username})
	}
	for _, tag := range wire.Tags {
		task.Tags = append(task.Tags, tag.Name)
	}
	if !withDetails {
		return task, nil
	}
	description := wire.MarkdownDescription
	if description == nil {
		description = wire.Description
	}
	if description != nil {
		task.MarkdownDescription, task.IsDescriptionTruncated = truncateUTF8(*description, MaxDescriptionBytes)
	}
	for _, field := range wire.CustomFields {
		if len(task.CustomFields) == MaxCustomFields {
			break
		}
		if !fieldIDPattern.MatchString(field.ID) {
			return Task{}, errors.New("task custom field has no valid ID")
		}
		value := field.Value
		if len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			value = nil
		}
		task.CustomFields = append(task.CustomFields, CustomFieldValue{ID: field.ID, Name: field.Name, Type: field.Type, Value: value})
	}
	return task, nil
}

// decodeTaskBody decodes a response whose body is one task object.
func decodeTaskBody(body []byte, withDetails bool) (Task, error) {
	var wire taskWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return Task{}, errors.New("task is not a JSON object")
	}
	return decodeTask(wire, withDetails)
}

// decodePriority accepts ClickUp's priority object, a bare number, or null.
func decodePriority(raw json.RawMessage) (TaskPriority, string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return 0, "", nil
	}
	var level int
	var name string
	if trimmed[0] == '{' {
		var wire priorityWire
		if err := json.Unmarshal(trimmed, &wire); err != nil {
			return 0, "", errors.New("task priority is invalid")
		}
		parsed, err := strconv.Atoi(string(wire.ID))
		if err != nil {
			return 0, "", errors.New("task priority has no valid level")
		}
		level, name = parsed, wire.Priority
	} else if err := json.Unmarshal(trimmed, &level); err != nil {
		return 0, "", errors.New("task priority is invalid")
	}
	if level < int(TaskPriorityUrgent) || level > int(TaskPriorityLow) {
		return 0, "", fmt.Errorf("task priority %d is not 1 through 4", level)
	}
	return TaskPriority(level), name, nil
}

// unixMilliseconds is a ClickUp time: milliseconds since the Unix epoch as a string or number, or null.
type unixMilliseconds int64

// UnmarshalJSON accepts a string or number of milliseconds, and treats null or an empty string as absent.
func (value *unixMilliseconds) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if text == "" || text == "null" {
		*value = 0
		return nil
	}
	milliseconds, err := strconv.ParseInt(text, 10, 64)
	if err != nil || milliseconds < 0 {
		return errors.New("time is not Unix milliseconds")
	}
	*value = unixMilliseconds(milliseconds)
	return nil
}

func (value unixMilliseconds) timePointer() *time.Time {
	if value <= 0 {
		return nil
	}
	converted := time.UnixMilli(int64(value)).UTC()
	return &converted
}

// flexibleString accepts a JSON string or number, because ClickUp returns IDs as either, and null.
type flexibleString string

// UnmarshalJSON keeps a string, the text of a number, or empty for null.
func (value *flexibleString) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*value = ""
		return nil
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		*value = flexibleString(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(trimmed, &number); err != nil {
		return errors.New("identifier is not a string or number")
	}
	*value = flexibleString(number.String())
	return nil
}

// flexibleInt64 accepts a JSON integer or a string of digits, because ClickUp user IDs appear as both.
type flexibleInt64 int64

// UnmarshalJSON accepts an integer, a string of digits, or null as zero.
func (value *flexibleInt64) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if text == "null" || text == "" {
		*value = 0
		return nil
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return errors.New("user ID is not an integer")
	}
	*value = flexibleInt64(parsed)
	return nil
}

// truncateUTF8 returns at most maximumBytes of text, cut at a rune boundary, and whether it was cut.
func truncateUTF8(text string, maximumBytes int) (string, bool) {
	if len(text) <= maximumBytes {
		return text, false
	}
	cut := maximumBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut], true
}

func validateTaskID(field string, taskID string) error {
	if !taskIDPattern.MatchString(taskID) {
		return fmt.Errorf("%s must be a ClickUp task ID of letters, digits, '-', or '_'", field)
	}
	return nil
}

func validateNumericID(field string, id string) error {
	if !numericIDPattern.MatchString(id) {
		return fmt.Errorf("%s must be a ClickUp ID of digits", field)
	}
	return nil
}

func validateTagNames(field string, tags []string) error {
	for _, tag := range tags {
		if strings.TrimSpace(tag) != tag || tag == "" || len(tag) > MaxTagNameBytes || !utf8.ValidString(tag) || strings.ContainsAny(tag, "/\x00\r\n\t") {
			return fmt.Errorf("%s must be tag names of 1 to %d bytes without surrounding spaces, '/', or control characters", field, MaxTagNameBytes)
		}
		// A dot segment in the tag path could resolve to DELETE /task/{task_id}.
		if tag == "." || tag == ".." {
			return fmt.Errorf("%s cannot name the tag %q", field, tag)
		}
	}
	if containsDuplicate(tags) {
		return fmt.Errorf("%s lists a tag twice", field)
	}
	return nil
}

func validateUserIDs(field string, userIDs []int64) error {
	for _, userID := range userIDs {
		if userID <= 0 {
			return fmt.Errorf("%s must be positive ClickUp user IDs", field)
		}
	}
	if containsDuplicate(userIDs) {
		return fmt.Errorf("%s lists a user twice", field)
	}
	return nil
}

func validatePriority(priority TaskPriority) error {
	if priority != 0 && (priority < TaskPriorityUrgent || priority > TaskPriorityLow) {
		return errors.New("priority must be 1 (urgent), 2 (high), 3 (normal), 4 (low), or zero")
	}
	return nil
}

func validateTaskName(field string, name string) error {
	if strings.TrimSpace(name) == "" || len(name) > MaxTaskNameBytes || !utf8.ValidString(name) {
		return fmt.Errorf("%s must be valid UTF-8 text of 1 to %d bytes", field, MaxTaskNameBytes)
	}
	return nil
}

func validateStatusName(status string) error {
	if status != "" && (strings.TrimSpace(status) == "" || len(status) > 255 || !utf8.ValidString(status)) {
		return errors.New("status must be a status name of the task's List of at most 255 bytes")
	}
	return nil
}
