// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maximumTaskNameCharacters = 1024
	maximumTextCharacters     = 65536
	maximumCustomFieldValues  = 20
	maximumEnumOptionIDs      = 50
	maximumListedMemberships  = 20
	maximumListedCustomFields = 50
	maximumEmailBytes         = 254
	dueDateLayout             = "2006-01-02"
	assigneeMe                = "me"
)

var (
	// gidPattern accepts Asana's globally unique identifiers, decimal strings that can exceed 64 bits.
	gidPattern   = regexp.MustCompile(`^[0-9]{1,64}$`)
	emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

	// taskSummaryFields are the opt_fields of a listed task; related objects keep to gid and name, which
	// Asana returns without additional OAuth scopes.
	taskSummaryFields = []string{
		"name", "resource_subtype", "completed", "completed_at", "assignee.name", "due_on", "due_at", "start_on",
		"memberships.project.name", "memberships.section.name", "parent.name", "permalink_url", "created_at", "modified_at",
	}
	// taskDetailFields add the workspace, plain-text notes, and custom field values for getTask and updateTask.
	taskDetailFields = append(append([]string(nil), taskSummaryFields...),
		"workspace.name", "notes", "custom_fields.name", "custom_fields.resource_subtype", "custom_fields.display_value",
		"custom_fields.text_value", "custom_fields.number_value", "custom_fields.enum_value.name", "custom_fields.multi_enum_values.name",
	)
)

// Task is the connector's view of one Asana task. Asana's own status vocabulary is the completed flag;
// section membership carries the workflow state, so moving a task between sections is its status change.
type Task struct {
	// ID is the task's Asana gid, a decimal string.
	ID string `json:"id"`
	// Name is the task name.
	Name string `json:"name"`
	// ResourceSubtype is default_task, milestone, approval, or another Asana task subtype.
	ResourceSubtype string `json:"resourceSubtype,omitempty"`
	// IsCompleted reports a completed task.
	IsCompleted bool `json:"isCompleted"`
	// CompletedAt is when the task was completed, or nil while it is incomplete.
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	// Assignee is the assigned user, or nil for an unassigned task. Email addresses are never read.
	Assignee *UserReference `json:"assignee,omitempty"`
	// DueOn is the due date as YYYY-MM-DD, or empty.
	DueOn string `json:"dueOn,omitempty"`
	// DueAt is the due date and time, set only for a task with a due time.
	DueAt *time.Time `json:"dueAt,omitempty"`
	// StartOn is the start date as YYYY-MM-DD, or empty.
	StartOn string `json:"startOn,omitempty"`
	// Memberships lists up to 20 projects that hold the task and the section of each.
	Memberships []TaskMembership `json:"memberships,omitempty"`
	// Parent is the parent task of a subtask, or nil.
	Parent *TaskReference `json:"parent,omitempty"`
	// Workspace is the task's workspace, returned by getTask and updateTask only.
	Workspace *WorkspaceReference `json:"workspace,omitempty"`
	// Notes is the plain-text description, returned by getTask and updateTask only, at most 65536 characters.
	Notes string `json:"notes,omitempty"`
	// IsNotesTruncated reports that Notes stopped at 65536 characters.
	IsNotesTruncated bool `json:"isNotesTruncated,omitempty"`
	// CustomFields lists up to 50 custom field values, returned by getTask and updateTask only.
	CustomFields []CustomFieldValue `json:"customFields,omitempty"`
	// PermalinkURL is the task's Asana web URL.
	PermalinkURL string `json:"permalinkUrl,omitempty"`
	// CreatedAt is when the task was created.
	CreatedAt time.Time `json:"createdAt"`
	// ModifiedAt is when the task, or an association such as a project or comment, last changed.
	ModifiedAt time.Time `json:"modifiedAt"`
}

// TaskMembership is one project that holds a task, with the task's section in it.
type TaskMembership struct {
	// Project is the project.
	Project ProjectReference `json:"project"`
	// Section is the task's section in the project, or nil when Asana reports none.
	Section *SectionReference `json:"section,omitempty"`
}

