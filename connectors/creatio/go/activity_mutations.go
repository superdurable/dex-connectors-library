// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateActivityInput supplies a title and schedule for a new activity.
type CreateActivityInput struct {
	// Title is the required full title.
	Title string `json:"title"`
	// Start is the RFC3339 start time.
	Start string `json:"start"`
	// Due is the RFC3339 end time, later than Start.
	Due string `json:"due"`
}

// UpdateActivityInput replaces selected activity fields.
type UpdateActivityInput struct {
	// ActivityID identifies an existing activity.
	ActivityID string `json:"activityId"`
	// Title replaces the full title when set.
	Title *string `json:"title,omitempty"`
	// Start replaces the RFC3339 start time when set.
	Start *string `json:"start,omitempty"`
	// Due replaces the RFC3339 end time when set.
	Due *string `json:"due,omitempty"`
}

// DeleteActivityInput identifies an activity to delete.
type DeleteActivityInput struct {
	// ActivityID is the Activity.Id UUID.
	ActivityID string `json:"activityId"`
}

// ActivityWriteOutput confirms the target UUID of an update or deletion.
type ActivityWriteOutput struct {
	// ID is the target Activity.Id UUID.
	ID string `json:"id"`
}

// CreateActivityOperation creates one activity with bounded sends.
type CreateActivityOperation struct{ client *Client }

// UpdateActivityOperation changes selected fields of one activity.
type UpdateActivityOperation struct{ client *Client }

// DeleteActivityOperation removes one activity.
type DeleteActivityOperation struct{ client *Client }

// CreateActivity binds the mutation to this client.
func (client *Client) CreateActivity() CreateActivityOperation {
	return CreateActivityOperation{client}
}

// UpdateActivity binds the mutation to this client.
func (client *Client) UpdateActivity() UpdateActivityOperation {
	return UpdateActivityOperation{client}
}

// DeleteActivity binds the mutation to this client.
func (client *Client) DeleteActivity() DeleteActivityOperation {
	return DeleteActivityOperation{client}
}

// Definition returns the generated mutation contract.
func (CreateActivityOperation) Definition() sdkgo.MutationDefinition { return CreateActivityDefinition }

// Definition returns the generated mutation contract.
func (UpdateActivityOperation) Definition() sdkgo.MutationDefinition { return UpdateActivityDefinition }

// Definition returns the generated mutation contract.
func (DeleteActivityOperation) Definition() sdkgo.MutationDefinition { return DeleteActivityDefinition }

