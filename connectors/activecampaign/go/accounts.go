// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListAccountsInput selects one page of CRM accounts.
type ListAccountsInput struct {
	// Offset is the zero-based number of accounts to skip.
	Offset int `json:"offset,omitempty"`
	// Limit is the maximum account count; zero uses 20 and 100 is the maximum.
	Limit int `json:"limit,omitempty"`
}

// GetAccountInput selects one account by numeric provider ID.
type GetAccountInput struct {
	// AccountID is the numeric CRM account ID.
	AccountID string `json:"accountId"`
}

// AccountMetadata contains the public CRM account identity and website.
type AccountMetadata struct {
	// ID is the numeric provider account ID.
	ID string `json:"id"`
	// Name is the account's display name.
	Name string `json:"name"`
	// AccountURL is the optional company website URL.
	AccountURL string `json:"accountUrl"`
}

// AccountsPage contains one offset-based page of CRM accounts.
type AccountsPage struct {
	// Accounts contains the selected account metadata.
	Accounts []AccountMetadata `json:"accounts"`
	// Offset is the requested zero-based offset.
	Offset int `json:"offset"`
	// Limit is the requested maximum count.
	Limit int `json:"limit"`
	// Total is the provider's matching account count.
	Total int `json:"total"`
}

// ListAccountsOperation reads one page of CRM accounts.
type ListAccountsOperation struct{ client *Client }

// GetAccountOperation reads one CRM account.
type GetAccountOperation struct{ client *Client }

// ListAccounts binds the account listing Query.
func (client *Client) ListAccounts() ListAccountsOperation { return ListAccountsOperation{client} }

// GetAccount binds the account lookup Query.
func (client *Client) GetAccount() GetAccountOperation { return GetAccountOperation{client} }

// Definition returns the manifest-generated account listing contract.
func (ListAccountsOperation) Definition() sdkgo.QueryDefinition { return ListAccountsDefinition }

// Definition returns the manifest-generated account lookup contract.
func (GetAccountOperation) Definition() sdkgo.QueryDefinition { return GetAccountDefinition }

// Invoke reads one page of CRM accounts.
func (operation ListAccountsOperation) Invoke(call sdkgo.Call, input ListAccountsInput) sdkgo.QueryAttempt[AccountsPage] {
	branches := queryBranches{found: ListAccountsBranchListed, rejected: ListAccountsBranchProviderRejected, invalid: ListAccountsBranchInvalidResponse, defect: ListAccountsBranchDefect}
	if input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return queryBranch("listAccounts", branches.defect, AccountsPage{}, sdkgo.FailureValidation, "offset or limit is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	query := url.Values{"limit": {strconv.Itoa(input.Limit)}, "offset": {strconv.Itoa(input.Offset)}}
	return invokeQuery(call, operation.client, "listAccounts", "/accounts?"+query.Encode(), branches, func(body []byte) (AccountsPage, error) {
		var wire struct {
			Accounts []accountWire `json:"accounts"`
			Meta     struct {
				Total string `json:"total"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(body, &wire); err != nil {
			return AccountsPage{}, err
		}
		total, err := strconv.Atoi(wire.Meta.Total)
		if wire.Accounts == nil || err != nil || total < 0 {
			return AccountsPage{}, fmt.Errorf("account collection or count missing")
		}
		page := AccountsPage{Accounts: make([]AccountMetadata, 0, len(wire.Accounts)), Offset: input.Offset, Limit: input.Limit, Total: total}
		for _, item := range wire.Accounts {
			account, err := accountMetadata(item)
			if err != nil {
				return AccountsPage{}, err
			}
			page.Accounts = append(page.Accounts, account)
		}
		return page, nil
	})
}

// Invoke reads one CRM account by ID.
func (operation GetAccountOperation) Invoke(call sdkgo.Call, input GetAccountInput) sdkgo.QueryAttempt[AccountMetadata] {
	branches := queryBranches{found: GetAccountBranchFound, notFound: GetAccountBranchNotFound, rejected: GetAccountBranchProviderRejected, invalid: GetAccountBranchInvalidResponse, defect: GetAccountBranchDefect}
	if !numericID.MatchString(input.AccountID) {
		return queryBranch("getAccount", branches.defect, AccountMetadata{}, sdkgo.FailureValidation, "account ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getAccount", "/accounts/"+input.AccountID, branches, decodeAccount)
}

type accountWire struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AccountURL string `json:"accountUrl"`
}

func decodeAccount(body []byte) (AccountMetadata, error) {
	var wire struct {
		Account *accountWire `json:"account"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return AccountMetadata{}, err
	}
	if wire.Account == nil {
		return AccountMetadata{}, fmt.Errorf("account missing")
	}
	return accountMetadata(*wire.Account)
}

func accountMetadata(wire accountWire) (AccountMetadata, error) {
	if !numericID.MatchString(wire.ID) || wire.Name == "" {
		return AccountMetadata{}, fmt.Errorf("account identity invalid")
	}
	return AccountMetadata{ID: wire.ID, Name: wire.Name, AccountURL: wire.AccountURL}, nil
}
