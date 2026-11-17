// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateOpportunityInput supplies the required deal and relationship fields.
type CreateOpportunityInput struct {
	// Name is the required full name.
	Name string `json:"name"`
	// ContactID is the buyer Contact.Id UUID.
	ContactID string `json:"contactId"`
	// AccountID is the buyer Account.Id UUID.
	AccountID string `json:"accountId"`
	// OwnerID is the responsible Contact.Id UUID.
	OwnerID string `json:"ownerId"`
}

// UpdateOpportunityInput replaces selected opportunity fields.
type UpdateOpportunityInput struct {
	// OpportunityID identifies an existing opportunity.
	OpportunityID string `json:"opportunityId"`
	// Name replaces the full name when set.
	Name *string `json:"name,omitempty"`
	// ContactID replaces the buyer contact UUID when set.
	ContactID *string `json:"contactId,omitempty"`
	// AccountID replaces the buyer account UUID when set.
	AccountID *string `json:"accountId,omitempty"`
	// OwnerID replaces the responsible contact UUID when set.
	OwnerID *string `json:"ownerId,omitempty"`
}

// DeleteOpportunityInput identifies a opportunity to delete.
type DeleteOpportunityInput struct {
	// OpportunityID is the Opportunity.Id UUID.
	OpportunityID string `json:"opportunityId"`
}

// OpportunityWriteOutput confirms the target UUID of an update or deletion.
type OpportunityWriteOutput struct {
	// ID is the target Opportunity.Id UUID.
	ID string `json:"id"`
}

// CreateOpportunityOperation creates one opportunity with bounded sends.
type CreateOpportunityOperation struct{ client *Client }

// UpdateOpportunityOperation changes selected fields of one opportunity.
type UpdateOpportunityOperation struct{ client *Client }

// DeleteOpportunityOperation removes one opportunity.
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

