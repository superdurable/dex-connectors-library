// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// maximumFreeBusySchedules is Microsoft's documented getSchedule limit of 20 entities per request.
	maximumFreeBusySchedules            = 20
	defaultAvailabilityViewInterval     = 30
	minimumAvailabilityViewInterval     = 5
	maximumAvailabilityViewInterval     = 1440
	knownAvailabilityViewCharacters     = "01234"
	busyAvailabilityViewCharacters      = "123"
	scheduleUnknownReasonError          = "error"
	scheduleUnknownReasonMissing        = "missing"
	scheduleUnknownReasonUnknownStatus  = "unknownStatus"
	scheduleUnknownReasonUnreadableView = "unreadableAvailabilityView"
)

// QueryFreeBusyInput asks Microsoft Graph getSchedule for the availability of users,
// distribution lists, or resources in a bounded window.
type QueryFreeBusyInput struct {
	// Schedules lists 1 to 20 unique bare SMTP addresses, such as colleague@contoso.com or a room mailbox.
	Schedules []string `json:"schedules"`
	// TimeMin is the required window start as RFC 3339 with an explicit offset.
	TimeMin string `json:"timeMin"`
	// TimeMax is the required window end as RFC 3339 with an explicit offset. The window must be shorter than 62 days.
	TimeMax string `json:"timeMax"`
	// AvailabilityViewInterval is the length in minutes of each AvailabilityView slot, from 5 to 1440; zero requests 30.
	AvailabilityViewInterval int64 `json:"availabilityViewInterval,omitempty"`
	// TimeZone is an optional IANA zone for the offsets of returned intervals; blank uses UTC.
	TimeZone string `json:"timeZone,omitempty"`
}

// QueryFreeBusyOutput holds the availability of every requested schedule in request order.
type QueryFreeBusyOutput struct {
	// TimeMin echoes the checked window start.
	TimeMin string `json:"timeMin"`
	// TimeMax echoes the checked window end.
	TimeMax string `json:"timeMax"`
	// TimeZone is the IANA zone of every returned offset.
	TimeZone string `json:"timeZone"`
	// AvailabilityViewInterval is the slot length in minutes of every AvailabilityView.
	AvailabilityViewInterval int64 `json:"availabilityViewInterval"`
	// Schedules lists one entry per requested address.
	Schedules []ScheduleAvailability `json:"schedules"`
}

// ScheduleAvailability is the availability of one schedule.
type ScheduleAvailability struct {
	// ScheduleID is the requested SMTP address.
	ScheduleID string `json:"scheduleId"`
	// IsBusy reports busy, tentative, or out-of-office time in the window, from an item or an AvailabilityView slot.
	IsBusy bool `json:"isBusy"`
	// IsAvailabilityKnown is false when Graph reported an error, an unknown item, or an unreadable view,
	// or omitted the schedule. Unknown availability is never free, and it selects the incomplete branch.
	IsAvailabilityKnown bool `json:"isAvailabilityKnown"`
	// UnknownReason is error, missing, unknownStatus, or unreadableAvailabilityView when availability is unknown.
	UnknownReason string `json:"unknownReason,omitempty"`
	// ErrorResponseCode is Graph's freeBusyError responseCode when it is word-shaped; the message is never kept.
	ErrorResponseCode string `json:"errorResponseCode,omitempty"`
	// Items lists Graph's schedule items, free ones included, in Graph's order.
	Items []ScheduleItem `json:"items"`
	// AvailabilityView is Graph's slot string: 0 free or working elsewhere, 1 tentative, 2 busy, 3 out of office.
	AvailabilityView string `json:"availabilityView"`
}

// ScheduleItem is one block of time on a schedule.
type ScheduleItem struct {
	// Start is the inclusive start as RFC 3339 with an explicit offset.
	Start string `json:"start"`
	// End is the exclusive end as RFC 3339 with an explicit offset.
	End string `json:"end"`
	// Status is free, tentative, busy, oof, workingElsewhere, or unknown.
	Status string `json:"status"`
}

// QueryFreeBusyOperation implements the queryFreeBusy connector Query.
type QueryFreeBusyOperation struct{ client *Client }

type graphScheduleResponse struct {
	Value []graphScheduleInformation `json:"value"`
}

type graphScheduleInformation struct {
	ScheduleID       string              `json:"scheduleId"`
	AvailabilityView string              `json:"availabilityView"`
	ScheduleItems    []graphScheduleItem `json:"scheduleItems"`
	Error            *struct {
		ResponseCode string `json:"responseCode"`
	} `json:"error"`
}

