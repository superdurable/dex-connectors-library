// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lever

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// InterviewSummary contains non-content scheduling metadata.
type InterviewSummary struct {
	// ID is the Lever interview UID.
	ID string `json:"id"`
	// PanelID is the associated panel UID.
	PanelID string `json:"panelId"`
	// Date is the scheduled time in Unix milliseconds.
	Date int64 `json:"date"`
	// DurationMinutes is the scheduled length in minutes.
	DurationMinutes int `json:"durationMinutes"`
}

// InterviewsPage contains one opportunity's visible interviews.
type InterviewsPage struct {
	// Interviews contains schedule metadata without candidate details.
	Interviews []InterviewSummary `json:"interviews"`
}

// InterviewIdentity identifies a deleted interview.
type InterviewIdentity struct {
	// ID is the Lever interview UID.
	ID string `json:"id"`
}

// GetInterviewInput identifies one interview.
type GetInterviewInput struct {
	// OpportunityID is the candidate opportunity UID.
	OpportunityID string `json:"opportunityId"`
	// InterviewID is the interview UID.
	InterviewID string `json:"interviewId"`
}

// ListInterviewsInput identifies an opportunity.
type ListInterviewsInput struct {
	// OpportunityID is the candidate opportunity UID.
	OpportunityID string `json:"opportunityId"`
}

// CreateInterviewInput schedules an interview on an externally managed panel.
type CreateInterviewInput struct {
	// OpportunityID is the candidate opportunity UID.
	OpportunityID string `json:"opportunityId"`
	// PerformAsUserID is the acting Lever user UID.
	PerformAsUserID string `json:"performAsUserId"`
	// PanelID is an externally managed panel UID.
	PanelID string `json:"panelId"`
	// InterviewerIDs is a nonempty list of Lever user UIDs.
	InterviewerIDs []string `json:"interviewerIds"`
	// Date is the interview time in Unix milliseconds.
	Date int64 `json:"date"`
	// DurationMinutes is the positive interview length in minutes.
	DurationMinutes int `json:"durationMinutes"`
	// Subject is an optional calendar subject.
	Subject string `json:"subject,omitempty"`
	// Note is optional interviewer guidance.
	Note string `json:"note,omitempty"`
	// Location is an optional room or phone location.
	Location string `json:"location,omitempty"`
}

// UpdateInterviewInput completely replaces an externally managed interview.
type UpdateInterviewInput struct {
	// OpportunityID is the candidate opportunity UID.
	OpportunityID string `json:"opportunityId"`
	// InterviewID is the interview UID.
	InterviewID string `json:"interviewId"`
	// PerformAsUserID is the acting Lever user UID.
	PerformAsUserID string `json:"performAsUserId"`
	// PanelID is an externally managed panel UID.
	PanelID string `json:"panelId"`
	// InterviewerIDs is a nonempty list of Lever user UIDs.
	InterviewerIDs []string `json:"interviewerIds"`
	// Date is the interview time in Unix milliseconds.
	Date int64 `json:"date"`
	// DurationMinutes is the positive interview length in minutes.
	DurationMinutes int `json:"durationMinutes"`
	// Subject is an optional calendar subject; blank clears it.
	Subject string `json:"subject,omitempty"`
	// Note is optional interviewer guidance; blank clears it.
	Note string `json:"note,omitempty"`
	// Location is an optional room or phone location; blank clears it.
	Location string `json:"location,omitempty"`
}

// DeleteInterviewInput identifies an externally managed interview to delete.
type DeleteInterviewInput struct {
	// OpportunityID is the candidate opportunity UID.
	OpportunityID string `json:"opportunityId"`
	// InterviewID is the interview UID.
	InterviewID string `json:"interviewId"`
	// PerformAsUserID is the acting Lever user UID.
	PerformAsUserID string `json:"performAsUserId"`
}

// GetInterviewOperation reads schedule metadata for one interview.
type GetInterviewOperation struct{ client *Client }

// GetInterview binds the Query to this client.
func (client *Client) GetInterview() GetInterviewOperation { return GetInterviewOperation{client} }

// Definition returns the generated Query contract.
func (GetInterviewOperation) Definition() sdkgo.QueryDefinition { return GetInterviewDefinition }

