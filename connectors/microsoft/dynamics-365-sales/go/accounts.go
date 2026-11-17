// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dynamics365sales

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Account is the stable ID and name of a Dynamics 365 Sales account.
type Account struct {
	// AccountID is the Dataverse account UUID.
	AccountID string `json:"accountId"`
	// Name is the account display name.
	Name string `json:"name"`
}

// Accounts contains one bounded account result set.
type Accounts struct {
	// Accounts are the returned account identities.
	Accounts []Account `json:"accounts"`
}

// ListAccountsInput limits the number of accounts read.
type ListAccountsInput struct {
	// Top is a result count from 1 to 100.
	Top int `json:"top"`
}

// GetAccountInput identifies an account by UUID.
type GetAccountInput struct {
	// AccountID is the Dataverse account UUID.
	AccountID string `json:"accountId"`
}

// CreateAccountInput supplies the name for a new account.
type CreateAccountInput struct {
	// Name is the nonblank account display name.
	Name string `json:"name"`
}

// UpdateAccountInput replaces one account's display name.
type UpdateAccountInput struct {
	// AccountID is the Dataverse account UUID.
	AccountID string `json:"accountId"`
	// Name is the replacement display name.
	Name string `json:"name"`
}

// DeleteAccountInput identifies one account to delete.
type DeleteAccountInput struct {
	// AccountID is the Dataverse account UUID.
	AccountID string `json:"accountId"`
}

// AccountDeletion reports that the account is absent.
type AccountDeletion struct {
	// Deleted is true after deletion or an already missing account.
	Deleted bool `json:"deleted"`
}

// ListAccountsOperation reads a bounded account set.
type ListAccountsOperation struct{ client *Client }

// GetAccountOperation reads one account.
type GetAccountOperation struct{ client *Client }

// CreateAccountOperation creates one account with a stable ID.
type CreateAccountOperation struct{ client *Client }

// UpdateAccountOperation replaces one account name.
type UpdateAccountOperation struct{ client *Client }

// DeleteAccountOperation deletes one account.
type DeleteAccountOperation struct{ client *Client }

// ListAccounts binds the Query to this client.
func (client *Client) ListAccounts() ListAccountsOperation { return ListAccountsOperation{client} }

// GetAccount binds the Query to this client.
func (client *Client) GetAccount() GetAccountOperation { return GetAccountOperation{client} }

// CreateAccount binds the Mutation to this client.
func (client *Client) CreateAccount() CreateAccountOperation { return CreateAccountOperation{client} }

// UpdateAccount binds the Mutation to this client.
func (client *Client) UpdateAccount() UpdateAccountOperation { return UpdateAccountOperation{client} }

// DeleteAccount binds the Mutation to this client.
func (client *Client) DeleteAccount() DeleteAccountOperation { return DeleteAccountOperation{client} }

// Definition returns the generated account list Query.
func (ListAccountsOperation) Definition() sdkgo.QueryDefinition { return ListAccountsDefinition }

// Definition returns the generated account lookup Query.
func (GetAccountOperation) Definition() sdkgo.QueryDefinition { return GetAccountDefinition }

// Definition returns the generated account creation Mutation.
func (CreateAccountOperation) Definition() sdkgo.MutationDefinition { return CreateAccountDefinition }

// Definition returns the generated account update Mutation.
func (UpdateAccountOperation) Definition() sdkgo.MutationDefinition { return UpdateAccountDefinition }

// Definition returns the generated account delete Mutation.
func (DeleteAccountOperation) Definition() sdkgo.MutationDefinition { return DeleteAccountDefinition }

