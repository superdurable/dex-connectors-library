// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateAccountInput creates a CRM account by its name and optional website.
type CreateAccountInput struct {
	// Name is the account's 1–250-character display name.
	Name string `json:"name"`
	// AccountURL is the optional public company website.
	AccountURL string `json:"accountUrl,omitempty"`
}

// UpdateAccountInput replaces selected account fields by numeric ID.
type UpdateAccountInput struct {
	// AccountID is the numeric CRM account ID.
	AccountID string `json:"accountId"`
	// Name replaces the name when supplied.
	Name *string `json:"name,omitempty"`
	// AccountURL replaces the website when supplied; blank clears it.
	AccountURL *string `json:"accountUrl,omitempty"`
}

// DeleteAccountInput selects one CRM account for deletion.
type DeleteAccountInput struct {
	// AccountID is the numeric CRM account ID.
	AccountID string `json:"accountId"`
}

// AccountDeletion confirms the selected account is absent.
type AccountDeletion struct {
	// AccountID is the account removed or already absent.
	AccountID string `json:"accountId"`
	// Deleted is true when the account is absent.
	Deleted bool `json:"deleted"`
}

// CreateAccountOperation creates an account with bounded sends.
type CreateAccountOperation struct{ client *Client }

// UpdateAccountOperation replaces selected account fields.
type UpdateAccountOperation struct{ client *Client }

// DeleteAccountOperation removes one account by ID.
type DeleteAccountOperation struct{ client *Client }

type accountCreateDispatch struct {
	client     *Client
	call       sdkgo.Call
	body       []byte
	response   []byte
	status     int
	retryAfter time.Duration
	err        error
}

// CreateAccount binds the create Mutation.
func (client *Client) CreateAccount() CreateAccountOperation { return CreateAccountOperation{client} }

// UpdateAccount binds the update Mutation.
func (client *Client) UpdateAccount() UpdateAccountOperation { return UpdateAccountOperation{client} }

// DeleteAccount binds the delete Mutation.
func (client *Client) DeleteAccount() DeleteAccountOperation { return DeleteAccountOperation{client} }

// Definition returns the generated create contract.
func (CreateAccountOperation) Definition() sdkgo.MutationDefinition { return CreateAccountDefinition }

// Definition returns the generated update contract.
func (UpdateAccountOperation) Definition() sdkgo.MutationDefinition { return UpdateAccountDefinition }

// Definition returns the generated delete contract.
func (DeleteAccountOperation) Definition() sdkgo.MutationDefinition { return DeleteAccountDefinition }

