// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateProjectInput supplies the fields required by Harvest project creation.
type CreateProjectInput struct {
	// ClientID identifies the owning Harvest client.
	ClientID int64 `json:"clientId"`
	// Name is the nonempty project name.
	Name string `json:"name"`
	// IsBillable selects whether work can be invoiced.
	IsBillable bool `json:"isBillable"`
	// BillBy is Project, Tasks, People, or none.
	BillBy string `json:"billBy"`
	// BudgetBy is project, project_cost, task, task_fees, person, or none.
	BudgetBy string `json:"budgetBy"`
	// Code is an optional project code.
	Code string `json:"code,omitempty"`
	// Notes are optional project notes.
	Notes string `json:"notes,omitempty"`
}

// UpdateProjectInput identifies a project and absolute fields to set.
type UpdateProjectInput struct {
	// ID is the positive Harvest project ID.
	ID int64 `json:"id"`
	// Name replaces the project name when supplied.
	Name *string `json:"name,omitempty"`
	// IsActive sets whether the project is active or archived.
	IsActive *bool `json:"isActive,omitempty"`
	// Code replaces the project code when supplied.
	Code *string `json:"code,omitempty"`
	// Notes replace project notes when supplied.
	Notes *string `json:"notes,omitempty"`
}

// DeleteProjectInput identifies one project for deletion.
type DeleteProjectInput struct {
	// ID is the positive Harvest project ID.
	ID int64 `json:"id"`
}

// DeletedProject identifies a project whose deletion Harvest confirmed.
type DeletedProject struct {
	// ID is the deleted Harvest project ID.
	ID int64 `json:"id"`
}

// CreateProjectOperation creates a project with a bounded number of sends.
type CreateProjectOperation struct{ client *Client }

// CreateProject binds the project creation Mutation to this client.
func (client *Client) CreateProject() CreateProjectOperation { return CreateProjectOperation{client} }

// Definition returns the generated project creation contract.
func (CreateProjectOperation) Definition() sdkgo.MutationDefinition { return CreateProjectDefinition }

// IdempotencyKey records the Dex call identity; Harvest does not deduplicate it.
func (CreateProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a project under the limited-repeat fence.
func (operation CreateProjectOperation) Invoke(call sdkgo.Call, input CreateProjectInput) sdkgo.MutationAttempt[Project] {
	if input.ClientID <= 0 || strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 ||
		!validHarvestBillBy(input.BillBy) || !validHarvestBudgetBy(input.BudgetBy) || len(input.Code) > 255 || len(input.Notes) > 10000 {
		return harvestMutationFailure[Project]("createProject", CreateProjectBranchDefect, sdkgo.FailureValidation, "project client, name, billing, budget, code, or notes are invalid")
	}
	payload, err := json.Marshal(struct {
		ClientID   int64  `json:"client_id"`
		Name       string `json:"name"`
		IsBillable bool   `json:"is_billable"`
		BillBy     string `json:"bill_by"`
		BudgetBy   string `json:"budget_by"`
		Code       string `json:"code,omitempty"`
		Notes      string `json:"notes,omitempty"`
	}{ClientID: input.ClientID, Name: input.Name, IsBillable: input.IsBillable, BillBy: input.BillBy, BudgetBy: input.BudgetBy, Code: input.Code, Notes: input.Notes})
	if err != nil {
		return harvestMutationFailure[Project]("createProject", CreateProjectBranchDefect, sdkgo.FailureValidation, "project input cannot be encoded")
	}
	return invokeLimitedHarvestCreate(operation.client, call, "createProject", CreateProjectDefinition.StepDefaults.ExecuteMethodTimeout, "/projects", payload, CreateProjectBranchCreated, CreateProjectBranchProviderRejected, CreateProjectBranchDefect, decodeProject, func(project Project) string { return strconv.FormatInt(project.ID, 10) })
}

// UpdateProjectOperation changes absolute project fields.
type UpdateProjectOperation struct{ client *Client }

// UpdateProject binds the project update Mutation to this client.
func (client *Client) UpdateProject() UpdateProjectOperation { return UpdateProjectOperation{client} }

// Definition returns the generated project update contract.
func (UpdateProjectOperation) Definition() sdkgo.MutationDefinition { return UpdateProjectDefinition }

// IdempotencyKey records the stable Dex call identity for this update.
func (UpdateProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies an idempotent PATCH to the project.
func (operation UpdateProjectOperation) Invoke(call sdkgo.Call, input UpdateProjectInput) sdkgo.MutationAttempt[Project] {
	if input.ID <= 0 || (input.Name == nil && input.IsActive == nil && input.Code == nil && input.Notes == nil) ||
		(input.Name != nil && (strings.TrimSpace(*input.Name) == "" || len(*input.Name) > 255)) ||
		(input.Code != nil && len(*input.Code) > 255) || (input.Notes != nil && len(*input.Notes) > 10000) {
		return harvestMutationFailure[Project]("updateProject", UpdateProjectBranchDefect, sdkgo.FailureValidation, "project ID or update fields are invalid")
	}
	payload, err := json.Marshal(struct {
		Name     *string `json:"name,omitempty"`
		IsActive *bool   `json:"is_active,omitempty"`
		Code     *string `json:"code,omitempty"`
		Notes    *string `json:"notes,omitempty"`
	}{Name: input.Name, IsActive: input.IsActive, Code: input.Code, Notes: input.Notes})
	if err != nil {
		return harvestMutationFailure[Project]("updateProject", UpdateProjectBranchDefect, sdkgo.FailureValidation, "project update cannot be encoded")
	}
	return invokeSafeHarvestWrite(operation.client, call, "updateProject", http.MethodPatch, "/projects/"+strconv.FormatInt(input.ID, 10), payload, UpdateProjectBranchUpdated, UpdateProjectBranchNotFound, UpdateProjectBranchProviderRejected, decodeProject)
}

// DeleteProjectOperation deletes a project without tracked records.
type DeleteProjectOperation struct{ client *Client }

// DeleteProject binds the project deletion Mutation to this client.
func (client *Client) DeleteProject() DeleteProjectOperation { return DeleteProjectOperation{client} }

// Definition returns the generated project deletion contract.
func (DeleteProjectOperation) Definition() sdkgo.MutationDefinition { return DeleteProjectDefinition }

// IdempotencyKey records the stable Dex call identity for this deletion.
func (DeleteProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the project with a safely repeatable DELETE.
func (operation DeleteProjectOperation) Invoke(call sdkgo.Call, input DeleteProjectInput) sdkgo.MutationAttempt[DeletedProject] {
	if input.ID <= 0 {
		return harvestMutationFailure[DeletedProject]("deleteProject", DeleteProjectBranchDefect, sdkgo.FailureValidation, "project ID must be positive")
	}
	decode := func(_ []byte) (DeletedProject, error) { return DeletedProject{ID: input.ID}, nil }
	return invokeSafeHarvestWrite(operation.client, call, "deleteProject", http.MethodDelete, "/projects/"+strconv.FormatInt(input.ID, 10), nil, DeleteProjectBranchDeleted, DeleteProjectBranchNotFound, DeleteProjectBranchProviderRejected, decode)
}

func validHarvestBillBy(value string) bool {
	switch value {
	case "Project", "Tasks", "People", "none":
		return true
	}
	return false
}

func validHarvestBudgetBy(value string) bool {
	switch value {
	case "project", "project_cost", "task", "task_fees", "person", "none":
		return true
	}
	return false
}
