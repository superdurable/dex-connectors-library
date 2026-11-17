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

// MeetingActivity contains a Close meeting and its user supplied note.
type MeetingActivity struct {
	// ID is the provider-assigned activity identifier.
	ID string `json:"id"`
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Title is the meeting title.
	Title string `json:"title"`
	// Status is the provider's meeting state.
	Status string `json:"status"`
	// StartsAt is the provider timestamp for the meeting start.
	StartsAt string `json:"startsAt"`
	// EndsAt is the provider timestamp for the meeting end.
	EndsAt string `json:"endsAt"`
	// UserNoteHTML is the rich text note on the meeting.
	UserNoteHTML string `json:"userNoteHTML"`
}

// MeetingPage contains a bounded page of meeting activities.
type MeetingPage struct {
	// Meetings contains the returned activities.
	Meetings []MeetingActivity `json:"meetings"`
	// HasMore reports whether another page exists.
	HasMore bool `json:"hasMore"`
}

// ListMeetingsInput selects a page of meetings on one lead.
type ListMeetingsInput struct {
	// LeadID identifies the parent lead.
	LeadID string `json:"leadId"`
	// Skip is the zero-based offset.
	Skip int `json:"skip,omitempty"`
	// Limit caps meetings; zero uses 100.
	Limit int `json:"limit,omitempty"`
}

// GetMeetingInput identifies a Close meeting activity.
type GetMeetingInput struct {
	// MeetingID is the Close acti_ identifier.
	MeetingID string `json:"meetingId"`
}

// UpdateMeetingInput replaces selected fields on a meeting activity.
type UpdateMeetingInput struct {
	// MeetingID identifies the activity.
	MeetingID string `json:"meetingId"`
	// UserNoteHTML replaces the rich text note when present; empty clears it.
	UserNoteHTML *string `json:"userNoteHTML,omitempty"`
	// OutcomeID replaces the Close meeting outcome when present.
	OutcomeID *string `json:"outcomeId,omitempty"`
}

// DeleteMeetingInput identifies a meeting activity to remove.
type DeleteMeetingInput struct {
	// MeetingID identifies the activity.
	MeetingID string `json:"meetingId"`
}

// DeletedMeeting confirms that a meeting activity is absent.
type DeletedMeeting struct {
	// MeetingID identifies the absent meeting.
	MeetingID string `json:"meetingId"`
}

// ListMeetingsOperation reads meeting activities on a lead.
type ListMeetingsOperation struct{ client *Client }

// ListMeetings binds this Query to the client.
func (client *Client) ListMeetings() ListMeetingsOperation { return ListMeetingsOperation{client} }

// Definition returns the generated listMeetings contract.
func (ListMeetingsOperation) Definition() sdkgo.QueryDefinition { return ListMeetingsDefinition }

