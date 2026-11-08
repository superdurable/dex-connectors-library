// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recurly

import (
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

// GetAccountInput selects a Recurly account by ID or code- prefixed account code.
type GetAccountInput struct {
	// AccountID is an account ID or a code- prefixed account code.
	AccountID string `json:"accountId"`
}

// AccountIdentity excludes contact, billing, and payment details.
type AccountIdentity struct {
	// ID is the Recurly account ID.
	ID string `json:"id"`
	// Code is the account's stable merchant-supplied code.
	Code string `json:"code"`
}

// GetAccountOperation reads one account's identity and state.
type GetAccountOperation struct{ client *Client }

// GetAccount binds the Query to this client.
func (client *Client) GetAccount() GetAccountOperation {
	return GetAccountOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetAccountOperation) Definition() sdkgo.QueryDefinition { return GetAccountDefinition }

// Invoke reads one account and classifies retryable and definitive outcomes.
func (operation GetAccountOperation) Invoke(call sdkgo.Call, input GetAccountInput) sdkgo.QueryAttempt[AccountIdentity] {
	if !validAccountID(input.AccountID) {
		return accountBranch(GetAccountBranchDefect, sdkgo.FailureValidation, "account ID must be 1–50 URL-safe characters")
	}
	response := operation.client.get(call, "/accounts/"+url.PathEscape(input.AccountID))
	if errors.Is(response.err, errCredential) {
		return accountBranch(GetAccountBranchDefect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[AccountIdentity](accountFailure(sdkgo.FailureTransport, "Recurly request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[AccountIdentity](accountFailure(sdkgo.FailureRateLimit, "Recurly rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[AccountIdentity](accountFailure(sdkgo.FailureAvailability, "Recurly is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return accountBranch(GetAccountBranchNotFound, sdkgo.FailureNotFound, "Recurly did not expose the account")
	}
	if response.status != http.StatusOK {
		return accountBranch(GetAccountBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return accountBranch(GetAccountBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Recurly response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[AccountIdentity](accountFailure(sdkgo.FailureTransport, "Recurly response was interrupted"), 0)
	}
	accountIdentity, err := decodeAccount(response.body, input.AccountID)
	if err != nil {
		return accountBranch(GetAccountBranchInvalidResponse, sdkgo.FailureProtocol, "Recurly returned invalid account JSON")
	}
	return sdkgo.NewQueryBranch(GetAccountBranchFound, accountIdentity, nil, sdkgo.Receipt{})
}

func validAccountID(accountID string) bool {
	if len(accountID) == 0 || len(accountID) > 55 || accountID == "code-" {
		return false
	}
	for _, character := range accountID {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func decodeAccount(body []byte, requestedAccountID string) (AccountIdentity, error) {
	var wire struct {
		ID   string `json:"id"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return AccountIdentity{}, err
	}
	if wire.ID == "" || wire.Code == "" || !validAccountID(wire.ID) ||
		(strings.HasPrefix(requestedAccountID, "code-") && wire.Code != strings.TrimPrefix(requestedAccountID, "code-")) ||
		(!strings.HasPrefix(requestedAccountID, "code-") && wire.ID != requestedAccountID) {
		return AccountIdentity{}, fmt.Errorf("account identity does not match request")
	}
	return AccountIdentity{ID: wire.ID, Code: wire.Code}, nil
}

func accountFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "recurly", Operation: "getAccount", Message: message}
}

func accountBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[AccountIdentity] {
	failure := accountFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, AccountIdentity{}, &failure, sdkgo.Receipt{})
}