// IdempotencyKey correlates bounded sends; Creatio does not deduplicate this key.
func (CreateOpportunityOperation) IdempotencyKey(id sdkgo.CallID, _ CreateOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies one repeatable absolute update.
func (UpdateOpportunityOperation) IdempotencyKey(id sdkgo.CallID, _ UpdateOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies one repeatable deletion.
func (DeleteOpportunityOperation) IdempotencyKey(id sdkgo.CallID, _ DeleteOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke creates an opportunity and reads back a name match before resending.
func (operation CreateOpportunityOperation) Invoke(call sdkgo.Call, input CreateOpportunityInput) sdkgo.MutationAttempt[OpportunitySummary] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 || !contactUUIDPattern.MatchString(input.ContactID) || !contactUUIDPattern.MatchString(input.AccountID) || !contactUUIDPattern.MatchString(input.OwnerID) {
		return createOpportunityBranch(CreateOpportunityBranchDefect, sdkgo.FailureValidation, "invalid opportunity name or relationship UUID")
	}
	fence, err := singledispatch.NewFence(call, CreateOpportunityDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createOpportunityBranch(CreateOpportunityBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return createOpportunityUncertain("previous creation cannot be confirmed")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
			return createOpportunityBranch(CreateOpportunityBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return sdkgo.NewMutationRetry[OpportunitySummary](opportunitiesFailure("createOpportunity", sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
	}
	if decision.Kind != singledispatch.SendAllowed {
		if decision.Marker.ProviderID != "" {
			response := operation.client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Opportunity("+decision.Marker.ProviderID+")?$select=Id%2CName", nil)
			if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
				return sdkgo.NewMutationRetry[OpportunitySummary](opportunitiesFailure("createOpportunity", sdkgo.FailureAvailability, "Creatio opportunity read-back unavailable"), response.retryAfter)
			}
			if response.status != http.StatusOK {
				return createOpportunityUncertain("saved opportunity ID cannot be confirmed")
			}
			opportunity, decodeErr := decodeOpportunityRecord(response.body)
			if decodeErr != nil || !strings.EqualFold(opportunity.ID, decision.Marker.ProviderID) {
				return createOpportunityUncertain("saved opportunity ID returned an invalid opportunity")
			}
			return sdkgo.NewMutationBranch(CreateOpportunityBranchCreated, opportunity, nil, sdkgo.Receipt{})
		}
		if input.Name != "" {
			matching, known, retry := operation.client.findOpportunityByName(call, token, input.Name)
			if retry {
				return sdkgo.NewMutationRetry[OpportunitySummary](opportunitiesFailure("createOpportunity", sdkgo.FailureAvailability, "Creatio opportunity read-back unavailable"), 0)
			}
			if matching != nil {
				if strings.EqualFold(matching.ID, decision.Marker.ProviderID) {
					return sdkgo.NewMutationBranch(CreateOpportunityBranchCreated, *matching, nil, sdkgo.Receipt{})
				}
				return createOpportunityUncertain("a matching opportunity cannot prove this creation")
			}
			if !known {
				return createOpportunityUncertain("previous creation cannot be confirmed")
			}
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return createOpportunityUncertain("previous creation cannot be confirmed")
		}
	}
	body, marshalErr := json.Marshal(struct {
		Name      string `json:"Name"`
		ContactID string `json:"ContactId"`
		AccountID string `json:"AccountId"`
		OwnerID   string `json:"OwnerId"`
	}{input.Name, input.ContactID, input.AccountID, input.OwnerID})
	if marshalErr != nil {
		return createOpportunityBranch(CreateOpportunityBranchDefect, sdkgo.FailureLocalDefect, "opportunity request could not be encoded")
	}
	var response odataResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.doODataRequest(requestContext, token, http.MethodPost, "/0/odata/Opportunity", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[OpportunitySummary](opportunitiesFailure("createOpportunity", sdkgo.FailureAvailability, "dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[OpportunitySummary](opportunitiesFailure("createOpportunity", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[OpportunitySummary](opportunitiesFailure("createOpportunity", kind, "Creatio did not create the opportunity"), response.retryAfter)
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[OpportunitySummary](opportunitiesFailure("createOpportunity", sdkgo.FailureTransport, "opportunity creation outcome is unknown"), 0)
	}
	if response.status >= 400 {
		if decision.MaximumPriorSends > 0 {
			return createOpportunityUncertain("an earlier opportunity creation may have succeeded")
		}
		return createOpportunityBranch(CreateOpportunityBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.status))
	}
	opportunity, decodeErr := decodeOpportunityRecord(response.body)
	if response.status != http.StatusCreated || decodeErr != nil {
		return sdkgo.NewMutationRetry[OpportunitySummary](opportunitiesFailure("createOpportunity", sdkgo.FailureProtocol, "Creatio returned invalid opportunity creation JSON"), 0)
	}
	if err := fence.RecordProviderID(opportunity.ID); err != nil {
		return sdkgo.NewMutationRetry[OpportunitySummary](opportunitiesFailure("createOpportunity", sdkgo.FailureAvailability, "provider ID checkpoint unavailable"), 0)
	}
	return sdkgo.NewMutationBranch(CreateOpportunityBranchCreated, opportunity, nil, sdkgo.Receipt{})
}

// Invoke patches the named fields without changing other opportunity properties.
func (operation UpdateOpportunityOperation) Invoke(call sdkgo.Call, input UpdateOpportunityInput) sdkgo.MutationAttempt[OpportunityWriteOutput] {
	if !contactUUIDPattern.MatchString(input.OpportunityID) || (input.Name == nil && input.ContactID == nil && input.AccountID == nil && input.OwnerID == nil) || (input.Name != nil && strings.TrimSpace(*input.Name) == "") || (input.ContactID != nil && !contactUUIDPattern.MatchString(*input.ContactID)) || (input.AccountID != nil && !contactUUIDPattern.MatchString(*input.AccountID)) || (input.OwnerID != nil && !contactUUIDPattern.MatchString(*input.OwnerID)) {
		return opportunityWriteBranch("updateOpportunity", UpdateOpportunityBranchDefect, sdkgo.FailureValidation, "invalid opportunity update")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		return opportunityWriteCredentialFailure("updateOpportunity", UpdateOpportunityBranchDefect, err)
	}
	fields := map[string]any{}
	if input.Name != nil {
		fields["Name"] = *input.Name
	}
	if input.ContactID != nil {
		fields["ContactId"] = *input.ContactID
	}
	if input.AccountID != nil {
		fields["AccountId"] = *input.AccountID
	}
	if input.OwnerID != nil {
		fields["OwnerId"] = *input.OwnerID
	}
	body, marshalErr := json.Marshal(fields)
	if marshalErr != nil {
		return opportunityWriteBranch("updateOpportunity", UpdateOpportunityBranchDefect, sdkgo.FailureLocalDefect, "opportunity update could not be encoded")
	}
	response := operation.client.doODataRequest(call.Context, token, http.MethodPatch, "/0/odata/Opportunity("+input.OpportunityID+")", body)
	return classifyOpportunityWrite("updateOpportunity", input.OpportunityID, response, UpdateOpportunityBranchUpdated, UpdateOpportunityBranchNotFound, UpdateOpportunityBranchProviderRejected)
}

// Invoke permanently deletes one Opportunity record.
func (operation DeleteOpportunityOperation) Invoke(call sdkgo.Call, input DeleteOpportunityInput) sdkgo.MutationAttempt[OpportunityWriteOutput] {
	if !contactUUIDPattern.MatchString(input.OpportunityID) {
		return opportunityWriteBranch("deleteOpportunity", DeleteOpportunityBranchDefect, sdkgo.FailureValidation, "invalid opportunity UUID")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		return opportunityWriteCredentialFailure("deleteOpportunity", DeleteOpportunityBranchDefect, err)
	}
	response := operation.client.doODataRequest(call.Context, token, http.MethodDelete, "/0/odata/Opportunity("+input.OpportunityID+")", nil)
	return classifyOpportunityWrite("deleteOpportunity", input.OpportunityID, response, DeleteOpportunityBranchDeleted, DeleteOpportunityBranchNotFound, DeleteOpportunityBranchProviderRejected)
}

func (client *Client) findOpportunityByName(call sdkgo.Call, token string, name string) (*OpportunitySummary, bool, bool) {
	escape := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	filter := "Name eq '" + escape(name) + "'"
	query := url.Values{"$filter": {filter}, "$select": {"Id,Name"}, "$top": {"2"}}
	response := client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Opportunity?"+query.Encode(), nil)
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return nil, false, true
	}
	if response.status != http.StatusOK {
		return nil, false, false
	}
	var envelope struct {
		Value []json.RawMessage `json:"value"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Value == nil {
		return nil, false, true
	}
	if len(envelope.Value) == 0 {
		return nil, true, false
	}
	for _, record := range envelope.Value {
		opportunity, err := decodeOpportunityRecord(record)
		if err != nil {
			return nil, false, true
		}
		if opportunity.Name == name {
			return &opportunity, true, false
		}
	}
	return nil, true, false
}

func classifyOpportunityWrite(operation string, targetID string, response odataResponse, successBranch sdkgo.BranchID, notFoundBranch sdkgo.BranchID, rejectedBranch sdkgo.BranchID) sdkgo.MutationAttempt[OpportunityWriteOutput] {
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[OpportunityWriteOutput](opportunitiesFailure(operation, sdkgo.FailureRateLimit, "Creatio rate limited the write"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[OpportunityWriteOutput](opportunitiesFailure(operation, sdkgo.FailureAvailability, "Creatio write is unavailable"), response.retryAfter)
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[OpportunityWriteOutput](opportunitiesFailure(operation, sdkgo.FailureTransport, "Creatio write response failed"), 0)
	}
	if response.status == http.StatusNotFound {
		return opportunityWriteBranch(operation, notFoundBranch, sdkgo.FailureNotFound, "Creatio opportunity was not found")
	}
	if response.status != http.StatusNoContent {
		return opportunityWriteBranch(operation, rejectedBranch, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.status))
	}
	return sdkgo.NewMutationBranch(successBranch, OpportunityWriteOutput{ID: targetID}, nil, sdkgo.Receipt{})
}

func opportunityWriteCredentialFailure(operation string, defectBranch sdkgo.BranchID, err error) sdkgo.MutationAttempt[OpportunityWriteOutput] {
	if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
		return opportunityWriteBranch(operation, defectBranch, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
	}
	return sdkgo.NewMutationRetry[OpportunityWriteOutput](opportunitiesFailure(operation, sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
}

func opportunityWriteBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[OpportunityWriteOutput] {
	failure := opportunitiesFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, OpportunityWriteOutput{}, &failure, sdkgo.Receipt{})
}

func createOpportunityBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[OpportunitySummary] {
	failure := opportunitiesFailure("createOpportunity", kind, message)
	return sdkgo.NewMutationBranch(branch, OpportunitySummary{}, &failure, sdkgo.Receipt{})
}

func createOpportunityUncertain(message string) sdkgo.MutationAttempt[OpportunitySummary] {
	return sdkgo.NewMutationUncertain(OpportunitySummary{}, opportunitiesFailure("createOpportunity", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}