// Invoke validates the page and classifies the Close response.
func (operation ListMeetingsOperation) Invoke(call sdkgo.Call, input ListMeetingsInput) sdkgo.QueryAttempt[MeetingPage] {
	if !validLeadID(input.LeadID) || input.Skip < 0 || input.Limit < 0 || input.Limit > 100 {
		return failedBranch[MeetingPage]("listMeetings", ListMeetingsBranchDefect, sdkgo.FailureValidation, "leadId, skip, or limit is invalid")
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	query := url.Values{"lead_id": {input.LeadID}, "_skip": {strconv.Itoa(input.Skip)}, "_limit": {strconv.Itoa(limit)}}
	return invokeGet(operation.client, call, "listMeetings", "/activity/meeting/?"+query.Encode(), ListMeetingsBranchListed, ListMeetingsBranchProviderRejected, decodeMeetingPage)
}

// GetMeetingOperation reads one meeting activity.
type GetMeetingOperation struct{ client *Client }

// GetMeeting binds this Query to the client.
func (client *Client) GetMeeting() GetMeetingOperation { return GetMeetingOperation{client} }

// Definition returns the generated getMeeting contract.
func (GetMeetingOperation) Definition() sdkgo.QueryDefinition { return GetMeetingDefinition }

// Invoke validates the ID and classifies the Close response.
func (operation GetMeetingOperation) Invoke(call sdkgo.Call, input GetMeetingInput) sdkgo.QueryAttempt[MeetingActivity] {
	if !validMeetingActivityID(input.MeetingID) {
		return failedBranch[MeetingActivity]("getMeeting", GetMeetingBranchDefect, sdkgo.FailureValidation, "meetingId is invalid")
	}
	return invokeGet(operation.client, call, "getMeeting", "/activity/meeting/"+input.MeetingID+"/", GetMeetingBranchFound, GetMeetingBranchNotFound, decodeMeetingActivity)
}

// UpdateMeetingOperation replaces meeting note or outcome fields.
type UpdateMeetingOperation struct{ client *Client }

// UpdateMeeting binds this Mutation to the client.
func (client *Client) UpdateMeeting() UpdateMeetingOperation { return UpdateMeetingOperation{client} }

// Definition returns the generated updateMeeting contract.
func (UpdateMeetingOperation) Definition() sdkgo.MutationDefinition { return UpdateMeetingDefinition }

// IdempotencyKey correlates attempts; absolute field replacement is repeat safe.
func (UpdateMeetingOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateMeetingInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the user note or outcome and classifies the Close response.
func (operation UpdateMeetingOperation) Invoke(call sdkgo.Call, input UpdateMeetingInput) sdkgo.MutationAttempt[MeetingActivity] {
	if !validMeetingActivityID(input.MeetingID) || (input.UserNoteHTML == nil && input.OutcomeID == nil) ||
		(input.UserNoteHTML != nil && len(*input.UserNoteHTML) > 100000) ||
		(input.OutcomeID != nil && !validCallOutcomeID(*input.OutcomeID)) {
		return closeMutationBranch[MeetingActivity]("updateMeeting", UpdateMeetingBranchDefect, sdkgo.FailureValidation, "meetingId and at least one valid field are required")
	}
	requestBody, err := json.Marshal(struct {
		UserNoteHTML *string `json:"user_note_html,omitempty"`
		OutcomeID    *string `json:"outcome_id,omitempty"`
	}{input.UserNoteHTML, input.OutcomeID})
	if err != nil {
		return closeMutationBranch[MeetingActivity]("updateMeeting", UpdateMeetingBranchDefect, sdkgo.FailureLocalDefect, "meeting update could not be encoded")
	}
	return invokeCloseWrite(operation.client, call, "updateMeeting", http.MethodPut, "/activity/meeting/"+input.MeetingID+"/", requestBody, UpdateMeetingBranchUpdated, UpdateMeetingBranchNotFound, decodeMeetingActivity)
}

// DeleteMeetingOperation removes one meeting activity.
type DeleteMeetingOperation struct{ client *Client }

// DeleteMeeting binds this Mutation to the client.
func (client *Client) DeleteMeeting() DeleteMeetingOperation { return DeleteMeetingOperation{client} }

// Definition returns the generated deleteMeeting contract.
func (DeleteMeetingOperation) Definition() sdkgo.MutationDefinition { return DeleteMeetingDefinition }

// IdempotencyKey correlates attempts; deleting an absent meeting is repeat safe.
func (DeleteMeetingOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteMeetingInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the meeting and classifies absence as completed deletion.
func (operation DeleteMeetingOperation) Invoke(call sdkgo.Call, input DeleteMeetingInput) sdkgo.MutationAttempt[DeletedMeeting] {
	if !validMeetingActivityID(input.MeetingID) {
		return closeMutationBranch[DeletedMeeting]("deleteMeeting", DeleteMeetingBranchDefect, sdkgo.FailureValidation, "meetingId is invalid")
	}
	return invokeCloseWrite(operation.client, call, "deleteMeeting", http.MethodDelete, "/activity/meeting/"+input.MeetingID+"/", nil, DeleteMeetingBranchDeleted, DeleteMeetingBranchDeleted, func(body []byte) (DeletedMeeting, error) {
		var response map[string]json.RawMessage
		if err := json.Unmarshal(body, &response); err != nil || response == nil {
			return DeletedMeeting{}, errors.New("invalid deletion response")
		}
		return DeletedMeeting{MeetingID: input.MeetingID}, nil
	})
}

func validMeetingActivityID(meetingID string) bool {
	return strings.HasPrefix(meetingID, "acti_") && safeKey.MatchString(meetingID)
}

func decodeMeetingActivity(body []byte) (MeetingActivity, error) {
	var wire struct {
		ID           string `json:"id"`
		LeadID       string `json:"lead_id"`
		Title        string `json:"title"`
		Status       string `json:"status"`
		StartsAt     string `json:"starts_at"`
		EndsAt       string `json:"ends_at"`
		UserNoteHTML string `json:"user_note_html"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return MeetingActivity{}, err
	}
	if !validMeetingActivityID(wire.ID) || !validLeadID(wire.LeadID) {
		return MeetingActivity{}, fmt.Errorf("incomplete meeting activity")
	}
	return MeetingActivity{ID: wire.ID, LeadID: wire.LeadID, Title: wire.Title, Status: wire.Status, StartsAt: wire.StartsAt, EndsAt: wire.EndsAt, UserNoteHTML: wire.UserNoteHTML}, nil
}

func decodeMeetingPage(body []byte) (MeetingPage, error) {
	var wire struct {
		Data    []json.RawMessage `json:"data"`
		HasMore bool              `json:"has_more"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return MeetingPage{}, err
	}
	if wire.Data == nil {
		return MeetingPage{}, errors.New("meeting page is missing")
	}
	page := MeetingPage{Meetings: make([]MeetingActivity, 0, len(wire.Data)), HasMore: wire.HasMore}
	for _, row := range wire.Data {
		meeting, err := decodeMeetingActivity(row)
		if err != nil {
			return MeetingPage{}, err
		}
		page.Meetings = append(page.Meetings, meeting)
	}
	return page, nil
}
