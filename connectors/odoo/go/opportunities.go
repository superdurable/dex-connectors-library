// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package odoo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// Opportunity is a CRM opportunity's public contact information.
type Opportunity struct {
	// ID is the positive CRM record ID.
	ID int64 `json:"id"`
	// Name is the opportunity title.
	Name string `json:"name"`
	// EmailFrom is the prospect's email, when set.
	EmailFrom string `json:"emailFrom"`
	// Phone is the prospect's phone, when set.
	Phone string `json:"phone"`
}

// OpportunitiesPage contains a bounded page of CRM opportunities.
type OpportunitiesPage struct {
	// Opportunities are the returned CRM records.
	Opportunities []Opportunity `json:"opportunities"`
}

// ListOpportunitiesInput selects a bounded opportunity page.
type ListOpportunitiesInput struct {
	// Limit is one through one hundred.
	Limit int `json:"limit"`
	// Offset skips this many records.
	Offset int `json:"offset"`
}

// GetOpportunityInput selects one opportunity.
type GetOpportunityInput struct {
	// ID is a positive CRM record ID.
	ID int64 `json:"id"`
}

// CreateOpportunityInput supplies an opportunity title and prospect details.
type CreateOpportunityInput struct {
	// Name is the required opportunity title.
	Name string `json:"name"`
	// EmailFrom is an optional prospect email used for read-back.
	EmailFrom string `json:"emailFrom"`
	// Phone is an optional prospect phone.
	Phone string `json:"phone"`
}

// CreatedOpportunity identifies the new opportunity.
type CreatedOpportunity struct {
	// ID is the new CRM record ID.
	ID int64 `json:"id"`
}

// UpdateOpportunityInput sets the title and prospect details.
type UpdateOpportunityInput struct {
	// ID is a positive CRM record ID.
	ID int64 `json:"id"`
	// Name is the required title.
	Name string `json:"name"`
	// EmailFrom is the new email; blank clears it.
	EmailFrom string `json:"emailFrom"`
	// Phone is the new phone; blank clears it.
	Phone string `json:"phone"`
}

// UpdatedOpportunity identifies the updated opportunity.
type UpdatedOpportunity struct {
	// ID is the CRM record ID.
	ID int64 `json:"id"`
}

// DeleteOpportunityInput selects one opportunity.
type DeleteOpportunityInput struct {
	// ID is a positive CRM record ID.
	ID int64 `json:"id"`
}

// DeletedOpportunity identifies the removed opportunity.
type DeletedOpportunity struct {
	// ID is the CRM record ID.
	ID int64 `json:"id"`
}

func decodeOpportunities(body []byte) ([]Opportunity, error) {
	var records []struct {
		ID        int64           `json:"id"`
		Name      string          `json:"name"`
		EmailFrom json.RawMessage `json:"email_from"`
		Phone     json.RawMessage `json:"phone"`
	}
	if err := json.Unmarshal(body, &records); err != nil || records == nil {
		return nil, errors.New("opportunity list is invalid")
	}
	opportunities := make([]Opportunity, 0, len(records))
	for _, record := range records {
		if record.ID <= 0 || record.Name == "" {
			return nil, errors.New("opportunity ID or title is invalid")
		}
		email, err := optionalOdooString(record.EmailFrom)
		if err != nil {
			return nil, err
		}
		phone, err := optionalOdooString(record.Phone)
		if err != nil {
			return nil, err
		}
		opportunities = append(opportunities, Opportunity{ID: record.ID, Name: record.Name, EmailFrom: email, Phone: phone})
	}
	return opportunities, nil
}

func validOpportunityWrite(name, email, phone string) bool {
	return validNamePattern(name) && len(email) <= 320 && len(phone) <= 64 && !strings.ContainsAny(email+phone, "\r\n\x00")
}

// ListOpportunitiesOperation lists CRM opportunities.
type ListOpportunitiesOperation struct{ client *Client }

