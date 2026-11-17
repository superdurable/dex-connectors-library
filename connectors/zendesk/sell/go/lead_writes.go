// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sell

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateLeadInput supplies the editable identity of a new individual or organization lead.
type CreateLeadInput struct {
	// FirstName is the optional given name of an individual.
	FirstName string `json:"firstName,omitempty"`
	// LastName is required for an individual lead.
	LastName string `json:"lastName,omitempty"`
	// OrganizationName is required for an organization lead.
	OrganizationName string `json:"organizationName,omitempty"`
	// EmailAddress is an optional email address.
	EmailAddress string `json:"emailAddress,omitempty"`
	// Phone is an optional phone number.
	Phone string `json:"phone,omitempty"`
	// Status is the optional account-defined lead status.
	Status string `json:"status,omitempty"`
	// OwnerID is an optional Sell user ID; zero uses the authenticated user.
	OwnerID int64 `json:"ownerId,omitempty"`
}

// UpdateLeadInput changes supplied fields of an existing lead; nil pointers leave fields unchanged.
type UpdateLeadInput struct {
	// LeadID is the positive decimal Sell lead ID.
	LeadID string `json:"leadId"`
	// FirstName sets or clears the lead's given name when supplied.
	FirstName *string `json:"firstName,omitempty"`
	// LastName sets or clears the lead's family name when supplied.
	LastName *string `json:"lastName,omitempty"`
	// OrganizationName sets or clears the lead's organization when supplied.
	OrganizationName *string `json:"organizationName,omitempty"`
	// EmailAddress sets or clears the lead's email when supplied.
	EmailAddress *string `json:"emailAddress,omitempty"`
	// Phone sets or clears the lead's phone when supplied.
	Phone *string `json:"phone,omitempty"`
	// Status sets or clears the account-defined status when supplied.
	Status *string `json:"status,omitempty"`
	// OwnerID assigns the lead to a Sell user when supplied.
	OwnerID *int64 `json:"ownerId,omitempty"`
}

// DeleteLeadInput identifies the lead to delete.
type DeleteLeadInput struct {
	// LeadID is the positive decimal Sell lead ID.
	LeadID string `json:"leadId"`
}

// DeleteLeadOutput records the deleted lead's ID.
type DeleteLeadOutput struct {
	// LeadID is the deleted lead's ID.
	LeadID string `json:"leadId"`
}

// CreateLeadOperation creates one lead under a bounded single-dispatch fence.
type CreateLeadOperation struct{ client *Client }

// UpdateLeadOperation applies an absolute partial update to one lead.
type UpdateLeadOperation struct{ client *Client }

// DeleteLeadOperation deletes one lead.
type DeleteLeadOperation struct{ client *Client }

// CreateLead binds the create Mutation to this client.
func (client *Client) CreateLead() CreateLeadOperation { return CreateLeadOperation{client} }

// UpdateLead binds the update Mutation to this client.
func (client *Client) UpdateLead() UpdateLeadOperation { return UpdateLeadOperation{client} }

// DeleteLead binds the delete Mutation to this client.
func (client *Client) DeleteLead() DeleteLeadOperation { return DeleteLeadOperation{client} }

// Definition returns the generated create Mutation contract.
func (CreateLeadOperation) Definition() sdkgo.MutationDefinition { return CreateLeadDefinition }

// Definition returns the generated update Mutation contract.
func (UpdateLeadOperation) Definition() sdkgo.MutationDefinition { return UpdateLeadDefinition }

// Definition returns the generated delete Mutation contract.
func (DeleteLeadOperation) Definition() sdkgo.MutationDefinition { return DeleteLeadDefinition }

