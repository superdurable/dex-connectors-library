// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package basecamp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ScheduleEntry contains one Basecamp calendar entry.
type ScheduleEntry struct {
	// ID is the numeric entry ID.
	ID string `json:"id"`
	// Summary is the entry title.
	Summary string `json:"summary"`
	// Description is optional rich text.
	Description string `json:"description,omitempty"`
	// StartsAt is an ISO 8601 date-time or all-day date.
	StartsAt string `json:"startsAt"`
	// EndsAt is an ISO 8601 date-time or all-day date.
	EndsAt string `json:"endsAt"`
	// AllDay reports whether only dates carry meaning.
	AllDay bool `json:"allDay,omitempty"`
	// Status is active or drafted.
	Status string `json:"status,omitempty"`
}

// ScheduleEntriesPage contains one provider page of calendar entries.
type ScheduleEntriesPage struct {
	// Entries are visible calendar entries on this page.
	Entries []ScheduleEntry `json:"entries"`
}

// ListScheduleEntriesInput identifies one project calendar.
type ListScheduleEntriesInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// ScheduleID comes from getProject's dock.
	ScheduleID string `json:"scheduleId"`
}

// GetScheduleEntryInput identifies one calendar entry.
type GetScheduleEntryInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// EntryID comes from listScheduleEntries or a Basecamp URL.
	EntryID string `json:"entryId"`
}

// CreateScheduleEntryInput supplies a calendar and interval.
type CreateScheduleEntryInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// ScheduleID comes from getProject.
	ScheduleID string `json:"scheduleId"`
	// Summary is the non-blank entry title.
	Summary string `json:"summary"`
	// Description is optional rich text.
	Description string `json:"description,omitempty"`
	// StartsAt is RFC3339, or YYYY-MM-DD when AllDay is true.
	StartsAt string `json:"startsAt"`
	// EndsAt is RFC3339, or YYYY-MM-DD when AllDay is true.
	EndsAt string `json:"endsAt"`
	// AllDay treats StartsAt and EndsAt as calendar dates.
	AllDay bool `json:"allDay,omitempty"`
	// Draft creates without publishing when true.
	Draft bool `json:"draft,omitempty"`
	// ParticipantIDs are Basecamp person IDs.
	ParticipantIDs []string `json:"participantIds,omitempty"`
}

// UpdateScheduleEntryInput replaces the selected entry's core fields.
type UpdateScheduleEntryInput struct {
	// AccountID comes from listAccounts.
	AccountID string `json:"accountId"`
	// EntryID comes from listScheduleEntries.
	EntryID string `json:"entryId"`
	// Summary is the non-blank replacement title.
	Summary string `json:"summary"`
	// Description is replacement rich text; blank clears it.
	Description string `json:"description"`
	// StartsAt is RFC3339, or YYYY-MM-DD when AllDay is true.
	StartsAt string `json:"startsAt"`
	// EndsAt is RFC3339, or YYYY-MM-DD when AllDay is true.
	EndsAt string `json:"endsAt"`
	// AllDay treats StartsAt and EndsAt as calendar dates.
	AllDay bool `json:"allDay"`
	// Status preserves active or drafted publication state.
	Status string `json:"status"`
	// ParticipantIDs replaces the participant list when provided; nil preserves it.
	ParticipantIDs []string `json:"participantIds,omitempty"`
}

// ScheduleEntryActionResult identifies the entry and applied action.
type ScheduleEntryActionResult struct {
	// EntryID is the selected entry.
	EntryID string `json:"entryId"`
	// Status names the applied action.
	Status string `json:"status"`
}

// ListScheduleEntriesOperation reads one page of calendar entries.
type ListScheduleEntriesOperation struct{ client *Client }

// ListScheduleEntries binds the calendar list Query.
func (client *Client) ListScheduleEntries() ListScheduleEntriesOperation {
	return ListScheduleEntriesOperation{client}
}

// Definition returns the generated Query contract.
func (ListScheduleEntriesOperation) Definition() sdkgo.QueryDefinition {
	return ListScheduleEntriesDefinition
}

// Invoke reads the selected calendar's active entries.
func (operation ListScheduleEntriesOperation) Invoke(call sdkgo.Call, input ListScheduleEntriesInput) sdkgo.QueryAttempt[ScheduleEntriesPage] {
	if !validResourceID(input.AccountID) || !validResourceID(input.ScheduleID) {
		return resourceBranch[ScheduleEntriesPage]("listScheduleEntries", ListScheduleEntriesBranchDefect, sdkgo.FailureValidation, "calendar IDs are invalid")
	}
	response := operation.client.getAccountResource(call, input.AccountID, "/schedules/"+input.ScheduleID+"/entries.json")
	return resourceQueryAttempt(response, "listScheduleEntries", ListScheduleEntriesBranchListed, "", ListScheduleEntriesBranchProviderRejected, ListScheduleEntriesBranchInvalidResponse, ListScheduleEntriesBranchDefect, decodeScheduleEntries)
}

