// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListCustomFieldsInput selects one cursor page of custom fields.
type ListCustomFieldsInput struct {
	// PerPage bounds the page size; zero uses 20.
	PerPage int `json:"perPage,omitempty"`
	// After is the prior page's opaque end cursor.
	After string `json:"after,omitempty"`
}

// CustomFieldIDInput identifies one custom field by numeric ID.
type CustomFieldIDInput struct {
	// CustomFieldID is Kit's numeric custom-field ID.
	CustomFieldID int64 `json:"customFieldId"`
}

// CreateCustomFieldInput names the field to create or reuse.
type CreateCustomFieldInput struct {
	// Label is the unique, non-empty field label.
	Label string `json:"label"`
}

// UpdateCustomFieldInput replaces a field's label.
type UpdateCustomFieldInput struct {
	// CustomFieldID is Kit's numeric custom-field ID.
	CustomFieldID int64 `json:"customFieldId"`
	// Label is the replacement non-empty field label.
	Label string `json:"label"`
}

// CustomFieldMetadata contains a field's stable identity and display label.
type CustomFieldMetadata struct {
	// ID is Kit's numeric custom-field ID.
	ID int64 `json:"id"`
	// Name is the stable HTML form field name.
	Name string `json:"name"`
	// Key is the personalization key, which can change after a label update.
	Key string `json:"key"`
	// Label is the human-readable field label.
	Label string `json:"label"`
}

// CustomFieldsPage contains one cursor page of custom fields.
type CustomFieldsPage struct {
	// CustomFields are the returned field definitions.
	CustomFields []CustomFieldMetadata `json:"customFields"`
	// NextCursor is nonempty when another page exists.
	NextCursor string `json:"nextCursor"`
}

// CustomFieldDeletion confirms deletion of one custom field.
type CustomFieldDeletion struct {
	// CustomFieldID is the deleted field ID.
	CustomFieldID int64 `json:"customFieldId"`
	// Deleted reports Kit's acceptance of deletion.
	Deleted bool `json:"deleted"`
}

// ListCustomFieldsOperation lists custom-field definitions.
type ListCustomFieldsOperation struct{ client *Client }

// CreateCustomFieldOperation creates or reuses a field by unique label.
type CreateCustomFieldOperation struct{ client *Client }

// UpdateCustomFieldOperation replaces a field label.
type UpdateCustomFieldOperation struct{ client *Client }

// DeleteCustomFieldOperation deletes a field and its subscriber values.
type DeleteCustomFieldOperation struct{ client *Client }

// ListCustomFields binds the list Query to this client.
func (client *Client) ListCustomFields() ListCustomFieldsOperation {
	return ListCustomFieldsOperation{client}
}

// CreateCustomField binds field creation to this client.
func (client *Client) CreateCustomField() CreateCustomFieldOperation {
	return CreateCustomFieldOperation{client}
}

// UpdateCustomField binds label replacement to this client.
func (client *Client) UpdateCustomField() UpdateCustomFieldOperation {
	return UpdateCustomFieldOperation{client}
}

// DeleteCustomField binds field deletion to this client.
func (client *Client) DeleteCustomField() DeleteCustomFieldOperation {
	return DeleteCustomFieldOperation{client}
}

// Definition returns the generated query contract.
func (ListCustomFieldsOperation) Definition() sdkgo.QueryDefinition {
	return ListCustomFieldsDefinition
}

// Definition returns the generated mutation contract.
func (CreateCustomFieldOperation) Definition() sdkgo.MutationDefinition {
	return CreateCustomFieldDefinition
}

// Definition returns the generated mutation contract.
func (UpdateCustomFieldOperation) Definition() sdkgo.MutationDefinition {
	return UpdateCustomFieldDefinition
}

// Definition returns the generated mutation contract.
func (DeleteCustomFieldOperation) Definition() sdkgo.MutationDefinition {
	return DeleteCustomFieldDefinition
}