// IdempotencyKey identifies the stable account ID derived from the call.
func (CreateAccountOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateAccountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies an absolute name replacement.
func (UpdateAccountOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateAccountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies a repeat-safe deletion.
func (DeleteAccountOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteAccountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists at most the requested number of accounts.
func (operation ListAccountsOperation) Invoke(call sdkgo.Call, input ListAccountsInput) sdkgo.QueryAttempt[Accounts] {
	if input.Top < 1 || input.Top > 100 {
		return accountQueryBranch(ListAccountsBranchDefect, Accounts{}, "listAccounts", sdkgo.FailureValidation, "account result limit is invalid")
	}
	query := url.Values{"$select": {"accountid,name"}, "$orderby": {"accountid asc"}, "$top": {strconv.Itoa(input.Top)}}
	response := operation.client.requestDataverse(call, http.MethodGet, "/api/data/v9.2/accounts?"+query.Encode(), nil, "", nil)
	if kind, retry := accountRetryKind(response); retry {
		return sdkgo.NewQueryRetry[Accounts](accountFailure("listAccounts", kind, "Microsoft account list is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return accountQueryBranch(ListAccountsBranchDefect, Accounts{}, "listAccounts", sdkgo.FailureAuthentication, "client credentials are unavailable")
	}
	if response.status != http.StatusOK {
		return accountQueryBranch(ListAccountsBranchProviderRejected, Accounts{}, "listAccounts", sdkgo.FailureProviderRejection, "Microsoft rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return accountQueryBranch(ListAccountsBranchInvalidResponse, Accounts{}, "listAccounts", sdkgo.FailureResponseTooLarge, "Microsoft account list was unreadable")
	}
	var wire struct {
		Value []accountWire `json:"value"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Value == nil || len(wire.Value) > input.Top {
		return accountQueryBranch(ListAccountsBranchInvalidResponse, Accounts{}, "listAccounts", sdkgo.FailureProtocol, "Dataverse returned invalid accounts JSON")
	}
	accounts := make([]Account, 0, len(wire.Value))
	for _, item := range wire.Value {
		account := Account{AccountID: item.AccountID, Name: item.Name}
		if !validAccount(account) {
			return accountQueryBranch(ListAccountsBranchInvalidResponse, Accounts{}, "listAccounts", sdkgo.FailureProtocol, "Dataverse returned invalid account identity")
		}
		accounts = append(accounts, account)
	}
	return sdkgo.NewQueryBranch(ListAccountsBranchListed, Accounts{Accounts: accounts}, nil, sdkgo.Receipt{})
}

// Invoke reads one account by UUID.
func (operation GetAccountOperation) Invoke(call sdkgo.Call, input GetAccountInput) sdkgo.QueryAttempt[Account] {
	if !identifierPattern.MatchString(input.AccountID) {
		return accountQueryBranch(GetAccountBranchDefect, Account{}, "getAccount", sdkgo.FailureValidation, "account ID is invalid")
	}
	response := operation.client.requestDataverse(call, http.MethodGet, accountPath(input.AccountID)+"?$select=accountid,name", nil, "", nil)
	if kind, retry := accountRetryKind(response); retry {
		return sdkgo.NewQueryRetry[Account](accountFailure("getAccount", kind, "Microsoft account lookup is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return accountQueryBranch(GetAccountBranchDefect, Account{}, "getAccount", sdkgo.FailureAuthentication, "client credentials are unavailable")
	}
	if response.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetAccountBranchMissing, Account{}, nil, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return accountQueryBranch(GetAccountBranchProviderRejected, Account{}, "getAccount", sdkgo.FailureProviderRejection, "Microsoft rejected HTTP "+strconv.Itoa(response.status))
	}
	account, decodeErr := decodeAccount(response.body)
	if response.err != nil || decodeErr != nil || !strings.EqualFold(account.AccountID, input.AccountID) {
		return accountQueryBranch(GetAccountBranchInvalidResponse, Account{}, "getAccount", sdkgo.FailureProtocol, "Dataverse returned invalid account JSON")
	}
	return sdkgo.NewQueryBranch(GetAccountBranchFound, account, nil, sdkgo.Receipt{})
}

// Invoke creates one account at a deterministic UUID using conditional PATCH.
func (operation CreateAccountOperation) Invoke(call sdkgo.Call, input CreateAccountInput) sdkgo.MutationAttempt[Account] {
	if !validAccountName(input.Name) {
		return accountMutationBranch(CreateAccountBranchDefect, Account{}, "createAccount", sdkgo.FailureValidation, "account name is invalid")
	}
	account := Account{AccountID: accountIDForCall(call.ID), Name: strings.TrimSpace(input.Name)}
	body, err := json.Marshal(map[string]string{"name": account.Name})
	if err != nil {
		return accountMutationBranch(CreateAccountBranchDefect, Account{}, "createAccount", sdkgo.FailureValidation, "account name cannot be encoded")
	}
	headers := http.Header{"If-None-Match": {"*"}}
	response := operation.client.requestDataverse(call, http.MethodPatch, accountPath(account.AccountID), body, "application/json", headers)
	if kind, retry := accountRetryKind(response); retry {
		return sdkgo.NewMutationRetry[Account](accountFailure("createAccount", kind, "Microsoft account create is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return accountMutationBranch(CreateAccountBranchDefect, Account{}, "createAccount", sdkgo.FailureAuthentication, "client credentials are unavailable")
	}
	if errors.Is(response.err, errInvalidToken) || errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return accountMutationBranch(CreateAccountBranchInvalidResponse, Account{}, "createAccount", sdkgo.FailureProtocol, "Microsoft token or account response was invalid")
	}
	if response.status == http.StatusPreconditionFailed {
		readBack := operation.client.requestDataverse(call, http.MethodGet, accountPath(account.AccountID)+"?$select=accountid,name", nil, "", nil)
		if kind, retry := accountRetryKind(readBack); retry {
			return sdkgo.NewMutationRetry[Account](accountFailure("createAccount", kind, "Microsoft account confirmation is temporarily unavailable"), readBack.retryAfter)
		}
		existing, decodeErr := decodeAccount(readBack.body)
		if readBack.status == http.StatusOK && readBack.err == nil && decodeErr == nil && strings.EqualFold(existing.AccountID, account.AccountID) && existing.Name == account.Name {
			return sdkgo.NewMutationBranch(CreateAccountBranchCreated, account, nil, sdkgo.Receipt{})
		}
		return accountMutationBranch(CreateAccountBranchProviderRejected, Account{}, "createAccount", sdkgo.FailureConflict, "Dataverse account ID already exists")
	}
	if response.status != http.StatusNoContent && response.status != http.StatusCreated {
		return accountMutationBranch(CreateAccountBranchProviderRejected, Account{}, "createAccount", sdkgo.FailureProviderRejection, "Microsoft rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return accountMutationBranch(CreateAccountBranchInvalidResponse, Account{}, "createAccount", sdkgo.FailureProtocol, "Microsoft account response was unreadable")
	}
	return sdkgo.NewMutationBranch(CreateAccountBranchCreated, account, nil, sdkgo.Receipt{})
}

// Invoke replaces one account name without creating a missing account.
func (operation UpdateAccountOperation) Invoke(call sdkgo.Call, input UpdateAccountInput) sdkgo.MutationAttempt[Account] {
	if !identifierPattern.MatchString(input.AccountID) || !validAccountName(input.Name) {
		return accountMutationBranch(UpdateAccountBranchDefect, Account{}, "updateAccount", sdkgo.FailureValidation, "account ID or name is invalid")
	}
	account := Account{AccountID: input.AccountID, Name: strings.TrimSpace(input.Name)}
	body, err := json.Marshal(map[string]string{"name": account.Name})
	if err != nil {
		return accountMutationBranch(UpdateAccountBranchDefect, Account{}, "updateAccount", sdkgo.FailureValidation, "account name cannot be encoded")
	}
	response := operation.client.requestDataverse(call, http.MethodPatch, accountPath(input.AccountID), body, "application/json", http.Header{"If-Match": {"*"}})
	if kind, retry := accountRetryKind(response); retry {
		return sdkgo.NewMutationRetry[Account](accountFailure("updateAccount", kind, "Microsoft account update is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return accountMutationBranch(UpdateAccountBranchDefect, Account{}, "updateAccount", sdkgo.FailureAuthentication, "client credentials are unavailable")
	}
	if response.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(UpdateAccountBranchMissing, Account{}, nil, sdkgo.Receipt{})
	}
	if response.status != http.StatusNoContent {
		return accountMutationBranch(UpdateAccountBranchProviderRejected, Account{}, "updateAccount", sdkgo.FailureProviderRejection, "Microsoft rejected HTTP "+strconv.Itoa(response.status))
	}
	return sdkgo.NewMutationBranch(UpdateAccountBranchUpdated, account, nil, sdkgo.Receipt{})
}

// Invoke removes one account and accepts an already missing UUID.
func (operation DeleteAccountOperation) Invoke(call sdkgo.Call, input DeleteAccountInput) sdkgo.MutationAttempt[AccountDeletion] {
	if !identifierPattern.MatchString(input.AccountID) {
		return accountMutationBranch(DeleteAccountBranchDefect, AccountDeletion{}, "deleteAccount", sdkgo.FailureValidation, "account ID is invalid")
	}
	response := operation.client.requestDataverse(call, http.MethodDelete, accountPath(input.AccountID), nil, "", nil)
	if kind, retry := accountRetryKind(response); retry {
		return sdkgo.NewMutationRetry[AccountDeletion](accountFailure("deleteAccount", kind, "Microsoft account deletion is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		return accountMutationBranch(DeleteAccountBranchDefect, AccountDeletion{}, "deleteAccount", sdkgo.FailureAuthentication, "client credentials are unavailable")
	}
	if response.status == http.StatusNoContent || response.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteAccountBranchDeleted, AccountDeletion{Deleted: true}, nil, sdkgo.Receipt{})
	}
	return accountMutationBranch(DeleteAccountBranchProviderRejected, AccountDeletion{}, "deleteAccount", sdkgo.FailureProviderRejection, "Microsoft rejected HTTP "+strconv.Itoa(response.status))
}

func accountPath(accountID string) string { return "/api/data/v9.2/accounts(" + accountID + ")" }

type accountWire struct {
	AccountID string `json:"accountid"`
	Name      string `json:"name"`
}

func decodeAccount(body []byte) (Account, error) {
	var wire accountWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return Account{}, err
	}
	account := Account{AccountID: wire.AccountID, Name: wire.Name}
	if !validAccount(account) {
		return Account{}, fmt.Errorf("invalid account identity")
	}
	return account, nil
}
func validAccountName(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 160 && !strings.ContainsAny(name, "\x00\r\n")
}
func validAccount(account Account) bool {
	return identifierPattern.MatchString(account.AccountID) && validAccountName(account.Name)
}

func accountIDForCall(callID sdkgo.CallID) string {
	digest := sha1.Sum([]byte("microsoft-dynamics-365-sales/account/" + string(callID)))
	digest[6] = (digest[6] & 0x0f) | 0x50
	digest[8] = (digest[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", digest[:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16])
}

func accountRetryKind(response providerResponse) (sdkgo.FailureKind, bool) {
	if errors.Is(response.err, errRequest) {
		return sdkgo.FailureTransport, true
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.FailureRateLimit, true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.FailureAvailability, true
	}
	if response.err != nil && !errors.Is(response.err, errCredential) && !errors.Is(response.err, errInvalidToken) && !errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.FailureTransport, true
	}
	return "", false
}

func accountFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "microsoft-dynamics-365-sales", Operation: operation, Message: message}
}

func accountQueryBranch[T any](branch sdkgo.BranchID, value T, operation string, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := accountFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func accountMutationBranch[T any](branch sdkgo.BranchID, value T, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := accountFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