// IdempotencyKey correlates bounded sends locally; the provider receives no key.
func (CreateAccountOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateAccountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies an absolute replacement.
func (UpdateAccountOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateAccountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies a convergent deletion.
func (DeleteAccountOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteAccountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an account after checking whether an earlier send succeeded.
func (operation CreateAccountOperation) Invoke(call sdkgo.Call, input CreateAccountInput) sdkgo.MutationAttempt[AccountMetadata] {
	if !validAccountName(input.Name) || !validAccountURL(input.AccountURL) {
		return accountMutationBranch("createAccount", CreateAccountBranchDefect, sdkgo.FailureValidation, "account name or URL is invalid")
	}
	encoded, err := json.Marshal(map[string]any{"account": map[string]string{"name": input.Name, "accountUrl": input.AccountURL}})
	if err != nil {
		return accountMutationBranch("createAccount", CreateAccountBranchDefect, sdkgo.FailureLocalDefect, "account request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateAccountDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return accountMutationBranch("createAccount", CreateAccountBranchDefect, sdkgo.FailureLocalDefect, "account checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return accountMutationBranch("createAccount", CreateAccountBranchDefect, sdkgo.FailureLocalDefect, "account checkpoint is invalid")
	}
	if decision.MaximumPriorSends > 0 || decision.Marker.ProviderID != "" {
		found, state := operation.readBack(call, input, decision.Marker.ProviderID)
		switch state {
		case "found":
			return sdkgo.NewMutationBranch(CreateAccountBranchCreated, found, nil, dealReceipt(call, found.ID))
		case "retry":
			return sdkgo.NewMutationRetry[AccountMetadata](queryFailure("createAccount", sdkgo.FailureAvailability, "account read-back unavailable"), 0)
		case "uncertain":
			return accountCreateUncertain(call, "account read-back cannot uniquely confirm the earlier create")
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return accountCreateUncertain(call, "an earlier account create may have reached the send cap")
	}
	dispatch := &accountCreateDispatch{client: operation.client, call: call, body: encoded}
	if err := fence.Dispatch(dispatch.execute); err != nil {
		return sdkgo.NewMutationRetry[AccountMetadata](queryFailure("createAccount", sdkgo.FailureAvailability, "account checkpoint unavailable"), 0)
	}
	if errors.Is(dispatch.err, errCredential) {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[AccountMetadata](queryFailure("createAccount", sdkgo.FailureAvailability, "account checkpoint unavailable"), 0)
		}
		return accountMutationBranch("createAccount", CreateAccountBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable")
	}
	if errors.Is(dispatch.err, errRequest) {
		return sdkgo.NewMutationRetry[AccountMetadata](queryFailure("createAccount", sdkgo.FailureTransport, "account create outcome is unknown"), 0)
	}
	if dispatch.status == http.StatusTooManyRequests || dispatch.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[AccountMetadata](queryFailure("createAccount", sdkgo.FailureAvailability, "account checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if dispatch.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[AccountMetadata](queryFailure("createAccount", kind, "ActiveCampaign temporarily rejected account creation"), dispatch.retryAfter)
	}
	if dispatch.status == http.StatusRequestTimeout || dispatch.status >= 500 {
		return sdkgo.NewMutationRetry[AccountMetadata](queryFailure("createAccount", sdkgo.FailureAvailability, "account create outcome is unknown"), dispatch.retryAfter)
	}
	if dispatch.status != http.StatusCreated && dispatch.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return accountCreateUncertain(call, "earlier account create may have succeeded")
		}
		return accountMutationBranch("createAccount", CreateAccountBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(dispatch.status))
	}
	if dispatch.err != nil {
		return accountCreateUncertain(call, "account create response was interrupted")
	}
	account, err := decodeAccount(dispatch.response)
	if err != nil {
		return accountCreateUncertain(call, "account create response was invalid")
	}
	if account.Name != input.Name || account.AccountURL != input.AccountURL {
		return accountCreateUncertain(call, "account create response does not match the request")
	}
	_ = fence.RecordProviderID(account.ID)
	return sdkgo.NewMutationBranch(CreateAccountBranchCreated, account, nil, dealReceipt(call, account.ID))
}

func (operation CreateAccountOperation) readBack(call sdkgo.Call, input CreateAccountInput, providerID string) (AccountMetadata, string) {
	path := "/accounts/" + providerID
	if providerID == "" {
		path = "/accounts?" + url.Values{"search": {input.Name}, "limit": {"100"}}.Encode()
	}
	body, status, _, err := operation.client.get(call, path)
	if err != nil || status == http.StatusTooManyRequests || status >= 500 {
		return AccountMetadata{}, "retry"
	}
	if status == http.StatusNotFound {
		return AccountMetadata{}, "absent"
	}
	if status != http.StatusOK {
		return AccountMetadata{}, "uncertain"
	}
	if providerID != "" {
		account, err := decodeAccount(body)
		if err != nil || account.Name != input.Name || account.AccountURL != input.AccountURL {
			return AccountMetadata{}, "uncertain"
		}
		return account, "found"
	}
	var wire struct {
		Accounts []accountWire `json:"accounts"`
		Meta     struct {
			Total string `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Accounts == nil {
		return AccountMetadata{}, "uncertain"
	}
	total, err := strconv.Atoi(wire.Meta.Total)
	if err != nil || total > len(wire.Accounts) {
		return AccountMetadata{}, "uncertain"
	}
	var match AccountMetadata
	for _, item := range wire.Accounts {
		account, err := accountMetadata(item)
		if err != nil {
			return AccountMetadata{}, "uncertain"
		}
		if account.Name == input.Name && account.AccountURL == input.AccountURL {
			if match.ID != "" {
				return AccountMetadata{}, "uncertain"
			}
			match = account
		}
	}
	if match.ID != "" {
		return match, "found"
	}
	return AccountMetadata{}, "absent"
}

// Invoke replaces selected account fields by ID.
func (operation UpdateAccountOperation) Invoke(call sdkgo.Call, input UpdateAccountInput) sdkgo.MutationAttempt[AccountMetadata] {
	if !numericID.MatchString(input.AccountID) || input.Name == nil && input.AccountURL == nil || input.Name != nil && !validAccountName(*input.Name) || input.AccountURL != nil && !validAccountURL(*input.AccountURL) {
		return accountMutationBranch("updateAccount", UpdateAccountBranchDefect, sdkgo.FailureValidation, "account ID or replacement field is invalid")
	}
	fields := map[string]string{}
	if input.Name != nil {
		fields["name"] = *input.Name
	}
	if input.AccountURL != nil {
		fields["accountUrl"] = *input.AccountURL
	}
	encoded, err := json.Marshal(map[string]any{"account": fields})
	if err != nil {
		return accountMutationBranch("updateAccount", UpdateAccountBranchDefect, sdkgo.FailureLocalDefect, "account update could not be encoded")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodPut, "/accounts/"+input.AccountID, encoded)
	if errors.Is(err, errCredential) {
		return accountMutationBranch("updateAccount", UpdateAccountBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[AccountMetadata](queryFailure("updateAccount", sdkgo.FailureTransport, "request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[AccountMetadata](queryFailure("updateAccount", sdkgo.FailureRateLimit, "rate limited"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[AccountMetadata](queryFailure("updateAccount", sdkgo.FailureAvailability, "provider unavailable"), retryAfter)
	}
	if status == http.StatusNotFound {
		return accountMutationBranch("updateAccount", UpdateAccountBranchNotFound, sdkgo.FailureNotFound, "account does not exist")
	}
	if status != http.StatusOK {
		return accountMutationBranch("updateAccount", UpdateAccountBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if err != nil {
		return sdkgo.NewMutationRetry[AccountMetadata](queryFailure("updateAccount", sdkgo.FailureTransport, "response interrupted"), 0)
	}
	account, err := decodeAccount(body)
	if err != nil || account.ID != input.AccountID {
		return accountMutationBranch("updateAccount", UpdateAccountBranchInvalidResponse, sdkgo.FailureProtocol, "account response is invalid")
	}
	return sdkgo.NewMutationBranch(UpdateAccountBranchUpdated, account, nil, dealReceipt(call, account.ID))
}

// Invoke deletes an account; an already-absent account converges on deletion.
func (operation DeleteAccountOperation) Invoke(call sdkgo.Call, input DeleteAccountInput) sdkgo.MutationAttempt[AccountDeletion] {
	if !numericID.MatchString(input.AccountID) {
		return accountDeletionBranch(DeleteAccountBranchDefect, sdkgo.FailureValidation, "account ID must be numeric")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodDelete, "/accounts/"+input.AccountID, nil)
	if errors.Is(err, errCredential) {
		return accountDeletionBranch(DeleteAccountBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[AccountDeletion](queryFailure("deleteAccount", sdkgo.FailureTransport, "request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[AccountDeletion](queryFailure("deleteAccount", sdkgo.FailureRateLimit, "rate limited"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[AccountDeletion](queryFailure("deleteAccount", sdkgo.FailureAvailability, "provider unavailable"), retryAfter)
	}
	if status != http.StatusOK && status != http.StatusNoContent && status != http.StatusNotFound {
		return accountDeletionBranch(DeleteAccountBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return accountDeletionBranch(DeleteAccountBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "response too large")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[AccountDeletion](queryFailure("deleteAccount", sdkgo.FailureTransport, "response interrupted"), 0)
	}
	if status == http.StatusOK && len(body) > 0 && !json.Valid(body) {
		return accountDeletionBranch(DeleteAccountBranchInvalidResponse, sdkgo.FailureProtocol, "invalid response JSON")
	}
	return sdkgo.NewMutationBranch(DeleteAccountBranchDeleted, AccountDeletion{AccountID: input.AccountID, Deleted: true}, nil, dealReceipt(call, input.AccountID))
}

func (dispatch *accountCreateDispatch) execute(ctx context.Context) error {
	dispatch.response, dispatch.status, dispatch.retryAfter, dispatch.err = dispatch.client.requestWithContext(ctx, dispatch.call, http.MethodPost, "/accounts", dispatch.body)
	return nil
}

func validAccountName(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 250 && !strings.ContainsAny(name, "\r\n")
}
func validAccountURL(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}
func accountCreateUncertain(call sdkgo.Call, message string) sdkgo.MutationAttempt[AccountMetadata] {
	return sdkgo.NewMutationUncertain(AccountMetadata{}, queryFailure("createAccount", sdkgo.FailureTransport, message), dealReceipt(call, ""))
}
func accountMutationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AccountMetadata] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, AccountMetadata{}, &failure, sdkgo.Receipt{})
}
func accountDeletionBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AccountDeletion] {
	failure := queryFailure("deleteAccount", kind, message)
	return sdkgo.NewMutationBranch(branch, AccountDeletion{}, &failure, sdkgo.Receipt{})
}
