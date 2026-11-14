// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kf5

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var tenantLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// GetTicketInput identifies one ticket in the configured KF5 tenant.
type GetTicketInput struct {
	// TicketID is a positive KF5 ticket identifier.
	TicketID int64 `json:"ticketID"`
}

// GetTicketOutput contains only non-content ticket status metadata.
type GetTicketOutput struct {
	// TicketID is the KF5 ticket identifier.
	TicketID int64 `json:"ticketID"`
	// Status is the provider's ticket status.
	Status string `json:"status"`
	// Priority is the provider's ticket priority when set.
	Priority string `json:"priority"`
	// Type is the provider's ticket type when set.
	Type string `json:"type"`
	// CreatedAt is the provider's ticket creation timestamp.
	CreatedAt string `json:"createdAt"`
	// UpdatedAt is the provider's latest ticket timestamp.
	UpdatedAt string `json:"updatedAt"`
}

// GetTicketOperation reads one ticket without changing provider state.
type GetTicketOperation struct{ client *Client }

// Definition returns the generated ticket read contract.
func (GetTicketOperation) Definition() sdkgo.QueryDefinition { return GetTicketDefinition }

// Invoke reads limited ticket metadata without returning the title, requester, or comments.
func (operation GetTicketOperation) Invoke(call sdkgo.Call, input GetTicketInput) sdkgo.QueryAttempt[GetTicketOutput] {
	if input.TicketID <= 0 {
		return ticketBranch(GetTicketBranchDefect, sdkgo.FailureValidation, "KF5 requires a positive ticket ID")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !tenantLabelPattern.MatchString(credentials.Subdomain) || !isValidEmail(credentials.Email.Reveal()) || !isValidAPIKey(credentials.APIToken.Reveal()) {
		return ticketBranch(GetTicketBranchDefect, sdkgo.FailureAuthentication, "KF5 tenant credentials are unavailable or invalid")
	}
	endpoint := "https://" + credentials.Subdomain + ".kf5.com/apiv2/tickets/" + strconv.FormatInt(input.TicketID, 10) + ".json"
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return ticketBranch(GetTicketBranchDefect, sdkgo.FailureValidation, "KF5 ticket request is invalid")
	}
	request.SetBasicAuth(credentials.Email.Reveal()+"/token", credentials.APIToken.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return ticketRetry(sdkgo.FailureAvailability, "KF5 ticket request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return ticketRetry(sdkgo.FailureRateLimit, "KF5 rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return ticketRetry(sdkgo.FailureAvailability, "KF5 ticket service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode == http.StatusNotFound {
		return ticketBranch(GetTicketBranchNoData, sdkgo.FailureNotFound, "KF5 did not find the ticket")
	}
	if response.StatusCode != http.StatusOK {
		return classifyTicketHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return ticketBranch(GetTicketBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "KF5 response exceeded the configured limit")
	}
	if err != nil {
		return ticketRetry(sdkgo.FailureAvailability, "KF5 response was interrupted", 0)
	}
	var wire struct {
		Ticket *struct {
			ID        int64  `json:"id"`
			Status    string `json:"status"`
			Priority  string `json:"priority"`
			Type      string `json:"type"`
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
		} `json:"ticket"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Ticket == nil || wire.Ticket.ID != input.TicketID || !isTicketStatus(wire.Ticket.Status) || len(wire.Ticket.Priority) > 30 || len(wire.Ticket.Type) > 30 || len(wire.Ticket.CreatedAt) > 40 || len(wire.Ticket.UpdatedAt) > 40 {
		return ticketBranch(GetTicketBranchInvalidResponse, sdkgo.FailureProtocol, "KF5 returned invalid ticket data")
	}
	output := GetTicketOutput{TicketID: wire.Ticket.ID, Status: wire.Ticket.Status, Priority: wire.Ticket.Priority, Type: wire.Ticket.Type, CreatedAt: wire.Ticket.CreatedAt, UpdatedAt: wire.Ticket.UpdatedAt}
	return sdkgo.NewQueryBranch(GetTicketBranchFound, output, nil, sdkgo.Receipt{})
}

func isTicketStatus(value string) bool {
	switch value {
	case "new", "open", "pending", "solved", "closed":
		return true
	}
	return false
}
func classifyTicketHTTPStatus(status int) sdkgo.QueryAttempt[GetTicketOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return ticketBranch(GetTicketBranchProviderRejected, kind, "KF5 rejected ticket request with HTTP "+strconv.Itoa(status))
}
func ticketBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetTicketOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "kf5", Operation: "getTicket", Message: message}
	return sdkgo.NewQueryBranch(branch, GetTicketOutput{}, &failure, sdkgo.Receipt{})
}
func ticketRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetTicketOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "kf5", Operation: "getTicket", Message: message}
	return sdkgo.NewQueryRetry[GetTicketOutput](failure, delay)
}
