// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	createTaskOperationID    = "createTask"
	createTaskFailureSubject = "task"
)

// createdTaskFields are the opt_fields of the created task Asana returns.
var createdTaskFields = []string{"name", "permalink_url", "created_at"}

// CreateTaskInput describes one task. Set exactly one of ProjectID and WorkspaceID; SectionID requires
// ProjectID.
type CreateTaskInput struct {
	// Name is the one-line task name, at most 1024 characters.
	Name string `json:"name"`
	// Notes is the plain-text description, at most 65536 characters; blank sends none.
	Notes string `json:"notes,omitempty"`
	// ProjectID adds the task to this project gid; its workspace becomes the task's workspace.
	ProjectID string `json:"projectId,omitempty"`
	// SectionID places the task in this section of ProjectID; blank uses the project's default section.
	SectionID string `json:"sectionId,omitempty"`
	// WorkspaceID creates the task in this workspace gid without a project.
	WorkspaceID string `json:"workspaceId,omitempty"`
	// AssigneeID assigns the task to me, a user gid, or the email address of a workspace member; blank
	// leaves it unassigned.
	AssigneeID string `json:"assigneeId,omitempty"`
	// DueOn is the due date as YYYY-MM-DD; blank sends none.
	DueOn string `json:"dueOn,omitempty"`
	// CustomFields sets up to 20 text, number, enum, or multi-enum custom field values.
	CustomFields []CustomFieldValueInput `json:"customFields,omitempty"`
}

