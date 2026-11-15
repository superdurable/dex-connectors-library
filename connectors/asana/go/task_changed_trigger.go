// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// TaskChangeAction is the Asana Events API action of one taskChanged delivery.
type TaskChangeAction string

const (
	// TaskChangeAdded is a task added to the watched project, one of its sections, or a parent task: a new
	// task, a task moved into a section, or a subtask added to a task. Parent names the container.
	TaskChangeAdded TaskChangeAction = "added"
	// TaskChangeChanged is a field change on a task, such as completed, assignee, due_on, or name;
	// ChangedField names the field.
	TaskChangeChanged TaskChangeAction = "changed"
	// TaskChangeRemoved is a task removed from the project, one of its sections, or a parent task, such as
	// the section a task left when it moved. Parent names the container.
	TaskChangeRemoved TaskChangeAction = "removed"
	// TaskChangeDeleted is a task moved to the Asana trash.
	TaskChangeDeleted TaskChangeAction = "deleted"
	// TaskChangeUndeleted is a task restored from the Asana trash.
	TaskChangeUndeleted TaskChangeAction = "undeleted"
)

// TaskChangedTriggerConfiguration is the taskChanged binding configuration that the project picker saves
// in Dex Web. Changing the project of a binding that already polled makes its next poll record a new
// baseline for the new project.
type TaskChangedTriggerConfiguration struct {
	// WorkspaceID is the gid of the project's workspace, kept for the picker only.
	WorkspaceID string `json:"workspaceId,omitempty"`
	// ProjectID is the gid of the watched project, a decimal string such as 1201000000000001; required.
	ProjectID string `json:"projectId"`
	// ProjectName is the picked project's name, kept for display only.
	ProjectName string `json:"projectName,omitempty"`
}

// Validate requires ProjectID to be an Asana gid and WorkspaceID to be blank or a gid.
func (configuration TaskChangedTriggerConfiguration) Validate() error {
	if !gidPattern.MatchString(configuration.ProjectID) {
		return errors.New("taskChanged projectId must be an Asana project gid, digits only")
	}
	if configuration.WorkspaceID != "" && !gidPattern.MatchString(configuration.WorkspaceID) {
		return errors.New("taskChanged workspaceId must be blank or an Asana workspace gid")
	}
	return nil
}

// TaskChangedEvent is one task event from the watched project's Asana event stream. Asana events are
// compact: they name the task and what happened, not the task's current state, so a Flow reads the task
// with getTask when it needs more.
type TaskChangedEvent struct {
	// ProjectID is the watched project.
	ProjectID string `json:"projectId"`
	// Action is what happened to the task; see TaskChangeAction.
	Action TaskChangeAction `json:"action"`
	// Task is the task or subtask the event is about, with its name as of the poll.
	Task TaskReference `json:"task"`
	// TaskSubtype is Asana's resource_subtype of the task, such as default_task, milestone, or approval.
	TaskSubtype string `json:"taskSubtype,omitempty"`
	// ChangedField is the changed task field, such as completed or assignee; set only for changed.
	ChangedField string `json:"changedField,omitempty"`
	// ChangeAction is how ChangedField changed: changed for a value, or added or removed for a list entry
	// such as a follower or a tag. Set only for changed.
	ChangeAction string `json:"changeAction,omitempty"`
	// Parent is the project, section, or task the task was added to or removed from; nil for other actions.
	Parent *EventParentReference `json:"parent,omitempty"`
	// User is the Asana user who made the change; nil for a change Asana made itself, such as a rule.
	User *UserReference `json:"user,omitempty"`
	// OccurredAt is when Asana recorded the change, as an RFC 3339 timestamp.
	OccurredAt string `json:"occurredAt"`
}

// EventParentReference identifies the container of an added or removed task.
type EventParentReference struct {
	// ID is the container's gid.
	ID string `json:"id"`
	// ResourceType is project, section, or task.
	ResourceType string `json:"resourceType"`
	// Name is the container's name, as of the poll.
	Name string `json:"name,omitempty"`
}

func isKnownTaskChangeAction(action TaskChangeAction) bool {
	switch action {
	case TaskChangeAdded, TaskChangeChanged, TaskChangeRemoved, TaskChangeDeleted, TaskChangeUndeleted:
		return true
	default:
		return false
	}
}

// parseEventTime reads Asana's created_at, falling back to now for a missing or unparsable time.
func parseEventTime(value string, now time.Time) time.Time {
	if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value)); err == nil {
		return parsed.UTC()
	}
	return now.UTC()
}

func taskChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("asana taskChanged configuration: %w", err)
}
