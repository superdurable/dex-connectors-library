// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetTimeEntryInput identifies one Harvest time entry.
type GetTimeEntryInput struct {
	// ID is the positive time-entry ID.
	ID int64 `json:"id"`
}

// CreateTimeEntryByDurationInput logs a completed duration against a project task.
type CreateTimeEntryByDurationInput struct {
	// ProjectID identifies the project.
	ProjectID int64 `json:"projectId"`
	// TaskID identifies a task assigned to that project.
	TaskID int64 `json:"taskId"`
	// SpentDate is the ISO calendar date YYYY-MM-DD.
	SpentDate string `json:"spentDate"`
	// Hours is a positive duration in decimal hours; a completed entry does not start a timer.
	Hours float64 `json:"hours"`
	// Notes are optional text attached to the entry.
	Notes string `json:"notes,omitempty"`
}

// CreateTimeEntryByStartEndInput logs a completed clock-time interval.
type CreateTimeEntryByStartEndInput struct {
	// ProjectID identifies the project.
	ProjectID int64 `json:"projectId"`
	// TaskID identifies a task assigned to that project.
	TaskID int64 `json:"taskId"`
	// SpentDate is the ISO calendar date YYYY-MM-DD.
	SpentDate string `json:"spentDate"`
	// StartedTime is a local 24-hour HH:MM time.
	StartedTime string `json:"startedTime"`
	// EndedTime is a local 24-hour HH:MM time after StartedTime.
	EndedTime string `json:"endedTime"`
	// Notes are optional text attached to the entry.
	Notes string `json:"notes,omitempty"`
}

// UpdateTimeEntryInput sets absolute values on a logged time entry.
type UpdateTimeEntryInput struct {
	// ID is the positive time-entry ID.
	ID int64 `json:"id"`
	// ProjectID replaces the project when supplied.
	ProjectID *int64 `json:"projectId,omitempty"`
	// TaskID replaces the task when supplied.
	TaskID *int64 `json:"taskId,omitempty"`
	// SpentDate replaces the YYYY-MM-DD calendar date when supplied.
	SpentDate *string `json:"spentDate,omitempty"`
	// Hours replaces the decimal duration when supplied.
	Hours *float64 `json:"hours,omitempty"`
	// Notes replace the text when supplied; empty clears it.
	Notes *string `json:"notes,omitempty"`
}

// DeleteTimeEntryInput identifies one time entry for deletion.
type DeleteTimeEntryInput struct {
	// ID is the positive time-entry ID.
	ID int64 `json:"id"`
}

// DeletedTimeEntry identifies a time entry whose deletion Harvest confirmed.
type DeletedTimeEntry struct {
	// ID is the deleted Harvest time-entry ID.
	ID int64 `json:"id"`
}

// DeletedTimeEntryExternalReference identifies a cleared external reference.
type DeletedTimeEntryExternalReference struct {
	// ID is the Harvest time-entry ID.
	ID int64 `json:"id"`
}

// TimeEntryActionInput identifies a running time entry for a state change.
type TimeEntryActionInput struct {
	// ID is the positive time-entry ID.
	ID int64 `json:"id"`
}

// GetTimeEntryOperation reads one Harvest time entry.
type GetTimeEntryOperation struct{ client *Client }

// GetTimeEntry binds the time-entry lookup Query to this client.
func (client *Client) GetTimeEntry() GetTimeEntryOperation { return GetTimeEntryOperation{client} }

// Definition returns the generated time-entry lookup contract.
func (GetTimeEntryOperation) Definition() sdkgo.QueryDefinition { return GetTimeEntryDefinition }

// Invoke validates the ID and reads one time entry.
func (operation GetTimeEntryOperation) Invoke(call sdkgo.Call, input GetTimeEntryInput) sdkgo.QueryAttempt[TimeEntry] {
	if input.ID <= 0 {
		return failedBranch[TimeEntry]("getTimeEntry", GetTimeEntryBranchDefect, sdkgo.FailureValidation, "time-entry ID must be positive")
	}
	return invokeGet(operation.client, call, "getTimeEntry", timeEntryPath(input.ID), GetTimeEntryBranchFound, GetTimeEntryBranchNotFound, decodeTimeEntry)
}

