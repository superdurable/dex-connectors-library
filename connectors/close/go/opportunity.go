// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package close

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Opportunity identifies a Close deal and its monetary value in cents.
type Opportunity struct {
	// ID is the provider-assigned opportunity identifier.
	ID string `json:"id"`
	// LeadID identifies the lead that owns the opportunity.
	LeadID string `json:"leadId"`
	// ValueCents is the optional monetary value in the organization's currency, in cents.
	ValueCents *int64 `json:"valueCents"`
	// StatusID identifies the current opportunity stage.
	StatusID string `json:"statusId"`
}

// OpportunityPage contains a bounded page of opportunities.
type OpportunityPage struct {
	// Opportunities contains the returned records.
	Opportunities []Opportunity `json:"opportunities"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"hasMore"`
}

// ListOpportunitiesInput selects a page under one lead.
type ListOpportunitiesInput struct {
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Skip is the zero-based offset.
	Skip int `json:"skip,omitempty"`
	// Limit caps records; zero uses 100.
	Limit int `json:"limit,omitempty"`
}

// GetOpportunityInput identifies a single Close opportunity.
type GetOpportunityInput struct {
	// OpportunityID is the Close oppo_ identifier.
	OpportunityID string `json:"opportunityId"`
}

// CreateOpportunityInput supplies fields for a new opportunity.
type CreateOpportunityInput struct {
	// LeadID identifies an existing lead.
	LeadID string `json:"leadId"`
	// ValueCents is optional monetary value in cents; a present zero sets a zero-value deal.
	ValueCents *int64 `json:"valueCents,omitempty"`
	// StatusID optionally selects an opportunity stage; blank uses Close's first status.
	StatusID string `json:"statusId,omitempty"`
	// ValuePeriod is one_time, monthly, or annual; blank uses one_time.
	ValuePeriod string `json:"valuePeriod,omitempty"`
}

// UpdateOpportunityInput replaces selected fields of one opportunity.
type UpdateOpportunityInput struct {
	// OpportunityID identifies the opportunity to change.
	OpportunityID string `json:"opportunityId"`
	// ValueCents replaces the monetary value when present, including zero.
	ValueCents *int64 `json:"valueCents,omitempty"`
	// StatusID replaces the opportunity stage when present.
	StatusID *string `json:"statusId,omitempty"`
}

// DeleteOpportunityInput identifies one opportunity to remove.
type DeleteOpportunityInput struct {
	// OpportunityID identifies the opportunity to remove.
	OpportunityID string `json:"opportunityId"`
}

// DeletedOpportunity confirms that an opportunity is absent.
type DeletedOpportunity struct {
	// OpportunityID identifies the absent opportunity.
	OpportunityID string `json:"opportunityId"`
}

// ListOpportunitiesOperation reads opportunities under one lead.
type ListOpportunitiesOperation struct{ client *Client }

// ListOpportunities binds this Query to the client.
func (client *Client) ListOpportunities() ListOpportunitiesOperation {
	return ListOpportunitiesOperation{client}
}

// Definition returns the generated listOpportunities contract.
func (ListOpportunitiesOperation) Definition() sdkgo.QueryDefinition {
	return ListOpportunitiesDefinition
}

