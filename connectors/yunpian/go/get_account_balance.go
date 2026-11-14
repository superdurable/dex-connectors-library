// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package yunpian

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"strconv"
)

// GetAccountBalanceInput has no parameters; the connection identifies the account.
type GetAccountBalanceInput struct{}

// AccountBalance contains only the amount remaining in the Yunpian account.
type AccountBalance struct {
	// Balance is the remaining amount reported by Yunpian, in account currency units.
	Balance float64 `json:"balance"`
}

// GetAccountBalanceOperation reads one account's balance.
type GetAccountBalanceOperation struct{ client *Client }

// GetAccountBalance binds the Query to this client.
func (client *Client) GetAccountBalance() GetAccountBalanceOperation {
	return GetAccountBalanceOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (GetAccountBalanceOperation) Definition() sdkgo.QueryDefinition {
	return GetAccountBalanceDefinition
}

// Invoke queries Yunpian and classifies the provider outcome without retaining contact fields.
func (operation GetAccountBalanceOperation) Invoke(call sdkgo.Call, _ GetAccountBalanceInput) sdkgo.QueryAttempt[AccountBalance] {
	response := operation.client.getBalance(call)
	if errors.Is(response.err, errCredential) {
		return balanceBranch(GetAccountBalanceBranchDefect, sdkgo.FailureAuthentication, "Yunpian credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[AccountBalance](balanceFailure(sdkgo.FailureTransport, "Yunpian request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[AccountBalance](balanceFailure(sdkgo.FailureRateLimit, "Yunpian rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[AccountBalance](balanceFailure(sdkgo.FailureAvailability, "Yunpian is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return balanceBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Yunpian response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[AccountBalance](balanceFailure(sdkgo.FailureTransport, "Yunpian response was interrupted"), 0)
	}
	var wire struct {
		Balance json.RawMessage `json:"balance"`
		Code    *int            `json:"code"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return balanceBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Yunpian returned invalid account JSON")
	}
	if wire.Code != nil {
		switch *wire.Code {
		case -5:
			return sdkgo.NewQueryRetry[AccountBalance](balanceFailure(sdkgo.FailureRateLimit, "Yunpian query frequency exceeded"), response.retryAfter)
		case -50, -51:
			return sdkgo.NewQueryRetry[AccountBalance](balanceFailure(sdkgo.FailureAvailability, "Yunpian reported a temporary failure"), response.retryAfter)
		default:
			if *wire.Code != 0 {
				return balanceBranch(GetAccountBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, "Yunpian rejected the account query with code "+strconv.Itoa(*wire.Code))
			}
		}
	}
	if response.status != http.StatusOK {
		return balanceBranch(GetAccountBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, "Yunpian rejected HTTP "+strconv.Itoa(response.status))
	}
	if len(wire.Balance) == 0 || bytes.Equal(wire.Balance, []byte("null")) {
		return balanceBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Yunpian account balance is missing")
	}
	var balance float64
	if json.Unmarshal(wire.Balance, &balance) != nil || balance < 0 {
		return balanceBranch(GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "Yunpian account balance is invalid")
	}
	return sdkgo.NewQueryBranch(GetAccountBalanceBranchRead, AccountBalance{Balance: balance}, nil, sdkgo.Receipt{})
}

func balanceFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "yunpian", Operation: "getAccountBalance", Message: message}
}
func balanceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[AccountBalance] {
	failure := balanceFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, AccountBalance{}, &failure, sdkgo.Receipt{})
}
