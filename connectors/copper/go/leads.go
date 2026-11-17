// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateLeadInput supplies the new lead's name and email for read-back.
type CreateLeadInput struct {
	// Name is the required display name.
	Name string `json:"name"`
	// Email is required to search for a possibly created lead after an unknown outcome.
	Email string `json:"email"`
}

// UpdateLeadInput changes the named fields of one lead.
type UpdateLeadInput struct {
	// LeadID is a positive numeric ID.
	LeadID string `json:"leadId"`
	// Name replaces the display name.
	Name string `json:"name"`
	// Details replaces the notes; blank clears them.
	Details string `json:"details"`
}

// DeleteLeadInput identifies the lead to remove permanently.
type DeleteLeadInput struct {
	// LeadID is a positive decimal Copper lead ID.
	LeadID string `json:"leadId"`
}

// DeleteLeadOutput confirms a deleted lead ID.
type DeleteLeadOutput struct {
	// ID is the deleted numeric ID.
	ID int64 `json:"id"`
	// Deleted is true when Copper confirms deletion.
	Deleted bool `json:"deleted"`
}

// CreateLeadOperation creates a lead with a bounded send count.
type CreateLeadOperation struct{ client *Client }

// UpdateLeadOperation changes only the name and details.
type UpdateLeadOperation struct{ client *Client }

// DeleteLeadOperation removes a lead permanently.
type DeleteLeadOperation struct{ client *Client }

// CreateLead binds the mutation to this client.
func (client *Client) CreateLead() CreateLeadOperation { return CreateLeadOperation{client} }

// UpdateLead binds the mutation to this client.
func (client *Client) UpdateLead() UpdateLeadOperation { return UpdateLeadOperation{client} }

// DeleteLead binds the mutation to this client.
func (client *Client) DeleteLead() DeleteLeadOperation { return DeleteLeadOperation{client} }

// Definition returns the generated mutation contract.
func (CreateLeadOperation) Definition() sdkgo.MutationDefinition { return CreateLeadDefinition }

// Definition returns the generated mutation contract.
func (UpdateLeadOperation) Definition() sdkgo.MutationDefinition { return UpdateLeadDefinition }

// Definition returns the generated mutation contract.
func (DeleteLeadOperation) Definition() sdkgo.MutationDefinition { return DeleteLeadDefinition }

