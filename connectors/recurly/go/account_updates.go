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
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SetAccountTimeZoneInput replaces an account's preferred time zone.
type SetAccountTimeZoneInput struct {
	// AccountID is an account ID or code- prefixed code.
	AccountID string `json:"accountId"`
	// TimeZone is a supported IANA time zone such as America/Los_Angeles.
	TimeZone string `json:"timeZone"`
}

// AccountTimeZone contains account identity and its preferred time zone.
type AccountTimeZone struct {
	// ID is the Recurly account ID.
	ID string `json:"id"`
	// Code is the stable account code.
	Code string `json:"code"`
	// TimeZone is the account's preferred IANA time zone.
	TimeZone string `json:"timeZone"`
}

// ReactivateAccountInput selects one inactive account.
type ReactivateAccountInput struct {
	// AccountID is an account ID or code- prefixed code.
	AccountID string `json:"accountId"`
}

// AccountLifecycle contains account identity and active state.
type AccountLifecycle struct {
	// ID is the Recurly account ID.
	ID string `json:"id"`
	// Code is the stable account code.
	Code string `json:"code"`
	// State is the provider lifecycle state.
	State string `json:"state"`
}

// SetAccountTimeZoneOperation replaces an account's preferred time zone.
type SetAccountTimeZoneOperation struct{ client *Client }

