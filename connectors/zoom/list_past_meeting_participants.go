// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const listPastMeetingParticipantsOperationID = "listPastMeetingParticipants"

var errParticipantResponseInvalid = errors.New("Zoom returned an unusable participant page")

// ListPastMeetingParticipantsInput selects one page of a past meeting's participants.
type ListPastMeetingParticipantsInput struct {
	// MeetingID is Zoom's numeric meeting ID; Zoom answers for its latest ended instance.
	MeetingID int64 `json:"meetingId"`
	// PageSize is the number of participants per page, from 1 to 300; zero uses Zoom's default of 30.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the NextPageToken of the previous page; Zoom expires it after 15 minutes.
	PageToken string `json:"pageToken,omitempty"`
}

// ParticipantPage is one page of a past meeting's participants. Zoom lists a
// person once per join, so a participant who rejoined appears more than once.
type ParticipantPage struct {
	// MeetingID is the meeting whose latest ended instance was listed.
	MeetingID int64 `json:"meetingId"`
	// Participants lists the page's participants.
	Participants []MeetingParticipant `json:"participants"`
	// NextPageToken requests the next page; blank means this is the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
	// TotalRecords is Zoom's count of participants across every page.
	TotalRecords int `json:"totalRecords"`
}

// MeetingParticipant is one join of a past meeting.
type MeetingParticipant struct {
	// ID is the participant's Zoom user ID when they joined signed in; blank or a
	// meeting-specific value otherwise.
	ID string `json:"id,omitempty"`
	// Name is the participant's display name.
	Name string `json:"name,omitempty"`
	// Email is the participant's email address; Zoom leaves it blank for most people outside the host's account.
	Email string `json:"email,omitempty"`
	// JoinTime is when this join started.
	JoinTime *time.Time `json:"joinTime,omitempty"`
	// LeaveTime is when this join ended.
	LeaveTime *time.Time `json:"leaveTime,omitempty"`
	// DurationSeconds is the time between JoinTime and LeaveTime in seconds.
	DurationSeconds int `json:"durationSeconds"`
	// Status is Zoom's status value, such as in_meeting or in_waiting_room, passed through unchanged.
	Status string `json:"status,omitempty"`
	// IsInternalUser reports whether the participant belongs to the host's Zoom account.
	IsInternalUser bool `json:"isInternalUser"`
}

// ListPastMeetingParticipantsOperation implements the listPastMeetingParticipants Query.
type ListPastMeetingParticipantsOperation struct{ client *Client }

type zoomParticipantPageResource struct {
	NextPageToken string                    `json:"next_page_token"`
	TotalRecords  int                       `json:"total_records"`
	Participants  []zoomParticipantResource `json:"participants"`
}

type zoomParticipantResource struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	UserEmail    string `json:"user_email"`
	JoinTime     string `json:"join_time"`
	LeaveTime    string `json:"leave_time"`
	Duration     int    `json:"duration"`
	Status       string `json:"status"`
	InternalUser bool   `json:"internal_user"`
}

// Definition returns the immutable connector operation definition.
func (ListPastMeetingParticipantsOperation) Definition() sdkgo.QueryDefinition {
	return ListPastMeetingParticipantsDefinition
}

