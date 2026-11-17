// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recurly

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListAccountsInput selects one bounded page of account identities.
type ListAccountsInput struct {
	// Limit is the number of accounts from 1 to 200.
	Limit int `json:"limit"`
}

// AccountPage contains only account IDs and codes.
type AccountPage struct {
	// Accounts excludes contact and billing information.
	Accounts []AccountIdentity `json:"accounts"`
	// HasMore reports whether Recurly has another page.
	HasMore bool `json:"hasMore"`
}

// CreateAccountInput supplies a readable prefix for a unique call-derived account code.
type CreateAccountInput struct {
	// CodePrefix is 1 to 12 URL-safe characters; the connector appends a unique Dex Call marker.
	CodePrefix string `json:"codePrefix"`
}

// ListAccountsOperation reads a page of account identities.
type ListAccountsOperation struct{ client *Client }

// ListAccounts binds the Query to this client.
func (client *Client) ListAccounts() ListAccountsOperation { return ListAccountsOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListAccountsOperation) Definition() sdkgo.QueryDefinition { return ListAccountsDefinition }

// Invoke lists accounts without contact or billing details.
func (operation ListAccountsOperation) Invoke(call sdkgo.Call, input ListAccountsInput) sdkgo.QueryAttempt[AccountPage] {
	if input.Limit < 1 || input.Limit > 200 {
		return accountPageBranch(ListAccountsBranchDefect, sdkgo.FailureValidation, "Recurly account page size is invalid")
	}
	response := operation.client.get(call, "/accounts?limit="+strconv.Itoa(input.Limit))
	if errors.Is(response.err, errCredential) {
		return accountPageBranch(ListAccountsBranchDefect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return accountPageRetry(sdkgo.FailureTransport, "Recurly account list request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return accountPageRetry(sdkgo.FailureRateLimit, "Recurly rate limited account listing", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return accountPageRetry(sdkgo.FailureAvailability, "Recurly account listing is unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return accountPageBranch(ListAccountsBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected account listing")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return accountPageBranch(ListAccountsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Recurly account list exceeded the response limit")
	}
	if response.err != nil {
		return accountPageRetry(sdkgo.FailureTransport, "Recurly account list was interrupted", 0)
	}
	var wire struct {
		Object  string            `json:"object"`
		Data    []json.RawMessage `json:"data"`
		HasMore bool              `json:"has_more"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Object != "list" || wire.Data == nil {
		return accountPageBranch(ListAccountsBranchInvalidResponse, sdkgo.FailureProtocol, "Recurly account list is invalid")
	}
	page := AccountPage{Accounts: make([]AccountIdentity, 0, len(wire.Data)), HasMore: wire.HasMore}
	for _, raw := range wire.Data {
		var item struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		}
		if json.Unmarshal(raw, &item) != nil || !validAccountID(item.ID) || !validAccountID(item.Code) {
			return accountPageBranch(ListAccountsBranchInvalidResponse, sdkgo.FailureProtocol, "Recurly account list item is invalid")
		}
		page.Accounts = append(page.Accounts, AccountIdentity{ID: item.ID, Code: item.Code})
	}
	return sdkgo.NewQueryBranch(ListAccountsBranchListed, page, nil, sdkgo.Receipt{})
}

// CreateAccountOperation creates an account whose code is unique to a Dex Call.
type CreateAccountOperation struct{ client *Client }

// CreateAccount binds the Mutation to this client.
func (client *Client) CreateAccount() CreateAccountOperation { return CreateAccountOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (CreateAccountOperation) Definition() sdkgo.MutationDefinition { return CreateAccountDefinition }

// IdempotencyKey uses the stable Dex Call ID for the account code.
func (CreateAccountOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateAccountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

var accountCodePrefixPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,12}$`)

// Invoke creates an account and confirms duplicate codes by unique read-back.
func (operation CreateAccountOperation) Invoke(call sdkgo.Call, input CreateAccountInput) sdkgo.MutationAttempt[AccountIdentity] {
	if !accountCodePrefixPattern.MatchString(input.CodePrefix) {
		return createAccountBranch(CreateAccountBranchDefect, sdkgo.FailureValidation, "Recurly account code prefix is invalid")
	}
	code := input.CodePrefix + "_" + strings.ReplaceAll(string(call.IdempotencyKey), "-", "")
	if len(code) > 50 || len(code) < 34 {
		return createAccountBranch(CreateAccountBranchDefect, sdkgo.FailureLocalDefect, "Recurly account code could not be derived")
	}
	payload, _ := json.Marshal(map[string]string{"code": code})
	response := operation.client.postAccount(call, "/accounts", payload)
	if errors.Is(response.err, errCredential) {
		return createAccountBranch(CreateAccountBranchDefect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return createAccountRetry(sdkgo.FailureTransport, "Recurly account request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return createAccountRetry(sdkgo.FailureRateLimit, "Recurly rate limited account creation", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return createAccountRetry(sdkgo.FailureAvailability, "Recurly account outcome is unknown", response.retryAfter)
	}
	if response.status == http.StatusUnprocessableEntity {
		return operation.confirmCreatedAccount(call, code)
	}
	if response.status != http.StatusCreated && response.status != http.StatusOK {
		return createAccountBranch(CreateAccountBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected account creation")
	}
	if response.err != nil {
		return createAccountRetry(sdkgo.FailureTransport, "Recurly account response was interrupted", 0)
	}
	account, err := decodeAccount(response.body, "code-"+code)
	if err != nil {
		return createAccountRetry(sdkgo.FailureProtocol, "Recurly account may exist but response was invalid", 0)
	}
	return sdkgo.NewMutationBranch(CreateAccountBranchCreated, account, nil, sdkgo.Receipt{})
}

func (operation CreateAccountOperation) confirmCreatedAccount(call sdkgo.Call, code string) sdkgo.MutationAttempt[AccountIdentity] {
	response := operation.client.get(call, "/accounts/"+url.PathEscape("code-"+code))
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return createAccountRetry(sdkgo.FailureAvailability, "Recurly account code could not be checked", response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return createAccountBranch(CreateAccountBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected account creation")
	}
	if response.status != http.StatusOK {
		return createAccountBranch(CreateAccountBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected account lookup")
	}
	account, err := decodeAccount(response.body, "code-"+code)
	if err != nil {
		return createAccountRetry(sdkgo.FailureProtocol, "Recurly account code response was invalid", 0)
	}
	return sdkgo.NewMutationBranch(CreateAccountBranchCreated, account, nil, sdkgo.Receipt{})
}

func (client *Client) postAccount(call sdkgo.Call, path string, payload []byte) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	apiKey := credentials.APIKey.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiKey) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/vnd.recurly.v2021-02-25+json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(apiKey+":")))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}

func accountPageFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "recurly", Operation: "listAccounts", Message: message}
}
func accountPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[AccountPage] {
	failure := accountPageFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, AccountPage{}, &failure, sdkgo.Receipt{})
}
func accountPageRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.QueryAttempt[AccountPage] {
	return sdkgo.NewQueryRetry[AccountPage](accountPageFailure(kind, message), after)
}
func createAccountFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "recurly", Operation: "createAccount", Message: message}
}
func createAccountBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AccountIdentity] {
	failure := createAccountFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, AccountIdentity{}, &failure, sdkgo.Receipt{})
}
func createAccountRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[AccountIdentity] {
	return sdkgo.NewMutationRetry[AccountIdentity](createAccountFailure(kind, message), after)
}
