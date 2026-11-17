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

// CreateLeadInput supplies a name for a new lead.
type CreateLeadInput struct {
	// Name is the required full name.
	Name string `json:"name"`
}

// UpdateLeadInput replaces selected lead fields.
type UpdateLeadInput struct {
	// LeadID identifies an existing lead.
	LeadID string `json:"leadId"`
	// Name replaces the full name when set.
	Name *string `json:"name,omitempty"`
}

// DeleteLeadInput identifies a lead to delete.
type DeleteLeadInput struct {
	// LeadID is the Lead.Id UUID.
	LeadID string `json:"leadId"`
}

// LeadWriteOutput confirms the target UUID of an update or deletion.
type LeadWriteOutput struct {
	// ID is the target Lead.Id UUID.
	ID string `json:"id"`
}

// CreateLeadOperation creates one lead with bounded sends.
type CreateLeadOperation struct{ client *Client }

// UpdateLeadOperation changes selected fields of one lead.
type UpdateLeadOperation struct{ client *Client }

// DeleteLeadOperation removes one lead.
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

// IdempotencyKey correlates bounded sends; Creatio does not deduplicate this key.
func (CreateLeadOperation) IdempotencyKey(id sdkgo.CallID, _ CreateLeadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies one repeatable absolute update.
func (UpdateLeadOperation) IdempotencyKey(id sdkgo.CallID, _ UpdateLeadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies one repeatable deletion.
func (DeleteLeadOperation) IdempotencyKey(id sdkgo.CallID, _ DeleteLeadInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke creates an lead and reads back a name match before resending.
func (operation CreateLeadOperation) Invoke(call sdkgo.Call, input CreateLeadInput) sdkgo.MutationAttempt[LeadSummary] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 255 {
		return createLeadBranch(CreateLeadBranchDefect, sdkgo.FailureValidation, "invalid lead name")
	}
	fence, err := singledispatch.NewFence(call, CreateLeadDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createLeadBranch(CreateLeadBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return createLeadUncertain("previous creation cannot be confirmed")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
			return createLeadBranch(CreateLeadBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return sdkgo.NewMutationRetry[LeadSummary](leadsFailure("createLead", sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
	}
	if decision.Kind != singledispatch.SendAllowed {
		if decision.Marker.ProviderID != "" {
			response := operation.client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Lead("+decision.Marker.ProviderID+")?$select=Id%2CLeadName", nil)
			if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
				return sdkgo.NewMutationRetry[LeadSummary](leadsFailure("createLead", sdkgo.FailureAvailability, "Creatio lead read-back unavailable"), response.retryAfter)
			}
			if response.status != http.StatusOK {
				return createLeadUncertain("saved lead ID cannot be confirmed")
			}
			lead, decodeErr := decodeLeadRecord(response.body)
			if decodeErr != nil || !strings.EqualFold(lead.ID, decision.Marker.ProviderID) {
				return createLeadUncertain("saved lead ID returned an invalid lead")
			}
			return sdkgo.NewMutationBranch(CreateLeadBranchCreated, lead, nil, sdkgo.Receipt{})
		}
		if input.Name != "" {
			matching, known, retry := operation.client.findLeadByName(call, token, input.Name)
			if retry {
				return sdkgo.NewMutationRetry[LeadSummary](leadsFailure("createLead", sdkgo.FailureAvailability, "Creatio lead read-back unavailable"), 0)
			}
			if matching != nil {
				if strings.EqualFold(matching.ID, decision.Marker.ProviderID) {
					return sdkgo.NewMutationBranch(CreateLeadBranchCreated, *matching, nil, sdkgo.Receipt{})
				}
				return createLeadUncertain("a matching lead cannot prove this creation")
			}
			if !known {
				return createLeadUncertain("previous creation cannot be confirmed")
			}
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return createLeadUncertain("previous creation cannot be confirmed")
		}
	}
	body, marshalErr := json.Marshal(struct {
		Name string `json:"LeadName"`
	}{input.Name})
	if marshalErr != nil {
		return createLeadBranch(CreateLeadBranchDefect, sdkgo.FailureLocalDefect, "lead request could not be encoded")
	}
	var response odataResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.doODataRequest(requestContext, token, http.MethodPost, "/0/odata/Lead", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[LeadSummary](leadsFailure("createLead", sdkgo.FailureAvailability, "dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[LeadSummary](leadsFailure("createLead", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[LeadSummary](leadsFailure("createLead", kind, "Creatio did not create the lead"), response.retryAfter)
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[LeadSummary](leadsFailure("createLead", sdkgo.FailureTransport, "lead creation outcome is unknown"), 0)
	}
	if response.status >= 400 {
		if decision.MaximumPriorSends > 0 {
			return createLeadUncertain("an earlier lead creation may have succeeded")
		}
		return createLeadBranch(CreateLeadBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.status))
	}
	lead, decodeErr := decodeLeadRecord(response.body)
	if response.status != http.StatusCreated || decodeErr != nil {
		return sdkgo.NewMutationRetry[LeadSummary](leadsFailure("createLead", sdkgo.FailureProtocol, "Creatio returned invalid lead creation JSON"), 0)
	}
	if err := fence.RecordProviderID(lead.ID); err != nil {
		return sdkgo.NewMutationRetry[LeadSummary](leadsFailure("createLead", sdkgo.FailureAvailability, "provider ID checkpoint unavailable"), 0)
	}
	return sdkgo.NewMutationBranch(CreateLeadBranchCreated, lead, nil, sdkgo.Receipt{})
}

// Invoke patches the named fields without changing other lead properties.
func (operation UpdateLeadOperation) Invoke(call sdkgo.Call, input UpdateLeadInput) sdkgo.MutationAttempt[LeadWriteOutput] {
	if !contactUUIDPattern.MatchString(input.LeadID) || input.Name == nil || strings.TrimSpace(*input.Name) == "" {
		return leadWriteBranch("updateLead", UpdateLeadBranchDefect, sdkgo.FailureValidation, "invalid lead update")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		return leadWriteCredentialFailure("updateLead", UpdateLeadBranchDefect, err)
	}
	fields := map[string]any{}
	if input.Name != nil {
		fields["LeadName"] = *input.Name
	}
	body, marshalErr := json.Marshal(fields)
	if marshalErr != nil {
		return leadWriteBranch("updateLead", UpdateLeadBranchDefect, sdkgo.FailureLocalDefect, "lead update could not be encoded")
	}
	response := operation.client.doODataRequest(call.Context, token, http.MethodPatch, "/0/odata/Lead("+input.LeadID+")", body)
	return classifyLeadWrite("updateLead", input.LeadID, response, UpdateLeadBranchUpdated, UpdateLeadBranchNotFound, UpdateLeadBranchProviderRejected)
}

// Invoke permanently deletes one Lead record.
func (operation DeleteLeadOperation) Invoke(call sdkgo.Call, input DeleteLeadInput) sdkgo.MutationAttempt[LeadWriteOutput] {
	if !contactUUIDPattern.MatchString(input.LeadID) {
		return leadWriteBranch("deleteLead", DeleteLeadBranchDefect, sdkgo.FailureValidation, "invalid lead UUID")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		return leadWriteCredentialFailure("deleteLead", DeleteLeadBranchDefect, err)
	}
	response := operation.client.doODataRequest(call.Context, token, http.MethodDelete, "/0/odata/Lead("+input.LeadID+")", nil)
	return classifyLeadWrite("deleteLead", input.LeadID, response, DeleteLeadBranchDeleted, DeleteLeadBranchNotFound, DeleteLeadBranchProviderRejected)
}

func (client *Client) findLeadByName(call sdkgo.Call, token string, name string) (*LeadSummary, bool, bool) {
	escape := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	filter := "LeadName eq '" + escape(name) + "'"
	query := url.Values{"$filter": {filter}, "$select": {"Id,LeadName"}, "$top": {"2"}}
	response := client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Lead?"+query.Encode(), nil)
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
		lead, err := decodeLeadRecord(record)
		if err != nil {
			return nil, false, true
		}
		if lead.Name == name {
			return &lead, true, false
		}
	}
	return nil, true, false
}

func classifyLeadWrite(operation string, targetID string, response odataResponse, successBranch sdkgo.BranchID, notFoundBranch sdkgo.BranchID, rejectedBranch sdkgo.BranchID) sdkgo.MutationAttempt[LeadWriteOutput] {
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[LeadWriteOutput](leadsFailure(operation, sdkgo.FailureRateLimit, "Creatio rate limited the write"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[LeadWriteOutput](leadsFailure(operation, sdkgo.FailureAvailability, "Creatio write is unavailable"), response.retryAfter)
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[LeadWriteOutput](leadsFailure(operation, sdkgo.FailureTransport, "Creatio write response failed"), 0)
	}
	if response.status == http.StatusNotFound {
		return leadWriteBranch(operation, notFoundBranch, sdkgo.FailureNotFound, "Creatio lead was not found")
	}
	if response.status != http.StatusNoContent {
		return leadWriteBranch(operation, rejectedBranch, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.status))
	}
	return sdkgo.NewMutationBranch(successBranch, LeadWriteOutput{ID: targetID}, nil, sdkgo.Receipt{})
}

func leadWriteCredentialFailure(operation string, defectBranch sdkgo.BranchID, err error) sdkgo.MutationAttempt[LeadWriteOutput] {
	if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
		return leadWriteBranch(operation, defectBranch, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
	}
	return sdkgo.NewMutationRetry[LeadWriteOutput](leadsFailure(operation, sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
}

func leadWriteBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[LeadWriteOutput] {
	failure := leadsFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, LeadWriteOutput{}, &failure, sdkgo.Receipt{})
}

func createLeadBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[LeadSummary] {
	failure := leadsFailure("createLead", kind, message)
	return sdkgo.NewMutationBranch(branch, LeadSummary{}, &failure, sdkgo.Receipt{})
}

func createLeadUncertain(message string) sdkgo.MutationAttempt[LeadSummary] {
	return sdkgo.NewMutationUncertain(LeadSummary{}, leadsFailure("createLead", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}
