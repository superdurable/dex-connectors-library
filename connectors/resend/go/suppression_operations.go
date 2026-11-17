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

// CreateSuppressionInput blocks delivery to one email address.
type CreateSuppressionInput struct {
	// Email is the address to suppress.
	Email string `json:"email"`
}

// ListSuppressionsInput selects one page, optionally filtered by origin.
type ListSuppressionsInput struct {
	// Origin is bounce, complaint, or manual; blank includes all.
	Origin string `json:"origin,omitempty"`
	// Limit is the page size from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive suppression cursor.
	After string `json:"after,omitempty"`
}

// GetSuppressionInput selects a suppression by ID or email.
type GetSuppressionInput struct {
	// Suppression is the ID or email address.
	Suppression string `json:"suppression"`
}

// DeleteSuppressionInput selects a suppression by ID or email.
type DeleteSuppressionInput struct {
	// Suppression is the ID or email address.
	Suppression string `json:"suppression"`
}

// BatchAddSuppressionsInput blocks up to 100 addresses.
type BatchAddSuppressionsInput struct {
	// Emails contains 1 through 100 addresses.
	Emails []string `json:"emails"`
}

// BatchRemoveSuppressionsInput removes suppressions by email or ID.
type BatchRemoveSuppressionsInput struct {
	// Emails contains 1 through 100 addresses when IDs is empty.
	Emails []string `json:"emails,omitempty"`
	// IDs contains 1 through 100 suppression IDs when Emails is empty.
	IDs []string `json:"ids,omitempty"`
}

// SuppressionReference identifies a created suppression.
type SuppressionReference struct {
	// ID is the provider suppression ID.
	ID string `json:"id"`
}

// Suppression contains suppression metadata.
type Suppression struct {
	// ID is the provider suppression ID.
	ID string `json:"id"`
	// Email is the suppressed address.
	Email string `json:"email"`
	// Origin is bounce, complaint, or manual.
	Origin string `json:"origin"`
	// SourceID is the event or email that caused suppression, when present.
	SourceID string `json:"source_id"`
	// CreatedAt is the creation timestamp.
	CreatedAt string `json:"created_at"`
}

// SuppressionPage contains one page of suppressions.
type SuppressionPage struct {
	// Data contains suppression metadata.
	Data []Suppression `json:"data"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"has_more"`
}

// SuppressionBatch contains created suppression IDs.
type SuppressionBatch struct {
	// Data contains created suppression IDs.
	Data []SuppressionReference `json:"data"`
}

// DeletedSuppression confirms one removal.
type DeletedSuppression struct {
	// ID is the removed suppression ID.
	ID string `json:"id"`
	// Deleted confirms removal.
	Deleted bool `json:"deleted"`
}

// RemovedSuppressionBatch contains removed suppression IDs.
type RemovedSuppressionBatch struct {
	// Data contains removal acknowledgements.
	Data []DeletedSuppression `json:"data"`
}

// CreateSuppressionOperation creates one suppression with bounded resend.
type CreateSuppressionOperation struct{ client *Client }

// ListSuppressionsOperation lists suppressions.
type ListSuppressionsOperation struct{ client *Client }

// GetSuppressionOperation retrieves a suppression.
type GetSuppressionOperation struct{ client *Client }

// DeleteSuppressionOperation removes a suppression.
type DeleteSuppressionOperation struct{ client *Client }

// BatchAddSuppressionsOperation adds up to 100 suppressions with bounded resend.
type BatchAddSuppressionsOperation struct{ client *Client }

// BatchRemoveSuppressionsOperation removes up to 100 suppressions.
type BatchRemoveSuppressionsOperation struct{ client *Client }

// CreateSuppression binds single suppression creation to this client.
func (client *Client) CreateSuppression() CreateSuppressionOperation {
	return CreateSuppressionOperation{client}
}

// ListSuppressions binds suppression listing to this client.
func (client *Client) ListSuppressions() ListSuppressionsOperation {
	return ListSuppressionsOperation{client}
}

// GetSuppression binds suppression retrieval to this client.
func (client *Client) GetSuppression() GetSuppressionOperation {
	return GetSuppressionOperation{client}
}