// UserReference identifies an Asana user by gid and display name.
type UserReference struct {
	// ID is the user's gid.
	ID string `json:"id"`
	// Name is the user's display name.
	Name string `json:"name,omitempty"`
}

// ProjectReference identifies an Asana project.
type ProjectReference struct {
	// ID is the project's gid.
	ID string `json:"id"`
	// Name is the project name.
	Name string `json:"name,omitempty"`
}

// SectionReference identifies a section of a project.
type SectionReference struct {
	// ID is the section's gid.
	ID string `json:"id"`
	// Name is the section name, such as Approved.
	Name string `json:"name,omitempty"`
}

// WorkspaceReference identifies an Asana workspace or organization.
type WorkspaceReference struct {
	// ID is the workspace's gid.
	ID string `json:"id"`
	// Name is the workspace name.
	Name string `json:"name,omitempty"`
}

// TaskReference identifies another task, such as a parent.
type TaskReference struct {
	// ID is the task's gid.
	ID string `json:"id"`
	// Name is the task name.
	Name string `json:"name,omitempty"`
}

// CustomFieldValue is one custom field's value on a task. DisplayValue is Asana's text rendering for every
// field type; the typed value is set for text, number, enum, and multi-enum fields.
type CustomFieldValue struct {
	// ID is the custom field's gid.
	ID string `json:"id"`
	// Name is the custom field name.
	Name string `json:"name,omitempty"`
	// Type is Asana's resource_subtype, such as text, number, enum, multi_enum, date, or people.
	Type string `json:"type,omitempty"`
	// DisplayValue is the value as Asana displays it, or empty for an unset field.
	DisplayValue string `json:"displayValue,omitempty"`
	// TextValue is a text field's value.
	TextValue *string `json:"textValue,omitempty"`
	// NumberValue is a number field's value.
	NumberValue *float64 `json:"numberValue,omitempty"`
	// EnumOption is an enum field's selected option.
	EnumOption *EnumOptionReference `json:"enumOption,omitempty"`
	// MultiEnumOptions lists a multi-enum field's selected options.
	MultiEnumOptions []EnumOptionReference `json:"multiEnumOptions,omitempty"`
}

// EnumOptionReference identifies one option of an enum or multi-enum custom field.
type EnumOptionReference struct {
	// ID is the enum option's gid.
	ID string `json:"id"`
	// Name is the option name, such as High.
	Name string `json:"name,omitempty"`
}

// CustomFieldValueInput sets one custom field. Set exactly one of Text, Number, EnumOptionID, and
// MultiEnumOptionIDs, matching the field's type; other custom field types are not supported.
type CustomFieldValueInput struct {
	// CustomFieldID is the custom field's gid.
	CustomFieldID string `json:"customFieldId"`
	// Text sets a text field.
	Text *string `json:"text,omitempty"`
	// Number sets a number field.
	Number *float64 `json:"number,omitempty"`
	// EnumOptionID selects an enum field's option by its gid, not its name.
	EnumOptionID string `json:"enumOptionId,omitempty"`
	// MultiEnumOptionIDs selects a multi-enum field's options by gid, replacing the current selection.
	MultiEnumOptionIDs []string `json:"multiEnumOptionIds,omitempty"`
}

type taskResource struct {
	GID             string                   `json:"gid"`
	Name            string                   `json:"name"`
	ResourceSubtype string                   `json:"resource_subtype"`
	Completed       bool                     `json:"completed"`
	CompletedAt     *string                  `json:"completed_at"`
	Assignee        *namedResource           `json:"assignee"`
	DueOn           *string                  `json:"due_on"`
	DueAt           *string                  `json:"due_at"`
	StartOn         *string                  `json:"start_on"`
	Memberships     []membershipResource     `json:"memberships"`
	Parent          *namedResource           `json:"parent"`
	Workspace       *namedResource           `json:"workspace"`
	Notes           string                   `json:"notes"`
	CustomFields    []customFieldValueResult `json:"custom_fields"`
	PermalinkURL    string                   `json:"permalink_url"`
	CreatedAt       string                   `json:"created_at"`
	ModifiedAt      string                   `json:"modified_at"`
}

