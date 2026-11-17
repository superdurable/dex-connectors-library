// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"encoding/json"
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// InterviewSummary exposes a reusable interview's identity and type.
type InterviewSummary struct {
	// ID is the Ashby interview UUID.
	ID string `json:"id"`
	// Title is the interview's internal title.
	Title string `json:"title"`
	// Type distinguishes interviews, assessments, take-homes, and debriefs.
	Type string `json:"type"`
	// JobID is the associated job UUID for local interviews; blank means shared.
	JobID string `json:"jobId,omitempty"`
}

// ListInterviewsInput selects a bounded interview page.
type ListInterviewsInput struct {
	// Cursor is an opaque Ashby page cursor; blank starts the first page.
	Cursor string `json:"cursor,omitempty"`
	// Limit caps the page at 100 interviews; zero uses Ashby's default.
	Limit int `json:"limit,omitempty"`
}

// InterviewPage contains interview identities and continuation information.
type InterviewPage struct {
	// Interviews omits instructions and feedback form content.
	Interviews []InterviewSummary `json:"interviews"`
	// NextCursor is passed to the next request when MoreDataAvailable is true.
	NextCursor string `json:"nextCursor,omitempty"`
	// MoreDataAvailable reports whether another page exists.
	MoreDataAvailable bool `json:"moreDataAvailable"`
}

// ListInterviewsOperation reads interview definitions.
type ListInterviewsOperation struct{ client *Client }

// ListInterviews binds the Query to this client.
func (client *Client) ListInterviews() ListInterviewsOperation {
	return ListInterviewsOperation{client}
}

// Definition returns the manifest-generated Query definition.
func (ListInterviewsOperation) Definition() sdkgo.QueryDefinition { return ListInterviewsDefinition }

// Invoke lists interview identities in one bounded page.
func (operation ListInterviewsOperation) Invoke(call sdkgo.Call, input ListInterviewsInput) sdkgo.QueryAttempt[InterviewPage] {
	branches := jobQueryBranches{ListInterviewsBranchListed, ListInterviewsBranchProviderRejected, ListInterviewsBranchInvalidResponse, ListInterviewsBranchDefect}
	if input.Limit < 0 || input.Limit > 100 || len(input.Cursor) > 1024 {
		return jobQueryBranch[InterviewPage]("listInterviews", branches.defect, sdkgo.FailureValidation, "interview page input is invalid")
	}
	return executeJobQuery(operation.client, call, "listInterviews", "/interview.list", input, branches, decodeInterviewPage)
}

// GetInterviewInput identifies one interview definition.
type GetInterviewInput struct {
	// InterviewID is the non-secret Ashby interview UUID.
	InterviewID string `json:"interviewId"`
}

// GetInterviewOperation reads one interview definition.
type GetInterviewOperation struct{ client *Client }

// GetInterview binds the Query to this client.
func (client *Client) GetInterview() GetInterviewOperation { return GetInterviewOperation{client} }

// Definition returns the manifest-generated Query definition.
func (GetInterviewOperation) Definition() sdkgo.QueryDefinition { return GetInterviewDefinition }

// Invoke reads one interview without instructions or form content.
func (operation GetInterviewOperation) Invoke(call sdkgo.Call, input GetInterviewInput) sdkgo.QueryAttempt[InterviewSummary] {
	branches := jobQueryBranches{GetInterviewBranchFound, GetInterviewBranchProviderRejected, GetInterviewBranchInvalidResponse, GetInterviewBranchDefect}
	if !validCandidateID(input.InterviewID) {
		return jobQueryBranch[InterviewSummary]("getInterview", branches.defect, sdkgo.FailureValidation, "interview ID is invalid")
	}
	return executeJobQuery(operation.client, call, "getInterview", "/interview.info", map[string]string{"id": input.InterviewID}, branches, decodeInterviewSummary)
}

// InterviewScheduleSummary exposes schedule state without interviewer details.
type InterviewScheduleSummary struct {
	// ID is the Ashby schedule UUID.
	ID string `json:"id"`
	// ApplicationID is the associated application UUID.
	ApplicationID string `json:"applicationId"`
	// InterviewStageID is the application stage for this schedule.
	InterviewStageID string `json:"interviewStageId"`
	// Status is Ashby's current schedule status.
	Status string `json:"status"`
	// CreatedAt is the schedule creation timestamp.
	CreatedAt string `json:"createdAt"`
	// UpdatedAt is the schedule update timestamp.
	UpdatedAt string `json:"updatedAt"`
}

