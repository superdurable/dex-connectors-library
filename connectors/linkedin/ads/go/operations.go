// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ads

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListAdAccountsInput selects a cursor-based page of LinkedIn ad accounts.
type ListAdAccountsInput struct {
	// PageSize accepts 1 through 1000 accounts; zero uses 100.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the opaque cursor from a previous page; blank starts at the beginning.
	PageToken string `json:"pageToken,omitempty"`
}

// AdAccount contains a stable LinkedIn ad account identifier and name.
type AdAccount struct {
	// ID is the decimal account ID returned by LinkedIn.
	ID string `json:"id"`
	// Name is the account's display name.
	Name string `json:"name"`
}

// AdAccountsPage contains one bounded page and its next cursor.
type AdAccountsPage struct {
	// Accounts are the visible account identities.
	Accounts []AdAccount `json:"accounts"`
	// NextPageToken is blank when LinkedIn reported no further page.
	NextPageToken string `json:"nextPageToken"`
}

// ListAdAccountsOperation reads one page of ad account identities.
type ListAdAccountsOperation struct{ client *Client }

// ListAdAccounts binds the account Query to this client.
func (client *Client) ListAdAccounts() ListAdAccountsOperation {
	return ListAdAccountsOperation{client}
}

// Definition returns the manifest-generated account listing contract.
func (ListAdAccountsOperation) Definition() sdkgo.QueryDefinition { return ListAdAccountsDefinition }

// Invoke reads one bounded cursor-based page of ad accounts.
func (operation ListAdAccountsOperation) Invoke(call sdkgo.Call, input ListAdAccountsInput) sdkgo.QueryAttempt[AdAccountsPage] {
	if input.PageSize < 0 || input.PageSize > 1000 || !isSafePageToken(input.PageToken) {
		return adAccountBranch(ListAdAccountsBranchDefect, sdkgo.FailureValidation, "account page size or cursor is invalid")
	}
	if input.PageSize == 0 {
		input.PageSize = 100
	}
	query := url.Values{"q": {"search"}, "pageSize": {strconv.Itoa(input.PageSize)}}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	response := operation.client.get(call, "/rest/adAccounts?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return adAccountBranch(ListAdAccountsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return adAccountRetry(sdkgo.FailureTransport, "LinkedIn Ads request failed", 0)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return adAccountBranch(ListAdAccountsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "LinkedIn Ads response exceeded the configured limit")
	}
	if response.err != nil {
		return adAccountRetry(sdkgo.FailureTransport, "LinkedIn Ads response was interrupted", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return adAccountRetry(sdkgo.FailureRateLimit, "LinkedIn Ads rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return adAccountRetry(sdkgo.FailureAvailability, "LinkedIn Ads is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return adAccountBranch(ListAdAccountsBranchProviderRejected, sdkgo.FailureProviderRejection, "LinkedIn Ads rejected HTTP "+strconv.Itoa(response.status))
	}
	page, err := decodeAdAccountsPage(response.body, input.PageSize)
	if err != nil {
		return adAccountBranch(ListAdAccountsBranchInvalidResponse, sdkgo.FailureProtocol, "LinkedIn Ads returned invalid account JSON")
	}
	return sdkgo.NewQueryBranch(ListAdAccountsBranchListed, page, nil, sdkgo.Receipt{})
}

func isSafePageToken(token string) bool {
	if len(token) > 1024 {
		return false
	}
	for _, character := range token {
		if character < 33 || character > 126 {
			return false
		}
	}
	return true
}

func decodeAdAccountsPage(body []byte, pageSize int) (AdAccountsPage, error) {
	var payload struct {
		Elements []*struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"elements"`
		Metadata struct {
			NextPageToken string `json:"nextPageToken"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return AdAccountsPage{}, err
	}
	if payload.Elements == nil || len(payload.Elements) > pageSize || !isSafePageToken(payload.Metadata.NextPageToken) {
		return AdAccountsPage{}, fmt.Errorf("LinkedIn ad account page is invalid")
	}
	accounts := make([]AdAccount, 0, len(payload.Elements))
	seen := make(map[int64]struct{}, len(payload.Elements))
	for _, entry := range payload.Elements {
		if entry == nil || entry.ID <= 0 || entry.ID > 9007199254740991 || strings.TrimSpace(entry.Name) == "" {
			return AdAccountsPage{}, fmt.Errorf("LinkedIn ad account is invalid")
		}
		if _, duplicate := seen[entry.ID]; duplicate {
			return AdAccountsPage{}, fmt.Errorf("LinkedIn ad account is duplicated")
		}
		seen[entry.ID] = struct{}{}
		accounts = append(accounts, AdAccount{ID: strconv.FormatInt(entry.ID, 10), Name: entry.Name})
	}
	return AdAccountsPage{Accounts: accounts, NextPageToken: payload.Metadata.NextPageToken}, nil
}

func adAccountBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[AdAccountsPage] {
	failure := adAccountFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, AdAccountsPage{}, &failure, sdkgo.Receipt{})
}

func adAccountRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[AdAccountsPage] {
	return sdkgo.NewQueryRetry[AdAccountsPage](adAccountFailure(kind, message), delay)
}

func adAccountFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "linkedin-ads", Operation: "listAdAccounts", Message: message}
}