// Invoke validates the page request and classifies the Close response.
func (operation ListOpportunitiesOperation) Invoke(call sdkgo.Call, input ListOpportunitiesInput) sdkgo.QueryAttempt[OpportunityPage] {
	if !validLeadID(input.LeadID) || input.Skip < 0 || input.Limit < 0 || input.Limit > 250 {
		return failedBranch[OpportunityPage]("listOpportunities", ListOpportunitiesBranchDefect, sdkgo.FailureValidation, "leadId, skip, or limit is invalid")
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	query := url.Values{"lead_id": {input.LeadID}, "_skip": {strconv.Itoa(input.Skip)}, "_limit": {strconv.Itoa(limit)}}
	return invokeGet(operation.client, call, "listOpportunities", "/opportunity/?"+query.Encode(), ListOpportunitiesBranchListed, ListOpportunitiesBranchProviderRejected, decodeOpportunityPage)
}

// GetOpportunityOperation reads one opportunity.
type GetOpportunityOperation struct{ client *Client }

// GetOpportunity binds this Query to the client.
func (client *Client) GetOpportunity() GetOpportunityOperation {
	return GetOpportunityOperation{client}
}

// Definition returns the generated getOpportunity contract.
func (GetOpportunityOperation) Definition() sdkgo.QueryDefinition { return GetOpportunityDefinition }

// Invoke validates the ID and classifies the Close response.
func (operation GetOpportunityOperation) Invoke(call sdkgo.Call, input GetOpportunityInput) sdkgo.QueryAttempt[Opportunity] {
	if !validOpportunityID(input.OpportunityID) {
		return failedBranch[Opportunity]("getOpportunity", GetOpportunityBranchDefect, sdkgo.FailureValidation, "opportunityId is invalid")
	}
	return invokeGet(operation.client, call, "getOpportunity", "/opportunity/"+input.OpportunityID+"/", GetOpportunityBranchFound, GetOpportunityBranchNotFound, decodeOpportunity)
}

// CreateOpportunityOperation creates an opportunity with bounded possible sends.
type CreateOpportunityOperation struct{ client *Client }

// CreateOpportunity binds this Mutation to the client.
func (client *Client) CreateOpportunity() CreateOpportunityOperation {
	return CreateOpportunityOperation{client}
}

// Definition returns the generated createOpportunity contract.
func (CreateOpportunityOperation) Definition() sdkgo.MutationDefinition {
	return CreateOpportunityDefinition
}

// IdempotencyKey correlates attempts; Close does not deduplicate this key.
func (CreateOpportunityOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one opportunity and reconciles a known provider ID before resending.
func (operation CreateOpportunityOperation) Invoke(call sdkgo.Call, input CreateOpportunityInput) sdkgo.MutationAttempt[Opportunity] {
	if !validLeadID(input.LeadID) || (input.ValueCents != nil && *input.ValueCents < 0) || (input.StatusID != "" && !validOpportunityStatusID(input.StatusID)) ||
		(input.ValuePeriod != "" && input.ValuePeriod != "one_time" && input.ValuePeriod != "monthly" && input.ValuePeriod != "annual") {
		return closeMutationBranch[Opportunity]("createOpportunity", CreateOpportunityBranchDefect, sdkgo.FailureValidation, "leadId, valueCents, statusId, or valuePeriod is invalid")
	}
	requestBody, err := json.Marshal(struct {
		LeadID      string `json:"lead_id"`
		Value       *int64 `json:"value,omitempty"`
		StatusID    string `json:"status_id,omitempty"`
		ValuePeriod string `json:"value_period,omitempty"`
	}{input.LeadID, input.ValueCents, input.StatusID, input.ValuePeriod})
	if err != nil {
		return closeMutationBranch[Opportunity]("createOpportunity", CreateOpportunityBranchDefect, sdkgo.FailureLocalDefect, "opportunity request could not be encoded")
	}
	return invokeCloseCreate(operation.client, call, "createOpportunity", "/opportunity/", requestBody, CreateOpportunityBranchCreated, CreateOpportunityBranchProviderRejected, CreateOpportunityBranchDefect, decodeOpportunity, validOpportunityID)
}

// UpdateOpportunityOperation replaces selected opportunity fields.
type UpdateOpportunityOperation struct{ client *Client }

// UpdateOpportunity binds this Mutation to the client.
func (client *Client) UpdateOpportunity() UpdateOpportunityOperation {
	return UpdateOpportunityOperation{client}
}

// Definition returns the generated updateOpportunity contract.
func (UpdateOpportunityOperation) Definition() sdkgo.MutationDefinition {
	return UpdateOpportunityDefinition
}

// IdempotencyKey correlates attempts; absolute field replacement is repeat safe.
func (UpdateOpportunityOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces supplied fields and classifies the Close response.
func (operation UpdateOpportunityOperation) Invoke(call sdkgo.Call, input UpdateOpportunityInput) sdkgo.MutationAttempt[Opportunity] {
	if !validOpportunityID(input.OpportunityID) || (input.ValueCents == nil && input.StatusID == nil) ||
		(input.ValueCents != nil && *input.ValueCents < 0) || (input.StatusID != nil && !validOpportunityStatusID(*input.StatusID)) {
		return closeMutationBranch[Opportunity]("updateOpportunity", UpdateOpportunityBranchDefect, sdkgo.FailureValidation, "opportunityId and at least one valid field are required")
	}
	requestBody, err := json.Marshal(struct {
		Value    *int64  `json:"value,omitempty"`
		StatusID *string `json:"status_id,omitempty"`
	}{input.ValueCents, input.StatusID})
	if err != nil {
		return closeMutationBranch[Opportunity]("updateOpportunity", UpdateOpportunityBranchDefect, sdkgo.FailureLocalDefect, "opportunity update could not be encoded")
	}
	return invokeCloseWrite(operation.client, call, "updateOpportunity", http.MethodPut, "/opportunity/"+input.OpportunityID+"/", requestBody, UpdateOpportunityBranchUpdated, UpdateOpportunityBranchNotFound, decodeOpportunity)
}

// DeleteOpportunityOperation removes one opportunity.
type DeleteOpportunityOperation struct{ client *Client }

// DeleteOpportunity binds this Mutation to the client.
func (client *Client) DeleteOpportunity() DeleteOpportunityOperation {
	return DeleteOpportunityOperation{client}
}

// Definition returns the generated deleteOpportunity contract.
func (DeleteOpportunityOperation) Definition() sdkgo.MutationDefinition {
	return DeleteOpportunityDefinition
}

// IdempotencyKey correlates attempts; deleting an absent opportunity is repeat safe.
func (DeleteOpportunityOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteOpportunityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes an opportunity and classifies absence as completed deletion.
func (operation DeleteOpportunityOperation) Invoke(call sdkgo.Call, input DeleteOpportunityInput) sdkgo.MutationAttempt[DeletedOpportunity] {
	if !validOpportunityID(input.OpportunityID) {
		return closeMutationBranch[DeletedOpportunity]("deleteOpportunity", DeleteOpportunityBranchDefect, sdkgo.FailureValidation, "opportunityId is invalid")
	}
	return invokeCloseWrite(operation.client, call, "deleteOpportunity", http.MethodDelete, "/opportunity/"+input.OpportunityID+"/", nil, DeleteOpportunityBranchDeleted, DeleteOpportunityBranchDeleted, func(body []byte) (DeletedOpportunity, error) {
		var response map[string]json.RawMessage
		if err := json.Unmarshal(body, &response); err != nil || response == nil {
			return DeletedOpportunity{}, errors.New("invalid deletion response")
		}
		return DeletedOpportunity{OpportunityID: input.OpportunityID}, nil
	})
}

func validOpportunityID(opportunityID string) bool {
	return strings.HasPrefix(opportunityID, "oppo_") && safeKey.MatchString(opportunityID)
}
func validOpportunityStatusID(statusID string) bool {
	return strings.HasPrefix(statusID, "stat_") && safeKey.MatchString(statusID)
}

func decodeOpportunity(body []byte) (Opportunity, error) {
	var wire struct {
		ID       string `json:"id"`
		LeadID   string `json:"lead_id"`
		Value    *int64 `json:"value"`
		StatusID string `json:"status_id"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return Opportunity{}, err
	}
	if !validOpportunityID(wire.ID) || !validLeadID(wire.LeadID) || (wire.Value != nil && *wire.Value < 0) || !validOpportunityStatusID(wire.StatusID) {
		return Opportunity{}, fmt.Errorf("incomplete opportunity")
	}
	return Opportunity{ID: wire.ID, LeadID: wire.LeadID, ValueCents: wire.Value, StatusID: wire.StatusID}, nil
}

func decodeOpportunityPage(body []byte) (OpportunityPage, error) {
	var wire struct {
		Data    []json.RawMessage `json:"data"`
		HasMore bool              `json:"has_more"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return OpportunityPage{}, err
	}
	if wire.Data == nil {
		return OpportunityPage{}, errors.New("opportunity page is missing")
	}
	page := OpportunityPage{Opportunities: make([]Opportunity, 0, len(wire.Data)), HasMore: wire.HasMore}
	for _, row := range wire.Data {
		opportunity, err := decodeOpportunity(row)
		if err != nil {
			return OpportunityPage{}, err
		}
		page.Opportunities = append(page.Opportunities, opportunity)
	}
	return page, nil
}