// ListInterviewSchedulesInput selects schedules for one application.
type ListInterviewSchedulesInput struct {
	// ApplicationID is the application UUID to filter by.
	ApplicationID string `json:"applicationId"`
	// Cursor is an opaque Ashby page cursor; blank starts the first page.
	Cursor string `json:"cursor,omitempty"`
	// Limit caps a page at 100 schedules; zero uses Ashby's default.
	Limit int `json:"limit,omitempty"`
}

// InterviewSchedulePage contains schedules and continuation information.
type InterviewSchedulePage struct {
	// Schedules omits meeting links and interviewer details.
	Schedules []InterviewScheduleSummary `json:"schedules"`
	// NextCursor is passed to the next request when MoreDataAvailable is true.
	NextCursor string `json:"nextCursor,omitempty"`
	// MoreDataAvailable reports whether another page exists.
	MoreDataAvailable bool `json:"moreDataAvailable"`
}

// ListInterviewSchedulesOperation reads schedules for one application.
type ListInterviewSchedulesOperation struct{ client *Client }

// ListInterviewSchedules binds the Query to this client.
func (client *Client) ListInterviewSchedules() ListInterviewSchedulesOperation {
	return ListInterviewSchedulesOperation{client}
}

// Definition returns the manifest-generated Query definition.
func (ListInterviewSchedulesOperation) Definition() sdkgo.QueryDefinition {
	return ListInterviewSchedulesDefinition
}

// Invoke lists schedules in one bounded page.
func (operation ListInterviewSchedulesOperation) Invoke(call sdkgo.Call, input ListInterviewSchedulesInput) sdkgo.QueryAttempt[InterviewSchedulePage] {
	branches := jobQueryBranches{ListInterviewSchedulesBranchListed, ListInterviewSchedulesBranchProviderRejected, ListInterviewSchedulesBranchInvalidResponse, ListInterviewSchedulesBranchDefect}
	if !validCandidateID(input.ApplicationID) || input.Limit < 0 || input.Limit > 100 || len(input.Cursor) > 1024 {
		return jobQueryBranch[InterviewSchedulePage]("listInterviewSchedules", branches.defect, sdkgo.FailureValidation, "schedule page input is invalid")
	}
	return executeJobQuery(operation.client, call, "listInterviewSchedules", "/interviewSchedule.list", input, branches, decodeSchedulePage)
}

// GetInterviewScheduleInput identifies one schedule.
type GetInterviewScheduleInput struct {
	// InterviewScheduleID is the non-secret Ashby schedule UUID.
	InterviewScheduleID string `json:"interviewScheduleId"`
}

// GetInterviewScheduleOperation reads one interview schedule.
type GetInterviewScheduleOperation struct{ client *Client }

// GetInterviewSchedule binds the Query to this client.
func (client *Client) GetInterviewSchedule() GetInterviewScheduleOperation {
	return GetInterviewScheduleOperation{client}
}

// Definition returns the manifest-generated Query definition.
func (GetInterviewScheduleOperation) Definition() sdkgo.QueryDefinition {
	return GetInterviewScheduleDefinition
}

// Invoke reads one schedule without meeting links or interviewer details.
func (operation GetInterviewScheduleOperation) Invoke(call sdkgo.Call, input GetInterviewScheduleInput) sdkgo.QueryAttempt[InterviewScheduleSummary] {
	branches := jobQueryBranches{GetInterviewScheduleBranchFound, GetInterviewScheduleBranchProviderRejected, GetInterviewScheduleBranchInvalidResponse, GetInterviewScheduleBranchDefect}
	if !validCandidateID(input.InterviewScheduleID) {
		return jobQueryBranch[InterviewScheduleSummary]("getInterviewSchedule", branches.defect, sdkgo.FailureValidation, "schedule ID is invalid")
	}
	return executeJobQuery(operation.client, call, "getInterviewSchedule", "/interviewSchedule.info", map[string]string{"id": input.InterviewScheduleID}, branches, decodeScheduleSummary)
}

