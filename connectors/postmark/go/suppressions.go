// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postmark

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Suppression identifies an inactive recipient in one Postmark message stream.
type Suppression struct {
	// EmailAddress is the suppressed recipient.
	EmailAddress string `json:"EmailAddress"`
	// SuppressionReason distinguishes hard bounces, complaints, and manual blocks.
	SuppressionReason string `json:"SuppressionReason"`
	// Origin identifies the recipient, customer, or administrator source.
	Origin string `json:"Origin"`
	// CreatedAt is the provider timestamp for the suppression.
	CreatedAt string `json:"CreatedAt,omitempty"`
}

// SuppressionPage contains matching suppressions for one message stream.
type SuppressionPage struct {
	// Suppressions contains the matching recipients.
	Suppressions []Suppression `json:"Suppressions"`
}

// SuppressionAction reports the provider's per-address outcome.
type SuppressionAction struct {
	// EmailAddress is the requested recipient.
	EmailAddress string `json:"EmailAddress"`
	// Status is Suppressed or Deleted on success.
	Status string `json:"Status"`
}

// ListSuppressionsInput chooses a message stream and optional recipient filter.
type ListSuppressionsInput struct {
	// StreamID is the ID shown under the server's Message Streams page.
	StreamID string `json:"streamId"`
	// EmailAddress filters to one recipient when set.
	EmailAddress string `json:"emailAddress,omitempty"`
}

// CreateSuppressionInput blocks one recipient in one message stream.
type CreateSuppressionInput struct {
	// StreamID is the ID shown under the server's Message Streams page.
	StreamID string `json:"streamId"`
	// EmailAddress is the recipient to suppress.
	EmailAddress string `json:"emailAddress"`
}

// DeleteSuppressionInput reactivates one non-complaint recipient in one stream.
type DeleteSuppressionInput struct {
	// StreamID is the ID shown under the server's Message Streams page.
	StreamID string `json:"streamId"`
	// EmailAddress is the recipient to unsuppress.
	EmailAddress string `json:"emailAddress"`
}

// ListSuppressionsOperation lists suppressed recipients.
type ListSuppressionsOperation struct{ client *Client }

// CreateSuppressionOperation blocks one recipient.
type CreateSuppressionOperation struct{ client *Client }

// DeleteSuppressionOperation reactivates one recipient.
type DeleteSuppressionOperation struct{ client *Client }

// ListSuppressions binds the list operation to this client.
func (client *Client) ListSuppressions() ListSuppressionsOperation {
	return ListSuppressionsOperation{client}
}

// CreateSuppression binds the block operation to this client.
func (client *Client) CreateSuppression() CreateSuppressionOperation {
	return CreateSuppressionOperation{client}
}

// DeleteSuppression binds the unblock operation to this client.
func (client *Client) DeleteSuppression() DeleteSuppressionOperation {
	return DeleteSuppressionOperation{client}
}

// Definition returns the generated listSuppressions contract.
func (ListSuppressionsOperation) Definition() sdkgo.QueryDefinition {
	return ListSuppressionsDefinition
}

// Definition returns the generated createSuppression contract.
func (CreateSuppressionOperation) Definition() sdkgo.MutationDefinition {
	return CreateSuppressionDefinition
}

// Definition returns the generated deleteSuppression contract.
func (DeleteSuppressionOperation) Definition() sdkgo.MutationDefinition {
	return DeleteSuppressionDefinition
}

