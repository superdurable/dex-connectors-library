// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetInboxAgentInput selects one inbox's agent settings.
type GetInboxAgentInput struct {
	// InboxID is the inbox ID or email address.
	InboxID string `json:"inboxId"`
}

// UpdateInboxAgentInput replaces selected agent settings.
type UpdateInboxAgentInput struct {
	// InboxID is the inbox ID or email address.
	InboxID string `json:"inboxId"`
	// Instructions replaces the agent instructions when non-nil; do not include secrets.
	Instructions *string `json:"instructions,omitempty"`
	// ClearInstructions removes existing instructions.
	ClearInstructions bool `json:"clearInstructions,omitempty"`
	// Tone replaces the agent's writing tone when non-nil.
	Tone *string `json:"tone,omitempty"`
	// ClearTone removes an existing tone.
	ClearTone bool `json:"clearTone,omitempty"`
	// Actions replaces the enabled action set when non-nil; empty disables all.
	Actions *[]string `json:"actions,omitempty"`
}

// ListInboxLabelsInput selects all labels on one inbox.
type ListInboxLabelsInput struct {
	// InboxID is the inbox ID or email address.
	InboxID string `json:"inboxId"`
}

// CreateInboxLabelInput creates one named inbox label.
type CreateInboxLabelInput struct {
	// InboxID is the inbox ID or email address.
	InboxID string `json:"inboxId"`
	// Name is unique within the inbox, ignoring case.
	Name string `json:"name"`
	// Color is optional #RRGGBB; blank uses Resend's palette.
	Color string `json:"color,omitempty"`
}

// UpdateInboxLabelInput replaces selected absolute label fields.
type UpdateInboxLabelInput struct {
	// InboxID is the inbox ID or email address.
	InboxID string `json:"inboxId"`
	// LabelID is the label ID.
	LabelID string `json:"labelId"`
	// Name replaces the label name when non-nil.
	Name *string `json:"name,omitempty"`
	// Color replaces the #RRGGBB color when non-nil.
	Color *string `json:"color,omitempty"`
}

// DeleteInboxLabelInput selects one inbox label for removal.
type DeleteInboxLabelInput struct {
	// InboxID is the inbox ID or email address.
	InboxID string `json:"inboxId"`
	// LabelID is the label ID.
	LabelID string `json:"labelId"`
}

// InboxAgentSettings exposes agent tone and actions without instructions.
type InboxAgentSettings struct {
	// Tone is concise_professional, concise_casual, expanded_professional, or expanded_casual.
	Tone string `json:"tone"`
	// Actions contains enabled provider action names.
	Actions []string `json:"actions"`
}

// InboxAgentReference identifies changed agent settings.
type InboxAgentReference struct {
	// ID is the provider agent ID.
	ID string `json:"id"`
}

// InboxLabelReference identifies a created or changed label.
type InboxLabelReference struct {
	// ID is the provider label ID.
	ID string `json:"id"`
}

// InboxLabel contains label metadata.
type InboxLabel struct {
	// ID is the label ID.
	ID string `json:"id"`
	// Name is the label name.
	Name string `json:"name"`
	// Color is the uppercase #RRGGBB color.
	Color string `json:"color"`
	// CreatedAt is the creation timestamp.
	CreatedAt string `json:"created_at"`
}

// InboxLabelList contains all labels on one inbox.
type InboxLabelList struct {
	// Data contains the labels.
	Data []InboxLabel `json:"data"`
}

// DeletedInboxLabel confirms one label removal.
type DeletedInboxLabel struct {
	// ID is the removed label ID.
	ID string `json:"id"`
	// Deleted confirms removal.
	Deleted bool `json:"deleted"`
}

// GetInboxAgentOperation retrieves safe agent settings.
type GetInboxAgentOperation struct{ client *Client }

// UpdateInboxAgentOperation replaces selected agent settings.
type UpdateInboxAgentOperation struct{ client *Client }

// ListInboxLabelsOperation lists labels.
type ListInboxLabelsOperation struct{ client *Client }

// CreateInboxLabelOperation creates a label with bounded resend.
type CreateInboxLabelOperation struct{ client *Client }

// UpdateInboxLabelOperation changes absolute label fields.
type UpdateInboxLabelOperation struct{ client *Client }

// DeleteInboxLabelOperation removes a label.
type DeleteInboxLabelOperation struct{ client *Client }

// GetInboxAgent binds agent settings retrieval to this client.
func (client *Client) GetInboxAgent() GetInboxAgentOperation { return GetInboxAgentOperation{client} }