func decodeInterviewSummary(body []byte) (InterviewSummary, string, error) {
	var wire struct {
		Success   *bool            `json:"success"`
		Results   InterviewSummary `json:"results"`
		ErrorInfo struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return InterviewSummary{}, "", errors.New("invalid interview response")
	}
	if !*wire.Success {
		if wire.ErrorInfo.Code == "" {
			return InterviewSummary{}, "", errors.New("missing interview rejection code")
		}
		return InterviewSummary{}, wire.ErrorInfo.Code, nil
	}
	if !validInterviewSummary(wire.Results) {
		return InterviewSummary{}, "", errors.New("incomplete interview")
	}
	return wire.Results, "", nil
}

func decodeInterviewPage(body []byte) (InterviewPage, string, error) {
	var wire struct {
		Success           *bool              `json:"success"`
		Results           []InterviewSummary `json:"results"`
		NextCursor        string             `json:"nextCursor"`
		MoreDataAvailable bool               `json:"moreDataAvailable"`
		ErrorInfo         struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return InterviewPage{}, "", errors.New("invalid interview page")
	}
	if !*wire.Success {
		if wire.ErrorInfo.Code == "" {
			return InterviewPage{}, "", errors.New("missing interview rejection code")
		}
		return InterviewPage{}, wire.ErrorInfo.Code, nil
	}
	if wire.Results == nil || wire.MoreDataAvailable && wire.NextCursor == "" {
		return InterviewPage{}, "", errors.New("incomplete interview page")
	}
	for _, interview := range wire.Results {
		if !validInterviewSummary(interview) {
			return InterviewPage{}, "", errors.New("invalid interview in page")
		}
	}
	return InterviewPage{Interviews: wire.Results, NextCursor: wire.NextCursor, MoreDataAvailable: wire.MoreDataAvailable}, "", nil
}

func decodeScheduleSummary(body []byte) (InterviewScheduleSummary, string, error) {
	var wire struct {
		Success   *bool                    `json:"success"`
		Results   InterviewScheduleSummary `json:"results"`
		ErrorInfo struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return InterviewScheduleSummary{}, "", errors.New("invalid schedule response")
	}
	if !*wire.Success {
		if wire.ErrorInfo.Code == "" {
			return InterviewScheduleSummary{}, "", errors.New("missing schedule rejection code")
		}
		return InterviewScheduleSummary{}, wire.ErrorInfo.Code, nil
	}
	if !validScheduleSummary(wire.Results) {
		return InterviewScheduleSummary{}, "", errors.New("incomplete schedule")
	}
	return wire.Results, "", nil
}

func decodeSchedulePage(body []byte) (InterviewSchedulePage, string, error) {
	var wire struct {
		Success           *bool                      `json:"success"`
		Results           []InterviewScheduleSummary `json:"results"`
		NextCursor        string                     `json:"nextCursor"`
		MoreDataAvailable bool                       `json:"moreDataAvailable"`
		ErrorInfo         struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return InterviewSchedulePage{}, "", errors.New("invalid schedule page")
	}
	if !*wire.Success {
		if wire.ErrorInfo.Code == "" {
			return InterviewSchedulePage{}, "", errors.New("missing schedule rejection code")
		}
		return InterviewSchedulePage{}, wire.ErrorInfo.Code, nil
	}
	if wire.Results == nil || wire.MoreDataAvailable && wire.NextCursor == "" {
		return InterviewSchedulePage{}, "", errors.New("incomplete schedule page")
	}
	for _, schedule := range wire.Results {
		if !validScheduleSummary(schedule) {
			return InterviewSchedulePage{}, "", errors.New("invalid schedule in page")
		}
	}
	return InterviewSchedulePage{Schedules: wire.Results, NextCursor: wire.NextCursor, MoreDataAvailable: wire.MoreDataAvailable}, "", nil
}

func validInterviewSummary(interview InterviewSummary) bool {
	if !validCandidateID(interview.ID) || interview.Title == "" || interview.JobID != "" && !validCandidateID(interview.JobID) {
		return false
	}
	switch interview.Type {
	case "Assessment", "Interview", "InterviewWithoutSchedule", "TakeHome", "Debrief":
		return true
	}
	return false
}

func validScheduleSummary(schedule InterviewScheduleSummary) bool {
	return validCandidateID(schedule.ID) && validCandidateID(schedule.ApplicationID) && validCandidateID(schedule.InterviewStageID) && schedule.Status != "" && schedule.CreatedAt != "" && schedule.UpdatedAt != ""
}
