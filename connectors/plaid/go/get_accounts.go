// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package plaid

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetAccountsInput selects a linked Item by its non-secret Plaid ID.
type GetAccountsInput struct {
	// ItemID is the Item ID returned by Plaid Link token exchange.
	ItemID string `json:"itemId"`
}

// AccountSummary contains only routing fields for one linked account.
type AccountSummary struct {
	// ID is the Plaid account ID.
	ID string `json:"id"`
	// Type is the broad account category.
	Type string `json:"type"`
	// Subtype is the narrower category when supplied by Plaid.
	Subtype string `json:"subtype"`
}

// AccountsSnapshot excludes names, masks, balances, and credentials.
type AccountsSnapshot struct {
	// ItemID identifies the linked Item.
	ItemID string `json:"itemId"`
	// Accounts contains the active account routing summaries.
	Accounts []AccountSummary `json:"accounts"`
}

// GetAccountsOperation reads one Item's account routing fields.
type GetAccountsOperation struct{ client *Client }

// GetAccounts binds the Query to this client.
func (client *Client) GetAccounts() GetAccountsOperation {
	return GetAccountsOperation{client}
}

// Definition returns the manifest-generated account Query.
func (GetAccountsOperation) Definition() sdkgo.QueryDefinition { return GetAccountsDefinition }

// Invoke reads one Item's accounts and classifies retryable and definitive outcomes.
func (operation GetAccountsOperation) Invoke(call sdkgo.Call, input GetAccountsInput) sdkgo.QueryAttempt[AccountsSnapshot] {
	if !validItemID(input.ItemID) {
		return accountsBranch(GetAccountsBranchDefect, sdkgo.FailureValidation, "Item ID must be 1–128 URL-safe characters")
	}
	response := operation.client.getAccounts(call)
	if errors.Is(response.err, errCredential) {
		return accountsBranch(GetAccountsBranchDefect, sdkgo.FailureAuthentication, "Plaid credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[AccountsSnapshot](accountsFailure(sdkgo.FailureTransport, "Plaid request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[AccountsSnapshot](accountsFailure(sdkgo.FailureRateLimit, "Plaid rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[AccountsSnapshot](accountsFailure(sdkgo.FailureAvailability, "Plaid is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return accountsBranch(GetAccountsBranchNotFound, sdkgo.FailureNotFound, "Plaid did not expose the Item")
	}
	if response.status != http.StatusOK {
		return accountsBranch(GetAccountsBranchProviderRejected, sdkgo.FailureProviderRejection, "Plaid rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return accountsBranch(GetAccountsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Plaid response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[AccountsSnapshot](accountsFailure(sdkgo.FailureTransport, "Plaid response was interrupted"), 0)
	}
	accountsStatus, err := decodeAccounts(response.body, input.ItemID)
	if err != nil {
		return accountsBranch(GetAccountsBranchInvalidResponse, sdkgo.FailureProtocol, "Plaid returned invalid accounts JSON")
	}
	return sdkgo.NewQueryBranch(GetAccountsBranchFound, accountsStatus, nil, sdkgo.Receipt{})
}

func validItemID(itemID string) bool {
	if len(itemID) == 0 || len(itemID) > 128 {
		return false
	}
	for _, character := range itemID {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func decodeAccounts(body []byte, requestedItemID string) (AccountsSnapshot, error) {
	var wire struct {
		Item struct {
			ID string `json:"item_id"`
		} `json:"item"`
		Accounts []struct {
			ID      string `json:"account_id"`
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return AccountsSnapshot{}, err
	}
	if wire.Item.ID != requestedItemID || wire.Accounts == nil {
		return AccountsSnapshot{}, fmt.Errorf("Item identity or accounts missing")
	}
	accounts := make([]AccountSummary, 0, len(wire.Accounts))
	for _, account := range wire.Accounts {
		if strings.TrimSpace(account.ID) == "" || strings.TrimSpace(account.Type) == "" {
			return AccountsSnapshot{}, fmt.Errorf("account identity or type missing")
		}
		accounts = append(accounts, AccountSummary{ID: account.ID, Type: account.Type, Subtype: account.Subtype})
	}
	return AccountsSnapshot{ItemID: wire.Item.ID, Accounts: accounts}, nil
}

func accountsFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "plaid", Operation: "getAccounts", Message: message}
}

func accountsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[AccountsSnapshot] {
	failure := accountsFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, AccountsSnapshot{}, &failure, sdkgo.Receipt{})
}