// UpdateInboxAgent binds agent settings updates to this client.
func (client *Client) UpdateInboxAgent() UpdateInboxAgentOperation {
	return UpdateInboxAgentOperation{client}
}

// ListInboxLabels binds label listing to this client.
func (client *Client) ListInboxLabels() ListInboxLabelsOperation {
	return ListInboxLabelsOperation{client}
}

// CreateInboxLabel binds label creation to this client.
func (client *Client) CreateInboxLabel() CreateInboxLabelOperation {
	return CreateInboxLabelOperation{client}
}

// UpdateInboxLabel binds label updates to this client.
func (client *Client) UpdateInboxLabel() UpdateInboxLabelOperation {
	return UpdateInboxLabelOperation{client}
}

// DeleteInboxLabel binds label removal to this client.
func (client *Client) DeleteInboxLabel() DeleteInboxLabelOperation {
	return DeleteInboxLabelOperation{client}
}

// Definition returns the generated getInboxAgent contract.
func (GetInboxAgentOperation) Definition() sdkgo.QueryDefinition { return GetInboxAgentDefinition }

// Definition returns the generated updateInboxAgent contract.
func (UpdateInboxAgentOperation) Definition() sdkgo.MutationDefinition {
	return UpdateInboxAgentDefinition
}

// Definition returns the generated listInboxLabels contract.
func (ListInboxLabelsOperation) Definition() sdkgo.QueryDefinition { return ListInboxLabelsDefinition }

// Definition returns the generated createInboxLabel contract.
func (CreateInboxLabelOperation) Definition() sdkgo.MutationDefinition {
	return CreateInboxLabelDefinition
}

// Definition returns the generated updateInboxLabel contract.
func (UpdateInboxLabelOperation) Definition() sdkgo.MutationDefinition {
	return UpdateInboxLabelDefinition
}

// Definition returns the generated deleteInboxLabel contract.
func (DeleteInboxLabelOperation) Definition() sdkgo.MutationDefinition {
	return DeleteInboxLabelDefinition
}