// SetAccountTimeZone binds the Mutation to this client.
func (client *Client) SetAccountTimeZone() SetAccountTimeZoneOperation {
	return SetAccountTimeZoneOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (SetAccountTimeZoneOperation) Definition() sdkgo.MutationDefinition {
	return SetAccountTimeZoneDefinition
}

// IdempotencyKey records the stable Dex Call ID for this replacement.
func (SetAccountTimeZoneOperation) IdempotencyKey(callID sdkgo.CallID, _ SetAccountTimeZoneInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the account time zone with an absolute IANA value.
func (operation SetAccountTimeZoneOperation) Invoke(call sdkgo.Call, input SetAccountTimeZoneInput) sdkgo.MutationAttempt[AccountTimeZone] {
	if !validAccountID(input.AccountID) || input.TimeZone == "" {
		return accountTimeZoneBranch(SetAccountTimeZoneBranchDefect, sdkgo.FailureValidation, "Recurly account ID or time zone is invalid")
	}
	if _, err := time.LoadLocation(input.TimeZone); err != nil {
		return accountTimeZoneBranch(SetAccountTimeZoneBranchDefect, sdkgo.FailureValidation, "Recurly account time zone is invalid")
	}
	payload, err := json.Marshal(map[string]string{"preferred_time_zone": input.TimeZone})
	if err != nil {
		return accountTimeZoneBranch(SetAccountTimeZoneBranchDefect, sdkgo.FailureLocalDefect, "Recurly account payload could not be encoded")
	}
	response := operation.client.putAccount(call, "/accounts/"+url.PathEscape(input.AccountID), payload)
	if errors.Is(response.err, errCredential) {
		return accountTimeZoneBranch(SetAccountTimeZoneBranchDefect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return accountTimeZoneRetry(sdkgo.FailureTransport, "Recurly account update request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return accountTimeZoneRetry(sdkgo.FailureRateLimit, "Recurly rate limited account update", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return accountTimeZoneRetry(sdkgo.FailureAvailability, "Recurly account update outcome is unknown", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return accountTimeZoneBranch(SetAccountTimeZoneBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected account update")
	}
	if response.err != nil {
		return accountTimeZoneRetry(sdkgo.FailureTransport, "Recurly account update response was interrupted", 0)
	}
	var wire struct {
		ID       string `json:"id"`
		Code     string `json:"code"`
		TimeZone string `json:"preferred_time_zone"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.TimeZone != input.TimeZone || !accountMatchesID(wire.ID, wire.Code, input.AccountID) {
		return accountTimeZoneRetry(sdkgo.FailureProtocol, "Recurly account may have updated but response was invalid", 0)
	}
	return sdkgo.NewMutationBranch(SetAccountTimeZoneBranchUpdated, AccountTimeZone{ID: wire.ID, Code: wire.Code, TimeZone: wire.TimeZone}, nil, sdkgo.Receipt{})
}

// ReactivateAccountOperation reactivates an inactive account.
type ReactivateAccountOperation struct{ client *Client }

// ReactivateAccount binds the Mutation to this client.
func (client *Client) ReactivateAccount() ReactivateAccountOperation {
	return ReactivateAccountOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (ReactivateAccountOperation) Definition() sdkgo.MutationDefinition {
	return ReactivateAccountDefinition
}

// IdempotencyKey records the stable Dex Call ID for this state change.
func (ReactivateAccountOperation) IdempotencyKey(callID sdkgo.CallID, _ ReactivateAccountInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reactivates an account and confirms an already active response by read-back.
func (operation ReactivateAccountOperation) Invoke(call sdkgo.Call, input ReactivateAccountInput) sdkgo.MutationAttempt[AccountLifecycle] {
	if !validAccountID(input.AccountID) {
		return reactivateAccountBranch(ReactivateAccountBranchDefect, sdkgo.FailureValidation, "Recurly account ID is invalid")
	}
	response := operation.client.putAccount(call, "/accounts/"+url.PathEscape(input.AccountID)+"/reactivate", nil)
	if errors.Is(response.err, errCredential) {
		return reactivateAccountBranch(ReactivateAccountBranchDefect, sdkgo.FailureAuthentication, "Recurly credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return reactivateAccountRetry(sdkgo.FailureTransport, "Recurly account reactivation request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return reactivateAccountRetry(sdkgo.FailureRateLimit, "Recurly rate limited account reactivation", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return reactivateAccountRetry(sdkgo.FailureAvailability, "Recurly account reactivation outcome is unknown", response.retryAfter)
	}
	if response.status == http.StatusUnprocessableEntity {
		return operation.confirmAccountActive(call, input.AccountID)
	}
	if response.status != http.StatusOK {
		return reactivateAccountBranch(ReactivateAccountBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected account reactivation")
	}
	if response.err != nil {
		return reactivateAccountRetry(sdkgo.FailureTransport, "Recurly account reactivation response was interrupted", 0)
	}
	lifecycle, valid := decodeAccountLifecycle(response.body, input.AccountID)
	if !valid || lifecycle.State != "active" {
		return reactivateAccountRetry(sdkgo.FailureProtocol, "Recurly account may have reactivated but response was invalid", 0)
	}
	return sdkgo.NewMutationBranch(ReactivateAccountBranchReactivated, lifecycle, nil, sdkgo.Receipt{})
}
func (operation ReactivateAccountOperation) confirmAccountActive(call sdkgo.Call, id string) sdkgo.MutationAttempt[AccountLifecycle] {
	response := operation.client.get(call, "/accounts/"+url.PathEscape(id))
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return reactivateAccountRetry(sdkgo.FailureAvailability, "Recurly account state could not be checked", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return reactivateAccountBranch(ReactivateAccountBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly rejected account state lookup")
	}
	lifecycle, valid := decodeAccountLifecycle(response.body, id)
	if !valid {
		return reactivateAccountRetry(sdkgo.FailureProtocol, "Recurly account state is invalid", 0)
	}
	if lifecycle.State != "active" {
		return reactivateAccountBranch(ReactivateAccountBranchProviderRejected, sdkgo.FailureProviderRejection, "Recurly account is not active")
	}
	return sdkgo.NewMutationBranch(ReactivateAccountBranchReactivated, lifecycle, nil, sdkgo.Receipt{})
}

func (client *Client) putAccount(call sdkgo.Call, path string, payload []byte) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	key := credentials.APIKey.Reveal()
	if !providerhttp.IsHeaderSafeCredential(key) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPut, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/vnd.recurly.v2021-02-25+json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(key+":")))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}

func accountMatchesID(id, code, requested string) bool {
	if !validAccountID(id) || !validAccountID(code) {
		return false
	}
	if len(requested) > 5 && requested[:5] == "code-" {
		return code == requested[5:]
	}
	return id == requested
}
func decodeAccountLifecycle(body []byte, requested string) (AccountLifecycle, bool) {
	var wire struct {
		ID    string `json:"id"`
		Code  string `json:"code"`
		State string `json:"state"`
	}
	if json.Unmarshal(body, &wire) != nil || !accountMatchesID(wire.ID, wire.Code, requested) || wire.State == "" {
		return AccountLifecycle{}, false
	}
	return AccountLifecycle{ID: wire.ID, Code: wire.Code, State: wire.State}, true
}
func accountUpdateFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "recurly", Operation: operation, Message: message}
}
func accountTimeZoneBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AccountTimeZone] {
	failure := accountUpdateFailure("setAccountTimeZone", kind, message)
	return sdkgo.NewMutationBranch(branch, AccountTimeZone{}, &failure, sdkgo.Receipt{})
}
func accountTimeZoneRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[AccountTimeZone] {
	return sdkgo.NewMutationRetry[AccountTimeZone](accountUpdateFailure("setAccountTimeZone", kind, message), after)
}
func reactivateAccountBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[AccountLifecycle] {
	failure := accountUpdateFailure("reactivateAccount", kind, message)
	return sdkgo.NewMutationBranch(branch, AccountLifecycle{}, &failure, sdkgo.Receipt{})
}
func reactivateAccountRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[AccountLifecycle] {
	return sdkgo.NewMutationRetry[AccountLifecycle](accountUpdateFailure("reactivateAccount", kind, message), after)
}
