// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshsales

import (
	"encoding/json"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var dealAmountPattern = regexp.MustCompile(`^[0-9]{1,13}(?:\.[0-9]{1,2})?$`)

// ListDealViewsInput requests accessible deal views.
type ListDealViewsInput struct{}

// GetDealInput identifies one deal.
type GetDealInput struct {
	DealID string `json:"dealId"`
}

// ListDealsInput selects a one-based page of a deal view.
type ListDealsInput struct {
	ViewID string `json:"viewId"`
	Page   int    `json:"page,omitempty"`
}

// CreateDealInput names a deal, its decimal amount, and its existing sales account.
type CreateDealInput struct {
	Name           string `json:"name"`
	Amount         string `json:"amount"`
	SalesAccountID string `json:"salesAccountId"`
}

// UpdateDealInput replaces one deal name.
type UpdateDealInput struct {
	DealID string `json:"dealId"`
	Name   string `json:"name"`
}

// DeleteDealInput identifies the deal to delete.
type DeleteDealInput struct {
	DealID string `json:"dealId"`
}

// ListDealViewsOperation discovers deal views.
type ListDealViewsOperation struct{ client *Client }

// GetDealOperation reads one deal.
type GetDealOperation struct{ client *Client }

// ListDealsOperation lists one deal view page.
type ListDealsOperation struct{ client *Client }

// CreateDealOperation creates a deal with bounded sends.
type CreateDealOperation struct{ client *Client }

// UpdateDealOperation replaces a deal name.
type UpdateDealOperation struct{ client *Client }

// DeleteDealOperation deletes one deal.
type DeleteDealOperation struct{ client *Client }

// ListDealViews binds view discovery to this client.
func (client *Client) ListDealViews() ListDealViewsOperation { return ListDealViewsOperation{client} }

// GetDeal binds the query to this client.
func (client *Client) GetDeal() GetDealOperation { return GetDealOperation{client} }

// ListDeals binds the query to this client.
func (client *Client) ListDeals() ListDealsOperation { return ListDealsOperation{client} }

// CreateDeal binds the mutation to this client.
func (client *Client) CreateDeal() CreateDealOperation { return CreateDealOperation{client} }

// UpdateDeal binds the mutation to this client.
func (client *Client) UpdateDeal() UpdateDealOperation { return UpdateDealOperation{client} }

// DeleteDeal binds the mutation to this client.
func (client *Client) DeleteDeal() DeleteDealOperation { return DeleteDealOperation{client} }

// Definition returns the manifest-generated operation contract.
func (ListDealViewsOperation) Definition() sdkgo.QueryDefinition { return ListDealViewsDefinition }

// Definition returns the manifest-generated operation contract.
func (GetDealOperation) Definition() sdkgo.QueryDefinition { return GetDealDefinition }

// Definition returns the manifest-generated operation contract.
func (ListDealsOperation) Definition() sdkgo.QueryDefinition { return ListDealsDefinition }

// Definition returns the manifest-generated operation contract.
func (CreateDealOperation) Definition() sdkgo.MutationDefinition { return CreateDealDefinition }

// Definition returns the manifest-generated operation contract.
func (UpdateDealOperation) Definition() sdkgo.MutationDefinition { return UpdateDealDefinition }

// Definition returns the manifest-generated operation contract.
func (DeleteDealOperation) Definition() sdkgo.MutationDefinition { return DeleteDealDefinition }

// IdempotencyKey correlates a Dex call; Freshsales does not deduplicate this create.
func (CreateDealOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateDealInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates a Dex call; repeated replacement is harmless.
func (UpdateDealOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateDealInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates a Dex call; repeated deletion is harmless.
func (DeleteDealOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteDealInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists deal views visible to the API user.
func (operation ListDealViewsOperation) Invoke(call sdkgo.Call, _ ListDealViewsInput) sdkgo.QueryAttempt[CRMViews] {
	branches := queryBranches{found: ListDealViewsBranchListed, rejected: ListDealViewsBranchProviderRejected, invalid: ListDealViewsBranchInvalidResponse, defect: ListDealViewsBranchDefect}
	return invokeNamedViews(call, operation.client, "listDealViews", "/deals/filters", branches)
}

// Invoke reads one deal.
func (operation GetDealOperation) Invoke(call sdkgo.Call, input GetDealInput) sdkgo.QueryAttempt[CRMRecordMetadata] {
	branches := queryBranches{found: GetDealBranchFound, notFound: GetDealBranchNotFound, rejected: GetDealBranchProviderRejected, invalid: GetDealBranchInvalidResponse, defect: GetDealBranchDefect}
	if !isPositiveContactID(input.DealID) {
		return queryBranch("getDeal", branches.defect, CRMRecordMetadata{}, sdkgo.FailureValidation, "deal ID is invalid")
	}
	return invokeNamedGet(call, operation.client, "getDeal", "/deals/"+input.DealID, "deal", branches)
}

// Invoke lists one page from a deal view.
func (operation ListDealsOperation) Invoke(call sdkgo.Call, input ListDealsInput) sdkgo.QueryAttempt[CRMRecordsPage] {
	branches := queryBranches{found: ListDealsBranchListed, notFound: ListDealsBranchNotFound, rejected: ListDealsBranchProviderRejected, invalid: ListDealsBranchInvalidResponse, defect: ListDealsBranchDefect}
	if input.Page == 0 {
		input.Page = 1
	}
	if !isPositiveContactID(input.ViewID) || input.Page < 1 || input.Page > 10000 {
		return queryBranch("listDeals", branches.defect, CRMRecordsPage{}, sdkgo.FailureValidation, "view ID or page is invalid")
	}
	return invokeNamedList(call, operation.client, "listDeals", "/deals/view/"+input.ViewID+"?page="+strconv.Itoa(input.Page), "deals", input.Page, branches)
}

// Invoke creates a deal with a bounded number of possible sends.
func (operation CreateDealOperation) Invoke(call sdkgo.Call, input CreateDealInput) sdkgo.MutationAttempt[CRMRecordMetadata] {
	if validateCRMName(input.Name) != nil || !dealAmountPattern.MatchString(input.Amount) || !isPositiveContactID(input.SalesAccountID) {
		return createBranch[CRMRecordMetadata]("createDeal", CreateDealBranchDefect, sdkgo.FailureValidation, "deal name, decimal amount, or sales account ID is invalid")
	}
	accountID, _ := strconv.ParseInt(input.SalesAccountID, 10, 64)
	requestBody, err := json.Marshal(map[string]any{"deal": map[string]any{"name": input.Name, "amount": json.Number(input.Amount), "sales_account_id": accountID}})
	if err != nil {
		return createBranch[CRMRecordMetadata]("createDeal", CreateDealBranchDefect, sdkgo.FailureLocalDefect, "deal request cannot be encoded")
	}
	return invokeLimitedCreate(operation.client, call, "createDeal", "/deals", requestBody, CreateDealDefinition.StepDefaults.ExecuteMethodTimeout, createBranches{CreateDealBranchCreated, CreateDealBranchProviderRejected, CreateDealBranchDefect}, func(body []byte) (CRMRecordMetadata, error) { return decodeCRMRecord(body, "deal") }, func(record CRMRecordMetadata) string { return strconv.FormatInt(record.ID, 10) }, func(id string) string { return "/deals/" + id })
}

// Invoke replaces a deal name and confirms it by read-back.
func (operation UpdateDealOperation) Invoke(call sdkgo.Call, input UpdateDealInput) sdkgo.MutationAttempt[CRMRecordMetadata] {
	branches := namedRecordBranches{UpdateDealBranchUpdated, UpdateDealBranchNotFound, UpdateDealBranchProviderRejected, UpdateDealBranchInvalidResponse, UpdateDealBranchDefect}
	if !isPositiveContactID(input.DealID) {
		return namedBranch("updateDeal", branches.defect, sdkgo.FailureValidation, "deal ID is invalid")
	}
	return invokeNamedUpdate(operation.client, call, "updateDeal", "/deals/"+input.DealID, "deal", input.Name, branches)
}

// Invoke deletes one deal.
func (operation DeleteDealOperation) Invoke(call sdkgo.Call, input DeleteDealInput) sdkgo.MutationAttempt[DeletedCRMRecord] {
	if !isPositiveContactID(input.DealID) {
		return deletedNamedBranch("deleteDeal", DeleteDealBranchDefect, sdkgo.FailureValidation, "deal ID is invalid")
	}
	id, _ := strconv.ParseInt(input.DealID, 10, 64)
	return invokeNamedDelete(operation.client, call, "deleteDeal", "/deals/"+input.DealID, id, DeleteDealBranchDeleted, DeleteDealBranchProviderRejected, DeleteDealBranchDefect)
}