type graphScheduleItem struct {
	Status string                 `json:"status"`
	Start  *graphDateTimeTimeZone `json:"start"`
	End    *graphDateTimeTimeZone `json:"end"`
}

// Definition returns the immutable connector operation definition.
func (QueryFreeBusyOperation) Definition() sdkgo.QueryDefinition { return QueryFreeBusyDefinition }

// Invoke executes one getSchedule request and classifies its attempt. Any schedule whose
// availability is unknown selects incomplete, so an unknown never reads as free.
func (operation QueryFreeBusyOperation) Invoke(call sdkgo.Call, input QueryFreeBusyInput) sdkgo.QueryAttempt[QueryFreeBusyOutput] {
	const operationID = "queryFreeBusy"
	window, err := validateQueryFreeBusyInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(QueryFreeBusyBranchDefect, QueryFreeBusyOutput{}, failurePointer(sdkgo.FailureValidation, operationID, err.Error()), sdkgo.Receipt{})
	}
	credentials, mailboxRoot, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(QueryFreeBusyBranchDefect, QueryFreeBusyOutput{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.send(call, &credentials, graphRequest{
		method: http.MethodPost, path: mailboxRoot + "/calendar/getSchedule",
		payload: map[string]any{
			"schedules": input.Schedules, "startTime": encodeGraphUTC(window.start), "endTime": encodeGraphUTC(window.end),
			"availabilityViewInterval": window.interval,
		},
	})
	if err != nil {
		requestFailure := asProviderRequestError(err)
		switch requestFailure.kind {
		case sdkgo.FailureResponseTooLarge:
			return sdkgo.NewQueryBranch(QueryFreeBusyBranchInvalidResponse, QueryFreeBusyOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), operation.client.receipt(call, response, ""))
		case sdkgo.FailureLocalDefect:
			return sdkgo.NewQueryBranch(QueryFreeBusyBranchDefect, QueryFreeBusyOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), sdkgo.Receipt{})
		default:
			return sdkgo.NewQueryRetry[QueryFreeBusyOutput](calendarFailure(sdkgo.FailureAvailability, operationID, "provider is unavailable"), 0)
		}
	}
	receipt := operation.client.receipt(call, response, "")
	if !isSuccessStatus(response.statusCode) {
		classification := classifyFailureStatus(response)
		if classification.isRetryable {
			return sdkgo.NewQueryRetry[QueryFreeBusyOutput](calendarFailure(classification.kind, operationID, classification.message), classification.retryAfter)
		}
		return sdkgo.NewQueryBranch(QueryFreeBusyBranchProviderRejected, QueryFreeBusyOutput{}, failurePointer(classification.kind, operationID, classification.message), receipt)
	}
	output, err := decodeSchedules(response.body, input, window)
	if err != nil {
		return sdkgo.NewQueryBranch(QueryFreeBusyBranchInvalidResponse, QueryFreeBusyOutput{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned an invalid schedule response: "+err.Error()), receipt)
	}
	for _, schedule := range output.Schedules {
		if !schedule.IsAvailabilityKnown {
			return sdkgo.NewQueryBranch(QueryFreeBusyBranchIncomplete, output, failurePointer(sdkgo.FailureProviderRejection, operationID, "provider could not report availability for every schedule"), receipt)
		}
	}
	return sdkgo.NewQueryBranch(QueryFreeBusyBranchQueried, output, nil, receipt)
}

// freeBusyWindow is the validated query window and display zone.
type freeBusyWindow struct {
	start    time.Time
	end      time.Time
	interval int64
	display  eventDisplay
}

func validateQueryFreeBusyInput(input QueryFreeBusyInput) (freeBusyWindow, error) {
	if len(input.Schedules) == 0 || len(input.Schedules) > maximumFreeBusySchedules {
		return freeBusyWindow{}, fmt.Errorf("schedules must list 1 to %d addresses", maximumFreeBusySchedules)
	}
	seen := make(map[string]bool, len(input.Schedules))
	for _, schedule := range input.Schedules {
		if err := validateBareEmailAddress("schedule", schedule); err != nil {
			return freeBusyWindow{}, err
		}
		if seen[strings.ToLower(schedule)] {
			return freeBusyWindow{}, fmt.Errorf("schedule %q is duplicated", schedule)
		}
		seen[strings.ToLower(schedule)] = true
	}
	start, end, err := parseQueryWindow(input.TimeMin, input.TimeMax)
	if err != nil {
		return freeBusyWindow{}, err
	}
	if end.Sub(start) >= maximumFreeBusyWindow {
		return freeBusyWindow{}, errors.New("the timeMin to timeMax window must be shorter than 62 days")
	}
	interval := input.AvailabilityViewInterval
	if interval == 0 {
		interval = defaultAvailabilityViewInterval
	}
	if interval < minimumAvailabilityViewInterval || interval > maximumAvailabilityViewInterval {
		return freeBusyWindow{}, errors.New("availabilityViewInterval must be between 5 and 1440 minutes, or zero for the default")
	}
	location, zone, err := loadDisplayLocation(input.TimeZone)
	if err != nil {
		return freeBusyWindow{}, err
	}
	return freeBusyWindow{start: start, end: end, interval: interval, display: eventDisplay{location: location, zone: zone}}, nil
}

func decodeSchedules(body []byte, input QueryFreeBusyInput, window freeBusyWindow) (QueryFreeBusyOutput, error) {
	var response graphScheduleResponse
	if err := json.Unmarshal(body, &response); err != nil || response.Value == nil {
		return QueryFreeBusyOutput{}, errors.New("schedule response is not a JSON object with a value array")
	}
	returned := make(map[string]graphScheduleInformation, len(response.Value))
	for _, information := range response.Value {
		returned[strings.ToLower(information.ScheduleID)] = information
	}
	output := QueryFreeBusyOutput{
		TimeMin: input.TimeMin, TimeMax: input.TimeMax, TimeZone: window.display.zone,
		AvailabilityViewInterval: window.interval, Schedules: make([]ScheduleAvailability, 0, len(input.Schedules)),
	}
	for _, scheduleID := range input.Schedules {
		information, ok := returned[strings.ToLower(scheduleID)]
		if !ok {
			output.Schedules = append(output.Schedules, ScheduleAvailability{ScheduleID: scheduleID, UnknownReason: scheduleUnknownReasonMissing, Items: []ScheduleItem{}})
			continue
		}
		availability, err := convertScheduleAvailability(scheduleID, information, window.display)
		if err != nil {
			return QueryFreeBusyOutput{}, err
		}
		output.Schedules = append(output.Schedules, availability)
	}
	return output, nil
}

// convertScheduleAvailability marks a schedule busy from any blocking item or slot, and unknown from any doubt.
func convertScheduleAvailability(scheduleID string, information graphScheduleInformation, display eventDisplay) (ScheduleAvailability, error) {
	availability := ScheduleAvailability{
		ScheduleID: scheduleID, AvailabilityView: information.AvailabilityView, Items: make([]ScheduleItem, 0, len(information.ScheduleItems)),
		IsAvailabilityKnown: true,
	}
	for _, item := range information.ScheduleItems {
		start, startInstant, startErr := decodeGraphBoundary("scheduleItem.start", item.Start, display.location, display.zone)
		end, endInstant, endErr := decodeGraphBoundary("scheduleItem.end", item.End, display.location, display.zone)
		if startErr != nil || endErr != nil || endInstant.Before(startInstant) {
			// Graph returns one placeholder item beside an error, which the error already makes unknown.
			if information.Error != nil {
				continue
			}
			return ScheduleAvailability{}, fmt.Errorf("schedule %q has an item without two ordered times", scheduleID)
		}
		status := item.Status
		if status == "" {
			status = unknownShowAs
		}
		availability.Items = append(availability.Items, ScheduleItem{Start: start.DateTime, End: end.DateTime, Status: status})
		switch status {
		case "tentative", "busy", "oof":
			availability.IsBusy = true
		case "free", "workingElsewhere":
		default:
			availability.markUnknown(scheduleUnknownReasonUnknownStatus)
		}
	}
	if strings.ContainsAny(information.AvailabilityView, busyAvailabilityViewCharacters) {
		availability.IsBusy = true
	}
	if information.AvailabilityView == "" || strings.Trim(information.AvailabilityView, knownAvailabilityViewCharacters) != "" {
		availability.markUnknown(scheduleUnknownReasonUnreadableView)
	}
	if information.Error != nil {
		availability.markUnknown(scheduleUnknownReasonError)
		if graphErrorCodePattern.MatchString(information.Error.ResponseCode) {
			availability.ErrorResponseCode = information.Error.ResponseCode
		}
	}
	return availability, nil
}

// markUnknown records the first reason availability is unknown; an error outranks the others.
func (availability *ScheduleAvailability) markUnknown(reason string) {
	if availability.IsAvailabilityKnown || reason == scheduleUnknownReasonError {
		availability.UnknownReason = reason
	}
	availability.IsAvailabilityKnown = false
}