// CreateTimeEntryByDurationOperation logs one completed duration.
type CreateTimeEntryByDurationOperation struct{ client *Client }

// CreateTimeEntryByDuration binds the duration creation Mutation to this client.
func (client *Client) CreateTimeEntryByDuration() CreateTimeEntryByDurationOperation {
	return CreateTimeEntryByDurationOperation{client}
}

// Definition returns the generated duration creation contract.
func (CreateTimeEntryByDurationOperation) Definition() sdkgo.MutationDefinition {
	return CreateTimeEntryByDurationDefinition
}

// IdempotencyKey records the call identity; Harvest does not deduplicate this POST.
func (CreateTimeEntryByDurationOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTimeEntryByDurationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke logs a completed duration under the limited-repeat fence.
func (operation CreateTimeEntryByDurationOperation) Invoke(call sdkgo.Call, input CreateTimeEntryByDurationInput) sdkgo.MutationAttempt[TimeEntry] {
	if input.ProjectID <= 0 || input.TaskID <= 0 || !validHarvestDate(input.SpentDate) || math.IsNaN(input.Hours) || math.IsInf(input.Hours, 0) || input.Hours <= 0 || len(input.Notes) > 10000 {
		return harvestMutationFailure[TimeEntry]("createTimeEntryByDuration", CreateTimeEntryByDurationBranchDefect, sdkgo.FailureValidation, "project, task, date, hours, or notes are invalid")
	}
	payload, err := json.Marshal(struct {
		ProjectID int64   `json:"project_id"`
		TaskID    int64   `json:"task_id"`
		SpentDate string  `json:"spent_date"`
		Hours     float64 `json:"hours"`
		Notes     string  `json:"notes,omitempty"`
	}{input.ProjectID, input.TaskID, input.SpentDate, input.Hours, input.Notes})
	if err != nil {
		return harvestMutationFailure[TimeEntry]("createTimeEntryByDuration", CreateTimeEntryByDurationBranchDefect, sdkgo.FailureValidation, "time entry cannot be encoded")
	}
	return invokeLimitedHarvestCreate(operation.client, call, "createTimeEntryByDuration", CreateTimeEntryByDurationDefinition.StepDefaults.ExecuteMethodTimeout, "/time_entries", payload, CreateTimeEntryByDurationBranchCreated, CreateTimeEntryByDurationBranchProviderRejected, CreateTimeEntryByDurationBranchDefect, decodeTimeEntry, func(entry TimeEntry) string { return strconv.FormatInt(entry.ID, 10) })
}

// CreateTimeEntryByStartEndOperation logs one completed clock interval.
type CreateTimeEntryByStartEndOperation struct{ client *Client }

// CreateTimeEntryByStartEnd binds the clock interval Mutation to this client.
func (client *Client) CreateTimeEntryByStartEnd() CreateTimeEntryByStartEndOperation {
	return CreateTimeEntryByStartEndOperation{client}
}

// Definition returns the generated clock interval creation contract.
func (CreateTimeEntryByStartEndOperation) Definition() sdkgo.MutationDefinition {
	return CreateTimeEntryByStartEndDefinition
}

// IdempotencyKey records the call identity; Harvest does not deduplicate this POST.
func (CreateTimeEntryByStartEndOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTimeEntryByStartEndInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke logs a completed interval under the limited-repeat fence.
func (operation CreateTimeEntryByStartEndOperation) Invoke(call sdkgo.Call, input CreateTimeEntryByStartEndInput) sdkgo.MutationAttempt[TimeEntry] {
	if input.ProjectID <= 0 || input.TaskID <= 0 || !validHarvestDate(input.SpentDate) || !validHarvestClockInterval(input.StartedTime, input.EndedTime) || len(input.Notes) > 10000 {
		return harvestMutationFailure[TimeEntry]("createTimeEntryByStartEnd", CreateTimeEntryByStartEndBranchDefect, sdkgo.FailureValidation, "project, task, date, clock interval, or notes are invalid")
	}
	payload, err := json.Marshal(struct {
		ProjectID   int64  `json:"project_id"`
		TaskID      int64  `json:"task_id"`
		SpentDate   string `json:"spent_date"`
		StartedTime string `json:"started_time"`
		EndedTime   string `json:"ended_time"`
		Notes       string `json:"notes,omitempty"`
	}{input.ProjectID, input.TaskID, input.SpentDate, input.StartedTime, input.EndedTime, input.Notes})
	if err != nil {
		return harvestMutationFailure[TimeEntry]("createTimeEntryByStartEnd", CreateTimeEntryByStartEndBranchDefect, sdkgo.FailureValidation, "time entry cannot be encoded")
	}
	return invokeLimitedHarvestCreate(operation.client, call, "createTimeEntryByStartEnd", CreateTimeEntryByStartEndDefinition.StepDefaults.ExecuteMethodTimeout, "/time_entries", payload, CreateTimeEntryByStartEndBranchCreated, CreateTimeEntryByStartEndBranchProviderRejected, CreateTimeEntryByStartEndBranchDefect, decodeTimeEntry, func(entry TimeEntry) string { return strconv.FormatInt(entry.ID, 10) })
}

// UpdateTimeEntryOperation sets absolute fields on a time entry.
type UpdateTimeEntryOperation struct{ client *Client }

// UpdateTimeEntry binds the time-entry update Mutation to this client.
func (client *Client) UpdateTimeEntry() UpdateTimeEntryOperation {
	return UpdateTimeEntryOperation{client}
}

// Definition returns the generated time-entry update contract.
func (UpdateTimeEntryOperation) Definition() sdkgo.MutationDefinition {
	return UpdateTimeEntryDefinition
}

// IdempotencyKey records the stable Dex call identity for this update.
func (UpdateTimeEntryOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTimeEntryInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies an idempotent PATCH to a time entry.
func (operation UpdateTimeEntryOperation) Invoke(call sdkgo.Call, input UpdateTimeEntryInput) sdkgo.MutationAttempt[TimeEntry] {
	if input.ID <= 0 || (input.ProjectID == nil && input.TaskID == nil && input.SpentDate == nil && input.Hours == nil && input.Notes == nil) ||
		(input.ProjectID != nil && *input.ProjectID <= 0) || (input.TaskID != nil && *input.TaskID <= 0) ||
		(input.SpentDate != nil && !validHarvestDate(*input.SpentDate)) ||
		(input.Hours != nil && (math.IsNaN(*input.Hours) || math.IsInf(*input.Hours, 0) || *input.Hours < 0)) ||
		(input.Notes != nil && len(*input.Notes) > 10000) {
		return harvestMutationFailure[TimeEntry]("updateTimeEntry", UpdateTimeEntryBranchDefect, sdkgo.FailureValidation, "time-entry ID or fields are invalid")
	}
	payload, err := json.Marshal(struct {
		ProjectID *int64   `json:"project_id,omitempty"`
		TaskID    *int64   `json:"task_id,omitempty"`
		SpentDate *string  `json:"spent_date,omitempty"`
		Hours     *float64 `json:"hours,omitempty"`
		Notes     *string  `json:"notes,omitempty"`
	}{input.ProjectID, input.TaskID, input.SpentDate, input.Hours, input.Notes})
	if err != nil {
		return harvestMutationFailure[TimeEntry]("updateTimeEntry", UpdateTimeEntryBranchDefect, sdkgo.FailureValidation, "time entry cannot be encoded")
	}
	return invokeSafeHarvestWrite(operation.client, call, "updateTimeEntry", http.MethodPatch, timeEntryPath(input.ID), payload, UpdateTimeEntryBranchUpdated, UpdateTimeEntryBranchNotFound, UpdateTimeEntryBranchProviderRejected, decodeTimeEntry)
}

// DeleteTimeEntryOperation deletes a time entry.
type DeleteTimeEntryOperation struct{ client *Client }

// DeleteTimeEntry binds the time-entry deletion Mutation to this client.
func (client *Client) DeleteTimeEntry() DeleteTimeEntryOperation {
	return DeleteTimeEntryOperation{client}
}

// Definition returns the generated time-entry deletion contract.
func (DeleteTimeEntryOperation) Definition() sdkgo.MutationDefinition {
	return DeleteTimeEntryDefinition
}

// IdempotencyKey records the stable Dex call identity for this deletion.
func (DeleteTimeEntryOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteTimeEntryInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes a time entry with safely repeatable DELETE.
func (operation DeleteTimeEntryOperation) Invoke(call sdkgo.Call, input DeleteTimeEntryInput) sdkgo.MutationAttempt[DeletedTimeEntry] {
	if input.ID <= 0 {
		return harvestMutationFailure[DeletedTimeEntry]("deleteTimeEntry", DeleteTimeEntryBranchDefect, sdkgo.FailureValidation, "time-entry ID must be positive")
	}
	decode := func(_ []byte) (DeletedTimeEntry, error) { return DeletedTimeEntry{ID: input.ID}, nil }
	return invokeSafeHarvestWrite(operation.client, call, "deleteTimeEntry", http.MethodDelete, timeEntryPath(input.ID), nil, DeleteTimeEntryBranchDeleted, DeleteTimeEntryBranchNotFound, DeleteTimeEntryBranchProviderRejected, decode)
}

// DeleteTimeEntryExternalReferenceOperation clears an external reference.
type DeleteTimeEntryExternalReferenceOperation struct{ client *Client }

// DeleteTimeEntryExternalReference binds reference deletion to this client.
func (client *Client) DeleteTimeEntryExternalReference() DeleteTimeEntryExternalReferenceOperation {
	return DeleteTimeEntryExternalReferenceOperation{client}
}

// Definition returns the generated external-reference deletion contract.
func (DeleteTimeEntryExternalReferenceOperation) Definition() sdkgo.MutationDefinition {
	return DeleteTimeEntryExternalReferenceDefinition
}

// IdempotencyKey records the stable Dex call identity for this deletion.
func (DeleteTimeEntryExternalReferenceOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteTimeEntryInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke clears a time entry's external reference with repeat-safe DELETE.
func (operation DeleteTimeEntryExternalReferenceOperation) Invoke(call sdkgo.Call, input DeleteTimeEntryInput) sdkgo.MutationAttempt[DeletedTimeEntryExternalReference] {
	if input.ID <= 0 {
		return harvestMutationFailure[DeletedTimeEntryExternalReference]("deleteTimeEntryExternalReference", DeleteTimeEntryExternalReferenceBranchDefect, sdkgo.FailureValidation, "time-entry ID must be positive")
	}
	decode := func(_ []byte) (DeletedTimeEntryExternalReference, error) {
		return DeletedTimeEntryExternalReference{ID: input.ID}, nil
	}
	return invokeSafeHarvestWrite(operation.client, call, "deleteTimeEntryExternalReference", http.MethodDelete, timeEntryPath(input.ID)+"/external_reference", nil, DeleteTimeEntryExternalReferenceBranchDeleted, DeleteTimeEntryExternalReferenceBranchNotFound, DeleteTimeEntryExternalReferenceBranchProviderRejected, decode)
}

// StopTimeEntryOperation stops a running timer.
type StopTimeEntryOperation struct{ client *Client }

// StopTimeEntry binds the timer stop Mutation to this client.
func (client *Client) StopTimeEntry() StopTimeEntryOperation { return StopTimeEntryOperation{client} }

// Definition returns the generated timer stop contract.
func (StopTimeEntryOperation) Definition() sdkgo.MutationDefinition { return StopTimeEntryDefinition }

// IdempotencyKey records the stable Dex call identity for this stop.
func (StopTimeEntryOperation) IdempotencyKey(callID sdkgo.CallID, _ TimeEntryActionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke stops the timer; an unknown response can safely retry the same target state.
func (operation StopTimeEntryOperation) Invoke(call sdkgo.Call, input TimeEntryActionInput) sdkgo.MutationAttempt[TimeEntry] {
	if input.ID <= 0 {
		return harvestMutationFailure[TimeEntry]("stopTimeEntry", StopTimeEntryBranchDefect, sdkgo.FailureValidation, "time-entry ID must be positive")
	}
	if call.Context.Attempt() > 1 {
		credentials, err := operation.client.credentials.Resolve(call)
		if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
			return harvestMutationRetry[TimeEntry]("stopTimeEntry", sdkgo.FailureAuthentication, "cannot confirm an earlier stop", 0)
		}
		request, err := operation.client.newHarvestRequest(call.Context, credentials, http.MethodGet, timeEntryPath(input.ID), nil)
		if err != nil {
			return harvestMutationFailure[TimeEntry]("stopTimeEntry", StopTimeEntryBranchDefect, sdkgo.FailureLocalDefect, "stop read-back cannot be built")
		}
		response, err := operation.client.httpClient.Do(request)
		if err != nil {
			return harvestMutationRetry[TimeEntry]("stopTimeEntry", sdkgo.FailureTransport, "stop read-back failed", 0)
		}
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode == http.StatusNotFound {
			return harvestMutationFailure[TimeEntry]("stopTimeEntry", StopTimeEntryBranchNotFound, sdkgo.FailureNotFound, "time entry was not found")
		}
		if response.StatusCode != http.StatusOK {
			return harvestMutationRetry[TimeEntry]("stopTimeEntry", sdkgo.FailureAvailability, "stop read-back is unavailable", 0)
		}
		body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
		if err != nil {
			return harvestMutationRetry[TimeEntry]("stopTimeEntry", sdkgo.FailureTransport, "stop read-back was interrupted", 0)
		}
		var state struct {
			IsRunning *bool `json:"is_running"`
		}
		if err := json.Unmarshal(body, &state); err != nil || state.IsRunning == nil {
			return harvestMutationRetry[TimeEntry]("stopTimeEntry", sdkgo.FailureProtocol, "stop read-back was invalid", 0)
		}
		if !*state.IsRunning {
			entry, err := decodeTimeEntry(body)
			if err != nil {
				return harvestMutationRetry[TimeEntry]("stopTimeEntry", sdkgo.FailureProtocol, "stopped entry was invalid", 0)
			}
			return sdkgo.NewMutationBranch(StopTimeEntryBranchStopped, entry, nil, sdkgo.Receipt{})
		}
	}
	return invokeSafeHarvestWrite(operation.client, call, "stopTimeEntry", http.MethodPatch, timeEntryPath(input.ID)+"/stop", nil, StopTimeEntryBranchStopped, StopTimeEntryBranchNotFound, StopTimeEntryBranchProviderRejected, decodeTimeEntry)
}

func timeEntryPath(id int64) string { return "/time_entries/" + strconv.FormatInt(id, 10) }

func decodeTimeEntry(body []byte) (TimeEntry, error) {
	var wire struct {
		ID      int64 `json:"id"`
		Project struct {
			ID int64 `json:"id"`
		} `json:"project"`
		Hours     float64 `json:"hours"`
		SpentDate string  `json:"spent_date"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TimeEntry{}, err
	}
	if wire.ID <= 0 || wire.Project.ID <= 0 || !validHarvestDate(wire.SpentDate) {
		return TimeEntry{}, fmt.Errorf("missing time-entry identity")
	}
	return TimeEntry{ID: wire.ID, Project: wire.Project, Hours: wire.Hours, SpentDate: wire.SpentDate}, nil
}

func validHarvestDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func validHarvestClockInterval(start, end string) bool {
	startTime, startErr := time.Parse("15:04", start)
	endTime, endErr := time.Parse("15:04", end)
	return startErr == nil && endErr == nil && endTime.After(startTime)
}