// IdempotencyKey correlates absolute agent settings changes.
func (UpdateInboxAgentOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateInboxAgentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates bounded label creation attempts.
func (CreateInboxLabelOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateInboxLabelInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates absolute label changes.
func (UpdateInboxLabelOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateInboxLabelInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates harmless repeated deletion.
func (DeleteInboxLabelOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteInboxLabelInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke retrieves agent tone and actions without stored instructions.
func (operation GetInboxAgentOperation) Invoke(call sdkgo.Call, input GetInboxAgentInput) sdkgo.QueryAttempt[InboxAgentSettings] {
	path, ok := inboxAgentPath(input.InboxID)
	if !ok {
		return queryFailure[InboxAgentSettings]("getInboxAgent", GetInboxAgentBranchDefect, sdkgo.FailureValidation, "inbox ID is required")
	}
	return runQuery(operation.client, call, "getInboxAgent", path, GetInboxAgentBranchFound, GetInboxAgentBranchNotFound, GetInboxAgentBranchProviderRejected, GetInboxAgentBranchInvalidResponse, GetInboxAgentBranchDefect, decodeInboxAgentSettings)
}

// Invoke replaces selected agent settings, including explicit clears.
func (operation UpdateInboxAgentOperation) Invoke(call sdkgo.Call, input UpdateInboxAgentInput) sdkgo.MutationAttempt[InboxAgentReference] {
	path, ok := inboxAgentPath(input.InboxID)
	if !ok || (input.Instructions == nil && !input.ClearInstructions && input.Tone == nil && !input.ClearTone && input.Actions == nil) || (input.Instructions != nil && input.ClearInstructions) || (input.Tone != nil && input.ClearTone) {
		return mutationFailure[InboxAgentReference]("updateInboxAgent", UpdateInboxAgentBranchDefect, sdkgo.FailureValidation, "inbox ID and one unambiguous setting are required")
	}
	fields := map[string]any{}
	if input.ClearInstructions {
		fields["instructions"] = nil
	} else if input.Instructions != nil {
		if strings.TrimSpace(*input.Instructions) == "" {
			return mutationFailure[InboxAgentReference]("updateInboxAgent", UpdateInboxAgentBranchDefect, sdkgo.FailureValidation, "instructions cannot be blank")
		}
		fields["instructions"] = *input.Instructions
	}
	if input.ClearTone {
		fields["tone"] = nil
	} else if input.Tone != nil {
		if !validInboxAgentTone(*input.Tone) {
			return mutationFailure[InboxAgentReference]("updateInboxAgent", UpdateInboxAgentBranchDefect, sdkgo.FailureValidation, "tone is invalid")
		}
		fields["tone"] = *input.Tone
	}
	if input.Actions != nil {
		for _, action := range *input.Actions {
			if !validInboxAgentAction(action) {
				return mutationFailure[InboxAgentReference]("updateInboxAgent", UpdateInboxAgentBranchDefect, sdkgo.FailureValidation, "action is invalid")
			}
		}
		fields["actions"] = *input.Actions
	}
	body, _ := json.Marshal(fields)
	return runMutation(operation.client, call, "updateInboxAgent", http.MethodPatch, path, body, "", "", UpdateInboxAgentBranchUpdated, UpdateInboxAgentBranchNotFound, UpdateInboxAgentBranchProviderRejected, UpdateInboxAgentBranchInvalidResponse, UpdateInboxAgentBranchDefect, decodeInboxAgentReference)
}

// Invoke lists every label on one inbox.
func (operation ListInboxLabelsOperation) Invoke(call sdkgo.Call, input ListInboxLabelsInput) sdkgo.QueryAttempt[InboxLabelList] {
	path, ok := inboxLabelsPath(input.InboxID)
	if !ok {
		return queryFailure[InboxLabelList]("listInboxLabels", ListInboxLabelsBranchDefect, sdkgo.FailureValidation, "inbox ID is required")
	}
	return runQuery(operation.client, call, "listInboxLabels", path, ListInboxLabelsBranchListed, ListInboxLabelsBranchNotFound, ListInboxLabelsBranchProviderRejected, ListInboxLabelsBranchInvalidResponse, ListInboxLabelsBranchDefect, decodeInboxLabelList)
}

// Invoke creates one named inbox label.
func (operation CreateInboxLabelOperation) Invoke(call sdkgo.Call, input CreateInboxLabelInput) sdkgo.MutationAttempt[InboxLabelReference] {
	path, ok := inboxLabelsPath(input.InboxID)
	if !ok || strings.TrimSpace(input.Name) == "" || !validInboxLabelColor(input.Color, true) {
		return mutationFailure[InboxLabelReference]("createInboxLabel", CreateInboxLabelBranchDefect, sdkgo.FailureValidation, "inbox ID, label name, or color is invalid")
	}
	fields := map[string]any{"name": input.Name}
	if input.Color != "" {
		fields["color"] = input.Color
	}
	body, _ := json.Marshal(fields)
	request := resendLimitedCreate[InboxLabelReference]{client: operation.client, call: call, operation: "createInboxLabel", path: path, body: body, timeout: CreateInboxLabelDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateInboxLabelBranchCreated, rejected: CreateInboxLabelBranchProviderRejected, defect: CreateInboxLabelBranchDefect, decode: decodeInboxLabelReference, readBack: func(call sdkgo.Call) (InboxLabelReference, readBackState) {
		return operation.readBackLabel(call, input)
	}}
	return request.invoke()
}

// Invoke replaces selected absolute label fields.
func (operation UpdateInboxLabelOperation) Invoke(call sdkgo.Call, input UpdateInboxLabelInput) sdkgo.MutationAttempt[InboxLabelReference] {
	path, ok := inboxLabelPath(input.InboxID, input.LabelID)
	if !ok || (input.Name == nil && input.Color == nil) {
		return mutationFailure[InboxLabelReference]("updateInboxLabel", UpdateInboxLabelBranchDefect, sdkgo.FailureValidation, "inbox ID, label ID, and one field are required")
	}
	fields := map[string]any{}
	if input.Name != nil {
		if strings.TrimSpace(*input.Name) == "" {
			return mutationFailure[InboxLabelReference]("updateInboxLabel", UpdateInboxLabelBranchDefect, sdkgo.FailureValidation, "label name cannot be blank")
		}
		fields["name"] = *input.Name
	}
	if input.Color != nil {
		if !validInboxLabelColor(*input.Color, false) {
			return mutationFailure[InboxLabelReference]("updateInboxLabel", UpdateInboxLabelBranchDefect, sdkgo.FailureValidation, "label color is invalid")
		}
		fields["color"] = *input.Color
	}
	body, _ := json.Marshal(fields)
	return runMutation(operation.client, call, "updateInboxLabel", http.MethodPatch, path, body, "", "", UpdateInboxLabelBranchUpdated, UpdateInboxLabelBranchNotFound, UpdateInboxLabelBranchProviderRejected, UpdateInboxLabelBranchInvalidResponse, UpdateInboxLabelBranchDefect, decodeInboxLabelReference)
}

// Invoke removes one inbox label.
func (operation DeleteInboxLabelOperation) Invoke(call sdkgo.Call, input DeleteInboxLabelInput) sdkgo.MutationAttempt[DeletedInboxLabel] {
	path, ok := inboxLabelPath(input.InboxID, input.LabelID)
	if !ok {
		return mutationFailure[DeletedInboxLabel]("deleteInboxLabel", DeleteInboxLabelBranchDefect, sdkgo.FailureValidation, "inbox and label IDs are required")
	}
	return runMutation(operation.client, call, "deleteInboxLabel", http.MethodDelete, path, nil, "", "", DeleteInboxLabelBranchDeleted, DeleteInboxLabelBranchNotFound, DeleteInboxLabelBranchProviderRejected, DeleteInboxLabelBranchInvalidResponse, DeleteInboxLabelBranchDefect, decodeDeletedInboxLabel)
}

func (operation CreateInboxLabelOperation) readBackLabel(call sdkgo.Call, input CreateInboxLabelInput) (InboxLabelReference, readBackState) {
	path, _ := inboxLabelsPath(input.InboxID)
	body, state := operation.client.readBackResource(call, path)
	if state != readBackMatched {
		return InboxLabelReference{}, state
	}
	labels, err := decodeInboxLabelList(body)
	if err != nil {
		return InboxLabelReference{}, readBackRetry
	}
	for _, label := range labels.Data {
		if strings.EqualFold(label.Name, input.Name) {
			if input.Color != "" && !strings.EqualFold(label.Color, input.Color) {
				return InboxLabelReference{}, readBackConflicted
			}
			return InboxLabelReference{ID: label.ID}, readBackMatched
		}
	}
	return InboxLabelReference{}, readBackAbsent
}
func inboxAgentPath(inboxID string) (string, bool) {
	if strings.TrimSpace(inboxID) == "" {
		return "", false
	}
	return "/inboxes/" + url.PathEscape(inboxID) + "/agent", true
}
func inboxLabelsPath(inboxID string) (string, bool) {
	if strings.TrimSpace(inboxID) == "" {
		return "", false
	}
	return "/inboxes/" + url.PathEscape(inboxID) + "/labels", true
}
func inboxLabelPath(inboxID, labelID string) (string, bool) {
	path, ok := inboxLabelsPath(inboxID)
	if !ok || strings.TrimSpace(labelID) == "" {
		return "", false
	}
	return path + "/" + url.PathEscape(labelID), true
}
func validInboxAgentTone(value string) bool {
	switch value {
	case "concise_professional", "concise_casual", "expanded_professional", "expanded_casual":
		return true
	}
	return false
}
func validInboxAgentAction(value string) bool {
	switch value {
	case "draft_reply", "forward_thread", "add_labels", "assign_thread", "mark_as_spam", "archive", "move_to_trash":
		return true
	}
	return false
}
func validInboxLabelColor(value string, allowBlank bool) bool {
	if value == "" {
		return allowBlank
	}
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, char := range value[1:] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}
func decodeInboxAgentSettings(body []byte) (InboxAgentSettings, error) {
	var value InboxAgentSettings
	if err := json.Unmarshal(body, &value); err != nil || value.Actions == nil {
		return InboxAgentSettings{}, fmt.Errorf("invalid inbox agent response")
	}
	return value, nil
}
func decodeInboxAgentReference(body []byte) (InboxAgentReference, error) {
	var value InboxAgentReference
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return InboxAgentReference{}, fmt.Errorf("invalid inbox agent reference")
	}
	return value, nil
}
func decodeInboxLabelReference(body []byte) (InboxLabelReference, error) {
	var value InboxLabelReference
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return InboxLabelReference{}, fmt.Errorf("invalid inbox label reference")
	}
	return value, nil
}
func decodeInboxLabelList(body []byte) (InboxLabelList, error) {
	var value InboxLabelList
	if err := json.Unmarshal(body, &value); err != nil || value.Data == nil {
		return InboxLabelList{}, fmt.Errorf("invalid inbox label list")
	}
	return value, nil
}
func decodeDeletedInboxLabel(body []byte) (DeletedInboxLabel, error) {
	var value DeletedInboxLabel
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" || !value.Deleted {
		return DeletedInboxLabel{}, fmt.Errorf("invalid inbox label removal")
	}
	return value, nil
}