// DeleteSuppression binds suppression removal to this client.
func (client *Client) DeleteSuppression() DeleteSuppressionOperation {
	return DeleteSuppressionOperation{client}
}

// BatchAddSuppressions binds batch creation to this client.
func (client *Client) BatchAddSuppressions() BatchAddSuppressionsOperation {
	return BatchAddSuppressionsOperation{client}
}

// BatchRemoveSuppressions binds batch removal to this client.
func (client *Client) BatchRemoveSuppressions() BatchRemoveSuppressionsOperation {
	return BatchRemoveSuppressionsOperation{client}
}

// Definition returns the generated createSuppression contract.
func (CreateSuppressionOperation) Definition() sdkgo.MutationDefinition {
	return CreateSuppressionDefinition
}

// Definition returns the generated listSuppressions contract.
func (ListSuppressionsOperation) Definition() sdkgo.QueryDefinition {
	return ListSuppressionsDefinition
}

// Definition returns the generated getSuppression contract.
func (GetSuppressionOperation) Definition() sdkgo.QueryDefinition { return GetSuppressionDefinition }

// Definition returns the generated deleteSuppression contract.
func (DeleteSuppressionOperation) Definition() sdkgo.MutationDefinition {
	return DeleteSuppressionDefinition
}

// Definition returns the generated batchAddSuppressions contract.
func (BatchAddSuppressionsOperation) Definition() sdkgo.MutationDefinition {
	return BatchAddSuppressionsDefinition
}

// Definition returns the generated batchRemoveSuppressions contract.
func (BatchRemoveSuppressionsOperation) Definition() sdkgo.MutationDefinition {
	return BatchRemoveSuppressionsDefinition
}