type namedResource struct {
	GID  string `json:"gid"`
	Name string `json:"name"`
}

type membershipResource struct {
	Project *namedResource `json:"project"`
	Section *namedResource `json:"section"`
}

type customFieldValueResult struct {
	GID             string          `json:"gid"`
	Name            string          `json:"name"`
	ResourceSubtype string          `json:"resource_subtype"`
	DisplayValue    *string         `json:"display_value"`
	TextValue       *string         `json:"text_value"`
	NumberValue     *float64        `json:"number_value"`
	EnumValue       *namedResource  `json:"enum_value"`
	MultiEnumValues []namedResource `json:"multi_enum_values"`
}

// decodeTask converts one Asana task resource. includesDetail keeps the workspace, notes, and custom fields.
func decodeTask(resource taskResource, includesDetail bool) (Task, error) {
	if !gidPattern.MatchString(resource.GID) {
		return Task{}, errors.New("task gid is invalid")
	}
	task := Task{
		ID: resource.GID, Name: resource.Name, ResourceSubtype: resource.ResourceSubtype, IsCompleted: resource.Completed,
		Assignee: resource.Assignee.userView(), Parent: resource.Parent.taskView(), PermalinkURL: resource.PermalinkURL,
	}
	var err error
	if task.CompletedAt, err = parseOptionalAsanaTime(resource.CompletedAt); err != nil {
		return Task{}, fmt.Errorf("task completed time: %w", err)
	}
	if task.DueAt, err = parseOptionalAsanaTime(resource.DueAt); err != nil {
		return Task{}, fmt.Errorf("task due time: %w", err)
	}
	if task.DueOn, err = decodeOptionalDate(resource.DueOn); err != nil {
		return Task{}, fmt.Errorf("task due date: %w", err)
	}
	if task.StartOn, err = decodeOptionalDate(resource.StartOn); err != nil {
		return Task{}, fmt.Errorf("task start date: %w", err)
	}
	if task.CreatedAt, err = parseAsanaTime(resource.CreatedAt); err != nil {
		return Task{}, fmt.Errorf("task created time: %w", err)
	}
	if task.ModifiedAt, err = parseAsanaTime(resource.ModifiedAt); err != nil {
		return Task{}, fmt.Errorf("task modified time: %w", err)
	}
	for _, membership := range resource.Memberships {
		if membership.Project == nil || !gidPattern.MatchString(membership.Project.GID) || len(task.Memberships) == maximumListedMemberships {
			continue
		}
		view := TaskMembership{Project: ProjectReference{ID: membership.Project.GID, Name: membership.Project.Name}}
		if membership.Section != nil && gidPattern.MatchString(membership.Section.GID) {
			view.Section = &SectionReference{ID: membership.Section.GID, Name: membership.Section.Name}
		}
		task.Memberships = append(task.Memberships, view)
	}
	if !includesDetail {
		return task, nil
	}
	if resource.Workspace != nil && gidPattern.MatchString(resource.Workspace.GID) {
		task.Workspace = &WorkspaceReference{ID: resource.Workspace.GID, Name: resource.Workspace.Name}
	}
	task.Notes, task.IsNotesTruncated = truncateCharacters(resource.Notes, maximumTextCharacters)
	for _, field := range resource.CustomFields {
		if !gidPattern.MatchString(field.GID) || len(task.CustomFields) == maximumListedCustomFields {
			continue
		}
		task.CustomFields = append(task.CustomFields, field.view())
	}
	return task, nil
}

// SectionID returns the task's section in projectID, or "" when the task is not in that project.
func (task Task) SectionID(projectID string) string {
	for _, membership := range task.Memberships {
		if membership.Project.ID == projectID && membership.Section != nil {
			return membership.Section.ID
		}
	}
	return ""
}