// GetScheduleEntryOperation reads one calendar entry.
type GetScheduleEntryOperation struct{ client *Client }

// GetScheduleEntry binds the singular Query.
func (client *Client) GetScheduleEntry() GetScheduleEntryOperation {
	return GetScheduleEntryOperation{client}
}

// Definition returns the generated Query contract.
func (GetScheduleEntryOperation) Definition() sdkgo.QueryDefinition {
	return GetScheduleEntryDefinition
}

// Invoke reads the selected calendar entry.
func (operation GetScheduleEntryOperation) Invoke(call sdkgo.Call, input GetScheduleEntryInput) sdkgo.QueryAttempt[ScheduleEntry] {
	if !validResourceID(input.AccountID) || !validResourceID(input.EntryID) {
		return resourceBranch[ScheduleEntry]("getScheduleEntry", GetScheduleEntryBranchDefect, sdkgo.FailureValidation, "entry IDs are invalid")
	}
	response := operation.client.getAccountResource(call, input.AccountID, "/schedule_entries/"+input.EntryID+".json")
	return resourceQueryAttempt(response, "getScheduleEntry", GetScheduleEntryBranchFound, GetScheduleEntryBranchNotFound, GetScheduleEntryBranchProviderRejected, GetScheduleEntryBranchInvalidResponse, GetScheduleEntryBranchDefect, func(body []byte) (ScheduleEntry, error) { return decodeScheduleEntry(body, input.EntryID) })
}

// CreateScheduleEntryOperation creates one entry with bounded sends.
type CreateScheduleEntryOperation struct{ client *Client }

// CreateScheduleEntry binds the calendar creation Mutation.
func (client *Client) CreateScheduleEntry() CreateScheduleEntryOperation {
	return CreateScheduleEntryOperation{client}
}

// Definition returns the generated Mutation contract.
func (CreateScheduleEntryOperation) Definition() sdkgo.MutationDefinition {
	return CreateScheduleEntryDefinition
}

