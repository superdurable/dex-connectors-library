// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package basecamp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListAccountsInput selects a locally bounded page of authorized accounts.
type ListAccountsInput struct {
	// Offset is the zero-based number of accounts to skip.
	Offset int `json:"offset"`
	// Limit is the maximum account count; zero uses 100.
	Limit int `json:"limit,omitempty"`
}

// Account contains the stable account ID and display company.
type Account struct {
	// ID is the Basecamp account's numeric identifier.
	ID string `json:"id"`
	// Company is the Basecamp account's display name.
	Company string `json:"company"`
}

// AccountsPage is one offset-based account page.
type AccountsPage struct {
	// Accounts contains the visible Basecamp accounts.
	Accounts []Account `json:"accounts"`
	// Offset is the zero-based offset used for this request.
	Offset int `json:"offset"`
	// Limit is the maximum account count used for this request.
	Limit int `json:"limit"`
	// Total is the accessible Basecamp account count.
	Total int `json:"total"`
}

// ListAccountsOperation retrieves a local page of authorized account identities.
type ListAccountsOperation struct{ client *Client }

// ListAccounts binds the account Query to this client.
func (client *Client) ListAccounts() ListAccountsOperation { return ListAccountsOperation{client} }

// Definition returns the manifest-generated account contract.
func (ListAccountsOperation) Definition() sdkgo.QueryDefinition { return ListAccountsDefinition }

// Invoke reads account IDs and company names from Basecamp.
func (operation ListAccountsOperation) Invoke(call sdkgo.Call, input ListAccountsInput) sdkgo.QueryAttempt[AccountsPage] {
	if input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return accountBranch(ListAccountsBranchDefect, sdkgo.FailureValidation, "offset or limit is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 100
	}
	response := operation.client.get(call, "/authorization.json")
	if errors.Is(response.err, errCredential) {
		return accountBranch(ListAccountsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return accountRetry(sdkgo.FailureTransport, "Basecamp request failed", 0)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return accountBranch(ListAccountsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Basecamp response exceeded the configured limit")
	}
	if response.err != nil {
		return accountRetry(sdkgo.FailureTransport, "Basecamp response was interrupted", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return accountRetry(sdkgo.FailureRateLimit, "Basecamp rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return accountRetry(sdkgo.FailureAvailability, "Basecamp is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return accountBranch(ListAccountsBranchProviderRejected, sdkgo.FailureProviderRejection, "Basecamp rejected HTTP "+strconv.Itoa(response.status))
	}
	page, err := decodeAccountsPage(response.body, input)
	if err != nil {
		return accountBranch(ListAccountsBranchInvalidResponse, sdkgo.FailureProtocol, "Basecamp returned invalid account JSON")
	}
	return sdkgo.NewQueryBranch(ListAccountsBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeAccountsPage(body []byte, input ListAccountsInput) (AccountsPage, error) {
	var payload struct {
		Accounts []*struct {
			Product string `json:"product"`
			ID      int64  `json:"id"`
			Name    string `json:"name"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return AccountsPage{}, err
	}
	if payload.Accounts == nil {
		return AccountsPage{}, fmt.Errorf("Basecamp accounts are missing")
	}
	accounts := make([]Account, 0, len(payload.Accounts))
	seen := make(map[int64]struct{}, len(payload.Accounts))
	for _, entry := range payload.Accounts {
		if entry == nil {
			return AccountsPage{}, fmt.Errorf("Basecamp account entry is invalid")
		}
		if entry.Product != "bc3" {
			continue
		}
		if entry.ID <= 0 || entry.ID > 9007199254740991 || entry.Name == "" {
			return AccountsPage{}, fmt.Errorf("Basecamp account entry is invalid")
		}
		if _, exists := seen[entry.ID]; exists {
			return AccountsPage{}, fmt.Errorf("Basecamp account entry is duplicated")
		}
		seen[entry.ID] = struct{}{}
		accounts = append(accounts, Account{ID: strconv.FormatInt(entry.ID, 10), Company: entry.Name})
	}
	page := AccountsPage{Accounts: make([]Account, 0), Offset: input.Offset, Limit: input.Limit, Total: len(accounts)}
	if input.Offset >= len(accounts) {
		return page, nil
	}
	end := input.Offset + input.Limit
	if end > len(accounts) {
		end = len(accounts)
	}
	page.Accounts = append(page.Accounts, accounts[input.Offset:end]...)
	return page, nil
}

func accountBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[AccountsPage] {
	failure := accountFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, AccountsPage{}, &failure, sdkgo.Receipt{})
}

func accountRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[AccountsPage] {
	return sdkgo.NewQueryRetry[AccountsPage](accountFailure(kind, message), delay)
}

func accountFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "basecamp", Operation: "listAccounts", Message: message}
}