// IsInProject reports whether the task belongs to projectID.
func (task Task) IsInProject(projectID string) bool {
	for _, membership := range task.Memberships {
		if membership.Project.ID == projectID {
			return true
		}
	}
	return false
}

func (field customFieldValueResult) view() CustomFieldValue {
	view := CustomFieldValue{
		ID: field.GID, Name: field.Name, Type: field.ResourceSubtype, TextValue: field.TextValue, NumberValue: field.NumberValue,
	}
	if field.DisplayValue != nil {
		view.DisplayValue = *field.DisplayValue
	}
	if field.EnumValue != nil && gidPattern.MatchString(field.EnumValue.GID) {
		view.EnumOption = &EnumOptionReference{ID: field.EnumValue.GID, Name: field.EnumValue.Name}
	}
	for _, option := range field.MultiEnumValues {
		if gidPattern.MatchString(option.GID) {
			view.MultiEnumOptions = append(view.MultiEnumOptions, EnumOptionReference{ID: option.GID, Name: option.Name})
		}
	}
	return view
}

func (resource *namedResource) userView() *UserReference {
	if resource == nil || !gidPattern.MatchString(resource.GID) {
		return nil
	}
	return &UserReference{ID: resource.GID, Name: resource.Name}
}

func (resource *namedResource) taskView() *TaskReference {
	if resource == nil || !gidPattern.MatchString(resource.GID) {
		return nil
	}
	return &TaskReference{ID: resource.GID, Name: resource.Name}
}

// parseAsanaTime accepts Asana's ISO 8601 UTC timestamp, such as 2026-09-30T16:15:00.000Z; empty is zero.
func parseAsanaTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, errors.New("timestamp is not an ISO 8601 date-time")
	}
	return parsed.UTC(), nil
}

func parseOptionalAsanaTime(value *string) (*time.Time, error) {
	if value == nil || *value == "" {
		return nil, nil
	}
	parsed, err := parseAsanaTime(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func decodeOptionalDate(value *string) (string, error) {
	if value == nil || *value == "" {
		return "", nil
	}
	if _, err := time.Parse(dueDateLayout, *value); err != nil {
		return "", errors.New("date is not YYYY-MM-DD")
	}
	return *value, nil
}

// truncateCharacters keeps at most maximumCharacters runes.
func truncateCharacters(value string, maximumCharacters int) (string, bool) {
	if utf8.RuneCountInString(value) <= maximumCharacters {
		return value, false
	}
	runes := []rune(value)
	return string(runes[:maximumCharacters]), true
}

// validateGID checks one Asana gid used in a path or body.
func validateGID(value string, fieldName string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !gidPattern.MatchString(trimmed) {
		return "", fmt.Errorf("%s must be an Asana gid, a decimal string such as 1204567890123456", fieldName)
	}
	return trimmed, nil
}

// validateOptionalGID checks a gid that may be blank.
func validateOptionalGID(value string, fieldName string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return validateGID(value, fieldName)
}

// validateAssignee accepts me, a user gid, or an email address of a workspace member.
func validateAssignee(value string, fieldName string) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == assigneeMe || gidPattern.MatchString(trimmed):
		return trimmed, nil
	case len(trimmed) <= maximumEmailBytes && emailPattern.MatchString(trimmed):
		return trimmed, nil
	default:
		return "", fmt.Errorf("%s must be me, a user gid, or an email address", fieldName)
	}
}

// validateDueDate checks a YYYY-MM-DD calendar date.
func validateDueDate(value string, fieldName string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if _, err := time.Parse(dueDateLayout, trimmed); err != nil {
		return "", fmt.Errorf("%s must be a YYYY-MM-DD date such as 2026-10-15", fieldName)
	}
	return trimmed, nil
}

