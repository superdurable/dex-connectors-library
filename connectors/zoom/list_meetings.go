// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const listMeetingsOperationID = "listMeetings"

// MeetingListType selects which of the user's meetings listMeetings returns.
// Zoom lists only scheduled meetings that have not expired, never instant meetings.
type MeetingListType string

const (
	// MeetingListUpcoming lists upcoming meetings, including live ones, at most six months ahead. It is the default.
	MeetingListUpcoming MeetingListType = "upcoming"
	// MeetingListScheduled lists every unexpired previous, live, and upcoming scheduled meeting.
	MeetingListScheduled MeetingListType = "scheduled"
	// MeetingListLive lists meetings in progress.
	MeetingListLive MeetingListType = "live"
	// MeetingListPrevious lists meetings whose scheduled end has passed, at most six months back.
	MeetingListPrevious MeetingListType = "previous_meetings"
)

// ListMeetingsInput selects one page of the authorized user's meetings.
type ListMeetingsInput struct {
	// Type selects the meetings to list; blank lists upcoming meetings.
	Type MeetingListType `json:"type,omitempty"`
	// PageSize is the number of meetings per page, from 1 to 300; zero uses Zoom's default of 30.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the NextPageToken of the previous page; Zoom expires it after 15 minutes.
	PageToken string `json:"pageToken,omitempty"`
}

// MeetingPage is one page of meetings in Zoom's order.
type MeetingPage struct {
	// Meetings lists the page's meetings.
	Meetings []MeetingSummary `json:"meetings"`
	// NextPageToken requests the next page; blank means this is the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
	// TotalRecords is Zoom's count of meetings across every page.
	TotalRecords int `json:"totalRecords"`
}