// CreateTaskOutput identifies the created task. On providerRejected and uncertain, TaskID is empty and the
// requested name and container are echoed so the application can reconcile.
type CreateTaskOutput struct {
	// TaskID is the new task's gid.
	TaskID string `json:"taskId,omitempty"`
	// Name echoes the requested name.
	Name string `json:"name"`
	// ProjectID echoes the requested project.
	ProjectID string `json:"projectId,omitempty"`
	// SectionID echoes the requested section.
	SectionID string `json:"sectionId,omitempty"`
	// WorkspaceID echoes the requested workspace.
	WorkspaceID string `json:"workspaceId,omitempty"`
	// PermalinkURL is the new task's Asana web URL.
	PermalinkURL string `json:"permalinkUrl,omitempty"`
	// CreatedAt is when Asana created the task.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}

// CreateTaskOperation implements the createTask Mutation.
type CreateTaskOperation struct{ client *Client }

type createTaskFields struct {
	Name         string                 `json:"name"`
	Notes        string                 `json:"notes,omitempty"`
	Projects     []string               `json:"projects,omitempty"`
	Memberships  []createTaskMembership `json:"memberships,omitempty"`
	Workspace    string                 `json:"workspace,omitempty"`
	Assignee     string                 `json:"assignee,omitempty"`
	DueOn        string                 `json:"due_on,omitempty"`
	CustomFields map[string]any         `json:"custom_fields,omitempty"`
}

type createTaskMembership struct {
	Project string `json:"project"`
	Section string `json:"section"`
}

type createdTaskResource struct {
	GID          string `json:"gid"`
	PermalinkURL string `json:"permalink_url"`
	CreatedAt    string `json:"created_at"`
}

// Definition returns the immutable connector operation definition.
func (CreateTaskOperation) Definition() sdkgo.MutationDefinition { return CreateTaskDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID.
// Asana accepts no idempotency key, so it is never sent and cannot deduplicate a repeated create.
func (CreateTaskOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTaskInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one task and never resends a request Asana may have received.
func (operation CreateTaskOperation) Invoke(call sdkgo.Call, input CreateTaskInput) sdkgo.MutationAttempt[CreateTaskOutput] {
	client := operation.client
	fields, requested, err := buildCreateTaskFields(input)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateTaskBranchDefect, requested, failurePointer(createTaskOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, startFailure := client.startSession(call, createTaskOperationID)
	if startFailure != nil {
		return sdkgo.NewMutationBranch(CreateTaskBranchDefect, requested, startFailure, sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, asanaRequest{
		method: http.MethodPost, path: "/tasks", query: url.Values{"opt_fields": {strings.Join(createdTaskFields, ",")}},
		payload: dataEnvelope[createTaskFields]{Data: fields},
	})
	classification := client.classifyUnkeyedWrite(createTaskOperationID, createTaskFailureSubject, result)
	receipt := client.receipt(session, "")
	switch classification.outcome {
	case writeAccepted:
	case writeRetry:
		return sdkgo.NewMutationRetry[CreateTaskOutput](classification.failure, classification.retryAfter)
	case writeDefect:
		return sdkgo.NewMutationBranch(CreateTaskBranchDefect, requested, &classification.failure, receipt)
	case writeUncertain:
		return sdkgo.NewMutationUncertain(requested, classification.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(CreateTaskBranchProviderRejected, requested, &classification.failure, receipt)
	}
	created, err := decodeData[createdTaskResource](result.response.body)
	if err != nil || !gidPattern.MatchString(created.GID) {
		return sdkgo.NewMutationUncertain(requested, newFailure(createTaskOperationID, sdkgo.FailureProtocol, "Asana accepted the task but returned an unusable task reference"), receipt)
	}
	output := requested
	output.TaskID, output.PermalinkURL = created.GID, created.PermalinkURL
	// The task exists either way; an unparseable timestamp only leaves CreatedAt nil.
	if createdAt, err := parseOptionalAsanaTime(&created.CreatedAt); err == nil {
		output.CreatedAt = createdAt
	}
	receipt.ProviderObjectID = created.GID
	return sdkgo.NewMutationBranch(CreateTaskBranchCreated, output, nil, receipt)
}

// buildCreateTaskFields validates input and returns the request plus the echo used by every branch.
func buildCreateTaskFields(input CreateTaskInput) (createTaskFields, CreateTaskOutput, error) {
	requested := CreateTaskOutput{
		Name: strings.TrimSpace(input.Name), ProjectID: strings.TrimSpace(input.ProjectID),
		SectionID: strings.TrimSpace(input.SectionID), WorkspaceID: strings.TrimSpace(input.WorkspaceID),
	}
	name, err := validateTaskName(input.Name)
	if err != nil {
		return createTaskFields{}, requested, err
	}
	fields := createTaskFields{Name: name}
	if fields.Notes, err = validatedNotes(input.Notes); err != nil {
		return createTaskFields{}, requested, err
	}
	switch {
	case (requested.ProjectID == "") == (requested.WorkspaceID == ""):
		return createTaskFields{}, requested, errors.New("set exactly one of projectId and workspaceId")
	case requested.SectionID != "" && requested.ProjectID == "":
		return createTaskFields{}, requested, errors.New("sectionId requires projectId")
	}
	if requested.ProjectID != "" {
		if _, err := validateGID(requested.ProjectID, "projectId"); err != nil {
			return createTaskFields{}, requested, err
		}
	}
	if requested.WorkspaceID != "" {
		if fields.Workspace, err = validateGID(requested.WorkspaceID, "workspaceId"); err != nil {
			return createTaskFields{}, requested, err
		}
	}
	switch {
	case requested.SectionID != "":
		if _, err := validateGID(requested.SectionID, "sectionId"); err != nil {
			return createTaskFields{}, requested, err
		}
		fields.Memberships = []createTaskMembership{{Project: requested.ProjectID, Section: requested.SectionID}}
	case requested.ProjectID != "":
		fields.Projects = []string{requested.ProjectID}
	}
	if strings.TrimSpace(input.AssigneeID) != "" {
		if fields.Assignee, err = validateAssignee(input.AssigneeID, "assigneeId"); err != nil {
			return createTaskFields{}, requested, err
		}
	}
	if strings.TrimSpace(input.DueOn) != "" {
		if fields.DueOn, err = validateDueDate(input.DueOn, "dueOn"); err != nil {
			return createTaskFields{}, requested, err
		}
	}
	if fields.CustomFields, err = buildCustomFieldValues(input.CustomFields); err != nil {
		return createTaskFields{}, requested, err
	}
	return fields, requested, nil
}

// validatedNotes checks optional notes; whitespace-only notes send none.
func validatedNotes(notes string) (string, error) {
	if err := validatePlainText(notes, "notes", false); err != nil {
		return "", err
	}
	if strings.TrimSpace(notes) == "" {
		return "", nil
	}
	return notes, nil
}