// IdempotencyKey derives a stable call identity across retries.
func (CreateCustomFieldOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCustomFieldInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call identity across retries.
func (UpdateCustomFieldOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCustomFieldInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable call identity across retries.
func (DeleteCustomFieldOperation) IdempotencyKey(callID sdkgo.CallID, _ CustomFieldIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads one cursor page of custom-field definitions.
func (operation ListCustomFieldsOperation) Invoke(call sdkgo.Call, input ListCustomFieldsInput) sdkgo.QueryAttempt[CustomFieldsPage] {
	branches := queryBranches{found: ListCustomFieldsBranchListed, rejected: ListCustomFieldsBranchProviderRejected, invalid: ListCustomFieldsBranchInvalidResponse, defect: ListCustomFieldsBranchDefect}
	if input.PerPage < 0 || input.PerPage > 1000 || len(input.After) > 2048 {
		return queryBranch("listCustomFields", branches.defect, CustomFieldsPage{}, sdkgo.FailureValidation, "cursor or perPage is invalid")
	}
	if input.PerPage == 0 {
		input.PerPage = 20
	}
	query := url.Values{"per_page": {strconv.Itoa(input.PerPage)}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	return invokeQuery(call, operation.client, "listCustomFields", "/custom_fields?"+query.Encode(), branches, decodeCustomFieldsPage)
}

// Invoke creates or reuses a field by Kit's unique label.
func (operation CreateCustomFieldOperation) Invoke(call sdkgo.Call, input CreateCustomFieldInput) sdkgo.MutationAttempt[CustomFieldMetadata] {
	if !validCustomFieldLabel(input.Label) {
		return kitMutationBranch("createCustomField", CreateCustomFieldBranchDefect, CustomFieldMetadata{}, sdkgo.FailureValidation, "bounded field label is required")
	}
	body, _ := json.Marshal(map[string]string{"label": input.Label})
	return invokeKitMutation(call, operation.client, "createCustomField", http.MethodPost, "/custom_fields", body,
		mutationBranches{success: CreateCustomFieldBranchCreated, rejected: CreateCustomFieldBranchProviderRejected, invalid: CreateCustomFieldBranchInvalidResponse, defect: CreateCustomFieldBranchDefect}, decodeCustomField)
}

// Invoke replaces a field's label and personalization key.
func (operation UpdateCustomFieldOperation) Invoke(call sdkgo.Call, input UpdateCustomFieldInput) sdkgo.MutationAttempt[CustomFieldMetadata] {
	if input.CustomFieldID <= 0 || !validCustomFieldLabel(input.Label) {
		return kitMutationBranch("updateCustomField", UpdateCustomFieldBranchDefect, CustomFieldMetadata{}, sdkgo.FailureValidation, "positive field ID and bounded label are required")
	}
	body, _ := json.Marshal(map[string]string{"label": input.Label})
	path := "/custom_fields/" + strconv.FormatInt(input.CustomFieldID, 10)
	return invokeKitMutation(call, operation.client, "updateCustomField", http.MethodPut, path, body,
		mutationBranches{success: UpdateCustomFieldBranchUpdated, notFound: UpdateCustomFieldBranchNotFound, rejected: UpdateCustomFieldBranchProviderRejected, invalid: UpdateCustomFieldBranchInvalidResponse, defect: UpdateCustomFieldBranchDefect}, decodeCustomField)
}

// Invoke deletes a field and all subscriber values stored in it.
func (operation DeleteCustomFieldOperation) Invoke(call sdkgo.Call, input CustomFieldIDInput) sdkgo.MutationAttempt[CustomFieldDeletion] {
	if input.CustomFieldID <= 0 {
		return kitMutationBranch("deleteCustomField", DeleteCustomFieldBranchDefect, CustomFieldDeletion{}, sdkgo.FailureValidation, "field ID must be positive")
	}
	path := "/custom_fields/" + strconv.FormatInt(input.CustomFieldID, 10)
	body, status, retryAfter, err := operation.client.request(call, http.MethodDelete, path, nil)
	branches := mutationBranches{success: DeleteCustomFieldBranchDeleted, notFound: DeleteCustomFieldBranchNotFound, rejected: DeleteCustomFieldBranchProviderRejected, invalid: DeleteCustomFieldBranchInvalidResponse, defect: DeleteCustomFieldBranchDefect}
	if outcome, handled := classifyKitMutation[CustomFieldDeletion](body, status, retryAfter, err, "deleteCustomField", branches); handled {
		return outcome
	}
	if status != http.StatusNoContent {
		return kitMutationBranch("deleteCustomField", branches.invalid, CustomFieldDeletion{}, sdkgo.FailureProtocol, "Kit returned unexpected deletion response")
	}
	return sdkgo.NewMutationBranch(branches.success, CustomFieldDeletion{CustomFieldID: input.CustomFieldID, Deleted: true}, nil, sdkgo.Receipt{})
}

func decodeCustomFieldsPage(body []byte) (CustomFieldsPage, error) {
	var wire struct {
		CustomFields []CustomFieldMetadata `json:"custom_fields"`
		Pagination   *struct {
			HasNextPage bool   `json:"has_next_page"`
			EndCursor   string `json:"end_cursor"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.CustomFields == nil || wire.Pagination == nil {
		return CustomFieldsPage{}, fmt.Errorf("invalid custom-field page")
	}
	page := CustomFieldsPage{CustomFields: wire.CustomFields}
	if wire.Pagination.HasNextPage {
		if wire.Pagination.EndCursor == "" {
			return CustomFieldsPage{}, fmt.Errorf("missing custom-field cursor")
		}
		page.NextCursor = wire.Pagination.EndCursor
	}
	for _, field := range page.CustomFields {
		if !validCustomFieldMetadata(field) {
			return CustomFieldsPage{}, fmt.Errorf("invalid custom-field identity")
		}
	}
	return page, nil
}

func decodeCustomField(body []byte) (CustomFieldMetadata, error) {
	var wire struct {
		CustomField CustomFieldMetadata `json:"custom_field"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || !validCustomFieldMetadata(wire.CustomField) {
		return CustomFieldMetadata{}, fmt.Errorf("invalid custom-field JSON")
	}
	return wire.CustomField, nil
}

func validCustomFieldLabel(label string) bool {
	return strings.TrimSpace(label) != "" && len(label) <= 255
}

func validCustomFieldMetadata(field CustomFieldMetadata) bool {
	return field.ID > 0 && field.Name != "" && field.Key != "" && field.Label != ""
}
