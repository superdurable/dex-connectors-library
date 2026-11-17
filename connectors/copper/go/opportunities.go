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

// CreateOpportunityInput supplies the new opportunity's name.
type CreateOpportunityInput struct {
	// Name is the required display name.
	Name string `json:"name"`
}

// UpdateOpportunityInput changes the named fields of one opportunity.
type UpdateOpportunityInput struct {
	// OpportunityID is a positive numeric ID.
	OpportunityID string `json:"opportunityId"`
	// Name replaces the display name.
	Name string `json:"name"`
	// Details replaces the notes; blank clears them.
	Details string `json:"details"`
}

// DeleteOpportunityInput identifies the opportunity to remove permanently.
type DeleteOpportunityInput struct {
	// OpportunityID is a positive decimal Copper opportunity ID.
	OpportunityID string `json:"opportunityId"`
}

// DeleteOpportunityOutput confirms a deleted opportunity ID.
type DeleteOpportunityOutput struct {
	// ID is the deleted numeric ID.
	ID int64 `json:"id"`
	// Deleted is true when Copper confirms deletion.
	Deleted bool `json:"deleted"`
}

// CreateOpportunityOperation creates an opportunity with a bounded send count.
type CreateOpportunityOperation struct{ client *Client }

// UpdateOpportunityOperation changes only the name and details.
type UpdateOpportunityOperation struct{ client *Client }

// DeleteOpportunityOperation removes an opportunity permanently.
type DeleteOpportunityOperation struct{ client *Client }

// CreateOpportunity binds the mutation to this client.
func (client *Client) CreateOpportunity() CreateOpportunityOperation {
	return CreateOpportunityOperation{client}
}

// UpdateOpportunity binds the mutation to this client.
func (client *Client) UpdateOpportunity() UpdateOpportunityOperation {
	return UpdateOpportunityOperation{client}
}

// DeleteOpportunity binds the mutation to this client.
func (client *Client) DeleteOpportunity() DeleteOpportunityOperation {
	return DeleteOpportunityOperation{client}
}

// Definition returns the generated mutation contract.
func (CreateOpportunityOperation) Definition() sdkgo.MutationDefinition {
	return CreateOpportunityDefinition
}

// Definition returns the generated mutation contract.
func (UpdateOpportunityOperation) Definition() sdkgo.MutationDefinition {
	return UpdateOpportunityDefinition
}

// Definition returns the generated mutation contract.
func (DeleteOpportunityOperation) Definition() sdkgo.MutationDefinition {
	return DeleteOpportunityDefinition
}