// ListOpportunities binds the opportunity list Query.
func (client *Client) ListOpportunities() ListOpportunitiesOperation {
	return ListOpportunitiesOperation{client}
}

// Definition returns the generated list contract.
func (ListOpportunitiesOperation) Definition() sdkgo.QueryDefinition {
	return ListOpportunitiesDefinition
}

// Invoke lists a bounded page of opportunities.
func (operation ListOpportunitiesOperation) Invoke(call sdkgo.Call, input ListOpportunitiesInput) sdkgo.QueryAttempt[OpportunitiesPage] {
	branches := contactQueryBranches{found: ListOpportunitiesBranchListed, rejected: ListOpportunitiesBranchProviderRejected, invalid: ListOpportunitiesBranchInvalidResponse, defect: ListOpportunitiesBranchDefect}
	if input.Limit < 1 || input.Limit > 100 || input.Offset < 0 {
		return contactQueryBranch[OpportunitiesPage]("listOpportunities", branches.defect, sdkgo.FailureValidation, "limit or offset is invalid")
	}
	body, _ := json.Marshal(map[string]any{"domain": [][]any{{"type", "=", "opportunity"}}, "fields": []string{"id", "name", "email_from", "phone"}, "limit": input.Limit, "offset": input.Offset})
	return invokeContactQuery(call, operation.client, "listOpportunities", "crm.lead", "search_read", body, branches, func(body []byte) (OpportunitiesPage, bool, error) {
		records, err := decodeOpportunities(body)
		if len(records) > input.Limit {
			return OpportunitiesPage{}, false, errors.New("opportunity page exceeds limit")
		}
		return OpportunitiesPage{Opportunities: records}, true, err
	})
}

// GetOpportunityOperation reads one CRM opportunity.
type GetOpportunityOperation struct{ client *Client }

// GetOpportunity binds the opportunity read Query.
func (client *Client) GetOpportunity() GetOpportunityOperation {
	return GetOpportunityOperation{client}
}

// Definition returns the generated read contract.
func (GetOpportunityOperation) Definition() sdkgo.QueryDefinition { return GetOpportunityDefinition }

// Invoke reads one CRM opportunity by ID.
func (operation GetOpportunityOperation) Invoke(call sdkgo.Call, input GetOpportunityInput) sdkgo.QueryAttempt[Opportunity] {
	branches := contactQueryBranches{found: GetOpportunityBranchFound, notFound: GetOpportunityBranchNotFound, rejected: GetOpportunityBranchProviderRejected, invalid: GetOpportunityBranchInvalidResponse, defect: GetOpportunityBranchDefect}
	if input.ID <= 0 {
		return contactQueryBranch[Opportunity]("getOpportunity", branches.defect, sdkgo.FailureValidation, "opportunity ID is invalid")
	}
	body, _ := json.Marshal(map[string]any{"ids": []int64{input.ID}, "fields": []string{"id", "name", "email_from", "phone"}})
	return invokeContactQuery(call, operation.client, "getOpportunity", "crm.lead", "read", body, branches, func(body []byte) (Opportunity, bool, error) {
		records, err := decodeOpportunities(body)
		if err != nil {
			return Opportunity{}, false, err
		}
		if len(records) == 0 {
			return Opportunity{}, false, nil
		}
		if len(records) != 1 || records[0].ID != input.ID {
			return Opportunity{}, false, errors.New("unexpected opportunity ID")
		}
		return records[0], true, nil
	})
}

// CreateOpportunityOperation creates one CRM opportunity with limited sends.
type CreateOpportunityOperation struct{ client *Client }

// CreateOpportunity binds opportunity creation.
func (client *Client) CreateOpportunity() CreateOpportunityOperation {
	return CreateOpportunityOperation{client}
}

// Definition returns the generated creation contract.
func (CreateOpportunityOperation) Definition() sdkgo.MutationDefinition {
	return CreateOpportunityDefinition
}