// Invoke reads one interview without returning candidate or calendar content.
func (operation GetInterviewOperation) Invoke(call sdkgo.Call, input GetInterviewInput) sdkgo.QueryAttempt[InterviewSummary] {
	if !validOpportunityID(input.OpportunityID) || !validOpportunityID(input.InterviewID) {
		return postingQueryBranch[InterviewSummary]("getInterview", GetInterviewBranchDefect, sdkgo.FailureValidation, "opportunity or interview ID is invalid")
	}
	return readPostingQuery(operation.client.get(call, interviewPath(input.OpportunityID, input.InterviewID)), "getInterview", GetInterviewBranchFound, GetInterviewBranchNotFound, GetInterviewBranchProviderRejected, GetInterviewBranchInvalidResponse, GetInterviewBranchDefect, decodeInterview)
}

// ListInterviewsOperation reads one opportunity's interviews.
type ListInterviewsOperation struct{ client *Client }

// ListInterviews binds the Query to this client.
func (client *Client) ListInterviews() ListInterviewsOperation {
	return ListInterviewsOperation{client}
}

// Definition returns the generated Query contract.
func (ListInterviewsOperation) Definition() sdkgo.QueryDefinition { return ListInterviewsDefinition }

// Invoke lists schedule metadata without returning candidate or calendar content.
func (operation ListInterviewsOperation) Invoke(call sdkgo.Call, input ListInterviewsInput) sdkgo.QueryAttempt[InterviewsPage] {
	if !validOpportunityID(input.OpportunityID) {
		return postingQueryBranch[InterviewsPage]("listInterviews", ListInterviewsBranchDefect, sdkgo.FailureValidation, "opportunity ID is invalid")
	}
	return readPostingQuery(operation.client.get(call, interviewPath(input.OpportunityID, "")), "listInterviews", ListInterviewsBranchListed, "", ListInterviewsBranchProviderRejected, ListInterviewsBranchInvalidResponse, ListInterviewsBranchDefect, decodeInterviews)
}

// CreateInterviewOperation schedules one interview.
type CreateInterviewOperation struct{ client *Client }

// CreateInterview binds the Mutation to this client.
func (client *Client) CreateInterview() CreateInterviewOperation {
	return CreateInterviewOperation{client}
}

// Definition returns the generated Mutation contract.
func (CreateInterviewOperation) Definition() sdkgo.MutationDefinition {
	return CreateInterviewDefinition
}