// Invoke lists one page. Transport failures, 408, 429, and 5xx responses are retried.
func (operation ListPastMeetingParticipantsOperation) Invoke(call sdkgo.Call, input ListPastMeetingParticipantsInput) sdkgo.QueryAttempt[ParticipantPage] {
	client := operation.client
	query, err := listPastMeetingParticipantsQuery(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListPastMeetingParticipantsBranchDefect, ParticipantPage{}, failurePointer(listPastMeetingParticipantsOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure, isRetryable := client.resolveCredentials(call, listPastMeetingParticipantsOperationID)
	if failure != nil {
		if isRetryable {
			return sdkgo.NewQueryRetry[ParticipantPage](*failure, 0)
		}
		return sdkgo.NewQueryBranch(ListPastMeetingParticipantsBranchDefect, ParticipantPage{}, failure, sdkgo.Receipt{})
	}
	meetingID := strconv.FormatInt(input.MeetingID, 10)
	empty := ParticipantPage{MeetingID: input.MeetingID}
	response, _, err := client.exchange(call, &credentials, providerRequest{
		method: http.MethodGet, path: "/past_meetings/" + meetingID + "/participants", query: query,
	})
	if err != nil {
		if errors.Is(err, errRequestNotBuilt) {
			return sdkgo.NewQueryBranch(ListPastMeetingParticipantsBranchDefect, ParticipantPage{}, failurePointer(listPastMeetingParticipantsOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
		}
		return sdkgo.NewQueryRetry[ParticipantPage](newFailure(listPastMeetingParticipantsOperationID, sdkgo.FailureTransport, "Zoom could not be reached"), 0)
	}
	classification := client.classifyReadResponse(listPastMeetingParticipantsOperationID, response)
	switch classification.outcome {
	case statusOutcomeUsable:
	case statusOutcomeRetry:
		return sdkgo.NewQueryRetry[ParticipantPage](classification.failure, classification.retryAfter)
	case statusOutcomeNotFound:
		return sdkgo.NewQueryBranch(ListPastMeetingParticipantsBranchNotFound, empty, &classification.failure, client.receipt(call, meetingID, classification.errorCode))
	case statusOutcomeInvalid:
		return sdkgo.NewQueryBranch(ListPastMeetingParticipantsBranchInvalidResponse, empty, &classification.failure, client.receipt(call, meetingID, ""))
	default:
		return sdkgo.NewQueryBranch(ListPastMeetingParticipantsBranchProviderRejected, empty, &classification.failure, client.receipt(call, meetingID, classification.errorCode))
	}
	page, err := decodeParticipantPage(response.body, input.MeetingID)
	if err != nil {
		return sdkgo.NewQueryBranch(ListPastMeetingParticipantsBranchInvalidResponse, empty, failurePointer(listPastMeetingParticipantsOperationID, sdkgo.FailureProtocol, err.Error()), client.receipt(call, meetingID, ""))
	}
	return sdkgo.NewQueryBranch(ListPastMeetingParticipantsBranchListed, page, nil, client.receipt(call, meetingID, ""))
}

func listPastMeetingParticipantsQuery(input ListPastMeetingParticipantsInput) (url.Values, error) {
	if err := validateMeetingID(input.MeetingID); err != nil {
		return nil, err
	}
	pageSize, err := resolvePageSize(input.PageSize)
	if err != nil {
		return nil, err
	}
	if err := validatePageToken(input.PageToken); err != nil {
		return nil, err
	}
	query := url.Values{"page_size": {strconv.Itoa(pageSize)}}
	if input.PageToken != "" {
		query.Set("next_page_token", input.PageToken)
	}
	return query, nil
}

func decodeParticipantPage(contents []byte, meetingID int64) (ParticipantPage, error) {
	var resource zoomParticipantPageResource
	if err := decodeSingleJSONObject(contents, &resource); err != nil || resource.TotalRecords < 0 ||
		validatePageToken(resource.NextPageToken) != nil {
		return ParticipantPage{}, errParticipantResponseInvalid
	}
	page := ParticipantPage{
		MeetingID: meetingID, Participants: make([]MeetingParticipant, 0, len(resource.Participants)),
		NextPageToken: resource.NextPageToken, TotalRecords: resource.TotalRecords,
	}
	for _, item := range resource.Participants {
		joinTime, joinErr := parseOptionalZoomTime(item.JoinTime)
		leaveTime, leaveErr := parseOptionalZoomTime(item.LeaveTime)
		if joinErr != nil || leaveErr != nil || item.Duration < 0 {
			return ParticipantPage{}, errParticipantResponseInvalid
		}
		page.Participants = append(page.Participants, MeetingParticipant{
			ID: item.ID, Name: item.Name, Email: item.UserEmail, JoinTime: joinTime, LeaveTime: leaveTime,
			DurationSeconds: item.Duration, Status: item.Status, IsInternalUser: item.InternalUser,
		})
	}
	return page, nil
}