// IdempotencyKey correlates bounded sends; Creatio does not deduplicate this key.
func (CreateActivityOperation) IdempotencyKey(id sdkgo.CallID, _ CreateActivityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies one repeatable absolute update.
func (UpdateActivityOperation) IdempotencyKey(id sdkgo.CallID, _ UpdateActivityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey identifies one repeatable deletion.
func (DeleteActivityOperation) IdempotencyKey(id sdkgo.CallID, _ DeleteActivityInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke creates an activity and reads back a title match before resending.
func (operation CreateActivityOperation) Invoke(call sdkgo.Call, input CreateActivityInput) sdkgo.MutationAttempt[ActivitySummary] {
	start, startErr := time.Parse(time.RFC3339, input.Start)
	due, dueErr := time.Parse(time.RFC3339, input.Due)
	if strings.TrimSpace(input.Title) == "" || len(input.Title) > 255 || startErr != nil || dueErr != nil || !due.After(start) {
		return createActivityBranch(CreateActivityBranchDefect, sdkgo.FailureValidation, "invalid activity title or schedule")
	}
	fence, err := singledispatch.NewFence(call, CreateActivityDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createActivityBranch(CreateActivityBranchDefect, sdkgo.FailureLocalDefect, "dispatch fence unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return createActivityUncertain("previous creation cannot be confirmed")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
			return createActivityBranch(CreateActivityBranchDefect, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
		}
		return sdkgo.NewMutationRetry[ActivitySummary](activitiesFailure("createActivity", sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
	}
	if decision.Kind != singledispatch.SendAllowed {
		if decision.Marker.ProviderID != "" {
			response := operation.client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Activity("+decision.Marker.ProviderID+")?$select=Id%2CTitle", nil)
			if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
				return sdkgo.NewMutationRetry[ActivitySummary](activitiesFailure("createActivity", sdkgo.FailureAvailability, "Creatio activity read-back unavailable"), response.retryAfter)
			}
			if response.status != http.StatusOK {
				return createActivityUncertain("saved activity ID cannot be confirmed")
			}
			activity, decodeErr := decodeActivityRecord(response.body)
			if decodeErr != nil || !strings.EqualFold(activity.ID, decision.Marker.ProviderID) {
				return createActivityUncertain("saved activity ID returned an invalid activity")
			}
			return sdkgo.NewMutationBranch(CreateActivityBranchCreated, activity, nil, sdkgo.Receipt{})
		}
		if input.Title != "" {
			matching, known, retry := operation.client.findActivityByTitle(call, token, input.Title)
			if retry {
				return sdkgo.NewMutationRetry[ActivitySummary](activitiesFailure("createActivity", sdkgo.FailureAvailability, "Creatio activity read-back unavailable"), 0)
			}
			if matching != nil {
				if strings.EqualFold(matching.ID, decision.Marker.ProviderID) {
					return sdkgo.NewMutationBranch(CreateActivityBranchCreated, *matching, nil, sdkgo.Receipt{})
				}
				return createActivityUncertain("a matching activity cannot prove this creation")
			}
			if !known {
				return createActivityUncertain("previous creation cannot be confirmed")
			}
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return createActivityUncertain("previous creation cannot be confirmed")
		}
	}
	body, marshalErr := json.Marshal(struct {
		Title string `json:"Title"`
		Start string `json:"Start"`
		Due   string `json:"Due"`
	}{input.Title, input.Start, input.Due})
	if marshalErr != nil {
		return createActivityBranch(CreateActivityBranchDefect, sdkgo.FailureLocalDefect, "activity request could not be encoded")
	}
	var response odataResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.doODataRequest(requestContext, token, http.MethodPost, "/0/odata/Activity", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[ActivitySummary](activitiesFailure("createActivity", sdkgo.FailureAvailability, "dispatch did not start"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[ActivitySummary](activitiesFailure("createActivity", sdkgo.FailureAvailability, "retry checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[ActivitySummary](activitiesFailure("createActivity", kind, "Creatio did not create the activity"), response.retryAfter)
	}
	if response.err != nil || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[ActivitySummary](activitiesFailure("createActivity", sdkgo.FailureTransport, "activity creation outcome is unknown"), 0)
	}
	if response.status >= 400 {
		if decision.MaximumPriorSends > 0 {
			return createActivityUncertain("an earlier activity creation may have succeeded")
		}
		return createActivityBranch(CreateActivityBranchProviderRejected, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.status))
	}
	activity, decodeErr := decodeActivityRecord(response.body)
	if response.status != http.StatusCreated || decodeErr != nil {
		return sdkgo.NewMutationRetry[ActivitySummary](activitiesFailure("createActivity", sdkgo.FailureProtocol, "Creatio returned invalid activity creation JSON"), 0)
	}
	if err := fence.RecordProviderID(activity.ID); err != nil {
		return sdkgo.NewMutationRetry[ActivitySummary](activitiesFailure("createActivity", sdkgo.FailureAvailability, "provider ID checkpoint unavailable"), 0)
	}
	return sdkgo.NewMutationBranch(CreateActivityBranchCreated, activity, nil, sdkgo.Receipt{})
}

// Invoke patches selected fields without changing other activity properties.
func (operation UpdateActivityOperation) Invoke(call sdkgo.Call, input UpdateActivityInput) sdkgo.MutationAttempt[ActivityWriteOutput] {
	if !contactUUIDPattern.MatchString(input.ActivityID) || (input.Title == nil && input.Start == nil && input.Due == nil) || (input.Title != nil && strings.TrimSpace(*input.Title) == "") {
		return activityWriteBranch("updateActivity", UpdateActivityBranchDefect, sdkgo.FailureValidation, "invalid activity update")
	}
	if input.Start != nil {
		if _, err := time.Parse(time.RFC3339, *input.Start); err != nil {
			return activityWriteBranch("updateActivity", UpdateActivityBranchDefect, sdkgo.FailureValidation, "invalid activity start")
		}
	}
	if input.Due != nil {
		if _, err := time.Parse(time.RFC3339, *input.Due); err != nil {
			return activityWriteBranch("updateActivity", UpdateActivityBranchDefect, sdkgo.FailureValidation, "invalid activity due")
		}
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		return activityWriteCredentialFailure("updateActivity", UpdateActivityBranchDefect, err)
	}
	fields := map[string]any{}
	if input.Title != nil {
		fields["Title"] = *input.Title
	}
	if input.Start != nil {
		fields["Start"] = *input.Start
	}
	if input.Due != nil {
		fields["Due"] = *input.Due
	}
	body, marshalErr := json.Marshal(fields)
	if marshalErr != nil {
		return activityWriteBranch("updateActivity", UpdateActivityBranchDefect, sdkgo.FailureLocalDefect, "activity update could not be encoded")
	}
	response := operation.client.doODataRequest(call.Context, token, http.MethodPatch, "/0/odata/Activity("+input.ActivityID+")", body)
	return classifyActivityWrite("updateActivity", input.ActivityID, response, UpdateActivityBranchUpdated, UpdateActivityBranchNotFound, UpdateActivityBranchProviderRejected)
}

// Invoke permanently deletes one Activity record.
func (operation DeleteActivityOperation) Invoke(call sdkgo.Call, input DeleteActivityInput) sdkgo.MutationAttempt[ActivityWriteOutput] {
	if !contactUUIDPattern.MatchString(input.ActivityID) {
		return activityWriteBranch("deleteActivity", DeleteActivityBranchDefect, sdkgo.FailureValidation, "invalid activity UUID")
	}
	token, err := operation.client.accessToken(call)
	if err != nil {
		return activityWriteCredentialFailure("deleteActivity", DeleteActivityBranchDefect, err)
	}
	response := operation.client.doODataRequest(call.Context, token, http.MethodDelete, "/0/odata/Activity("+input.ActivityID+")", nil)
	return classifyActivityWrite("deleteActivity", input.ActivityID, response, DeleteActivityBranchDeleted, DeleteActivityBranchNotFound, DeleteActivityBranchProviderRejected)
}

func (client *Client) findActivityByTitle(call sdkgo.Call, token string, title string) (*ActivitySummary, bool, bool) {
	escape := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	filter := "Title eq '" + escape(title) + "'"
	query := url.Values{"$filter": {filter}, "$select": {"Id,Title"}, "$top": {"2"}}
	response := client.doODataRequest(call.Context, token, http.MethodGet, "/0/odata/Activity?"+query.Encode(), nil)
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status >= 500 {
		return nil, false, true
	}
	if response.status != http.StatusOK {
		return nil, false, false
	}
	var envelope struct {
		Value []json.RawMessage `json:"value"`
	}
	if json.Unmarshal(response.body, &envelope) != nil || envelope.Value == nil {
		return nil, false, true
	}
	if len(envelope.Value) == 0 {
		return nil, true, false
	}
	for _, record := range envelope.Value {
		activity, err := decodeActivityRecord(record)
		if err != nil {
			return nil, false, true
		}
		if activity.Title == title {
			return &activity, true, false
		}
	}
	return nil, true, false
}

func classifyActivityWrite(operation string, targetID string, response odataResponse, successBranch sdkgo.BranchID, notFoundBranch sdkgo.BranchID, rejectedBranch sdkgo.BranchID) sdkgo.MutationAttempt[ActivityWriteOutput] {
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[ActivityWriteOutput](activitiesFailure(operation, sdkgo.FailureRateLimit, "Creatio rate limited the write"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[ActivityWriteOutput](activitiesFailure(operation, sdkgo.FailureAvailability, "Creatio write is unavailable"), response.retryAfter)
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[ActivityWriteOutput](activitiesFailure(operation, sdkgo.FailureTransport, "Creatio write response failed"), 0)
	}
	if response.status == http.StatusNotFound {
		return activityWriteBranch(operation, notFoundBranch, sdkgo.FailureNotFound, "Creatio activity was not found")
	}
	if response.status != http.StatusNoContent {
		return activityWriteBranch(operation, rejectedBranch, sdkgo.FailureProviderRejection, "Creatio rejected HTTP "+strconv.Itoa(response.status))
	}
	return sdkgo.NewMutationBranch(successBranch, ActivityWriteOutput{ID: targetID}, nil, sdkgo.Receipt{})
}

func activityWriteCredentialFailure(operation string, defectBranch sdkgo.BranchID, err error) sdkgo.MutationAttempt[ActivityWriteOutput] {
	if sdkgo.IsReauthorizationRequired(err) || errors.Is(err, errInvalidAccessToken) {
		return activityWriteBranch(operation, defectBranch, sdkgo.FailureAuthentication, "Creatio connection needs authorization")
	}
	return sdkgo.NewMutationRetry[ActivityWriteOutput](activitiesFailure(operation, sdkgo.FailureTransport, "Creatio credential minting failed"), 0)
}

func activityWriteBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ActivityWriteOutput] {
	failure := activitiesFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, ActivityWriteOutput{}, &failure, sdkgo.Receipt{})
}

func createActivityBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ActivitySummary] {
	failure := activitiesFailure("createActivity", kind, message)
	return sdkgo.NewMutationBranch(branch, ActivitySummary{}, &failure, sdkgo.Receipt{})
}

func createActivityUncertain(message string) sdkgo.MutationAttempt[ActivitySummary] {
	return sdkgo.NewMutationUncertain(ActivitySummary{}, activitiesFailure("createActivity", sdkgo.FailureTransport, message), sdkgo.Receipt{})
}
