// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package submail

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetSMSBalanceInput has no parameters; the connection identifies the SMS app.
type GetSMSBalanceInput struct{}

// SMSBalance contains general and transactional SMS credit amounts.
type SMSBalance struct {
	// GeneralCredits is the documented decimal count of general SMS credits.
	GeneralCredits string `json:"generalCredits"`
	// TransactionalCredits is the documented decimal count of transactional SMS credits.
	TransactionalCredits string `json:"transactionalCredits"`
}

// GetSMSBalanceOperation reads account SMS credits without sending messages.
type GetSMSBalanceOperation struct{ client *Client }

// GetSMSBalance binds the Query to this client.
func (client *Client) GetSMSBalance() GetSMSBalanceOperation { return GetSMSBalanceOperation{client} }

// Definition returns the manifest-generated Query worker.
func (GetSMSBalanceOperation) Definition() sdkgo.QueryDefinition { return GetSMSBalanceDefinition }

// Invoke queries SUBMAIL and classifies the provider outcome.
func (operation GetSMSBalanceOperation) Invoke(call sdkgo.Call, _ GetSMSBalanceInput) sdkgo.QueryAttempt[SMSBalance] {
	response := operation.client.getBalance(call)
	if errors.Is(response.err, errCredential) {
		return balanceBranch(GetSMSBalanceBranchDefect, sdkgo.FailureAuthentication, "SUBMAIL credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[SMSBalance](balanceFailure(sdkgo.FailureTransport, "SUBMAIL request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[SMSBalance](balanceFailure(sdkgo.FailureRateLimit, "SUBMAIL rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[SMSBalance](balanceFailure(sdkgo.FailureAvailability, "SUBMAIL is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return balanceBranch(GetSMSBalanceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "SUBMAIL balance response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[SMSBalance](balanceFailure(sdkgo.FailureTransport, "SUBMAIL response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return balanceBranch(GetSMSBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, "SUBMAIL rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return balanceBranch(GetSMSBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "SUBMAIL returned invalid UTF-8")
	}
	var wire struct {
		Status               string `json:"status"`
		Balance              string `json:"balance"`
		TransactionalBalance string `json:"transactional_balance"`
		Code                 string `json:"code"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return balanceBranch(GetSMSBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "SUBMAIL returned invalid JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return balanceBranch(GetSMSBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "SUBMAIL returned trailing JSON")
	}
	switch wire.Status {
	case "success":
		if !validCredits(wire.Balance) || !validCredits(wire.TransactionalBalance) {
			return balanceBranch(GetSMSBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "SUBMAIL returned invalid SMS credits")
		}
		return sdkgo.NewQueryBranch(GetSMSBalanceBranchFound, SMSBalance{GeneralCredits: wire.Balance, TransactionalCredits: wire.TransactionalBalance}, nil, sdkgo.Receipt{})
	case "error":
		return balanceBranch(GetSMSBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, "SUBMAIL rejected balance query ("+safeErrorCode(wire.Code)+")")
	default:
		return balanceBranch(GetSMSBalanceBranchInvalidResponse, sdkgo.FailureProtocol, "SUBMAIL returned unknown status")
	}
}
func validCredits(value string) bool {
	if len(value) == 0 || len(value) > 30 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
func safeErrorCode(value string) string {
	if len(value) == 0 || len(value) > 8 {
		return "unknown"
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return "unknown"
		}
	}
	return value
}
func balanceFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "submail", Operation: "getSMSBalance", Message: message}
}
func balanceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SMSBalance] {
	failure := balanceFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, SMSBalance{}, &failure, sdkgo.Receipt{})
}