// IdempotencyKey records the Dex call identity; Basecamp does not deduplicate it.
func (CreateScheduleEntryOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateScheduleEntryInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an entry, bounding possible duplicate calendar posts.
func (operation CreateScheduleEntryOperation) Invoke(call sdkgo.Call, input CreateScheduleEntryInput) sdkgo.MutationAttempt[ScheduleEntry] {
	if !validResourceID(input.AccountID) || !validResourceID(input.ScheduleID) || !validScheduleEntryFields(input.Summary, input.Description, input.StartsAt, input.EndsAt, input.AllDay) {
		return projectMutationBranch[ScheduleEntry]("createScheduleEntry", CreateScheduleEntryBranchDefect, sdkgo.FailureValidation, "calendar entry input is invalid")
	}
	participants, valid := scheduleParticipantIDs(input.ParticipantIDs)
	if !valid {
		return projectMutationBranch[ScheduleEntry]("createScheduleEntry", CreateScheduleEntryBranchDefect, sdkgo.FailureValidation, "participant ID is invalid")
	}
	status := "active"
	if input.Draft {
		status = "drafted"
	}
	requestBody := struct {
		Summary        string   `json:"summary"`
		Description    string   `json:"description,omitempty"`
		StartsAt       string   `json:"starts_at"`
		EndsAt         string   `json:"ends_at"`
		AllDay         bool     `json:"all_day"`
		Status         string   `json:"status"`
		ParticipantIDs *[]int64 `json:"participant_ids,omitempty"`
	}{Summary: input.Summary, Description: input.Description, StartsAt: input.StartsAt, EndsAt: input.EndsAt, AllDay: input.AllDay, Status: status}
	if input.ParticipantIDs != nil {
		requestBody.ParticipantIDs = &participants
	}
	fence, err := singledispatch.NewFence(call, CreateScheduleEntryDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return projectMutationBranch[ScheduleEntry]("createScheduleEntry", CreateScheduleEntryBranchDefect, sdkgo.FailureLocalDefect, "entry dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return projectMutationUncertain[ScheduleEntry]("createScheduleEntry", "entry creation may have completed without a stable lookup key")
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.accountMutation(call, requestContext, http.MethodPost, input.AccountID, "/schedules/"+input.ScheduleID+"/entries.json", requestBody)
		return nil
	})
	if err != nil {
		return projectMutationRetry[ScheduleEntry]("createScheduleEntry", sdkgo.FailureAvailability, "entry dispatch was deferred", 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return projectMutationRetry[ScheduleEntry]("createScheduleEntry", sdkgo.FailureAvailability, "entry resend checkpoint is unavailable", 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return projectMutationRetry[ScheduleEntry]("createScheduleEntry", kind, "Basecamp temporarily rejected entry creation", response.retryAfter)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return projectMutationUncertain[ScheduleEntry]("createScheduleEntry", "an earlier entry creation may have completed")
		}
		return projectMutationBranch[ScheduleEntry]("createScheduleEntry", CreateScheduleEntryBranchDefect, sdkgo.FailureAuthentication, "Basecamp credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return projectMutationRetry[ScheduleEntry]("createScheduleEntry", sdkgo.FailureTransport, "entry creation outcome is unknown", response.retryAfter)
	}
	if response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return projectMutationUncertain[ScheduleEntry]("createScheduleEntry", "an earlier entry creation may have completed")
		}
		return projectMutationBranch[ScheduleEntry]("createScheduleEntry", CreateScheduleEntryBranchProviderRejected, sdkgo.FailureProviderRejection, "Basecamp rejected entry creation")
	}
	entry, err := decodeScheduleEntry(response.body, "")
	if err != nil || entry.Summary != input.Summary {
		return projectMutationRetry[ScheduleEntry]("createScheduleEntry", sdkgo.FailureProtocol, "Basecamp returned invalid created entry JSON", 0)
	}
	return sdkgo.NewMutationBranch(CreateScheduleEntryBranchCreated, entry, nil, sdkgo.Receipt{})
}

// UpdateScheduleEntryOperation replaces one entry's core fields.
type UpdateScheduleEntryOperation struct{ client *Client }

// UpdateScheduleEntry binds the calendar update Mutation.
func (client *Client) UpdateScheduleEntry() UpdateScheduleEntryOperation {
	return UpdateScheduleEntryOperation{client}
}

// Definition returns the generated Mutation contract.
func (UpdateScheduleEntryOperation) Definition() sdkgo.MutationDefinition {
	return UpdateScheduleEntryDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (UpdateScheduleEntryOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateScheduleEntryInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the selected entry's title, interval, description, and status.
func (operation UpdateScheduleEntryOperation) Invoke(call sdkgo.Call, input UpdateScheduleEntryInput) sdkgo.MutationAttempt[ScheduleEntry] {
	if !validResourceID(input.AccountID) || !validResourceID(input.EntryID) || !validScheduleEntryFields(input.Summary, input.Description, input.StartsAt, input.EndsAt, input.AllDay) || (input.Status != "active" && input.Status != "drafted") {
		return projectMutationBranch[ScheduleEntry]("updateScheduleEntry", UpdateScheduleEntryBranchDefect, sdkgo.FailureValidation, "calendar entry input is invalid")
	}
	participants, valid := scheduleParticipantIDs(input.ParticipantIDs)
	if !valid {
		return projectMutationBranch[ScheduleEntry]("updateScheduleEntry", UpdateScheduleEntryBranchDefect, sdkgo.FailureValidation, "participant ID is invalid")
	}
	requestBody := struct {
		Summary        string   `json:"summary"`
		Description    string   `json:"description,omitempty"`
		StartsAt       string   `json:"starts_at"`
		EndsAt         string   `json:"ends_at"`
		AllDay         bool     `json:"all_day"`
		Status         string   `json:"status"`
		ParticipantIDs *[]int64 `json:"participant_ids,omitempty"`
	}{Summary: input.Summary, Description: input.Description, StartsAt: input.StartsAt, EndsAt: input.EndsAt, AllDay: input.AllDay, Status: input.Status}
	if input.ParticipantIDs != nil {
		requestBody.ParticipantIDs = &participants
	}
	response := operation.client.accountMutation(call, call.Context, http.MethodPut, input.AccountID, "/schedule_entries/"+input.EntryID+".json", requestBody)
	if attempt, done := projectMutationResponse[ScheduleEntry](response, "updateScheduleEntry", http.StatusOK, UpdateScheduleEntryBranchNotFound, UpdateScheduleEntryBranchProviderRejected, UpdateScheduleEntryBranchDefect, UpdateScheduleEntryBranchInvalidResponse); done {
		return attempt
	}
	entry, err := decodeScheduleEntry(response.body, input.EntryID)
	if err != nil || entry.Summary != input.Summary {
		return projectMutationBranch[ScheduleEntry]("updateScheduleEntry", UpdateScheduleEntryBranchInvalidResponse, sdkgo.FailureProtocol, "Basecamp returned invalid updated entry JSON")
	}
	return sdkgo.NewMutationBranch(UpdateScheduleEntryBranchUpdated, entry, nil, sdkgo.Receipt{})
}

// TrashScheduleEntryOperation trashes an entry recording.
type TrashScheduleEntryOperation struct{ client *Client }

// TrashScheduleEntry binds the calendar trash Mutation.
func (client *Client) TrashScheduleEntry() TrashScheduleEntryOperation {
	return TrashScheduleEntryOperation{client}
}

// Definition returns the generated Mutation contract.
func (TrashScheduleEntryOperation) Definition() sdkgo.MutationDefinition {
	return TrashScheduleEntryDefinition
}

// IdempotencyKey records the stable Dex call identity.
func (TrashScheduleEntryOperation) IdempotencyKey(callID sdkgo.CallID, _ GetScheduleEntryInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke trashes the selected entry recording.
func (operation TrashScheduleEntryOperation) Invoke(call sdkgo.Call, input GetScheduleEntryInput) sdkgo.MutationAttempt[ScheduleEntryActionResult] {
	if !validResourceID(input.AccountID) || !validResourceID(input.EntryID) {
		return projectMutationBranch[ScheduleEntryActionResult]("trashScheduleEntry", TrashScheduleEntryBranchDefect, sdkgo.FailureValidation, "entry IDs are invalid")
	}
	response := operation.client.accountMutation(call, call.Context, http.MethodPut, input.AccountID, "/recordings/"+input.EntryID+"/status/trashed.json", nil)
	if attempt, done := projectMutationResponse[ScheduleEntryActionResult](response, "trashScheduleEntry", http.StatusNoContent, TrashScheduleEntryBranchNotFound, TrashScheduleEntryBranchProviderRejected, TrashScheduleEntryBranchDefect, TrashScheduleEntryBranchProviderRejected); done {
		return attempt
	}
	return sdkgo.NewMutationBranch(TrashScheduleEntryBranchTrashed, ScheduleEntryActionResult{EntryID: input.EntryID, Status: "trashed"}, nil, sdkgo.Receipt{})
}

func validScheduleEntryFields(summary, description, startsAt, endsAt string, allDay bool) bool {
	if strings.TrimSpace(summary) == "" || len(summary) > 512 || len(description) > 100000 {
		return false
	}
	format := time.RFC3339
	if allDay {
		format = "2006-01-02"
	}
	start, startErr := time.Parse(format, startsAt)
	end, endErr := time.Parse(format, endsAt)
	return startErr == nil && endErr == nil && !end.Before(start)
}

func scheduleParticipantIDs(values []string) ([]int64, bool) {
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if !validResourceID(value) {
			return nil, false
		}
		personID, _ := strconv.ParseInt(value, 10, 64)
		result = append(result, personID)
	}
	return result, true
}

func decodeScheduleEntries(body []byte) (ScheduleEntriesPage, error) {
	var records []struct {
		ID          int64  `json:"id"`
		Summary     string `json:"summary"`
		Description string `json:"description"`
		StartsAt    string `json:"starts_at"`
		EndsAt      string `json:"ends_at"`
		AllDay      bool   `json:"all_day"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(body, &records); err != nil || records == nil {
		return ScheduleEntriesPage{}, fmt.Errorf("schedule entries are invalid")
	}
	page := ScheduleEntriesPage{Entries: make([]ScheduleEntry, 0, len(records))}
	for _, record := range records {
		if record.ID <= 0 || !validScheduleEntryFields(record.Summary, record.Description, record.StartsAt, record.EndsAt, record.AllDay) {
			return ScheduleEntriesPage{}, fmt.Errorf("schedule entry is invalid")
		}
		page.Entries = append(page.Entries, ScheduleEntry{ID: strconv.FormatInt(record.ID, 10), Summary: record.Summary, Description: record.Description, StartsAt: record.StartsAt, EndsAt: record.EndsAt, AllDay: record.AllDay, Status: record.Status})
	}
	return page, nil
}

func decodeScheduleEntry(body []byte, expectedID string) (ScheduleEntry, error) {
	var record struct {
		ID          int64  `json:"id"`
		Summary     string `json:"summary"`
		Description string `json:"description"`
		StartsAt    string `json:"starts_at"`
		EndsAt      string `json:"ends_at"`
		AllDay      bool   `json:"all_day"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(body, &record); err != nil || record.ID <= 0 || (expectedID != "" && strconv.FormatInt(record.ID, 10) != expectedID) || !validScheduleEntryFields(record.Summary, record.Description, record.StartsAt, record.EndsAt, record.AllDay) {
		return ScheduleEntry{}, fmt.Errorf("schedule entry is invalid")
	}
	return ScheduleEntry{ID: strconv.FormatInt(record.ID, 10), Summary: record.Summary, Description: record.Description, StartsAt: record.StartsAt, EndsAt: record.EndsAt, AllDay: record.AllDay, Status: record.Status}, nil
}