// IdempotencyKey identifies one repeat-safe suppression state change.
func (CreateSuppressionOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSuppressionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies one repeat-safe suppression state change.
func (DeleteSuppressionOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSuppressionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke lists suppressions for one message stream.
func (operation ListSuppressionsOperation) Invoke(call sdkgo.Call, input ListSuppressionsInput) sdkgo.QueryAttempt[SuppressionPage] {
	if !validStreamID(input.StreamID) || input.EmailAddress != "" && !validEmailAddress(input.EmailAddress) {
		return queryFailure[SuppressionPage]("listSuppressions", ListSuppressionsBranchDefect, sdkgo.FailureValidation, "message stream ID or recipient email is invalid")
	}
	path := "/message-streams/" + url.PathEscape(input.StreamID) + "/suppressions/dump"
	if input.EmailAddress != "" {
		path += "?" + url.Values{"EmailAddress": {input.EmailAddress}}.Encode()
	}
	return runQuery(operation.client, call, "listSuppressions", path, ListSuppressionsBranchListed, "", ListSuppressionsBranchProviderRejected, ListSuppressionsBranchInvalidResponse, ListSuppressionsBranchDefect, func(body []byte) (SuppressionPage, error) {
		var page SuppressionPage
		err := json.Unmarshal(body, &page)
		if err == nil && page.Suppressions == nil {
			err = fmt.Errorf("suppressions missing")
		}
		return page, err
	})
}

// Invoke blocks one recipient in the selected message stream.
func (operation CreateSuppressionOperation) Invoke(call sdkgo.Call, input CreateSuppressionInput) sdkgo.MutationAttempt[SuppressionAction] {
	return operation.client.changeSuppression(call, "createSuppression", input.StreamID, input.EmailAddress, false, CreateSuppressionBranchSuppressed, CreateSuppressionBranchProviderRejected, CreateSuppressionBranchInvalidResponse, CreateSuppressionBranchDefect)
}

// Invoke removes one recipient from the selected stream's suppression list.
func (operation DeleteSuppressionOperation) Invoke(call sdkgo.Call, input DeleteSuppressionInput) sdkgo.MutationAttempt[SuppressionAction] {
	return operation.client.changeSuppression(call, "deleteSuppression", input.StreamID, input.EmailAddress, true, DeleteSuppressionBranchDeleted, DeleteSuppressionBranchProviderRejected, DeleteSuppressionBranchInvalidResponse, DeleteSuppressionBranchDefect)
}

func validStreamID(streamID string) bool {
	return streamID != "" && strings.TrimSpace(streamID) == streamID && !strings.Contains(streamID, "/")
}

func (client *Client) changeSuppression(call sdkgo.Call, operation, streamID, emailAddress string, isDelete bool, success, rejected, invalid, defect sdkgo.BranchID) sdkgo.MutationAttempt[SuppressionAction] {
	if !validStreamID(streamID) || !validEmailAddress(emailAddress) {
		return mutationFailure[SuppressionAction](operation, defect, sdkgo.FailureValidation, "message stream ID or recipient email is invalid")
	}
	payload, err := json.Marshal(map[string]any{"Suppressions": []map[string]string{{"EmailAddress": emailAddress}}})
	if err != nil {
		return mutationFailure[SuppressionAction](operation, defect, sdkgo.FailureLocalDefect, "suppression request cannot be encoded")
	}
	path := "/message-streams/" + url.PathEscape(streamID) + "/suppressions"
	if isDelete {
		path += "/delete"
	}
	request, err := client.newRequest(call, http.MethodPost, path, payload)
	if err != nil {
		return mutationFailure[SuppressionAction](operation, defect, sdkgo.FailureAuthentication, "connection credentials or request are invalid")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[SuppressionAction](newFailure(operation, sdkgo.FailureAvailability, "provider request failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[SuppressionAction](newFailure(operation, sdkgo.FailureRateLimit, "provider rate limited the request"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[SuppressionAction](newFailure(operation, sdkgo.FailureAvailability, "provider outcome is unknown"), 0)
	case response.StatusCode != http.StatusOK:
		return mutationFailure[SuppressionAction](operation, rejected, sdkgo.FailureProviderRejection, "provider rejected suppression request")
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return mutationFailure[SuppressionAction](operation, invalid, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[SuppressionAction](newFailure(operation, sdkgo.FailureAvailability, "provider response was interrupted"), 0)
	}
	var envelope struct {
		Suppressions []SuppressionAction `json:"Suppressions"`
	}
	if err := json.Unmarshal(content, &envelope); err != nil || len(envelope.Suppressions) != 1 || envelope.Suppressions[0].EmailAddress != emailAddress {
		return mutationFailure[SuppressionAction](operation, invalid, sdkgo.FailureProtocol, "provider suppression response is invalid")
	}
	action := envelope.Suppressions[0]
	expectedStatus := "Suppressed"
	if isDelete {
		expectedStatus = "Deleted"
	}
	if action.Status == "Failed" {
		return mutationFailure[SuppressionAction](operation, rejected, sdkgo.FailureProviderRejection, "Postmark refused the recipient suppression change")
	}
	if action.Status != expectedStatus {
		return mutationFailure[SuppressionAction](operation, invalid, sdkgo.FailureProtocol, "provider suppression response has unknown status")
	}
	return sdkgo.NewMutationBranch(success, action, nil, sdkgo.Receipt{})
}
