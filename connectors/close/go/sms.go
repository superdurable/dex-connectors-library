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

// SMSActivity contains a Close SMS activity.
type SMSActivity struct {
	// ID is the provider-assigned activity identifier.
	ID string `json:"id"`
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Status is the Close delivery state.
	Status string `json:"status"`
	// Text is the message content.
	Text string `json:"text"`
	// LocalPhone is the sender E.164 number.
	LocalPhone string `json:"localPhone"`
	// RemotePhone is the recipient E.164 number.
	RemotePhone string `json:"remotePhone"`
}

// SMSPage contains a bounded page of SMS activities.
type SMSPage struct {
	// Messages contains the returned activities.
	Messages []SMSActivity `json:"messages"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"hasMore"`
}

// ListSMSInput selects a page of SMS activities on one lead.
type ListSMSInput struct {
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Skip is the zero-based offset.
	Skip int `json:"skip,omitempty"`
	// Limit caps messages; zero uses 100.
	Limit int `json:"limit,omitempty"`
}

// GetSMSInput identifies a Close SMS activity.
type GetSMSInput struct {
	// SMSID is the Close acti_ identifier.
	SMSID string `json:"smsId"`
}

// CreateSMSInput creates a draft or sends an outbound SMS from Close.
type CreateSMSInput struct {
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Status is draft or outbox; outbox sends the SMS.
	Status string `json:"status"`
	// LocalPhone is a Close internal E.164 sender number.
	LocalPhone string `json:"localPhone"`
	// RemotePhone is the recipient E.164 number.
	RemotePhone string `json:"remotePhone"`
	// Text is nonempty SMS content.
	Text string `json:"text"`
}

// UpdateSMSDraftInput replaces text on an existing draft without sending it.
type UpdateSMSDraftInput struct {
	// SMSID identifies the draft.
	SMSID string `json:"smsId"`
	// Text replaces the message content.
	Text string `json:"text"`
}

// DeleteSMSInput identifies an SMS activity to remove.
type DeleteSMSInput struct {
	// SMSID identifies the activity.
	SMSID string `json:"smsId"`
}

// DeletedSMS confirms that an SMS activity is absent.
type DeletedSMS struct {
	// SMSID identifies the absent SMS.
	SMSID string `json:"smsId"`
}

// ListSMSOperation reads SMS activities on a lead.
type ListSMSOperation struct{ client *Client }

// ListSMS binds this Query to the client.
func (client *Client) ListSMS() ListSMSOperation { return ListSMSOperation{client} }

// Definition returns the generated listSMS contract.
func (ListSMSOperation) Definition() sdkgo.QueryDefinition { return ListSMSDefinition }