// IdempotencyKey identifies this call; Odoo accepts no idempotency header.
func (CreateOpportunityOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an opportunity and reads back after an ambiguous send when email is present.
func (operation CreateOpportunityOperation) Invoke(call sdkgo.Call, input CreateOpportunityInput) sdkgo.MutationAttempt[CreatedOpportunity] {
	if !validOpportunityWrite(input.Name, input.EmailFrom, input.Phone) {
		return contactMutationBranch[CreatedOpportunity]("createOpportunity", CreateOpportunityBranchDefect, sdkgo.FailureValidation, "opportunity fields are invalid")
	}
	body, _ := json.Marshal(map[string]any{"vals_list": []any{map[string]any{"type": "opportunity", "name": input.Name, "email_from": input.EmailFrom, "phone": input.Phone}}})
	fence, err := singledispatch.NewFence(call, CreateOpportunityDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return contactMutationBranch[CreatedOpportunity]("createOpportunity", CreateOpportunityBranchDefect, sdkgo.FailureLocalDefect, "dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.MaximumPriorSends > 0 || decision.Kind != singledispatch.SendAllowed {
		found, readError := operation.client.readBackOpportunity(call, input)
		if found.ID > 0 {
			return sdkgo.NewMutationBranch(CreateOpportunityBranchCreated, CreatedOpportunity{ID: found.ID}, nil, sdkgo.Receipt{})
		}
		if readError != nil {
			return sdkgo.NewMutationRetry[CreatedOpportunity](contactFailure("createOpportunity", sdkgo.FailureAvailability, "opportunity read-back is unavailable"), 0)
		}
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(CreatedOpportunity{}, contactFailure("createOpportunity", sdkgo.FailureTransport, "opportunity creation may have succeeded"), sdkgo.Receipt{})
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.callModel(call, requestContext, "crm.lead", "create", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[CreatedOpportunity](contactFailure("createOpportunity", sdkgo.FailureAvailability, "opportunity was not dispatched"), 0)
	}
	if errors.Is(err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CreatedOpportunity{}, contactFailure("createOpportunity", sdkgo.FailureAuthentication, "earlier create may have succeeded"), sdkgo.Receipt{})
		}
		if fence.RecordNotSent() != nil {
			return sdkgo.NewMutationRetry[CreatedOpportunity](contactFailure("createOpportunity", sdkgo.FailureAvailability, "no-send checkpoint failed"), 0)
		}
		return contactMutationBranch[CreatedOpportunity]("createOpportunity", CreateOpportunityBranchDefect, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[CreatedOpportunity](contactFailure("createOpportunity", sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[CreatedOpportunity](contactFailure("createOpportunity", kind, "Odoo temporarily rejected the create"), response.retryAfter)
	}
	if err != nil || response.status >= 500 {
		failure := contactFailure("createOpportunity", sdkgo.FailureAvailability, "opportunity create outcome is unknown")
		if canResend {
			return sdkgo.NewMutationRetry[CreatedOpportunity](failure, response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(CreatedOpportunity{}, failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CreatedOpportunity{}, contactFailure("createOpportunity", sdkgo.FailureProviderRejection, "earlier create may have succeeded"), sdkgo.Receipt{})
		}
		return contactMutationBranch[CreatedOpportunity]("createOpportunity", CreateOpportunityBranchProviderRejected, sdkgo.FailureProviderRejection, "Odoo rejected opportunity creation")
	}
	var ids []int64
	if json.Unmarshal(response.body, &ids) != nil || len(ids) != 1 || ids[0] <= 0 {
		return sdkgo.NewMutationUncertain(CreatedOpportunity{}, contactFailure("createOpportunity", sdkgo.FailureProtocol, "Odoo accepted creation without a valid ID"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateOpportunityBranchCreated, CreatedOpportunity{ID: ids[0]}, nil, sdkgo.Receipt{})
}

func (client *Client) readBackOpportunity(call sdkgo.Call, input CreateOpportunityInput) (Opportunity, error) {
	if input.EmailFrom == "" {
		return Opportunity{}, nil
	}
	body, _ := json.Marshal(map[string]any{"domain": [][]any{{"type", "=", "opportunity"}, {"email_from", "=", input.EmailFrom}, {"name", "=", input.Name}}, "fields": []string{"id", "name", "email_from", "phone"}, "limit": 2})
	response := client.callModel(call, call.Context, "crm.lead", "search_read", body)
	if response.err != nil || response.status != http.StatusOK {
		return Opportunity{}, errors.New("opportunity read-back failed")
	}
	records, err := decodeOpportunities(response.body)
	if err != nil {
		return Opportunity{}, err
	}
	if len(records) == 1 && records[0].Name == input.Name && records[0].EmailFrom == input.EmailFrom && records[0].Phone == input.Phone {
		return records[0], nil
	}
	return Opportunity{}, nil
}

// UpdateOpportunityOperation replaces common CRM opportunity fields.
type UpdateOpportunityOperation struct{ client *Client }

// UpdateOpportunity binds opportunity replacement.
func (client *Client) UpdateOpportunity() UpdateOpportunityOperation {
	return UpdateOpportunityOperation{client}
}

// Definition returns the generated update contract.
func (UpdateOpportunityOperation) Definition() sdkgo.MutationDefinition {
	return UpdateOpportunityDefinition
}

// IdempotencyKey identifies the absolute replacement across retries.
func (UpdateOpportunityOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets title, email, and phone to supplied absolute values.
func (operation UpdateOpportunityOperation) Invoke(call sdkgo.Call, input UpdateOpportunityInput) sdkgo.MutationAttempt[UpdatedOpportunity] {
	if input.ID <= 0 || !validOpportunityWrite(input.Name, input.EmailFrom, input.Phone) {
		return contactMutationBranch[UpdatedOpportunity]("updateOpportunity", UpdateOpportunityBranchDefect, sdkgo.FailureValidation, "opportunity fields are invalid")
	}
	body, _ := json.Marshal(map[string]any{"ids": []int64{input.ID}, "vals": map[string]any{"name": input.Name, "email_from": input.EmailFrom, "phone": input.Phone}})
	return invokeContactBooleanMutation(call, operation.client, "updateOpportunity", "crm.lead", "write", body, input.ID, UpdateOpportunityBranchUpdated, UpdateOpportunityBranchNotFound, UpdateOpportunityBranchProviderRejected, UpdateOpportunityBranchInvalidResponse, UpdateOpportunityBranchDefect, UpdatedOpportunity{ID: input.ID})
}

// DeleteOpportunityOperation deletes one CRM opportunity.
type DeleteOpportunityOperation struct{ client *Client }

// DeleteOpportunity binds opportunity deletion.
func (client *Client) DeleteOpportunity() DeleteOpportunityOperation {
	return DeleteOpportunityOperation{client}
}

// Definition returns the generated deletion contract.
func (DeleteOpportunityOperation) Definition() sdkgo.MutationDefinition {
	return DeleteOpportunityDefinition
}

// IdempotencyKey identifies repeated deletion of the same CRM record.
func (DeleteOpportunityOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the selected opportunity.
func (operation DeleteOpportunityOperation) Invoke(call sdkgo.Call, input DeleteOpportunityInput) sdkgo.MutationAttempt[DeletedOpportunity] {
	if input.ID <= 0 {
		return contactMutationBranch[DeletedOpportunity]("deleteOpportunity", DeleteOpportunityBranchDefect, sdkgo.FailureValidation, "opportunity ID is invalid")
	}
	body, _ := json.Marshal(map[string]any{"ids": []int64{input.ID}})
	return invokeContactBooleanMutation(call, operation.client, "deleteOpportunity", "crm.lead", "unlink", body, input.ID, DeleteOpportunityBranchDeleted, DeleteOpportunityBranchNotFound, DeleteOpportunityBranchProviderRejected, DeleteOpportunityBranchInvalidResponse, DeleteOpportunityBranchDefect, DeletedOpportunity{ID: input.ID})
}
