// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshsales

import (
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListAccountViewsInput requests accessible sales account views.
type ListAccountViewsInput struct{}

// GetAccountInput identifies one sales account.
type GetAccountInput struct {
	AccountID string `json:"accountId"`
}

// ListAccountsInput selects a one-based page of a sales account view.
type ListAccountsInput struct {
	ViewID string `json:"viewId"`
	Page   int    `json:"page,omitempty"`
}

// CreateAccountInput names one new sales account.
type CreateAccountInput struct {
	Name string `json:"name"`
}

// UpdateAccountInput replaces one sales account name.
type UpdateAccountInput struct {
	AccountID string `json:"accountId"`
	Name      string `json:"name"`
}

// DeleteAccountInput identifies the sales account to delete.
type DeleteAccountInput struct {
	AccountID string `json:"accountId"`
}

// ListAccountViewsOperation discovers sales account views.
type ListAccountViewsOperation struct{ client *Client }

// GetAccountOperation reads a sales account.
type GetAccountOperation struct{ client *Client }

// ListAccountsOperation lists sales accounts in a view.
type ListAccountsOperation struct{ client *Client }

// CreateAccountOperation creates a sales account with bounded sends.
type CreateAccountOperation struct{ client *Client }

// UpdateAccountOperation replaces a sales account name.
type UpdateAccountOperation struct{ client *Client }

// DeleteAccountOperation deletes one sales account.
type DeleteAccountOperation struct{ client *Client }

// ListAccountViews binds view discovery to this client.
func (client *Client) ListAccountViews() ListAccountViewsOperation {
	return ListAccountViewsOperation{client}
}

// GetAccount binds the query to this client.
func (client *Client) GetAccount() GetAccountOperation { return GetAccountOperation{client} }

// ListAccounts binds the query to this client.
func (client *Client) ListAccounts() ListAccountsOperation { return ListAccountsOperation{client} }

// CreateAccount binds the mutation to this client.
func (client *Client) CreateAccount() CreateAccountOperation { return CreateAccountOperation{client} }

// UpdateAccount binds the mutation to this client.
func (client *Client) UpdateAccount() UpdateAccountOperation { return UpdateAccountOperation{client} }

// DeleteAccount binds the mutation to this client.
func (client *Client) DeleteAccount() DeleteAccountOperation { return DeleteAccountOperation{client} }

// Definition returns the manifest-generated operation contract.
func (ListAccountViewsOperation) Definition() sdkgo.QueryDefinition {
	return ListAccountViewsDefinition
}

// Definition returns the manifest-generated operation contract.
func (GetAccountOperation) Definition() sdkgo.QueryDefinition { return GetAccountDefinition }

// Definition returns the manifest-generated operation contract.
func (ListAccountsOperation) Definition() sdkgo.QueryDefinition { return ListAccountsDefinition }

// Definition returns the manifest-generated operation contract.
func (CreateAccountOperation) Definition() sdkgo.MutationDefinition { return CreateAccountDefinition }

// Definition returns the manifest-generated operation contract.
func (UpdateAccountOperation) Definition() sdkgo.MutationDefinition { return UpdateAccountDefinition }

// Definition returns the manifest-generated operation contract.
func (DeleteAccountOperation) Definition() sdkgo.MutationDefinition { return DeleteAccountDefinition }

// IdempotencyKey correlates a Dex call; Freshsales does not deduplicate this create.
func (CreateAccountOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateAccountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates a Dex call; repeated replacement is harmless.
func (UpdateAccountOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateAccountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates a Dex call; repeated deletion is harmless.
func (DeleteAccountOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteAccountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists account views visible to the API user.
func (operation ListAccountViewsOperation) Invoke(call sdkgo.Call, _ ListAccountViewsInput) sdkgo.QueryAttempt[CRMViews] {
	branches := queryBranches{found: ListAccountViewsBranchListed, rejected: ListAccountViewsBranchProviderRejected, invalid: ListAccountViewsBranchInvalidResponse, defect: ListAccountViewsBranchDefect}
	return invokeNamedViews(call, operation.client, "listAccountViews", "/sales_accounts/filters", branches)
}

// Invoke reads one sales account.
func (operation GetAccountOperation) Invoke(call sdkgo.Call, input GetAccountInput) sdkgo.QueryAttempt[CRMRecordMetadata] {
	branches := queryBranches{found: GetAccountBranchFound, notFound: GetAccountBranchNotFound, rejected: GetAccountBranchProviderRejected, invalid: GetAccountBranchInvalidResponse, defect: GetAccountBranchDefect}
	if !isPositiveContactID(input.AccountID) {
		return queryBranch("getAccount", branches.defect, CRMRecordMetadata{}, sdkgo.FailureValidation, "account ID is invalid")
	}
	return invokeNamedGet(call, operation.client, "getAccount", "/sales_accounts/"+input.AccountID, "sales_account", branches)
}

// Invoke lists one page from a sales account view.
func (operation ListAccountsOperation) Invoke(call sdkgo.Call, input ListAccountsInput) sdkgo.QueryAttempt[CRMRecordsPage] {
	branches := queryBranches{found: ListAccountsBranchListed, notFound: ListAccountsBranchNotFound, rejected: ListAccountsBranchProviderRejected, invalid: ListAccountsBranchInvalidResponse, defect: ListAccountsBranchDefect}
	if input.Page == 0 {
		input.Page = 1
	}
	if !isPositiveContactID(input.ViewID) || input.Page < 1 || input.Page > 10000 {
		return queryBranch("listAccounts", branches.defect, CRMRecordsPage{}, sdkgo.FailureValidation, "view ID or page is invalid")
	}
	return invokeNamedList(call, operation.client, "listAccounts", "/sales_accounts/view/"+input.ViewID+"?page="+strconv.Itoa(input.Page), "sales_accounts", input.Page, branches)
}

// Invoke creates a sales account with a bounded number of possible sends.
func (operation CreateAccountOperation) Invoke(call sdkgo.Call, input CreateAccountInput) sdkgo.MutationAttempt[CRMRecordMetadata] {
	return invokeNamedCreate(operation.client, call, "createAccount", "/sales_accounts", "sales_account", input.Name, CreateAccountDefinition.StepDefaults.ExecuteMethodTimeout, createBranches{CreateAccountBranchCreated, CreateAccountBranchProviderRejected, CreateAccountBranchDefect})
}

// Invoke replaces a sales account name and confirms it by read-back.
func (operation UpdateAccountOperation) Invoke(call sdkgo.Call, input UpdateAccountInput) sdkgo.MutationAttempt[CRMRecordMetadata] {
	branches := namedRecordBranches{UpdateAccountBranchUpdated, UpdateAccountBranchNotFound, UpdateAccountBranchProviderRejected, UpdateAccountBranchInvalidResponse, UpdateAccountBranchDefect}
	if !isPositiveContactID(input.AccountID) {
		return namedBranch("updateAccount", branches.defect, sdkgo.FailureValidation, "account ID is invalid")
	}
	return invokeNamedUpdate(operation.client, call, "updateAccount", "/sales_accounts/"+input.AccountID, "sales_account", input.Name, branches)
}

// Invoke deletes a sales account without requesting dependent-record deletion.
func (operation DeleteAccountOperation) Invoke(call sdkgo.Call, input DeleteAccountInput) sdkgo.MutationAttempt[DeletedCRMRecord] {
	if !isPositiveContactID(input.AccountID) {
		return deletedNamedBranch("deleteAccount", DeleteAccountBranchDefect, sdkgo.FailureValidation, "account ID is invalid")
	}
	id, _ := strconv.ParseInt(input.AccountID, 10, 64)
	return invokeNamedDelete(operation.client, call, "deleteAccount", "/sales_accounts/"+input.AccountID, id, DeleteAccountBranchDeleted, DeleteAccountBranchProviderRejected, DeleteAccountBranchDefect)
}
