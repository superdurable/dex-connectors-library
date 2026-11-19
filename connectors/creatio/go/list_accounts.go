// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// AccountSummary contains the stable identity and name of one Creatio account.
type AccountSummary struct {
	// ID is the Account.Id UUID.
	ID string `json:"id"`
	// Name is the account display name.
	Name string `json:"name"`
}

// ListAccountsInput selects one bounded page of Creatio accounts.
type ListAccountsInput struct {
	// PageNumber starts at one; zero selects the first page.
	PageNumber int `json:"pageNumber,omitempty"`
	// PageSize is 1 through 200; zero selects 20 records.
	PageSize int `json:"pageSize,omitempty"`
}

// AccountsPage contains one Creatio OData result page.
type AccountsPage struct {
	// Accounts are the selected public account fields.
	Accounts []AccountSummary `json:"accounts"`
}

// ListAccountsOperation reads one OData page without changing records.
type ListAccountsOperation struct{ client *Client }

// ListAccounts binds the Query to this client.
func (client *Client) ListAccounts() ListAccountsOperation { return ListAccountsOperation{client} }

// Definition returns the generated Query contract.
func (ListAccountsOperation) Definition() sdkgo.QueryDefinition { return ListAccountsDefinition }

// Invoke reads account IDs and names from one OData page.
func (operation ListAccountsOperation) Invoke(call sdkgo.Call, input ListAccountsInput) sdkgo.QueryAttempt[AccountsPage] {
	if input.PageNumber < 0 || input.PageNumber > 5000 || input.PageSize < 0 || input.PageSize > 200 {
		return accountsPageBranch(ListAccountsBranchDefect, sdkgo.FailureValidation, "invalid account page")
	}
	pageNumber, pageSize := input.PageNumber, input.PageSize
	if pageNumber == 0 {
		pageNumber = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
			return accountsPageBranch(ListAccountsBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return sdkgo.NewQueryRetry[AccountsPage](accountsFailure("listAccounts", sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
	}
	query := url.Values{"$select": {"Id,Name"}, "$top": {strconv.Itoa(pageSize)}, "$skip": {strconv.Itoa((pageNumber - 1) * pageSize)}, "$orderby": {"Id"}}
	response := operation.client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Account?"+query.Encode(), nil)
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[AccountsPage](accountsFailure("listAccounts", sdkgo.FailureRateLimit, "Creatio rate limited accounts"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[AccountsPage](accountsFailure("listAccounts", sdkgo.FailureAvailability, "Creatio accounts are unavailable"), response.retryAfter)
	}
	if response.isBodyTooLarge {
		return accountsPageBranch(ListAccountsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Creatio accounts response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[AccountsPage](accountsFailure("listAccounts", sdkgo.FailureTransport, "Creatio accounts response failed"), 0)
	}
	if response.status != http.StatusOK {
		return accountsPageBranch(ListAccountsBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected accounts HTTP "+strconv.Itoa(response.status))
	}
	var envelope struct {
		Value []json.RawMessage `json:"value"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Value == nil {
		return accountsPageBranch(ListAccountsBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned invalid accounts JSON")
	}
	page := AccountsPage{Accounts: make([]AccountSummary, 0, len(envelope.Value))}
	for _, record := range envelope.Value {
		account, err := decodeAccountRecord(record)
		if err != nil {
			return accountsPageBranch(ListAccountsBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned an invalid account")
		}
		page.Accounts = append(page.Accounts, account)
	}
	return sdkgo.NewQueryBranch(ListAccountsBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeAccountRecord(body []byte) (AccountSummary, error) {
	var wire struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return AccountSummary{}, err
	}
	if !contactUUIDPattern.MatchString(wire.ID) || strings.TrimSpace(wire.Name) == "" {
		return AccountSummary{}, errors.New("account identity missing")
	}
	return AccountSummary{ID: wire.ID, Name: wire.Name}, nil
}

func accountsFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "creatio", Operation: operation, Message: message}
}

func accountsPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[AccountsPage] {
	failure := accountsFailure("listAccounts", kind, message)
	return sdkgo.NewQueryBranch(branch, AccountsPage{}, &failure, sdkgo.Receipt{})
}