// IdempotencyKey correlates repeats; Lever does not deduplicate interview creates by this key.
func (CreateInterviewOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateInterviewInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke schedules an interview with bounded sends and a durable dispatch fence.
func (operation CreateInterviewOperation) Invoke(call sdkgo.Call, input CreateInterviewInput) sdkgo.MutationAttempt[InterviewSummary] {
	if !validInterviewWrite(input.OpportunityID, input.PerformAsUserID, input.PanelID, input.InterviewerIDs, input.Date, input.DurationMinutes) {
		return interviewMutationBranch("createInterview", CreateInterviewBranchDefect, sdkgo.FailureValidation, "interview schedule or IDs are invalid", sdkgo.Receipt{})
	}
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "lever"}
	fence, err := singledispatch.NewFence(call, CreateInterviewDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return interviewMutationBranch("createInterview", CreateInterviewBranchDefect, sdkgo.FailureLocalDefect, "interview dispatch checkpoint failed", receipt)
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(InterviewSummary{}, interviewFailure("createInterview", sdkgo.FailureTransport, "interview may exist at the send cap"), receipt)
	}
	if decision.Marker.ProviderID != "" {
		readback := operation.client.get(call, interviewPath(input.OpportunityID, decision.Marker.ProviderID))
		if readback.status == http.StatusOK && readback.err == nil {
			interview, err := decodeInterview(readback.body)
			if err == nil && interview.ID == decision.Marker.ProviderID && interview.PanelID == input.PanelID && interview.Date == input.Date && interview.DurationMinutes == input.DurationMinutes {
				receipt.ProviderObjectID = interview.ID
				return sdkgo.NewMutationBranch(CreateInterviewBranchCreated, interview, nil, receipt)
			}
		}
		return sdkgo.NewMutationRetry[InterviewSummary](interviewFailure("createInterview", sdkgo.FailureAvailability, "created interview read-back unavailable"), readback.retryAfter)
	}
	payload, err := encodeInterviewWrite(input.PanelID, input.InterviewerIDs, input.Date, input.DurationMinutes, input.Subject, input.Note, input.Location)
	if err != nil {
		return interviewMutationBranch("createInterview", CreateInterviewBranchDefect, sdkgo.FailureLocalDefect, "interview request encoding failed", receipt)
	}
	var response providerResponse
	path := interviewPath(input.OpportunityID, "") + "?" + url.Values{"perform_as": {input.PerformAsUserID}}.Encode()
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.doWithContext(call, requestContext, http.MethodPost, path, payload)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[InterviewSummary](interviewFailure("createInterview", sdkgo.FailureAvailability, "interview send claim was not durable"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[InterviewSummary](interviewFailure("createInterview", sdkgo.FailureAvailability, "interview retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[InterviewSummary](interviewFailure("createInterview", kind, "Lever temporarily refused interview creation"), response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(InterviewSummary{}, interviewFailure("createInterview", sdkgo.FailureAuthentication, "credentials unavailable after a possible create"), receipt)
		}
		return interviewMutationBranch("createInterview", CreateInterviewBranchDefect, sdkgo.FailureAuthentication, "Lever credentials unavailable", receipt)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return unknownInterviewCreate(call, decision.MaximumPriorSends, receipt, sdkgo.FailureTransport, "interview create outcome is unknown")
	}
	if response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(InterviewSummary{}, interviewFailure("createInterview", sdkgo.FailureProviderRejection, "Lever rejected a resend after a possible create"), receipt)
		}
		return interviewMutationBranch("createInterview", CreateInterviewBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected interview creation", receipt)
	}
	interview, err := decodeInterviewWrite(response.body)
	if err != nil || interview.PanelID != input.PanelID || interview.Date != input.Date || interview.DurationMinutes != input.DurationMinutes {
		return unknownInterviewCreate(call, decision.MaximumPriorSends, receipt, sdkgo.FailureProtocol, "Lever did not confirm interview schedule")
	}
	if err := fence.RecordProviderID(interview.ID); err != nil {
		return sdkgo.NewMutationRetry[InterviewSummary](interviewFailure("createInterview", sdkgo.FailureAvailability, "interview ID checkpoint failed"), 0)
	}
	receipt.ProviderObjectID = interview.ID
	return sdkgo.NewMutationBranch(CreateInterviewBranchCreated, interview, nil, receipt)
}

// UpdateInterviewOperation replaces one externally managed interview.
type UpdateInterviewOperation struct{ client *Client }

// UpdateInterview binds the Mutation to this client.
func (client *Client) UpdateInterview() UpdateInterviewOperation {
	return UpdateInterviewOperation{client}
}

// Definition returns the generated Mutation contract.
func (UpdateInterviewOperation) Definition() sdkgo.MutationDefinition {
	return UpdateInterviewDefinition
}

// IdempotencyKey correlates absolute replacements.
func (UpdateInterviewOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateInterviewInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the complete interview schedule and checks the returned state.
func (operation UpdateInterviewOperation) Invoke(call sdkgo.Call, input UpdateInterviewInput) sdkgo.MutationAttempt[InterviewSummary] {
	if !validOpportunityID(input.InterviewID) || !validInterviewWrite(input.OpportunityID, input.PerformAsUserID, input.PanelID, input.InterviewerIDs, input.Date, input.DurationMinutes) {
		return interviewMutationBranch("updateInterview", UpdateInterviewBranchDefect, sdkgo.FailureValidation, "interview schedule or IDs are invalid", sdkgo.Receipt{})
	}
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "lever", ProviderObjectID: input.InterviewID}
	payload, err := encodeInterviewWrite(input.PanelID, input.InterviewerIDs, input.Date, input.DurationMinutes, input.Subject, input.Note, input.Location)
	if err != nil {
		return interviewMutationBranch("updateInterview", UpdateInterviewBranchDefect, sdkgo.FailureLocalDefect, "interview replacement encoding failed", receipt)
	}
	path := interviewPath(input.OpportunityID, input.InterviewID) + "?" + url.Values{"perform_as": {input.PerformAsUserID}}.Encode()
	response := operation.client.do(call, http.MethodPut, path, payload)
	if errors.Is(response.err, errCredential) {
		return interviewMutationBranch("updateInterview", UpdateInterviewBranchDefect, sdkgo.FailureAuthentication, "Lever credentials unavailable", receipt)
	}
	if errors.Is(response.err, errRequest) || response.err != nil {
		return sdkgo.NewMutationRetry[InterviewSummary](interviewFailure("updateInterview", sdkgo.FailureTransport, "interview replacement outcome is unknown"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[InterviewSummary](interviewFailure("updateInterview", sdkgo.FailureRateLimit, "Lever rate limited interview replacement"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[InterviewSummary](interviewFailure("updateInterview", sdkgo.FailureAvailability, "interview replacement outcome is unknown"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return interviewMutationBranch("updateInterview", UpdateInterviewBranchNotFound, sdkgo.FailureNotFound, "Lever did not find interview", receipt)
	}
	if response.status != http.StatusCreated && response.status != http.StatusOK {
		return interviewMutationBranch("updateInterview", UpdateInterviewBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected interview replacement", receipt)
	}
	interview, err := decodeInterviewWrite(response.body)
	if err != nil || interview.ID != input.InterviewID || interview.PanelID != input.PanelID || interview.Date != input.Date || interview.DurationMinutes != input.DurationMinutes {
		return sdkgo.NewMutationRetry[InterviewSummary](interviewFailure("updateInterview", sdkgo.FailureProtocol, "Lever did not confirm interview replacement"), 0)
	}
	return sdkgo.NewMutationBranch(UpdateInterviewBranchUpdated, interview, nil, receipt)
}

// DeleteInterviewOperation removes one externally managed interview.
type DeleteInterviewOperation struct{ client *Client }

// DeleteInterview binds the Mutation to this client.
func (client *Client) DeleteInterview() DeleteInterviewOperation {
	return DeleteInterviewOperation{client}
}

// Definition returns the generated Mutation contract.
func (DeleteInterviewOperation) Definition() sdkgo.MutationDefinition {
	return DeleteInterviewDefinition
}

// IdempotencyKey correlates repeated deletes.
func (DeleteInterviewOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteInterviewInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes an interview; a missing interview confirms the requested absent state.
func (operation DeleteInterviewOperation) Invoke(call sdkgo.Call, input DeleteInterviewInput) sdkgo.MutationAttempt[InterviewIdentity] {
	if !validOpportunityID(input.OpportunityID) || !validOpportunityID(input.InterviewID) || !validOpportunityID(input.PerformAsUserID) {
		return interviewDeleteBranch(DeleteInterviewBranchDefect, sdkgo.FailureValidation, "opportunity, interview, or actor ID is invalid", sdkgo.Receipt{})
	}
	receipt := sdkgo.Receipt{CallID: call.ID, Provider: "lever", ProviderObjectID: input.InterviewID}
	path := interviewPath(input.OpportunityID, input.InterviewID) + "?" + url.Values{"perform_as": {input.PerformAsUserID}}.Encode()
	response := operation.client.do(call, http.MethodDelete, path, nil)
	if errors.Is(response.err, errCredential) {
		return interviewDeleteBranch(DeleteInterviewBranchDefect, sdkgo.FailureAuthentication, "Lever credentials unavailable", receipt)
	}
	if errors.Is(response.err, errRequest) || response.err != nil {
		return sdkgo.NewMutationRetry[InterviewIdentity](interviewFailure("deleteInterview", sdkgo.FailureTransport, "interview delete outcome is unknown"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[InterviewIdentity](interviewFailure("deleteInterview", sdkgo.FailureRateLimit, "Lever rate limited interview deletion"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[InterviewIdentity](interviewFailure("deleteInterview", sdkgo.FailureAvailability, "interview delete outcome is unknown"), response.retryAfter)
	}
	if response.status == http.StatusNoContent || response.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteInterviewBranchDeleted, InterviewIdentity{ID: input.InterviewID}, nil, receipt)
	}
	return interviewDeleteBranch(DeleteInterviewBranchProviderRejected, sdkgo.FailureProviderRejection, "Lever rejected interview deletion", receipt)
}

func interviewPath(opportunityID, interviewID string) string {
	path := "/opportunities/" + url.PathEscape(opportunityID) + "/interviews"
	if interviewID != "" {
		path += "/" + url.PathEscape(interviewID)
	}
	return path
}

func validInterviewWrite(opportunityID, actorID, panelID string, interviewerIDs []string, date int64, durationMinutes int) bool {
	if !validOpportunityID(opportunityID) || !validOpportunityID(actorID) || !validOpportunityID(panelID) || len(interviewerIDs) == 0 || date <= 0 || durationMinutes <= 0 {
		return false
	}
	for _, interviewerID := range interviewerIDs {
		if !validOpportunityID(interviewerID) {
			return false
		}
	}
	return true
}

func encodeInterviewWrite(panelID string, interviewerIDs []string, date int64, durationMinutes int, subject, note, location string) ([]byte, error) {
	type interviewer struct {
		ID string `json:"id"`
	}
	payload := struct {
		Panel        string        `json:"panel"`
		Interviewers []interviewer `json:"interviewers"`
		Date         int64         `json:"date"`
		Duration     int           `json:"duration"`
		Subject      string        `json:"subject,omitempty"`
		Note         string        `json:"note,omitempty"`
		Location     string        `json:"location,omitempty"`
	}{Panel: panelID, Date: date, Duration: durationMinutes, Subject: subject, Note: note, Location: location}
	for _, id := range interviewerIDs {
		payload.Interviewers = append(payload.Interviewers, interviewer{ID: id})
	}
	return json.Marshal(payload)
}

func decodeInterview(body []byte) (InterviewSummary, error) {
	var wire struct {
		Data struct {
			ID       string `json:"id"`
			Panel    string `json:"panel"`
			Date     int64  `json:"date"`
			Duration int    `json:"duration"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || !validOpportunityID(wire.Data.ID) || !validOpportunityID(wire.Data.Panel) || wire.Data.Date <= 0 || wire.Data.Duration <= 0 {
		return InterviewSummary{}, fmt.Errorf("Lever interview JSON invalid")
	}
	return InterviewSummary{ID: wire.Data.ID, PanelID: wire.Data.Panel, Date: wire.Data.Date, DurationMinutes: wire.Data.Duration}, nil
}
func decodeInterviews(body []byte) (InterviewsPage, error) {
	var wire struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Data == nil {
		return InterviewsPage{}, fmt.Errorf("Lever interviews JSON invalid")
	}
	page := InterviewsPage{Interviews: make([]InterviewSummary, 0, len(wire.Data))}
	for _, entry := range wire.Data {
		interview, err := decodeInterview(append([]byte(`{"data":`), append(entry, '}')...))
		if err != nil {
			return InterviewsPage{}, err
		}
		page.Interviews = append(page.Interviews, interview)
	}
	return page, nil
}
func decodeInterviewWrite(body []byte) (InterviewSummary, error) {
	var wire struct {
		Data struct {
			Data []json.RawMessage `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || len(wire.Data.Data) != 1 {
		return InterviewSummary{}, fmt.Errorf("Lever interview write JSON invalid")
	}
	return decodeInterview(append([]byte(`{"data":`), append(wire.Data.Data[0], '}')...))
}
func unknownInterviewCreate(call sdkgo.Call, priorSends int32, receipt sdkgo.Receipt, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[InterviewSummary] {
	if priorSends+1 < call.MaxSends() {
		return sdkgo.NewMutationRetry[InterviewSummary](interviewFailure("createInterview", kind, message), 0)
	}
	return sdkgo.NewMutationUncertain(InterviewSummary{}, interviewFailure("createInterview", kind, message+" at the send cap"), receipt)
}
func interviewFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "lever", Operation: operation, Message: message}
}
func interviewMutationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[InterviewSummary] {
	failure := interviewFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, InterviewSummary{}, &failure, receipt)
}
func interviewDeleteBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) sdkgo.MutationAttempt[InterviewIdentity] {
	failure := interviewFailure("deleteInterview", kind, message)
	return sdkgo.NewMutationBranch(branch, InterviewIdentity{}, &failure, receipt)
}
