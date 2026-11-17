// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// UpdateConnectionInput replaces selected ecommerce connection fields.
type UpdateConnectionInput struct {
	// ConnectionID is the numeric provider connection ID.
	ConnectionID string `json:"connectionId"`
	// Service replaces the integration service name when supplied.
	Service *string `json:"service,omitempty"`
	// ExternalID replaces the store identifier when supplied.
	ExternalID *string `json:"externalId,omitempty"`
	// Name replaces the display name when supplied.
	Name *string `json:"name,omitempty"`
	// ListID replaces the assigned contact list when supplied.
	ListID *string `json:"listId,omitempty"`
}

// DeleteConnectionInput selects one ecommerce connection for removal.
type DeleteConnectionInput struct {
	// ConnectionID is the numeric provider connection ID.
	ConnectionID string `json:"connectionId"`
}

// ConnectionDeletion confirms the selected connection is absent.
type ConnectionDeletion struct {
	// ConnectionID is the connection removed or already absent.
	ConnectionID string `json:"connectionId"`
	// Deleted is true when the connection is absent.
	Deleted bool `json:"deleted"`
}

// UpdateConnectionOperation replaces selected connection fields.
type UpdateConnectionOperation struct{ client *Client }

// DeleteConnectionOperation removes one ecommerce integration.
type DeleteConnectionOperation struct{ client *Client }

// UpdateConnection binds the update Mutation.
func (client *Client) UpdateConnection() UpdateConnectionOperation {
	return UpdateConnectionOperation{client}
}

// DeleteConnection binds the delete Mutation.
func (client *Client) DeleteConnection() DeleteConnectionOperation {
	return DeleteConnectionOperation{client}
}

// Definition returns the manifest-generated update contract.
func (UpdateConnectionOperation) Definition() sdkgo.MutationDefinition {
	return UpdateConnectionDefinition
}

// Definition returns the manifest-generated delete contract.
func (DeleteConnectionOperation) Definition() sdkgo.MutationDefinition {
	return DeleteConnectionDefinition
}

// IdempotencyKey identifies an absolute connection replacement.
func (UpdateConnectionOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateConnectionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey identifies a convergent connection deletion.
func (DeleteConnectionOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteConnectionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets selected absolute connection fields by ID.
func (operation UpdateConnectionOperation) Invoke(call sdkgo.Call, input UpdateConnectionInput) sdkgo.MutationAttempt[ConnectionMetadata] {
	if !numericID.MatchString(input.ConnectionID) || input.Service == nil && input.ExternalID == nil && input.Name == nil && input.ListID == nil || input.Service != nil && !validConnectionLabel(*input.Service) || input.ExternalID != nil && !validConnectionLabel(*input.ExternalID) || input.Name != nil && !validConnectionLabel(*input.Name) || input.ListID != nil && !numericID.MatchString(*input.ListID) {
		return connectionMutationBranch("updateConnection", UpdateConnectionBranchDefect, sdkgo.FailureValidation, "connection ID or replacement field is invalid")
	}
	fields := map[string]string{}
	if input.Service != nil {
		fields["service"] = *input.Service
	}
	if input.ExternalID != nil {
		fields["externalid"] = *input.ExternalID
	}
	if input.Name != nil {
		fields["name"] = *input.Name
	}
	if input.ListID != nil {
		fields["listId"] = *input.ListID
	}
	encoded, err := json.Marshal(map[string]any{"connection": fields})
	if err != nil {
		return connectionMutationBranch("updateConnection", UpdateConnectionBranchDefect, sdkgo.FailureLocalDefect, "connection update could not be encoded")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodPut, "/connections/"+input.ConnectionID, encoded)
	if errors.Is(err, errCredential) {
		return connectionMutationBranch("updateConnection", UpdateConnectionBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[ConnectionMetadata](queryFailure("updateConnection", sdkgo.FailureTransport, "request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[ConnectionMetadata](queryFailure("updateConnection", sdkgo.FailureRateLimit, "rate limited"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[ConnectionMetadata](queryFailure("updateConnection", sdkgo.FailureAvailability, "provider unavailable"), retryAfter)
	}
	if status == http.StatusNotFound {
		return connectionMutationBranch("updateConnection", UpdateConnectionBranchNotFound, sdkgo.FailureNotFound, "connection does not exist")
	}
	if status != http.StatusOK {
		return connectionMutationBranch("updateConnection", UpdateConnectionBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return connectionMutationBranch("updateConnection", UpdateConnectionBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "response too large")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[ConnectionMetadata](queryFailure("updateConnection", sdkgo.FailureTransport, "response interrupted"), 0)
	}
	connection, err := decodeConnection(body)
	if err != nil || connection.ID != input.ConnectionID {
		return connectionMutationBranch("updateConnection", UpdateConnectionBranchInvalidResponse, sdkgo.FailureProtocol, "connection response invalid")
	}
	return sdkgo.NewMutationBranch(UpdateConnectionBranchUpdated, connection, nil, dealReceipt(call, connection.ID))
}

// Invoke deletes one connection and converges when the ID is already absent.
func (operation DeleteConnectionOperation) Invoke(call sdkgo.Call, input DeleteConnectionInput) sdkgo.MutationAttempt[ConnectionDeletion] {
	if !numericID.MatchString(input.ConnectionID) {
		return connectionDeletionBranch(DeleteConnectionBranchDefect, sdkgo.FailureValidation, "connection ID must be numeric")
	}
	body, status, retryAfter, err := operation.client.request(call, http.MethodDelete, "/connections/"+input.ConnectionID, nil)
	if errors.Is(err, errCredential) {
		return connectionDeletionBranch(DeleteConnectionBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable")
	}
	if errors.Is(err, errRequest) {
		return sdkgo.NewMutationRetry[ConnectionDeletion](queryFailure("deleteConnection", sdkgo.FailureTransport, "request failed"), 0)
	}
	if status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[ConnectionDeletion](queryFailure("deleteConnection", sdkgo.FailureRateLimit, "rate limited"), retryAfter)
	}
	if status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[ConnectionDeletion](queryFailure("deleteConnection", sdkgo.FailureAvailability, "provider unavailable"), retryAfter)
	}
	if status != http.StatusOK && status != http.StatusNoContent && status != http.StatusNotFound {
		return connectionDeletionBranch(DeleteConnectionBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return connectionDeletionBranch(DeleteConnectionBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "response too large")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[ConnectionDeletion](queryFailure("deleteConnection", sdkgo.FailureTransport, "response interrupted"), 0)
	}
	if status == http.StatusOK && len(body) > 0 && !json.Valid(body) {
		return connectionDeletionBranch(DeleteConnectionBranchInvalidResponse, sdkgo.FailureProtocol, "invalid response JSON")
	}
	return sdkgo.NewMutationBranch(DeleteConnectionBranchDeleted, ConnectionDeletion{ConnectionID: input.ConnectionID, Deleted: true}, nil, dealReceipt(call, input.ConnectionID))
}

func validConnectionLabel(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 250 && !strings.ContainsAny(value, "\r\n")
}
func connectionMutationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ConnectionMetadata] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, ConnectionMetadata{}, &failure, sdkgo.Receipt{})
}
func connectionDeletionBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ConnectionDeletion] {
	failure := queryFailure("deleteConnection", kind, message)
	return sdkgo.NewMutationBranch(branch, ConnectionDeletion{}, &failure, sdkgo.Receipt{})
}
