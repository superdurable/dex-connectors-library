// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateAccountInput supplies a name for a new account.
type CreateAccountInput struct {
	// Name is the required full name.
	Name string `json:"name"`
}

// UpdateAccountInput replaces selected account fields.
type UpdateAccountInput struct {
	// AccountID identifies an existing account.
	AccountID string `json:"accountId"`
	// Name replaces the full name when set.
	Name *string `json:"name,omitempty"`
}

// DeleteAccountInput identifies a account to delete.
type DeleteAccountInput struct {
	// AccountID is the Account.Id UUID.
	AccountID string `json:"accountId"`
}

// AccountWriteOutput confirms the target UUID of an update or deletion.
type AccountWriteOutput struct {
	// ID is the target Account.Id UUID.
	ID string `json:"id"`
}

// CreateAccountOperation creates one account with bounded sends.
type CreateAccountOperation struct{ client *Client }

// UpdateAccountOperation changes selected fields of one account.
type UpdateAccountOperation struct{ client *Client }

// DeleteAccountOperation removes one account.
type DeleteAccountOperation struct{ client *Client }

// CreateAccount binds the mutation to this client.
func (client *Client) CreateAccount() CreateAccountOperation { return CreateAccountOperation{client} }

// UpdateAccount binds the mutation to this client.
func (client *Client) UpdateAccount() UpdateAccountOperation { return UpdateAccountOperation{client} }

// DeleteAccount binds the mutation to this client.
func (client *Client) DeleteAccount() DeleteAccountOperation { return DeleteAccountOperation{client} }

// Definition returns the generated mutation contract.
func (CreateAccountOperation) Definition() sdkgo.MutationDefinition { return CreateAccountDefinition }

// Definition returns the generated mutation contract.
func (UpdateAccountOperation) Definition() sdkgo.MutationDefinition { return UpdateAccountDefinition }

// Definition returns the generated mutation contract.
func (DeleteAccountOperation) Definition() sdkgo.MutationDefinition { return DeleteAccountDefinition }

