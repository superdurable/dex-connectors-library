// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package udesk

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ListTicketStatusesInput optionally filters by Udesk's enable flag.
type ListTicketStatusesInput struct {
	// EnabledOnly is omitted to list every status; true or false filters by enabled state.
	EnabledOnly *bool `json:"enabledOnly,omitempty"`
}

// TicketStatus is one account-defined Udesk ticket status.
type TicketStatus struct {
	// StatusID is the stable Udesk status identifier.
	StatusID int64 `json:"statusId"`
	// Name is the Chinese display name, which can be edited for solving substates.
	Name string `json:"name"`
	// EnglishName is the provider-generated English name.
	EnglishName string `json:"englishName"`
	// Enabled reports whether this status is available to users.
	Enabled bool `json:"enabled"`
	// Color is the provider's color value.
	Color string `json:"color"`
}

// ListTicketStatusesOutput contains zero or more ticket statuses.
type ListTicketStatusesOutput struct {
	// Statuses is empty when no statuses match.
	Statuses []TicketStatus `json:"statuses"`
}

// ListTicketStatusesOperation reads account ticket statuses.
type ListTicketStatusesOperation struct{ client *Client }

// Definition returns the generated ticket-status contract.
func (ListTicketStatusesOperation) Definition() sdkgo.QueryDefinition {
	return ListTicketStatusesDefinition
}

// Invoke signs and executes one status-list request and classifies provider and transport outcomes.
func (operation ListTicketStatusesOperation) Invoke(call sdkgo.Call, input ListTicketStatusesInput) sdkgo.QueryAttempt[ListTicketStatusesOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAdminEmail(credentials.AdminEmail.Reveal()) || !isValidToken(credentials.APIToken.Reveal()) {
		return statusBranch(ListTicketStatusesBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return statusRetry(sdkgo.FailureAvailability, "request nonce could not be generated", 0)
	}
	nonce := hex.EncodeToString(nonceBytes)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	email := credentials.AdminEmail.Reveal()
	token := credentials.APIToken.Reveal()
	query := url.Values{"email": {email}, "timestamp": {timestamp}, "nonce": {nonce}, "sign_version": {"v2"}, "sign": {udeskSignature(email, token, timestamp, nonce)}}
	if input.EnabledOnly != nil {
		query.Set("enable", strconv.FormatBool(*input.EnabledOnly))
	}
	endpoint := operation.client.baseURL + "/open_api_v1/tickets/status?" + query.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return statusBranch(ListTicketStatusesBranchDefect, sdkgo.FailureValidation, "ticket-status request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return statusRetry(sdkgo.FailureAvailability, "Udesk request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return statusRetry(sdkgo.FailureRateLimit, "Udesk rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return statusRetry(sdkgo.FailureAvailability, "Udesk is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return statusBranch(ListTicketStatusesBranchProviderRejected, kind, "Udesk rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return statusBranch(ListTicketStatusesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Udesk response exceeded the configured limit")
	}
	if err != nil {
		return statusRetry(sdkgo.FailureAvailability, "Udesk response was interrupted", 0)
	}
	output, code, err := decodeTicketStatuses(body)
	if err != nil {
		return statusBranch(ListTicketStatusesBranchInvalidResponse, sdkgo.FailureProtocol, "Udesk returned an invalid ticket-status response")
	}
	if code != 1000 {
		kind := sdkgo.FailureProviderRejection
		if code == 2059 {
			kind = sdkgo.FailureAuthentication
		}
		return statusBranch(ListTicketStatusesBranchProviderRejected, kind, "Udesk rejected the status query with code "+strconv.Itoa(code))
	}
	return sdkgo.NewQueryBranch(ListTicketStatusesBranchListed, output, nil, sdkgo.Receipt{})
}
func udeskSignature(email string, token string, timestamp string, nonce string) string {
	sum := sha256.Sum256([]byte(email + "&" + token + "&" + timestamp + "&" + nonce + "&v2"))
	return hex.EncodeToString(sum[:])
}
func decodeTicketStatuses(body []byte) (ListTicketStatusesOutput, int, error) {
	var wire struct {
		Code   *int `json:"code"`
		Status []struct {
			StatusID    *int64 `json:"status_id"`
			Name        string `json:"name"`
			EnglishName string `json:"en_name"`
			Enabled     *bool  `json:"enable"`
			Color       string `json:"color"`
		} `json:"status"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Code == nil {
		return ListTicketStatusesOutput{}, 0, errors.New("invalid Udesk response envelope")
	}
	if *wire.Code != 1000 {
		return ListTicketStatusesOutput{}, *wire.Code, nil
	}
	if wire.Status == nil {
		return ListTicketStatusesOutput{}, 0, errors.New("missing Udesk status list")
	}
	output := ListTicketStatusesOutput{Statuses: make([]TicketStatus, 0, len(wire.Status))}
	for _, item := range wire.Status {
		if item.StatusID == nil || *item.StatusID <= 0 || item.Name == "" || item.EnglishName == "" || item.Enabled == nil {
			return ListTicketStatusesOutput{}, 0, errors.New("invalid Udesk status")
		}
		output.Statuses = append(output.Statuses, TicketStatus{StatusID: *item.StatusID, Name: item.Name, EnglishName: item.EnglishName, Enabled: *item.Enabled, Color: item.Color})
	}
	return output, 1000, nil
}
func statusBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListTicketStatusesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "udesk", Operation: "listTicketStatuses", Message: message}
	return sdkgo.NewQueryBranch(branch, ListTicketStatusesOutput{Statuses: []TicketStatus{}}, &failure, sdkgo.Receipt{})
}
func statusRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListTicketStatusesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "udesk", Operation: "listTicketStatuses", Message: message}
	return sdkgo.NewQueryRetry[ListTicketStatusesOutput](failure, delay)
}