// IdempotencyKey correlates bounded create attempts; Copper does not deduplicate this key.
func (CreateOpportunityOperation) IdempotencyKey(id sdkgo.CallID, _ CreateOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies attempts of an absolute field update.
func (UpdateOpportunityOperation) IdempotencyKey(id sdkgo.CallID, _ UpdateOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies attempts of a deletion.
func (DeleteOpportunityOperation) IdempotencyKey(id sdkgo.CallID, _ DeleteOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke replaces one opportunity's name and notes, leaving other provider fields intact.
func (operation UpdateOpportunityOperation) Invoke(call sdkgo.Call, input UpdateOpportunityInput) sdkgo.MutationAttempt[OpportunityMetadata] {
	if !validOpportunityID(input.OpportunityID) || strings.TrimSpace(input.Name) == "" {
		return opportunityFailure[OpportunityMetadata]("updateOpportunity", UpdateOpportunityBranchDefect, sdkgo.FailureValidation, "opportunity ID and name are required")
	}
	body, _ := json.Marshal(struct {
		Name    string `json:"name"`
		Details string `json:"details"`
	}{input.Name, input.Details})
	response := operation.client.personRequest(call.Context, call, http.MethodPut, "/opportunities/"+input.OpportunityID, body)
	if retry, failure := opportunityResponseFailure("updateOpportunity", response); failure != nil {
		if retry {
			return sdkgo.NewMutationRetry[OpportunityMetadata](*failure, response.retryAfter)
		}
		branch := UpdateOpportunityBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = UpdateOpportunityBranchNotFound
		}
		if errors.Is(response.err, errCredential) {
			branch = UpdateOpportunityBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, OpportunityMetadata{}, failure, sdkgo.Receipt{})
	}
	opportunity, err := decodeOpportunity(response.body)
	if response.status != http.StatusOK || err != nil {
		return sdkgo.NewMutationRetry[OpportunityMetadata](queryFailure("updateOpportunity", sdkgo.FailureProtocol, "Copper update response is invalid"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateOpportunityBranchUpdated, opportunity, nil, sdkgo.Receipt{})
}

// Invoke permanently removes one opportunity.
func (operation DeleteOpportunityOperation) Invoke(call sdkgo.Call, input DeleteOpportunityInput) sdkgo.MutationAttempt[DeleteOpportunityOutput] {
	if !validOpportunityID(input.OpportunityID) {
		return opportunityFailure[DeleteOpportunityOutput]("deleteOpportunity", DeleteOpportunityBranchDefect, sdkgo.FailureValidation, "opportunity ID must be positive")
	}
	response := operation.client.personRequest(call.Context, call, http.MethodDelete, "/opportunities/"+input.OpportunityID, nil)
	if retry, failure := opportunityResponseFailure("deleteOpportunity", response); failure != nil {
		if retry {
			return sdkgo.NewMutationRetry[DeleteOpportunityOutput](*failure, response.retryAfter)
		}
		branch := DeleteOpportunityBranchProviderRejected
		if response.status == http.StatusNotFound {
			branch = DeleteOpportunityBranchNotFound
		}
		if errors.Is(response.err, errCredential) {
			branch = DeleteOpportunityBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, DeleteOpportunityOutput{}, failure, sdkgo.Receipt{})
	}
	var wire struct {
		ID      int64 `json:"id"`
		Deleted bool  `json:"is_deleted"`
	}
	if response.status != http.StatusOK || json.Unmarshal(response.body, &wire) != nil || wire.ID <= 0 || !wire.Deleted {
		return sdkgo.NewMutationRetry[DeleteOpportunityOutput](queryFailure("deleteOpportunity", sdkgo.FailureProtocol, "Copper deletion response is invalid"), 0)
	}
	return sdkgo.NewMutationBranch(DeleteOpportunityBranchDeleted, DeleteOpportunityOutput{wire.ID, true}, nil, sdkgo.Receipt{})
}

// Invoke creates an opportunity and searches its name before any resend.
func (operation CreateOpportunityOperation) Invoke(call sdkgo.Call, input CreateOpportunityInput) sdkgo.MutationAttempt[OpportunityMetadata] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 {
		return opportunityFailure[OpportunityMetadata]("createOpportunity", CreateOpportunityBranchDefect, sdkgo.FailureValidation, "name is required")
	}
	fence, err := singledispatch.NewFence(call, CreateOpportunityDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return opportunityFailure[OpportunityMetadata]("createOpportunity", CreateOpportunityBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return opportunityUncertain("previous opportunity creation cannot be confirmed")
	}
	if decision.Kind != singledispatch.SendAllowed {
		found, isKnown, retry := operation.client.findOpportunityByName(call, input.Name)
		if retry {
			return sdkgo.NewMutationRetry[OpportunityMetadata](queryFailure("createOpportunity", sdkgo.FailureAvailability, "Copper opportunity search unavailable"), 0)
		}
		if found != nil {
			if decision.Marker.ProviderID == strconv.FormatInt(found.ID, 10) {
				return sdkgo.NewMutationBranch(CreateOpportunityBranchCreated, *found, nil, sdkgo.Receipt{})
			}
			return opportunityUncertain("a matching opportunity cannot prove this creation")
		}
		if decision.Kind == singledispatch.ReadBackRequired || !isKnown {
			return opportunityUncertain("previous opportunity creation cannot be confirmed")
		}
	}
	body, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: input.Name})
	if err != nil {
		return opportunityFailure[OpportunityMetadata]("createOpportunity", CreateOpportunityBranchDefect, sdkgo.FailureLocalDefect, "opportunity request could not be encoded")
	}
	var response personResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.personRequest(requestContext, call, http.MethodPost, "/opportunities", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[OpportunityMetadata](queryFailure("createOpportunity", sdkgo.FailureAvailability, "dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[OpportunityMetadata](queryFailure("createOpportunity", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[OpportunityMetadata](queryFailure("createOpportunity", kind, "Copper did not create the opportunity"), response.retryAfter)
	}
	if response.err != nil || response.status >= 500 {
		return sdkgo.NewMutationRetry[OpportunityMetadata](queryFailure("createOpportunity", sdkgo.FailureTransport, "opportunity creation outcome is unknown"), 0)
	}
	if response.status >= 400 {
		if decision.MaximumPriorSends > 0 {
			return opportunityUncertain("an earlier opportunity creation may have succeeded")
		}
		return opportunityFailure[OpportunityMetadata]("createOpportunity", CreateOpportunityBranchProviderRejected, sdkgo.FailureProviderRejection, "Copper rejected HTTP "+strconv.Itoa(response.status))
	}
	opportunity, decodeErr := decodeOpportunity(response.body)
	if response.status != http.StatusOK || decodeErr != nil {
		return sdkgo.NewMutationRetry[OpportunityMetadata](queryFailure("createOpportunity", sdkgo.FailureProtocol, "opportunity creation response is invalid"), 0)
	}
	if err := fence.RecordProviderID(strconv.FormatInt(opportunity.ID, 10)); err != nil {
		return sdkgo.NewMutationRetry[OpportunityMetadata](queryFailure("createOpportunity", sdkgo.FailureAvailability, "provider ID checkpoint unavailable"), 0)
	}
	return sdkgo.NewMutationBranch(CreateOpportunityBranchCreated, opportunity, nil, sdkgo.Receipt{})
}

func (client *Client) findOpportunityByName(call sdkgo.Call, name string) (*OpportunityMetadata, bool, bool) {
	body, err := json.Marshal(struct {
		Name string `json:"name"`
	}{name})
	if err != nil {
		return nil, false, true
	}
	response := client.personRequest(call.Context, call, http.MethodPost, "/opportunities/search", body)
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
		opportunity, err := decodeOpportunity(record)
		if err != nil {
			return nil, false, true
		}
		if opportunity.Name == name {
			return &opportunity, true, false
		}
	}
	return nil, true, false
}

func validOpportunityID(id string) bool {
	value, err := strconv.ParseUint(id, 10, 63)
	return err == nil && value > 0 && strings.TrimSpace(id) == id
}

func opportunityResponseFailure(operation string, response personResponse) (bool, *sdkgo.Failure) {
	retry, failure := personResponseFailure(operation, response)
	if failure != nil {
		failure.Message = "Copper opportunity request failed"
	}
	return retry, failure
}

func opportunityFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := queryFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}

func opportunityUncertain(message string) sdkgo.MutationAttempt[OpportunityMetadata] {
	return sdkgo.NewMutationUncertain(OpportunityMetadata{}, queryFailure("createOpportunity", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}
