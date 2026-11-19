// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"encoding/json"
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ApplicationFeedbackSummary exposes feedback identity without answers or users.
type ApplicationFeedbackSummary struct {
	// ID is the Ashby feedback UUID.
	ID string `json:"id"`
	// ApplicationID is the associated application UUID.
	ApplicationID string `json:"applicationId"`
	// InterviewID is the associated interview definition UUID.
	InterviewID string `json:"interviewId"`
	// InterviewEventID is the scheduled event UUID.
	InterviewEventID string `json:"interviewEventId"`
	// SubmittedAt is the Ashby submission timestamp.
	SubmittedAt string `json:"submittedAt"`
}

// ListApplicationFeedbackInput selects feedback for an application.
type ListApplicationFeedbackInput struct {
	// ApplicationID is the application UUID.
	ApplicationID string `json:"applicationId"`
	// Cursor is the opaque Ashby continuation cursor; blank starts the first page.
	Cursor string `json:"cursor,omitempty"`
	// Limit caps the page at 100 submissions; zero uses Ashby's default.
	Limit int `json:"limit,omitempty"`
}

// ApplicationFeedbackPage contains submission identities and continuation state.
type ApplicationFeedbackPage struct {
	// Feedback excludes answers, users, and form definitions.
	Feedback []ApplicationFeedbackSummary `json:"feedback"`
	// NextCursor is passed to the next request when MoreDataAvailable is true.
	NextCursor string `json:"nextCursor,omitempty"`
	// MoreDataAvailable reports whether another page exists.
	MoreDataAvailable bool `json:"moreDataAvailable"`
}

// ListApplicationFeedbackOperation reads a page of feedback identities.
type ListApplicationFeedbackOperation struct{ client *Client }

// ListApplicationFeedback binds the Query to this client.
func (client *Client) ListApplicationFeedback() ListApplicationFeedbackOperation {
	return ListApplicationFeedbackOperation{client}
}

// Definition returns the manifest-generated Query definition.
func (ListApplicationFeedbackOperation) Definition() sdkgo.QueryDefinition {
	return ListApplicationFeedbackDefinition
}

// Invoke lists feedback identities without exposing submitted answers.
func (operation ListApplicationFeedbackOperation) Invoke(call sdkgo.Call, input ListApplicationFeedbackInput) sdkgo.QueryAttempt[ApplicationFeedbackPage] {
	branches := jobQueryBranches{ListApplicationFeedbackBranchListed, ListApplicationFeedbackBranchProviderRejected, ListApplicationFeedbackBranchInvalidResponse, ListApplicationFeedbackBranchDefect}
	if !validCandidateID(input.ApplicationID) || input.Limit < 0 || input.Limit > 100 || len(input.Cursor) > 1024 {
		return jobQueryBranch[ApplicationFeedbackPage]("listApplicationFeedback", branches.defect, sdkgo.FailureValidation, "feedback page input is invalid")
	}
	return executeJobQuery(operation.client, call, "listApplicationFeedback", "/applicationFeedback.list", input, branches, decodeApplicationFeedbackPage)
}

func decodeApplicationFeedbackPage(body []byte) (ApplicationFeedbackPage, string, error) {
	var wire struct {
		Success           *bool                        `json:"success"`
		Results           []ApplicationFeedbackSummary `json:"results"`
		NextCursor        string                       `json:"nextCursor"`
		MoreDataAvailable bool                         `json:"moreDataAvailable"`
		ErrorInfo         struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return ApplicationFeedbackPage{}, "", errors.New("invalid feedback page")
	}
	if !*wire.Success {
		if wire.ErrorInfo.Code == "" {
			return ApplicationFeedbackPage{}, "", errors.New("missing feedback rejection code")
		}
		return ApplicationFeedbackPage{}, wire.ErrorInfo.Code, nil
	}
	if wire.Results == nil || wire.MoreDataAvailable && wire.NextCursor == "" {
		return ApplicationFeedbackPage{}, "", errors.New("incomplete feedback page")
	}
	for _, feedback := range wire.Results {
		if !validCandidateID(feedback.ID) || !validCandidateID(feedback.ApplicationID) || feedback.InterviewID != "" && !validCandidateID(feedback.InterviewID) || feedback.InterviewEventID != "" && !validCandidateID(feedback.InterviewEventID) || feedback.SubmittedAt == "" {
			return ApplicationFeedbackPage{}, "", errors.New("invalid feedback in page")
		}
	}
	return ApplicationFeedbackPage{Feedback: wire.Results, NextCursor: wire.NextCursor, MoreDataAvailable: wire.MoreDataAvailable}, "", nil
}