// IdempotencyKey correlates bounded create attempts; Copper does not deduplicate this key.
func (CreateLeadOperation) IdempotencyKey(id sdkgo.CallID, _ CreateLeadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies attempts of an absolute field update.
func (UpdateLeadOperation) IdempotencyKey(id sdkgo.CallID, _ UpdateLeadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies attempts of a deletion.
func (DeleteLeadOperation) IdempotencyKey(id sdkgo.CallID, _ DeleteLeadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke replaces one lead's name and notes, leaving other provider fields intact.
func (operation UpdateLeadOperation) Invoke(call sdkgo.Call, input UpdateLeadInput) sdkgo.MutationAttempt[LeadMetadata] {
	if !validLeadID(input.LeadID) || strings.TrimSpace(input.Name) == "" {
		return leadFailure[LeadMetadata]("updateLead", UpdateLeadBranchDefect, sdkgo.FailureValidation, "lead ID and name are required")
	}
	body, _ := json.Marshal(struct {
		Name    string `json:"name"`
		Details string `json:"details"`
	}{input.Name, input.Details})
	response := operation.client.personRequest(call.Context, call, http.MethodPut, "/leads/"+input.LeadID, body)
	if retry, failure := leadResponseFailure("updateLead", response); failure != nil {
		if retry {
			return sdkgo.NewMutationRetry[LeadMetadata](*failure, response.retryAfter)
		}
		branch := UpdateLeadBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = UpdateLeadBranchNotFound
		}
		if errors.Is(response.err, errCredential) {
			branch = UpdateLeadBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, LeadMetadata{}, failure, sdkgo.Receipt{})
	}
	lead, err := decodeLead(response.body)
	if response.status != http.StatusOK || err != nil {
		return sdkgo.NewMutationRetry[LeadMetadata](queryFailure("updateLead", sdkgo.FailureProtocol, "Copper update response is invalid"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateLeadBranchUpdated, lead, nil, sdkgo.Receipt{})
}

// Invoke permanently removes one lead.
func (operation DeleteLeadOperation) Invoke(call sdkgo.Call, input DeleteLeadInput) sdkgo.MutationAttempt[DeleteLeadOutput] {
	if !validLeadID(input.LeadID) {
		return leadFailure[DeleteLeadOutput]("deleteLead", DeleteLeadBranchDefect, sdkgo.FailureValidation, "lead ID must be positive")
	}
	response := operation.client.personRequest(call.Context, call, http.MethodDelete, "/leads/"+input.LeadID, nil)
	if retry, failure := leadResponseFailure("deleteLead", response); failure != nil {
		if retry {
			return sdkgo.NewMutationRetry[DeleteLeadOutput](*failure, response.retryAfter)
		}
		branch := DeleteLeadBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = DeleteLeadBranchNotFound
		}
		if errors.Is(response.err, errCredential) {
			branch = DeleteLeadBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, DeleteLeadOutput{}, failure, sdkgo.Receipt{})
	}
	var wire struct {
		ID      int64 `json:"id"`
		Deleted bool  `json:"is_deleted"`
	}
	if response.status != http.StatusOK || json.Unmarshal(response.body, &wire) != nil || wire.ID <= 0 || !wire.Deleted {
		return sdkgo.NewMutationRetry[DeleteLeadOutput](queryFailure("deleteLead", sdkgo.FailureProtocol, "Copper deletion response is invalid"), 0)
	}
	return sdkgo.NewMutationBranch(DeleteLeadBranchDeleted, DeleteLeadOutput{wire.ID, true}, nil, sdkgo.Receipt{})
}

// Invoke creates a lead and searches its email and name before any resend.
func (operation CreateLeadOperation) Invoke(call sdkgo.Call, input CreateLeadInput) sdkgo.MutationAttempt[LeadMetadata] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 || !validLeadEmail(input.Email) {
		return leadFailure[LeadMetadata]("createLead", CreateLeadBranchDefect, sdkgo.FailureValidation, "name and email are required")
	}
	fence, err := singledispatch.NewFence(call, CreateLeadDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return leadFailure[LeadMetadata]("createLead", CreateLeadBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return leadUncertain("previous lead creation cannot be confirmed")
	}
	if decision.Kind != singledispatch.SendAllowed {
		found, isKnown, retry := operation.client.findLeadByEmail(call, input.Email, input.Name)
		if retry {
			return sdkgo.NewMutationRetry[LeadMetadata](queryFailure("createLead", sdkgo.FailureAvailability, "Copper email search unavailable"), 0)
		}
		if found != nil {
			if decision.Marker.ProviderID == strconv.FormatInt(found.ID, 10) {
				return sdkgo.NewMutationBranch(CreateLeadBranchCreated, *found, nil, sdkgo.Receipt{})
			}
			return leadUncertain("a matching pre-existing lead cannot be distinguished from this creation")
		}
		if decision.Kind == singledispatch.ReadBackRequired || !isKnown {
			return leadUncertain("previous lead creation cannot be confirmed")
		}
	}
	body, err := json.Marshal(struct {
		Name  string `json:"name"`
		Email struct {
			Email    string `json:"email"`
			Category string `json:"category"`
		} `json:"email"`
	}{Name: input.Name, Email: struct {
		Email    string `json:"email"`
		Category string `json:"category"`
	}{Email: input.Email, Category: "work"}})
	if err != nil {
		return leadFailure[LeadMetadata]("createLead", CreateLeadBranchDefect, sdkgo.FailureLocalDefect, "lead request could not be encoded")
	}
	var response personResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.personRequest(requestContext, call, http.MethodPost, "/leads", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[LeadMetadata](queryFailure("createLead", sdkgo.FailureAvailability, "dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[LeadMetadata](queryFailure("createLead", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[LeadMetadata](queryFailure("createLead", kind, "Copper did not create the lead"), response.retryAfter)
	}
	if response.err != nil || response.status >= 500 {
		return sdkgo.NewMutationRetry[LeadMetadata](queryFailure("createLead", sdkgo.FailureTransport, "lead creation outcome is unknown"), 0)
	}
	if response.status >= 400 {
		if decision.MaximumPriorSends > 0 {
			return leadUncertain("an earlier lead creation may have succeeded")
		}
		return leadFailure[LeadMetadata]("createLead", CreateLeadBranchProviderRejected, sdkgo.FailureProviderRejection, "Copper rejected HTTP "+strconv.Itoa(response.status))
	}
	lead, decodeErr := decodeLead(response.body)
	if response.status != http.StatusOK || decodeErr != nil {
		return sdkgo.NewMutationRetry[LeadMetadata](queryFailure("createLead", sdkgo.FailureProtocol, "lead creation response is invalid"), 0)
	}
	if err := fence.RecordProviderID(strconv.FormatInt(lead.ID, 10)); err != nil {
		return sdkgo.NewMutationRetry[LeadMetadata](queryFailure("createLead", sdkgo.FailureAvailability, "provider ID checkpoint unavailable"), 0)
	}
	return sdkgo.NewMutationBranch(CreateLeadBranchCreated, lead, nil, sdkgo.Receipt{})
}

func (client *Client) findLeadByEmail(call sdkgo.Call, email string, name string) (*LeadMetadata, bool, bool) {
	body, err := json.Marshal(struct {
		Emails string `json:"emails"`
	}{email})
	if err != nil {
		return nil, false, true
	}
	response := client.personRequest(call.Context, call, http.MethodPost, "/leads/search", body)
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return nil, false, true
	}
	if response.status != http.StatusOK {
		return nil, false, false
	}
	var records []json.RawMessage
	if json.Unmarshal(response.body, &records) != nil || records == nil {
		return nil, false, true
	}
	for _, record := range records {
		var wire struct {
			Email struct {
				Email string `json:"email"`
			} `json:"email"`
		}
		if json.Unmarshal(record, &wire) != nil {
			return nil, false, true
		}
		lead, err := decodeLead(record)
		if err != nil {
			return nil, false, true
		}
		if strings.EqualFold(wire.Email.Email, email) && lead.Name == name {
			return &lead, true, false
		}
	}
	return nil, true, false
}

func validLeadID(id string) bool {
	value, err := strconv.ParseUint(id, 10, 63)
	return err == nil && value > 0 && strings.TrimSpace(id) == id
}

func validLeadEmail(email string) bool {
	parts := strings.Split(email, "@")
	return len(email) <= 254 && len(parts) == 2 && parts[0] != "" && strings.Contains(parts[1], ".") && !strings.ContainsAny(email, " /\\\r\n\t") && strings.TrimSpace(email) == email
}

func leadResponseFailure(operation string, response personResponse) (bool, *sdkgo.Failure) {
	retry, failure := personResponseFailure(operation, response)
	if failure != nil {
		failure.Message = "Copper lead request failed"
	}
	return retry, failure
}

func leadFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func leadUncertain(message string) sdkgo.MutationAttempt[LeadMetadata] {
	return sdkgo.NewMutationUncertain(LeadMetadata{}, queryFailure("createLead", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}