// IdempotencyKey correlates possible suppression creation attempts.
func (CreateSuppressionOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSuppressionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates harmless repeated deletion.
func (DeleteSuppressionOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSuppressionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates possible batch addition attempts.
func (BatchAddSuppressionsOperation) IdempotencyKey(callID sdkgo.CallID, _ BatchAddSuppressionsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates harmless repeated batch removal.
func (BatchRemoveSuppressionsOperation) IdempotencyKey(callID sdkgo.CallID, _ BatchRemoveSuppressionsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke adds one address to the suppression list.
func (operation CreateSuppressionOperation) Invoke(call sdkgo.Call, input CreateSuppressionInput) sdkgo.MutationAttempt[SuppressionReference] {
	if strings.TrimSpace(input.Email) == "" {
		return mutationFailure[SuppressionReference]("createSuppression", CreateSuppressionBranchDefect, sdkgo.FailureValidation, "email is required")
	}
	body, _ := json.Marshal(map[string]string{"email": input.Email})
	request := resendLimitedCreate[SuppressionReference]{client: operation.client, call: call, operation: "createSuppression", path: "/suppressions", body: body, timeout: CreateSuppressionDefinition.StepDefaults.ExecuteMethodTimeout, created: CreateSuppressionBranchCreated, rejected: CreateSuppressionBranchProviderRejected, defect: CreateSuppressionBranchDefect, decode: decodeSuppressionReference, readBack: func(call sdkgo.Call) (SuppressionReference, readBackState) {
		return operation.readBackSuppression(call, input.Email)
	}}
	return request.invoke()
}

// Invoke lists suppressions, optionally by origin.
func (operation ListSuppressionsOperation) Invoke(call sdkgo.Call, input ListSuppressionsInput) sdkgo.QueryAttempt[SuppressionPage] {
	if input.Origin != "" && input.Origin != "bounce" && input.Origin != "complaint" && input.Origin != "manual" {
		return queryFailure[SuppressionPage]("listSuppressions", ListSuppressionsBranchDefect, sdkgo.FailureValidation, "origin must be bounce, complaint, or manual")
	}
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[SuppressionPage]("listSuppressions", ListSuppressionsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	if input.Origin != "" {
		values, _ := url.ParseQuery(strings.TrimPrefix(query, "?"))
		values.Set("origin", input.Origin)
		query = "?" + values.Encode()
	}
	return runQuery(operation.client, call, "listSuppressions", "/suppressions"+query, ListSuppressionsBranchListed, "", ListSuppressionsBranchProviderRejected, ListSuppressionsBranchInvalidResponse, ListSuppressionsBranchDefect, decodeSuppressionPage)
}

// Invoke retrieves one suppression by ID or email.
func (operation GetSuppressionOperation) Invoke(call sdkgo.Call, input GetSuppressionInput) sdkgo.QueryAttempt[Suppression] {
	if strings.TrimSpace(input.Suppression) == "" {
		return queryFailure[Suppression]("getSuppression", GetSuppressionBranchDefect, sdkgo.FailureValidation, "suppression is required")
	}
	return runQuery(operation.client, call, "getSuppression", "/suppressions/"+url.PathEscape(input.Suppression), GetSuppressionBranchFound, GetSuppressionBranchNotFound, GetSuppressionBranchProviderRejected, GetSuppressionBranchInvalidResponse, GetSuppressionBranchDefect, decodeSuppression)
}

// Invoke removes one suppression by ID or email.
func (operation DeleteSuppressionOperation) Invoke(call sdkgo.Call, input DeleteSuppressionInput) sdkgo.MutationAttempt[DeletedSuppression] {
	if strings.TrimSpace(input.Suppression) == "" {
		return mutationFailure[DeletedSuppression]("deleteSuppression", DeleteSuppressionBranchDefect, sdkgo.FailureValidation, "suppression is required")
	}
	return runMutation(operation.client, call, "deleteSuppression", http.MethodDelete, "/suppressions/"+url.PathEscape(input.Suppression), nil, "", "", DeleteSuppressionBranchDeleted, DeleteSuppressionBranchNotFound, DeleteSuppressionBranchProviderRejected, DeleteSuppressionBranchInvalidResponse, DeleteSuppressionBranchDefect, decodeDeletedSuppression)
}

// Invoke adds one batch of addresses to the suppression list.
func (operation BatchAddSuppressionsOperation) Invoke(call sdkgo.Call, input BatchAddSuppressionsInput) sdkgo.MutationAttempt[SuppressionBatch] {
	if !validSuppressionBatch(input.Emails) {
		return mutationFailure[SuppressionBatch]("batchAddSuppressions", BatchAddSuppressionsBranchDefect, sdkgo.FailureValidation, "1 through 100 nonblank emails are required")
	}
	body, _ := json.Marshal(map[string][]string{"emails": input.Emails})
	request := resendLimitedCreate[SuppressionBatch]{client: operation.client, call: call, operation: "batchAddSuppressions", path: "/suppressions/batch/add", body: body, timeout: BatchAddSuppressionsDefinition.StepDefaults.ExecuteMethodTimeout, created: BatchAddSuppressionsBranchAdded, rejected: BatchAddSuppressionsBranchProviderRejected, defect: BatchAddSuppressionsBranchDefect, decode: decodeSuppressionBatch, readBack: func(call sdkgo.Call) (SuppressionBatch, readBackState) {
		return operation.readBackSuppressions(call, input.Emails)
	}}
	return request.invoke()
}

// Invoke removes one batch of suppressions by email or ID.
func (operation BatchRemoveSuppressionsOperation) Invoke(call sdkgo.Call, input BatchRemoveSuppressionsInput) sdkgo.MutationAttempt[RemovedSuppressionBatch] {
	if (len(input.Emails) > 0 && len(input.IDs) > 0) || (len(input.Emails) == 0 && len(input.IDs) == 0) || (len(input.Emails) > 0 && !validSuppressionBatch(input.Emails)) || (len(input.IDs) > 0 && !validSuppressionBatch(input.IDs)) {
		return mutationFailure[RemovedSuppressionBatch]("batchRemoveSuppressions", BatchRemoveSuppressionsBranchDefect, sdkgo.FailureValidation, "provide 1 through 100 emails or IDs, never both")
	}
	fields := map[string]any{}
	if len(input.Emails) > 0 {
		fields["emails"] = input.Emails
	} else {
		fields["ids"] = input.IDs
	}
	body, _ := json.Marshal(fields)
	return runMutation(operation.client, call, "batchRemoveSuppressions", http.MethodPost, "/suppressions/batch/remove", body, "", "", BatchRemoveSuppressionsBranchRemoved, "", BatchRemoveSuppressionsBranchProviderRejected, BatchRemoveSuppressionsBranchInvalidResponse, BatchRemoveSuppressionsBranchDefect, decodeRemovedSuppressionBatch)
}

func (operation CreateSuppressionOperation) readBackSuppression(call sdkgo.Call, email string) (SuppressionReference, readBackState) {
	body, state := operation.client.readBackResource(call, "/suppressions/"+url.PathEscape(email))
	if state != readBackMatched {
		return SuppressionReference{}, state
	}
	suppression, err := decodeSuppression(body)
	if err != nil {
		return SuppressionReference{}, readBackRetry
	}
	if !strings.EqualFold(suppression.Email, email) {
		return SuppressionReference{}, readBackConflicted
	}
	return SuppressionReference{ID: suppression.ID}, readBackMatched
}

func (operation BatchAddSuppressionsOperation) readBackSuppressions(call sdkgo.Call, emails []string) (SuppressionBatch, readBackState) {
	batch := SuppressionBatch{Data: make([]SuppressionReference, 0, len(emails))}
	for _, email := range emails {
		body, state := operation.client.readBackResource(call, "/suppressions/"+url.PathEscape(email))
		if state != readBackMatched {
			return SuppressionBatch{}, state
		}
		suppression, err := decodeSuppression(body)
		if err != nil {
			return SuppressionBatch{}, readBackRetry
		}
		if !strings.EqualFold(suppression.Email, email) {
			return SuppressionBatch{}, readBackConflicted
		}
		batch.Data = append(batch.Data, SuppressionReference{ID: suppression.ID})
	}
	return batch, readBackMatched
}

func validSuppressionBatch(values []string) bool {
	if len(values) < 1 || len(values) > 100 {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}
func decodeSuppressionReference(body []byte) (SuppressionReference, error) {
	var value SuppressionReference
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" {
		return SuppressionReference{}, fmt.Errorf("invalid suppression reference")
	}
	return value, nil
}
func decodeSuppression(body []byte) (Suppression, error) {
	var value Suppression
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" || value.Email == "" {
		return Suppression{}, fmt.Errorf("invalid suppression response")
	}
	return value, nil
}
func decodeSuppressionPage(body []byte) (SuppressionPage, error) {
	var value SuppressionPage
	if err := json.Unmarshal(body, &value); err != nil || value.Data == nil {
		return SuppressionPage{}, fmt.Errorf("invalid suppression page")
	}
	return value, nil
}
func decodeSuppressionBatch(body []byte) (SuppressionBatch, error) {
	var value SuppressionBatch
	if err := json.Unmarshal(body, &value); err != nil || len(value.Data) == 0 {
		return SuppressionBatch{}, fmt.Errorf("invalid suppression batch")
	}
	for _, item := range value.Data {
		if item.ID == "" {
			return SuppressionBatch{}, fmt.Errorf("invalid suppression batch item")
		}
	}
	return value, nil
}
func decodeDeletedSuppression(body []byte) (DeletedSuppression, error) {
	var value DeletedSuppression
	if err := json.Unmarshal(body, &value); err != nil || value.ID == "" || !value.Deleted {
		return DeletedSuppression{}, fmt.Errorf("invalid suppression removal")
	}
	return value, nil
}
func decodeRemovedSuppressionBatch(body []byte) (RemovedSuppressionBatch, error) {
	var value RemovedSuppressionBatch
	if err := json.Unmarshal(body, &value); err != nil || len(value.Data) == 0 {
		return RemovedSuppressionBatch{}, fmt.Errorf("invalid suppression removal batch")
	}
	for _, item := range value.Data {
		if item.ID == "" || !item.Deleted {
			return RemovedSuppressionBatch{}, fmt.Errorf("invalid suppression removal item")
		}
	}
	return value, nil
}
