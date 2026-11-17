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

// CallActivity identifies a recorded Close call and its result.
type CallActivity struct {
	// ID is the provider-assigned activity identifier.
	ID string `json:"id"`
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Direction is inbound or outbound.
	Direction string `json:"direction"`
	// Status is the provider's call state.
	Status string `json:"status"`
	// DurationSeconds is the optional call duration in seconds.
	DurationSeconds *int `json:"durationSeconds"`
	// Note is the plaintext call note.
	Note string `json:"note"`
}

// CallPage contains a bounded page of call activities.
type CallPage struct {
	// Calls contains the returned call activities.
	Calls []CallActivity `json:"calls"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"hasMore"`
}

// ListCallsInput selects a page of calls on one lead.
type ListCallsInput struct {
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Skip is the zero-based offset.
	Skip int `json:"skip,omitempty"`
	// Limit caps calls; zero uses 100.
	Limit int `json:"limit,omitempty"`
}

// GetCallInput identifies a single Close call activity.
type GetCallInput struct {
	// CallID is the Close acti_ identifier.
	CallID string `json:"callId"`
}

// LogCallInput supplies an external call to record on a lead.
type LogCallInput struct {
	// LeadID identifies the existing lead.
	LeadID string `json:"leadId"`
	// Direction is inbound or outbound.
	Direction string `json:"direction"`
	// DurationSeconds is the duration in seconds, from zero through 604800.
	DurationSeconds int `json:"durationSeconds"`
	// Phone is an optional E.164 number, such as +16505551234.
	Phone string `json:"phone,omitempty"`
	// Note is optional plaintext context for the logged call.
	Note string `json:"note,omitempty"`
}

// UpdateCallInput replaces selected call fields.
type UpdateCallInput struct {
	// CallID identifies the call activity to change.
	CallID string `json:"callId"`
	// Note replaces the plaintext note when present.
	Note *string `json:"note,omitempty"`
	// OutcomeID replaces the Close call outcome when present.
	OutcomeID *string `json:"outcomeId,omitempty"`
}

// DeleteCallInput identifies a call activity to remove.
type DeleteCallInput struct {
	// CallID identifies the call activity to remove.
	CallID string `json:"callId"`
}

// DeletedCall confirms that a call activity is absent.
type DeletedCall struct {
	// CallID identifies the absent call.
	CallID string `json:"callId"`
}

// ListCallsOperation reads call activities on one lead.
type ListCallsOperation struct{ client *Client }

// ListCalls binds this Query to the client.
func (client *Client) ListCalls() ListCallsOperation { return ListCallsOperation{client} }

// Definition returns the generated listCalls contract.
func (ListCallsOperation) Definition() sdkgo.QueryDefinition { return ListCallsDefinition }

