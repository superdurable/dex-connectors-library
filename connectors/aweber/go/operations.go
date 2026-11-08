// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package aweber

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListAccountsInput selects an offset-based page of authorized accounts.
type ListAccountsInput struct {
	// Offset is the zero-based number of accounts to skip.
	Offset int `json:"offset"`
	// Limit is the maximum account count; zero uses the provider default of 100.
	Limit int `json:"limit,omitempty"`
}

// Account contains the stable account ID and display company.
type Account struct {
	// ID is the AWeber account's numeric identifier.
	ID string `json:"id"`
	// Company is the customer account's display name.
	Company string `json:"company"`
}

// AccountsPage is one offset-based account page.
type AccountsPage struct {
	// Accounts contains the visible customer accounts.
	Accounts []Account `json:"accounts"`
	// Offset is the zero-based offset used for this request.
	Offset int `json:"offset"`
	// Limit is the maximum account count used for this request.
	Limit int `json:"limit"`
	// Total is the provider's full account count.
	Total int `json:"total"`
}

// ListAccountsOperation retrieves one page of authorized account identities.
type ListAccountsOperation struct{ client *Client }

// ListAccounts binds the account Query to this client.
func (client *Client) ListAccounts() ListAccountsOperation { return ListAccountsOperation{client} }

// Definition returns the manifest-generated account contract.
func (ListAccountsOperation) Definition() sdkgo.QueryDefinition { return ListAccountsDefinition }

// Invoke reads account IDs and company names from AWeber.
func (operation ListAccountsOperation) Invoke(call sdkgo.Call, input ListAccountsInput) sdkgo.QueryAttempt[AccountsPage] {
	if input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return accountBranch(ListAccountsBranchDefect, sdkgo.FailureValidation, "offset or limit is invalid")
	}
	if input.Limit == 0 {
		input.Limit = 100
	}
	query := url.Values{"ws.start": {strconv.Itoa(input.Offset)}, "ws.size": {strconv.Itoa(input.Limit)}}
	response := operation.client.get(call, "/1.0/accounts?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return accountBranch(ListAccountsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return accountRetry(sdkgo.FailureTransport, "AWeber request failed", 0)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return accountBranch(ListAccountsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "AWeber response exceeded the configured limit")
	}
	if response.err != nil {
		return accountRetry(sdkgo.FailureTransport, "AWeber response was interrupted", 0)
	}
	if response.status == http.StatusForbidden && isRateLimit(response.body) {
		return accountRetry(sdkgo.FailureRateLimit, "AWeber rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusTooManyRequests {
		return accountRetry(sdkgo.FailureRateLimit, "AWeber rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return accountRetry(sdkgo.FailureAvailability, "AWeber is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return accountBranch(ListAccountsBranchProviderRejected, sdkgo.FailureProviderRejection, "AWeber rejected HTTP "+strconv.Itoa(response.status))
	}
	page, err := decodeAccountsPage(response.body, input)
	if err != nil {
		return accountBranch(ListAccountsBranchInvalidResponse, sdkgo.FailureProtocol, "AWeber returned invalid account JSON")
	}
	return sdkgo.NewQueryBranch(ListAccountsBranchListed, page, nil, sdkgo.Receipt{})
}

func isRateLimit(body []byte) bool {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	return json.Unmarshal(body, &payload) == nil && payload.Error.Type == "ForbiddenError" && strings.EqualFold(payload.Error.Message, "Rate Limit Error")
}

func decodeAccountsPage(body []byte, input ListAccountsInput) (AccountsPage, error) {
	var payload struct {
		Entries []struct {
			ID      int64   `json:"id"`
			Company *string `json:"company"`
		} `json:"entries"`
		Start     int `json:"start"`
		TotalSize int `json:"total_size"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return AccountsPage{}, err
	}
	if payload.Entries == nil || payload.Start != input.Offset || payload.TotalSize < 0 || payload.TotalSize < len(payload.Entries) {
		return AccountsPage{}, fmt.Errorf("AWeber account page is incomplete")
	}
	page := AccountsPage{Accounts: make([]Account, 0, len(payload.Entries)), Offset: input.Offset, Limit: input.Limit, Total: payload.TotalSize}
	for _, entry := range payload.Entries {
		if entry.ID <= 0 || entry.ID > math.MaxInt32 || entry.Company == nil {
			return AccountsPage{}, fmt.Errorf("AWeber account entry is invalid")
		}
		page.Accounts = append(page.Accounts, Account{ID: strconv.FormatInt(entry.ID, 10), Company: *entry.Company})
	}
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
	return sdkgo.Failure{Kind: kind, Provider: "aweber", Operation: "listAccounts", Message: message}
}
