// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetAccountInput identifies one Creatio account by UUID.
type GetAccountInput struct {
	// AccountID is the Account.Id UUID.
	AccountID string `json:"accountId"`
}

// GetAccountOperation reads one account through OData 4.
type GetAccountOperation struct{ client *Client }

// GetAccount binds the Query to this client.
func (client *Client) GetAccount() GetAccountOperation { return GetAccountOperation{client} }

// Definition returns the generated Query contract.
func (GetAccountOperation) Definition() sdkgo.QueryDefinition { return GetAccountDefinition }

// Invoke reads an account's stable ID and name.
func (operation GetAccountOperation) Invoke(call sdkgo.Call, input GetAccountInput) sdkgo.QueryAttempt[AccountSummary] {
	if !contactUUIDPattern.MatchString(input.AccountID) {
		return accountQueryBranch(GetAccountBranchDefect, sdkgo.FailureValidation, "invalid account UUID")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
			return accountQueryBranch(GetAccountBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return sdkgo.NewQueryRetry[AccountSummary](accountsFailure("getAccount", sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
	}
	response := operation.client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Account("+input.AccountID+")?$select=Id%2CName", nil)
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[AccountSummary](accountsFailure("getAccount", sdkgo.FailureRateLimit, "Creatio rate limited account lookup"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[AccountSummary](accountsFailure("getAccount", sdkgo.FailureAvailability, "Creatio account lookup unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return accountQueryBranch(GetAccountBranchNotFound, sdkgo.FailureNotFound, "Creatio account was not found")
	}
	if response.isBodyTooLarge {
		return accountQueryBranch(GetAccountBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Creatio account response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[AccountSummary](accountsFailure("getAccount", sdkgo.FailureTransport, "Creatio account response failed"), 0)
	}
	if response.status != http.StatusOK {
		return accountQueryBranch(GetAccountBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.status))
	}
	account, err := decodeAccountRecord(response.body)
	if err != nil || account.ID != input.AccountID {
		return accountQueryBranch(GetAccountBranchInvalidResponse, sdkgo.FailureProtocol, "Creatio returned invalid account JSON")
	}
	return sdkgo.NewQueryBranch(GetAccountBranchFound, account, nil, sdkgo.Receipt{})
}

func accountQueryBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[AccountSummary] {
	failure := accountsFailure("getAccount", kind, message)
	return sdkgo.NewQueryBranch(branch, AccountSummary{}, &failure, sdkgo.Receipt{})
}