// IdempotencyKey correlates attempts; Sell does not deduplicate this key.
func (CreateLeadOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateLeadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates retries of the same absolute update.
func (UpdateLeadOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateLeadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates retries of the same delete.
func (DeleteLeadOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteLeadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one lead, resending an unconfirmed create only within the call's send cap.
func (operation CreateLeadOperation) Invoke(call sdkgo.Call, input CreateLeadInput) sdkgo.MutationAttempt[LeadMetadata] {
	const operationName = "createLead"
	if strings.TrimSpace(input.LastName) == "" && strings.TrimSpace(input.OrganizationName) == "" || input.OwnerID < 0 {
		return leadWriteBranch(CreateLeadBranchDefect, LeadMetadata{}, operationName, sdkgo.FailureValidation, "a last name or organization name and a nonnegative owner ID are required", call)
	}
	payload, err := json.Marshal(map[string]any{"data": struct {
		FirstName        string `json:"first_name,omitempty"`
		LastName         string `json:"last_name,omitempty"`
		OrganizationName string `json:"organization_name,omitempty"`
		Email            string `json:"email,omitempty"`
		Phone            string `json:"phone,omitempty"`
		Status           string `json:"status,omitempty"`
		OwnerID          int64  `json:"owner_id,omitempty"`
	}{input.FirstName, input.LastName, input.OrganizationName, input.EmailAddress, input.Phone, input.Status, input.OwnerID}})
	if err != nil {
		return leadWriteBranch(CreateLeadBranchDefect, LeadMetadata{}, operationName, sdkgo.FailureValidation, "lead input could not be encoded", call)
	}
	fence, err := singledispatch.NewFence(call, CreateLeadDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return leadWriteBranch(CreateLeadBranchDefect, LeadMetadata{}, operationName, sdkgo.FailureLocalDefect, "lead dispatch fence could not be constructed", call)
	}
	decision := fence.Decide()
	if decision.Marker.ProviderID != "" && decision.Marker.CallID == call.ID {
		body, status, _, readErr := operation.client.get(call, "/leads/"+decision.Marker.ProviderID)
		if readErr == nil && status == http.StatusOK {
			lead, decodeErr := decodeLead(body)
			if decodeErr == nil {
				return sdkgo.NewMutationBranch(CreateLeadBranchCreated, lead, nil, leadWriteReceipt(call, decision.Marker.ProviderID))
			}
		}
		return sdkgo.NewMutationRetry[LeadMetadata](queryFailure(operationName, sdkgo.FailureAvailability, "created lead read-back is unavailable"), 0)
	}
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return sdkgo.NewMutationUncertain(LeadMetadata{}, queryFailure(operationName, sdkgo.FailureTransport, "an earlier lead create may have succeeded"), leadWriteReceipt(call, ""))
	}
	var body []byte
	var status int
	var retryAfter time.Duration
	var sendErr error
	err = fence.Dispatch(func(ctx context.Context) error {
		body, status, retryAfter, sendErr = operation.client.request(ctx, call, http.MethodPost, "/leads", payload)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[LeadMetadata](queryFailure(operationName, sdkgo.FailureAvailability, "lead dispatch checkpoint is unavailable"), 0)
	}
	if errors.Is(sendErr, errCredential) {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[LeadMetadata](queryFailure(operationName, sdkgo.FailureAvailability, "lead no-send checkpoint is unavailable"), 0)
		}
		return leadWriteBranch(CreateLeadBranchDefect, LeadMetadata{}, operationName, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid", call)
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[LeadMetadata](queryFailure(operationName, sdkgo.FailureAvailability, "lead no-send checkpoint is unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[LeadMetadata](queryFailure(operationName, sdkgo.FailureRateLimit, "Zendesk Sell deferred the lead create"), retryAfter)
	}
	if status == http.StatusCreated || status == http.StatusOK {
		if sendErr == nil {
			lead, decodeErr := decodeLead(body)
			if decodeErr == nil {
				_ = fence.RecordProviderID(strings.TrimSpace(strconv.FormatInt(lead.ID, 10)))
				return sdkgo.NewMutationBranch(CreateLeadBranchCreated, lead, nil, leadWriteReceipt(call, strconv.FormatInt(lead.ID, 10)))
			}
		}
	} else if status >= 400 && status < 500 && decision.MaximumPriorSends == 0 {
		return leadWriteBranch(CreateLeadBranchProviderRejected, LeadMetadata{}, operationName, sdkgo.FailureProviderRejection, "Zendesk Sell rejected the lead create", call)
	}
	if decision.MaximumPriorSends+1 < call.MaxSends() {
		return sdkgo.NewMutationRetry[LeadMetadata](queryFailure(operationName, sdkgo.FailureTransport, "lead create response is unconfirmed"), retryAfter)
	}
	return sdkgo.NewMutationUncertain(LeadMetadata{}, queryFailure(operationName, sdkgo.FailureTransport, "lead create may have succeeded"), leadWriteReceipt(call, ""))
}

// Invoke applies fields to an existing lead; repeating the same PUT is safe.
func (operation UpdateLeadOperation) Invoke(call sdkgo.Call, input UpdateLeadInput) sdkgo.MutationAttempt[LeadMetadata] {
	const operationName = "updateLead"
	if !isPositiveLeadID(input.LeadID) || input.FirstName == nil && input.LastName == nil && input.OrganizationName == nil && input.EmailAddress == nil && input.Phone == nil && input.Status == nil && input.OwnerID == nil {
		return leadWriteBranch(UpdateLeadBranchDefect, LeadMetadata{}, operationName, sdkgo.FailureValidation, "positive lead ID and at least one field are required", call)
	}
	if input.OwnerID != nil && *input.OwnerID <= 0 {
		return leadWriteBranch(UpdateLeadBranchDefect, LeadMetadata{}, operationName, sdkgo.FailureValidation, "owner ID must be positive", call)
	}
	fields := map[string]any{}
	if input.FirstName != nil {
		fields["first_name"] = *input.FirstName
	}
	if input.LastName != nil {
		fields["last_name"] = *input.LastName
	}
	if input.OrganizationName != nil {
		fields["organization_name"] = *input.OrganizationName
	}
	if input.EmailAddress != nil {
		fields["email"] = *input.EmailAddress
	}
	if input.Phone != nil {
		fields["phone"] = *input.Phone
	}
	if input.Status != nil {
		fields["status"] = *input.Status
	}
	if input.OwnerID != nil {
		fields["owner_id"] = *input.OwnerID
	}
	payload, err := json.Marshal(map[string]any{"data": fields})
	if err != nil {
		return leadWriteBranch(UpdateLeadBranchDefect, LeadMetadata{}, operationName, sdkgo.FailureValidation, "lead input could not be encoded", call)
	}
	body, status, retryAfter, requestErr := operation.client.request(call.Context, call, http.MethodPut, "/leads/"+input.LeadID, payload)
	if errors.Is(requestErr, errCredential) {
		return leadWriteBranch(UpdateLeadBranchDefect, LeadMetadata{}, operationName, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid", call)
	}
	if requestErr != nil && !errors.Is(requestErr, providerhttp.ErrBodyTooLarge) || status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[LeadMetadata](queryFailure(operationName, sdkgo.FailureTransport, "Zendesk Sell update is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusNotFound {
		return leadWriteBranch(UpdateLeadBranchNotFound, LeadMetadata{}, operationName, sdkgo.FailureNotFound, "lead does not exist", call)
	}
	if status != http.StatusOK {
		return leadWriteBranch(UpdateLeadBranchProviderRejected, LeadMetadata{}, operationName, sdkgo.FailureProviderRejection, "Zendesk Sell rejected the lead update", call)
	}
	if requestErr != nil {
		return leadWriteBranch(UpdateLeadBranchInvalidResponse, LeadMetadata{}, operationName, sdkgo.FailureResponseTooLarge, "lead response exceeded the configured limit", call)
	}
	lead, err := decodeLead(body)
	if err != nil {
		return leadWriteBranch(UpdateLeadBranchInvalidResponse, LeadMetadata{}, operationName, sdkgo.FailureProtocol, "Zendesk Sell returned invalid lead JSON", call)
	}
	return sdkgo.NewMutationBranch(UpdateLeadBranchUpdated, lead, nil, leadWriteReceipt(call, input.LeadID))
}

// Invoke deletes one lead; repeating the DELETE cannot create another lead.
func (operation DeleteLeadOperation) Invoke(call sdkgo.Call, input DeleteLeadInput) sdkgo.MutationAttempt[DeleteLeadOutput] {
	const operationName = "deleteLead"
	if !isPositiveLeadID(input.LeadID) {
		return leadWriteBranch(DeleteLeadBranchDefect, DeleteLeadOutput{}, operationName, sdkgo.FailureValidation, "positive lead ID is required", call)
	}
	_, status, retryAfter, requestErr := operation.client.request(call.Context, call, http.MethodDelete, "/leads/"+input.LeadID, nil)
	if errors.Is(requestErr, errCredential) {
		return leadWriteBranch(DeleteLeadBranchDefect, DeleteLeadOutput{}, operationName, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid", call)
	}
	if requestErr != nil || status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[DeleteLeadOutput](queryFailure(operationName, sdkgo.FailureTransport, "Zendesk Sell delete is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusNotFound {
		return leadWriteBranch(DeleteLeadBranchNotFound, DeleteLeadOutput{}, operationName, sdkgo.FailureNotFound, "lead does not exist", call)
	}
	if status != http.StatusNoContent {
		return leadWriteBranch(DeleteLeadBranchProviderRejected, DeleteLeadOutput{}, operationName, sdkgo.FailureProviderRejection, "Zendesk Sell rejected the lead delete", call)
	}
	return sdkgo.NewMutationBranch(DeleteLeadBranchDeleted, DeleteLeadOutput{LeadID: input.LeadID}, nil, leadWriteReceipt(call, input.LeadID))
}

func leadWriteBranch[T any](branch sdkgo.BranchID, value T, operation string, kind sdkgo.FailureKind, message string, call sdkgo.Call) sdkgo.MutationAttempt[T] {
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, leadWriteReceipt(call, ""))
}

func leadWriteReceipt(call sdkgo.Call, providerID string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "zendesk-sell", ProviderObjectID: providerID, ObservedAt: time.Now()}
}
