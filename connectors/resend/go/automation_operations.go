// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// AutomationStep defines a provider automation graph node.
type AutomationStep struct {
	// Key uniquely identifies the node within its graph.
	Key string `json:"key"`
	// Type is trigger, send_email, delay, wait_for_event, condition, contact_update, contact_delete, or add_to_segment.
	Type string `json:"type"`
	// Config is the provider-defined JSON for the selected type; do not include secrets.
	Config json.RawMessage `json:"config"`
}

// AutomationConnection links two graph nodes.
type AutomationConnection struct {
	// From is the source step key.
	From string `json:"from"`
	// To is the destination step key.
	To string `json:"to"`
	// Type is default, condition_met, condition_not_met, timeout, or event_received.
	Type string `json:"type,omitempty"`
}

// CreateAutomationInput creates a disabled or enabled automation graph.
type CreateAutomationInput struct {
	// Name is the automation name.
	Name string `json:"name"`
	// Status is enabled or disabled; blank uses the provider's disabled default.
	Status string `json:"status,omitempty"`
	// Steps contains 1 through 150 nodes including a trigger.
	Steps []AutomationStep `json:"steps"`
	// Connections links the graph nodes and may be empty.
	Connections []AutomationConnection `json:"connections"`
}

// GetAutomationInput selects an automation.
type GetAutomationInput struct {
	// ID is the automation ID.
	ID string `json:"id"`
}

// ListAutomationsInput selects a page, optionally by status.
type ListAutomationsInput struct {
	// Status is enabled or disabled; blank includes both.
	Status string `json:"status,omitempty"`
	// Limit is the page size from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive automation cursor.
	After string `json:"after,omitempty"`
}

// UpdateAutomationInput replaces selected absolute automation fields.
type UpdateAutomationInput struct {
	// ID is the automation ID.
	ID string `json:"id"`
	// Name replaces the name when non-nil.
	Name *string `json:"name,omitempty"`
	// Status replaces enabled or disabled state when non-nil.
	Status *string `json:"status,omitempty"`
	// Steps replaces the graph nodes together with Connections.
	Steps *[]AutomationStep `json:"steps,omitempty"`
	// Connections replaces graph edges together with Steps.
	Connections *[]AutomationConnection `json:"connections,omitempty"`
}

// DeleteAutomationInput selects an automation for removal.
type DeleteAutomationInput struct {
	// ID is the automation ID.
	ID string `json:"id"`
}

// DuplicateAutomationInput selects an automation to copy.
type DuplicateAutomationInput struct {
	// ID is the source automation ID.
	ID string `json:"id"`
}

// StopAutomationInput selects an automation to disable.
type StopAutomationInput struct {
	// ID is the automation ID.
	ID string `json:"id"`
}

// ListAutomationRunsInput selects a run page.
type ListAutomationRunsInput struct {
	// AutomationID is the automation ID.
	AutomationID string `json:"automationId"`
	// Status filters by a comma-separated list of running, completed, failed, cancelled.
	Status string `json:"status,omitempty"`
	// Limit is the page size from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive run cursor.
	After string `json:"after,omitempty"`
}

// GetAutomationRunInput selects one run.
type GetAutomationRunInput struct {
	// AutomationID is the automation ID.
	AutomationID string `json:"automationId"`
	// RunID is the run ID.
	RunID string `json:"runId"`
}

// AutomationReference identifies a created or changed automation.
type AutomationReference struct {
	// ID is the provider automation ID.
	ID string `json:"id"`
}

// Automation contains safe metadata without step configurations.
type Automation struct {
	// ID is the automation ID.
	ID string `json:"id"`
	// Name is the automation name.
	Name string `json:"name"`
	// Status is enabled or disabled.
	Status string `json:"status"`
	// CreatedAt is the creation timestamp.
	CreatedAt string `json:"created_at"`
	// UpdatedAt is the update timestamp.
	UpdatedAt string `json:"updated_at"`
}