// Invoke validates the page and classifies the Close response.
func (operation ListSMSOperation) Invoke(call sdkgo.Call, input ListSMSInput) sdkgo.QueryAttempt[SMSPage] {
	if !validLeadID(input.LeadID) || input.Skip < 0 || input.Limit < 0 || input.Limit > 100 {
		return failedBranch[SMSPage]("listSMS", ListSMSBranchDefect, sdkgo.FailureValidation, "leadId, skip, or limit is invalid")
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	query := url.Values{"lead_id": {input.LeadID}, "_skip": {strconv.Itoa(input.Skip)}, "_limit": {strconv.Itoa(limit)}}
	return invokeGet(operation.client, call, "listSMS", "/activity/sms/?"+query.Encode(), ListSMSBranchListed, ListSMSBranchProviderRejected, decodeSMSPage)
}

// GetSMSOperation reads one SMS activity.
type GetSMSOperation struct{ client *Client }

// GetSMS binds this Query to the client.
func (client *Client) GetSMS() GetSMSOperation { return GetSMSOperation{client} }

// Definition returns the generated getSMS contract.
func (GetSMSOperation) Definition() sdkgo.QueryDefinition { return GetSMSDefinition }

// Invoke validates the ID and classifies the Close response.
func (operation GetSMSOperation) Invoke(call sdkgo.Call, input GetSMSInput) sdkgo.QueryAttempt[SMSActivity] {
	if !validSMSActivityID(input.SMSID) {
		return failedBranch[SMSActivity]("getSMS", GetSMSBranchDefect, sdkgo.FailureValidation, "smsId is invalid")
	}
	return invokeGet(operation.client, call, "getSMS", "/activity/sms/"+input.SMSID+"/", GetSMSBranchFound, GetSMSBranchNotFound, decodeSMSActivity)
}

// CreateSMSOperation creates a draft or sends an SMS with bounded possible sends.
type CreateSMSOperation struct{ client *Client }

// CreateSMS binds this Mutation to the client.
func (client *Client) CreateSMS() CreateSMSOperation { return CreateSMSOperation{client} }

// Definition returns the generated createSMS contract.
func (CreateSMSOperation) Definition() sdkgo.MutationDefinition { return CreateSMSDefinition }

// IdempotencyKey correlates attempts; Close does not deduplicate this key.
func (CreateSMSOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSMSInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a draft or sends from outbox, reconciling a known provider ID first.
func (operation CreateSMSOperation) Invoke(call sdkgo.Call, input CreateSMSInput) sdkgo.MutationAttempt[SMSActivity] {
	if !validLeadID(input.LeadID) || (input.Status != "draft" && input.Status != "outbox") ||
		!validE164(input.LocalPhone) || !validE164(input.RemotePhone) ||
		strings.TrimSpace(input.Text) == "" || len(input.Text) > 10000 {
		return closeMutationBranch[SMSActivity]("createSMS", CreateSMSBranchDefect, sdkgo.FailureValidation, "leadId, status, phone numbers, or text is invalid")
	}
	requestBody, err := json.Marshal(struct {
		LeadID      string `json:"lead_id"`
		Status      string `json:"status"`
		LocalPhone  string `json:"local_phone"`
		RemotePhone string `json:"remote_phone"`
		Text        string `json:"text"`
	}{input.LeadID, input.Status, input.LocalPhone, input.RemotePhone, input.Text})
	if err != nil {
		return closeMutationBranch[SMSActivity]("createSMS", CreateSMSBranchDefect, sdkgo.FailureLocalDefect, "SMS request could not be encoded")
	}
	return invokeCloseCreate(operation.client, call, "createSMS", "/activity/sms/", requestBody, CreateSMSBranchCreated, CreateSMSBranchProviderRejected, CreateSMSBranchDefect, decodeSMSActivity, validSMSActivityID)
}

// UpdateSMSDraftOperation replaces text without changing draft status.
type UpdateSMSDraftOperation struct{ client *Client }

// UpdateSMSDraft binds this Mutation to the client.
func (client *Client) UpdateSMSDraft() UpdateSMSDraftOperation {
	return UpdateSMSDraftOperation{client}
}

// Definition returns the generated updateSMSDraft contract.
func (UpdateSMSDraftOperation) Definition() sdkgo.MutationDefinition { return UpdateSMSDraftDefinition }

// IdempotencyKey correlates attempts; absolute text replacement is repeat safe.
func (UpdateSMSDraftOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateSMSDraftInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces draft text; Close refuses modifications to sent SMS.
func (operation UpdateSMSDraftOperation) Invoke(call sdkgo.Call, input UpdateSMSDraftInput) sdkgo.MutationAttempt[SMSActivity] {
	if !validSMSActivityID(input.SMSID) || strings.TrimSpace(input.Text) == "" || len(input.Text) > 10000 {
		return closeMutationBranch[SMSActivity]("updateSMSDraft", UpdateSMSDraftBranchDefect, sdkgo.FailureValidation, "smsId or text is invalid")
	}
	requestBody, err := json.Marshal(struct {
		Text string `json:"text"`
	}{input.Text})
	if err != nil {
		return closeMutationBranch[SMSActivity]("updateSMSDraft", UpdateSMSDraftBranchDefect, sdkgo.FailureLocalDefect, "SMS update could not be encoded")
	}
	return invokeCloseWrite(operation.client, call, "updateSMSDraft", http.MethodPut, "/activity/sms/"+input.SMSID+"/", requestBody, UpdateSMSDraftBranchUpdated, UpdateSMSDraftBranchNotFound, decodeSMSActivity)
}

// DeleteSMSOperation removes one SMS activity.
type DeleteSMSOperation struct{ client *Client }

// DeleteSMS binds this Mutation to the client.
func (client *Client) DeleteSMS() DeleteSMSOperation { return DeleteSMSOperation{client} }

// Definition returns the generated deleteSMS contract.
func (DeleteSMSOperation) Definition() sdkgo.MutationDefinition { return DeleteSMSDefinition }

// IdempotencyKey correlates attempts; deleting an absent SMS is repeat safe.
func (DeleteSMSOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteSMSInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the SMS and classifies absence as completed deletion.
func (operation DeleteSMSOperation) Invoke(call sdkgo.Call, input DeleteSMSInput) sdkgo.MutationAttempt[DeletedSMS] {
	if !validSMSActivityID(input.SMSID) {
		return closeMutationBranch[DeletedSMS]("deleteSMS", DeleteSMSBranchDefect, sdkgo.FailureValidation, "smsId is invalid")
	}
	return invokeCloseWrite(operation.client, call, "deleteSMS", http.MethodDelete, "/activity/sms/"+input.SMSID+"/", nil, DeleteSMSBranchDeleted, DeleteSMSBranchDeleted, func(body []byte) (DeletedSMS, error) {
		var response map[string]json.RawMessage
		if err := json.Unmarshal(body, &response); err != nil || response == nil {
			return DeletedSMS{}, errors.New("invalid deletion response")
		}
		return DeletedSMS{SMSID: input.SMSID}, nil
	})
}

func validSMSActivityID(smsID string) bool {
	return strings.HasPrefix(smsID, "acti_") && safeKey.MatchString(smsID)
}

func decodeSMSActivity(body []byte) (SMSActivity, error) {
	var wire struct {
		ID          string `json:"id"`
		LeadID      string `json:"lead_id"`
		Status      string `json:"status"`
		Text        string `json:"text"`
		LocalPhone  string `json:"local_phone"`
		RemotePhone string `json:"remote_phone"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SMSActivity{}, err
	}
	if !validSMSActivityID(wire.ID) || !validLeadID(wire.LeadID) {
		return SMSActivity{}, fmt.Errorf("incomplete SMS activity")
	}
	return SMSActivity{ID: wire.ID, LeadID: wire.LeadID, Status: wire.Status, Text: wire.Text, LocalPhone: wire.LocalPhone, RemotePhone: wire.RemotePhone}, nil
}

func decodeSMSPage(body []byte) (SMSPage, error) {
	var wire struct {
		Data    []json.RawMessage `json:"data"`
		HasMore bool              `json:"has_more"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SMSPage{}, err
	}
	if wire.Data == nil {
		return SMSPage{}, errors.New("SMS page is missing")
	}
	page := SMSPage{Messages: make([]SMSActivity, 0, len(wire.Data)), HasMore: wire.HasMore}
	for _, row := range wire.Data {
		message, err := decodeSMSActivity(row)
		if err != nil {
			return SMSPage{}, err
		}
		page.Messages = append(page.Messages, message)
	}
	return page, nil
}