// validateTaskName checks a required one-line task name.
func validateTaskName(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch {
	case trimmed == "":
		return "", errors.New("name is required")
	case !utf8.ValidString(trimmed):
		return "", errors.New("name must be valid UTF-8")
	case utf8.RuneCountInString(trimmed) > maximumTaskNameCharacters:
		return "", fmt.Errorf("name cannot exceed %d characters", maximumTaskNameCharacters)
	}
	for _, character := range trimmed {
		if character < ' ' || character == 0x7f {
			return "", errors.New("name must be one line without control characters")
		}
	}
	return trimmed, nil
}

// validatePlainText checks notes or a comment; line breaks and tabs are kept, other control characters
// are rejected.
func validatePlainText(value string, fieldName string, isRequired bool) error {
	switch {
	case isRequired && strings.TrimSpace(value) == "":
		return errors.New(fieldName + " is required")
	case !utf8.ValidString(value):
		return errors.New(fieldName + " must be valid UTF-8")
	case utf8.RuneCountInString(value) > maximumTextCharacters:
		return fmt.Errorf("%s cannot exceed %d characters", fieldName, maximumTextCharacters)
	}
	for _, character := range value {
		if (character < ' ' && character != '\n' && character != '\t' && character != '\r') || character == 0x7f {
			return errors.New(fieldName + " cannot contain control characters")
		}
	}
	return nil
}

// buildCustomFieldValues converts custom field inputs to Asana's map keyed by custom field gid.
func buildCustomFieldValues(inputs []CustomFieldValueInput) (map[string]any, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	if len(inputs) > maximumCustomFieldValues {
		return nil, fmt.Errorf("customFields accepts at most %d values", maximumCustomFieldValues)
	}
	values := make(map[string]any, len(inputs))
	for _, input := range inputs {
		fieldID, err := validateGID(input.CustomFieldID, "customFieldId")
		if err != nil {
			return nil, err
		}
		if _, isDuplicate := values[fieldID]; isDuplicate {
			return nil, fmt.Errorf("custom field %s is set twice", fieldID)
		}
		value, err := customFieldWireValue(input)
		if err != nil {
			return nil, fmt.Errorf("custom field %s: %w", fieldID, err)
		}
		values[fieldID] = value
	}
	return values, nil
}

func customFieldWireValue(input CustomFieldValueInput) (any, error) {
	setCount := 0
	for _, isSet := range []bool{input.Text != nil, input.Number != nil, strings.TrimSpace(input.EnumOptionID) != "", input.MultiEnumOptionIDs != nil} {
		if isSet {
			setCount++
		}
	}
	if setCount != 1 {
		return nil, errors.New("set exactly one of text, number, enumOptionId, and multiEnumOptionIds")
	}
	switch {
	case input.Text != nil:
		if err := validatePlainText(*input.Text, "text", false); err != nil {
			return nil, err
		}
		return *input.Text, nil
	case input.Number != nil:
		if math.IsNaN(*input.Number) || math.IsInf(*input.Number, 0) {
			return nil, errors.New("number must be finite")
		}
		return *input.Number, nil
	case input.MultiEnumOptionIDs != nil:
		if len(input.MultiEnumOptionIDs) > maximumEnumOptionIDs {
			return nil, fmt.Errorf("multiEnumOptionIds accepts at most %d options", maximumEnumOptionIDs)
		}
		optionIDs := make([]string, 0, len(input.MultiEnumOptionIDs))
		for _, optionID := range input.MultiEnumOptionIDs {
			validated, err := validateGID(optionID, "multiEnumOptionIds")
			if err != nil {
				return nil, err
			}
			optionIDs = append(optionIDs, validated)
		}
		return optionIDs, nil
	default:
		return validateGID(input.EnumOptionID, "enumOptionId")
	}
}

// decodeData unmarshals Asana's {"data": ...} envelope.
func decodeData[T any](body []byte) (T, error) {
	var envelope dataEnvelope[*T]
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data == nil {
		var zero T
		return zero, errors.New("response has no data object")
	}
	return *envelope.Data, nil
}