// MeetingSummary is one listed meeting. Zoom truncates a listed agenda to 250
// characters; use getMeeting for the full agenda and the settings.
type MeetingSummary struct {
	// ID is Zoom's numeric meeting ID.
	ID int64 `json:"id"`
	// UUID identifies the current meeting instance.
	UUID string `json:"uuid,omitempty"`
	// HostID is the Zoom user ID of the meeting host.
	HostID string `json:"hostId,omitempty"`
	// Topic is the meeting topic.
	Topic string `json:"topic"`
	// Type is Zoom's meeting type: 2 scheduled, 3 recurring without a fixed time, 8 recurring with a fixed time.
	Type int `json:"type,omitempty"`
	// StartTime is the scheduled start instant in UTC; nil for a meeting without a fixed time.
	StartTime *time.Time `json:"startTime,omitempty"`
	// DurationMinutes is the scheduled duration in minutes.
	DurationMinutes int `json:"durationMinutes,omitempty"`
	// TimeZone is the IANA time zone Zoom uses to display StartTime.
	TimeZone string `json:"timeZone,omitempty"`
	// Agenda is the meeting description, truncated by Zoom to 250 characters.
	Agenda string `json:"agenda,omitempty"`
	// JoinURL is the participant join link. Share it only with invitees: it can embed the passcode.
	JoinURL string `json:"joinUrl,omitempty"`
	// CreatedAt is when Zoom created the meeting.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}

// ListMeetingsOperation implements the listMeetings Query.
type ListMeetingsOperation struct{ client *Client }

type zoomMeetingPageResource struct {
	NextPageToken string                `json:"next_page_token"`
	TotalRecords  int                   `json:"total_records"`
	Meetings      []zoomMeetingResource `json:"meetings"`
}

// Definition returns the immutable connector operation definition.
func (ListMeetingsOperation) Definition() sdkgo.QueryDefinition { return ListMeetingsDefinition }

// Invoke lists one page. Transport failures, 408, 429, and 5xx responses are retried.
func (operation ListMeetingsOperation) Invoke(call sdkgo.Call, input ListMeetingsInput) sdkgo.QueryAttempt[MeetingPage] {
	client := operation.client
	query, err := listMeetingsQuery(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListMeetingsBranchDefect, MeetingPage{}, failurePointer(listMeetingsOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure, isRetryable := client.resolveCredentials(call, listMeetingsOperationID)
	if failure != nil {
		if isRetryable {
			return sdkgo.NewQueryRetry[MeetingPage](*failure, 0)
		}
		return sdkgo.NewQueryBranch(ListMeetingsBranchDefect, MeetingPage{}, failure, sdkgo.Receipt{})
	}
	response, _, err := client.exchange(call, &credentials, providerRequest{method: http.MethodGet, path: currentUserPath + "/meetings", query: query})
	if err != nil {
		if errors.Is(err, errRequestNotBuilt) {
			return sdkgo.NewQueryBranch(ListMeetingsBranchDefect, MeetingPage{}, failurePointer(listMeetingsOperationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
		}
		return sdkgo.NewQueryRetry[MeetingPage](newFailure(listMeetingsOperationID, sdkgo.FailureTransport, "Zoom could not be reached"), 0)
	}
	classification := client.classifyReadResponse(listMeetingsOperationID, response)
	switch classification.outcome {
	case statusOutcomeUsable:
	case statusOutcomeRetry:
		return sdkgo.NewQueryRetry[MeetingPage](classification.failure, classification.retryAfter)
	case statusOutcomeInvalid:
		return sdkgo.NewQueryBranch(ListMeetingsBranchInvalidResponse, MeetingPage{}, &classification.failure, client.receipt(call, "", ""))
	default:
		// The user is always "me", so a missing user is a rejection of this connection, not a lookup miss.
		return sdkgo.NewQueryBranch(ListMeetingsBranchProviderRejected, MeetingPage{}, &classification.failure, client.receipt(call, "", classification.errorCode))
	}
	page, err := decodeMeetingPage(response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(ListMeetingsBranchInvalidResponse, MeetingPage{}, failurePointer(listMeetingsOperationID, sdkgo.FailureProtocol, "Zoom returned an unusable meeting page"), client.receipt(call, "", ""))
	}
	return sdkgo.NewQueryBranch(ListMeetingsBranchListed, page, nil, client.receipt(call, "", ""))
}

func listMeetingsQuery(input ListMeetingsInput) (url.Values, error) {
	listType := input.Type
	switch listType {
	case "":
		listType = MeetingListUpcoming
	case MeetingListUpcoming, MeetingListScheduled, MeetingListLive, MeetingListPrevious:
	default:
		return nil, fmt.Errorf("type %q must be upcoming, scheduled, live, or previous_meetings", input.Type)
	}
	pageSize, err := resolvePageSize(input.PageSize)
	if err != nil {
		return nil, err
	}
	if err := validatePageToken(input.PageToken); err != nil {
		return nil, err
	}
	query := url.Values{"type": {string(listType)}, "page_size": {strconv.Itoa(pageSize)}}
	if input.PageToken != "" {
		query.Set("next_page_token", input.PageToken)
	}
	return query, nil
}

func decodeMeetingPage(contents []byte) (MeetingPage, error) {
	var resource zoomMeetingPageResource
	if err := decodeSingleJSONObject(contents, &resource); err != nil || resource.TotalRecords < 0 ||
		validatePageToken(resource.NextPageToken) != nil {
		return MeetingPage{}, errMeetingResponseInvalid
	}
	page := MeetingPage{Meetings: make([]MeetingSummary, 0, len(resource.Meetings)), NextPageToken: resource.NextPageToken, TotalRecords: resource.TotalRecords}
	for _, item := range resource.Meetings {
		meeting, err := item.toMeeting()
		if err != nil {
			return MeetingPage{}, err
		}
		page.Meetings = append(page.Meetings, MeetingSummary{
			ID: meeting.ID, UUID: meeting.UUID, HostID: meeting.HostID, Topic: meeting.Topic, Type: meeting.Type,
			StartTime: meeting.StartTime, DurationMinutes: meeting.DurationMinutes, TimeZone: meeting.TimeZone,
			Agenda: meeting.Agenda, JoinURL: meeting.JoinURL, CreatedAt: meeting.CreatedAt,
		})
	}
	return page, nil
}