// Invoke validates pagination and classifies the Close response.
func (operation ListCallsOperation) Invoke(call sdkgo.Call, input ListCallsInput) sdkgo.QueryAttempt[CallPage] {
	if !validLeadID(input.LeadID) || input.Skip < 0 || input.Limit < 0 || input.Limit > 100 {
		return failedBranch[CallPage]("listCalls", ListCallsBranchDefect, sdkgo.FailureValidation, "leadId, skip, or limit is invalid")
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	query := url.Values{"lead_id": {input.LeadID}, "_skip": {strconv.Itoa(input.Skip)}, "_limit": {strconv.Itoa(limit)}}
	return invokeGet(operation.client, call, "listCalls", "/activity/call/?"+query.Encode(), ListCallsBranchListed, ListCallsBranchProviderRejected, decodeCallPage)
}

// GetCallOperation reads one call activity.
type GetCallOperation struct{ client *Client }

// GetCall binds this Query to the client.
func (client *Client) GetCall() GetCallOperation { return GetCallOperation{client} }

// Definition returns the generated getCall contract.
func (GetCallOperation) Definition() sdkgo.QueryDefinition { return GetCallDefinition }

// Invoke validates the ID and classifies the Close response.
func (operation GetCallOperation) Invoke(call sdkgo.Call, input GetCallInput) sdkgo.QueryAttempt[CallActivity] {
	if !validCallActivityID(input.CallID) {
		return failedBranch[CallActivity]("getCall", GetCallBranchDefect, sdkgo.FailureValidation, "callId is invalid")
	}
	return invokeGet(operation.client, call, "getCall", "/activity/call/"+input.CallID+"/", GetCallBranchFound, GetCallBranchNotFound, decodeCallActivity)
}

// LogCallOperation records an external call with bounded possible sends.
type LogCallOperation struct{ client *Client }

// LogCall binds this Mutation to the client.
func (client *Client) LogCall() LogCallOperation { return LogCallOperation{client} }

// Definition returns the generated logCall contract.
func (LogCallOperation) Definition() sdkgo.MutationDefinition { return LogCallDefinition }

// IdempotencyKey correlates attempts; Close does not deduplicate this key.
func (LogCallOperation) IdempotencyKey(callID sdkgo.CallID, _ LogCallInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke logs an external call and reconciles a known provider ID before resending.
func (operation LogCallOperation) Invoke(call sdkgo.Call, input LogCallInput) sdkgo.MutationAttempt[CallActivity] {
	if !validLeadID(input.LeadID) || (input.Direction != "inbound" && input.Direction != "outbound") || input.DurationSeconds < 0 || input.DurationSeconds > 604800 ||
		(input.Phone != "" && !validE164(input.Phone)) || len(input.Note) > 100000 {
		return closeMutationBranch[CallActivity]("logCall", LogCallBranchDefect, sdkgo.FailureValidation, "leadId, direction, durationSeconds, phone, or note is invalid")
	}
	requestBody, err := json.Marshal(struct {
		LeadID    string `json:"lead_id"`
		Direction string `json:"direction"`
		Duration  int    `json:"duration"`
		Phone     string `json:"phone,omitempty"`
		Note      string `json:"note,omitempty"`
	}{input.LeadID, input.Direction, input.DurationSeconds, input.Phone, input.Note})
	if err != nil {
		return closeMutationBranch[CallActivity]("logCall", LogCallBranchDefect, sdkgo.FailureLocalDefect, "call request could not be encoded")
	}
	return invokeCloseCreate(operation.client, call, "logCall", "/activity/call/", requestBody, LogCallBranchLogged, LogCallBranchProviderRejected, LogCallBranchDefect, decodeCallActivity, validCallActivityID)
}

// UpdateCallOperation replaces selected call fields.
type UpdateCallOperation struct{ client *Client }

// UpdateCall binds this Mutation to the client.
func (client *Client) UpdateCall() UpdateCallOperation { return UpdateCallOperation{client} }

// Definition returns the generated updateCall contract.
func (UpdateCallOperation) Definition() sdkgo.MutationDefinition { return UpdateCallDefinition }

// IdempotencyKey correlates attempts; absolute field replacement is repeat safe.
func (UpdateCallOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCallInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces selected call fields and classifies the Close response.
func (operation UpdateCallOperation) Invoke(call sdkgo.Call, input UpdateCallInput) sdkgo.MutationAttempt[CallActivity] {
	if !validCallActivityID(input.CallID) || (input.Note == nil && input.OutcomeID == nil) ||
		(input.Note != nil && len(*input.Note) > 100000) ||
		(input.OutcomeID != nil && !validCallOutcomeID(*input.OutcomeID)) {
		return closeMutationBranch[CallActivity]("updateCall", UpdateCallBranchDefect, sdkgo.FailureValidation, "callId and at least one valid field are required")
	}
	requestBody, err := json.Marshal(struct {
		Note      *string `json:"note,omitempty"`
		OutcomeID *string `json:"outcome_id,omitempty"`
	}{input.Note, input.OutcomeID})
	if err != nil {
		return closeMutationBranch[CallActivity]("updateCall", UpdateCallBranchDefect, sdkgo.FailureLocalDefect, "call update could not be encoded")
	}
	return invokeCloseWrite(operation.client, call, "updateCall", http.MethodPut, "/activity/call/"+input.CallID+"/", requestBody, UpdateCallBranchUpdated, UpdateCallBranchNotFound, decodeCallActivity)
}

// DeleteCallOperation removes one call activity.
type DeleteCallOperation struct{ client *Client }

// DeleteCall binds this Mutation to the client.
func (client *Client) DeleteCall() DeleteCallOperation { return DeleteCallOperation{client} }

// Definition returns the generated deleteCall contract.
func (DeleteCallOperation) Definition() sdkgo.MutationDefinition { return DeleteCallDefinition }

// IdempotencyKey correlates attempts; deleting an absent call is repeat safe.
func (DeleteCallOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteCallInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes a call and classifies absence as completed deletion.
func (operation DeleteCallOperation) Invoke(call sdkgo.Call, input DeleteCallInput) sdkgo.MutationAttempt[DeletedCall] {
	if !validCallActivityID(input.CallID) {
		return closeMutationBranch[DeletedCall]("deleteCall", DeleteCallBranchDefect, sdkgo.FailureValidation, "callId is invalid")
	}
	return invokeCloseWrite(operation.client, call, "deleteCall", http.MethodDelete, "/activity/call/"+input.CallID+"/", nil, DeleteCallBranchDeleted, DeleteCallBranchDeleted, func(body []byte) (DeletedCall, error) {
		var response map[string]json.RawMessage
		if err := json.Unmarshal(body, &response); err != nil || response == nil {
			return DeletedCall{}, errors.New("invalid deletion response")
		}
		return DeletedCall{CallID: input.CallID}, nil
	})
}

func validCallActivityID(callID string) bool {
	return strings.HasPrefix(callID, "acti_") && safeKey.MatchString(callID)
}
func validCallOutcomeID(outcomeID string) bool {
	return strings.HasPrefix(outcomeID, "outcome_") && safeKey.MatchString(outcomeID)
}
func validE164(phone string) bool {
	if len(phone) < 3 || len(phone) > 16 || phone[0] != '+' || phone[1] < '1' || phone[1] > '9' {
		return false
	}
	for index := 2; index < len(phone); index++ {
		if phone[index] < '0' || phone[index] > '9' {
			return false
		}
	}
	return true
}

func decodeCallActivity(body []byte) (CallActivity, error) {
	var wire struct {
		ID        string `json:"id"`
		LeadID    string `json:"lead_id"`
		Direction string `json:"direction"`
		Status    string `json:"status"`
		Duration  *int   `json:"duration"`
		Note      string `json:"note"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CallActivity{}, err
	}
	if !validCallActivityID(wire.ID) || !validLeadID(wire.LeadID) {
		return CallActivity{}, fmt.Errorf("incomplete call activity")
	}
	return CallActivity{ID: wire.ID, LeadID: wire.LeadID, Direction: wire.Direction, Status: wire.Status, DurationSeconds: wire.Duration, Note: wire.Note}, nil
}

func decodeCallPage(body []byte) (CallPage, error) {
	var wire struct {
		Data    []json.RawMessage `json:"data"`
		HasMore bool              `json:"has_more"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CallPage{}, err
	}
	if wire.Data == nil {
		return CallPage{}, errors.New("call page is missing")
	}
	page := CallPage{Calls: make([]CallActivity, 0, len(wire.Data)), HasMore: wire.HasMore}
	for _, row := range wire.Data {
		call, err := decodeCallActivity(row)
		if err != nil {
			return CallPage{}, err
		}
		page.Calls = append(page.Calls, call)
	}
	return page, nil
}