// IdempotencyKey correlates bounded sends; Creatio does not deduplicate this key.
func (CreateAccountOperation) IdempotencyKey(id sdkgo.CallID, _ CreateAccountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies one repeatable absolute update.
func (UpdateAccountOperation) IdempotencyKey(id sdkgo.CallID, _ UpdateAccountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies one repeatable deletion.
func (DeleteAccountOperation) IdempotencyKey(id sdkgo.CallID, _ DeleteAccountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke creates an account and reads back a name match before resending.
func (operation CreateAccountOperation) Invoke(call sdkgo.Call, input CreateAccountInput) sdkgo.MutationAttempt[AccountSummary] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 {
		return createAccountBranch(CreateAccountBranchDefect, sdkgo.FailureValidation, "invalid account name")
	}
	fence, err := singledispatch.NewFence(call, CreateAccountDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createAccountBranch(CreateAccountBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return createAccountUncertain("previous creation cannot be confirmed")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
			return createAccountBranch(CreateAccountBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return sdkgo.NewMutationRetry[AccountSummary](accountsFailure("createAccount", sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
	}
	if decision.Kind != singledispatch.SendAllowed {
		if decision.Marker.ProviderID != "" {
			response := operation.client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Account("+decision.Marker.ProviderID+")?$select=Id%2CName", nil)
			if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
				return sdkgo.NewMutationRetry[AccountSummary](accountsFailure("createAccount", sdkgo.FailureAvailability, "Creatio account read-back unavailable"), response.retryAfter)
			}
			if response.status != http.StatusOK {
				return createAccountUncertain("saved account ID cannot be confirmed")
			}
			account, decodeErr := decodeAccountRecord(response.body)
			if decodeErr != nil || !strings.EqualFold(account.ID, decision.Marker.ProviderID) {
				return createAccountUncertain("saved account ID returned an invalid account")
			}
			return sdkgo.NewMutationBranch(CreateAccountBranchCreated, account, nil, sdkgo.Receipt{})
		}
		if input.Name != "" {
			matching, known, retry := operation.client.findAccountByName(call, token, input.Name)
			if retry {
				return sdkgo.NewMutationRetry[AccountSummary](accountsFailure("createAccount", sdkgo.FailureAvailability, "Creatio account read-back unavailable"), 0)
			}
			if matching != nil {
				if strings.EqualFold(matching.ID, decision.Marker.ProviderID) {
					return sdkgo.NewMutationBranch(CreateAccountBranchCreated, *matching, nil, sdkgo.Receipt{})
				}
				return createAccountUncertain("a matching account cannot prove this creation")
			}
			if !known {
				return createAccountUncertain("previous creation cannot be confirmed")
			}
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return createAccountUncertain("previous creation cannot be confirmed")
		}
	}
	body, marshalErr := json.Marshal(struct {
		Name string `json:"Name"`
	}{input.Name})
	if marshalErr != nil {
		return createAccountBranch(CreateAccountBranchDefect, sdkgo.FailureLocalDefect, "account request could not be encoded")
	}
	var response odataResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.doODataRequest(requestContext, token, http.MethodPost, "/0/odata/Account", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[AccountSummary](accountsFailure("createAccount", sdkgo.FailureAvailability, "dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[AccountSummary](accountsFailure("createAccount", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[AccountSummary](accountsFailure("createAccount", kind, "Creatio did not create the account"), response.retryAfter)
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[AccountSummary](accountsFailure("createAccount", sdkgo.FailureTransport, "account creation outcome is unknown"), 0)
	}
	if response.status >= 400 {
		if decision.MaximumPriorSends > 0 {
			return createAccountUncertain("an earlier account creation may have succeeded")
		}
		return createAccountBranch(CreateAccountBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.status))
	}
	account, decodeErr := decodeAccountRecord(response.body)
	if response.status != http.StatusCreated || decodeErr != nil {
		return sdkgo.NewMutationRetry[AccountSummary](accountsFailure("createAccount", sdkgo.FailureProtocol, "Creatio returned invalid account creation JSON"), 0)
	}
	if err := fence.RecordProviderID(account.ID); err != nil {
		return sdkgo.NewMutationRetry[AccountSummary](accountsFailure("createAccount", sdkgo.FailureAvailability, "provider ID checkpoint unavailable"), 0)
	}
	return sdkgo.NewMutationBranch(CreateAccountBranchCreated, account, nil, sdkgo.Receipt{})
}

// Invoke patches the named fields without changing other account properties.
func (operation UpdateAccountOperation) Invoke(call sdkgo.Call, input UpdateAccountInput) sdkgo.MutationAttempt[AccountWriteOutput] {
	if !contactUUIDPattern.MatchString(input.AccountID) || input.Name == nil || strings.TrimSpace(*input.Name) == "" {
		return accountWriteBranch("updateAccount", UpdateAccountBranchDefect, sdkgo.FailureValidation, "invalid account update")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		return accountWriteCredentialFailure("updateAccount", UpdateAccountBranchDefect, err)
	}
	fields := map[string]any{}
	if input.Name != nil {
		fields["Name"] = *input.Name
	}
	body, marshalErr := json.Marshal(fields)
	if marshalErr != nil {
		return accountWriteBranch("updateAccount", UpdateAccountBranchDefect, sdkgo.FailureLocalDefect, "account update could not be encoded")
	}
	response := operation.client.doODataRequest(call.Context, token, http.MethodPatch, "/0/odata/Account("+input.AccountID+")", body)
	return classifyAccountWrite("updateAccount", input.AccountID, response, UpdateAccountBranchUpdated, UpdateAccountBranchNotFound, UpdateAccountBranchProviderRejected)
}

// Invoke permanently deletes one Account record.
func (operation DeleteAccountOperation) Invoke(call sdkgo.Call, input DeleteAccountInput) sdkgo.MutationAttempt[AccountWriteOutput] {
	if !contactUUIDPattern.MatchString(input.AccountID) {
		return accountWriteBranch("deleteAccount", DeleteAccountBranchDefect, sdkgo.FailureValidation, "invalid account UUID")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		return accountWriteCredentialFailure("deleteAccount", DeleteAccountBranchDefect, err)
	}
	response := operation.client.doODataRequest(call.Context, token, http.MethodDelete, "/0/odata/Account("+input.AccountID+")", nil)
	return classifyAccountWrite("deleteAccount", input.AccountID, response, DeleteAccountBranchDeleted, DeleteAccountBranchNotFound, DeleteAccountBranchProviderRejected)
}

func (client *Client) findAccountByName(call sdkgo.Call, token string, name string) (*AccountSummary, bool, bool) {
	escape := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	filter := "Name eq '" + escape(name) + "'"
	query := url.Values{"$filter": {filter}, "$select": {"Id,Name"}, "$top": {"2"}}
	response := client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Account?"+query.Encode(), nil)
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return nil, false, true
	}
	if response.status != http.StatusOK {
		return nil, false, false
	}
	var envelope struct {
		Value []json.RawMessage `json:"value"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Value == nil {
		return nil, false, true
	}
	if len(envelope.Value) == 0 {
		return nil, true, false
	}
	for _, record := range envelope.Value {
		account, err := decodeAccountRecord(record)
		if err != nil {
			return nil, false, true
		}
		if account.Name == name {
			return &account, true, false
		}
	}
	return nil, true, false
}

func classifyAccountWrite(operation string, targetID string, response odataResponse, successBranch sdkgo.BranchID, notFoundBranch sdkgo.BranchID, rejectedBranch sdkgo.BranchID) sdkgo.MutationAttempt[AccountWriteOutput] {
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[AccountWriteOutput](accountsFailure(operation, sdkgo.FailureRateLimit, "Creatio rate limited the write"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[AccountWriteOutput](accountsFailure(operation, sdkgo.FailureAvailability, "Creatio write is unavailable"), response.retryAfter)
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[AccountWriteOutput](accountsFailure(operation, sdkgo.FailureTransport, "Creatio write response failed"), 0)
	}
	if response.status == http.StatusNotFound {
		return accountWriteBranch(operation, notFoundBranch, sdkgo.FailureNotFound, "Creatio account was not found")
	}
	if response.status != http.StatusNoContent {
		return accountWriteBranch(operation, rejectedBranch, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.status))
	}
	return sdkgo.NewMutationBranch(successBranch, AccountWriteOutput{ID: targetID}, nil, sdkgo.Receipt{})
}

func accountWriteCredentialFailure(operation string, defectBranch sdkgo.BranchID, err error) sdkgo.MutationAttempt[AccountWriteOutput] {
	if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
		return accountWriteBranch(operation, defectBranch, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
	}
	return sdkgo.NewMutationRetry[AccountWriteOutput](accountsFailure(operation, sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
}

func accountWriteBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AccountWriteOutput] {
	failure := accountsFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, AccountWriteOutput{}, &failure, sdkgo.Receipt{})
}

func createAccountBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AccountSummary] {
	failure := accountsFailure("createAccount", kind, message)
	return sdkgo.NewMutationBranch(branch, AccountSummary{}, &failure, sdkgo.Receipt{})
}

func createAccountUncertain(message string) sdkgo.MutationAttempt[AccountSummary] {
	return sdkgo.NewMutationUncertain(AccountSummary{}, accountsFailure("createAccount", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}