// AutomationPage contains one automation page.
type AutomationPage struct {
	// Data contains safe automation metadata.
	Data []Automation `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// DeletedAutomation confirms one removal.
type DeletedAutomation struct {
	// ID is the removed automation ID.
	ID string `json:"id"`
	// Deleted confirms removal.
	Deleted bool `json:"deleted"`
}

// AutomationStatus reports the state after stopping.
type AutomationStatus struct {
	// ID is the automation ID.
	ID string `json:"id"`
	// Status is the resulting disabled state.
	Status string `json:"status"`
}

// AutomationRun contains run status without step outputs or errors.
type AutomationRun struct {
	// ID is the run ID.
	ID string `json:"id"`
	// Status is running, completed, failed, or cancelled.
	Status string `json:"status"`
	// StartedAt is the optional start timestamp.
	StartedAt string `json:"started_at"`
	// CompletedAt is the optional completion timestamp.
	CompletedAt string `json:"completed_at"`
	// CreatedAt is the creation timestamp.
	CreatedAt string `json:"created_at"`
}

// AutomationRunPage contains one run page.
type AutomationRunPage struct {
	// Data contains safe run status metadata.
	Data []AutomationRun `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// CreateAutomationOperation creates a graph with bounded resend.
type CreateAutomationOperation struct{ client *Client }

// GetAutomationOperation retrieves automation metadata.
type GetAutomationOperation struct{ client *Client }

// ListAutomationsOperation lists automation metadata.
type ListAutomationsOperation struct{ client *Client }

// UpdateAutomationOperation replaces absolute automation fields.
type UpdateAutomationOperation struct{ client *Client }

// DeleteAutomationOperation removes an automation.
type DeleteAutomationOperation struct{ client *Client }

// DuplicateAutomationOperation copies an automation with bounded resend.
type DuplicateAutomationOperation struct{ client *Client }

// StopAutomationOperation disables an automation.
type StopAutomationOperation struct{ client *Client }

// ListAutomationRunsOperation lists run status metadata.
type ListAutomationRunsOperation struct{ client *Client }

// GetAutomationRunOperation retrieves run status metadata.
type GetAutomationRunOperation struct{ client *Client }

// CreateAutomation binds graph creation to this client.
func (client *Client) CreateAutomation() CreateAutomationOperation {
	return CreateAutomationOperation{client}
}

// GetAutomation binds metadata retrieval to this client.
func (client *Client) GetAutomation() GetAutomationOperation { return GetAutomationOperation{client} }

// ListAutomations binds metadata listing to this client.
func (client *Client) ListAutomations() ListAutomationsOperation {
	return ListAutomationsOperation{client}
}

// UpdateAutomation binds graph and status updates to this client.
func (client *Client) UpdateAutomation() UpdateAutomationOperation {
	return UpdateAutomationOperation{client}
}

// DeleteAutomation binds removal to this client.
func (client *Client) DeleteAutomation() DeleteAutomationOperation {
	return DeleteAutomationOperation{client}
}

// DuplicateAutomation binds graph duplication to this client.
func (client *Client) DuplicateAutomation() DuplicateAutomationOperation {
	return DuplicateAutomationOperation{client}
}

// StopAutomation binds disablement to this client.
func (client *Client) StopAutomation() StopAutomationOperation {
	return StopAutomationOperation{client}
}

// ListAutomationRuns binds run listing to this client.
func (client *Client) ListAutomationRuns() ListAutomationRunsOperation {
	return ListAutomationRunsOperation{client}
}

// GetAutomationRun binds run retrieval to this client.
func (client *Client) GetAutomationRun() GetAutomationRunOperation {
	return GetAutomationRunOperation{client}
}

// Definition returns the generated createAutomation contract.
func (CreateAutomationOperation) Definition() sdkgo.MutationDefinition {
	return CreateAutomationDefinition
}

// Definition returns the generated getAutomation contract.
func (GetAutomationOperation) Definition() sdkgo.QueryDefinition { return GetAutomationDefinition }

// Definition returns the generated listAutomations contract.
func (ListAutomationsOperation) Definition() sdkgo.QueryDefinition { return ListAutomationsDefinition }

// Definition returns the generated updateAutomation contract.
func (UpdateAutomationOperation) Definition() sdkgo.MutationDefinition {
	return UpdateAutomationDefinition
}

// Definition returns the generated deleteAutomation contract.
func (DeleteAutomationOperation) Definition() sdkgo.MutationDefinition {
	return DeleteAutomationDefinition
}

// Definition returns the generated duplicateAutomation contract.
func (DuplicateAutomationOperation) Definition() sdkgo.MutationDefinition {
	return DuplicateAutomationDefinition
}

// Definition returns the generated stopAutomation contract.
func (StopAutomationOperation) Definition() sdkgo.MutationDefinition { return StopAutomationDefinition }

// Definition returns the generated listAutomationRuns contract.
func (ListAutomationRunsOperation) Definition() sdkgo.QueryDefinition {
	return ListAutomationRunsDefinition
}

// Definition returns the generated getAutomationRun contract.
func (GetAutomationRunOperation) Definition() sdkgo.QueryDefinition {
	return GetAutomationRunDefinition
}

// IdempotencyKey correlates possible graph creation attempts.
func (CreateAutomationOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateAutomationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute graph updates.
func (UpdateAutomationOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateAutomationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates harmless repeated deletion.
func (DeleteAutomationOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteAutomationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates bounded duplication attempts.
func (DuplicateAutomationOperation) IdempotencyKey(callID sdkgo.CallID, _ DuplicateAutomationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute disablement.
func (StopAutomationOperation) IdempotencyKey(callID sdkgo.CallID, _ StopAutomationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an automation graph and discards provider step data.
func (operation CreateAutomationOperation) Invoke(call sdkgo.Call, input CreateAutomationInput) sdkgo.MutationAttempt[AutomationReference] {
	if strings.TrimSpace(input.Name) == "" || !validAutomationStatus(input.Status, true) || !validAutomationGraph(input.Steps, input.Connections) {
		return mutationFailure[AutomationReference]("createAutomation", CreateAutomationBranchDefect, sdkgo.FailureValidation, "automation name, status, or graph is invalid")
	}
	fields := map[string]any{"name": input.Name, "steps": input.Steps, "connections": input.Connections}
	if input.Status != "" {
		fields["status"] = input.Status
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return mutationFailure[AutomationReference]("createAutomation", CreateAutomationBranchDefect, sdkgo.FailureValidation, "automation graph cannot be encoded")
	}
	request := resendLimitedCreate[AutomationReference]{client: operation.client, call: call, operation: "createAutomation", path: "/automations", body: body, timeout: CreateAutomationDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateAutomationBranchCreated, rejected: CreateAutomationBranchProviderRejected, defect: CreateAutomationBranchDefect, decode: decodeAutomationReference, readBack: func(call sdkgo.Call) (AutomationReference, readBackState) {
		return operation.readBackAutomation(call, input)
	}}
	return request.invoke()
}

// Invoke retrieves automation metadata without graph configurations.
func (operation GetAutomationOperation) Invoke(call sdkgo.Call, input GetAutomationInput) sdkgo.QueryAttempt[Automation] {
	if strings.TrimSpace(input.ID) == "" {
		return queryFailure[Automation]("getAutomation", GetAutomationBranchDefect, sdkgo.FailureValidation, "automation ID is required")
	}
	return runQuery(operation.client, call, "getAutomation", "/automations/"+url.PathEscape(input.ID), GetAutomationBranchFound, GetAutomationBranchNotFound, GetAutomationBranchProviderRejected, GetAutomationBranchInvalidResponse, GetAutomationBranchDefect, decodeAutomation)
}

// Invoke lists automation metadata by optional status.
func (operation ListAutomationsOperation) Invoke(call sdkgo.Call, input ListAutomationsInput) sdkgo.QueryAttempt[AutomationPage] {
	if !validAutomationStatus(input.Status, true) {
		return queryFailure[AutomationPage]("listAutomations", ListAutomationsBranchDefect, sdkgo.FailureValidation, "status must be enabled or disabled")
	}
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[AutomationPage]("listAutomations", ListAutomationsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	if input.Status != "" {
		values, _ := url.ParseQuery(strings.TrimPrefix(query, "?"))
		values.Set("status", input.Status)
		query = "?" + values.Encode()
	}
	return runQuery(operation.client, call, "listAutomations", "/automations"+query, ListAutomationsBranchListed, "", ListAutomationsBranchProviderRejected, ListAutomationsBranchInvalidResponse, ListAutomationsBranchDefect, decodeAutomationPage)
}

// Invoke replaces selected absolute graph or metadata fields.
func (operation UpdateAutomationOperation) Invoke(call sdkgo.Call, input UpdateAutomationInput) sdkgo.MutationAttempt[AutomationReference] {
	if strings.TrimSpace(input.ID) == "" || (input.Name == nil && input.Status == nil && input.Steps == nil && input.Connections == nil) || (input.Steps == nil) != (input.Connections == nil) {
		return mutationFailure[AutomationReference]("updateAutomation", UpdateAutomationBranchDefect, sdkgo.FailureValidation, "automation ID and valid field selection are required")
	}
	fields := map[string]any{}
	if input.Name != nil {
		if strings.TrimSpace(*input.Name) == "" {
			return mutationFailure[AutomationReference]("updateAutomation", UpdateAutomationBranchDefect, sdkgo.FailureValidation, "name cannot be blank")
		}
		fields["name"] = *input.Name
	}
	if input.Status != nil {
		if !validAutomationStatus(*input.Status, false) {
			return mutationFailure[AutomationReference]("updateAutomation", UpdateAutomationBranchDefect, sdkgo.FailureValidation, "status must be enabled or disabled")
		}
		fields["status"] = *input.Status
	}
	if input.Steps != nil {
		if !validAutomationGraph(*input.Steps, *input.Connections) {
			return mutationFailure[AutomationReference]("updateAutomation", UpdateAutomationBranchDefect, sdkgo.FailureValidation, "automation graph is invalid")
		}
		fields["steps"] = *input.Steps
		fields["connections"] = *input.Connections
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return mutationFailure[AutomationReference]("updateAutomation", UpdateAutomationBranchDefect, sdkgo.FailureValidation, "automation graph cannot be encoded")
	}
	return runMutation(operation.client, call, "updateAutomation", http.MethodPatch, "/automations/"+url.PathEscape(input.ID), body, "", "", UpdateAutomationBranchUpdated, UpdateAutomationBranchNotFound, UpdateAutomationBranchProviderRejected, UpdateAutomationBranchInvalidResponse, UpdateAutomationBranchDefect, decodeAutomationReference)
}

// Invoke removes one automation.
func (operation DeleteAutomationOperation) Invoke(call sdkgo.Call, input DeleteAutomationInput) sdkgo.MutationAttempt[DeletedAutomation] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[DeletedAutomation]("deleteAutomation", DeleteAutomationBranchDefect, sdkgo.FailureValidation, "automation ID is required")
	}
	return runMutation(operation.client, call, "deleteAutomation", http.MethodDelete, "/automations/"+url.PathEscape(input.ID), nil, "", "", DeleteAutomationBranchDeleted, DeleteAutomationBranchNotFound, DeleteAutomationBranchProviderRejected, DeleteAutomationBranchInvalidResponse, DeleteAutomationBranchDefect, decodeDeletedAutomation)
}

// Invoke duplicates one automation with bounded resend.
func (operation DuplicateAutomationOperation) Invoke(call sdkgo.Call, input DuplicateAutomationInput) sdkgo.MutationAttempt[AutomationReference] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[AutomationReference]("duplicateAutomation", DuplicateAutomationBranchDefect, sdkgo.FailureValidation, "automation ID is required")
	}
	request := resendLimitedCreate[AutomationReference]{client: operation.client, call: call, operation: "duplicateAutomation", path: "/automations/" + url.PathEscape(input.ID) + "/duplicate", timeout: DuplicateAutomationDefinition.StepDefaults.ExecuteMethodTimeout, created: DuplicateAutomationBranchDuplicated, rejected: DuplicateAutomationBranchProviderRejected, defect: DuplicateAutomationBranchDefect, decode: decodeAutomationReference, readBack: func(sdkgo.Call) (AutomationReference, readBackState) { return AutomationReference{}, readBackAbsent }}
	return request.invoke()
}

// Invoke disables one automation.
func (operation StopAutomationOperation) Invoke(call sdkgo.Call, input StopAutomationInput) sdkgo.MutationAttempt[AutomationStatus] {
	if strings.TrimSpace(input.ID) == "" {
		return mutationFailure[AutomationStatus]("stopAutomation", StopAutomationBranchDefect, sdkgo.FailureValidation, "automation ID is required")
	}
	return runMutation(operation.client, call, "stopAutomation", http.MethodPost, "/automations/"+url.PathEscape(input.ID)+"/stop", nil, "", "", StopAutomationBranchStopped, StopAutomationBranchNotFound, StopAutomationBranchProviderRejected, StopAutomationBranchInvalidResponse, StopAutomationBranchDefect, decodeAutomationStatus)
}

// Invoke lists run status metadata without step outputs.
func (operation ListAutomationRunsOperation) Invoke(call sdkgo.Call, input ListAutomationRunsInput) sdkgo.QueryAttempt[AutomationRunPage] {
	if strings.TrimSpace(input.AutomationID) == "" || !validAutomationRunStatus(input.Status) {
		return queryFailure[AutomationRunPage]("listAutomationRuns", ListAutomationRunsBranchDefect, sdkgo.FailureValidation, "automation ID or run status is invalid")
	}
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[AutomationRunPage]("listAutomationRuns", ListAutomationRunsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	if input.Status != "" {
		values, _ := url.ParseQuery(strings.TrimPrefix(query, "?"))
		values.Set("status", input.Status)
		query = "?" + values.Encode()
	}
	return runQuery(operation.client, call, "listAutomationRuns", "/automations/"+url.PathEscape(input.AutomationID)+"/runs"+query, ListAutomationRunsBranchListed, "", ListAutomationRunsBranchProviderRejected, ListAutomationRunsBranchInvalidResponse, ListAutomationRunsBranchDefect, decodeAutomationRunPage)
}

// Invoke retrieves one run without step outputs or errors.
func (operation GetAutomationRunOperation) Invoke(call sdkgo.Call, input GetAutomationRunInput) sdkgo.QueryAttempt[AutomationRun] {
	if strings.TrimSpace(input.AutomationID) == "" || strings.TrimSpace(input.RunID) == "" {
		return queryFailure[AutomationRun]("getAutomationRun", GetAutomationRunBranchDefect, sdkgo.FailureValidation, "automation and run IDs are required")
	}
	return runQuery(operation.client, call, "getAutomationRun", "/automations/"+url.PathEscape(input.AutomationID)+"/runs/"+url.PathEscape(input.RunID), GetAutomationRunBranchFound, GetAutomationRunBranchNotFound, GetAutomationRunBranchProviderRejected, GetAutomationRunBranchInvalidResponse, GetAutomationRunBranchDefect, decodeAutomationRun)
}

func (operation CreateAutomationOperation) readBackAutomation(call sdkgo.Call, input CreateAutomationInput) (AutomationReference, readBackState) {
	cursor := ""
	var confirmed AutomationReference
	expectedStatus := input.Status
	if expectedStatus == "" {
		expectedStatus = "disabled"
	}
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		query, _ := listQuery(100, cursor, false)
		body, state := operation.client.readBackResource(call, "/automations"+query)
		if state != readBackMatched {
			return AutomationReference{}, readBackRetry
		}
		page, err := decodeAutomationPage(body)
		if err != nil {
			return AutomationReference{}, readBackRetry
		}
		for _, candidate := range page.Data {
			if candidate.Name != input.Name || candidate.Status != expectedStatus {
				continue
			}
			detailBody, detailState := operation.client.readBackResource(call, "/automations/"+url.PathEscape(candidate.ID))
			if detailState != readBackMatched {
				return AutomationReference{}, readBackRetry
			}
			var detail struct {
				Steps       []AutomationStep       `json:"steps"`
				Connections []AutomationConnection `json:"connections"`
			}
			if json.Unmarshal(detailBody, &detail) != nil {
				return AutomationReference{}, readBackRetry
			}
			if automationGraphEqual(detail.Steps, detail.Connections, input.Steps, input.Connections) {
				if confirmed.ID != "" {
					return AutomationReference{}, readBackConflicted
				}
				confirmed = AutomationReference{ID: candidate.ID}
			}
		}
		if !page.HasMore {
			if confirmed.ID != "" {
				return confirmed, readBackMatched
			}
			return AutomationReference{}, readBackAbsent
		}
		if len(page.Data) == 0 || page.Data[len(page.Data)-1].ID == cursor {
			return AutomationReference{}, readBackRetry
		}
		cursor = page.Data[len(page.Data)-1].ID
	}
	return AutomationReference{}, readBackRetry
}

func validAutomationStatus(status string, allowBlank bool) bool {
	return status == "enabled" || status == "disabled" || (allowBlank && status == "")
}
func validAutomationRunStatus(status string) bool {
	if status == "" {
		return true
	}
	for _, part := range strings.Split(status, ",") {
		switch part {
		case "running", "completed", "failed", "cancelled":
		default:
			return false
		}
	}
	return true
}
func validAutomationGraph(steps []AutomationStep, connections []AutomationConnection) bool {
	if len(steps) < 1 || len(steps) > 150 || connections == nil {
		return false
	}
	keys := map[string]bool{}
	hasTrigger := false
	for _, step := range steps {
		if strings.TrimSpace(step.Key) == "" || keys[step.Key] || !validAutomationStepType(step.Type) || !json.Valid(step.Config) {
			return false
		}
		keys[step.Key] = true
		if step.Type == "trigger" {
			hasTrigger = true
		}
	}
	if !hasTrigger {
		return false
	}
	for _, edge := range connections {
		if !keys[edge.From] || !keys[edge.To] || !validAutomationConnectionType(edge.Type) {
			return false
		}
	}
	return true
}
func validAutomationStepType(value string) bool {
	switch value {
	case "trigger", "send_email", "delay", "wait_for_event", "condition", "contact_update", "contact_delete", "add_to_segment":
		return true
	}
	return false
}
func validAutomationConnectionType(value string) bool {
	switch value {
	case "", "default", "condition_met", "condition_not_met", "timeout", "event_received":
		return true
	}
	return false
}
func automationGraphEqual(actualSteps []AutomationStep, actualConnections []AutomationConnection, expectedSteps []AutomationStep, expectedConnections []AutomationConnection) bool {
	if len(actualSteps) != len(expectedSteps) || !slices.Equal(actualConnections, expectedConnections) {
		return false
	}
	for index := range actualSteps {
		if actualSteps[index].Key != expectedSteps[index].Key || actualSteps[index].Type != expectedSteps[index].Type {
			return false
		}
		var actualConfig, expectedConfig any
		if json.Unmarshal(actualSteps[index].Config, &actualConfig) != nil || json.Unmarshal(expectedSteps[index].Config, &expectedConfig) != nil || !reflect.DeepEqual(actualConfig, expectedConfig) {
			return false
		}
	}
	return true
}
func decodeAutomationReference(body []byte) (AutomationReference, error) {
	var value AutomationReference
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return AutomationReference{}, fmt.Errorf("invalid automation reference")
	}
	return value, nil
}
func decodeAutomation(body []byte) (Automation, error) {
	var value Automation
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return Automation{}, fmt.Errorf("invalid automation response")
	}
	return value, nil
}
func decodeAutomationPage(body []byte) (AutomationPage, error) {
	var value AutomationPage
	if err := json.Unmarshal(body, &value); err != nil || value.Data == nil {
		return AutomationPage{}, fmt.Errorf("invalid automation page")
	}
	return value, nil
}
func decodeDeletedAutomation(body []byte) (DeletedAutomation, error) {
	var value DeletedAutomation
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" || !value.Deleted {
		return DeletedAutomation{}, fmt.Errorf("invalid automation removal")
	}
	return value, nil
}
func decodeAutomationStatus(body []byte) (AutomationStatus, error) {
	var value AutomationStatus
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" || value.Status != "disabled" {
		return AutomationStatus{}, fmt.Errorf("invalid automation stop response")
	}
	return value, nil
}
func decodeAutomationRun(body []byte) (AutomationRun, error) {
	var value AutomationRun
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return AutomationRun{}, fmt.Errorf("invalid automation run")
	}
	return value, nil
}
func decodeAutomationRunPage(body []byte) (AutomationRunPage, error) {
	var value AutomationRunPage
	if err := json.Unmarshal(body, &value); err != nil || value.Data == nil {
		return AutomationRunPage{}, fmt.Errorf("invalid automation run page")
	}
	return value, nil
}
